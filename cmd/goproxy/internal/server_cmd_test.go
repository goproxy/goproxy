package internal

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goproxy/goproxy"
	"github.com/spf13/cobra"
)

func TestNewServerCmdConfig(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		args                  []string
		wantReadHeaderTimeout time.Duration
		wantWriteTimeout      time.Duration
		wantIdleTimeout       time.Duration
	}{
		{"Default", nil, 10 * time.Second, 20 * time.Minute, time.Minute},
		{"ReadHeaderTimeout", []string{"--read-header-timeout=250ms"}, 250 * time.Millisecond, 20 * time.Minute, time.Minute},
		{"DisabledReadHeaderTimeout", []string{"--read-header-timeout=0"}, 0, 20 * time.Minute, time.Minute},
		{"WriteTimeout", []string{"--write-timeout=250ms"}, 10 * time.Second, 250 * time.Millisecond, time.Minute},
		{"DisabledWriteTimeout", []string{"--write-timeout=0"}, 10 * time.Second, 0, time.Minute},
		{"IdleTimeout", []string{"--idle-timeout=250ms"}, 10 * time.Second, 20 * time.Minute, 250 * time.Millisecond},
		{"DisabledIdleTimeout", []string{"--idle-timeout=0"}, 10 * time.Second, 20 * time.Minute, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cfg := newServerCmdConfig(cmd)
			if err := cmd.Flags().Parse(tt.args); err != nil {
				t.Fatal(err)
			}
			if got, want := cfg.readHeaderTimeout, tt.wantReadHeaderTimeout; got != want {
				t.Errorf("got read header timeout %v, want %v", got, want)
			}
			if got, want := cfg.writeTimeout, tt.wantWriteTimeout; got != want {
				t.Errorf("got write timeout %v, want %v", got, want)
			}
			if got, want := cfg.idleTimeout, tt.wantIdleTimeout; got != want {
				t.Errorf("got idle timeout %v, want %v", got, want)
			}
		})
	}
}

