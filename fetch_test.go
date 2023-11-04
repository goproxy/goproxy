package goproxy

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/sumdb/note"
)

func TestNewFetch(t *testing.T) {
	for _, tt := range []struct {
		n                    int
		env                  []string
		name                 string
		wantOps              fetchOps
		wantModulePath       string
		wantModuleVersion    string
		wantModAtVer         string
		wantRequiredToVerify bool
		wantContentType      string
		wantError            error
	}{
		{
			n:                    1,
			name:                 "example.com/@latest",
			wantOps:              fetchOpsQuery,
			wantModulePath:       "example.com",
			wantModuleVersion:    "latest",
			wantModAtVer:         "example.com@latest",
			wantRequiredToVerify: true,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:                    2,
			env:                  []string{"GOSUMDB=off"},
			name:                 "example.com/@latest",
			wantOps:              fetchOpsQuery,
			wantModulePath:       "example.com",
			wantModuleVersion:    "latest",
			wantModAtVer:         "example.com@latest",
			wantRequiredToVerify: false,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:                    3,
			env:                  []string{"GONOSUMDB=example.com"},
			name:                 "example.com/@latest",
			wantOps:              fetchOpsQuery,
			wantModulePath:       "example.com",
			wantModuleVersion:    "latest",
			wantModAtVer:         "example.com@latest",
			wantRequiredToVerify: false,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:                    4,
			env:                  []string{"GOPRIVATE=example.com"},
			name:                 "example.com/@latest",
			wantOps:              fetchOpsQuery,
			wantModulePath:       "example.com",
			wantModuleVersion:    "latest",
			wantModAtVer:         "example.com@latest",
			wantRequiredToVerify: false,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:                    5,
			name:                 "example.com/@v/list",
			wantOps:              fetchOpsList,
			wantModulePath:       "example.com",
			wantModuleVersion:    "latest",
			wantModAtVer:         "example.com@latest",
			wantRequiredToVerify: true,
			wantContentType:      "text/plain; charset=utf-8",
		},
		{
			n:                    6,
			name:                 "example.com/@v/v1.0.0.info",
			wantOps:              fetchOpsDownload,
			wantModulePath:       "example.com",
			wantModuleVersion:    "v1.0.0",
			wantModAtVer:         "example.com@v1.0.0",
			wantRequiredToVerify: true,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:                    7,
			name:                 "example.com/@v/v1.0.0.mod",
			wantOps:              fetchOpsDownload,
			wantModulePath:       "example.com",
			wantModuleVersion:    "v1.0.0",
			wantModAtVer:         "example.com@v1.0.0",
			wantRequiredToVerify: true,
			wantContentType:      "text/plain; charset=utf-8",
		},
		{
			n:                    8,
			name:                 "example.com/@v/v1.0.0.zip",
			wantOps:              fetchOpsDownload,
			wantModulePath:       "example.com",
			wantModuleVersion:    "v1.0.0",
			wantModAtVer:         "example.com@v1.0.0",
			wantRequiredToVerify: true,
			wantContentType:      "application/zip",
		},
		{
			n:                    9,
			name:                 "example.com/@v/master.info",
			wantOps:              fetchOpsQuery,
			wantModulePath:       "example.com",
			wantModuleVersion:    "master",
			wantModAtVer:         "example.com@master",
			wantRequiredToVerify: true,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:                    10,
			name:                 "example.com/!foobar/@v/!v1.0.0.info",
			wantOps:              fetchOpsQuery,
			wantModulePath:       "example.com/Foobar",
			wantModuleVersion:    "V1.0.0",
			wantModAtVer:         "example.com/Foobar@V1.0.0",
			wantRequiredToVerify: true,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:         11,
			name:      "example.com/@v/v1.0.0.ext",
			wantError: errors.New(`unexpected extension ".ext"`),
		},
		{
			n:         12,
			name:      "example.com/@v/latest.info",
			wantError: errors.New("invalid version"),
		},
		{
			n:         13,
			name:      "example.com/@v/upgrade.info",
			wantError: errors.New("invalid version"),
		},
		{
			n:         14,
			name:      "example.com/@v/patch.info",
			wantError: errors.New("invalid version"),
		},
		{
			n:         15,
			name:      "example.com/@v/master.mod",
			wantError: errors.New("unrecognized version"),
		},
		{
			n:         16,
			name:      "example.com/@v/master.zip",
			wantError: errors.New("unrecognized version"),
		},
		{
			n:         17,
			name:      "example.com",
			wantError: errors.New("missing /@v/"),
		},
		{
			n:         18,
			name:      "example.com/@v/",
			wantError: errors.New(`no file extension in filename ""`),
		},
		{
			n:         19,
			name:      "example.com/@v/main",
			wantError: errors.New(`no file extension in filename "main"`),
		},
		{
			n:         20,
			name:      "example.com/!!foobar/@latest",
			wantError: errors.New(`invalid escaped module path "example.com/!!foobar"`),
		},
		{
			n:         21,
			name:      "example.com/@v/!!v1.0.0.info",
			wantError: errors.New(`invalid escaped version "!!v1.0.0"`),
		},
	} {
		g := &Goproxy{Env: tt.env}
		g.init()
		f, err := newFetch(g, tt.name, "tempDir")
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
			if got, want := f.ops, tt.wantOps; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := f.name, tt.name; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := f.tempDir, "tempDir"; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := f.modulePath, tt.wantModulePath; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := f.moduleVersion, tt.wantModuleVersion; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := f.modAtVer, tt.wantModAtVer; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := f.requiredToVerify, tt.wantRequiredToVerify; got != want {
				t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
			}
			if got, want := f.contentType, tt.wantContentType; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}
}

