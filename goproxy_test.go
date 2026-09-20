package goproxy

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/tlog"
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
}

func TestGoproxyServeHTTP(t *testing.T) {
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
	}))
	for _, tt := range []struct {
		n                int
		method           string
		path             string
		wantStatusCode   int
		wantContentType  string
		wantCacheControl string
		wantContent      string
	}{
		{
			n:                1,
			path:             "/example.com/@latest",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n:                2,
			method:           http.MethodHead,
			path:             "/example.com/@latest",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
		},
		{
			n:                3,
			method:           http.MethodPost,
			path:             "/example.com/@latest",
			wantStatusCode:   http.StatusMethodNotAllowed,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
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
			wantCacheControl: "public, max-age=86400",
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

func TestGoproxyServeFetch(t *testing.T) {
	list := "v1.0.0"
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
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
			n: 2,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader(info)), nil
				},
			},
			target:             "example.com/@latest",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusOK,
			wantContentType:    "application/json; charset=utf-8",
			wantCacheControl:   "public, max-age=60",
			wantContent:        info,
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
			n: 4,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader(list)), nil
				},
			},
			target:             "example.com/@v/list",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusOK,
			wantContentType:    "text/plain; charset=utf-8",
			wantCacheControl:   "public, max-age=60",
			wantContent:        list,
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
		n                int
		proxyHandler     http.HandlerFunc
		cacher           Cacher
		noFetch          bool
		wantStatusCode   int
		wantContentType  string
		wantCacheControl string
		wantContent      string
	}{
		{
			n:                1,
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
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
			noFetch:          true,
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n:                3,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
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

			rec := httptest.NewRecorder()
			g.serveFetchQuery(rec, httptest.NewRequest("", "/", nil), "example.com/@latest", "example.com", "latest", tt.noFetch)
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
		n              int
		proxyHandler   http.HandlerFunc
		cacher         Cacher
		noFetch        bool
		wantStatusCode int
		wantContent    string
	}{
		{
			n:              1,
			wantStatusCode: http.StatusOK,
			wantContent:    list,
		},
		{
			n: 2,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader(list)), nil
				},
			},
			noFetch:        true,
			wantStatusCode: http.StatusOK,
			wantContent:    list,
		},
		{
			n:              3,
			proxyHandler:   func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			wantStatusCode: http.StatusNotFound,
			wantContent:    "not found",
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

			rec := httptest.NewRecorder()
			g.serveFetchList(rec, httptest.NewRequest("", "/", nil), "example.com/@v/list", "example.com", tt.noFetch)
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
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
			wantCacheControl: "public, max-age=600",
			wantContent:      "not found",
		},
		{
			n:                7,
			target:           "example.com/@v/v1.0.0.info",
			noFetch:          true,
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
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
			target:          "example.com/@v/v1.0.0.info",
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
		{
			n: 9,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
					return errors.New("cannot put")
				},
			},
			target:          "example.com/@v/v1.0.0.info",
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
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
			target:          "example.com/@v/v1.0.0.mod",
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
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
				cacheReads := 0
				g := &Goproxy{
					ProxiedSumDBs: []string{"sumdb.example.com " + server.URL},
					TempDir:       t.TempDir(),
					Logger:        slog.New(slog.DiscardHandler),
					Cacher: &testCacher{
						get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
							cacheReads++
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
				wantStatusCode, wantContent, wantCacheControl := http.StatusInternalServerError, "internal server error", ""
				if tt.cached {
					wantStatusCode, wantContent, wantCacheControl = http.StatusOK, body, "public, max-age=86400"
				}
				if got, want := rec.Code, wantStatusCode; got != want {
					t.Errorf("got status %d, want %d", got, want)
				}
				if got, want := rec.Body.String(), wantContent; got != want {
					t.Errorf("got content %q, want %q", got, want)
				}
				if got, want := rec.Header().Get("Cache-Control"), wantCacheControl; got != want {
					t.Errorf("got cache control %q, want %q", got, want)
				}
				if got, want := cacheReads, 1; got != want {
					t.Errorf("got %d cache reads, want %d", got, want)
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
				cached  bool
				chunked bool
				gzip    bool
			}{
				{"GET", http.MethodGet, false, false, false},
				{"HEAD", http.MethodHead, false, false, false},
				{"Cached", http.MethodGet, true, false, false},
				{"CachedHEAD", http.MethodHead, true, false, false},
				{"Chunked", http.MethodGet, false, true, false},
				{"Gzip", http.MethodGet, false, false, true},
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
					cacheReads, cacheWrites := 0, 0
					g := &Goproxy{
						ProxiedSumDBs: []string{"sumdb.example.com " + sumdbServer.URL},
						TempDir:       t.TempDir(),
						Logger:        slog.New(slog.DiscardHandler),
						Cacher: &testCacher{
							get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
								cacheReads++
								if mode.cached {
									return io.NopCloser(strings.NewReader("cached")), nil
								}
								return nil, fs.ErrNotExist
							},
							put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
								cacheWrites++
								if b, err := io.ReadAll(content); err != nil {
									t.Errorf("unexpected error %v", err)
								} else if got, want := string(b), tt.body; got != want {
									t.Errorf("got cached content %q, want %q", got, want)
								}
								return nil
							},
						},
					}
					rec := httptest.NewRecorder()
					g.ServeHTTP(rec, httptest.NewRequest(mode.method, "/sumdb/sumdb.example.com"+tt.path, nil))
					wantStatusCode, wantContent := http.StatusOK, tt.body
					wantContentType := tt.wantContentType
					wantCacheControl := "public, max-age=86400"
					if tt.path == "/latest" {
						wantCacheControl = "public, max-age=3600"
					}
					wantReads, wantWrites := 0, 1
					if !tt.valid {
						wantReads, wantWrites = 1, 0
						if mode.cached {
							wantContent = "cached"
						} else {
							wantStatusCode, wantContent = http.StatusNotFound, "not found: bad upstream"
							wantContentType = "text/plain; charset=utf-8"
							wantCacheControl = "must-revalidate, no-cache, no-store"
						}
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
					if got, want := cacheReads, wantReads; got != want {
						t.Errorf("got %d cache reads, want %d", got, want)
					}
					if got, want := cacheWrites, wantWrites; got != want {
						t.Errorf("got %d cache writes, want %d", got, want)
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
						cacheWrites := 0
						g := &Goproxy{
							ProxiedSumDBs: []string{"sumdb.example.com " + sumdbServer.URL + "/base"},
							Cacher: &testCacher{
								get: func(ctx context.Context, c Cacher, name string) (io.ReadCloser, error) {
									t.Errorf("unexpected cache read %q", name)
									return nil, fs.ErrNotExist
								},
								put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
									cacheWrites++
									if got, want := name, strings.TrimPrefix(req.URL.Path, "/"); got != want {
										t.Errorf("got cache name %q, want %q", got, want)
									}
									if b, err := io.ReadAll(content); err != nil {
										t.Errorf("unexpected error %v", err)
									} else if got, want := string(b), wantContent; got != want {
										t.Errorf("got cached content %q, want %q", got, want)
									}
									return nil
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
						if got, want := cacheWrites, wantCalls; got != want {
							t.Errorf("got %d cache writes, want %d", got, want)
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
		n                int
		sumdbHandler     http.HandlerFunc
		cacher           Cacher
		tempDir          string
		target           string
		wantStatusCode   int
		wantContentType  string
		wantCacheControl string
		wantContent      string
	}{
		{
			n:                1,
			target:           "sumdb/sumdb.example.com/supported",
			wantStatusCode:   http.StatusOK,
			wantCacheControl: "public, max-age=86400",
		},
		{
			n:                2,
			target:           "sumdb/sumdb.example.com/latest",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=3600",
			wantContent:      "/latest",
		},
		{
			n:                3,
			target:           "sumdb/sumdb.example.com/lookup/example.com@v1.0.0",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "/lookup/example.com@v1.0.0",
		},
		{
			n:                4,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, strings.Repeat("x", 128)) },
			target:           "sumdb/sumdb.example.com/tile/2/0/000",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/octet-stream",
			wantCacheControl: "public, max-age=86400",
			wantContent:      strings.Repeat("x", 128),
		},
		{
			n:                5,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			target:           "sumdb/sumdb.example.com/latest",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			n: 6,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
					return errors.New("cannot put")
				},
			},
			target:          "sumdb/sumdb.example.com/latest",
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
		{
			n:                7,
			target:           "sumdb/sumdb.example.com",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                8,
			target:           "sumdb/sumdb.example.com/404",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                9,
			target:           "sumdb/sumdb2.example.com/supported",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                10,
			target:           "://invalid",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:               11,
			tempDir:         filepath.Join(t.TempDir(), "404"),
			target:          "sumdb/sumdb.example.com/latest",
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.sumdbHandler == nil {
				tt.sumdbHandler = func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, req.URL.Path) }
			}
			sumdbServer := newHTTPTestServer(t, tt.sumdbHandler)
			if tt.cacher == nil {
				tt.cacher = DirCacher(t.TempDir())
			}
			if tt.tempDir == "" {
				tt.tempDir = t.TempDir()
			}

			g := &Goproxy{
				ProxiedSumDBs: []string{"sumdb.example.com " + sumdbServer.URL},
				Cacher:        tt.cacher,
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
		n              int
		cacher         Cacher
		onNotFound     func(rw http.ResponseWriter, req *http.Request)
		wantStatusCode int
		wantContent    string
	}{
		{
			n: 1,
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
			n:              2,
			onNotFound:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			wantStatusCode: http.StatusNotFound,
			wantContent:    "not found",
		},
		{
			n:              3,
			wantStatusCode: http.StatusNotFound,
			wantContent:    "not found: temporarily unavailable",
		},
		{
			n: 4,
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
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
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
			var onNotFound func()
			if tt.onNotFound != nil {
				onNotFound = func() { tt.onNotFound(rec, req) }
			}
			g.serveCache(rec, req, "target", "", -2, onNotFound)
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

func TestGoproxyServePutCache(t *testing.T) {
	for _, tt := range []struct {
		n              int
		content        io.ReadSeeker
		wantStatusCode int
		wantContent    string
	}{
		{
			n:              1,
			content:        strings.NewReader("foobar"),
			wantStatusCode: http.StatusOK,
			wantContent:    "foobar",
		},
		{
			n: 2,
			content: &testReadSeeker{
				ReadSeeker: strings.NewReader("foobar"),
				read: func(rs io.ReadSeeker, p []byte) (n int, err error) {
					return 0, errors.New("cannot read")
				},
			},
			wantStatusCode: http.StatusInternalServerError,
			wantContent:    "internal server error",
		},
		{
			n: 3,
			content: &testReadSeeker{
				ReadSeeker: strings.NewReader("foobar"),
				seek: func(rs io.ReadSeeker, offset int64, whence int) (int64, error) {
					return 0, errors.New("cannot seek")
				},
			},
			wantStatusCode: http.StatusInternalServerError,
			wantContent:    "internal server error",
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			g := &Goproxy{
				Cacher:  DirCacher(t.TempDir()),
				TempDir: t.TempDir(),
				Logger:  slog.New(slog.DiscardHandler),
			}
			g.initOnce.Do(g.init)

			rec := httptest.NewRecorder()
			g.servePutCache(rec, httptest.NewRequest("", "/", nil), "target", "", -2, tt.content)
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

func TestGoproxyServePutCacheFile(t *testing.T) {
	for _, tt := range []struct {
		n              int
		createFile     func() (string, error)
		wantStatusCode int
		wantContent    string
	}{
		{
			n:              1,
			createFile:     func() (string, error) { return makeTempFile(t, []byte("foobar")) },
			wantStatusCode: http.StatusOK,
			wantContent:    "foobar",
		},
		{
			n:              2,
			createFile:     func() (string, error) { return filepath.Join(t.TempDir(), "404"), nil },
			wantStatusCode: http.StatusInternalServerError,
			wantContent:    "internal server error",
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			g := &Goproxy{
				Cacher:  DirCacher(t.TempDir()),
				TempDir: t.TempDir(),
				Logger:  slog.New(slog.DiscardHandler),
			}
			g.initOnce.Do(g.init)

			rec := httptest.NewRecorder()
			file, err := tt.createFile()
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			g.servePutCacheFile(rec, httptest.NewRequest("", "/", nil), "target", "", -2, file)
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

func TestGoproxyPutCacheFile(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		cacheDir := t.TempDir()
		g := &Goproxy{Cacher: DirCacher(cacheDir), TempDir: t.TempDir()}
		g.initOnce.Do(g.init)

		srcFile := filepath.Join(t.TempDir(), "foo-source")
		if err := os.WriteFile(srcFile, []byte("bar"), 0o644); err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		if err := g.putCacheFile(t.Context(), "foo", srcFile); err != nil {
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

	t.Run("NonExistentSource", func(t *testing.T) {
		g := &Goproxy{Cacher: DirCacher(t.TempDir()), TempDir: t.TempDir()}
		g.initOnce.Do(g.init)

		err := g.putCacheFile(t.Context(), "foo", filepath.Join(t.TempDir(), "foo-source"))
		if err == nil {
			t.Fatal("expected error")
		}
		if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
			t.Errorf("got %v, want %v", got, want)
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