func TestServerCmdConfigValidate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"Default", nil, ""},
		{"TLS", []string{"--tls-cert-file=cert.pem", "--tls-key-file=key.pem"}, ""},
		{"CertificateWithoutKey", []string{"--tls-cert-file=cert.pem"}, "invalid TLS configuration: --tls-cert-file and --tls-key-file must be set together"},
		{"KeyWithoutCertificate", []string{"--tls-key-file=key.pem"}, "invalid TLS configuration: --tls-cert-file and --tls-key-file must be set together"},
		{"EmptyKey", []string{"--tls-cert-file=cert.pem", "--tls-key-file="}, "invalid TLS configuration: --tls-cert-file and --tls-key-file must be set together"},
		{"RootPrefix", []string{"--path-prefix=/"}, ""},
		{"Prefix", []string{"--path-prefix=/proxy"}, ""},
		{"TrailingSlash", []string{"--path-prefix=/proxy/"}, ""},
		{"NestedPrefix", []string{"--path-prefix=/a/b/"}, ""},
		{"LiteralPrefix", []string{"--path-prefix=/{proxy}"}, ""},
		{"RelativePrefix", []string{"--path-prefix=proxy"}, `invalid --path-prefix: "proxy" is not a clean absolute path`},
		{"DoubleRootSlash", []string{"--path-prefix=//"}, `invalid --path-prefix: "//" is not a clean absolute path`},
		{"DoubleTrailingSlash", []string{"--path-prefix=/proxy//"}, `invalid --path-prefix: "/proxy//" is not a clean absolute path`},
		{"RepeatedSlash", []string{"--path-prefix=/a//b"}, `invalid --path-prefix: "/a//b" is not a clean absolute path`},
		{"DotSegment", []string{"--path-prefix=/a/./b"}, `invalid --path-prefix: "/a/./b" is not a clean absolute path`},
		{"ParentSegment", []string{"--path-prefix=/a/../b"}, `invalid --path-prefix: "/a/../b" is not a clean absolute path`},
		{"NegativeConcurrency", []string{"--max-concurrent-direct-fetches=-1"}, "invalid --max-concurrent-direct-fetches: -1 must not be negative"},
		{"NegativeReadHeaderTimeout", []string{"--read-header-timeout=-1s"}, "invalid --read-header-timeout: -1s must not be negative"},
		{"NegativeWriteTimeout", []string{"--write-timeout=-1s"}, "invalid --write-timeout: -1s must not be negative"},
		{"NegativeIdleTimeout", []string{"--idle-timeout=-1s"}, "invalid --idle-timeout: -1s must not be negative"},
		{"NegativeFetchTimeout", []string{"--fetch-timeout=-1s"}, "invalid --fetch-timeout: -1s must not be negative"},
		{"NegativeConnectTimeout", []string{"--connect-timeout=-1s"}, "invalid --connect-timeout: -1s must not be negative"},
		{"NegativeShutdownTimeout", []string{"--shutdown-timeout=-1s"}, "invalid --shutdown-timeout: -1s must not be negative"},
		{"ZeroLimits", []string{"--read-header-timeout=0", "--write-timeout=0", "--idle-timeout=0", "--fetch-timeout=0", "--connect-timeout=0", "--shutdown-timeout=0", "--max-concurrent-direct-fetches=0"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cfg := newServerCmdConfig(cmd)
			if err := cmd.Flags().Parse(tt.args); err != nil {
				t.Fatal(err)
			}
			err := cfg.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("got error %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunServerCmd(t *testing.T) {
	t.Run("InvalidConfiguration", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			args    []string
			wantErr string
		}{
			{"PositionalArgument", []string{"unexpected"}, `unknown command "unexpected" for "server"`},
			{"BooleanArgument", []string{"--insecure", "false"}, `unknown command "false" for "server"`},
			{"TLS", []string{"--tls-cert-file=cert.pem"}, "invalid TLS configuration:"},
			{"PathPrefix", []string{"--path-prefix=proxy"}, "invalid --path-prefix:"},
			{"Concurrency", []string{"--max-concurrent-direct-fetches=-1"}, "invalid --max-concurrent-direct-fetches:"},
			{"Timeout", []string{"--idle-timeout=-1s"}, "invalid --idle-timeout:"},
			{"Cacher", []string{"--cacher=invalid"}, "invalid --cacher:"},
			{"S3", []string{"--cacher=s3"}, "invalid S3 bucket:"},
			{"LogFormat", []string{"--log-format=invalid"}, "invalid --log-format:"},
			{"Address", nil, "listen tcp: address invalid: missing port in address"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				cmd := newServerCmd()
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				cmd.SetArgs(append([]string{"--address=invalid"}, tt.args...))
				if err := cmd.ExecuteContext(t.Context()); err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
					t.Fatalf("got error %v, want prefix %q", err, tt.wantErr)
				}
			})
		}
	})
	t.Run("ReadHeaderTimeout", func(t *testing.T) {
		address := startTestServerCmd(t,
			"--read-header-timeout=500ms",
			"--fetch-timeout=20ms",
		)

		for _, tt := range []struct {
			name           string
			request        string
			keepAlive      bool
			wantStatusCode int
		}{
			{"NoRequest", "", false, 0},
			{"RequestLine", "GET /healthz", false, http.StatusBadRequest},
			{"Headers", "GET /healthz HTTP/1.1\r\nHost: localhost\r\n", false, 0},
			{"KeepAlive", "GET /healthz HTTP/1.1\r\nHost: localhost\r\n", true, 0},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				conn, err := net.DialTimeout("tcp", address, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Fatal(err)
				}
				reader := bufio.NewReader(conn)
				if tt.keepAlive {
					if _, err := io.WriteString(conn, "GET /healthz HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
						t.Fatal(err)
					}
					resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
					if err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
					if resp.StatusCode != http.StatusNoContent || resp.Close {
						t.Fatalf("got status %d, close %t, want status 204 and keep-alive", resp.StatusCode, resp.Close)
					}
				}
				if _, err := io.WriteString(conn, tt.request); err != nil {
					t.Fatal(err)
				}
				if tt.wantStatusCode != 0 {
					resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
					if err != nil {
						t.Fatal(err)
					}
					if resp.StatusCode != tt.wantStatusCode || !resp.Close {
						t.Errorf("got status %d, close %t, want status %d and closure", resp.StatusCode, resp.Close, tt.wantStatusCode)
					}
					if _, err := io.Copy(io.Discard, resp.Body); err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
				}
				if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
					t.Errorf("connection did not close after the header timeout: %v", err)
				}
			})
		}
	})

	t.Run("WriteTimeout", func(t *testing.T) {
		t.Run("Configuration", func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				time.Sleep(200 * time.Millisecond)
				io.WriteString(rw, "latest")
			}))
			defer upstream.Close()
			for _, tt := range []struct {
				name         string
				writeTimeout string
				wantError    bool
			}{
				{"Enabled", "100ms", true},
				{"Disabled", "0", false},
			} {
				t.Run(tt.name, func(t *testing.T) {
					address := startTestServerCmd(t,
						"--write-timeout="+tt.writeTimeout, "--fetch-timeout=2s",
						"--proxied-sumdbs=sumdb.example.com "+upstream.URL,
					)
					client := &http.Client{Timeout: 2 * time.Second}
					defer client.CloseIdleConnections()
					resp, err := client.Get("http://" + address + "/sumdb/sumdb.example.com/latest")
					if tt.wantError {
						if err == nil {
							resp.Body.Close()
							t.Fatal("configured write timeout did not expire")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					content, err := io.ReadAll(resp.Body)
					if err != nil || resp.StatusCode != http.StatusOK || string(content) != "latest" {
						t.Errorf("got status %d, content %q, error %v", resp.StatusCode, content, err)
					}
				})
			}
		})
		for _, tt := range []struct {
			name  string
			http2 bool
		}{
			{"HTTP1", false},
			{"HTTP2", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				startServer := func(t *testing.T, cfg *serverCmdConfig, base http.Handler) *httptest.Server {
					t.Helper()
					server := httptest.NewUnstartedServer(newServerHandler(cfg, base))
					server.Config.WriteTimeout = cfg.writeTimeout
					server.EnableHTTP2 = tt.http2
					if tt.http2 {
						server.StartTLS()
					} else {
						server.Start()
					}
					t.Cleanup(server.Close)
					server.Client().Timeout = 3 * time.Second
					return server
				}
				t.Run("CachedModule", func(t *testing.T) {
					cacher := goproxy.DirCacher(t.TempDir())
					content := strings.Repeat("content", 10_000)
					const target = "example.com/@v/v1.0.0.zip"
					if err := cacher.Put(t.Context(), target, strings.NewReader(content)); err != nil {
						t.Fatal(err)
					}
					server := startServer(t, &serverCmdConfig{writeTimeout: time.Second, fetchTimeout: time.Second}, &goproxy.Goproxy{Cacher: cacher})
					for _, tt := range []struct {
						name           string
						method         string
						header         http.Header
						wantStatusCode int
						wantContent    string
					}{
						{"GET", http.MethodGet, nil, http.StatusOK, content},
						{"HEAD", http.MethodHead, nil, http.StatusOK, ""},
						{"Range", http.MethodGet, http.Header{"Range": {"bytes=100-66000"}}, http.StatusPartialContent, content[100:66001]},
						{"HEADRange", http.MethodHead, http.Header{"Range": {"bytes=100-66000"}}, http.StatusOK, ""},
						{"NotModified", http.MethodGet, http.Header{"If-Modified-Since": {time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)}}, http.StatusNotModified, ""},
					} {
						t.Run(tt.name, func(t *testing.T) {
							req, err := http.NewRequestWithContext(t.Context(), tt.method, server.URL+"/"+target, nil)
							if err != nil {
								t.Fatal(err)
							}
							if tt.header != nil {
								req.Header = tt.header
							}
							resp, err := server.Client().Do(req)
							if err != nil {
								t.Fatal(err)
							}
							defer resp.Body.Close()
							body, err := io.ReadAll(resp.Body)
							if err != nil || resp.StatusCode != tt.wantStatusCode || string(body) != tt.wantContent {
								t.Errorf("got status %d, content length %d, error %v", resp.StatusCode, len(body), err)
							}
							if got, want := resp.Header.Get("Cache-Control"), "public, max-age=604800"; got != want {
								t.Errorf("got cache control %q, want %q", got, want)
							}
						})
					}
				})
				t.Run("RequestProcessing", func(t *testing.T) {
					server := startServer(t, &serverCmdConfig{writeTimeout: 100 * time.Millisecond}, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
						time.Sleep(200 * time.Millisecond)
						io.WriteString(rw, "content")
					}))
					if resp, err := server.Client().Get(server.URL + "/slow"); err == nil {
						resp.Body.Close()
						t.Fatal("request processing did not consume the write timeout")
					}
				})
				for _, copyContent := range []bool{false, true} {
					name := "Write"
					if copyContent {
						name = "Copy"
					}
					t.Run(name, func(t *testing.T) {
						t.Run("Stalled", func(t *testing.T) {
							const contentLength = 32 << 20
							file, err := os.CreateTemp(t.TempDir(), "content")
							if err != nil {
								t.Fatal(err)
							}
							defer file.Close()
							if err := file.Truncate(contentLength); err != nil {
								t.Fatal(err)
							}
							done := make(chan error, 1)
							server := startServer(t, &serverCmdConfig{writeTimeout: 100 * time.Millisecond}, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
								rw.Header().Set("Content-Length", fmt.Sprint(contentLength))
								rw.WriteHeader(http.StatusOK)
								var err error
								if copyContent {
									_, err = io.CopyN(rw, file, contentLength)
								} else {
									_, err = rw.Write(make([]byte, contentLength))
								}
								done <- err
							}))
							resp, err := server.Client().Get(server.URL + "/large")
							if err != nil {
								t.Fatal(err)
							}
							defer resp.Body.Close()
							select {
							case err := <-done:
								if !errors.Is(err, os.ErrDeadlineExceeded) {
									t.Errorf("got write error %v, want deadline exceeded", err)
								}
							case <-time.After(2 * time.Second):
								t.Fatal("stalled response write did not time out")
							}
							ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
								GotConn: func(info httptrace.GotConnInfo) {
									if tt.http2 && !info.Reused {
										t.Error("stalled stream closed the HTTP/2 connection")
									}
								},
							})
							req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/healthz", nil)
							if err != nil {
								t.Fatal(err)
							}
							health, err := server.Client().Do(req)
							if err != nil {
								t.Fatal(err)
							}
							health.Body.Close()
							if health.StatusCode != http.StatusNoContent {
								t.Errorf("got health status %d, want 204", health.StatusCode)
							}
							if n, err := io.Copy(io.Discard, resp.Body); err == nil || n >= contentLength {
								t.Errorf("incomplete response appeared complete: length %d, error %v", n, err)
							}
						})
						t.Run("FixedDeadline", func(t *testing.T) {
							done := make(chan error, 1)
							server := startServer(t, &serverCmdConfig{writeTimeout: 250 * time.Millisecond}, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
								send := func(w io.Writer) error {
									for range 24 {
										time.Sleep(75 * time.Millisecond)
										if _, err := w.Write(make([]byte, 32<<10)); err != nil {
											return err
										}
									}
									return nil
								}
								rw.WriteHeader(http.StatusOK)
								var err error
								if copyContent {
									reader, writer := io.Pipe()
									defer reader.Close()
									go func() { writer.CloseWithError(send(writer)) }()
									_, err = io.Copy(rw, reader)
								} else {
									err = send(rw)
								}
								done <- err
							}))
							resp, err := server.Client().Get(server.URL + "/large")
							if err != nil {
								t.Fatal(err)
							}
							defer resp.Body.Close()
							n, err := io.Copy(io.Discard, resp.Body)
							if err == nil || n >= 24*(32<<10) {
								t.Errorf("write deadline was extended by progress: length %d, error %v", n, err)
							}
							if err := <-done; !errors.Is(err, os.ErrDeadlineExceeded) {
								t.Errorf("got write error %v, want deadline exceeded", err)
							}
						})
					})
				}
				t.Run("TimeoutResponseAndKeepAlive", func(t *testing.T) {
					server := startServer(t, &serverCmdConfig{writeTimeout: 100 * time.Millisecond, fetchTimeout: 20 * time.Millisecond}, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
						<-req.Context().Done()
						rw.WriteHeader(http.StatusGatewayTimeout)
						io.WriteString(rw, "fetch timed out")
					}))
					client := server.Client()
					resp, err := client.Get(server.URL + "/slow")
					if err != nil {
						t.Fatal(err)
					}
					content, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil || resp.StatusCode != http.StatusGatewayTimeout || string(content) != "fetch timed out" {
						t.Fatalf("got status %d, content %q, error %v", resp.StatusCode, content, err)
					}
					time.Sleep(200 * time.Millisecond)
					ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
						GotConn: func(info httptrace.GotConnInfo) {
							if !info.Reused {
								t.Error("completed response left a write deadline on the connection")
							}
						},
					})
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/healthz", nil)
					if err != nil {
						t.Fatal(err)
					}
					resp, err = client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
					if resp.StatusCode != http.StatusNoContent {
						t.Errorf("got status %d, want 204", resp.StatusCode)
					}
				})
				t.Run("Disabled", func(t *testing.T) {
					done := make(chan error, 1)
					server := startServer(t, &serverCmdConfig{fetchTimeout: 20 * time.Millisecond}, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
						rw.WriteHeader(http.StatusOK)
						content := make([]byte, 64<<10)
						for range 512 {
							if _, err := rw.Write(content); err != nil {
								done <- err
								return
							}
						}
						done <- nil
					}))
					resp, err := server.Client().Get(server.URL + "/large")
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					select {
					case err := <-done:
						t.Fatalf("write timeout was not disabled: %v", err)
					case <-time.After(200 * time.Millisecond):
					}
					resp.Body.Close()
					select {
					case <-done:
					case <-time.After(2 * time.Second):
						t.Error("handler did not stop after closing the client")
					}
				})
			})
		}
	})

	t.Run("IdleTimeout", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			request string
		}{
			{"NoRequest", ""},
			{"PartialNextRequest", "GET"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				address := startTestServerCmd(t, "--idle-timeout=100ms", "--read-header-timeout=2s")
				conn, err := net.DialTimeout("tcp", address, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Fatal(err)
				}
				reader := bufio.NewReader(conn)
				for range 2 {
					if _, err := io.WriteString(conn, "GET /healthz HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
						t.Fatal(err)
					}
					resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
					if err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
					if resp.StatusCode != http.StatusNoContent || resp.Close {
						t.Fatalf("got status %d, close %t, want status 204 and keep-alive", resp.StatusCode, resp.Close)
					}
				}
				if _, err := io.WriteString(conn, tt.request); err != nil {
					t.Fatal(err)
				}
				if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
					t.Errorf("idle connection did not close: %v", err)
				}
			})
		}
	})

	t.Run("Methods", func(t *testing.T) {
		address := startTestServerCmd(t, "--path-prefix=/proxy")
		for _, tt := range []struct {
			name   string
			method string
			target string
		}{
			{"Healthz", http.MethodPost, "/proxy/healthz"},
			{"MissingPrefix", http.MethodPost, "/healthz"},
			{"Redirect", http.MethodPost, "/proxy/./healthz"},
			{"GeneralOptions", http.MethodOptions, "*"},
			{"Connect", http.MethodConnect, "example.com:443"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				conn, err := net.DialTimeout("tcp", address, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Fatal(err)
				}
				if _, err := fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", tt.method, tt.target); err != nil {
					t.Fatal(err)
				}
				resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: tt.method})
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if got, want := resp.StatusCode, http.StatusMethodNotAllowed; got != want {
					t.Errorf("got status %d, want %d", got, want)
				}
				if got, want := resp.Header.Get("Allow"), "GET, HEAD"; got != want {
					t.Errorf("got allow %q, want %q", got, want)
				}
				if got, want := resp.Header.Get("Cache-Control"), "no-store"; got != want {
					t.Errorf("got cache control %q, want %q", got, want)
				}
			})
		}
	})

	t.Run("CancellationDuringListenResolution", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		previousResolver := net.DefaultResolver
		net.DefaultResolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				once.Do(func() { close(started) })
				select {
				case <-release:
					return nil, errors.New("DNS lookup released")
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}
		defer func() { net.DefaultResolver = previousResolver }()
		defer close(release)
		cmd := newServerCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--address=server.invalid:8080", "--shutdown-timeout=20ms"})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- cmd.ExecuteContext(ctx) }()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not resolve its listen address")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("cancelled startup failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("server did not stop during address resolution")
		}
	})

	t.Run("TLS", func(t *testing.T) {
		certFile, keyFile, roots := testServerTLSFiles(t)
		for _, tt := range []struct {
			name    string
			http2   bool
			disable bool
		}{
			{"HTTP1", false, false},
			{"HTTP2", true, false},
			{"DisabledIdleTimeoutHTTP1", false, true},
			{"DisabledIdleTimeoutHTTP2", true, true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					time.Sleep(300 * time.Millisecond)
					io.WriteString(rw, "latest")
				}))
				defer upstream.Close()
				idleTimeout := "100ms"
				if tt.disable {
					idleTimeout = "0"
				}
				address := startTestServerCmd(t,
					"--tls-cert-file="+certFile, "--tls-key-file="+keyFile,
					"--idle-timeout="+idleTimeout, "--fetch-timeout=2s",
					"--proxied-sumdbs=sumdb.example.com "+upstream.URL,
				)
				transport := &http.Transport{
					TLSClientConfig:   &tls.Config{RootCAs: roots},
					ForceAttemptHTTP2: tt.http2,
				}
				defer transport.CloseIdleConnections()
				client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
				request := func(path string) bool {
					var reused bool
					ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
						GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused },
					})
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+address+path, nil)
					if err != nil {
						t.Fatal(err)
					}
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					protocol := 1
					if tt.http2 {
						protocol = 2
					}
					if resp.ProtoMajor != protocol {
						t.Errorf("got HTTP/%d, want HTTP/%d", resp.ProtoMajor, protocol)
					}
					content, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					if path == "/healthz" {
						if resp.StatusCode != http.StatusNoContent || len(content) != 0 {
							t.Fatalf("got health status %d and content %q", resp.StatusCode, content)
						}
					} else if resp.StatusCode != http.StatusOK || string(content) != "latest" {
						t.Fatalf("active request failed: status %d, content %q", resp.StatusCode, content)
					}
					return reused
				}
				request("/healthz")
				if !request("/healthz") {
					t.Fatal("connection was not reused before the idle timeout")
				}
				time.Sleep(200 * time.Millisecond)
				if reused := request("/healthz"); reused != tt.disable {
					t.Errorf("got connection reuse %t after idle wait, want %t", reused, tt.disable)
				}
				request("/sumdb/sumdb.example.com/latest")
			})
		}

		t.Run("JSONServerErrors", func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			previousStderr := os.Stderr
			os.Stderr = writer
			t.Cleanup(func() {
				os.Stderr = previousStderr
				writer.Close()
				reader.Close()
			})
			address := startTestServerCmd(t, "--tls-cert-file="+certFile, "--tls-key-file="+keyFile, "--log-format=json")
			conn, err := net.DialTimeout("tcp", address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := io.WriteString(conn, "GET /healthz HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			if err := reader.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var record struct {
				Level string
				Msg   string
			}
			if err := json.NewDecoder(reader).Decode(&record); err != nil {
				t.Fatalf("server error was not JSON: %v", err)
			}
			if record.Level != "ERROR" || !strings.Contains(record.Msg, "client sent an HTTP request to an HTTPS server") {
				t.Errorf("unexpected server log: %+v", record)
			}
		})
	})

	t.Run("TLSLoadFailure", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.Addr().String()
		listener.Close()
		cmd := newServerCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{
			"--address=" + address,
			"--tls-cert-file=" + filepath.Join(t.TempDir(), "missing.pem"),
			"--tls-key-file=" + filepath.Join(t.TempDir(), "missing.pem"),
		})
		if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("got TLS load error %v, want file not found", err)
		}
		listener, err = net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("listener was not released: %v", err)
		}
		listener.Close()
	})
}