func TestFetchDo(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		env          []string
		setupGorpoxy func(g *Goproxy) error
		name         string
		wantVersion  string
		wantTime     time.Time
		wantError    error
	}{
		{
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(marshalInfo("v1.0.0", infoTime)), "application/json; charset=utf-8", 60)
			},
			env: []string{
				"GOPROXY=" + proxyServer.URL,
				"GOSUMDB=off",
			},
			name:        "example.com/@latest",
			wantVersion: "v1.0.0",
			wantTime:    infoTime,
		},
		{
			n:            2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			env: []string{
				"GOPATH=" + t.TempDir(),
				"GOPROXY=off",
				"GONOPROXY=example.com",
				"GOSUMDB=off",
			},
			setupGorpoxy: func(g *Goproxy) error {
				g.env = append(g.env, "GOPROXY="+proxyServer.URL)
				return nil
			},
			name:      "example.com/@latest",
			wantError: notExistErrorf("module example.com: reading %s/example.com/@v/list: 404 Not Found\n\tserver response: not found", proxyServer.URL),
		},
		{
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			env: []string{
				"GOPATH=" + t.TempDir(),
				"GOPROXY=" + proxyServer.URL + ",direct",
				"GOSUMDB=off",
			},
			setupGorpoxy: func(g *Goproxy) error {
				g.env = append(g.env, "GOPROXY=off")
				return nil
			},
			name:      "example.com/@latest",
			wantError: notExistErrorf("example.com@latest: module lookup disabled by GOPROXY=off"),
		},
		{
			n:            4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			env: []string{
				"GOPROXY=off",
				"GOSUMDB=off",
			},
			name:      "example.com/@latest",
			wantError: notExistErrorf("module lookup disabled by GOPROXY=off"),
		},
	} {
		setProxyHandler(tt.proxyHandler)
		g := &Goproxy{Env: tt.env}
		g.init()
		if tt.setupGorpoxy != nil {
			if err := tt.setupGorpoxy(g); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
		f, err := newFetch(g, tt.name, t.TempDir())
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		fr, err := f.do(context.Background())
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if tt.wantError != fs.ErrNotExist && errors.Is(tt.wantError, fs.ErrNotExist) {
				if got, want := err, tt.wantError; !errors.Is(got, fs.ErrNotExist) || got.Error() != want.Error() {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			} else if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := fr.Version, tt.wantVersion; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := fr.Time, tt.wantTime; !got.Equal(want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}
}

func TestFetchDoProxy(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	sumdbServer, setSumDBHandler := newHTTPTestServer()
	defer sumdbServer.Close()

	infoVersion := "v1.0.0"
	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	info := marshalInfo(infoVersion, infoTime)
	mod := "module example.com"
	modFile := filepath.Join(t.TempDir(), "mod")
	if err := os.WriteFile(modFile, []byte(mod), 0o644); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
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

	dirHash, err := dirhash.HashZip(zipFile, dirhash.DefaultHash)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
		return os.Open(modFile)
	})
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	skey, vkey, err := note.GenerateKey(nil, "sumdb.example.com")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	sumdbHandler := sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
		gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
		gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
		return []byte(gosum), nil
	}))

	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		sumdbHandler http.Handler
		name         string
		tempDir      string
		proxy        string
		wantVersion  string
		wantTime     time.Time
		wantVersions []string
		wantInfo     string
		wantGoMod    string
		wantZip      string
		wantError    error
	}{
		{
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
			},
			name:        "example.com/@latest",
			proxy:       proxyServer.URL,
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			n: 2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(`v1.0.0
v1.1.0
v1.1.1-0.20200101000000-0123456789ab
v1.2.0 foo bar
invalid
`), "text/plain; charset=utf-8", 60)
			},
			name:         "example.com/@v/list",
			proxy:        proxyServer.URL,
			wantVersions: []string{"v1.0.0", "v1.1.0", "v1.2.0"},
		},
		{
			n:            3,
			proxyHandler: proxyHandler,
			name:         "example.com/@v/v1.0.0.info",
			tempDir:      t.TempDir(),
			proxy:        proxyServer.URL,
			wantInfo:     info,
			wantGoMod:    mod,
			wantZip:      string(zip),
		},
		{
			n:            4,
			proxyHandler: proxyHandler,
			name:         "example.com/@v/v1.0.0.mod",
			tempDir:      t.TempDir(),
			proxy:        proxyServer.URL,
			wantInfo:     info,
			wantGoMod:    mod,
			wantZip:      string(zip),
		},
		{
			n:            5,
			proxyHandler: proxyHandler,
			sumdbHandler: sumdbHandler,
			name:         "example.com/@v/v1.0.0.mod",
			tempDir:      t.TempDir(),
			proxy:        proxyServer.URL,
			wantInfo:     info,
			wantGoMod:    mod,
			wantZip:      string(zip),
		},
		{
			n:            6,
			proxyHandler: proxyHandler,
			name:         "example.com/@v/v1.0.0.zip",
			tempDir:      t.TempDir(),
			proxy:        proxyServer.URL,
			wantInfo:     info,
			wantGoMod:    mod,
			wantZip:      string(zip),
		},
		{
			n:            7,
			proxyHandler: proxyHandler,
			sumdbHandler: sumdbHandler,
			name:         "example.com/@v/v1.0.0.zip",
			tempDir:      t.TempDir(),
			proxy:        proxyServer.URL,
			wantInfo:     info,
			wantGoMod:    mod,
			wantZip:      string(zip),
		},
		{
			n: 8,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(marshalInfo(infoVersion, time.Time{})), "application/json; charset=utf-8", 60)
			},
			name:      "example.com/@latest",
			proxy:     proxyServer.URL,
			wantError: notExistErrorf("invalid info response: zero time"),
		},
		{
			n: 9,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(marshalInfo(infoVersion, time.Time{})), "application/json; charset=utf-8", 60)
			},
			name:      "example.com/@v/v1.0.0.info",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: notExistErrorf("invalid info file: zero time"),
		},
		{
			n: 10,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				switch path.Ext(req.URL.Path) {
				case ".info":
					responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
				case ".mod":
					responseSuccess(rw, req, strings.NewReader("Go 1.13\n"), "text/plain; charset=utf-8", 60)
				case ".zip":
					responseSuccess(rw, req, bytes.NewReader(zip), "application/zip", 60)
				default:
					responseNotFound(rw, req, 60)
				}
			},
			name:      "example.com/@v/v1.0.0.mod",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: notExistErrorf("invalid mod file: missing module directive"),
		},
		{
			n:            11,
			proxyHandler: proxyHandler,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, "v1.0.0", dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, "v1.1.0", modHash)
				return []byte(gosum), nil
			})),
			name:      "example.com/@v/v1.0.0.mod",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
		{
			n: 12,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				switch path.Ext(req.URL.Path) {
				case ".info":
					responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
				case ".mod":
					responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", 60)
				case ".zip":
					responseSuccess(rw, req, strings.NewReader("zip"), "application/zip", 60)
				default:
					responseNotFound(rw, req, 60)
				}
			},
			name:      "example.com/@v/v1.0.0.zip",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: notExistErrorf("invalid zip file: zip: not a valid zip file"),
		},
		{
			n:            13,
			proxyHandler: proxyHandler,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, "v1.1.0", dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, "v1.0.0", modHash)
				return []byte(gosum), nil
			})),
			name:      "example.com/@v/v1.0.0.zip",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
		{
			n:            14,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			name:         "example.com/@latest",
			proxy:        proxyServer.URL,
			wantError:    notExistErrorf("not found"),
		},
		{
			n:            15,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			name:         "example.com/@v/list",
			proxy:        proxyServer.URL,
			wantError:    notExistErrorf("not found"),
		},
		{
			n:            16,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			name:         "example.com/@v/v1.0.0.info",
			tempDir:      t.TempDir(),
			proxy:        proxyServer.URL,
			wantError:    notExistErrorf("not found"),
		},
		{
			n: 17,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if path.Ext(req.URL.Path) == ".info" {
					responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
				} else {
					responseNotFound(rw, req, 60)
				}
			},
			name:      "example.com/@v/v1.0.0.mod",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: notExistErrorf("not found"),
		},
		{
			n: 18,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				switch path.Ext(req.URL.Path) {
				case ".info":
					responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", 60)
				case ".mod":
					responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", 60)
				default:
					responseNotFound(rw, req, 60)
				}
			},
			name:      "example.com/@v/v1.0.0.zip",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: notExistErrorf("not found"),
		},
	} {
		setProxyHandler(tt.proxyHandler)
		envGOSUMDB := "off"
		if tt.sumdbHandler != nil {
			setSumDBHandler(tt.sumdbHandler.ServeHTTP)
			envGOSUMDB = vkey + " " + sumdbServer.URL
		}
		g := &Goproxy{
			Env: []string{
				"GOPROXY=off",
				"GOSUMDB=" + envGOSUMDB,
			},
		}
		g.init()
		f, err := newFetch(g, tt.name, tt.tempDir)
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		proxy, err := url.Parse(tt.proxy)
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		fr, err := f.doProxy(context.Background(), proxy)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if tt.wantError != fs.ErrNotExist && errors.Is(tt.wantError, fs.ErrNotExist) {
				if got, want := err, tt.wantError; !errors.Is(got, fs.ErrNotExist) || got.Error() != want.Error() {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			} else if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := fr.Version, tt.wantVersion; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := fr.Time, tt.wantTime; !got.Equal(want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := strings.Join(fr.Versions, "\n"), strings.Join(tt.wantVersions, "\n"); got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if tt.wantInfo != "" {
				if b, err := os.ReadFile(fr.Info); err != nil {
					t.Fatalf("test(%d): unexpected error %q", tt.n, err)
				} else if got, want := string(b), tt.wantInfo; got != want {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			}
			if tt.wantGoMod != "" {
				if b, err := os.ReadFile(fr.GoMod); err != nil {
					t.Fatalf("test(%d): unexpected error %q", tt.n, err)
				} else if got, want := string(b), tt.wantGoMod; got != want {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			}
			if tt.wantZip != "" {
				if b, err := os.ReadFile(fr.Zip); err != nil {
					t.Fatalf("test(%d): unexpected error %q", tt.n, err)
				} else if got, want := string(b), tt.wantZip; got != want {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			}
		}
	}
}

func TestFetchDoDirect(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	sumdbServer, setSumDBHandler := newHTTPTestServer()
	defer sumdbServer.Close()

	t.Setenv("GOFLAGS", "-modcacherw")
	gopathDir := filepath.Join(t.TempDir(), "gopath")
	staticGOPROXYDir := filepath.Join(t.TempDir(), "static-goproxy")
	setProxyHandler(func(rw http.ResponseWriter, req *http.Request) {
		http.FileServer(http.Dir(staticGOPROXYDir)).ServeHTTP(rw, req)
	})

	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	mod := "module example.com"
	for k, v := range map[string][]byte{
		filepath.Join(staticGOPROXYDir, "example.com", "@latest"):           []byte(marshalInfo("v1.1.0", infoTime)),
		filepath.Join(staticGOPROXYDir, "example.com", "@v", "list"):        []byte("v1.1.0\nv1.0.0"),
		filepath.Join(staticGOPROXYDir, "example.com", "@v", "v1.0.0.info"): []byte(marshalInfo("v1.0.0", infoTime)),
		filepath.Join(staticGOPROXYDir, "example.com", "@v", "v1.1.0.info"): []byte(marshalInfo("v1.1.0", infoTime.Add(time.Hour))),
		filepath.Join(staticGOPROXYDir, "example.com", "@v", "v1.0.0.mod"):  []byte(mod),
		filepath.Join(staticGOPROXYDir, "example.com", "@v", "v1.1.0.mod"):  []byte(mod),
	} {
		if err := os.MkdirAll(filepath.Dir(k), 0o755); err != nil {
			t.Fatalf("unexpected error %q", err)
		}
		if err := os.WriteFile(k, v, 0o644); err != nil {
			t.Fatalf("unexpected error %q", err)
		}
	}
	zipFile := filepath.Join(staticGOPROXYDir, "example.com", "@v", "v1.0.0.zip")
	if err := writeZipFile(zipFile, map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)}); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipFile2 := filepath.Join(staticGOPROXYDir, "example.com", "@v", "v1.1.0.zip")
	if err := writeZipFile(zipFile2, map[string][]byte{"example.com@v1.0.0/go.mod": []byte(mod)}); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	infoCacheFile := filepath.Join(gopathDir, "pkg", "mod", "cache", "download", "example.com", "@v", "v1.0.0.info")
	modCacheFile := filepath.Join(gopathDir, "pkg", "mod", "cache", "download", "example.com", "@v", "v1.0.0.mod")
	modCacheFile2 := filepath.Join(gopathDir, "pkg", "mod", "cache", "download", "example.com", "@v", "v1.1.0.mod")
	zipCacheFile := filepath.Join(gopathDir, "pkg", "mod", "cache", "download", "example.com", "@v", "v1.0.0.zip")

	dirHash, err := dirhash.HashZip(zipFile, dirhash.DefaultHash)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(mod)), nil
	})
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	skey, vkey, err := note.GenerateKey(nil, "sumdb.example.com")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	for _, tt := range []struct {
		n            int
		ctxTimeout   time.Duration
		sumdbHandler http.Handler
		name         string
		setupFetch   func(f *fetch) error
		wantVersion  string
		wantTime     time.Time
		wantVersions []string
		wantInfo     string
		wantGoMod    string
		wantZip      string
		wantError    error
	}{
		{
			n:           1,
			name:        "example.com/@latest",
			wantVersion: "v1.1.0",
			wantTime:    infoTime.Add(time.Hour),
			wantGoMod:   modCacheFile2,
		},
		{
			n:            2,
			name:         "example.com/@v/list",
			wantVersion:  "v1.1.0",
			wantTime:     infoTime.Add(time.Hour),
			wantVersions: []string{"v1.0.0", "v1.1.0"},
			wantGoMod:    modCacheFile2,
		},
		{
			n:           3,
			name:        "example.com/@v/v1.0.0.info",
			wantVersion: "v1.0.0",
			wantInfo:    infoCacheFile,
			wantGoMod:   modCacheFile,
			wantZip:     zipCacheFile,
		},
		{
			n:           4,
			name:        "example.com/@v/v1.0.0.mod",
			wantVersion: "v1.0.0",
			wantInfo:    infoCacheFile,
			wantGoMod:   modCacheFile,
			wantZip:     zipCacheFile,
		},
		{
			n:           5,
			name:        "example.com/@v/v1.0.0.zip",
			wantVersion: "v1.0.0",
			wantInfo:    infoCacheFile,
			wantGoMod:   modCacheFile,
			wantZip:     zipCacheFile,
		},
		{
			n: 6,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
				return []byte(gosum), nil
			})),
			name:        "example.com/@v/v1.0.0.info",
			wantVersion: "v1.0.0",
			wantInfo:    infoCacheFile,
			wantGoMod:   modCacheFile,
			wantZip:     zipCacheFile,
		},
		{
			n:         7,
			name:      "example.com/@v/v1.1.0.info",
			wantError: notExistErrorf("zip for example.com@v1.1.0 has unexpected file example.com@v1.0.0/go.mod"),
		},
		{
			n:          8,
			ctxTimeout: -1,
			name:       "example.com/@v/v1.0.0.info",
			wantError:  context.Canceled,
		},
		{
			n:          9,
			ctxTimeout: -time.Hour,
			name:       "example.com/@v/v1.0.0.info",
			wantError:  context.DeadlineExceeded,
		},
		{
			n:         10,
			name:      "example.com/@v/v1.2.0.info",
			wantError: fs.ErrNotExist,
		},
		{
			n:    11,
			name: "example.com/@v/v1.0.0.info",
			setupFetch: func(f *fetch) error {
				f.modAtVer = "invalid"
				return nil
			},
			wantError: fs.ErrNotExist,
		},
		{
			n: 12,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, modHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, dirHash)
				return []byte(gosum), nil
			})),
			name:      "example.com/@v/v1.0.0.info",
			wantError: notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
		{
			n: 13,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, modHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
				return []byte(gosum), nil
			})),
			name:      "example.com/@v/v1.0.0.info",
			wantError: notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
	} {
		ctx := context.Background()
		switch tt.ctxTimeout {
		case 0:
		case -1:
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		default:
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
			defer cancel()
		}
		envGOSUMDB := "off"
		if tt.sumdbHandler != nil {
			setSumDBHandler(tt.sumdbHandler.ServeHTTP)
			envGOSUMDB = vkey + " " + sumdbServer.URL
		}
		g := &Goproxy{
			Env: append(
				os.Environ(),
				"GOPATH="+gopathDir,
				"GOPROXY=off",
				"GOSUMDB="+envGOSUMDB,
			),
			MaxDirectFetches: 1,
		}
		g.init()
		g.env = append(g.env, "GOPROXY="+proxyServer.URL)
		f, err := newFetch(g, tt.name, t.TempDir())
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		if tt.setupFetch != nil {
			if err := tt.setupFetch(f); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
		fr, err := f.doDirect(ctx)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if tt.wantError != fs.ErrNotExist && errors.Is(tt.wantError, fs.ErrNotExist) {
				if got, want := err, tt.wantError; !errors.Is(got, fs.ErrNotExist) || got.Error() != want.Error() {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			} else if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := fr.Version, tt.wantVersion; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := fr.Time, tt.wantTime; !got.Equal(want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := strings.Join(fr.Versions, "\n"), strings.Join(tt.wantVersions, "\n"); got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := fr.Info, tt.wantInfo; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := fr.GoMod, tt.wantGoMod; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := fr.Zip, tt.wantZip; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}
}

func TestFetchOpsString(t *testing.T) {
	for _, tt := range []struct {
		n            int
		fo           fetchOps
		wantFetchOps string
	}{
		{1, fetchOpsQuery, "query"},
		{2, fetchOpsList, "list"},
		{3, fetchOpsDownload, "download"},
		{4, fetchOpsInvalid, "invalid"},
		{5, fetchOps(255), "invalid"},
	} {
		if got, want := tt.fo.String(), tt.wantFetchOps; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestMarshalInfo(t *testing.T) {
	got := marshalInfo("v1.0.0", time.Date(2000, 1, 1, 1, 0, 0, 1, time.FixedZone("", 3600)))
	want := `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00.000000001Z"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnmarshalInfo(t *testing.T) {
	for _, tt := range []struct {
		n           int
		info        string
		wantVersion string
		wantTime    time.Time
		wantError   error
	}{
		{
			n:           1,
			info:        `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
			wantVersion: "v1.0.0",
			wantTime:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			n:           2,
			info:        `{"Version":"v1.0.0","Time":"2000-01-01T01:00:00+01:00"}`,
			wantVersion: "v1.0.0",
			wantTime:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			n:         3,
			wantError: errors.New("unexpected end of JSON input"),
		},
		{
			n:         4,
			info:      "{}",
			wantError: errors.New("empty version"),
		},
		{
			n:         5,
			info:      `{"Version":"v1.0.0"}`,
			wantError: errors.New("zero time"),
		},
	} {
		infoVersion, infoTime, err := unmarshalInfo(tt.info)
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
			if got, want := infoVersion, tt.wantVersion; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := infoTime, tt.wantTime; !infoTime.Equal(tt.wantTime) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}
}

func TestCheckAndFormatInfoFile(t *testing.T) {
	for _, tt := range []struct {
		n         int
		info      string
		wantInfo  string
		wantError error
	}{
		{
			n:        1,
			info:     `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
			wantInfo: `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
		},
		{
			n:        2,
			info:     `{"Version":"v1.0.0","Time":"2000-01-01T01:00:00+01:00"}`,
			wantInfo: `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
		},
		{
			n:         3,
			info:      "{}",
			wantError: notExistErrorf("invalid info file: empty version"),
		},
		{
			n:         4,
			info:      "",
			wantError: fs.ErrNotExist,
		},
	} {
		infoFile := filepath.Join(t.TempDir(), "info")
		if tt.info != "" {
			if err := os.WriteFile(infoFile, []byte(tt.info), 0o644); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
		err := checkAndFormatInfoFile(infoFile)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if tt.wantError != fs.ErrNotExist && errors.Is(tt.wantError, fs.ErrNotExist) {
				if got, want := err, tt.wantError; !errors.Is(got, fs.ErrNotExist) || got.Error() != want.Error() {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			} else if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if b, err := os.ReadFile(infoFile); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if got, want := string(b), tt.wantInfo; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}
}

func TestCheckModFile(t *testing.T) {
	for _, tt := range []struct {
		n         int
		mod       string
		wantError error
	}{
		{1, "module", nil},
		{2, "// foobar\nmodule foobar", nil},
		{3, "foobar", notExistErrorf("invalid mod file: missing module directive")},
		{4, "", fs.ErrNotExist},
	} {
		modFile := filepath.Join(t.TempDir(), "mod")
		if tt.mod != "" {
			if err := os.WriteFile(modFile, []byte(tt.mod), 0o644); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
		err := checkModFile(modFile)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if tt.wantError != fs.ErrNotExist && errors.Is(tt.wantError, fs.ErrNotExist) {
				if got, want := err, tt.wantError; !errors.Is(got, fs.ErrNotExist) || got.Error() != want.Error() {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			} else if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
	}
}

func TestVerifyModFile(t *testing.T) {
	sumdbServer, setSumDBHandler := newHTTPTestServer()
	defer sumdbServer.Close()

	modFile := filepath.Join(t.TempDir(), "mod")
	if err := os.WriteFile(modFile, []byte("module example.com"), 0o644); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	modFile2 := filepath.Join(t.TempDir(), "mod")
	if err := os.WriteFile(modFile2, []byte("module example.com/v2"), 0o644); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	skey, vkey, err := note.GenerateKey(nil, "example.com")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	setSumDBHandler(sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
		if modulePath == "example.com" && moduleVersion == "v1.0.0" {
			dirHash, err := dirhash.HashDir(t.TempDir(), "example.com@v1.0.0", dirhash.DefaultHash)
			if err != nil {
				return nil, err
			}
			modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) { return os.Open(modFile) })
			if err != nil {
				return nil, err
			}
			gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
			gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
			return []byte(gosum), nil
		}
		return nil, errors.New("unknown module version")
	})).ServeHTTP)

	g := &Goproxy{
		Env: []string{
			"GOPROXY=off",
			"GOSUMDB=" + vkey + " " + sumdbServer.URL,
		},
	}
	g.init()
	for _, tt := range []struct {
		n             int
		modFile       string
		modulePath    string
		moduleVersion string
		wantError     error
	}{
		{
			n:             1,
			modFile:       modFile,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			n:             2,
			modFile:       modFile,
			modulePath:    "example.com",
			moduleVersion: "v1.1.0",
			wantError:     errors.New("example.com@v1.1.0/go.mod: bad upstream"),
		},
		{
			n:             3,
			modFile:       filepath.Join(t.TempDir(), "404"),
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantError:     fs.ErrNotExist,
		},
		{
			n:             4,
			modFile:       modFile2,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantError:     notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
	} {
		err := verifyModFile(g.sumdbClient, tt.modFile, tt.modulePath, tt.moduleVersion)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if tt.wantError != fs.ErrNotExist && errors.Is(tt.wantError, fs.ErrNotExist) {
				if got, want := err, tt.wantError; !errors.Is(got, fs.ErrNotExist) || got.Error() != want.Error() {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			} else if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
	}
}

func writeZipFile(name string, files map[string][]byte) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for k, v := range files {
		w, err := zw.Create(k)
		if err != nil {
			return err
		}
		if _, err := w.Write(v); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

func TestCheckZipFile(t *testing.T) {
	zipFile := filepath.Join(t.TempDir(), "zip")
	if err := writeZipFile(zipFile, map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")}); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	for _, tt := range []struct {
		n             int
		zipFile       string
		modulePath    string
		moduleVersion string
		wantError     error
	}{
		{
			n:             1,
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			n:             2,
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.1.0",
			wantError:     notExistErrorf(`invalid zip file: example.com@v1.0.0/go.mod: path does not have prefix "example.com@v1.1.0/"`),
		},
		{
			n:             3,
			zipFile:       filepath.Join(t.TempDir(), "404"),
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantError:     fs.ErrNotExist,
		},
	} {
		err := checkZipFile(tt.zipFile, tt.modulePath, tt.moduleVersion)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if tt.wantError != fs.ErrNotExist && errors.Is(tt.wantError, fs.ErrNotExist) {
				if got, want := err, tt.wantError; !errors.Is(got, fs.ErrNotExist) || got.Error() != want.Error() {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			} else if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
	}
}

func TestVerifyZipFile(t *testing.T) {
	sumdbServer, setSumDBHandler := newHTTPTestServer()
	defer sumdbServer.Close()

	zipFile := filepath.Join(t.TempDir(), "zip")
	if err := writeZipFile(zipFile, map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")}); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipFile2 := filepath.Join(t.TempDir(), "zip")
	if err := writeZipFile(zipFile2, map[string][]byte{"example.com/v2@v2.0.0/go.mod": []byte("module example.com/v2")}); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	skey, vkey, err := note.GenerateKey(nil, "example.com")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	setSumDBHandler(sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
		if modulePath == "example.com" && moduleVersion == "v1.0.0" {
			dirHash, err := dirhash.HashZip(zipFile, dirhash.DefaultHash)
			if err != nil {
				return nil, err
			}
			modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("example.com@v1.0.0/go.mod")), nil
			})
			if err != nil {
				return nil, err
			}
			gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
			gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
			return []byte(gosum), nil
		}
		return nil, errors.New("unknown module version")
	})).ServeHTTP)

	g := &Goproxy{
		Env: []string{
			"GOPROXY=off",
			"GOSUMDB=" + vkey + " " + sumdbServer.URL,
		},
	}
	g.init()
	for _, tt := range []struct {
		n             int
		zipFile       string
		modulePath    string
		moduleVersion string
		wantError     error
	}{
		{
			n:             1,
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			n:             2,
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.1.0",
			wantError:     errors.New("example.com@v1.1.0: bad upstream"),
		},
		{
			n:             3,
			zipFile:       filepath.Join(t.TempDir(), "404"),
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantError:     fs.ErrNotExist,
		},
		{
			n:             4,
			zipFile:       zipFile2,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantError:     notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
	} {
		err := verifyZipFile(g.sumdbClient, tt.zipFile, tt.modulePath, tt.moduleVersion)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if tt.wantError != fs.ErrNotExist && errors.Is(tt.wantError, fs.ErrNotExist) {
				if got, want := err, tt.wantError; !errors.Is(got, fs.ErrNotExist) || got.Error() != want.Error() {
					t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
				}
			} else if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
	}
}
