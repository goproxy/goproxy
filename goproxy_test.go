package goproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGoproxyInit(t *testing.T) {
	for _, key := range []string{
		"GO111MODULE",
		"GOPROXY",
		"GONOPROXY",
		"GOSUMDB",
		"GONOSUMDB",
		"GOPRIVATE",
	} {
		if err := os.Setenv(key, ""); err != nil {
			t.Fatalf("unexpected error %q", err)
		}
	}

	g := &Goproxy{}
	g.init()
	if got, want := g.goBinName, "go"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	var goBinEnvPATH string
	for _, env := range g.goBinEnv {
		if envParts := strings.SplitN(env, "=", 2); len(envParts) == 2 {
			if strings.TrimSpace(envParts[0]) == "PATH" {
				goBinEnvPATH = envParts[1]
			}
		}
	}
	if got, want := goBinEnvPATH, os.Getenv("PATH"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	gotEnvGOPROXY := g.goBinEnvGOPROXY
	wantEnvGOPROXY := "https://proxy.golang.org,direct"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}
	if got, want := g.goBinEnvGONOPROXY, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	gotEnvGOSUMDB := g.goBinEnvGOSUMDB
	wantEnvGOSUMDB := "sum.golang.org"
	if gotEnvGOSUMDB != wantEnvGOSUMDB {
		t.Errorf("got %q, want %q", gotEnvGOSUMDB, wantEnvGOSUMDB)
	}
	if got, want := g.goBinEnvGONOSUMDB, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	var goBinEnvGOPRIVATE string
	for _, env := range g.goBinEnv {
		if envParts := strings.SplitN(env, "=", 2); len(envParts) == 2 {
			if strings.TrimSpace(envParts[0]) == "GOPRIVATE" {
				goBinEnvPATH = envParts[1]
			}
		}
	}
	if got, want := goBinEnvGOPRIVATE, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	wantEnvGOPROXY = "https://example.com|https://backup.example.com,direct"
	g.GoBinEnv = []string{"GOPROXY=" + wantEnvGOPROXY}
	g.init()
	gotEnvGOPROXY = g.goBinEnvGOPROXY
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{
		"GOPROXY=https://example.com,direct,https://backup.example.com",
	}
	g.init()
	gotEnvGOPROXY = g.goBinEnvGOPROXY
	wantEnvGOPROXY = "https://example.com,direct"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{
		"GOPROXY=https://example.com,off,https://backup.example.com",
	}
	g.init()
	gotEnvGOPROXY = g.goBinEnvGOPROXY
	wantEnvGOPROXY = "https://example.com,off"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPROXY=https://example.com|"}
	g.init()
	gotEnvGOPROXY = g.goBinEnvGOPROXY
	wantEnvGOPROXY = "https://example.com"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPROXY=,"}
	g.init()
	gotEnvGOPROXY = g.goBinEnvGOPROXY
	wantEnvGOPROXY = "off"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOSUMDB=example.com"}
	g.init()
	gotEnvGOSUMDB = g.goBinEnvGOSUMDB
	wantEnvGOSUMDB = "example.com"
	if gotEnvGOSUMDB != wantEnvGOSUMDB {
		t.Errorf("got %q, want %q", gotEnvGOSUMDB, wantEnvGOSUMDB)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPRIVATE=example.com"}
	g.init()
	gotEnvGONOPROXY := g.goBinEnvGONOPROXY
	wantEnvGONOPROXY := "example.com"
	if gotEnvGONOPROXY != wantEnvGONOPROXY {
		t.Errorf("got %q, want %q", gotEnvGONOPROXY, wantEnvGONOPROXY)
	}
	gotEnvGONOSUMDB := g.goBinEnvGONOSUMDB
	wantEnvGONOSUMDB := "example.com"
	if gotEnvGONOSUMDB != wantEnvGONOSUMDB {
		t.Errorf("got %q, want %q", gotEnvGONOSUMDB, wantEnvGONOSUMDB)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{
		"GOPRIVATE=example.com",
		"GONOPROXY=alt1.example.com",
		"GONOSUMDB=alt2.example.com",
	}
	g.init()
	gotEnvGONOPROXY = g.goBinEnvGONOPROXY
	wantEnvGONOPROXY = "alt1.example.com"
	if gotEnvGONOPROXY != wantEnvGONOPROXY {
		t.Errorf("got %q, want %q", gotEnvGONOPROXY, wantEnvGONOPROXY)
	}
	gotEnvGONOSUMDB = g.goBinEnvGONOSUMDB
	wantEnvGONOSUMDB = "alt2.example.com"
	if gotEnvGONOSUMDB != wantEnvGONOSUMDB {
		t.Errorf("got %q, want %q", gotEnvGONOSUMDB, wantEnvGONOSUMDB)
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
		"example.com wrongurl",
	}
	g.init()
	if got, want := len(g.proxiedSUMDBs), 2; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	gotSUMDBURL := g.proxiedSUMDBs["sum.golang.google.cn"].String()
	wantSUMDBURL := "https://sum.golang.google.cn"
	if gotSUMDBURL != wantSUMDBURL {
		t.Errorf("got %q, want %q", gotSUMDBURL, wantSUMDBURL)
	}
	gotSUMDBURL = g.proxiedSUMDBs["sum.golang.org"].String()
	wantSUMDBURL = "https://sum.golang.google.cn"
	if gotSUMDBURL != wantSUMDBURL {
		t.Errorf("got %q, want %q", gotSUMDBURL, wantSUMDBURL)
	}
	if got := g.proxiedSUMDBs["example.com"]; got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func TestGoproxyServeHTTP(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestGoproxyServeHTTP")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/example.com/@latest":
			responseSuccess(
				rw,
				req,
				strings.NewReader(marshalInfo(
					"v1.0.0",
					infoTime,
				)),
				"application/json; charset=utf-8",
				-2,
			)
		default:
			responseNotFound(rw, req, 60)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		handlerFunc(rw, req)
	}))
	defer server.Close()

	g := &Goproxy{
		Cacher:      DirCacher(tempDir),
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		TempDir:     tempDir,
		ErrorLogger: log.New(&discardWriter{}, "", 0),
	}
	g.init()

	req := httptest.NewRequest("", "/example.com/@latest", nil)
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest(http.MethodHead, "/example.com/@latest", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest(http.MethodPost, "/example.com/@latest", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusMethodNotAllowed; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		"method not allowed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/../example.com/@latest", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/sumdb/sumdb.example.com/supported", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		PathPrefix:  "/prefix/",
		Cacher:      DirCacher(tempDir),
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		TempDir:     filepath.Join(tempDir, "404"),
		ErrorLogger: log.New(&discardWriter{}, "", 0),
	}
	g.init()

	req = httptest.NewRequest("", "/prefix/example.com/@latest", nil)
	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		"internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyServeFetch(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestGoproxyServeFetch")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/example.com/@latest", "/example.com/@v/v1.0.0.info":
			responseSuccess(
				rw,
				req,
				strings.NewReader(marshalInfo(
					"v1.0.0",
					infoTime,
				)),
				"application/json; charset=utf-8",
				-2,
			)
		case "/example.com/@v/list":
			responseSuccess(
				rw,
				req,
				strings.NewReader("v1.0.0"),
				"text/plain; charset=utf-8",
				-2,
			)
		default:
			responseNotFound(rw, req, 60)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		handlerFunc(rw, req)
	}))
	defer server.Close()

	g := &Goproxy{
		Cacher:      DirCacher(tempDir),
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		ErrorLogger: log.New(&discardWriter{}, "", 0),
	}
	g.init()

	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@latest", tempDir)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/v2/@latest", tempDir)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@v/v1.0.0.info", tempDir)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=604800"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@v/v1.1.0.info", tempDir)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=600"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	req.Header.Set("Disable-Module-Fetch", "true")
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@latest", tempDir)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	req.Header.Set("Disable-Module-Fetch", "true")
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/v2/@latest", tempDir)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		"not found: temporarily unavailable"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	req.Header.Set("Disable-Module-Fetch", "true")
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@v/v1.0.0.info", tempDir)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=604800"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "invalid", tempDir)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		"not found: missing /@v/"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		Cacher:      &errorCacher{},
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		ErrorLogger: log.New(&discardWriter{}, "", 0),
	}
	g.init()

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveFetch(rec, req, "example.com/@v/list", tempDir)
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		"internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyServeFetchDownload(t *testing.T) {
	tempDir, err := ioutil.TempDir(
		"",
		"goproxy.TestGoproxyServeFetchDownload",
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/example.com/@v/v1.0.0.info":
			responseSuccess(
				rw,
				req,
				strings.NewReader(marshalInfo(
					"v1.0.0",
					infoTime,
				)),
				"application/json; charset=utf-8",
				-2,
			)
		case "/example.com/@v/v1.0.0.mod":
			responseSuccess(
				rw,
				req,
				strings.NewReader("module example.com"),
				"text/plain; charset=utf-8",
				-2,
			)
		default:
			responseNotFound(rw, req, 60)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		handlerFunc(rw, req)
	}))
	defer server.Close()

	g := &Goproxy{
		Cacher:      DirCacher(tempDir),
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		ErrorLogger: log.New(&discardWriter{}, "", 0),
	}
	g.init()

	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	f, err := newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	g.serveFetchDownload(rec, req, f)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"application/json; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=604800"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		marshalInfo("v1.0.0", infoTime); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	f, err = newFetch(g, "example.com/@v/v1.1.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	g.serveFetchDownload(rec, req, f)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=600"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		Cacher:      &errorCacher{},
		GoBinEnv:    []string{"GOPROXY=" + server.URL, "GOSUMDB=off"},
		ErrorLogger: log.New(&discardWriter{}, "", 0),
	}
	g.init()

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	f, err = newFetch(g, "example.com/@v/v1.0.0.mod", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	g.serveFetchDownload(rec, req, f)
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		"internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyServeSUMDB(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestGoproxyServeSUMDB")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		fmt.Fprint(rw, req.URL.Path)
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		handlerFunc(rw, req)
	}))
	defer server.Close()

	g := &Goproxy{
		Cacher:        DirCacher(tempDir),
		ProxiedSUMDBs: []string{"sumdb.example.com " + server.URL},
		ErrorLogger:   log.New(&discardWriter{}, "", 0),
	}
	g.init()

	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/supported", tempDir)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/latest", tempDir)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=3600"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "/latest"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(
		rec,
		req,
		"sumdb/sumdb.example.com/lookup/example.com@v1.0.0",
		tempDir,
	)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		"/lookup/example.com@v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/tile/2/0/0", tempDir)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"application/octet-stream"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "/tile/2/0/0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/404", tempDir)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "404", tempDir)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb2.example.com/supported", tempDir)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=86400"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(
		rec,
		req,
		"sumdb/sumdb.example.com/latest",
		filepath.Join(tempDir, "404"),
	)
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		"internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(
		rec,
		req,
		"sumdb/sumdb.example.com/lookup/example.com/v2@v2.0.0",
		tempDir,
	)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		"public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		Cacher:        &errorCacher{},
		ProxiedSUMDBs: []string{"sumdb.example.com " + server.URL},
		ErrorLogger:   log.New(&discardWriter{}, "", 0),
	}
	g.init()

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		fmt.Fprint(rw, req.URL.Path)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	g.serveSUMDB(rec, req, "sumdb/sumdb.example.com/latest", tempDir)
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.HeaderMap.Get("Content-Type"),
		"text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.HeaderMap.Get("Cache-Control"),
		""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := rec.Body.String(),
		"internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

