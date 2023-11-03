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
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
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
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
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
	for _, tt := range []struct {
		n                  int
		proxyHandler       http.HandlerFunc
		cacher             Cacher
		setupCacher        func(cacher Cacher) error
		name               string
		disableModuleFetch bool
		wantStatusCode     int
		wantContentType    string
		wantCacheControl   string
		wantContent        string
	}{
		{
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
			},
			cacher:           DirCacher(t.TempDir()),
			name:             "example.com/@latest",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      info,
		},
		{
			n:                2,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			cacher:           DirCacher(t.TempDir()),
			name:             "example.com/v2/@latest",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			n: 3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
			},
			cacher:           DirCacher(t.TempDir()),
			name:             "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      info,
		},
		{
			n:                4,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			cacher:           DirCacher(t.TempDir()),
			name:             "example.com/v2/@v/v1.1.0.info",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=600",
			wantContent:      "not found",
		},
		{
			n:            5,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
			cacher:       DirCacher(t.TempDir()),
			setupCacher: func(cacher Cacher) error {
				return cacher.Put(context.Background(), "example.com/@latest", strings.NewReader(info))
			},
			name:               "example.com/@latest",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusOK,
			wantContentType:    "application/json; charset=utf-8",
			wantCacheControl:   "public, max-age=60",
			wantContent:        info,
		},
		{
			n:                  6,
			proxyHandler:       func(rw http.ResponseWriter, req *http.Request) {},
			cacher:             DirCacher(t.TempDir()),
			name:               "example.com/v2/@latest",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusNotFound,
			wantContentType:    "text/plain; charset=utf-8",
			wantCacheControl:   "public, max-age=60",
			wantContent:        "not found: temporarily unavailable",
		},
		{
			n:            7,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
			cacher:       DirCacher(t.TempDir()),
			setupCacher: func(cacher Cacher) error {
				return cacher.Put(context.Background(), "example.com/@v/v1.0.0.info", strings.NewReader(info))
			},
			name:               "example.com/@v/v1.0.0.info",
			disableModuleFetch: true,
			wantStatusCode:     http.StatusOK,
			wantContentType:    "application/json; charset=utf-8",
			wantCacheControl:   "public, max-age=604800",
			wantContent:        info,
		},
		{
			n:                8,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			cacher:           DirCacher(t.TempDir()),
			name:             "invalid",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found: missing /@v/",
		},
		{
			n: 9,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader("v1.0.0"), "text/plain; charset=utf-8", -2)
			},
			cacher:          &errorCacher{},
			name:            "example.com/@v/list",
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
	} {
		setProxyHandler(tt.proxyHandler)
		if tt.setupCacher != nil {
			if err := tt.setupCacher(tt.cacher); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
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
		g.serveFetch(rec, req, tt.name, t.TempDir())
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

func TestGoproxyServeFetchDownload(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	info := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	for _, tt := range []struct {
		n                int
		proxyHandler     http.HandlerFunc
		cacher           Cacher
		name             string
		wantStatusCode   int
		wantContentType  string
		wantCacheControl string
		wantContent      string
	}{
		{
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
			},
			cacher:           DirCacher(t.TempDir()),
			name:             "example.com/@v/v1.0.0.info",
			wantStatusCode:   http.StatusOK,
			wantContentType:  "application/json; charset=utf-8",
			wantCacheControl: "public, max-age=604800",
			wantContent:      info,
		},
		{
			n:                2,
			proxyHandler:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			cacher:           DirCacher(t.TempDir()),
			name:             "example.com/@v/v1.1.0.info",
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=600",
			wantContent:      "not found",
		},
		{
			n: 3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader("module example.com"), "text/plain; charset=utf-8", -2)
			},
			cacher:          &errorCacher{},
			name:            "example.com/@v/v1.0.0.mod",
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
		f, err := newFetch(g, tt.name, t.TempDir())
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		rec := httptest.NewRecorder()
		g.serveFetchDownload(rec, httptest.NewRequest("", "/", nil), f)
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
		name             string
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
			name:             "sumdb/sumdb.example.com/supported",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusOK,
			wantCacheControl: "public, max-age=86400",
		},
		{
			n:                2,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, req.URL.Path) },
			cacher:           DirCacher(t.TempDir()),
			name:             "sumdb/sumdb.example.com/latest",
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
			name:             "sumdb/sumdb.example.com/lookup/example.com@v1.0.0",
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
			name:             "sumdb/sumdb.example.com/tile/2/0/0",
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
			name:             "sumdb/sumdb.example.com",
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
			name:             "sumdb/sumdb.example.com/404",
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
			name:             "://invalid",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:                8,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) {},
			cacher:           DirCacher(t.TempDir()),
			name:             "sumdb/sumdb2.example.com/supported",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:               9,
			sumdbHandler:    func(rw http.ResponseWriter, req *http.Request) {},
			cacher:          DirCacher(t.TempDir()),
			name:            "sumdb/sumdb.example.com/latest",
			tempDir:         filepath.Join(t.TempDir(), "404"),
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
		{
			n:                10,
			sumdbHandler:     func(rw http.ResponseWriter, req *http.Request) { rw.WriteHeader(http.StatusNotFound) },
			cacher:           DirCacher(t.TempDir()),
			name:             "sumdb/sumdb.example.com/lookup/example.com/v2@v2.0.0",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found",
		},
		{
			n:               11,
			sumdbHandler:    func(rw http.ResponseWriter, req *http.Request) {},
			cacher:          &errorCacher{},
			name:            "sumdb/sumdb.example.com/latest",
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
		g.serveSumDB(rec, httptest.NewRequest("", "/", nil), tt.name, tt.tempDir)
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

type errorCacher struct{}

func (errorCacher) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("error cacher")
}

