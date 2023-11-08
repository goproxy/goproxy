package goproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func getenv(env []string, key string) string {
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			if k == key {
				return v
			}
		}
	}
	return ""
}

func TestGoproxyInit(t *testing.T) {
	for _, key := range []string{
		"GO111MODULE",
		"GOPROXY",
		"GONOPROXY",
		"GOSUMDB",
		"GONOSUMDB",
		"GOPRIVATE",
	} {
		t.Setenv(key, "")
	}

	for _, tt := range []struct {
		n                int
		env              []string
		wantEnvGOPROXY   string
		wantEnvGONOPROXY string
		wantEnvGOSUMDB   string
		wantEnvGONOSUMDB string
	}{
		{
			n:              1,
			wantEnvGOPROXY: defaultEnvGOPROXY,
			wantEnvGOSUMDB: defaultEnvGOSUMDB,
		},
		{
			n:              2,
			env:            append(os.Environ(), "GOPROXY=https://example.com"),
			wantEnvGOPROXY: "https://example.com",
			wantEnvGOSUMDB: defaultEnvGOSUMDB,
		},
		{
			n:              3,
			env:            append(os.Environ(), "GOSUMDB=example.com"),
			wantEnvGOPROXY: defaultEnvGOPROXY,
			wantEnvGOSUMDB: "example.com",
		},
		{
			n:                4,
			env:              append(os.Environ(), "GOPRIVATE=example.com"),
			wantEnvGOPROXY:   defaultEnvGOPROXY,
			wantEnvGONOPROXY: "example.com",
			wantEnvGOSUMDB:   defaultEnvGOSUMDB,
			wantEnvGONOSUMDB: "example.com",
		},
		{
			n: 5,
			env: append(
				os.Environ(),
				"GOPRIVATE=example.com",
				"GONOPROXY=alt1.example.com",
				"GONOSUMDB=alt2.example.com",
			),
			wantEnvGOPROXY:   defaultEnvGOPROXY,
			wantEnvGONOPROXY: "alt1.example.com",
			wantEnvGOSUMDB:   defaultEnvGOSUMDB,
			wantEnvGONOSUMDB: "alt2.example.com",
		},
	} {
		g := &Goproxy{Env: tt.env}
		g.init()
		if got, want := g.goBinName, "go"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := getenv(g.env, "PATH"), os.Getenv("PATH"); got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := g.envGOPROXY, tt.wantEnvGOPROXY; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := g.envGONOPROXY, tt.wantEnvGONOPROXY; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := g.envGOSUMDB, tt.wantEnvGOSUMDB; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := g.envGONOSUMDB, tt.wantEnvGONOSUMDB; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := getenv(g.env, "GOPRIVATE"), ""; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}

	g := &Goproxy{MaxDirectFetches: 1}
	g.init()
	if g.directFetchWorkerPool == nil {
		t.Fatal("unexpected nil")
	}

	g = &Goproxy{ProxiedSumDBs: []string{
		"sum.golang.google.cn",
		defaultEnvGOSUMDB + " https://sum.golang.google.cn",
		"",
		"example.com ://invalid",
	}}
	g.init()
	if got, want := len(g.proxiedSumDBs), 2; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if got, want := g.proxiedSumDBs["sum.golang.google.cn"].String(), "https://sum.golang.google.cn"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.proxiedSumDBs[defaultEnvGOSUMDB].String(), "https://sum.golang.google.cn"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := g.proxiedSumDBs["example.com"]; got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}

