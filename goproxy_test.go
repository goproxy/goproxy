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
			if strings.TrimSpace(k) == key {
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
			wantEnvGOPROXY: "https://proxy.golang.org,direct",
			wantEnvGOSUMDB: "sum.golang.org",
		},
		{
			n:              2,
			env:            append(os.Environ(), "GOPROXY=https://example.com|https://backup.example.com,direct"),
			wantEnvGOPROXY: "https://example.com|https://backup.example.com,direct",
			wantEnvGOSUMDB: "sum.golang.org",
		},
		{
			n:              3,
			env:            append(os.Environ(), "GOPROXY=https://example.com,direct,https://backup.example.com"),
			wantEnvGOPROXY: "https://example.com,direct",
			wantEnvGOSUMDB: "sum.golang.org",
		},
		{
			n:              4,
			env:            append(os.Environ(), "GOPROXY=https://example.com,off,https://backup.example.com"),
			wantEnvGOPROXY: "https://example.com,off",
			wantEnvGOSUMDB: "sum.golang.org",
		},
		{
			n:              5,
			env:            append(os.Environ(), "GOPROXY=https://example.com|"),
			wantEnvGOPROXY: "https://example.com",
			wantEnvGOSUMDB: "sum.golang.org",
		},
		{
			n:              6,
			env:            append(os.Environ(), "GOPROXY=,"),
			wantEnvGOPROXY: "off",
			wantEnvGOSUMDB: "sum.golang.org",
		},
		{
			n:              7,
			env:            append(os.Environ(), "GOSUMDB=example.com"),
			wantEnvGOPROXY: "https://proxy.golang.org,direct",
			wantEnvGOSUMDB: "example.com",
		},
		{
			n:                8,
			env:              append(os.Environ(), "GOPRIVATE=example.com"),
			wantEnvGOPROXY:   "https://proxy.golang.org,direct",
			wantEnvGONOPROXY: "example.com",
			wantEnvGOSUMDB:   "sum.golang.org",
			wantEnvGONOSUMDB: "example.com",
		},
		{
			n: 9,
			env: append(
				os.Environ(),
				"GOPRIVATE=example.com",
				"GONOPROXY=alt1.example.com",
				"GONOSUMDB=alt2.example.com",
			),
			wantEnvGOPROXY:   "https://proxy.golang.org,direct",
			wantEnvGONOPROXY: "alt1.example.com",
			wantEnvGOSUMDB:   "sum.golang.org",
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

	g := &Goproxy{}
	g.GoBinMaxWorkers = 1
	g.init()
	if g.goBinWorkerChan == nil {
		t.Fatal("unexpected nil")
	}

	g = &Goproxy{}
	g.ProxiedSUMDBs = []string{
		"sum.golang.google.cn",
		"sum.golang.org https://sum.golang.google.cn",
		"",
		"example.com ://invalid",
	}
	g.init()
	if got, want := len(g.proxiedSUMDBs), 2; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if got, want := g.proxiedSUMDBs["sum.golang.google.cn"].String(), "https://sum.golang.google.cn"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.proxiedSUMDBs["sum.golang.org"].String(), "https://sum.golang.google.cn"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := g.proxiedSUMDBs["example.com"]; got != nil {
		t.Errorf("got %v, want nil", got)
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

func TestGoproxyServeSUMDB(t *testing.T) {
	sumdbServer, setSUMDBHandler := newHTTPTestServer()
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
			name:             "sumdb/sumdb.example.com/404",
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
			name:             "://invalid",
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
			name:             "sumdb/sumdb2.example.com/supported",
			tempDir:          t.TempDir(),
			wantStatusCode:   http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "public, max-age=86400",
			wantContent:      "not found",
		},
		{
			n:               8,
			sumdbHandler:    func(rw http.ResponseWriter, req *http.Request) {},
			cacher:          DirCacher(t.TempDir()),
			name:            "sumdb/sumdb.example.com/latest",
			tempDir:         filepath.Join(t.TempDir(), "404"),
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
		{
			n:                9,
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
			n:               10,
			sumdbHandler:    func(rw http.ResponseWriter, req *http.Request) {},
			cacher:          &errorCacher{},
			name:            "sumdb/sumdb.example.com/latest",
			tempDir:         t.TempDir(),
			wantStatusCode:  http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantContent:     "internal server error",
		},
	} {
		setSUMDBHandler(tt.sumdbHandler)
		g := &Goproxy{
			Cacher:        tt.cacher,
			ProxiedSUMDBs: []string{"sumdb.example.com " + sumdbServer.URL},
			ErrorLogger:   log.New(io.Discard, "", 0),
		}
		g.init()
		rec := httptest.NewRecorder()
		g.serveSUMDB(rec, httptest.NewRequest("", "/", nil), tt.name, tt.tempDir)
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

func TestWalkGOPROXY(t *testing.T) {
	for _, tt := range []struct {
		n            int
		goproxy      string
		onProxy      func(proxy string) (string, error)
		wantOnProxy  string
		wantOnDirect bool
		wantOnOff    bool
		wantError    error
	}{
		{
			n:            1,
			goproxy:      "direct",
			onProxy:      func(proxy string) (string, error) { return proxy, nil },
			wantOnDirect: true,
		},
		{
			n:         2,
			goproxy:   "off",
			onProxy:   func(proxy string) (string, error) { return proxy, nil },
			wantOnOff: true,
		},
		{
			n:            3,
			goproxy:      "direct,off",
			onProxy:      func(proxy string) (string, error) { return proxy, nil },
			wantOnDirect: true,
		},
		{
			n:         4,
			goproxy:   "off,direct",
			onProxy:   func(proxy string) (string, error) { return proxy, nil },
			wantOnOff: true,
		},
		{
			n:           5,
			goproxy:     "https://example.com,direct",
			onProxy:     func(proxy string) (string, error) { return proxy, nil },
			wantOnProxy: "https://example.com",
		},
		{
			n:            6,
			goproxy:      "https://example.com,direct",
			onProxy:      func(proxy string) (string, error) { return proxy, errNotFound },
			wantOnProxy:  "https://example.com",
			wantOnDirect: true,
		},
		{
			n:       7,
			goproxy: "https://example.com|https://alt.example.com",
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
			goproxy:     "https://example.com,direct",
			onProxy:     func(proxy string) (string, error) { return proxy, errors.New("foobar") },
			wantOnProxy: "https://example.com",
			wantError:   errors.New("foobar"),
		},
		{
			n:           9,
			goproxy:     "https://example.com",
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
		err := walkGOPROXY(tt.goproxy, func(proxy string) error {
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
			t.Errorf("test(%d): got %v, want %v", tt.n, got, want)
		}
		if got, want := onOff, tt.wantOnOff; got != want {
			t.Errorf("test(%d): got %v, want %v", tt.n, got, want)
		}
	}

	if err := walkGOPROXY("", nil, nil, nil); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "missing GOPROXY"; got != want {
		t.Errorf("got %q, want %q", got, want)
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
			t.Errorf("test(%d): got %v, want %v", tt.n, got, want)
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
			t.Errorf("test(%d): got %v, want %v", tt.n, got, want)
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
			t.Errorf("test(%d): got %v, want %v", tt.n, got, want)
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
