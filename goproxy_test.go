package goproxy

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"testing/iotest"
	"testing/synctest"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"
	modzip "golang.org/x/mod/zip"
)

var dialableTCPAddrs sync.Map

func TestMain(m *testing.M) {
	os.Exit(func() int {
		// Unset Go modules related environment variables.
		//
		// See https://go.dev/ref/mod#environment-variables.
		for _, key := range []string{
			"GO111MODULE",
			"GOMODCACHE",
			"GOINSECURE",
			"GONOPROXY",
			"GONOSUMDB",
			"GOPATH",
			"GOPRIVATE",
			"GOPROXY",
			"GOSUMDB",
			"GOVCS",
			"GOWORK",
		} {
			os.Unsetenv(key)
		}

		// Enable Go modules.
		os.Setenv("GO111MODULE", "on")

		// Set GOPATH to a temporary directory.
		envGOPATH, err := os.MkdirTemp(os.TempDir(), "goproxy-test-gopath")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create temporary directory for GOPATH: %v\n", err)
			os.Exit(1)
		}
		defer os.RemoveAll(envGOPATH)
		os.Setenv("GOPATH", envGOPATH)

		// Disable external network access.
		httpDefaultTransport := http.DefaultTransport.(*http.Transport)
		httpDefaultTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if _, ok := dialableTCPAddrs.Load(addr); !ok || network != "tcp" {
				return nil, fmt.Errorf("unexpected dial %q %q", network, addr)
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}

		return m.Run()
	}())
}

func TestGoproxyInit(t *testing.T) {
	g := &Goproxy{
		ProxiedSumDBs: []string{
			"sum.golang.google.cn",
			defaultEnvGOSUMDB + " https://sum.golang.google.cn",
			"",
			"example.com ://invalid",
		},
		TempDir: t.TempDir(),
	}
	g.initOnce.Do(g.init)
	if g.fetcher == nil {
		t.Error("unexpected nil")
	}
	if got, want := len(g.proxiedSumDBs), 2; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := g.proxiedSumDBs["sum.golang.google.cn"].String(), "https://sum.golang.google.cn"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := g.proxiedSumDBs[defaultEnvGOSUMDB].String(), "https://sum.golang.google.cn"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got := g.proxiedSumDBs["example.com"]; got != nil {
		t.Errorf("got %#v, want nil", got)
	}
	if g.httpClient == nil {
		t.Error("unexpected nil")
	} else if got := g.httpClient.Transport; got != nil {
		t.Errorf("got %#v, want nil", got)
	}

	t.Run("ProxiedSumDBs", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			entries []string
			want    map[string]string
		}{
			{"Empty", nil, nil},
			{
				"Invalid",
				[]string{
					"", " \t", "example.com/v2 ://invalid",
					"example.com /base", "example.com //upstream.example.com/base", "example.com upstream.example.com/base",
					"example.com http://", "example.com https://", "example.com https:///base", "example.com https:/base",
					"example.com https://:443/base", "example.com https:opaque",
					"example.com file:", "example.com file://", "example.com custom://", "example.com custom:opaque",
				},
				nil,
			},
			{
				"Valid",
				[]string{
					"example.com/v2",
					"http.example.com http://upstream.example.com:8080/base",
					"https.example.com HTTPS://upstream.example.com/base%20path?token=value#fragment",
					"auth.example.com https://user:pass@upstream.example.com/base",
					"file.example.com file:///base",
					"file-root.example.com file:///",
					"custom.example.com custom://upstream/base",
					"custom-path.example.com custom:/base",
					"custom-root.example.com custom:///",
				},
				map[string]string{
					"example.com/v2":          "https://example.com/v2",
					"http.example.com":        "http://upstream.example.com:8080/base",
					"https.example.com":       "https://upstream.example.com/base%20path?token=value#fragment",
					"auth.example.com":        "https://user:pass@upstream.example.com/base",
					"file.example.com":        "file:///base",
					"file-root.example.com":   "file:///",
					"custom.example.com":      "custom://upstream/base",
					"custom-path.example.com": "custom:/base",
					"custom-root.example.com": "custom:///",
				},
			},
			{
				"Duplicate",
				[]string{"example.com/v2 https://first.example.com", "example.com", "example.com/v2 https://last.example.com/base"},
				map[string]string{"example.com/v2": "https://last.example.com/base", "example.com": "https://example.com"},
			},
			{
				"InvalidDuplicate",
				[]string{"example.com/v2 https://first.example.com", "example.com/v2 ://invalid", "example.com/v2 https://", "example.com/v2 custom:opaque"},
				map[string]string{"example.com/v2": "https://first.example.com"},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				g := &Goproxy{ProxiedSumDBs: slices.Clone(tt.entries)}
				g.initOnce.Do(g.init)
				got := make(map[string]string, len(g.proxiedSumDBs))
				for name, u := range g.proxiedSumDBs {
					got[name] = u.String()
				}
				if !maps.Equal(got, tt.want) {
					t.Errorf("got %q, want %q", got, tt.want)
				}
				if !slices.Equal(g.ProxiedSumDBs, tt.entries) {
					t.Errorf("configuration changed: got %q, want %q", g.ProxiedSumDBs, tt.entries)
				}
			})
		}
	})
}

