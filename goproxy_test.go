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

	g := &Goproxy{}
	g.init()
	if got, want := g.goBinName, "go"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := getenv(g.goBinEnv, "PATH"), os.Getenv("PATH"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.goBinEnvGOPROXY, "https://proxy.golang.org,direct"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.goBinEnvGONOPROXY, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.goBinEnvGOSUMDB, "sum.golang.org"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.goBinEnvGONOSUMDB, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := getenv(g.goBinEnv, "GOPRIVATE"), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	envGOPROXY := "https://example.com|https://backup.example.com,direct"
	g.GoBinEnv = []string{"GOPROXY=" + envGOPROXY}
	g.init()
	if got, want := g.goBinEnvGOPROXY, envGOPROXY; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPROXY=https://example.com,direct,https://backup.example.com"}
	g.init()
	if got, want := g.goBinEnvGOPROXY, "https://example.com,direct"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPROXY=https://example.com,off,https://backup.example.com"}
	g.init()
	if got, want := g.goBinEnvGOPROXY, "https://example.com,off"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPROXY=https://example.com|"}
	g.init()
	if got, want := g.goBinEnvGOPROXY, "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPROXY=,"}
	g.init()
	if got, want := g.goBinEnvGOPROXY, "off"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOSUMDB=example.com"}
	g.init()
	if got, want := g.goBinEnvGOSUMDB, "example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPRIVATE=example.com"}
	g.init()
	if got, want := g.goBinEnvGONOPROXY, "example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.goBinEnvGONOSUMDB, "example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{
		"GOPRIVATE=example.com",
		"GONOPROXY=alt1.example.com",
		"GONOSUMDB=alt2.example.com",
	}
	g.init()
	if got, want := g.goBinEnvGONOPROXY, "alt1.example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.goBinEnvGONOSUMDB, "alt2.example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
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
	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/example.com/@latest":
			responseSuccess(rw, req, strings.NewReader(marshalInfo("v1.0.0", infoTime)), "application/json; charset=utf-8", -2)
		default:
			responseNotFound(rw, req, 60)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		handlerFunc(rw, req)
	}))
	defer server.Close()

	g := &Goproxy{
		Cacher:      DirCacher(t.TempDir()),
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		TempDir:     t.TempDir(),
		ErrorLogger: log.New(io.Discard, "", 0),
	}
	g.init()

	req := httptest.NewRequest("", "/example.com/@latest", nil)
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	recr := rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest(http.MethodHead, "/example.com/@latest", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	recr = rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest(http.MethodPost, "/example.com/@latest", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	recr = rec.Result()
	if got, want := rec.Code, http.StatusMethodNotAllowed; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "method not allowed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/../example.com/@latest", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/sumdb/sumdb.example.com/supported", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		PathPrefix:  "/prefix/",
		Cacher:      DirCacher(t.TempDir()),
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		TempDir:     filepath.Join(t.TempDir(), "404"),
		ErrorLogger: log.New(io.Discard, "", 0),
	}
	g.init()

	req = httptest.NewRequest("", "/prefix/example.com/@latest", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	recr = rec.Result()
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyServeFetch(t *testing.T) {
	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/example.com/@latest", "/example.com/@v/v1.0.0.info":
			responseSuccess(rw, req, strings.NewReader(marshalInfo("v1.0.0", infoTime)), "application/json; charset=utf-8", -2)
		case "/example.com/@v/list":
			responseSuccess(rw, req, strings.NewReader("v1.0.0"), "text/plain; charset=utf-8", -2)
		default:
			responseNotFound(rw, req, 60)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		handlerFunc(rw, req)
	}))
	defer server.Close()

	g := &Goproxy{
		Cacher:      DirCacher(t.TempDir()),
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		ErrorLogger: log.New(io.Discard, "", 0),
	}
	g.init()

	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@latest", t.TempDir())
	recr := rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/v2/@latest", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@v/v1.0.0.info", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=604800"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@v/v1.1.0.info", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=600"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	req.Header.Set("Disable-Module-Fetch", "true")
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@latest", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	req.Header.Set("Disable-Module-Fetch", "true")
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/v2/@latest", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found: temporarily unavailable"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	req.Header.Set("Disable-Module-Fetch", "true")
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@v/v1.0.0.info", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=604800"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "invalid", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found: missing /@v/"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		Cacher:      &errorCacher{},
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		ErrorLogger: log.New(io.Discard, "", 0),
	}
	g.init()

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@v/list", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyServeFetchDownload(t *testing.T) {
	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/example.com/@v/v1.0.0.info":
			responseSuccess(rw, req, strings.NewReader(marshalInfo("v1.0.0", infoTime)), "application/json; charset=utf-8", -2)
		case "/example.com/@v/v1.0.0.mod":
			responseSuccess(rw, req, strings.NewReader("module example.com"), "text/plain; charset=utf-8", -2)
		default:
			responseNotFound(rw, req, 60)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		handlerFunc(rw, req)
	}))
	defer server.Close()

	g := &Goproxy{
		Cacher:      DirCacher(t.TempDir()),
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		ErrorLogger: log.New(io.Discard, "", 0),
	}
	g.init()

	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	f, err := newFetch(g, "example.com/@v/v1.0.0.info", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	g.serveFetchDownload(rec, req, f)
	recr := rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=604800"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	f, err = newFetch(g, "example.com/@v/v1.1.0.info", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	g.serveFetchDownload(rec, req, f)
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=600"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		Cacher:      &errorCacher{},
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		ErrorLogger: log.New(io.Discard, "", 0),
	}
	g.init()

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	f, err = newFetch(g, "example.com/@v/v1.0.0.mod", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	g.serveFetchDownload(rec, req, f)
	recr = rec.Result()
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyServeSUMDB(t *testing.T) {
	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		fmt.Fprint(rw, req.URL.Path)
	}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		handlerFunc(rw, req)
	}))
	defer server.Close()

	g := &Goproxy{
		Cacher:        DirCacher(t.TempDir()),
		ProxiedSUMDBs: []string{"sumdb.example.com " + server.URL},
		ErrorLogger:   log.New(io.Discard, "", 0),
	}
	g.init()

	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/supported", t.TempDir())
	recr := rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/latest", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=3600"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "/latest"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/lookup/example.com@v1.0.0", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "/lookup/example.com@v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/tile/2/0/0", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "application/octet-stream"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "/tile/2/0/0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/404", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "://invalid", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb2.example.com/supported", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/latest", filepath.Join(t.TempDir(), "404"))
	recr = rec.Result()
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/lookup/example.com/v2@v2.0.0", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		Cacher:        &errorCacher{},
		ProxiedSUMDBs: []string{"sumdb.example.com " + server.URL},
		ErrorLogger:   log.New(io.Discard, "", 0),
	}
	g.init()

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		fmt.Fprint(rw, req.URL.Path)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/latest", t.TempDir())
	recr = rec.Result()
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := recr.Header.Get("Cache-Control"), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
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

	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	g.serveCache(rec, req, "foo", "", 60, func() {})
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.Body.String(), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveCache(rec, req, "bar", "", 60, func() {
		responseNotFound(rec, req, 60)
	})
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g = &Goproxy{
		Cacher:      &errorCacher{},
		ErrorLogger: log.New(io.Discard, "", 0),
	}
	g.init()
	g.serveCache(rec, req, "foo", "", 60, func() {})
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.Body.String(), "internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyCache(t *testing.T) {
	dc := DirCacher(t.TempDir())
	if err := os.WriteFile(filepath.Join(string(dc), "foo"), []byte("bar"), 0o644); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	g := &Goproxy{Cacher: dc}
	g.init()
	if rc, err := g.cache(context.Background(), "foo"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if b, err := io.ReadAll(rc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := rc.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if _, err := g.cache(context.Background(), "bar"); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, fs.ErrNotExist), true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	g = &Goproxy{}
	g.init()
	if _, err := g.cache(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, fs.ErrNotExist), true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

type testReaderSeeker struct {
	io.ReadSeeker

	cannotSeekStart bool
	cannotSeekEnd   bool
}

func (trs *testReaderSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		if trs.cannotSeekStart {
			return 0, errors.New("cannot seek start")
		}
	case io.SeekEnd:
		if trs.cannotSeekEnd {
			return 0, errors.New("cannot seek end")
		}
	}
	return trs.ReadSeeker.Seek(offset, whence)
}

