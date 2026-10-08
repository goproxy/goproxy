package internal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
	}{
		{"Default", nil, 10 * time.Second},
		{"ReadHeaderTimeout", []string{"--read-header-timeout=250ms"}, 250 * time.Millisecond},
		{"DisabledReadHeaderTimeout", []string{"--read-header-timeout=0"}, 0},
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
		})
	}
}

func TestRunServerCmd(t *testing.T) {
	t.Run("ReadHeaderTimeout", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.Addr().String()
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}

		cmd := newServerCmd()
		cmd.SetArgs([]string{
			"--address=" + address,
			"--cacher-dir=" + t.TempDir(),
			"--read-header-timeout=500ms",
			"--fetch-timeout=20ms",
			"--shutdown-timeout=1s",
		})
		ctx, cancel := context.WithCancel(t.Context())
		var serverErr error
		serverDone := make(chan struct{})
		go func() {
			serverErr = cmd.ExecuteContext(ctx)
			close(serverDone)
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-serverDone:
				if serverErr != nil {
					t.Errorf("server failed: %v", serverErr)
				}
			case <-time.After(5 * time.Second):
				t.Error("server did not shut down")
			}
		})

		deadline := time.Now().Add(5 * time.Second)
		for {
			conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("server did not start: %v", err)
			}
			select {
			case <-serverDone:
				t.Fatalf("server stopped during startup: %v", serverErr)
			case <-time.After(10 * time.Millisecond):
			}
		}

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
			var handledPath string
			handler := newServerHandler(&tt.cfg, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				handledPath = req.URL.Path
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
			{"RedirectWithPrefix", "/proxy", "/proxy/example.com//@latest", http.StatusTemporaryRedirect, "no-store", "/example.com/@latest"},
			{"EmptyPathWithPrefix", "/proxy", "/proxy", http.StatusTemporaryRedirect, "no-store", "/"},
			{"PartialSegmentPrefix", "/proxy", "/proxyx/healthz", http.StatusTemporaryRedirect, "no-store", "/x/healthz"},
			{"MissingPrefix", "/proxy", "/healthz", http.StatusNotFound, "no-store", ""},
			{"EscapedPrefix", "/proxy", "/%70roxy/healthz", http.StatusNotFound, "no-store", ""},
			{"SupportedWithPrefix", "/proxy", "/proxy/sumdb/sumdb.example.com/supported", http.StatusOK, "public, max-age=86400", ""},
			{"UnknownDatabaseWithPrefix", "/proxy", "/proxy/sumdb/other.example.com/supported", http.StatusNotFound, "public, max-age=60", ""},
			{"InvalidResourceWithPrefix", "/proxy", "/proxy/sumdb/sumdb.example.com/unknown", http.StatusNotFound, "public, max-age=86400", ""},
			{"HealthzWithPrefix", "/proxy", "/proxy/healthz", http.StatusNoContent, "no-store", ""},
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