func TestGoproxyServeHTTP(t *testing.T) {
	t.Run("RequestBodies", func(t *testing.T) {
		t.Run("Validation", func(t *testing.T) {
			for _, tt := range []struct {
				name           string
				method         string
				path           string
				contentLength  int64
				protoMajor     int
				wantStatusCode int
			}{
				{"Latest", http.MethodGet, "/example.com/@latest", 1, 1, http.StatusBadRequest},
				{"List", http.MethodGet, "/example.com/@v/list", 1, 1, http.StatusBadRequest},
				{"Info", http.MethodGet, "/example.com/@v/v1.0.0.info", 1, 1, http.StatusBadRequest},
				{"Mod", http.MethodGet, "/example.com/@v/v1.0.0.mod", 1, 1, http.StatusBadRequest},
				{"Zip", http.MethodGet, "/example.com/@v/v1.0.0.zip", 1, 1, http.StatusBadRequest},
				{"SumDBLatest", http.MethodGet, "/sumdb/sum.golang.org/latest", 1, 1, http.StatusBadRequest},
				{"SumDBLookup", http.MethodGet, "/sumdb/sum.golang.org/lookup/example.com@v1.0.0", 1, 1, http.StatusBadRequest},
				{"SumDBTile", http.MethodGet, "/sumdb/sum.golang.org/tile/8/0/000.p/1", 1, 1, http.StatusBadRequest},
				{"InvalidPath", http.MethodGet, "/invalid/", 1, 1, http.StatusBadRequest},
				{"HEAD", http.MethodHead, "/example.com/@v/v1.0.0.info", 1, 1, http.StatusBadRequest},
				{"UnknownLength", http.MethodGet, "/sumdb/sum.golang.org/supported", -1, 1, http.StatusBadRequest},
				{"HTTP2", http.MethodGet, "/sumdb/sum.golang.org/supported", -1, 2, http.StatusBadRequest},
				{"UnsupportedMethod", http.MethodPost, "/sumdb/sum.golang.org/supported", 1, 1, http.StatusMethodNotAllowed},
				{"NoBody", http.MethodGet, "/sumdb/sum.golang.org/supported", 0, 1, http.StatusOK},
			} {
				t.Run(tt.name, func(t *testing.T) {
					g := &Goproxy{
						ProxiedSumDBs: []string{"sum.golang.org"},
						Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
							t.Error("unexpected upstream request")
							return nil, errors.New("unexpected upstream request")
						}),
						Cacher: &testCacher{
							get: func(context.Context, Cacher, string) (io.ReadCloser, error) {
								t.Error("unexpected cache read")
								return nil, fs.ErrNotExist
							},
							put: func(context.Context, Cacher, string, io.ReadSeeker) error {
								t.Error("unexpected cache write")
								return nil
							},
						},
						Logger: slog.New(slog.DiscardHandler),
					}
					req := httptest.NewRequest(tt.method, tt.path, nil)
					req.ProtoMajor = tt.protoMajor
					req.ContentLength = tt.contentLength
					if tt.contentLength != 0 {
						req.Body = io.NopCloser(&testReadSeeker{read: func(io.ReadSeeker, []byte) (int, error) {
							t.Error("unexpected request body read")
							return 0, io.EOF
						}})
					}
					rec := httptest.NewRecorder()
					g.ServeHTTP(rec, req)
					if got, want := rec.Code, tt.wantStatusCode; got != want {
						t.Errorf("got status %d, want %d", got, want)
					}
					wantContent := "bad request: request bodies are not supported"
					wantCacheControl, wantConnection, wantAllow := "no-store", "", ""
					wantContentType := "text/plain; charset=utf-8"
					if tt.contentLength != 0 && tt.protoMajor == 1 {
						wantConnection = "close"
					}
					if tt.wantStatusCode == http.StatusOK {
						wantContent, wantCacheControl = "", "public, max-age=86400"
						wantContentType = ""
					} else if tt.wantStatusCode == http.StatusMethodNotAllowed {
						wantContent, wantAllow = "method not allowed", "GET, HEAD"
					}
					if tt.method == http.MethodHead {
						wantContent = ""
					}
					if got, want := rec.Body.String(), wantContent; got != want {
						t.Errorf("got content %q, want %q", got, want)
					}
					for name, want := range map[string]string{
						"Cache-Control": wantCacheControl,
						"Content-Type":  wantContentType,
						"Connection":    wantConnection,
						"Allow":         wantAllow,
					} {
						if got := rec.Header().Get(name); got != want {
							t.Errorf("got %s %q, want %q", name, got, want)
						}
					}
				})
			}
		})

		t.Run("HTTP1", func(t *testing.T) {
			for _, wrapper := range []struct {
				name    string
				wrapped bool
			}{
				{"Unwrapped", false},
				{"Wrapped", true},
			} {
				for _, tt := range []struct {
					name           string
					method         string
					headers        string
					body           string
					wantStatusCode int
				}{
					{"NoBody", http.MethodGet, "", "", http.StatusOK},
					{"EmptyBody", http.MethodGet, "Content-Length: 0\r\n", "", http.StatusOK},
					{"GETLength", http.MethodGet, "Content-Length: 1\r\n", "", http.StatusBadRequest},
					{"HEADLength", http.MethodHead, "Content-Length: 1\r\n", "", http.StatusBadRequest},
					{"GETFullBody", http.MethodGet, "Content-Length: 1\r\n", "x", http.StatusBadRequest},
					{"GETPartialBody", http.MethodGet, "Content-Length: 2\r\n", "x", http.StatusBadRequest},
					{"GETChunked", http.MethodGet, "Transfer-Encoding: chunked\r\n", "", http.StatusBadRequest},
					{"HEADChunked", http.MethodHead, "Transfer-Encoding: chunked\r\n", "", http.StatusBadRequest},
					{"GETChunkedBody", http.MethodGet, "Transfer-Encoding: chunked\r\n", "1\r\nx\r\n0\r\n\r\n", http.StatusBadRequest},
					{"GETContinue", http.MethodGet, "Content-Length: 1\r\nExpect: 100-continue\r\n", "", http.StatusBadRequest},
					{"POSTLength", http.MethodPost, "Content-Length: 1\r\n", "", http.StatusMethodNotAllowed},
					{"POSTChunked", http.MethodPost, "Transfer-Encoding: chunked\r\n", "", http.StatusMethodNotAllowed},
				} {
					t.Run(wrapper.name+"/"+tt.name, func(t *testing.T) {
						g := &Goproxy{ProxiedSumDBs: []string{"sum.golang.org"}}
						server := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
							if wrapper.wrapped {
								rw = testUnwrapResponseWriter{rw}
							}
							g.ServeHTTP(rw, req)
						}))
						conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
						if err != nil {
							t.Fatal(err)
						}
						defer conn.Close()
						if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
							t.Fatal(err)
						}
						if _, err := fmt.Fprintf(conn, "%s /sumdb/sum.golang.org/supported HTTP/1.1\r\nHost: localhost\r\n%s\r\n%s", tt.method, tt.headers, tt.body); err != nil {
							t.Fatal(err)
						}
						reader := bufio.NewReader(conn)
						resp, err := http.ReadResponse(reader, &http.Request{Method: tt.method})
						if err != nil {
							t.Fatalf("response waited for the request body: %v", err)
						}
						defer resp.Body.Close()
						if got, want := resp.StatusCode, tt.wantStatusCode; got != want {
							t.Errorf("got status %d, want %d", got, want)
						}
						wantCacheControl := "no-store"
						wantClose := tt.wantStatusCode != http.StatusOK
						if !wantClose {
							wantCacheControl = "public, max-age=86400"
						}
						if got, want := resp.Header.Get("Cache-Control"), wantCacheControl; got != want {
							t.Errorf("got cache control %q, want %q", got, want)
						}
						if got, want := resp.Close, wantClose; got != want {
							t.Errorf("got connection closure %t, want %t", got, want)
						}
						wantContent := "bad request: request bodies are not supported"
						if tt.wantStatusCode == http.StatusOK {
							wantContent = ""
						} else if tt.wantStatusCode == http.StatusMethodNotAllowed {
							wantContent = "method not allowed"
							if got, want := resp.Header.Get("Allow"), "GET, HEAD"; got != want {
								t.Errorf("got allow %q, want %q", got, want)
							}
						}
						if tt.method == http.MethodHead {
							wantContent = ""
						}
						content, err := io.ReadAll(resp.Body)
						if err != nil {
							t.Fatal(err)
						}
						if got, want := string(content), wantContent; got != want {
							t.Errorf("got content %q, want %q", got, want)
						}
						if wantClose {
							if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
								t.Errorf("connection waited for the request body: %v", err)
							}
						}
					})
				}
			}
		})

		t.Run("HTTP2", func(t *testing.T) {
			server := httptest.NewUnstartedServer(&Goproxy{ProxiedSumDBs: []string{"sum.golang.org"}})
			server.EnableHTTP2 = true
			server.StartTLS()
			t.Cleanup(server.Close)
			client := server.Client()
			client.Timeout = 2 * time.Second
			for _, tt := range []struct {
				name          string
				contentLength int64
			}{
				{"KnownLength", 1},
				{"UnknownLength", -1},
			} {
				for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
					t.Run(tt.name+"/"+method, func(t *testing.T) {
						bodyReader, bodyWriter := io.Pipe()
						defer bodyReader.Close()
						defer bodyWriter.Close()
						req, err := http.NewRequestWithContext(t.Context(), method, server.URL+"/sumdb/sum.golang.org/supported", bodyReader)
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
						wantStatusCode := http.StatusBadRequest
						wantContent := "bad request: request bodies are not supported"
						if method == http.MethodPost {
							wantStatusCode, wantContent = http.StatusMethodNotAllowed, "method not allowed"
						}
						if resp.ProtoMajor != 2 || resp.StatusCode != wantStatusCode || resp.Close {
							t.Errorf("got protocol %d, status %d, close %t, want HTTP/2, status %d, no closure", resp.ProtoMajor, resp.StatusCode, resp.Close, wantStatusCode)
						}
						if got, want := resp.Header.Get("Cache-Control"), "no-store"; got != want {
							t.Errorf("got cache control %q, want %q", got, want)
						}
						content, err := io.ReadAll(resp.Body)
						if err != nil {
							t.Fatal(err)
						}
						if method == http.MethodHead {
							wantContent = ""
						}
						if got, want := string(content), wantContent; got != want {
							t.Errorf("got content %q, want %q", got, want)
						}

						var reused bool
						ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
							reused = info.Reused
						}})
						req, err = http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/sumdb/sum.golang.org/supported", nil)
						if err != nil {
							t.Fatal(err)
						}
						resp, err = client.Do(req)
						if err != nil {
							t.Fatal(err)
						}
						defer resp.Body.Close()
						if !reused || resp.ProtoMajor != 2 || resp.StatusCode != http.StatusOK {
							t.Errorf("got reused %t, protocol %d, status %d, want a reused HTTP/2 connection and 200", reused, resp.ProtoMajor, resp.StatusCode)
						}
					})
				}
			}
		})
	})

	t.Run("GOPROXYOff", func(t *testing.T) {
		info := marshalInfo("v1.0.1", time.Time{})
		cacher := DirCacher(t.TempDir())
		if err := cacher.Put(t.Context(), "example.com/@v/v1.0.1.info", strings.NewReader(info)); err != nil {
			t.Fatal(err)
		}
		g := &Goproxy{
			Fetcher: &GoFetcher{Env: []string{"GOPROXY=off", "GOSUMDB=off"}},
			Cacher:  cacher,
			Logger:  slog.New(slog.DiscardHandler),
		}
		for _, tt := range []struct {
			name             string
			path             string
			wantStatusCode   int
			wantCacheControl string
			wantContent      string
		}{
			{"Latest", "/example.com/@latest", http.StatusNotFound, "no-store", "not found: module lookup disabled by GOPROXY=off"},
			{"List", "/example.com/@v/list", http.StatusNotFound, "no-store", "not found: module lookup disabled by GOPROXY=off"},
			{"Revision", "/example.com/@v/main.info", http.StatusNotFound, "no-store", "not found: module lookup disabled by GOPROXY=off"},
			{"Info", "/example.com/@v/v1.0.0.info", http.StatusNotFound, "no-store", "not found: module lookup disabled by GOPROXY=off"},
			{"Mod", "/example.com/@v/v1.0.0.mod", http.StatusNotFound, "no-store", "not found: module lookup disabled by GOPROXY=off"},
			{"Zip", "/example.com/@v/v1.0.0.zip", http.StatusNotFound, "no-store", "not found: module lookup disabled by GOPROXY=off"},
			{"CachedInfo", "/example.com/@v/v1.0.1.info", http.StatusOK, "public, max-age=604800", info},
		} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				t.Run(tt.name+"/"+method, func(t *testing.T) {
					rec := httptest.NewRecorder()
					g.ServeHTTP(rec, httptest.NewRequest(method, tt.path, nil))
					if got, want := rec.Code, tt.wantStatusCode; got != want {
						t.Errorf("got %d, want %d", got, want)
					}
					if got, want := rec.Header().Get("Cache-Control"), tt.wantCacheControl; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					if got, want := rec.Header().Get("Vary"), "Disable-Module-Fetch"; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					wantContent := tt.wantContent
					if method == http.MethodHead {
						wantContent = ""
					}
					if got, want := rec.Body.String(), wantContent; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
				})
			}
		}
	})

	t.Run("DynamicQueryCaching", func(t *testing.T) {
		info := marshalInfo("v1.0.0", time.Time{})
		for _, resource := range []struct {
			name    string
			path    string
			content string
		}{
			{"Latest", "/example.com/@latest", info},
			{"List", "/example.com/@v/list", "v1.0.0"},
			{"EmptyList", "/example.com/@v/list", ""},
			{"Branch", "/example.com/@v/main.info", info},
			{"AbbreviatedVersion", "/example.com/@v/v1.info", info},
			{"Revision", "/example.com/@v/deadbeef.info", info},
		} {
			for _, tt := range []struct {
				name             string
				statusCode       int
				err              error
				restricted       bool
				noFetch          bool
				wantStatusCode   int
				wantCacheControl string
				wantContent      string
			}{
				{"Success", http.StatusOK, nil, false, false, http.StatusOK, "public, max-age=60", ""},
				{"NotFound", http.StatusNotFound, nil, false, false, http.StatusNotFound, "public, max-age=60", "not found: missing"},
				{"RestrictedNotFound", http.StatusNotFound, nil, true, false, http.StatusNotFound, "no-store", "not found: missing"},
				{"BadUpstream", http.StatusBadGateway, nil, false, false, http.StatusNotFound, "no-store", "not found: bad upstream"},
				{"Timeout", 0, context.DeadlineExceeded, false, false, http.StatusNotFound, "no-store", "not found: fetch timed out"},
				{"ReadError", http.StatusOK, errors.New("cannot read"), false, false, http.StatusInternalServerError, "no-store", "internal server error"},
				{"NoFetch", http.StatusOK, nil, false, true, http.StatusNotFound, "no-store", "not found: temporarily unavailable"},
			} {
				for _, condition := range []struct {
					name   string
					header http.Header
				}{
					{"Unconditional", nil},
					{"IfNoneMatch", http.Header{"If-None-Match": {`"old"`}}},
					{"IfModifiedSince", http.Header{"If-Modified-Since": {"Sat, 01 Jan 2000 00:00:00 GMT"}}},
				} {
					for _, method := range []string{http.MethodGet, http.MethodHead} {
						t.Run(resource.name+"/"+tt.name+"/"+condition.name+"/"+method, func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								content := resource.content
								contentType := "application/json; charset=utf-8"
								if strings.HasSuffix(resource.path, "/@v/list") {
									contentType = "text/plain; charset=utf-8"
								}
								upstreamCalls := 0
								g := &Goproxy{
									Fetcher: &GoFetcher{
										Env: []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=off"},
										Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
											upstreamCalls++
											if got, want := req.URL.Path, resource.path; got != want {
												t.Errorf("got upstream path %q, want %q", got, want)
											}
											if tt.statusCode == 0 {
												return nil, tt.err
											}
											var body io.Reader = strings.NewReader(content)
											if tt.statusCode != http.StatusOK {
												body = strings.NewReader("missing")
											} else if tt.err != nil {
												body = iotest.ErrReader(tt.err)
											}
											header := make(http.Header)
											if tt.restricted {
												header.Set("Cache-Control", "no-store")
											}
											return &http.Response{StatusCode: tt.statusCode, Header: header, Body: io.NopCloser(body), Request: req}, nil
										}),
									},
									Cacher: &testCacher{
										get: func(context.Context, Cacher, string) (io.ReadCloser, error) {
											t.Error("unexpected cache read")
											return struct {
												io.ReadSeeker
												io.Closer
												successResponseBody_ModTime
												successResponseBody_ETag
											}{
												strings.NewReader("old result"),
												closerFunc(func() error { return nil }),
												successResponseBody_ModTime{modTime: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
												successResponseBody_ETag{etag: `"old"`},
											}, nil
										},
										put: func(context.Context, Cacher, string, io.ReadSeeker) error {
											t.Error("unexpected cache write")
											return errors.New("cannot put")
										},
									},
									Logger: slog.New(slog.DiscardHandler),
								}
								req := httptest.NewRequest(method, resource.path, nil)
								maps.Copy(req.Header, condition.header)
								if tt.noFetch {
									req.Header.Set("Disable-Module-Fetch", "true")
								}
								rec := httptest.NewRecorder()
								g.ServeHTTP(rec, req)
								if got, want := rec.Code, tt.wantStatusCode; got != want {
									t.Errorf("got status %d, want %d", got, want)
								}
								if got, want := rec.Header().Get("Cache-Control"), tt.wantCacheControl; got != want {
									t.Errorf("got cache control %q, want %q", got, want)
								}
								if got, want := rec.Header().Get("Vary"), "Disable-Module-Fetch"; got != want {
									t.Errorf("got vary %q, want %q", got, want)
								}
								if tt.wantStatusCode != http.StatusOK {
									contentType = "text/plain; charset=utf-8"
								}
								if got, want := rec.Header().Get("Content-Type"), contentType; got != want {
									t.Errorf("got content type %q, want %q", got, want)
								}
								wantContent := tt.wantContent
								if tt.wantStatusCode == http.StatusOK {
									wantContent = content
								}
								if method == http.MethodHead {
									wantContent = ""
								}
								if got, want := rec.Body.String(), wantContent; got != want {
									t.Errorf("got content %q, want %q", got, want)
								}
								if got, want := upstreamCalls > 0, !tt.noFetch; got != want {
									t.Errorf("got upstream request %t, want %t", got, want)
								}
								for _, name := range []string{"ETag", "Last-Modified"} {
									if got := rec.Header().Get(name); got != "" {
										t.Errorf("unexpected %s header %q", name, got)
									}
								}
							})
						})
					}
				}
			}
		}
	})

	t.Run("LocalFileErrors", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			proxy string
			goBin bool
		}{
			{"ProxyTempDir", "https://proxy.example.com", false},
			{"DirectTempDir", "direct", false},
			{"GoBinary", "direct", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, resource := range []struct {
					name     string
					path     string
					download bool
				}{
					{"Latest", "/example.com/@latest", false},
					{"Query", "/example.com/@v/main.info", false},
					{"List", "/example.com/@v/list", false},
					{"Info", "/example.com/@v/v1.0.0.info", true},
					{"Mod", "/example.com/@v/v1.0.0.mod", true},
					{"Zip", "/example.com/@v/v1.0.0.zip", true},
				} {
					if tt.proxy != "direct" && !resource.download {
						continue
					}
					t.Run(resource.name, func(t *testing.T) {
						for _, method := range []string{http.MethodGet, http.MethodHead} {
							t.Run(method, func(t *testing.T) {
								tempDir := t.TempDir()
								missing := filepath.Join(tempDir, "missing")
								gf := &GoFetcher{
									Env:     []string{"GOPROXY=" + tt.proxy, "GOSUMDB=off"},
									TempDir: missing,
									Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
										t.Errorf("unexpected request %s", req.URL)
										return nil, errors.New("unexpected request")
									}),
								}
								if tt.goBin {
									gf.GoBin = missing
									gf.TempDir = tempDir
								}
								var log bytes.Buffer
								g := &Goproxy{
									Fetcher: gf,
									Cacher: &testCacher{
										get: func(context.Context, Cacher, string) (io.ReadCloser, error) {
											if !resource.download {
												t.Error("unexpected cache read")
											}
											return nil, fs.ErrNotExist
										},
										put: func(_ context.Context, _ Cacher, name string, _ io.ReadSeeker) error {
											t.Errorf("unexpected cache write %q", name)
											return nil
										},
									},
									Logger: slog.New(slog.NewJSONHandler(&log, nil)),
								}
								rec := httptest.NewRecorder()
								g.ServeHTTP(rec, httptest.NewRequest(method, resource.path, nil))
								if got, want := rec.Code, http.StatusInternalServerError; got != want {
									t.Errorf("got %d, want %d", got, want)
								}
								if got, want := rec.Header().Get("Cache-Control"), "no-store"; got != want {
									t.Errorf("got %q, want %q", got, want)
								}
								wantContent := "internal server error"
								if method == http.MethodHead {
									wantContent = ""
								}
								if got, want := rec.Body.String(), wantContent; got != want {
									t.Errorf("got %q, want %q", got, want)
								}
								var record struct{ Error string }
								if err := json.Unmarshal(log.Bytes(), &record); err != nil {
									t.Errorf("unexpected error %v", err)
								} else if !strings.Contains(record.Error, missing) {
									t.Errorf("got logged error %q, want path %q", record.Error, missing)
								}
								if entries, err := os.ReadDir(tempDir); err != nil {
									t.Errorf("unexpected error %v", err)
								} else if len(entries) != 0 {
									t.Errorf("unexpected temporary files %v", entries)
								}
							})
						}
					})
				}
			})
		}
	})

	t.Run("ModuleResponseValidation", func(t *testing.T) {
		info := marshalInfo("v1.0.0", time.Time{})
		mod := "module example.com"
		var corruptZip bytes.Buffer
		zw := zip.NewWriter(&corruptZip)
		w, err := zw.CreateRaw(&zip.FileHeader{
			Name:               "example.com@v1.0.0/go.mod",
			Method:             zip.Store,
			CRC32:              crc32.ChecksumIEEE([]byte(mod)) ^ 1,
			CompressedSize64:   uint64(len(mod)),
			UncompressedSize64: uint64(len(mod)),
		})
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		if _, err := io.WriteString(w, mod); err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)})
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		wrongZip, err := makeZip(map[string][]byte{"example.com@v1.1.0/go.mod": []byte(mod)})
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		changedZip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod + "\n")})
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		zipFile, err := makeTempFile(t, zip)
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		zipHash, err := dirhash.HashZip(zipFile, dirhash.DefaultHash)
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(mod)), nil
		})
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		skey, vkey, err := note.GenerateKey(nil, "sumdb.example.com")
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		sumdbServer := newHTTPTestServer(t, sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
			return fmt.Appendf(nil, "%s %s %s\n%s %s/go.mod %s\n", modulePath, moduleVersion, zipHash, modulePath, moduleVersion, modHash), nil
		})))

		for _, tt := range []struct {
			name    string
			ext     string
			content string
			query   bool
		}{
			{"EmptyInfo", ".info", "", true},
			{"MalformedInfo", ".info", "{", true},
			{"OversizedInfo", ".info", info + strings.Repeat(" ", maxInfoSize+1-len(info)), true},
			{"MissingVersion", ".info", "{}", true},
			{"InvalidVersion", ".info", marshalInfo("main", time.Time{}), true},
			{"NoncanonicalVersion", ".info", marshalInfo("v1", time.Time{}), true},
			{"WrongMajor", ".info", marshalInfo("v2.0.0", time.Time{}), true},
			{"WrongVersion", ".info", marshalInfo("v1.1.0", time.Time{}), false},
			{"MissingModuleDirective", ".mod", "", false},
			{"InvalidZip", ".zip", "invalid zip", false},
			{"WrongZipPrefix", ".zip", string(wrongZip), false},
			{"CorruptZipPayload", ".zip", corruptZip.String(), false},
			{"ModChecksumMismatch", ".mod", mod + "\n", false},
			{"ZipChecksumMismatch", ".zip", string(changedZip), false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				upstream := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					ext := path.Ext(req.URL.Path)
					if strings.HasSuffix(req.URL.Path, "/@latest") {
						ext = ".info"
					}
					content := map[string]string{".info": info, ".mod": mod, ".zip": string(zip)}[ext]
					if ext == tt.ext {
						content = tt.content
					}
					rw.Header().Set("Cache-Control", "public, max-age=86400")
					fmt.Fprint(rw, content)
				}))
				for _, resource := range []struct {
					name  string
					path  string
					query bool
				}{
					{"Latest", "/example.com/@latest", true},
					{"Query", "/example.com/@v/main.info", true},
					{"Info", "/example.com/@v/v1.0.0.info", false},
					{"Mod", "/example.com/@v/v1.0.0.mod", false},
					{"Zip", "/example.com/@v/v1.0.0.zip", false},
				} {
					if resource.query && !tt.query {
						continue
					}
					t.Run(resource.name, func(t *testing.T) {
						gf := &GoFetcher{
							Env:     []string{"GOPROXY=" + upstream.URL, "GOSUMDB=" + vkey + " " + sumdbServer.URL},
							TempDir: t.TempDir(),
						}
						g := &Goproxy{
							Fetcher: gf,
							Cacher: &testCacher{
								get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
									if resource.query {
										t.Error("unexpected cache read")
									}
									if got, want := name, strings.TrimPrefix(resource.path, "/"); got != want {
										t.Errorf("got cache key %q, want %q", got, want)
									}
									return nil, fs.ErrNotExist
								},
								put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
									t.Errorf("unexpected cache write %q", name)
									return nil
								},
							},
							Logger: slog.New(slog.DiscardHandler),
						}
						for _, method := range []string{http.MethodGet, http.MethodHead} {
							t.Run(method, func(t *testing.T) {
								rec := httptest.NewRecorder()
								g.ServeHTTP(rec, httptest.NewRequest(method, resource.path, nil))
								wantContent := "not found: bad upstream"
								if got, want := rec.Code, http.StatusNotFound; got != want {
									t.Errorf("got %d, want %d", got, want)
								}
								if got, want := rec.Header().Get("Cache-Control"), "no-store"; got != want {
									t.Errorf("got %q, want %q", got, want)
								}
								if method == http.MethodHead {
									wantContent = ""
								}
								if got, want := rec.Body.String(), wantContent; got != want {
									t.Errorf("got %q, want %q", got, want)
								}
								if entries, err := os.ReadDir(gf.TempDir); err != nil {
									t.Errorf("unexpected error %v", err)
								} else if len(entries) != 0 {
									t.Errorf("unexpected temporary files %v", entries)
								}
							})
						}
					})
				}
			})
		}
	})

	t.Run("ModFileModuleDirective", func(t *testing.T) {
		const mod = "module example.com\n//"
		zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)})
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		for _, tt := range []struct {
			name    string
			mod     string
			wantErr bool
		}{
			{"LongComment", "//" + strings.Repeat("x", bufio.MaxScanTokenSize) + "\nmodule example.com\n", false},
			{"LongModuleLine", "module " + strings.Repeat(" ", bufio.MaxScanTokenSize) + "example.com\n", false},
			{"QuotedPath", "module \"example.com\"\n", false},
			{"DirectivePrefix", "modulexxx example.com", true},
			{"MissingPath", "module", true},
			{"EmptyQuotedPath", `module ""`, true},
			{"MaxSize", mod + strings.Repeat("x", modzip.MaxGoMod-len(mod)), false},
			{"TooLarge", mod + strings.Repeat("x", modzip.MaxGoMod+1-len(mod)), true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"example.com/@v/v1.0.0.info": []byte(marshalInfo("v1.0.0", time.Time{})),
					"example.com/@v/v1.0.0.mod":  []byte(tt.mod),
					"example.com/@v/v1.0.0.zip":  zip,
				}
				upstream := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					rw.Write(files[strings.TrimPrefix(req.URL.Path, "/")])
				}))
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					t.Run(method, func(t *testing.T) {
						gf := &GoFetcher{
							Env:     []string{"GOPROXY=" + upstream.URL, "GOSUMDB=off"},
							TempDir: t.TempDir(),
						}
						g := &Goproxy{
							Fetcher: gf,
							Cacher:  DirCacher(t.TempDir()),
							Logger:  slog.New(slog.DiscardHandler),
						}
						rec := httptest.NewRecorder()
						g.ServeHTTP(rec, httptest.NewRequest(method, "/example.com/@v/v1.0.0.mod", nil))
						wantStatusCode := http.StatusOK
						wantCacheControl := "public, max-age=604800"
						wantContent := tt.mod
						if tt.wantErr {
							wantStatusCode = http.StatusNotFound
							wantCacheControl = "no-store"
							wantContent = "not found: bad upstream"
						}
						if got, want := rec.Code, wantStatusCode; got != want {
							t.Errorf("got %d, want %d", got, want)
						}
						if got, want := rec.Header().Get("Cache-Control"), wantCacheControl; got != want {
							t.Errorf("got %q, want %q", got, want)
						}
						if method == http.MethodHead {
							wantContent = ""
						}
						if rec.Body.String() != wantContent {
							t.Error("unexpected response body")
						}
						for name, want := range files {
							content, err := g.Cacher.Get(t.Context(), name)
							if err != nil {
								if !tt.wantErr || !errors.Is(err, fs.ErrNotExist) {
									t.Errorf("unexpected cache error for %q: %v", name, err)
								}
								continue
							}
							got, err := io.ReadAll(content)
							content.Close()
							if tt.wantErr {
								t.Errorf("unexpected cached file %q", name)
							} else if err != nil {
								t.Errorf("unexpected error %v", err)
							} else if !bytes.Equal(got, want) {
								t.Errorf("unexpected cached content for %q", name)
							}
						}
						if entries, err := os.ReadDir(gf.TempDir); err != nil {
							t.Errorf("unexpected error %v", err)
						} else if len(entries) != 0 {
							t.Errorf("unexpected temporary files %v", entries)
						}
					})
				}
			})
		}
	})

	t.Run("UpstreamCacheRestrictions", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			statusCode   int
			cacheControl string
			restricted   bool
		}{
			{"Ordinary", http.StatusNotFound, "", false},
			{"Restricted", http.StatusNotFound, "no-store", true},
			{"Gone", http.StatusGone, "", false},
			{"RestrictedGone", http.StatusGone, "no-store", true},
			{"BadRequest", http.StatusBadRequest, "", false},
			{"RestrictedBadRequest", http.StatusBadRequest, "no-store", true},
			{"MustRevalidate", http.StatusNotFound, "must-revalidate", true},
			{"ProxyRevalidate", http.StatusNotFound, "max-age=60, proxy-revalidate", true},
			{"ZeroMaxAge", http.StatusNotFound, "max-age=0", true},
			{"ZeroMaxAgeGone", http.StatusGone, "max-age=0", true},
			{"ZeroSharedMaxAge", http.StatusNotFound, "s-maxage=0", true},
			{"PositiveSharedMaxAge", http.StatusNotFound, "s-maxage=60", true},
			{"PositiveSharedMaxAgeGone", http.StatusGone, "s-maxage=60", true},
			{"PositiveMaxAge", http.StatusNotFound, "max-age=60", false},
			{"PositiveMaxAgeGone", http.StatusGone, "max-age=60", false},
			{"InvalidMaxAge", http.StatusNotFound, "max-age=invalid", true},
			{"DuplicateMaxAge", http.StatusNotFound, "max-age=60, max-age=120", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				upstream := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					if tt.cacheControl != "" {
						rw.Header().Set("Cache-Control", tt.cacheControl)
					}
					rw.WriteHeader(tt.statusCode)
					fmt.Fprint(rw, "module unavailable")
				}))
				for _, resource := range []struct {
					name string
					path string
				}{
					{"Latest", "/example.com/@latest"},
					{"Query", "/example.com/@v/master.info"},
					{"List", "/example.com/@v/list"},
					{"Info", "/example.com/@v/v1.0.0.info"},
					{"Mod", "/example.com/@v/v1.0.0.mod"},
					{"Zip", "/example.com/@v/v1.0.0.zip"},
					{"SumDB", "/sumdb/sumdb.example.com/latest"},
				} {
					t.Run(resource.name, func(t *testing.T) {
						g := &Goproxy{
							Fetcher: &GoFetcher{
								Env:     []string{"GOPROXY=" + upstream.URL, "GOSUMDB=off"},
								TempDir: t.TempDir(),
							},
							ProxiedSumDBs: []string{"sumdb.example.com " + upstream.URL},
							TempDir:       t.TempDir(),
							Logger:        slog.New(slog.DiscardHandler),
						}
						for _, method := range []string{http.MethodGet, http.MethodHead} {
							t.Run(method, func(t *testing.T) {
								rec := httptest.NewRecorder()
								g.ServeHTTP(rec, httptest.NewRequest(method, resource.path, nil))
								wantStatusCode := http.StatusNotFound
								wantCacheControl := "public, max-age=60"
								wantContent := "not found: module unavailable"
								if tt.restricted {
									wantCacheControl = "no-store"
								}
								if tt.statusCode == http.StatusBadRequest {
									wantStatusCode = http.StatusInternalServerError
									wantCacheControl = "no-store"
									wantContent = "internal server error"
									if resource.name == "SumDB" {
										wantStatusCode = http.StatusBadGateway
										wantContent = "bad gateway"
									}
								}
								if got, want := rec.Code, wantStatusCode; got != want {
									t.Errorf("got %d, want %d", got, want)
								}
								if got, want := rec.Header().Get("Cache-Control"), wantCacheControl; got != want {
									t.Errorf("got %q, want %q", got, want)
								}
								if method == http.MethodHead {
									wantContent = ""
								}
								if got, want := rec.Body.String(), wantContent; got != want {
									t.Errorf("got %q, want %q", got, want)
								}
							})
						}
					})
				}
			})
		}
	})

	t.Run("MethodNotAllowed", func(t *testing.T) {
		g := &Goproxy{ProxiedSumDBs: []string{"sumdb.example.com"}}
		for _, tt := range []struct {
			name string
			path string
		}{
			{"Module", "/example.com/@latest"},
			{"SumDB", "/sumdb/sumdb.example.com/supported"},
		} {
			for _, method := range []string{
				http.MethodPost,
				http.MethodPut,
				http.MethodPatch,
				http.MethodDelete,
				http.MethodConnect,
				http.MethodOptions,
				http.MethodTrace,
			} {
				t.Run(tt.name+"/"+method, func(t *testing.T) {
					rec := httptest.NewRecorder()
					g.ServeHTTP(rec, httptest.NewRequest(method, tt.path, nil))
					recr := rec.Result()
					if got, want := recr.StatusCode, http.StatusMethodNotAllowed; got != want {
						t.Errorf("got %d, want %d", got, want)
					}
					if got, want := recr.Header.Get("Allow"), "GET, HEAD"; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					if got, want := recr.Header.Get("Cache-Control"), "no-store"; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					if b, err := io.ReadAll(recr.Body); err != nil {
						t.Errorf("unexpected error %v", err)
					} else if got, want := string(b), "method not allowed"; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
				})
			}
		}
	})

	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
	}))
	for _, tt := range []struct {
		n                int
		method           string
		path             string
		wantStatusCode   int
		wantAllow        string
		wantContentType  string
		wantCacheControl string
		wantVary         string
		wantContent      string
	}{
		{
			n:                1,
			path:             "/example.com/@latest",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantVary:         "Disable-Module-Fetch",
			wantContent:      info,
		},
		{
			n:                2,
			method:           http.MethodHead,
			path:             "/example.com/@latest",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantVary:         "Disable-Module-Fetch",
		},
		{
			n:                3,
			method:           http.MethodPost,
			path:             "/example.com/@latest",
			wantStatusCode:   http.StatusMethodNotAllowed,
			wantAllow:        "GET, HEAD",
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "no-store",
			wantContent:      "method not allowed",
		},
		{
			n:                4,
			path:             "/",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                5,
			path:             "/.",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                6,
			path:             "/../example.com/@latest",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                7,
			path:             "/example.com/@latest/",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                8,
			path:             "/sumdb/sumdb.example.com/supported",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			g := &Goproxy{
				Fetcher: &GoFetcher{
					Env:     []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
					TempDir: t.TempDir(),
				},
				Cacher:  DirCacher(t.TempDir()),
				TempDir: t.TempDir(),
				Logger:  slog.New(slog.DiscardHandler),
			}

			rec := httptest.NewRecorder()
			g.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Allow"), tt.wantAllow; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Vary"), tt.wantVary; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestGoproxyServeFetch(t *testing.T) {
	list := "v1.0.0"
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	var upstreamRequests atomic.Int64
	proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		upstreamRequests.Add(1)
		switch req.URL.Path {
		case "/example.com/@latest":
			responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
		case "/example.com/@v/list":
			responseSuccess(rw, req, strings.NewReader(list), "text/plain; charset=utf-8", -2)
		default:
			switch path.Ext(req.URL.Path) {
			case ".info":
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
			case ".mod":
				responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", -2)
			case ".zip":
				responseSuccess(rw, req, bytes.NewReader(zip), "application/zip", -2)
			default:
				responseNotFound(rw, req, -2)
			}
		}
	}))

	t.Run("CacheControl", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			target       string
			content      string
			cacheControl string
			dynamic      bool
		}{
			{"Latest", "example.com/@latest", info, "public, max-age=60", true},
			{"List", "example.com/@v/list", list, "public, max-age=60", true},
			{"Query", "example.com/@v/master.info", info, "public, max-age=60", true},
			{"Info", "example.com/@v/v1.0.0.info", info, "public, max-age=604800", false},
			{"Mod", "example.com/@v/v1.0.0.mod", mod, "public, max-age=604800", false},
			{"Zip", "example.com/@v/v1.0.0.zip", string(zip), "public, max-age=604800", false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					t.Run(method, func(t *testing.T) {
						for _, mode := range []struct {
							name             string
							noCache          bool
							cached           bool
							noFetch          bool
							header           http.Header
							wantStatusCode   int
							wantCacheControl string
							wantContent      string
						}{
							{
								name:             "Fetch",
								header:           http.Header{"Disable-Module-Fetch": {"false"}},
								wantStatusCode:   http.StatusOK,
								wantCacheControl: tt.cacheControl,
								wantContent:      tt.content,
							},
							{
								name:             "CacheMiss",
								noFetch:          true,
								wantStatusCode:   http.StatusNotFound,
								wantCacheControl: "no-store",
								wantContent:      "not found: temporarily unavailable",
							},
							{
								name:             "CacheDisabled",
								noCache:          true,
								noFetch:          true,
								wantStatusCode:   http.StatusNotFound,
								wantCacheControl: "no-store",
								wantContent:      "not found: temporarily unavailable",
							},
							{
								name:             "CacheHit",
								cached:           true,
								noFetch:          true,
								wantStatusCode:   http.StatusOK,
								wantCacheControl: tt.cacheControl,
								wantContent:      tt.content,
							},
							{
								name:             "NotModified",
								cached:           true,
								noFetch:          true,
								header:           http.Header{"If-None-Match": {"*"}},
								wantStatusCode:   http.StatusNotModified,
								wantCacheControl: tt.cacheControl,
							},
							{
								name:             "PartialContent",
								cached:           true,
								noFetch:          true,
								header:           http.Header{"Range": {"bytes=0-0"}},
								wantStatusCode:   http.StatusPartialContent,
								wantCacheControl: tt.cacheControl,
								wantContent:      tt.content[:1],
							},
							{
								name:             "InvalidRange",
								cached:           true,
								noFetch:          true,
								header:           http.Header{"Range": {"bytes=invalid"}},
								wantStatusCode:   http.StatusRequestedRangeNotSatisfiable,
								wantCacheControl: "no-store",
								wantContent:      "invalid range\n",
							},
						} {
							t.Run(mode.name, func(t *testing.T) {
								g := &Goproxy{
									Fetcher: &GoFetcher{
										Env:     []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
										TempDir: t.TempDir(),
									},
									TempDir: t.TempDir(),
								}
								if !mode.noCache {
									g.Cacher = DirCacher(t.TempDir())
								}
								if mode.cached {
									if err := g.Cacher.Put(t.Context(), tt.target, strings.NewReader(tt.content)); err != nil {
										t.Fatal(err)
									}
								}
								server := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
									rw.Header().Add("Vary", "Accept-Encoding")
									rw.Header().Add("Vary", "Accept-Language")
									g.ServeHTTP(rw, req)
								}))
								req, err := http.NewRequest(method, server.URL+"/"+tt.target, nil)
								if err != nil {
									t.Fatal(err)
								}
								maps.Copy(req.Header, mode.header)
								if mode.noFetch {
									req.Header.Set("Disable-Module-Fetch", "true")
								}
								requestsBefore := upstreamRequests.Load()
								resp, err := server.Client().Do(req)
								if err != nil {
									t.Fatal(err)
								}
								defer resp.Body.Close()
								wantStatusCode, wantCacheControl := mode.wantStatusCode, mode.wantCacheControl
								wantContent := mode.wantContent
								if mode.noFetch && tt.dynamic {
									wantStatusCode = http.StatusNotFound
									wantCacheControl = "no-store"
									wantContent = "not found: temporarily unavailable"
								}
								if method == http.MethodHead && (wantStatusCode == http.StatusPartialContent || wantStatusCode == http.StatusRequestedRangeNotSatisfiable) {
									wantStatusCode, wantCacheControl = http.StatusOK, tt.cacheControl
								}
								if got, want := resp.StatusCode, wantStatusCode; got != want {
									t.Errorf("got status %d, want %d", got, want)
								}
								if got, want := resp.Header.Get("Cache-Control"), wantCacheControl; got != want {
									t.Errorf("got cache control %q, want %q", got, want)
								}
								if method == http.MethodHead && wantStatusCode == http.StatusOK {
									if got, want := resp.Header.Get("Content-Length"), strconv.Itoa(len(tt.content)); got != want {
										t.Errorf("got content length %q, want %q", got, want)
									}
									if got, want := resp.Header.Get("Content-Range"), ""; got != want {
										t.Errorf("got content range %q, want %q", got, want)
									}
								}
								if got, want := resp.Header.Values("Vary"), []string{"Accept-Encoding", "Accept-Language", "Disable-Module-Fetch"}; !slices.Equal(got, want) {
									t.Errorf("got vary %q, want %q", got, want)
								}
								if method == http.MethodHead {
									wantContent = ""
								}
								if b, err := io.ReadAll(resp.Body); err != nil {
									t.Errorf("unexpected error %v", err)
								} else if got, want := string(b), wantContent; got != want {
									t.Errorf("got content %q, want %q", got, want)
								}
								if mode.noFetch {
									if got, want := upstreamRequests.Load(), requestsBefore; got != want {
										t.Errorf("got upstream requests %d, want %d", got, want)
									}
								}
							})
						}
					})
				}
			})
		}
	})

	for _, tt := range []struct {
		n                  int
		cacher             Cacher
		target             string
		disableModuleFetch bool
		wantStatusCode     int
		wantContentType    string
		wantCacheControl   string
		wantContent        string
	}{
		{
			n:                1,
			target:           "example.com/@latest",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n:                  2,
			target:             "example.com/@latest",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusNotFound,
			wantContentType:    "text/plain; charset=utf-8",
			wantCacheControl:   "no-store",
			wantContent:        "not found: temporarily unavailable",
		},
		{
			n:                3,
			target:           "example.com/@v/list",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      list,
		},
		{
			n:                  4,
			target:             "example.com/@v/list",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusNotFound,
			wantContentType:    "text/plain; charset=utf-8",
			wantCacheControl:   "no-store",
			wantContent:        "not found: temporarily unavailable",
		},
		{
			n:                5,
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      info,
		},
		{
			n: 6,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader(info)), nil
				},
			},
			target:             "example.com/@v/v1.0.0.info",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusOK,
			wantContentType:    "application/json; charset=utf-8",
			wantCacheControl:   "public, max-age=604800",
			wantContent:        info,
		},
		{
			n:                7,
			target:           "example.com/@v/v1.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n:                8,
			target:           "example.com/@v/v1.0.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n:                9,
			target:           "example.com/@v/master.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n:                10,
			target:           "example.com/@v/v1.0.0.mod",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      mod,
		},
		{
			n: 11,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader(mod)), nil
				},
			},
			target:             "example.com/@v/v1.0.0.mod",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusOK,
			wantContentType:    "text/plain; charset=utf-8",
			wantCacheControl:   "public, max-age=604800",
			wantContent:        mod,
		},
		{
			n:                12,
			target:           "example.com/@v/v1.0.0.zip",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/zip",
			wantCacheControl: "public, max-age=604800",
			wantContent:      string(zip),
		},
		{
			n: 13,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader(zip)), nil
				},
			},
			target:             "example.com/@v/v1.0.0.zip",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusOK,
			wantContentType:    "application/zip",
			wantCacheControl:   "public, max-age=604800",
			wantContent:        string(zip),
		},
		{
			n:                14,
			target:           "example.com",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found: missing /@v/",
		},
		{
			n:                15,
			target:           "example.com/@/",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found: missing /@v/",
		},
		{
			n:                16,
			target:           "foobar/@latest",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      `not found: invalid escaped module path "foobar": malformed module path "foobar": missing dot in first path element`,
		},
		{
			n:                17,
			target:           "example.com/@v/foobar",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      `not found: no file extension in filename "foobar"`,
		},
		{
			n:                18,
			target:           "example.com/@v/foo.bar",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      `not found: unexpected extension ".bar"`,
		},
		{
			n:                19,
			target:           "example.com/@v/!!v1.0.0.info",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      `not found: invalid escaped version "!!v1.0.0"`,
		},
		{
			n:                20,
			target:           "example.com/@v/latest.info",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found: invalid version",
		},
		{
			n:                21,
			target:           "example.com/@v/upgrade.info",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found: invalid version",
		},
		{
			n:                22,
			target:           "example.com/@v/patch.info",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found: invalid version",
		},
		{
			n:                23,
			target:           "example.com/@v/master.mod",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found: unrecognized version",
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.cacher == nil {
				tt.cacher = DirCacher(t.TempDir())
			}

			g := &Goproxy{
				Fetcher: &GoFetcher{
					Env:     []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
					TempDir: t.TempDir(),
				},
				Cacher:  tt.cacher,
				TempDir: t.TempDir(),
				Logger:  slog.New(slog.DiscardHandler),
			}
			g.initOnce.Do(g.init)

			req := httptest.NewRequest("", "/", nil)
			if tt.disableModuleFetch {
				req.Header.Set("Disable-Module-Fetch", "true")
			}
			rec := httptest.NewRecorder()
			g.serveFetch(rec, req, tt.target)
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Vary"), "Disable-Module-Fetch"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestGoproxyServeFetchQuery(t *testing.T) {
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
	}
	for _, tt := range []struct {
		name             string
		proxyHandler     http.HandlerFunc
		wantStatusCode   int
		wantContentType  string
		wantCacheControl string
		wantContent      string
	}{
		{
			name:             "Success",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			name:             "NotFound",
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.proxyHandler == nil {
				tt.proxyHandler = proxyHandler
			}
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)

			g := &Goproxy{
				Fetcher: &GoFetcher{
					Env:     []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
					TempDir: t.TempDir(),
				},
				TempDir: t.TempDir(),
				Logger:  slog.New(slog.DiscardHandler),
			}
			g.initOnce.Do(g.init)

			rec := httptest.NewRecorder()
			g.serveFetchQuery(rec, httptest.NewRequest("", "/", nil), "example.com/@latest", "example.com", "latest")
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestGoproxyServeFetchList(t *testing.T) {
	list := "v1.0.0\nv1.1.0"
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(list), "text/plain; charset=utf-8", -2)
	}
	for _, tt := range []struct {
		name             string
		proxyHandler     http.HandlerFunc
		wantStatusCode   int
		wantCacheControl string
		wantContent      string
	}{
		{
			name:             "Success",
			wantStatusCode:   http.StatusOK,
			wantCacheControl: "public, max-age=60",
			wantContent:      list,
		},
		{
			name:             "NotFound",
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.proxyHandler == nil {
				tt.proxyHandler = proxyHandler
			}
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)

			g := &Goproxy{
				Fetcher: &GoFetcher{
					Env:     []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
					TempDir: t.TempDir(),
				},
				TempDir: t.TempDir(),
				Logger:  slog.New(slog.DiscardHandler),
			}
			g.initOnce.Do(g.init)

			rec := httptest.NewRecorder()
			g.serveFetchList(rec, httptest.NewRequest("", "/", nil), "example.com/@v/list", "example.com")
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestGoproxyServeFetchDownload(t *testing.T) {
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		switch path.Ext(req.URL.Path) {
		case ".info":
			responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
		case ".mod":
			responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", -2)
		case ".zip":
			responseSuccess(rw, req, bytes.NewReader(zip), "application/zip", -2)
		default:
			responseNotFound(rw, req, -2)
		}
	}

	t.Run("SumDBVerificationFailure", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			statusCode   int
			body         string
			transportErr error
			readError    bool
		}{
			{name: "NotFound", statusCode: http.StatusNotFound, body: "module not found"},
			{name: "Gone", statusCode: http.StatusGone, body: "module removed"},
			{name: "BadRequest", statusCode: http.StatusBadRequest, body: "fetch timed out"},
			{name: "BadGateway", statusCode: http.StatusBadGateway},
			{name: "GatewayTimeout", statusCode: http.StatusGatewayTimeout},
			{name: "TransportFailure", transportErr: &net.DNSError{Err: "no such host", Name: "sumdb.example.com", IsNotFound: true}},
			{name: "TransportTimeout", transportErr: context.DeadlineExceeded},
			{name: "ReadFailure", statusCode: http.StatusOK, readError: true},
			{name: "MalformedLookup", statusCode: http.StatusOK, body: "bad upstream"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				gf := &GoFetcher{
					Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=sum.golang.org https://sumdb.example.com"},
					TempDir: t.TempDir(),
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						rec := httptest.NewRecorder()
						if req.URL.Host == "sumdb.example.com" {
							if tt.transportErr != nil {
								return nil, tt.transportErr
							}
							rec.Header().Set("Retry-After", "2")
							rec.WriteHeader(tt.statusCode)
							fmt.Fprint(rec, tt.body)
						} else {
							proxyHandler(rec, req)
						}
						resp := rec.Result()
						resp.Request = req
						if req.URL.Host == "sumdb.example.com" && tt.readError {
							resp.Body = io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))
						}
						return resp, nil
					}),
				}
				g := &Goproxy{
					Fetcher: gf,
					Cacher: &testCacher{
						Cacher: DirCacher(t.TempDir()),
						put: func(context.Context, Cacher, string, io.ReadSeeker) error {
							t.Error("unexpected cache write")
							return nil
						},
					},
					Logger: slog.New(slog.DiscardHandler),
				}
				for _, resource := range []struct {
					name string
					ext  string
				}{
					{"Info", ".info"},
					{"Mod", ".mod"},
					{"Zip", ".zip"},
				} {
					t.Run(resource.name, func(t *testing.T) {
						for _, method := range []string{http.MethodGet, http.MethodHead} {
							t.Run(method, func(t *testing.T) {
								rec := httptest.NewRecorder()
								g.ServeHTTP(rec, httptest.NewRequest(method, "/example.com/@v/v1.0.0"+resource.ext, nil))
								if got, want := rec.Code, http.StatusNotFound; got != want {
									t.Errorf("got status %d, want %d", got, want)
								}
								if got, want := rec.Header().Get("Cache-Control"), "no-store"; got != want {
									t.Errorf("got cache control %q, want %q", got, want)
								}
								if got, want := rec.Header().Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
									t.Errorf("got content type %q, want %q", got, want)
								}
								wantContent := "not found: bad upstream"
								if method == http.MethodHead {
									wantContent = ""
								}
								if got, want := rec.Body.String(), wantContent; got != want {
									t.Errorf("got content %q, want %q", got, want)
								}
							})
						}
					})
				}
				t.Run("ProxyFallback", func(t *testing.T) {
					var fallbackCalled bool
					fetcher := &GoFetcher{
						Env:     []string{"GOPROXY=https://first.example.com,https://second.example.com", "GOSUMDB=off"},
						TempDir: t.TempDir(),
						Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
							rec := httptest.NewRecorder()
							if req.URL.Host == "first.example.com" {
								g.ServeHTTP(rec, req)
							} else {
								fallbackCalled = true
								proxyHandler(rec, req)
							}
							resp := rec.Result()
							resp.Request = req
							return resp, nil
						}),
					}
					info, mod, zip, err := fetcher.Download(t.Context(), "example.com", "v1.0.0")
					for _, content := range []io.ReadSeekCloser{info, mod, zip} {
						if content != nil {
							content.Close()
						} else {
							t.Error("missing module content")
						}
					}
					if err != nil {
						t.Errorf("unexpected error %v", err)
					}
					if !fallbackCalled {
						t.Error("next proxy was not contacted")
					}
				})
				if entries, err := os.ReadDir(gf.TempDir); err != nil {
					t.Fatal(err)
				} else if len(entries) != 0 {
					t.Errorf("unexpected temporary files %v", entries)
				}
			})
		}
	})

	for _, tt := range []struct {
		n                int
		proxyHandler     http.HandlerFunc
		cacher           Cacher
		target           string
		noFetch          bool
		wantStatusCode   int
		wantContentType  string
		wantCacheControl string
		wantContent      string
	}{
		{
			n:                1,
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      info,
		},
		{
			n: 2,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader(info)), nil
				},
			},
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      info,
		},
		{
			n: 3,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader(info)), nil
				},
			},
			target:           "example.com/@v/v1.0.0.info",
			noFetch:          true,
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      info,
		},
		{
			n:                4,
			target:           "example.com/@v/v1.0.0.mod",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      mod,
		},
		{
			n:                5,
			target:           "example.com/@v/v1.0.0.zip",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/zip",
			wantCacheControl: "public, max-age=604800",
			wantContent:      string(zip),
		},
		{
			n:                6,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			n:                7,
			target:           "example.com/@v/v1.0.0.info",
			noFetch:          true,
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "no-store",
			wantContent:      "not found: temporarily unavailable",
		},
		{
			n: 8,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return nil, errors.New("cannot get")
				},
			},
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusInternalServerError,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "no-store",
			wantContent:      "internal server error",
		},
		{
			n: 9,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
					return errors.New("cannot put")
				},
			},
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusInternalServerError,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "no-store",
			wantContent:      "internal server error",
		},
		{
			n: 10,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
					if err := c.Put(ctx, name, content); err != nil {
						return err
					}
					return content.(io.Closer).Close()
				},
			},
			target:           "example.com/@v/v1.0.0.mod",
			wantStatusCode:   http.StatusInternalServerError,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "no-store",
			wantContent:      "internal server error",
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.proxyHandler == nil {
				tt.proxyHandler = proxyHandler
			}
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)
			if tt.cacher == nil {
				tt.cacher = DirCacher(t.TempDir())
			}

			g := &Goproxy{
				Fetcher: &GoFetcher{
					Env:     []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
					TempDir: t.TempDir(),
				},
				Cacher:  tt.cacher,
				TempDir: t.TempDir(),
				Logger:  slog.New(slog.DiscardHandler),
			}
			g.initOnce.Do(g.init)

			escapedModulePath, after, ok := strings.Cut(tt.target, "/@v/")
			if !ok {
				t.Fatalf("invalid target %q", tt.target)
			}
			modulePath, err := module.UnescapePath(escapedModulePath)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			moduleVersion, err := module.UnescapeVersion(strings.TrimSuffix(after, path.Ext(after)))
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			rec := httptest.NewRecorder()
			g.serveFetchDownload(rec, httptest.NewRequest("", "/", nil), tt.target, modulePath, moduleVersion, tt.noFetch)
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}

	t.Run("CachedContentClosed", func(t *testing.T) {
		// Track whether Close was called on the cached content
		var closeCalled bool

		// Create a custom cacher that returns closable content
		cacher := &testCacher{
			Cacher: DirCacher(t.TempDir()),
			get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
				return struct {
					io.Reader
					io.Closer
				}{
					Reader: strings.NewReader(info),
					Closer: closerFunc(func() error {
						closeCalled = true
						return nil
					}),
				}, nil
			},
		}

		g := &Goproxy{
			Fetcher: &GoFetcher{
				Env:     []string{"GOPROXY=off", "GOSUMDB=off"},
				TempDir: t.TempDir(),
			},
			Cacher:  cacher,
			TempDir: t.TempDir(),
			Logger:  slog.New(slog.DiscardHandler),
		}
		g.initOnce.Do(g.init)

		rec := httptest.NewRecorder()
		target := "example.com/@v/v1.0.0.info"
		g.serveFetchDownload(rec, httptest.NewRequest("", "/", nil), target, "example.com", "v1.0.0", false)

		if !closeCalled {
			t.Error("cached content was not closed, potential memory leak")
		}

		recr := rec.Result()
		if got, want := recr.StatusCode, http.StatusOK; got != want {
			t.Errorf("got status %d, want %d", got, want)
		}
	})
}