func TestGoproxyPutCache(t *testing.T) {
	dc := DirCacher(t.TempDir())
	g := &Goproxy{
		Cacher:              dc,
		CacherMaxCacheBytes: 5,
	}
	g.init()
	if err := g.putCache(context.Background(), "foo", strings.NewReader("bar")); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if b, err := os.ReadFile(filepath.Join(string(dc), "foo")); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := g.putCache(context.Background(), "foo", &testReaderSeeker{
		ReadSeeker:      strings.NewReader("bar"),
		cannotSeekStart: true,
	}); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "cannot seek start"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := g.putCache(context.Background(), "foo", &testReaderSeeker{
		ReadSeeker:    strings.NewReader("bar"),
		cannotSeekEnd: true,
	}); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "cannot seek end"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := g.putCache(context.Background(), "foobar", strings.NewReader("foobar")); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := os.ReadFile(filepath.Join(string(dc), "foobar")); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, fs.ErrNotExist), true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	g = &Goproxy{}
	g.init()
	if err := g.putCache(context.Background(), "foo", strings.NewReader("bar")); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
}

func TestGoproxyPutCacheFile(t *testing.T) {
	dc := DirCacher(t.TempDir())
	cacheFile, err := os.CreateTemp(string(dc), "")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := cacheFile.WriteString("bar"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := cacheFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	g := &Goproxy{Cacher: dc}
	g.init()
	if err := g.putCacheFile(context.Background(), "foo", cacheFile.Name()); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if b, err := os.ReadFile(filepath.Join(string(dc), "foo")); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := g.putCacheFile(context.Background(), "bar", filepath.Join(string(dc), "bar-sourcel")); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, fs.ErrNotExist), true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGoproxyLogErrorf(t *testing.T) {
	var errorLoggerBuffer bytes.Buffer
	g := &Goproxy{ErrorLogger: log.New(&errorLoggerBuffer, "", log.Ldate)}
	g.init()
	g.logErrorf("not found: %s", "invalid version")
	if got, want := errorLoggerBuffer.String(), fmt.Sprintf("%s goproxy: not found: invalid version\n", time.Now().Format("2006/01/02")); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	errorLoggerBuffer.Reset()
	g = &Goproxy{}
	g.init()
	log.SetFlags(log.Ldate)
	defer log.SetFlags(log.LstdFlags)
	log.SetOutput(&errorLoggerBuffer)
	defer log.SetOutput(os.Stderr)
	g.logErrorf("not found: %s", "invalid version")
	if got, want := errorLoggerBuffer.String(), fmt.Sprintf("%s goproxy: not found: invalid version\n", time.Now().Format("2006/01/02")); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWalkGOPROXY(t *testing.T) {
	if err := walkGOPROXY("", nil, nil, nil); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "missing GOPROXY"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	var (
		onProxy  string
		onDirect bool
		onOff    bool
	)
	if err := walkGOPROXY("direct", func(proxy string) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := onProxy, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("off", func(proxy string) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := onProxy, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("direct,off", func(proxy string) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := onProxy, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("off,direct", func(proxy string) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := onProxy, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("https://example.com,direct", func(proxy string) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := onProxy, "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("https://example.com,direct", func(proxy string) error {
		onProxy = proxy
		return notFoundError("not found")
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := onProxy, "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("https://example.com|https://alt.example.com", func(proxy string) error {
		onProxy = proxy
		if proxy == "https://alt.example.com" {
			return nil
		}
		return errors.New("foobar")
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := onProxy, "https://alt.example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("https://example.com,direct", func(proxy string) error {
		onProxy = proxy
		return errors.New("foobar")
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "foobar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onProxy, "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("https://example.com", func(proxy string) error {
		onProxy = proxy
		return notFoundError("not found")
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onProxy, "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBackoffSleep(t *testing.T) {
	if got, want := backoffSleep(100*time.Millisecond, time.Second, 0) <= time.Second, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := backoffSleep(time.Minute, time.Hour, 100) <= time.Hour, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStringSliceContains(t *testing.T) {
	if !stringSliceContains([]string{"foo", "bar"}, "foo") {
		t.Error("want true")
	}
	if stringSliceContains([]string{"foo", "bar"}, "foobar") {
		t.Error("want false")
	}
}

func TestGlobsMatchPath(t *testing.T) {
	if !globsMatchPath("foobar", "foobar") {
		t.Error("want true")
	}

	if !globsMatchPath("foo", "foo/bar") {
		t.Error("want true")
	}

	if globsMatchPath("foo", "bar/foo") {
		t.Error("want false")
	}

	if globsMatchPath("foo", "foobar") {
		t.Error("want false")
	}

	if !globsMatchPath("foo/bar", "foo/bar") {
		t.Error("want true")
	}

	if globsMatchPath("foo/bar", "foobar") {
		t.Error("want false")
	}

	if !globsMatchPath("foo,bar", "foo") {
		t.Error("want true")
	}

	if !globsMatchPath("foo,", "foo") {
		t.Error("want true")
	}

	if !globsMatchPath(",bar", "bar") {
		t.Error("want true")
	}

	if globsMatchPath("", "foobar") {
		t.Error("want false")
	}
}