func TestGoproxyServeHTTP(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	for _, tt := range []struct {
		n                int
		proxyHandler     http.HandlerFunc
		method           string
		path             string
		pathPrefix       string
		tempDir          string
		wantStatusCode   int
		wantContentType  string
		wantCacheControl string
		wantContent      string
	}{
		{
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
			},
			path:             "/example.com/@latest",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n: 2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
			},
			method:           http.MethodHead,
			path:             "/example.com/@latest",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
		},
		{
			n:                3,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			method:           http.MethodPost,
			path:             "/example.com/@latest",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusMethodNotAllowed,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "method not allowed",
		},
		{
			n:                4,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			path:             "/",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                5,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			path:             "/.",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                6,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			path:             "/../example.com/@latest",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                7,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			path:             "/example.com/@v/list/",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                8,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			path:             "/sumdb/sumdb.example.com/supported",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:               9,
			proxyHandler:    func(rw http.ResponseWriter, req *http.Request) {},
			path:            "/example.com/@latest",
			tempDir:         filepath.Join(t.TempDir(), "404"),
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
	} {
		setProxyHandler(tt.proxyHandler)
		g := &Goproxy{
			Env:         []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
			Cacher:      DirCacher(t.TempDir()),
			TempDir:     tt.tempDir,
			ErrorLogger: log.New(io.Discard, "", 0),
		}
		g.init()
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

	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zipFile := filepath.Join(t.TempDir(), "zip")
	if err := writeZipFile(zipFile, map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)}); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zip, err := os.ReadFile(zipFile)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		switch path.Ext(req.URL.Path) {
		case ".info":
			responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
		case ".mod":
			responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", 60)
		case ".zip":
			responseSuccess(rw, req, bytes.NewReader(zip), "application/zip", 60)
		default:
			responseNotFound(rw, req, 60)
		}
	}

	for _, tt := range []struct {
		n                  int
		proxyHandler       http.HandlerFunc
		cacher             Cacher
		target             string
		disableModuleFetch bool
		wantStatusCode     int
		wantContentType    string
		wantCacheControl   string
		wantContent        string
	}{
		{
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
			},
			cacher:           DirCacher(t.TempDir()),
			target:           "example.com/@latest",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n:                2,
			proxyHandler:     proxyHandler,
			cacher:           DirCacher(t.TempDir()),
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      info,
		},
		{
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
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
			n:            4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
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
			n:                5,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			cacher:           DirCacher(t.TempDir()),
			target:           "example.com/@latest",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			n:                6,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			cacher:           DirCacher(t.TempDir()),
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=600",
			wantContent:      "not found",
		},
		{
			n:                  7,
			proxyHandler:       func(rw http.ResponseWriter, req *http.Request) {},
			cacher:             DirCacher(t.TempDir()),
			target:             "example.com/@latest",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusNotFound,
			wantContentType:    "text/plain; charset=utf-8",
			wantCacheControl:   "public, max-age=60",
			wantContent:        "not found: temporarily unavailable",
		},
		{
			n:                8,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			cacher:           DirCacher(t.TempDir()),
			target:           "invalid",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found: missing /@v/",
		},
		{
			n: 9,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader("v1.0.0"), "text/plain; charset=utf-8", 60)
			},
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
					return errors.New("cannot put")
				},
			},
			target:          "example.com/@v/list",
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
	} {
		setProxyHandler(tt.proxyHandler)
		g := &Goproxy{
			Env:         []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
			Cacher:      tt.cacher,
			ErrorLogger: log.New(io.Discard, "", 0),
		}
		g.init()
		req := httptest.NewRequest("", "/", nil)
		if tt.disableModuleFetch {
			req.Header.Set("Disable-Module-Fetch", "true")
		}
		rec := httptest.NewRecorder()
		g.serveFetch(rec, req, tt.target, t.TempDir())
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
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
			},
			cacher:           DirCacher(t.TempDir()),
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n:            2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
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
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			cacher:           DirCacher(t.TempDir()),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
	} {
		setProxyHandler(tt.proxyHandler)
		g := &Goproxy{
			Env:         []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
			Cacher:      tt.cacher,
			ErrorLogger: log.New(io.Discard, "", 0),
		}
		g.init()
		f, err := newFetch(g, "example.com/@latest", t.TempDir())
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		rec := httptest.NewRecorder()
		g.serveFetchQuery(rec, httptest.NewRequest("", "/", nil), f, tt.noFetch)
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
	for _, tt := range []struct {
		n              int
		proxyHandler   http.HandlerFunc
		cacher         Cacher
		noFetch        bool
		wantStatusCode int
		wantContent    string
	}{
		{
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(list), "text/plain; charset=utf-8", 60)
			},
			cacher:         DirCacher(t.TempDir()),
			wantStatusCode: http.StatusOK,
			wantContent:    list,
		},
		{
			n:            2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
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
			proxyHandler:   func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			cacher:         DirCacher(t.TempDir()),
			wantStatusCode: http.StatusNotFound,
			wantContent:    "not found",
		},
	} {
		setProxyHandler(tt.proxyHandler)
		g := &Goproxy{
			Env:         []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
			Cacher:      tt.cacher,
			ErrorLogger: log.New(io.Discard, "", 0),
		}
		g.init()
		f, err := newFetch(g, "example.com/@v/list", t.TempDir())
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		rec := httptest.NewRecorder()
		g.serveFetchList(rec, httptest.NewRequest("", "/", nil), f, tt.noFetch)
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
	zipFile := filepath.Join(t.TempDir(), "zip")
	if err := writeZipFile(zipFile, map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)}); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zip, err := os.ReadFile(zipFile)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		switch path.Ext(req.URL.Path) {
		case ".info":
			responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
		case ".mod":
			responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", 60)
		case ".zip":
			responseSuccess(rw, req, bytes.NewReader(zip), "application/zip", 60)
		default:
			responseNotFound(rw, req, 60)
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
			proxyHandler:     proxyHandler,
			cacher:           DirCacher(t.TempDir()),
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      info,
		},
		{
			n:            2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
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
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
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
			proxyHandler:     proxyHandler,
			cacher:           DirCacher(t.TempDir()),
			target:           "example.com/@v/v1.0.0.mod",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      mod,
		},
		{
			n:                5,
			proxyHandler:     proxyHandler,
			cacher:           DirCacher(t.TempDir()),
			target:           "example.com/@v/v1.0.0.zip",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/zip",
			wantCacheControl: "public, max-age=604800",
			wantContent:      string(zip),
		},
		{
			n:                6,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			cacher:           DirCacher(t.TempDir()),
			target:           "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=600",
			wantContent:      "not found",
		},
		{
			n:                7,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			cacher:           DirCacher(t.TempDir()),
			target:           "example.com/@v/v1.0.0.info",
			noFetch:          true,
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found: temporarily unavailable",
		},
		{
			n:            8,
			proxyHandler: proxyHandler,
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
			n:            9,
			proxyHandler: proxyHandler,
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
			n:            10,
			proxyHandler: proxyHandler,
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
					if err := c.Put(ctx, name, content); err != nil {
						return err
					}
					return os.Remove(content.(*os.File).Name())
				},
			},
			target:          "example.com/@v/v1.0.0.info",
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
	} {
		setProxyHandler(tt.proxyHandler)
		g := &Goproxy{
			Env:         []string{"GOPROXY=" + proxyServer.URL, "GOSUMDB=off"},
			Cacher:      tt.cacher,
			ErrorLogger: log.New(io.Discard, "", 0),
		}
		g.init()
		f, err := newFetch(g, tt.target, t.TempDir())
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		rec := httptest.NewRecorder()
		g.serveFetchDownload(rec, httptest.NewRequest("", "/", nil), f, tt.noFetch)
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
	for _, tt := range []struct {
		n                int
		sumdbHandler     http.HandlerFunc
		cacher           Cacher
		target           string
		tempDir          string
		wantStatusCode   int
		wantContentType  string
		wantCacheControl string
		wantContent      string
	}{
		{
			n:                1,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, req.URL.Path) },
			cacher:           DirCacher(t.TempDir()),
			target:           "sumdb/sumdb.example.com/supported",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusOK,
			wantCacheControl: "public, max-age=86400",
		},
		{
			n:                2,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, req.URL.Path) },
			cacher:           DirCacher(t.TempDir()),
			target:           "sumdb/sumdb.example.com/latest",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=3600",
			wantContent:      "/latest",
		},
		{
			n:                3,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, req.URL.Path) },
			cacher:           DirCacher(t.TempDir()),
			target:           "sumdb/sumdb.example.com/lookup/example.com@v1.0.0",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "/lookup/example.com@v1.0.0",
		},
		{
			n:                4,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, req.URL.Path) },
			cacher:           DirCacher(t.TempDir()),
			target:           "sumdb/sumdb.example.com/tile/2/0/0",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/octet-stream",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "/tile/2/0/0",
		},
		{
			n:                5,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			cacher:           DirCacher(t.TempDir()),
			target:           "sumdb/sumdb.example.com",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                6,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			cacher:           DirCacher(t.TempDir()),
			target:           "sumdb/sumdb.example.com/404",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                7,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			cacher:           DirCacher(t.TempDir()),
			target:           "sumdb/sumdb2.example.com/supported",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                8,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { rw.WriteHeader(http.StatusNotFound) },
			cacher:           DirCacher(t.TempDir()),
			target:           "sumdb/sumdb.example.com/latest",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			n:                9,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			cacher:           DirCacher(t.TempDir()),
			target:           "://invalid",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:            10,
			sumdbHandler: func(rw http.ResponseWriter, req *http.Request) {},
			cacher: &testCacher{
				Cacher: DirCacher(t.TempDir()),
				put: func(ctx context.Context, c Cacher, name string, content io.ReadSeeker) error {
					return errors.New("cannot put")
				},
			},
			target:          "sumdb/sumdb.example.com/latest",
			tempDir:         t.TempDir(),
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
	} {
		setSumDBHandler(tt.sumdbHandler)
		g := &Goproxy{
			ProxiedSumDBs: []string{"sumdb.example.com " + sumdbServer.URL},
			Cacher:        tt.cacher,
			ErrorLogger:   log.New(io.Discard, "", 0),
		}
		g.init()
		rec := httptest.NewRecorder()
		g.serveSumDB(rec, httptest.NewRequest("", "/", nil), tt.target, tt.tempDir)
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
			onNotFound:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
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
		g := &Goproxy{
			Cacher:      tt.cacher,
			ErrorLogger: log.New(io.Discard, "", 0),
		}
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
			Cacher:      DirCacher(t.TempDir()),
			ErrorLogger: log.New(io.Discard, "", 0),
		}
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
			n: 1,
			createFile: func() (string, error) {
				file := filepath.Join(t.TempDir(), "foo")
				return file, os.WriteFile(file, []byte("bar"), 0o644)
			},
			wantStatusCode: http.StatusOK,
			wantContent:    "bar",
		},
		{
			n:              2,
			createFile:     func() (string, error) { return filepath.Join(t.TempDir(), "404"), nil },
			wantStatusCode: http.StatusInternalServerError,
			wantContent:    "internal server error",
		},
	} {
		g := &Goproxy{
			Cacher:      DirCacher(t.TempDir()),
			ErrorLogger: log.New(io.Discard, "", 0),
		}
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
	g := &Goproxy{Cacher: dc}
	g.init()
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
	} else if got, want := err, fs.ErrNotExist; !errors.Is(got, want) && got.Error() != want.Error() {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	g.init()
	if _, err := g.cache(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	} else if got, want := err, fs.ErrNotExist; !errors.Is(got, want) && got.Error() != want.Error() {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyPutCache(t *testing.T) {
	dc := DirCacher(t.TempDir())
	g := &Goproxy{Cacher: dc}
	g.init()
	if err := g.putCache(context.Background(), "foo", strings.NewReader("bar")); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if b, err := os.ReadFile(filepath.Join(string(dc), "foo")); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	g.init()
	if err := g.putCache(context.Background(), "foo", strings.NewReader("bar")); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
}

func TestGoproxyPutCacheFile(t *testing.T) {
	dc := DirCacher(t.TempDir())
	g := &Goproxy{Cacher: dc}
	g.init()

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
	} else if got, want := err, fs.ErrNotExist; !errors.Is(got, want) && got.Error() != want.Error() {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyLogErrorf(t *testing.T) {
	for _, tt := range []struct {
		n           int
		errorLogger *log.Logger
	}{
		{1, log.New(io.Discard, "", log.Ldate)},
		{2, nil},
	} {
		var errorLoggerBuffer bytes.Buffer
		g := &Goproxy{ErrorLogger: tt.errorLogger}
		if g.ErrorLogger != nil {
			g.ErrorLogger.SetOutput(&errorLoggerBuffer)
		} else {
			log.SetFlags(log.Ldate)
			defer log.SetFlags(log.LstdFlags)
			log.SetOutput(&errorLoggerBuffer)
			defer log.SetOutput(os.Stderr)
		}
		g.logErrorf("not found: %s", "invalid version")
		if got, want := errorLoggerBuffer.String(), fmt.Sprintf("%s goproxy: not found: invalid version\n", time.Now().Format("2006/01/02")); got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestCleanEnvGOPROXY(t *testing.T) {
	for _, tt := range []struct {
		n              int
		envGOPROXY     string
		wantEnvGOPROXY string
	}{
		{1, "", defaultEnvGOPROXY},
		{2, defaultEnvGOPROXY, defaultEnvGOPROXY},
		{3, "https://example.com", "https://example.com"},
		{4, "https://example.com,", "https://example.com"},
		{5, "https://example.com|", "https://example.com"},
		{6, "https://example.com|https://backup.example.com,direct", "https://example.com|https://backup.example.com,direct"},
		{7, "https://example.com,direct,https://backup.example.com", "https://example.com,direct"},
		{8, "https://example.com,off,https://backup.example.com", "https://example.com,off"},
		{9, ",", "off"},
		{10, "|", "off"},
		{11, " ", "off"},
	} {
		if got, want := cleanEnvGOPROXY(tt.envGOPROXY), tt.wantEnvGOPROXY; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestWalkEnvGOPROXY(t *testing.T) {
	for _, tt := range []struct {
		n            int
		envGOPROXY   string
		onProxy      func(proxy *url.URL) error
		wantOnProxy  string
		wantOnDirect bool
		wantError    error
	}{
		{
			n:           1,
			envGOPROXY:  "https://example.com,direct",
			onProxy:     func(proxy *url.URL) error { return nil },
			wantOnProxy: "https://example.com",
		},
		{
			n:            2,
			envGOPROXY:   "https://example.com,direct",
			onProxy:      func(proxy *url.URL) error { return fs.ErrNotExist },
			wantOnProxy:  "https://example.com",
			wantOnDirect: true,
		},
		{
			n:          3,
			envGOPROXY: "https://example.com|https://alt.example.com",
			onProxy: func(proxy *url.URL) error {
				if proxy.String() == "https://alt.example.com" {
					return nil
				}
				return errors.New("foobar")
			},
			wantOnProxy: "https://alt.example.com",
		},
		{
			n:            4,
			envGOPROXY:   "direct",
			wantOnDirect: true,
		},
		{
			n:            5,
			envGOPROXY:   "direct,off",
			wantOnDirect: true,
		},
		{
			n:          6,
			envGOPROXY: "off",
			wantError:  errors.New("module lookup disabled by GOPROXY=off"),
		},
		{
			n:          7,
			envGOPROXY: "off,direct",
			wantError:  errors.New("module lookup disabled by GOPROXY=off"),
		},
		{
			n:          8,
			envGOPROXY: "https://example.com,direct",
			onProxy:    func(proxy *url.URL) error { return errors.New("foobar") },
			wantError:  errors.New("foobar"),
		},
		{
			n:          9,
			envGOPROXY: "https://example.com",
			onProxy:    func(proxy *url.URL) error { return fs.ErrNotExist },
			wantError:  fs.ErrNotExist,
		},
		{
			n:          10,
			envGOPROXY: "",
			wantError:  errors.New("missing GOPROXY"),
		},
		{
			n:          11,
			envGOPROXY: "://invalid",
			wantError:  errors.New(`parse "://invalid": missing protocol scheme`),
		},
	} {
		var (
			onProxy  string
			onDirect bool
		)
		err := walkEnvGOPROXY(tt.envGOPROXY, func(proxy *url.URL) error {
			onProxy = proxy.String()
			return tt.onProxy(proxy)
		}, func() error {
			onDirect = true
			return nil
		})
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := onProxy, tt.wantOnProxy; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := onDirect, tt.wantOnDirect; got != want {
				t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
			}
		}
	}
}

func TestCleanEnvGOSUMDB(t *testing.T) {
	for _, tt := range []struct {
		n              int
		envGOSUMDB     string
		wantEnvGOSUMDB string
	}{
		{1, "", defaultEnvGOSUMDB},
		{2, defaultEnvGOSUMDB, defaultEnvGOSUMDB},
		{3, "example.com", "example.com"},
	} {
		if got, want := cleanEnvGOSUMDB(tt.envGOSUMDB), tt.wantEnvGOSUMDB; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestParseEnvGOSUMDB(t *testing.T) {
	for _, tt := range []struct {
		n               int
		envGOSUMDB      string
		wantName        string
		wantKey         string
		wantURL         string
		wantIsDirectURL bool
		wantError       error
	}{
		{
			n:               1,
			envGOSUMDB:      defaultEnvGOSUMDB,
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://" + defaultEnvGOSUMDB,
			wantIsDirectURL: true,
		},
		{
			n:               2,
			envGOSUMDB:      sumGolangOrgKey,
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://" + defaultEnvGOSUMDB,
			wantIsDirectURL: true,
		},
		{
			n:               3,
			envGOSUMDB:      sumGolangOrgKey + " https://" + defaultEnvGOSUMDB,
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://" + defaultEnvGOSUMDB,
			wantIsDirectURL: false,
		},
		{
			n:               4,
			envGOSUMDB:      "sum.golang.google.cn",
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://sum.golang.google.cn",
			wantIsDirectURL: false,
		},
		{
			n:               5,
			envGOSUMDB:      "sum.golang.google.cn https://example.com",
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://example.com",
			wantIsDirectURL: false,
		},
		{
			n:          6,
			envGOSUMDB: "",
			wantError:  errors.New("missing GOSUMDB"),
		},
		{
			n:          7,
			envGOSUMDB: " ",
			wantError:  errors.New("missing GOSUMDB"),
		},
		{
			n:          8,
			envGOSUMDB: "a b c",
			wantError:  errors.New("invalid GOSUMDB: too many fields"),
		},
		{
			n:          9,
			envGOSUMDB: "example.com",
			wantError:  errors.New("invalid GOSUMDB: malformed verifier id"),
		},
		{
			n:          10,
			envGOSUMDB: "example.com/+1a6413ba+AW5WXiP8oUq7RI2AuI4Wh14FJrMqJqnAplQ0kcLbnbqK",
			wantError:  fmt.Errorf("invalid sumdb name (must be host[/path]): example.com/ %+v", url.URL{Scheme: "https", Host: "example.com", Path: "/"}),
		},
		{
			n:          11,
			envGOSUMDB: defaultEnvGOSUMDB + " ://invalid",
			wantError:  errors.New(`invalid GOSUMDB URL: parse "://invalid": missing protocol scheme`),
		},
	} {
		name, key, u, isDirectURL, err := parseEnvGOSUMDB(tt.envGOSUMDB)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := name, tt.wantName; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := key, tt.wantKey; got != want {
				t.Errorf("test(%d): got %x, want %x", tt.n, got, want)
			}
			if got, want := u.String(), tt.wantURL; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := isDirectURL, tt.wantIsDirectURL; got != want {
				t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
			}
		}
	}
}

func TestCleanCommaSeparatedList(t *testing.T) {
	for _, tt := range []struct {
		n        int
		list     string
		wantList string
	}{
		{1, "", ""},
		{2, ",", ""},
		{3, "a,", "a"},
		{4, ",a", "a"},
		{5, " , a", "a"},
		{6, "a , b", "a,b"},
	} {
		if got, want := cleanCommaSeparatedList(tt.list), tt.wantList; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
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