func (errorCacher) Put(context.Context, string, io.ReadSeeker) error {
	return errors.New("error cacher")
}

func TestGoproxyServeCache(t *testing.T) {
	g := &Goproxy{Cacher: DirCacher(t.TempDir())}
	g.init()
	if err := g.putCache(context.Background(), "foo", strings.NewReader("bar")); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	for _, tt := range []struct {
		n              int
		name           string
		onNotFound     func(rw http.ResponseWriter, req *http.Request)
		wantStatusCode int
		wantContent    string
	}{
		{
			n:              1,
			name:           "foo",
			onNotFound:     func(rw http.ResponseWriter, req *http.Request) {},
			wantStatusCode: http.StatusOK,
			wantContent:    "bar",
		},
		{
			n:              2,
			name:           "bar",
			onNotFound:     func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			wantStatusCode: http.StatusNotFound,
			wantContent:    "not found",
		},
	} {
		req := httptest.NewRequest("", "/", nil)
		rec := httptest.NewRecorder()
		g.serveCache(rec, req, tt.name, "", 60, func() { tt.onNotFound(rec, req) })
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

	g = &Goproxy{
		Cacher:      &errorCacher{},
		ErrorLogger: log.New(io.Discard, "", 0),
	}
	g.init()
	rec := httptest.NewRecorder()
	g.serveCache(rec, httptest.NewRequest("", "/", nil), "foo", "", 60, func() {})
	recr := rec.Result()
	if got, want := recr.StatusCode, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if b, err := io.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
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
		onProxy      func(proxy string) (string, error)
		wantOnProxy  string
		wantOnDirect bool
		wantOnOff    bool
		wantError    error
	}{
		{
			n:            1,
			envGOPROXY:   "direct",
			onProxy:      func(proxy string) (string, error) { return proxy, nil },
			wantOnDirect: true,
		},
		{
			n:          2,
			envGOPROXY: "off",
			onProxy:    func(proxy string) (string, error) { return proxy, nil },
			wantOnOff:  true,
		},
		{
			n:            3,
			envGOPROXY:   "direct,off",
			onProxy:      func(proxy string) (string, error) { return proxy, nil },
			wantOnDirect: true,
		},
		{
			n:          4,
			envGOPROXY: "off,direct",
			onProxy:    func(proxy string) (string, error) { return proxy, nil },
			wantOnOff:  true,
		},
		{
			n:           5,
			envGOPROXY:  "https://example.com,direct",
			onProxy:     func(proxy string) (string, error) { return proxy, nil },
			wantOnProxy: "https://example.com",
		},
		{
			n:            6,
			envGOPROXY:   "https://example.com,direct",
			onProxy:      func(proxy string) (string, error) { return proxy, errNotFound },
			wantOnProxy:  "https://example.com",
			wantOnDirect: true,
		},
		{
			n:          7,
			envGOPROXY: "https://example.com|https://alt.example.com",
			onProxy: func(proxy string) (string, error) {
				if proxy == "https://alt.example.com" {
					return proxy, nil
				}
				return proxy, errors.New("foobar")
			},
			wantOnProxy: "https://alt.example.com",
		},
		{
			n:           8,
			envGOPROXY:  "https://example.com,direct",
			onProxy:     func(proxy string) (string, error) { return proxy, errors.New("foobar") },
			wantOnProxy: "https://example.com",
			wantError:   errors.New("foobar"),
		},
		{
			n:           9,
			envGOPROXY:  "https://example.com",
			onProxy:     func(proxy string) (string, error) { return proxy, errNotFound },
			wantOnProxy: "https://example.com",
			wantError:   errNotFound,
		},
	} {
		var (
			onProxy  string
			onDirect bool
			onOff    bool
		)
		err := walkEnvGOPROXY(tt.envGOPROXY, func(proxy string) error {
			var err error
			onProxy, err = tt.onProxy(proxy)
			return err
		}, func() error {
			onDirect = true
			return nil
		}, func() error {
			onOff = true
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
		}
		if got, want := onProxy, tt.wantOnProxy; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := onDirect, tt.wantOnDirect; got != want {
			t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
		}
		if got, want := onOff, tt.wantOnOff; got != want {
			t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
		}
	}

	if err := walkEnvGOPROXY("", nil, nil, nil); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "missing GOPROXY"; got != want {
		t.Errorf("got %q, want %q", got, want)
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

func TestBackoffSleep(t *testing.T) {
	for _, tt := range []struct {
		n       int
		base    time.Duration
		cap     time.Duration
		attempt int
	}{
		{1, 100 * time.Millisecond, time.Second, 0},
		{2, time.Minute, time.Hour, 100},
	} {
		if got, want := backoffSleep(tt.base, tt.cap, tt.attempt) <= tt.cap, true; got != want {
			t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
		}
	}
}

func TestStringSliceContains(t *testing.T) {
	for _, tt := range []struct {
		n            int
		ss           []string
		s            string
		wantContains bool
	}{
		{1, []string{"foo", "bar"}, "foo", true},
		{2, []string{"foo", "bar"}, "foobar", false},
	} {
		if got, want := stringSliceContains(tt.ss, tt.s), tt.wantContains; got != want {
			t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
		}
	}
}

func TestGlobsMatchPath(t *testing.T) {
	for _, tt := range []struct {
		n         int
		globs     string
		target    string
		wantMatch bool
	}{
		{1, "foobar", "foobar", true},
		{2, "foo", "foo/bar", true},
		{3, "foo", "bar/foo", false},
		{4, "foo", "foobar", false},
		{5, "foo/bar", "foo/bar", true},
		{6, "foo/bar", "foobar", false},
		{7, "foo,bar", "foo", true},
		{8, "foo,", "foo", true},
		{9, ",bar", "bar", true},
		{10, "", "foobar", false},
	} {
		if got, want := globsMatchPath(tt.globs, tt.target), tt.wantMatch; got != want {
			t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
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