func TestNewServerHandler(t *testing.T) {
	t.Run("RequestBodies", func(t *testing.T) {
		for _, tt := range []struct {
			name           string
			method         string
			prefix         string
			path           string
			headers        string
			wantStatusCode int
		}{
			{"HealthzGET", http.MethodGet, "", "/healthz", "Content-Length: 1\r\n", http.StatusBadRequest},
			{"HealthzHEAD", http.MethodHead, "", "/healthz", "Content-Length: 1\r\n", http.StatusBadRequest},
			{"Supported", http.MethodGet, "", "/sumdb/sum.golang.org/supported", "Content-Length: 1\r\n", http.StatusBadRequest},
			{"WithPrefix", http.MethodGet, "/proxy", "/proxy/example.com/@latest", "Content-Length: 1\r\n", http.StatusBadRequest},
			{"MissingPrefix", http.MethodGet, "/proxy", "/healthz", "Content-Length: 1\r\n", http.StatusBadRequest},
			{"Redirect", http.MethodGet, "", "/./healthz", "Content-Length: 1\r\n", http.StatusBadRequest},
			{"Chunked", http.MethodGet, "", "/healthz", "Transfer-Encoding: chunked\r\n", http.StatusBadRequest},
			{"Continue", http.MethodGet, "", "/healthz", "Content-Length: 1\r\nExpect: 100-continue\r\n", http.StatusBadRequest},
			{"POSTHealthz", http.MethodPost, "", "/healthz", "Content-Length: 1\r\n", http.StatusMethodNotAllowed},
			{"POSTSupported", http.MethodPost, "", "/sumdb/sum.golang.org/supported", "Content-Length: 1\r\n", http.StatusMethodNotAllowed},
			{"POSTMissingPrefix", http.MethodPost, "/proxy", "/healthz", "Content-Length: 1\r\n", http.StatusMethodNotAllowed},
			{"POSTRedirect", http.MethodPost, "", "/./healthz", "Content-Length: 1\r\n", http.StatusMethodNotAllowed},
		} {
			t.Run(tt.name, func(t *testing.T) {
				server := httptest.NewServer(newServerHandler(
					&serverCmdConfig{pathPrefix: tt.prefix, fetchTimeout: 20 * time.Millisecond},
					&goproxy.Goproxy{ProxiedSumDBs: []string{"sum.golang.org"}},
				))
				defer server.Close()
				conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Fatal(err)
				}
				if _, err := fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: localhost\r\n%s\r\n", tt.method, tt.path, tt.headers); err != nil {
					t.Fatal(err)
				}
				reader := bufio.NewReader(conn)
				resp, err := http.ReadResponse(reader, &http.Request{Method: tt.method})
				if err != nil {
					t.Fatalf("response waited for the request body: %v", err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != tt.wantStatusCode || !resp.Close {
					t.Errorf("got status %d, close %t, want status %d and closure", resp.StatusCode, resp.Close, tt.wantStatusCode)
				}
				if got, want := resp.Header.Get("Cache-Control"), "no-store"; got != want {
					t.Errorf("got cache control %q, want %q", got, want)
				}
				if tt.wantStatusCode == http.StatusMethodNotAllowed {
					if got, want := resp.Header.Get("Allow"), "GET, HEAD"; got != want {
						t.Errorf("got allow %q, want %q", got, want)
					}
				}
				content, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				if tt.wantStatusCode == http.StatusBadRequest {
					wantContent := "bad request: request bodies are not supported"
					if tt.method == http.MethodHead {
						wantContent = ""
					}
					if got, want := string(content), wantContent; got != want {
						t.Errorf("got content %q, want %q", got, want)
					}
				}
				if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
					t.Errorf("connection waited for the request body: %v", err)
				}
			})
		}

		t.Run("HTTP2", func(t *testing.T) {
			server := httptest.NewUnstartedServer(newServerHandler(
				&serverCmdConfig{pathPrefix: "/proxy"},
				&goproxy.Goproxy{ProxiedSumDBs: []string{"sum.golang.org"}},
			))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			client := server.Client()
			client.Timeout = 2 * time.Second
			for _, tt := range []struct {
				name          string
				method        string
				path          string
				contentLength int64
			}{
				{"GETLength", http.MethodGet, "/proxy/healthz", 1},
				{"HEADLength", http.MethodHead, "/proxy/healthz", 1},
				{"UnknownLength", http.MethodGet, "/healthz", -1},
				{"Redirect", http.MethodHead, "/proxy/./healthz", -1},
			} {
				t.Run(tt.name, func(t *testing.T) {
					bodyReader, bodyWriter := io.Pipe()
					defer bodyReader.Close()
					defer bodyWriter.Close()
					req, err := http.NewRequestWithContext(t.Context(), tt.method, server.URL+tt.path, bodyReader)
					if err != nil {
						t.Fatal(err)
					}
					req.ContentLength = tt.contentLength
					resp, err := client.Do(req)
					if err != nil {
						t.Fatalf("response waited for the request body: %v", err)
					}
					defer resp.Body.Close()
					bodyWriter.Close()
					if resp.ProtoMajor != 2 || resp.StatusCode != http.StatusBadRequest || resp.Close {
						t.Errorf("got protocol %d, status %d, close %t, want HTTP/2, status 400, no closure", resp.ProtoMajor, resp.StatusCode, resp.Close)
					}
					if got, want := resp.Header.Get("Cache-Control"), "no-store"; got != want {
						t.Errorf("got cache control %q, want %q", got, want)
					}
					content, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					wantContent := "bad request: request bodies are not supported"
					if tt.method == http.MethodHead {
						wantContent = ""
					}
					if got, want := string(content), wantContent; got != want {
						t.Errorf("got content %q, want %q", got, want)
					}
				})
			}
		})
	})

	for _, tt := range []struct {
		name              string
		cfg               serverCmdConfig
		base              http.Handler
		method            string
		path              string
		wantStatusCode    int
		wantCacheControl  string
		wantContentLength int64
		wantHandledPath   string
		wantRawPath       string
		wantRawQuery      string
	}{
		{
			name:             "HealthzGET",
			path:             "/healthz",
			wantStatusCode:   http.StatusNoContent,
			wantCacheControl: "no-store",
		},
		{
			name:             "HealthzHEAD",
			method:           http.MethodHead,
			path:             "/healthz",
			wantStatusCode:   http.StatusNoContent,
			wantCacheControl: "no-store",
		},
		{
			name:             "Passthrough",
			path:             "/anything",
			wantStatusCode:   http.StatusTeapot,
			wantCacheControl: "no-store",
			wantHandledPath:  "/anything",
		},
		{
			name: "PassthroughCacheControl",
			base: http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				rw.Header().Set("Cache-Control", "private, max-age=30")
				rw.WriteHeader(http.StatusOK)
			}),
			path:             "/anything",
			wantStatusCode:   http.StatusOK,
			wantCacheControl: "private, max-age=30",
			wantHandledPath:  "/anything",
		},
		{
			name:             "HealthzWithPrefix",
			cfg:              serverCmdConfig{pathPrefix: "/proxy"},
			path:             "/proxy/healthz",
			wantStatusCode:   http.StatusNoContent,
			wantCacheControl: "no-store",
		},
		{
			name:             "HealthzHEADWithPrefix",
			cfg:              serverCmdConfig{pathPrefix: "/proxy"},
			method:           http.MethodHead,
			path:             "/proxy/healthz",
			wantStatusCode:   http.StatusNoContent,
			wantCacheControl: "no-store",
		},
		{
			name:             "PassthroughWithPrefix",
			cfg:              serverCmdConfig{pathPrefix: "/proxy"},
			path:             "/proxy/anything",
			wantStatusCode:   http.StatusTeapot,
			wantCacheControl: "no-store",
			wantHandledPath:  "/anything",
		},
		{
			name:             "PassthroughEscaping",
			cfg:              serverCmdConfig{pathPrefix: "/proxy"},
			path:             "/proxy/a%2Fb?x=1%2F2",
			wantStatusCode:   http.StatusTeapot,
			wantCacheControl: "no-store",
			wantHandledPath:  "/a/b",
			wantRawPath:      "/a%2Fb",
			wantRawQuery:     "x=1%2F2",
		},
		{
			name: "FetchTimeout",
			cfg:  serverCmdConfig{fetchTimeout: 20 * time.Millisecond},
			base: http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				<-req.Context().Done()
				rw.WriteHeader(http.StatusGatewayTimeout)
			}),
			path:             "/slow",
			wantStatusCode:   http.StatusGatewayTimeout,
			wantCacheControl: "no-store",
			wantHandledPath:  "/slow",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var handledPath, rawPath, rawQuery string
			handler := newServerHandler(&tt.cfg, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				handledPath = req.URL.Path
				rawPath = req.URL.RawPath
				rawQuery = req.URL.RawQuery
				if tt.base != nil {
					tt.base.ServeHTTP(rw, req)
				} else {
					rw.WriteHeader(http.StatusTeapot)
				}
			}))

			req := httptest.NewRequest(tt.method, "https://example.com"+tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := int64(rec.Body.Len()), tt.wantContentLength; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := handledPath, tt.wantHandledPath; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if rawPath != tt.wantRawPath || rawQuery != tt.wantRawQuery {
				t.Errorf("got raw path %q and query %q, want %q and %q", rawPath, rawQuery, tt.wantRawPath, tt.wantRawQuery)
			}
		})
	}

	t.Run("RoutingCacheControl", func(t *testing.T) {
		for _, tt := range []struct {
			name             string
			prefix           string
			path             string
			wantStatusCode   int
			wantCacheControl string
			wantLocation     string
		}{
			{"RepeatedSlash", "", "/example.com//@latest", http.StatusTemporaryRedirect, "no-store", "/example.com/@latest"},
			{"DotSegment", "", "/./healthz", http.StatusTemporaryRedirect, "no-store", "/healthz"},
			{"ParentSegment", "", "/other/../healthz", http.StatusTemporaryRedirect, "no-store", "/healthz"},
			{"QueryString", "", "/other/../healthz?x=1", http.StatusTemporaryRedirect, "no-store", "/healthz?x=1"},
			{"RedirectWithPrefix", "/proxy", "/proxy/example.com//@latest", http.StatusTemporaryRedirect, "no-store", "/proxy/example.com/@latest"},
			{"PrefixQueryString", "/proxy", "/proxy/./healthz?x=1%2F2", http.StatusTemporaryRedirect, "no-store", "/proxy/healthz?x=1%2F2"},
			{"EmptyPathWithPrefix", "/proxy", "/proxy", http.StatusTemporaryRedirect, "no-store", "/proxy/"},
			{"PartialSegmentPrefix", "/proxy", "/proxyx/healthz", http.StatusNotFound, "no-store", ""},
			{"MissingPrefix", "/proxy", "/healthz", http.StatusNotFound, "no-store", ""},
			{"EscapedHealthPrefix", "/proxy", "/%70roxy/healthz", http.StatusNoContent, "no-store", ""},
			{"EscapedModulePrefix", "/proxy", "/%70roxy/sumdb/sumdb.example.com/supported", http.StatusNotFound, "no-store", ""},
			{"SupportedWithPrefix", "/proxy", "/proxy/sumdb/sumdb.example.com/supported", http.StatusOK, "public, max-age=86400", ""},
			{"UnknownDatabaseWithPrefix", "/proxy", "/proxy/sumdb/other.example.com/supported", http.StatusNotFound, "public, max-age=60", ""},
			{"InvalidResourceWithPrefix", "/proxy", "/proxy/sumdb/sumdb.example.com/unknown", http.StatusNotFound, "public, max-age=86400", ""},
			{"HealthzWithPrefix", "/proxy", "/proxy/healthz", http.StatusNoContent, "no-store", ""},
			{"RootPrefix", "/", "/healthz", http.StatusNoContent, "no-store", ""},
			{"TrailingSlashPrefix", "/proxy/", "/proxy/healthz", http.StatusNoContent, "no-store", ""},
			{"NestedPrefix", "/a/b/", "/a/b/healthz", http.StatusNoContent, "no-store", ""},
			{"LiteralWildcard", "/{proxy}", "/%7Bproxy%7D/healthz", http.StatusNoContent, "no-store", ""},
			{"LiteralWildcardModule", "/{proxy}", "/%7Bproxy%7D/sumdb/sumdb.example.com/supported", http.StatusOK, "public, max-age=86400", ""},
			{"WildcardDoesNotMatch", "/{proxy}", "/other/healthz", http.StatusNotFound, "no-store", ""},
		} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				t.Run(tt.name+"/"+method, func(t *testing.T) {
					server := httptest.NewServer(newServerHandler(
						&serverCmdConfig{pathPrefix: tt.prefix},
						&goproxy.Goproxy{ProxiedSumDBs: []string{"sumdb.example.com"}},
					))
					defer server.Close()
					client := server.Client()
					client.CheckRedirect = func(*http.Request, []*http.Request) error {
						return http.ErrUseLastResponse
					}
					req, err := http.NewRequest(method, server.URL+tt.path, nil)
					if err != nil {
						t.Fatal(err)
					}
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					if got, want := resp.StatusCode, tt.wantStatusCode; got != want {
						t.Errorf("got status %d, want %d", got, want)
					}
					if got, want := resp.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
						t.Errorf("got cache control %q, want %q", got, want)
					}
					if got, want := resp.Header.Get("Location"), tt.wantLocation; got != want {
						t.Errorf("got location %q, want %q", got, want)
					}
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					if method == http.MethodHead && len(body) != 0 {
						t.Errorf("got HEAD body %q, want empty", body)
					}
				})
			}
		}
	})
}