func TestGoproxyServeSumDB(t *testing.T) {
	t.Run("CacheFallback", func(t *testing.T) {
		for _, resource := range []struct {
			name string
			path string
		}{
			{"Latest", "/latest"},
			{"Lookup", "/lookup/example.com@v1.0.0"},
			{"DataTile", "/tile/2/data/000"},
			{"PartialDataTile", "/tile/2/data/000.p/1"},
			{"HashTile", "/tile/2/0/000"},
			{"PartialHashTile", "/tile/2/0/000.p/1"},
		} {
			for _, cache := range []struct {
				name string
				err  error
			}{
				{"Hit", nil},
				{"Miss", fs.ErrNotExist},
				{"Error", errors.New("cannot get")},
			} {
				for _, condition := range []struct {
					name   string
					header http.Header
				}{
					{"Unconditional", nil},
					{"IfNoneMatch", http.Header{"If-None-Match": {`"old"`}}},
					{"IfModifiedSince", http.Header{"If-Modified-Since": {"Sat, 01 Jan 2000 00:00:00 GMT"}}},
				} {
					for _, method := range []string{http.MethodGet, http.MethodHead} {
						t.Run(resource.name+"/"+cache.name+"/"+condition.name+"/"+method, func(t *testing.T) {
							g := &Goproxy{
								ProxiedSumDBs: []string{"sumdb.example.com"},
								TempDir:       t.TempDir(),
								Logger:        slog.New(slog.DiscardHandler),
								Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
									return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
								}),
								Cacher: &testCacher{
									get: func(context.Context, Cacher, string) (io.ReadCloser, error) {
										t.Error("unexpected cache read")
										if cache.err != nil {
											return nil, cache.err
										}
										return struct {
											io.ReadSeeker
											io.Closer
											successResponseBody_ModTime
											successResponseBody_ETag
										}{
											strings.NewReader("old checkpoint"),
											closerFunc(func() error { return nil }),
											successResponseBody_ModTime{modTime: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
											successResponseBody_ETag{etag: `"old"`},
										}, nil
									},
									put: func(context.Context, Cacher, string, io.ReadSeeker) error {
										t.Error("unexpected cache write")
										return nil
									},
								},
							}
							req := httptest.NewRequest(method, "/sumdb/sumdb.example.com"+resource.path, nil)
							maps.Copy(req.Header, condition.header)
							rec := httptest.NewRecorder()
							g.ServeHTTP(rec, req)
							if got, want := rec.Code, http.StatusBadGateway; got != want {
								t.Errorf("got status %d, want %d", got, want)
							}
							if got, want := rec.Header().Get("Cache-Control"), "no-store"; got != want {
								t.Errorf("got cache control %q, want %q", got, want)
							}
							for _, name := range []string{"ETag", "Last-Modified"} {
								if got := rec.Header().Get(name); got != "" {
									t.Errorf("unexpected %s header %q", name, got)
								}
							}
							wantContent := "bad gateway"
							if method == http.MethodHead {
								wantContent = ""
							}
							if got, want := rec.Body.String(), wantContent; got != want {
								t.Errorf("got content %q, want %q", got, want)
							}
						})
					}
				}
			}
		}
	})

	t.Run("SuccessfulCacheRestrictions", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			header     http.Header
			restricted bool
		}{
			{"NoHeaders", nil, false},
			{"Public", http.Header{"Cache-Control": {"public, max-age=3600"}}, false},
			{"NoStore", http.Header{"Cache-Control": {"no-store"}}, true},
			{"NoCache", http.Header{"Cache-Control": {"no-cache"}}, true},
			{"Private", http.Header{"Cache-Control": {"private"}}, true},
			{"MustRevalidate", http.Header{"Cache-Control": {"must-revalidate"}}, true},
			{"ProxyRevalidate", http.Header{"Cache-Control": {"proxy-revalidate"}}, true},
			{"FreshMustRevalidate", http.Header{"Cache-Control": {"max-age=86400, must-revalidate"}}, true},
			{"ZeroMaxAge", http.Header{"Cache-Control": {"max-age=0"}}, true},
			{"QuotedZeroMaxAge", http.Header{"Cache-Control": {`max-age="000"`}}, true},
			{"EscapedZeroMaxAge", http.Header{"Cache-Control": {`max-age="\0"`}}, true},
			{"QuotedPositiveMaxAge", http.Header{"Cache-Control": {`max-age="60"`}}, false},
			{"EscapedPositiveMaxAge", http.Header{"Cache-Control": {`max-age="\60"`}}, false},
			{"ZeroSharedMaxAge", http.Header{"Cache-Control": {"s-maxage=0"}}, true},
			{"PositiveSharedMaxAge", http.Header{"Cache-Control": {"s-maxage=86400"}}, true},
			{"SharedMaxAgeOverridesZeroMaxAge", http.Header{"Cache-Control": {"max-age=0, s-maxage=86400"}}, true},
			{"InvalidMaxAge", http.Header{"Cache-Control": {"max-age=invalid"}}, true},
			{"DuplicateMaxAge", http.Header{"Cache-Control": {"max-age=60", "max-age=120"}}, true},
			{"QuotedRevalidation", http.Header{"Cache-Control": {`extension="must-revalidate, proxy-revalidate, s-maxage=0, max-age=0"`}}, false},
			{"QualifiedNoCache", http.Header{"Cache-Control": {`no-cache="ETag"`}}, true},
			{"QualifiedPrivate", http.Header{"Cache-Control": {`private="Set-Cookie"`}}, true},
			{"VaryAll", http.Header{"Vary": {"Accept-Encoding", "*"}}, true},
			{"MultipleLines", http.Header{"Cache-Control": {"public", "No-Store"}}, true},
			{"QuotedExtension", http.Header{"Cache-Control": {`extension="no-store, private", public`}}, false},
		} {
			for _, endpoint := range []struct {
				name         string
				path         string
				content      string
				contentType  string
				cacheControl string
			}{
				{"Latest", "/latest", "latest", "text/plain; charset=utf-8", "public, max-age=60"},
				{"Lookup", "/lookup/example.com@v1.0.0", "lookup", "text/plain; charset=utf-8", "public, max-age=86400"},
				{"DataTile", "/tile/8/data/000", "data", "text/plain; charset=utf-8", "public, max-age=86400"},
				{"HashTile", "/tile/8/0/000.p/1", strings.Repeat("x", tlog.HashSize), "application/octet-stream", "public, max-age=86400"},
			} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					for _, cache := range []struct {
						name    string
						enabled bool
					}{
						{"NoCacher", false},
						{"WithCacher", true},
					} {
						t.Run(tt.name+"/"+endpoint.name+"/"+method+"/"+cache.name, func(t *testing.T) {
							g := &Goproxy{
								ProxiedSumDBs: []string{"sumdb.example.com"},
								TempDir:       t.TempDir(),
								Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
									if got, want := req.URL.Path, endpoint.path; got != want {
										t.Errorf("got upstream path %q, want %q", got, want)
									}
									return &http.Response{
										StatusCode: http.StatusOK,
										Header:     tt.header.Clone(),
										Body:       io.NopCloser(strings.NewReader(endpoint.content)),
									}, nil
								}),
							}
							target := "sumdb/sumdb.example.com" + endpoint.path
							if cache.enabled {
								g.Cacher = &testCacher{
									get: func(context.Context, Cacher, string) (io.ReadCloser, error) {
										t.Error("unexpected cache read")
										return nil, fs.ErrNotExist
									},
									put: func(context.Context, Cacher, string, io.ReadSeeker) error {
										t.Error("unexpected cache write")
										return errors.New("cannot put")
									},
								}
							}
							rec := httptest.NewRecorder()
							g.ServeHTTP(rec, httptest.NewRequest(method, "/"+target, nil))
							if got, want := rec.Code, http.StatusOK; got != want {
								t.Errorf("got status %d, want %d", got, want)
							}
							wantCacheControl := endpoint.cacheControl
							if tt.restricted {
								wantCacheControl = "no-store"
							}
							if got, want := rec.Header().Get("Cache-Control"), wantCacheControl; got != want {
								t.Errorf("got cache control %q, want %q", got, want)
							}
							if got, want := rec.Header().Get("Content-Type"), endpoint.contentType; got != want {
								t.Errorf("got content type %q, want %q", got, want)
							}
							wantContent := endpoint.content
							if method == http.MethodHead {
								wantContent = ""
							}
							if got, want := rec.Body.String(), wantContent; got != want {
								t.Errorf("got content %q, want %q", got, want)
							}
							if entries, err := os.ReadDir(g.TempDir); err != nil {
								t.Fatal(err)
							} else if len(entries) != 0 {
								t.Error("temporary files were not removed")
							}
						})
					}
				}
			}
		}
	})

	t.Run("ConditionalRequests", func(t *testing.T) {
		for _, resource := range []struct {
			name     string
			path     string
			maxAge   int
			bodySize int
		}{
			{"Latest", "/latest", 60, 6},
			{"Lookup", "/lookup/example.com@v1.0.0", 86400, 6},
			{"DataTile", "/tile/2/data/000", 86400, 6},
			{"PartialDataTile", "/tile/2/data/000.p/1", 86400, 6},
			{"HashTile", "/tile/2/0/000", 86400, 4 * tlog.HashSize},
			{"PartialHashTile", "/tile/2/0/000.p/1", 86400, tlog.HashSize},
		} {
			for _, policy := range []struct {
				name       string
				restricted bool
			}{
				{"Public", false},
				{"NoStore", true},
			} {
				for _, tt := range []struct {
					name           string
					method         string
					header         http.Header
					wantStatusCode int
				}{
					{"Get", http.MethodGet, nil, http.StatusOK},
					{"Head", http.MethodHead, nil, http.StatusOK},
					{"Range", http.MethodGet, http.Header{"Range": {"bytes=0-2"}}, http.StatusPartialContent},
					{"HeadRange", http.MethodHead, http.Header{"Range": {"bytes=0-2"}}, http.StatusOK},
					{"NotModified", http.MethodGet, http.Header{"If-None-Match": {"*"}}, http.StatusNotModified},
					{"PreconditionFailed", http.MethodGet, http.Header{"If-Match": {`"missing"`}}, http.StatusPreconditionFailed},
					{"InvalidRange", http.MethodGet, http.Header{"Range": {"bytes=1000-"}}, http.StatusRequestedRangeNotSatisfiable},
				} {
					t.Run(resource.name+"/"+policy.name+"/"+tt.name, func(t *testing.T) {
						body := strings.Repeat("x", resource.bodySize)
						g := &Goproxy{
							ProxiedSumDBs: []string{"sumdb.example.com"},
							TempDir:       t.TempDir(),
							Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
								header := make(http.Header)
								if policy.restricted {
									header.Set("Cache-Control", "no-store")
								}
								return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
							}),
							Cacher: &testCacher{
								get: func(context.Context, Cacher, string) (io.ReadCloser, error) {
									t.Error("unexpected cache read")
									return nil, errors.New("cannot get")
								},
								put: func(context.Context, Cacher, string, io.ReadSeeker) error {
									t.Error("unexpected cache write")
									return errors.New("cannot put")
								},
							},
						}
						req := httptest.NewRequest(tt.method, "/sumdb/sumdb.example.com"+resource.path, nil)
						maps.Copy(req.Header, tt.header)
						rec := httptest.NewRecorder()
						g.ServeHTTP(rec, req)
						if got, want := rec.Code, tt.wantStatusCode; got != want {
							t.Errorf("got status %d, want %d", got, want)
						}
						wantCacheControl := "public, max-age=" + strconv.Itoa(resource.maxAge)
						if policy.restricted || tt.wantStatusCode >= 400 {
							wantCacheControl = "no-store"
						}
						if got, want := rec.Header().Get("Cache-Control"), wantCacheControl; got != want {
							t.Errorf("got cache control %q, want %q", got, want)
						}
						var wantContent string
						if tt.method != http.MethodHead {
							switch tt.wantStatusCode {
							case http.StatusOK:
								wantContent = body
							case http.StatusPartialContent:
								wantContent = body[:3]
							case http.StatusRequestedRangeNotSatisfiable:
								wantContent = "invalid range: failed to overlap\n"
							}
						}
						if got, want := rec.Body.String(), wantContent; got != want {
							t.Errorf("got content %q, want %q", got, want)
						}
					})
				}
			}
		}
	})

	t.Run("UpstreamFailures", func(t *testing.T) {
		for _, tt := range []struct {
			name             string
			statusCode       int
			transportErr     error
			readErr          error
			header           http.Header
			wantStatusCode   int
			wantCacheControl string
			wantContent      string
		}{
			{"BadRequest", http.StatusBadRequest, nil, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"Unauthorized", http.StatusUnauthorized, nil, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"Forbidden", http.StatusForbidden, nil, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"RequestTimeout", http.StatusRequestTimeout, nil, nil, nil, http.StatusGatewayTimeout, "no-store", "gateway timeout"},
			{"NotFound", http.StatusNotFound, nil, nil, nil, http.StatusNotFound, "public, max-age=60", "not found: upstream details"},
			{"Gone", http.StatusGone, nil, nil, nil, http.StatusNotFound, "public, max-age=60", "not found: upstream details"},
			{"UncacheableAbsence", http.StatusNotFound, nil, nil, http.Header{"Cache-Control": {"no-store"}}, http.StatusNotFound, "no-store", "not found: upstream details"},
			{"TooManyRequests", http.StatusTooManyRequests, nil, nil, nil, http.StatusServiceUnavailable, "no-store", "service unavailable"},
			{"InternalServerError", http.StatusInternalServerError, nil, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"BadGateway", http.StatusBadGateway, nil, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"ServiceUnavailable", http.StatusServiceUnavailable, nil, nil, nil, http.StatusServiceUnavailable, "no-store", "service unavailable"},
			{"GatewayTimeout", http.StatusGatewayTimeout, nil, nil, nil, http.StatusGatewayTimeout, "no-store", "gateway timeout"},
			{"RateLimitRetryAfter", http.StatusTooManyRequests, nil, nil, http.Header{"Retry-After": {"60"}}, http.StatusServiceUnavailable, "no-store", "service unavailable"},
			{"UnavailableRetryAfter", http.StatusServiceUnavailable, nil, nil, http.Header{"Retry-After": {"60"}}, http.StatusServiceUnavailable, "no-store", "service unavailable"},
			{"BadGatewayRetryAfter", http.StatusBadGateway, nil, nil, http.Header{"Retry-After": {"60"}}, http.StatusBadGateway, "no-store", "bad gateway"},
			{"GatewayTimeoutRetryAfter", http.StatusGatewayTimeout, nil, nil, http.Header{"Retry-After": {"60"}}, http.StatusGatewayTimeout, "no-store", "gateway timeout"},
			{"NoContent", http.StatusNoContent, nil, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"PartialContent", http.StatusPartialContent, nil, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"NotModified", http.StatusNotModified, nil, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"Transport", 0, io.ErrUnexpectedEOF, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"TransportNotExist", 0, &net.OpError{Op: "dial", Net: "unix", Err: &os.SyscallError{Syscall: "connect", Err: fs.ErrNotExist}}, nil, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"TransportTimeout", 0, &net.DNSError{Err: "timeout", IsTimeout: true}, nil, nil, http.StatusGatewayTimeout, "no-store", "gateway timeout"},
			{"TransportDeadline", 0, context.DeadlineExceeded, nil, nil, http.StatusGatewayTimeout, "no-store", "gateway timeout"},
			{"TransportCanceled", 0, context.Canceled, nil, nil, http.StatusInternalServerError, "no-store", "internal server error"},
			{"Read", http.StatusOK, nil, io.ErrUnexpectedEOF, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"ReadNotExist", http.StatusOK, nil, fs.ErrNotExist, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"ReadTimeout", http.StatusOK, nil, os.ErrDeadlineExceeded, nil, http.StatusGatewayTimeout, "no-store", "gateway timeout"},
			{"ReadCanceled", http.StatusOK, nil, context.Canceled, nil, http.StatusInternalServerError, "no-store", "internal server error"},
			{"AbsenceReadError", http.StatusNotFound, nil, fs.ErrNotExist, nil, http.StatusBadGateway, "no-store", "bad gateway"},
			{"AbsenceReadTimeout", http.StatusGone, nil, os.ErrDeadlineExceeded, nil, http.StatusGatewayTimeout, "no-store", "gateway timeout"},
			{"UnavailableReadError", http.StatusServiceUnavailable, nil, io.ErrUnexpectedEOF, nil, http.StatusServiceUnavailable, "no-store", "service unavailable"},
			{"UnavailableReadTimeout", http.StatusServiceUnavailable, nil, os.ErrDeadlineExceeded, nil, http.StatusGatewayTimeout, "no-store", "gateway timeout"},
			{"GatewayTimeoutReadError", http.StatusGatewayTimeout, nil, io.ErrUnexpectedEOF, nil, http.StatusGatewayTimeout, "no-store", "gateway timeout"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					t.Run(method, func(t *testing.T) {
						synctest.Test(t, func(t *testing.T) {
							g := &Goproxy{
								ProxiedSumDBs: []string{"sumdb.example.com"},
								TempDir:       t.TempDir(),
								Logger:        slog.New(slog.DiscardHandler),
								Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
									if tt.transportErr != nil {
										return nil, tt.transportErr
									}
									var body io.Reader = strings.NewReader("upstream details")
									if tt.readErr != nil {
										body = iotest.ErrReader(tt.readErr)
									}
									header := http.Header{
										"Cache-Control":    {"public, max-age=86400"},
										"Content-Type":     {"text/html"},
										"Set-Cookie":       {"session=secret"},
										"Etag":             {`"upstream"`},
										"Www-Authenticate": {`Basic realm="upstream"`},
									}
									maps.Copy(header, tt.header)
									return &http.Response{
										StatusCode: tt.statusCode,
										Status:     strconv.Itoa(tt.statusCode) + " " + http.StatusText(tt.statusCode),
										Header:     header,
										Body:       io.NopCloser(body),
										Request:    req,
									}, nil
								}),
								Cacher: &testCacher{
									get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
										t.Error("unexpected cache read")
										return nil, fs.ErrNotExist
									},
									put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
										t.Error("unexpected cache write")
										return nil
									},
								},
							}
							rec := httptest.NewRecorder()
							g.ServeHTTP(rec, httptest.NewRequest(method, "/sumdb/sumdb.example.com/latest", nil))
							if got, want := rec.Code, tt.wantStatusCode; got != want {
								t.Errorf("got status %d, want %d", got, want)
							}
							if got, want := rec.Header().Get("Cache-Control"), tt.wantCacheControl; got != want {
								t.Errorf("got cache control %q, want %q", got, want)
							}
							if got, want := rec.Header().Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
								t.Errorf("got content type %q, want %q", got, want)
							}
							for _, name := range []string{"Set-Cookie", "ETag", "WWW-Authenticate", "Retry-After"} {
								if got := rec.Header().Get(name); got != "" {
									t.Errorf("unexpected %s header %q", name, got)
								}
							}
							wantContent := tt.wantContent
							if method == http.MethodHead {
								wantContent = ""
							}
							if got, want := rec.Body.String(), wantContent; got != want {
								t.Errorf("got content %q, want %q", got, want)
							}
							if entries, err := os.ReadDir(g.TempDir); err != nil {
								t.Fatal(err)
							} else if len(entries) != 0 {
								t.Errorf("unexpected temporary files %v", entries)
							}
						})
					})
				}
			})
		}
	})

	t.Run("UpstreamURLs", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			url     string
			wantURL string
		}{
			{"MissingHTTPHost", "http://", ""},
			{"MissingHTTPSHost", "https://", ""},
			{"HostlessHTTPPath", "https:///base", ""},
			{"PortOnlyHTTPHost", "https://:443/base", ""},
			{"Relative", "/base", ""},
			{"Opaque", "custom:opaque", ""},
			{"EmptyFilePath", "file://", ""},
			{"EmptyCustomPath", "custom://", ""},
			{"HTTP", "http://upstream.example.com/base", "http://upstream.example.com/base/latest"},
			{"HTTPS", "https://upstream.example.com/base%20path?token=value#fragment", "https://upstream.example.com/base%20path/latest?token=value#fragment"},
			{"File", "file:///base", "file:///base/latest"},
			{"Custom", "custom://upstream/base", "custom://upstream/base/latest"},
			{"CustomPath", "custom:/base", "custom:/base/latest"},
			{"CustomRoot", "custom:///", "custom:///latest"},
		} {
			for _, resource := range []struct {
				name string
				path string
			}{
				{"Supported", "/supported"},
				{"Latest", "/latest"},
			} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					t.Run(tt.name+"/"+resource.name+"/"+method, func(t *testing.T) {
						const content = "signed tree"
						upstreamCalls := 0
						g := &Goproxy{
							ProxiedSumDBs: []string{"sumdb.example.com " + tt.url},
							Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
								upstreamCalls++
								if got, want := req.URL.String(), tt.wantURL; got != want {
									t.Errorf("got upstream URL %q, want %q", got, want)
									return nil, context.Canceled
								}
								return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(content))}, nil
							}),
							TempDir: t.TempDir(),
							Logger:  slog.New(slog.DiscardHandler),
						}
						rec := httptest.NewRecorder()
						g.ServeHTTP(rec, httptest.NewRequest(method, "/sumdb/sumdb.example.com"+resource.path, nil))
						wantStatusCode, wantContent, wantCalls := http.StatusOK, content, 1
						wantCacheControl := "public, max-age=60"
						if tt.wantURL == "" {
							wantStatusCode, wantContent, wantCalls = http.StatusNotFound, "not found", 0
						} else if resource.path == "/supported" {
							wantContent, wantCalls = "", 0
							wantCacheControl = "public, max-age=86400"
						}
						if method == http.MethodHead {
							wantContent = ""
						}
						if got, want := rec.Code, wantStatusCode; got != want {
							t.Errorf("got status %d, want %d", got, want)
						}
						if got, want := rec.Header().Get("Cache-Control"), wantCacheControl; got != want {
							t.Errorf("got cache control %q, want %q", got, want)
						}
						if got, want := rec.Body.String(), wantContent; got != want {
							t.Errorf("got content %q, want %q", got, want)
						}
						if got, want := upstreamCalls, wantCalls; got != want {
							t.Errorf("got %d upstream requests, want %d", got, want)
						}
					})
				}
			}
		}

		t.Run("FileTransport", func(t *testing.T) {
			const content = "signed tree"
			transport := &http.Transport{}
			transport.RegisterProtocol("file", http.NewFileTransportFS(fstest.MapFS{
				"base/latest": {Data: []byte(content)},
			}))
			g := &Goproxy{
				ProxiedSumDBs: []string{"sumdb.example.com file:///base"},
				Transport:     transport,
				TempDir:       t.TempDir(),
				Logger:        slog.New(slog.DiscardHandler),
			}
			rec := httptest.NewRecorder()
			g.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sumdb/sumdb.example.com/latest", nil))
			if got, want := rec.Code, http.StatusOK; got != want {
				t.Errorf("got status %d, want %d", got, want)
			}
			if got, want := rec.Body.String(), content; got != want {
				t.Errorf("got content %q, want %q", got, want)
			}
		})
	})

	t.Run("NamePaths", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			sumdbName   string
			wantBaseURL string
		}{
			{"Host", "sumdb.example.com", "https://sumdb.example.com"},
			{"Path", "sumdb.example.com/v2", "https://sumdb.example.com/v2"},
			{"NestedPath", "sumdb.example.com/v2/nested", "https://upstream.example.com/base"},
			{"SiblingPrefix", "sumdb.example.com/v20", "https://other.example.com/mirror"},
			{"EqualLength", "sumdb.example.com/v3", "https://upstream.example.com/third"},
			{"EndpointName", "other.example.com/lookup", "https://other.example.com/lookup"},
		} {
			for _, resource := range []struct {
				name        string
				path        string
				contentType string
				maxAge      int
			}{
				{"Supported", "/supported", "", 86400},
				{"Latest", "/latest", "text/plain; charset=utf-8", 60},
				{"Lookup", "/lookup/example.com/!project@v1.2.3", "text/plain; charset=utf-8", 86400},
				{"HashTile", "/tile/2/0/000", "application/octet-stream", 86400},
				{"PartialHashTile", "/tile/3/0/000.p/4", "application/octet-stream", 86400},
				{"DataTile", "/tile/2/data/000", "text/plain; charset=utf-8", 86400},
				{"PartialDataTile", "/tile/2/data/000.p/1", "text/plain; charset=utf-8", 86400},
			} {
				for _, mode := range []struct {
					name     string
					method   string
					notFound bool
				}{
					{"GET", http.MethodGet, false},
					{"HEAD", http.MethodHead, false},
					{"NotFound", http.MethodGet, true},
					{"NotFoundHEAD", http.MethodHead, true},
				} {
					t.Run(tt.name+"/"+resource.name+"/"+mode.name, func(t *testing.T) {
						target := "sumdb/" + tt.sumdbName + resource.path
						body := strings.Repeat("x", 128)
						upstreamCalls := 0
						g := &Goproxy{
							ProxiedSumDBs: []string{
								"sumdb.example.com",
								"sumdb.example.com/v2",
								"sumdb.example.com/v2/nested https://upstream.example.com/base",
								"sumdb.example.com/v20 https://other.example.com/mirror",
								"sumdb.example.com/v3 https://upstream.example.com/third",
								"other.example.com/lookup",
							},
							Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
								upstreamCalls++
								if got, want := req.URL.String(), tt.wantBaseURL+resource.path; got != want {
									t.Errorf("got upstream URL %q, want %q", got, want)
								}
								if got, want := req.Method, http.MethodGet; got != want {
									t.Errorf("got upstream method %q, want %q", got, want)
								}
								statusCode := http.StatusOK
								if mode.notFound {
									statusCode = http.StatusNotFound
								}
								return &http.Response{StatusCode: statusCode, Body: io.NopCloser(strings.NewReader(body))}, nil
							}),
							Cacher: &testCacher{
								get: func(context.Context, Cacher, string) (io.ReadCloser, error) {
									t.Error("unexpected cache read")
									return nil, errors.New("cannot get")
								},
								put: func(context.Context, Cacher, string, io.ReadSeeker) error {
									t.Error("unexpected cache write")
									return errors.New("cannot put")
								},
							},
							TempDir: t.TempDir(),
							Logger:  slog.New(slog.DiscardHandler),
						}
						rec := httptest.NewRecorder()
						g.ServeHTTP(rec, httptest.NewRequest(mode.method, "/"+target, nil))
						wantStatusCode, wantMaxAge := http.StatusOK, resource.maxAge
						wantContent := body
						wantCalls := 1
						if resource.path == "/supported" {
							wantContent = ""
							wantCalls = 0
						} else if mode.notFound {
							wantStatusCode, wantMaxAge = http.StatusNotFound, 60
							wantContent = "not found: " + body
						}
						if mode.method == http.MethodHead {
							wantContent = ""
						}
						if got, want := rec.Code, wantStatusCode; got != want {
							t.Errorf("got status %d, want %d", got, want)
						}
						wantContentType := resource.contentType
						if mode.notFound && resource.path != "/supported" {
							wantContentType = "text/plain; charset=utf-8"
						}
						if got, want := rec.Header().Get("Content-Type"), wantContentType; got != want {
							t.Errorf("got content type %q, want %q", got, want)
						}
						if got, want := rec.Header().Get("Cache-Control"), "public, max-age="+strconv.Itoa(wantMaxAge); got != want {
							t.Errorf("got cache control %q, want %q", got, want)
						}
						if got, want := rec.Body.String(), wantContent; got != want {
							t.Errorf("got content %q, want %q", got, want)
						}
						if got, want := upstreamCalls, wantCalls; got != want {
							t.Errorf("got %d upstream requests, want %d", got, want)
						}
					})
				}
			}
		}
	})

	t.Run("NameBoundaries", func(t *testing.T) {
		g := &Goproxy{
			ProxiedSumDBs: []string{
				"sumdb.example.com",
				"sumdb.example.com/v2",
				"sumdb.example.com/lookup",
				"sumdb.example.com/tile",
			},
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				t.Errorf("unexpected upstream request %s", req.URL)
				return nil, context.Canceled
			}),
			TempDir: filepath.Join(t.TempDir(), "404"),
			Logger:  slog.New(slog.DiscardHandler),
		}
		for _, tt := range []struct {
			name             string
			path             string
			wantCacheControl string
		}{
			{"UnknownHost", "other.example.com/v2/supported", "public, max-age=60"},
			{"HostSuffix", "sumdb.example.com.evil/supported", "public, max-age=60"},
			{"UnknownPath", "sumdb.example.com/v3/supported", "public, max-age=86400"},
			{"PathSuffix", "sumdb.example.com/v20/supported", "public, max-age=86400"},
			{"MissingResource", "sumdb.example.com/v2", "public, max-age=86400"},
			{"UnknownResource", "sumdb.example.com/v2/unknown", "public, max-age=86400"},
			{"SupportedSuffix", "sumdb.example.com/v2/supported/extra", "public, max-age=86400"},
			{"LatestSuffix", "sumdb.example.com/v2/latest/extra", "public, max-age=86400"},
			{"InvalidLookup", "sumdb.example.com/v2/lookup/example.com@main", "public, max-age=86400"},
			{"InvalidTile", "sumdb.example.com/v2/tile/8/0/1", "public, max-age=86400"},
			{"LookupNameOverlap", "sumdb.example.com/lookup/example.com@v1.0.0", "public, max-age=86400"},
			{"TileNameOverlap", "sumdb.example.com/tile/2/0/000", "public, max-age=86400"},
		} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				t.Run(tt.name+"/"+method, func(t *testing.T) {
					rec := httptest.NewRecorder()
					g.ServeHTTP(rec, httptest.NewRequest(method, "/sumdb/"+tt.path, nil))
					if got, want := rec.Code, http.StatusNotFound; got != want {
						t.Errorf("got status %d, want %d", got, want)
					}
					if got, want := rec.Header().Get("Cache-Control"), tt.wantCacheControl; got != want {
						t.Errorf("got cache control %q, want %q", got, want)
					}
				})
			}
		}
	})

	t.Run("OpenError", func(t *testing.T) {
		probe := filepath.Join(t.TempDir(), "unreadable")
		if err := os.WriteFile(probe, []byte("content"), 0); err != nil {
			t.Fatal(err)
		}
		if f, err := os.Open(probe); err == nil {
			f.Close()
			t.Skip("file permissions do not prevent reads")
		} else if !errors.Is(err, fs.ErrPermission) {
			t.Fatal(err)
		}
		for _, tt := range []struct {
			name string
			path string
		}{
			{"Latest", "/latest"},
			{"Lookup", "/lookup/example.com@v1.0.0"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				tempDir := t.TempDir()
				g := &Goproxy{
					ProxiedSumDBs: []string{"sumdb.example.com"},
					TempDir:       tempDir,
					Logger:        slog.New(slog.DiscardHandler),
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						files, err := filepath.Glob(filepath.Join(tempDir, "*", "*"))
						if err != nil {
							t.Fatal(err)
						}
						if len(files) != 1 {
							t.Fatalf("got temporary files %v, want one file", files)
						}
						if err := os.Chmod(files[0], 0); err != nil {
							t.Fatal(err)
						}
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("content"))}, nil
					}),
				}
				rec := httptest.NewRecorder()
				g.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sumdb/sumdb.example.com"+tt.path, nil))
				if got, want := rec.Code, http.StatusInternalServerError; got != want {
					t.Errorf("got status %d, want %d", got, want)
				}
				if got, want := rec.Header().Get("Cache-Control"), "no-store"; got != want {
					t.Errorf("got cache control %q, want %q", got, want)
				}
				if got, want := rec.Body.String(), "internal server error"; got != want {
					t.Errorf("got content %q, want %q", got, want)
				}
				if entries, err := os.ReadDir(tempDir); err != nil {
					t.Fatal(err)
				} else if len(entries) != 0 {
					t.Error("temporary files were not removed")
				}
			})
		}
	})

	t.Run("StatError", func(t *testing.T) {
		tempDir := t.TempDir()
		removeErr := make(chan error, 1)
		server := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			removeErr <- os.RemoveAll(tempDir)
			fmt.Fprint(rw, "latest")
		}))
		cacheCalls := 0
		g := &Goproxy{
			ProxiedSumDBs: []string{"sumdb.example.com " + server.URL},
			TempDir:       tempDir,
			Logger:        slog.New(slog.DiscardHandler),
			Cacher: &testCacher{
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					cacheCalls++
					return nil, fs.ErrNotExist
				},
				put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
					cacheCalls++
					return nil
				},
			},
		}
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sumdb/sumdb.example.com/latest", nil))
		if err := <-removeErr; err != nil {
			t.Skipf("cannot remove open temporary file: %v", err)
		}
		if got, want := rec.Code, http.StatusInternalServerError; got != want {
			t.Errorf("got status %d, want %d", got, want)
		}
		if got, want := rec.Result().Header.Get("Cache-Control"), "no-store"; got != want {
			t.Errorf("got cache control %q, want %q", got, want)
		}
		if got, want := rec.Body.String(), "internal server error"; got != want {
			t.Errorf("got content %q, want %q", got, want)
		}
		if got, want := cacheCalls, 0; got != want {
			t.Errorf("got %d cache calls, want %d", got, want)
		}
	})

	t.Run("ReadError", func(t *testing.T) {
		body := strings.Repeat("x", 128)
		server := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.Header().Set("Content-Length", "129")
			fmt.Fprint(rw, body)
		}))
		for _, tt := range []struct {
			name   string
			cached bool
		}{
			{"CacheMiss", false},
			{"CacheHit", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				g := &Goproxy{
					ProxiedSumDBs: []string{"sumdb.example.com " + server.URL},
					TempDir:       t.TempDir(),
					Logger:        slog.New(slog.DiscardHandler),
					Cacher: &testCacher{
						get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
							t.Error("unexpected cache read")
							if tt.cached {
								return io.NopCloser(strings.NewReader(body)), nil
							}
							return nil, fs.ErrNotExist
						},
						put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
							t.Error("unexpected cache write")
							return nil
						},
					},
				}
				rec := httptest.NewRecorder()
				g.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sumdb/sumdb.example.com/tile/2/0/000", nil))
				if got, want := rec.Code, http.StatusBadGateway; got != want {
					t.Errorf("got status %d, want %d", got, want)
				}
				if got, want := rec.Body.String(), "bad gateway"; got != want {
					t.Errorf("got content %q, want %q", got, want)
				}
				if got, want := rec.Header().Get("Cache-Control"), "no-store"; got != want {
					t.Errorf("got cache control %q, want %q", got, want)
				}
				if entries, err := os.ReadDir(g.TempDir); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if len(entries) != 0 {
					t.Errorf("unexpected temporary files %v", entries)
				}
			})
		}
	})

	t.Run("ResponseBodies", func(t *testing.T) {
		for _, tt := range []struct {
			name            string
			path            string
			body            string
			valid           bool
			wantContentType string
		}{
			{"Latest", "/latest", "latest", true, "text/plain; charset=utf-8"},
			{"EmptyLatest", "/latest", "", false, "text/plain; charset=utf-8"},
			{"Lookup", "/lookup/example.com@v1.0.0", "lookup", true, "text/plain; charset=utf-8"},
			{"EmptyLookup", "/lookup/example.com@v1.0.0", "", false, "text/plain; charset=utf-8"},
			{"DataTile", "/tile/2/data/000", strings.Repeat("record\n\n", 4), true, "text/plain; charset=utf-8"},
			{"PartialDataTile", "/tile/2/data/000.p/1", strings.Repeat("record", 20) + "\n\n", true, "text/plain; charset=utf-8"},
			{"EmptyDataTile", "/tile/2/data/000", "", false, "text/plain; charset=utf-8"},
			{"EmptyPartialDataTile", "/tile/2/data/000.p/1", "", false, "text/plain; charset=utf-8"},
			{"HashTile", "/tile/2/0/000", strings.Repeat("x", 128), true, "application/octet-stream"},
			{"EmptyHashTile", "/tile/2/0/000", "", false, "application/octet-stream"},
			{"ShortHashTile", "/tile/2/0/000", strings.Repeat("x", 127), false, "application/octet-stream"},
			{"LongHashTile", "/tile/2/0/000", strings.Repeat("x", 129), false, "application/octet-stream"},
			{"PartialHashTile", "/tile/2/0/000.p/2", strings.Repeat("x", 64), true, "application/octet-stream"},
			{"EmptyPartialHashTile", "/tile/2/0/000.p/2", "", false, "application/octet-stream"},
			{"ShortPartialHashTile", "/tile/2/0/000.p/2", strings.Repeat("x", 63), false, "application/octet-stream"},
			{"LongPartialHashTile", "/tile/2/0/000.p/2", strings.Repeat("x", 65), false, "application/octet-stream"},
			{"FullTileForPartialTile", "/tile/2/0/000.p/2", strings.Repeat("x", 128), false, "application/octet-stream"},
			{"MaximumTileHeight", "/tile/30/0/000", "x", false, "application/octet-stream"},
		} {
			for _, mode := range []struct {
				name    string
				method  string
				chunked bool
				gzip    bool
			}{
				{"GET", http.MethodGet, false, false},
				{"HEAD", http.MethodHead, false, false},
				{"Chunked", http.MethodGet, true, false},
				{"Gzip", http.MethodGet, false, true},
			} {
				t.Run(tt.name+"/"+mode.name, func(t *testing.T) {
					sumdbServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
						switch {
						case mode.gzip:
							rw.Header().Set("Content-Encoding", "gzip")
							zw := gzip.NewWriter(rw)
							defer zw.Close()
							fmt.Fprint(zw, tt.body)
							return
						case mode.chunked:
							rw.(http.Flusher).Flush()
						default:
							rw.Header().Set("Content-Length", strconv.Itoa(len(tt.body)))
						}
						fmt.Fprint(rw, tt.body)
					}))
					g := &Goproxy{
						ProxiedSumDBs: []string{"sumdb.example.com " + sumdbServer.URL},
						TempDir:       t.TempDir(),
						Logger:        slog.New(slog.DiscardHandler),
						Cacher: &testCacher{
							get: func(context.Context, Cacher, string) (io.ReadCloser, error) {
								t.Error("unexpected cache read")
								return nil, errors.New("cannot get")
							},
							put: func(context.Context, Cacher, string, io.ReadSeeker) error {
								t.Error("unexpected cache write")
								return errors.New("cannot put")
							},
						},
					}
					rec := httptest.NewRecorder()
					g.ServeHTTP(rec, httptest.NewRequest(mode.method, "/sumdb/sumdb.example.com"+tt.path, nil))
					wantStatusCode, wantContent := http.StatusOK, tt.body
					wantContentType := tt.wantContentType
					wantCacheControl := "public, max-age=86400"
					if tt.path == "/latest" {
						wantCacheControl = "public, max-age=60"
					}
					if !tt.valid {
						wantStatusCode, wantContent = http.StatusBadGateway, "bad gateway"
						wantContentType = "text/plain; charset=utf-8"
						wantCacheControl = "no-store"
					}
					if mode.method == http.MethodHead {
						wantContent = ""
					}
					if got, want := rec.Code, wantStatusCode; got != want {
						t.Errorf("got status %d, want %d", got, want)
					}
					if got, want := rec.Header().Get("Content-Type"), wantContentType; got != want {
						t.Errorf("got content type %q, want %q", got, want)
					}
					if got, want := rec.Header().Get("Cache-Control"), wantCacheControl; got != want {
						t.Errorf("got cache control %q, want %q", got, want)
					}
					if got, want := rec.Body.String(), wantContent; got != want {
						t.Errorf("got content %q, want %q", got, want)
					}
					if entries, err := os.ReadDir(g.TempDir); err != nil {
						t.Errorf("unexpected error %v", err)
					} else if len(entries) != 0 {
						t.Errorf("unexpected temporary files %v", entries)
					}
				})
			}
		}
	})

	t.Run("Paths", func(t *testing.T) {
		var upstreamRequests atomic.Int32
		var upstreamPath atomic.Value
		sumdbServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			upstreamRequests.Add(1)
			upstreamPath.Store(req.URL.Path)
			if tile, err := tlog.ParseTilePath(strings.TrimPrefix(req.URL.Path, "/base/")); err == nil && tile.L >= 0 {
				fmt.Fprint(rw, strings.Repeat("x", tile.W*tlog.HashSize))
				return
			}
			fmt.Fprint(rw, req.URL.Path)
		}))
		for _, tt := range []struct {
			name  string
			path  string
			valid bool
		}{
			{"Lookup", "/lookup/example.com/project@v1.2.3", true},
			{"LookupEscapedModule", "/lookup/example.com/!project@v1.2.3", true},
			{"LookupEscapedVersion", "/lookup/example.com/project@v1.2.3-!r!c1", true},
			{"LookupMajorSuffix", "/lookup/example.com/project/v2@v2.0.0", true},
			{"LookupGopkgIn", "/lookup/gopkg.in/yaml.v3@v3.0.1", true},
			{"LookupIncompatible", "/lookup/example.com/project@v2.0.0+incompatible", true},
			{"LookupPseudoVersion", "/lookup/example.com/project@v0.0.0-20200101000000-0123456789ab", true},
			{"LookupToolchain", "/lookup/golang.org/toolchain@v0.0.1-go1.26.0.darwin-arm64", true},
			{"LookupURLEncoding", "/lookup/example.com/%21project%40v1.2.3", true},
			{"HashTile", "/tile/8/0/001", true},
			{"PartialHashTile", "/tile/8/0/001.p/10", true},
			{"DataTile", "/tile/8/data/001", true},
			{"PartialDataTile", "/tile/8/data/001.p/1", true},
			{"GroupedTile", "/tile/8/1/x001/234", true},
			{"MaximumTileHeight", "/tile/30/0/000.p/1", true},
			{"MaximumTileLevel", "/tile/1/63/000", true},
			{"TileURLEncoding", "/tile/%38/%30/%30%30%31", true},
			{"EmptyLookup", "/lookup/", false},
			{"LookupMissingVersion", "/lookup/example.com/project", false},
			{"LookupEmptyModule", "/lookup/@v1.2.3", false},
			{"LookupInvalidModule", "/lookup/invalid@v1.2.3", false},
			{"LookupUnescapedModule", "/lookup/example.com/Project@v1.2.3", false},
			{"LookupInvalidModuleEscape", "/lookup/example.com/project!@v1.2.3", false},
			{"LookupEmptyVersion", "/lookup/example.com/project@", false},
			{"LookupLatestVersion", "/lookup/example.com/project@latest", false},
			{"LookupBranchVersion", "/lookup/example.com/project@main", false},
			{"LookupShortVersion", "/lookup/example.com/project@v1.2", false},
			{"LookupBuildMetadata", "/lookup/example.com/project@v1.2.3+build", false},
			{"LookupMismatchedMajor", "/lookup/example.com/project/v2@v1.2.3", false},
			{"LookupMissingMajorSuffix", "/lookup/example.com/project@v2.0.0", false},
			{"LookupUnescapedVersion", "/lookup/example.com/project@v1.2.3-RC1", false},
			{"LookupInvalidVersionEscape", "/lookup/example.com/project@v1.2.3-!!rc", false},
			{"LookupMultipleVersions", "/lookup/example.com/project@v1.2.3@v1.2.4", false},
			{"LookupGoModSuffix", "/lookup/example.com/project@v1.2.3/go.mod", false},
			{"LookupBackslash", "/lookup/example.com/project%5Cextra@v1.2.3", false},
			{"LookupNUL", "/lookup/example.com/project@v1.2.3%00", false},
			{"LookupLiteralPercent", "/lookup/example.com/project%252fextra@v1.2.3", false},
			{"IncompleteTile", "/tile/8/0", false},
			{"InvalidTileNumber", "/tile/8/0/invalid", false},
			{"ZeroTileHeight", "/tile/0/0/001", false},
			{"ExcessiveTileHeight", "/tile/31/0/001", false},
			{"NegativeTileLevel", "/tile/8/-1/001", false},
			{"ExcessiveTileLevel", "/tile/8/64/001", false},
			{"LargeTileLevel", "/tile/8/2147483647/001", false},
			{"UnpaddedTileNumber", "/tile/8/0/1", false},
			{"InvalidTileGrouping", "/tile/8/0/x001/x234", false},
			{"EmptyPartialTile", "/tile/8/0/001.p/0", false},
			{"FullPartialTile", "/tile/8/0/001.p/256", false},
			{"OversizedPartialTile", "/tile/8/0/001.p/257", false},
			{"OverflowingTileNumber", "/tile/8/0/x999/x999/x999/x999/x999/x999/x999/999", false},
			{"TileBackslash", "/tile/8/0/001%5Cextra", false},
			{"TileNUL", "/tile/8/0/001%00", false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					t.Run(method, func(t *testing.T) {
						req := httptest.NewRequest(method, "/sumdb/sumdb.example.com"+tt.path, nil)
						wantPath := "/base" + strings.TrimPrefix(req.URL.Path, "/sumdb/sumdb.example.com")
						wantContent := wantPath
						if tt.valid {
							if tile, err := tlog.ParseTilePath(strings.TrimPrefix(wantPath, "/base/")); err == nil && tile.L >= 0 {
								wantContent = strings.Repeat("x", tile.W*tlog.HashSize)
							}
						}
						g := &Goproxy{
							ProxiedSumDBs: []string{"sumdb.example.com " + sumdbServer.URL + "/base"},
							Cacher: &testCacher{
								get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
									t.Errorf("unexpected cache read %q", name)
									return nil, fs.ErrNotExist
								},
								put: func(context.Context, Cacher, string, io.ReadSeeker) error {
									t.Error("unexpected cache write")
									return errors.New("cannot put")
								},
							},
							TempDir: t.TempDir(),
							Logger:  slog.New(slog.DiscardHandler),
						}
						rec := httptest.NewRecorder()
						g.ServeHTTP(rec, req)
						wantStatusCode, wantCalls := http.StatusNotFound, 0
						if tt.valid {
							wantStatusCode, wantCalls = http.StatusOK, 1
							if got, want := upstreamPath.Load(), wantPath; got != want {
								t.Errorf("got upstream path %q, want %q", got, want)
							}
						} else {
							wantContent = "not found"
						}
						if got, want := rec.Code, wantStatusCode; got != want {
							t.Errorf("got status %d, want %d", got, want)
						}
						if got, want := rec.Header().Get("Cache-Control"), "public, max-age=86400"; got != want {
							t.Errorf("got cache control %q, want %q", got, want)
						}
						if method == http.MethodHead {
							wantContent = ""
						}
						if got, want := rec.Body.String(), wantContent; got != want {
							t.Errorf("got content %q, want %q", got, want)
						}
						if got, want := int(upstreamRequests.Swap(0)), wantCalls; got != want {
							t.Errorf("got %d upstream requests, want %d", got, want)
						}
					})
				}
			})
		}
	})

	t.Run("InvalidPathsWithoutTempDir", func(t *testing.T) {
		g := &Goproxy{
			ProxiedSumDBs: []string{"sumdb.example.com"},
			TempDir:       filepath.Join(t.TempDir(), "404"),
			Logger:        slog.New(slog.DiscardHandler),
		}
		for _, path := range []string{"/lookup/example.com@main", "/tile/8/0/1"} {
			rec := httptest.NewRecorder()
			g.ServeHTTP(rec, httptest.NewRequest("", "/sumdb/sumdb.example.com"+path, nil))
			if got, want := rec.Code, http.StatusNotFound; got != want {
				t.Errorf("path %q: got status %d, want %d", path, got, want)
			}
		}
	})

	for _, tt := range []struct {
		name             string
		sumdbHandler     http.HandlerFunc
		tempDir          string
		target           string
		wantStatusCode   int
		wantContentType  string
		wantCacheControl string
		wantContent      string
	}{
		{
			name:             "Supported",
			target:           "sumdb/sumdb.example.com/supported",
			wantStatusCode:   http.StatusOK,
			wantCacheControl: "public, max-age=86400",
		},
		{
			name:             "Latest",
			target:           "sumdb/sumdb.example.com/latest",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "/latest",
		},
		{
			name:             "Lookup",
			target:           "sumdb/sumdb.example.com/lookup/example.com@v1.0.0",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "/lookup/example.com@v1.0.0",
		},
		{
			name:             "HashTile",
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, strings.Repeat("x", 128)) },
			target:           "sumdb/sumdb.example.com/tile/2/0/000",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/octet-stream",
			wantCacheControl: "public, max-age=86400",
			wantContent:      strings.Repeat("x", 128),
		},
		{
			name:             "NotFound",
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			target:           "sumdb/sumdb.example.com/latest",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			name:             "MissingPath",
			target:           "sumdb/sumdb.example.com",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			name:             "UnknownPath",
			target:           "sumdb/sumdb.example.com/404",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			name:             "UnknownDatabase",
			target:           "sumdb/sumdb2.example.com/supported",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			name:             "InvalidTarget",
			target:           "://invalid",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			name:             "TempDirError",
			tempDir:          filepath.Join(t.TempDir(), "404"),
			target:           "sumdb/sumdb.example.com/latest",
			wantStatusCode:   http.StatusInternalServerError,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "no-store",
			wantContent:      "internal server error",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.sumdbHandler == nil {
				tt.sumdbHandler = func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, req.URL.Path) }
			}
			sumdbServer := newHTTPTestServer(t, tt.sumdbHandler)
			if tt.tempDir == "" {
				tt.tempDir = t.TempDir()
			}

			g := &Goproxy{
				ProxiedSumDBs: []string{"sumdb.example.com " + sumdbServer.URL},
				TempDir:       tt.tempDir,
				Logger:        slog.New(slog.DiscardHandler),
			}
			g.initOnce.Do(g.init)

			rec := httptest.NewRecorder()
			g.serveSumDB(rec, httptest.NewRequest("", "/", nil), tt.target)
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestGoproxyServeCache(t *testing.T) {
	for _, tt := range []struct {
		name           string
		cacher         Cacher
		wantStatusCode int
		wantContent    string
	}{
		{
			name: "Hit",
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("foobar")), nil
				},
			},
			wantStatusCode: http.StatusOK,
			wantContent:    "foobar",
		},
		{
			name:           "Miss",
			wantStatusCode: http.StatusNotFound,
			wantContent:    "not found: temporarily unavailable",
		},
		{
			name: "Error",
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return nil, errors.New("cannot get")
				},
			},
			wantStatusCode: http.StatusInternalServerError,
			wantContent:    "internal server error",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cacher == nil {
				tt.cacher = DirCacher(t.TempDir())
			}

			g := &Goproxy{
				Cacher:  tt.cacher,
				TempDir: t.TempDir(),
				Logger:  slog.New(slog.DiscardHandler),
			}
			g.initOnce.Do(g.init)

			req := httptest.NewRequest("", "/", nil)
			rec := httptest.NewRecorder()
			g.serveCache(rec, req, "target", "", -2)
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestGoproxyCache(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		cacheDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(cacheDir, "foo"), []byte("bar"), 0o644); err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		g := &Goproxy{Cacher: DirCacher(cacheDir), TempDir: t.TempDir()}
		g.initOnce.Do(g.init)

		rc, err := g.cache(t.Context(), "foo")
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		if got, want := string(b), "bar"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("NonExistentCache", func(t *testing.T) {
		g := &Goproxy{Cacher: DirCacher(t.TempDir()), TempDir: t.TempDir()}
		g.initOnce.Do(g.init)

		_, err := g.cache(t.Context(), "foo")
		if err == nil {
			t.Fatal("expected error")
		}
		if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("NoCacher", func(t *testing.T) {
		g := &Goproxy{TempDir: t.TempDir()}
		g.initOnce.Do(g.init)

		_, err := g.cache(t.Context(), "")
		if err == nil {
			t.Fatal("expected error")
		}
		if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestGoproxyPutCache(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		cacheDir := t.TempDir()
		g := &Goproxy{Cacher: DirCacher(cacheDir), TempDir: t.TempDir()}
		g.initOnce.Do(g.init)

		if err := g.putCache(t.Context(), "foo", strings.NewReader("bar")); err != nil {
			t.Fatalf("unexpected error %v", err)
		}

		b, err := os.ReadFile(filepath.Join(cacheDir, "foo"))
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		if got, want := string(b), "bar"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("NoCacher", func(t *testing.T) {
		g := &Goproxy{TempDir: t.TempDir()}
		g.initOnce.Do(g.init)

		if err := g.putCache(t.Context(), "foo", strings.NewReader("bar")); err != nil {
			t.Errorf("unexpected error %v", err)
		}
	})
}

func TestCleanPath(t *testing.T) {
	for _, tt := range []struct {
		n        int
		path     string
		wantPath string
	}{
		{1, "", "/"},
		{2, ".", "/"},
		{3, "..", "/"},
		{4, "/.", "/"},
		{5, "/..", "/"},
		{6, "//", "/"},
		{7, "/foo//bar", "/foo/bar"},
		{8, "/foo//bar/", "/foo/bar/"},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if got, want := cleanPath(tt.path), tt.wantPath; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func getenv(env []string, key string) string {
	for _, e := range slices.Backward(env) {
		if k, v, ok := strings.Cut(e, "="); ok {
			if k == key {
				return v
			}
		}
	}
	return ""
}

func compareErrors(got, want error) bool {
	if want != fs.ErrNotExist && errors.Is(want, fs.ErrNotExist) {
		return errors.Is(got, fs.ErrNotExist) && got.Error() == want.Error()
	}
	return errors.Is(got, want) || got.Error() == want.Error()
}

type testUnwrapResponseWriter struct{ http.ResponseWriter }

func (rw testUnwrapResponseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

func newHTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	server := httptest.NewServer(handler)
	addr := server.Listener.Addr().String()
	dialableTCPAddrs.Store(addr, struct{}{})
	t.Cleanup(func() {
		dialableTCPAddrs.Delete(addr)
		server.Close()
	})
	return server
}

func makeTempFile(t *testing.T, content []byte) (tempFile string, err error) {
	f, err := os.CreateTemp(t.TempDir(), "")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func makeZip(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for k, v := range files {
		w, err := zw.Create(k)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(v); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type testReadSeeker struct {
	io.ReadSeeker
	read func(rs io.ReadSeeker, p []byte) (n int, err error)
	seek func(rs io.ReadSeeker, offset int64, whence int) (int64, error)
}

func (rs *testReadSeeker) Read(p []byte) (n int, err error) {
	if rs.read != nil {
		return rs.read(rs.ReadSeeker, p)
	}
	return rs.ReadSeeker.Read(p)
}

func (rs *testReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if rs.seek != nil {
		return rs.seek(rs.ReadSeeker, offset, whence)
	}
	return rs.ReadSeeker.Seek(offset, whence)
}

type testCacher struct {
	Cacher
	get func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error)
	put func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error
}

func (c *testCacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if c.get != nil {
		return c.get(ctx, c.Cacher, name)
	}
	return c.Cacher.Get(ctx, name)
}

func (c *testCacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	if c.put != nil {
		return c.put(ctx, c.Cacher, name, content)
	}
	return c.Cacher.Put(ctx, name, content)
}
