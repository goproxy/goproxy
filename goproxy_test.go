package goproxy

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/mod/module"
)

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
		t.Fatal("unexpected nil")
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
		t.Fatal("unexpected nil")
	} else if got := g.httpClient.Transport; got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}

func TestGoproxyServeHTTP(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	setProxyHandler(func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
	})
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
		g := &Goproxy{
			Fetcher: &GoFetcher{
				Env:     []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
				TempDir: t.TempDir(),
			},
			Cacher:  DirCacher(t.TempDir()),
			TempDir: t.TempDir(),
			Logger:  slog.New(slogDiscardHandler{}),
		}
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
		recr := rec.Result()
		if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestGoproxyServeFetch(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	list := "v1.0.0"
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)})
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	setProxyHandler(func(rw http.ResponseWriter, req *http.Request) {
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
			Logger:  slog.New(slogDiscardHandler{}),
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
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestGoproxyServeFetchQuery(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
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
		if tt.proxyHandler == nil {
			tt.proxyHandler = proxyHandler
		}
		setProxyHandler(tt.proxyHandler)
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
			Logger:  slog.New(slogDiscardHandler{}),
		}
		g.initOnce.Do(g.init)
		rec := httptest.NewRecorder()
		g.serveFetchQuery(rec, httptest.NewRequest("", "/", nil), "example.com/@latest", "example.com", "latest", tt.noFetch)
		recr := rec.Result()
		if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestGoproxyServeFetchList(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
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
		if tt.proxyHandler == nil {
			tt.proxyHandler = proxyHandler
		}
		setProxyHandler(tt.proxyHandler)
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
			Logger:  slog.New(slogDiscardHandler{}),
		}
		g.initOnce.Do(g.init)
		rec := httptest.NewRecorder()
		g.serveFetchList(rec, httptest.NewRequest("", "/", nil), "example.com/@v/list", "example.com", tt.noFetch)
		recr := rec.Result()
		if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestGoproxyServeFetchDownload(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)})
	if err != nil {
		t.Fatalf("unexpected error %q", err)
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
		if tt.proxyHandler == nil {
			tt.proxyHandler = proxyHandler
		}
		setProxyHandler(tt.proxyHandler)
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
			Logger:  slog.New(slogDiscardHandler{}),
		}
		g.initOnce.Do(g.init)
		escapedModulePath, after, ok := strings.Cut(tt.target, "/@v/")
		if !ok {
			t.Fatalf("test(%d): invalid target %q", tt.n, tt.target)
		}
		modulePath, err := module.UnescapePath(escapedModulePath)
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		moduleVersion, err := module.UnescapeVersion(strings.TrimSuffix(after, path.Ext(after)))
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		rec := httptest.NewRecorder()
		g.serveFetchDownload(rec, httptest.NewRequest("", "/", nil), tt.target, modulePath, moduleVersion, tt.noFetch)
		recr := rec.Result()
		if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestGoproxyServeSumDB(t *testing.T) {
	sumdbServer, setSumDBHandler := newHTTPTestServer()
	defer sumdbServer.Close()
	sumdbHandler := func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, req.URL.Path) }
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
			target:           "sumdb/sumdb.example.com/tile/2/0/0",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/octet-stream",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "/tile/2/0/0",
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
		if tt.sumdbHandler == nil {
			tt.sumdbHandler = sumdbHandler
		}
		setSumDBHandler(tt.sumdbHandler)
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
			Logger:        slog.New(slogDiscardHandler{}),
		}
		g.initOnce.Do(g.init)
		rec := httptest.NewRecorder()
		g.serveSumDB(rec, httptest.NewRequest("", "/", nil), tt.target)
		recr := rec.Result()
		if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), tt.wantContentType; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
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
		if tt.cacher == nil {
			tt.cacher = DirCacher(t.TempDir())
		}
		g := &Goproxy{
			Cacher:  tt.cacher,
			TempDir: t.TempDir(),
			Logger:  slog.New(slogDiscardHandler{}),
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
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
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
		g := &Goproxy{
			Cacher:  DirCacher(t.TempDir()),
			TempDir: t.TempDir(),
			Logger:  slog.New(slogDiscardHandler{}),
		}
		g.initOnce.Do(g.init)
		rec := httptest.NewRecorder()
		g.servePutCache(rec, httptest.NewRequest("", "/", nil), "target", "", -2, tt.content)
		recr := rec.Result()
		if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
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
		g := &Goproxy{
			Cacher:  DirCacher(t.TempDir()),
			TempDir: t.TempDir(),
			Logger:  slog.New(slogDiscardHandler{}),
		}
		g.initOnce.Do(g.init)
		rec := httptest.NewRecorder()
		file, err := tt.createFile()
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		g.servePutCacheFile(rec, httptest.NewRequest("", "/", nil), "target", "", -2, file)
		recr := rec.Result()
		if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestGoproxyCache(t *testing.T) {
	dc := DirCacher(t.TempDir())
	g := &Goproxy{Cacher: dc, TempDir: t.TempDir()}
	g.initOnce.Do(g.init)
	if err := os.WriteFile(filepath.Join(string(dc), "foo"), []byte("bar"), 0o644); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if rc, err := g.cache(context.Background(), "foo"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if b, err := io.ReadAll(rc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := rc.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := g.cache(context.Background(), "bar"); err == nil {
		t.Fatal("expected error")
	} else if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{TempDir: t.TempDir()}
	g.initOnce.Do(g.init)
	if _, err := g.cache(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	} else if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyPutCache(t *testing.T) {
	dc := DirCacher(t.TempDir())
	g := &Goproxy{Cacher: dc, TempDir: t.TempDir()}
	g.initOnce.Do(g.init)
	if err := g.putCache(context.Background(), "foo", strings.NewReader("bar")); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if b, err := os.ReadFile(filepath.Join(string(dc), "foo")); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{TempDir: t.TempDir()}
	g.initOnce.Do(g.init)
	if err := g.putCache(context.Background(), "foo", strings.NewReader("bar")); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
}

func TestGoproxyPutCacheFile(t *testing.T) {
	dc := DirCacher(t.TempDir())
	g := &Goproxy{Cacher: dc, TempDir: t.TempDir()}
	g.initOnce.Do(g.init)

	cacheFile := filepath.Join(string(dc), "cache")
	if err := os.WriteFile(cacheFile, []byte("bar"), 0o644); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if err := g.putCacheFile(context.Background(), "foo", cacheFile); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if b, err := os.ReadFile(filepath.Join(string(dc), "foo")); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := g.putCacheFile(context.Background(), "bar", filepath.Join(string(dc), "bar-sourcel")); err == nil {
		t.Fatal("expected error")
	} else if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
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
		if got, want := cleanPath(tt.path), tt.wantPath; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func compareErrors(got, want error) bool {
	if want != fs.ErrNotExist && errors.Is(want, fs.ErrNotExist) {
		return errors.Is(got, fs.ErrNotExist) && got.Error() == want.Error()
	}
	return errors.Is(got, want) || got.Error() == want.Error()
}

func newHTTPTestServer() (server *httptest.Server, setHandler func(http.HandlerFunc)) {
	var (
		handler      http.HandlerFunc
		handlerMutex sync.Mutex
	)
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			handlerMutex.Lock()
			handler(rw, req)
			handlerMutex.Unlock()
		})),
		func(h http.HandlerFunc) {
			handlerMutex.Lock()
			handler = h
			handlerMutex.Unlock()
		}
}

func makeTempFile(t *testing.T, content []byte) (tempFile string, err error) {
	f, err := os.CreateTemp(t.TempDir(), "")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(content); err != nil {
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

// slogDiscardHandler implements [slog.Handler] by discarding all logs.
//
// TODO: Remove slogDiscardHandler when the minimum supported Go version is
// 1.24. See https://go.dev/doc/go1.24#logslogpkglogslog.
type slogDiscardHandler struct{}

func (h slogDiscardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (h slogDiscardHandler) Handle(context.Context, slog.Record) error { return nil }
func (h slogDiscardHandler) WithAttrs([]slog.Attr) slog.Handler        { return h }
func (h slogDiscardHandler) WithGroup(string) slog.Handler             { return h }