func startTestServerCmd(t *testing.T, args ...string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := newServerCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(append([]string{
		"--address=" + address, "--cacher-dir=" + t.TempDir(), "--temp-dir=" + t.TempDir(), "--shutdown-timeout=3s",
	}, args...))
	ctx, cancel := context.WithCancel(t.Context())
	var serverErr error
	done := make(chan struct{})
	go func() {
		serverErr = cmd.ExecuteContext(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
			if serverErr != nil {
				t.Errorf("server failed: %v", serverErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not shut down")
		}
	})
	dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
	dial := dialer.DialContext
	if slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "--tls-cert-file=") }) {
		dial = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{InsecureSkipVerify: true}}).DialContext
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := dial(ctx, "tcp", address)
		if err == nil {
			conn.Close()
			return address
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v", err)
		}
		select {
		case <-done:
			t.Fatalf("server stopped during startup: %v", serverErr)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func testServerTLSFiles(t *testing.T) (certFile, keyFile string, roots *x509.CertPool) {
	t.Helper()
	server := httptest.NewTLSServer(http.NotFoundHandler())
	server.Close()
	certificate := server.TLS.Certificates[0]
	privateKey, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(t.TempDir(), "cert.pem")
	keyFile = filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}), 0o600); err != nil {
		t.Fatal(err)
	}
	roots = x509.NewCertPool()
	roots.AddCert(server.Certificate())
	return certFile, keyFile, roots
}