type errorCacher struct{}

func (errorCacher) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("error cacher")
}

func (errorCacher) Set(context.Context, string, io.ReadSeeker) error {
	return errors.New("error cacher")
}

func TestGoproxyServeCache(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestGoproxyServeCache")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	g := &Goproxy{Cacher: DirCacher(tempDir)}
	g.init()
	if err := g.setCache(
		context.Background(),
		"foo",
		strings.NewReader("bar"),
	); err != nil {
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
		ErrorLogger: log.New(&discardWriter{}, "", 0),
	}
	g.init()
	g.serveCache(rec, req, "foo", "", 60, func() {})
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	} else if got, want := rec.Body.String(),
		"internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoproxyCache(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestGoproxyCache")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	if err := ioutil.WriteFile(
		filepath.Join(tempDir, "foo"),
		[]byte("bar"),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	g := &Goproxy{Cacher: DirCacher(tempDir)}
	g.init()
	if rc, err := g.cache(context.Background(), "foo"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if b, err := ioutil.ReadAll(rc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := rc.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if _, err := g.cache(context.Background(), "bar"); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, os.ErrNotExist),
		true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	g = &Goproxy{}
	g.init()
	if _, err := g.cache(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, os.ErrNotExist),
		true; got != want {
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

func TestGoproxySetCache(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestGoproxySetCache")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	g := &Goproxy{
		Cacher:              DirCacher(tempDir),
		CacherMaxCacheBytes: 5,
	}
	g.init()
	if err := g.setCache(
		context.Background(),
		"foo",
		strings.NewReader("bar"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if b, err := ioutil.ReadFile(
		filepath.Join(tempDir, "foo"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := g.setCache(
		context.Background(),
		"foo",
		&testReaderSeeker{
			ReadSeeker:      strings.NewReader("bar"),
			cannotSeekStart: true,
		},
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "cannot seek start"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := g.setCache(
		context.Background(),
		"foo",
		&testReaderSeeker{
			ReadSeeker:    strings.NewReader("bar"),
			cannotSeekEnd: true,
		},
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "cannot seek end"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := g.setCache(
		context.Background(),
		"foobar",
		strings.NewReader("foobar"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := ioutil.ReadFile(
		filepath.Join(tempDir, "foobar"),
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, os.ErrNotExist),
		true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	g = &Goproxy{}
	g.init()
	if err := g.setCache(
		context.Background(),
		"foo",
		strings.NewReader("bar"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
}

func TestGoproxySetCacheFile(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestGoproxySetCacheFile")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	cacheFile, err := ioutil.TempFile(tempDir, "")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := cacheFile.WriteString("bar"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := cacheFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	g := &Goproxy{Cacher: DirCacher(tempDir)}
	g.init()
	if err := g.setCacheFile(
		context.Background(),
		"foo",
		cacheFile.Name(),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if b, err := ioutil.ReadFile(filepath.Join(
		tempDir,
		"foo",
	)); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := g.setCacheFile(
		context.Background(),
		"bar",
		filepath.Join(tempDir, "bar-sourcel"),
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, os.ErrNotExist),
		true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGoproxyLogErrorf(t *testing.T) {
	var errorLoggerBuffer bytes.Buffer
	g := &Goproxy{
		ErrorLogger: log.New(&errorLoggerBuffer, "", log.Ldate),
	}
	g.init()
	g.logErrorf("not found: %s", "invalid version")
	if got, want := errorLoggerBuffer.String(), fmt.Sprintf(
		"%s goproxy: not found: invalid version\n",
		time.Now().Format("2006/01/02"),
	); got != want {
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
	if got, want := errorLoggerBuffer.String(), fmt.Sprintf(
		"%s goproxy: not found: invalid version\n",
		time.Now().Format("2006/01/02"),
	); got != want {
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
	if err := walkGOPROXY("https://example.com,direct", func(
		proxy string,
	) error {
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
	if err := walkGOPROXY("https://example.com,direct", func(
		proxy string,
	) error {
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
	if err := walkGOPROXY(
		"https://example.com|https://alt.example.com",
		func(proxy string) error {
			onProxy = proxy
			if proxy == "https://alt.example.com" {
				return nil
			}
			return errors.New("foobar")
		},
		func() error {
			onDirect = true
			return nil
		},
		func() error {
			onOff = true
			return nil
		},
	); err != nil {
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
	if err := walkGOPROXY("https://example.com,direct", func(
		proxy string,
	) error {
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
