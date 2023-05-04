package goproxy

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
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
		wantOps              FetchOps
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
			wantOps:              FetchOpsResolve,
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
			wantOps:              FetchOpsResolve,
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
			wantOps:              FetchOpsResolve,
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
			wantOps:              FetchOpsResolve,
			wantModulePath:       "example.com",
			wantModuleVersion:    "latest",
			wantModAtVer:         "example.com@latest",
			wantRequiredToVerify: false,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:                    5,
			name:                 "example.com/@v/list",
			wantOps:              FetchOpsList,
			wantModulePath:       "example.com",
			wantModuleVersion:    "latest",
			wantModAtVer:         "example.com@latest",
			wantRequiredToVerify: true,
			wantContentType:      "text/plain; charset=utf-8",
		},
		{
			n:                    6,
			name:                 "example.com/@v/v1.0.0.info",
			wantOps:              FetchOpsDownloadInfo,
			wantModulePath:       "example.com",
			wantModuleVersion:    "v1.0.0",
			wantModAtVer:         "example.com@v1.0.0",
			wantRequiredToVerify: true,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:                    7,
			name:                 "example.com/@v/v1.0.0.mod",
			wantOps:              FetchOpsDownloadMod,
			wantModulePath:       "example.com",
			wantModuleVersion:    "v1.0.0",
			wantModAtVer:         "example.com@v1.0.0",
			wantRequiredToVerify: true,
			wantContentType:      "text/plain; charset=utf-8",
		},
		{
			n:                    8,
			name:                 "example.com/@v/v1.0.0.zip",
			wantOps:              FetchOpsDownloadZip,
			wantModulePath:       "example.com",
			wantModuleVersion:    "v1.0.0",
			wantModAtVer:         "example.com@v1.0.0",
			wantRequiredToVerify: true,
			wantContentType:      "application/zip",
		},
		{
			n:         9,
			name:      "example.com/@v/v1.0.0.ext",
			wantError: errors.New(`unexpected extension ".ext"`),
		},
		{
			n:         10,
			name:      "example.com/@v/latest.info",
			wantError: errors.New("invalid version"),
		},
		{
			n:                    11,
			name:                 "example.com/@v/master.info",
			wantOps:              FetchOpsResolve,
			wantModulePath:       "example.com",
			wantModuleVersion:    "master",
			wantModAtVer:         "example.com@master",
			wantRequiredToVerify: true,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:         12,
			name:      "example.com/@v/master.mod",
			wantError: errors.New("unrecognized version"),
		},
		{
			n:         13,
			name:      "example.com/@v/master.zip",
			wantError: errors.New("unrecognized version"),
		},
		{
			n:         14,
			name:      "example.com",
			wantError: errors.New("missing /@v/"),
		},
		{
			n:         15,
			name:      "example.com/@v/",
			wantError: errors.New(`no file extension in filename ""`),
		},
		{
			n:         16,
			name:      "example.com/@v/main",
			wantError: errors.New(`no file extension in filename "main"`),
		},
		{
			n:                    17,
			name:                 "example.com/!foobar/@v/!v1.0.0.info",
			wantOps:              FetchOpsResolve,
			wantModulePath:       "example.com/Foobar",
			wantModuleVersion:    "V1.0.0",
			wantModAtVer:         "example.com/Foobar@V1.0.0",
			wantRequiredToVerify: true,
			wantContentType:      "application/json; charset=utf-8",
		},
		{
			n:         18,
			name:      "example.com/!!foobar/@latest",
			wantError: errors.New(`invalid escaped module path "example.com/!!foobar"`),
		},
		{
			n:         19,
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
				t.Errorf("test(%d): got %v, want %v", tt.n, got, want)
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
		wantContent  string
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
			wantContent: marshalInfo("v1.0.0", infoTime),
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
			wantError: NotFoundError(fmt.Sprintf("module example.com: reading %s/example.com/@v/list: 404 Not Found\n\tserver response: not found", proxyServer.URL)),
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
			wantError: NotFoundError("example.com@latest: module lookup disabled by GOPROXY=off"),
		},
		{
			n:            4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			env: []string{
				"GOPROXY=off",
				"GOSUMDB=off",
			},
			name:      "example.com/@latest",
			wantError: NotFoundError("module lookup disabled by GOPROXY=off"),
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
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if rsc, err := fr.Open(); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if b, err := io.ReadAll(rsc); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if err := rsc.Close(); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
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
	sumdbServer, setSUMDBHandler := newHTTPTestServer()
	defer sumdbServer.Close()

	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	modFile := filepath.Join(t.TempDir(), "mod")
	if err := os.WriteFile(modFile, []byte("module example.com"), 0o644); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipFile := filepath.Join(t.TempDir(), "zip")
	if err := writeZipFile(zipFile, map[string][]byte{"example.com@v1.2.0/go.mod": []byte("module example.com")}); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zip, err := os.ReadFile(zipFile)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipFile2 := filepath.Join(t.TempDir(), "zip")
	if err := writeZipFile(zipFile2, map[string][]byte{"example.com@v1.3.0/go.mod": []byte("module example.com")}); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	dirHash, err := dirhash.HashDir(t.TempDir(), "example.com@v1.0.0", dirhash.DefaultHash)
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

	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		sumdbHandler http.Handler
		name         string
		tempDir      string
		proxy        string
		wantContent  string
		wantVersion  string
		wantTime     time.Time
		wantVersions []string
		wantError    error
	}{
		{
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(marshalInfo("v1.0.0", infoTime)), "application/json; charset=utf-8", 60)
			},
			name:        "example.com/@latest",
			tempDir:     t.TempDir(),
			proxy:       proxyServer.URL,
			wantContent: marshalInfo("v1.0.0", infoTime),
			wantVersion: "v1.0.0",
			wantTime:    infoTime,
		},
		{
			n: 2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(marshalInfo("v1.0.0", time.Time{})), "application/json; charset=utf-8", 60)
			},
			name:      "example.com/@latest",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: NotFoundError("invalid info response: zero time"),
		},
		{
			n: 3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(`v1.0.0
v1.1.0
v1.1.1-0.20200101000000-0123456789ab
v1.2.0 foo bar
invalid
`), "text/plain; charset=utf-8", 60)
			},
			name:         "example.com/@v/list",
			tempDir:      t.TempDir(),
			proxy:        proxyServer.URL,
			wantContent:  "v1.0.0\nv1.1.0\nv1.2.0",
			wantVersions: []string{"v1.0.0", "v1.1.0", "v1.2.0"},
		},
		{
			n: 4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(marshalInfo("v1.0.0", infoTime)), "application/json; charset=utf-8", 60)
			},
			name:        "example.com/@v/v1.0.0.info",
			tempDir:     t.TempDir(),
			proxy:       proxyServer.URL,
			wantContent: marshalInfo("v1.0.0", infoTime),
		},
		{
			n: 5,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(marshalInfo("v1.0.0", time.Time{})), "application/json; charset=utf-8", 60)
			},
			name:      "example.com/@v/v1.0.0.info",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: NotFoundError("invalid info file: zero time"),
		},
		{
			n: 6,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader("module example.com"), "text/plain; charset=utf-8", 60)
			},
			name:        "example.com/@v/v1.0.0.mod",
			tempDir:     t.TempDir(),
			proxy:       proxyServer.URL,
			wantContent: "module example.com",
		},
		{
			n: 7,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader("module example.com"), "text/plain; charset=utf-8", 60)
			},
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
				return []byte(gosum), nil
			})),
			name:        "example.com/@v/v1.0.0.mod",
			tempDir:     t.TempDir(),
			proxy:       proxyServer.URL,
			wantContent: "module example.com",
		},
		{
			n: 8,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader("module example.com"), "text/plain; charset=utf-8", 60)
			},
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, "v1.0.0", dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, "v1.0.0", modHash)
				return []byte(gosum), nil
			})),
			name:      "example.com/@v/v1.1.0.mod",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: NotFoundError("example.com@v1.1.0: invalid version: untrusted revision v1.1.0"),
		},
		{
			n: 9,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader("Go 1.13\n"), "text/plain; charset=utf-8", 60)
			},
			name:      "example.com/@v/v1.0.0.mod",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: NotFoundError("invalid mod file: missing module directive"),
		},
		{
			n: 10,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				f, err := os.Open(zipFile)
				if err != nil {
					t.Fatalf("unexpected error %q", err)
				}
				defer f.Close()
				responseSuccess(rw, req, f, "application/zip", 60)
			},
			name:        "example.com/@v/v1.2.0.zip",
			tempDir:     t.TempDir(),
			proxy:       proxyServer.URL,
			wantContent: string(zip),
		},
		{
			n: 11,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				f, err := os.Open(zipFile)
				if err != nil {
					t.Fatalf("unexpected error %q", err)
				}
				defer f.Close()
				responseSuccess(rw, req, f, "application/zip", 60)
			},
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				dirHash, err = dirhash.HashZip(zipFile, dirhash.DefaultHash)
				if err != nil {
					return nil, err
				}
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
				return []byte(gosum), nil
			})),
			name:        "example.com/@v/v1.2.0.zip",
			tempDir:     t.TempDir(),
			proxy:       proxyServer.URL,
			wantContent: string(zip),
		},
		{
			n: 12,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				f, err := os.Open(zipFile2)
				if err != nil {
					t.Fatalf("unexpected error %q", err)
				}
				defer f.Close()
				responseSuccess(rw, req, f, "application/zip", 60)
			},
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				dirHash, err = dirhash.HashZip(zipFile, dirhash.DefaultHash)
				if err != nil {
					return nil, err
				}
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, "v1.2.0", dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, "v1.2.0", modHash)
				return []byte(gosum), nil
			})),
			name:      "example.com/@v/v1.3.0.zip",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: NotFoundError("example.com@v1.3.0: invalid version: untrusted revision v1.3.0"),
		},
		{
			n: 13,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader("I'm a ZIP file!"), "application/zip", 60)
			},
			name:      "example.com/@v/v1.0.0.zip",
			tempDir:   t.TempDir(),
			proxy:     proxyServer.URL,
			wantError: NotFoundError("invalid zip file: zip: not a valid zip file"),
		},
		{
			n:            14,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, 60) },
			name:         "example.com/@latest",
			tempDir:      t.TempDir(),
			proxy:        proxyServer.URL,
			wantError:    errNotFound,
		},
		{
			n:         15,
			name:      "example.com/@latest",
			tempDir:   t.TempDir(),
			proxy:     "://invalid",
			wantError: errors.New(`parse "://invalid": missing protocol scheme`),
		},
		{
			n:         16,
			name:      "example.com/@latest",
			tempDir:   filepath.Join(t.TempDir(), "404"),
			proxy:     proxyServer.URL,
			wantError: fs.ErrNotExist,
		},
	} {
		setProxyHandler(tt.proxyHandler)
		envGOSUMDB := "off"
		if tt.sumdbHandler != nil {
			setSUMDBHandler(tt.sumdbHandler.ServeHTTP)
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
		fr, err := f.doProxy(context.Background(), tt.proxy)
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
			if rsc, err := fr.Open(); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if b, err := io.ReadAll(rsc); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if err := rsc.Close(); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
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
		}
	}
}

func TestFetchDoDirect(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	sumdbServer, setSUMDBHandler := newHTTPTestServer()
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
	zip, err := os.ReadFile(zipFile)
	if err != nil {
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
		wantContent  string
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
			wantContent: marshalInfo("v1.1.0", infoTime.Add(time.Hour)),
			wantVersion: "v1.1.0",
			wantTime:    infoTime.Add(time.Hour),
			wantGoMod:   modCacheFile2,
		},
		{
			n:            2,
			name:         "example.com/@v/list",
			wantContent:  "v1.0.0\nv1.1.0",
			wantVersion:  "v1.1.0",
			wantTime:     infoTime.Add(time.Hour),
			wantVersions: []string{"v1.0.0", "v1.1.0"},
			wantGoMod:    modCacheFile2,
		},
		{
			n:           3,
			name:        "example.com/@v/v1.0.0.info",
			wantContent: marshalInfo("v1.0.0", infoTime),
			wantVersion: "v1.0.0",
			wantInfo:    infoCacheFile,
			wantGoMod:   modCacheFile,
			wantZip:     zipCacheFile,
		},
		{
			n:           4,
			name:        "example.com/@v/v1.0.0.mod",
			wantContent: mod,
			wantVersion: "v1.0.0",
			wantInfo:    infoCacheFile,
			wantGoMod:   modCacheFile,
			wantZip:     zipCacheFile,
		},
		{
			n:           5,
			name:        "example.com/@v/v1.0.0.zip",
			wantContent: string(zip),
			wantVersion: "v1.0.0",
			wantInfo:    infoCacheFile,
			wantGoMod:   modCacheFile,
			wantZip:     zipCacheFile,
		},
		{
			n:         6,
			name:      "example.com/@v/v1.1.0.info",
			wantError: NotFoundError("zip for example.com@v1.1.0 has unexpected file example.com@v1.0.0/go.mod"),
		},
		{
			n:          7,
			ctxTimeout: -1,
			name:       "example.com/@v/v1.0.0.info",
			wantError:  context.Canceled,
		},
		{
			n:          8,
			ctxTimeout: -time.Hour,
			name:       "example.com/@v/v1.0.0.info",
			wantError:  context.DeadlineExceeded,
		},
		{
			n:         9,
			name:      "example.com/@v/v1.2.0.info",
			wantError: errNotFound,
		},
		{
			n:    10,
			name: "example.com/@v/v1.0.0.info",
			setupFetch: func(f *fetch) error {
				f.modAtVer = "invalid"
				return nil
			},
			wantError: errNotFound,
		},
		{
			n: 11,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
				return []byte(gosum), nil
			})),
			name:        "example.com/@v/v1.0.0.info",
			wantContent: marshalInfo("v1.0.0", infoTime),
			wantVersion: "v1.0.0",
			wantInfo:    infoCacheFile,
			wantGoMod:   modCacheFile,
			wantZip:     zipCacheFile,
		},
		{
			n: 12,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, modHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, dirHash)
				return []byte(gosum), nil
			})),
			name:      "example.com/@v/v1.0.0.info",
			wantError: NotFoundError("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
		{
			n: 13,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, modHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
				return []byte(gosum), nil
			})),
			name:      "example.com/@v/v1.0.0.info",
			wantError: NotFoundError("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
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
			setSUMDBHandler(tt.sumdbHandler.ServeHTTP)
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
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if rsc, err := fr.Open(); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if b, err := io.ReadAll(rsc); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if err := rsc.Close(); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
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
		fo           FetchOps
		wantFetchOps string
	}{
		{1, FetchOpsResolve, "resolve"},
		{2, FetchOpsList, "list"},
		{3, FetchOpsDownloadInfo, "download info"},
		{4, FetchOpsDownloadMod, "download mod"},
		{5, FetchOpsDownloadZip, "download zip"},
		{6, FetchOpsInvalid, "invalid"},
		{7, FetchOps(255), "invalid"},
	} {
		if got, want := tt.fo.String(), tt.wantFetchOps; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestFetchResultOpen(t *testing.T) {
	for _, tt := range []struct {
		n                int
		fr               *FetchResult
		setupFetchResult func(fr *FetchResult) error
		wantContent      string
		wantError        error
	}{
		{
			n:           1,
			fr:          &FetchResult{f: &Fetch{ops: FetchOpsResolve}, Version: "v1.0.0", Time: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
			wantContent: `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
		},
		{
			n:           2,
			fr:          &FetchResult{f: &Fetch{ops: FetchOpsList}, Versions: []string{"v1.0.0", "v1.1.0"}},
			wantContent: "v1.0.0\nv1.1.0",
		},
		{
			n:                3,
			fr:               &FetchResult{f: &Fetch{ops: FetchOpsDownloadInfo}, Info: filepath.Join(t.TempDir(), "info")},
			setupFetchResult: func(fr *FetchResult) error { return os.WriteFile(fr.Info, []byte("info"), 0o644) },
			wantContent:      "info",
		},
		{
			n:                4,
			fr:               &FetchResult{f: &Fetch{ops: FetchOpsDownloadMod}, GoMod: filepath.Join(t.TempDir(), "mod")},
			setupFetchResult: func(fr *FetchResult) error { return os.WriteFile(fr.GoMod, []byte("mod"), 0o644) },
			wantContent:      "mod",
		},
		{
			n:                5,
			fr:               &FetchResult{f: &Fetch{ops: FetchOpsDownloadZip}, Zip: filepath.Join(t.TempDir(), "zip")},
			setupFetchResult: func(fr *FetchResult) error { return os.WriteFile(fr.Zip, []byte("zip"), 0o644) },
			wantContent:      "zip",
		},
		{
			n:         6,
			fr:        &FetchResult{f: &Fetch{ops: FetchOpsInvalid}},
			wantError: errors.New("invalid fetch operation"),
		},
	} {
		if tt.setupFetchResult != nil {
			if err := tt.setupFetchResult(tt.fr); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
		rsc, err := tt.fr.Open()
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
			if b, err := io.ReadAll(rsc); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
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
			n:         1,
			wantError: errors.New("unexpected end of JSON input"),
		},
		{
			n:         2,
			info:      "{}",
			wantError: errors.New("empty version"),
		},
		{
			n:         3,
			info:      `{"Version":"v1.0.0"}`,
			wantError: errors.New("zero time"),
		},
		{
			n:           4,
			info:        `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
			wantVersion: "v1.0.0",
			wantTime:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			n:           5,
			info:        `{"Version":"v1.0.0","Time":"2000-01-01T01:00:00+01:00"}`,
			wantVersion: "v1.0.0",
			wantTime:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
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
			n:         1,
			info:      "{}",
			wantError: NotFoundError("invalid info file: empty version"),
		},
		{
			n:        2,
			info:     `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
			wantInfo: `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
		},
		{
			n:        3,
			info:     `{"Version":"v1.0.0","Time":"2000-01-01T01:00:00+01:00"}`,
			wantInfo: `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
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
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
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
		{1, "foobar", NotFoundError("invalid mod file: missing module directive")},
		{2, "module", nil},
		{3, "// foobar\nmodule foobar", nil},
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
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
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
	sumdbServer, setSUMDBHandler := newHTTPTestServer()
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
	setSUMDBHandler(sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
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
			wantError:     NotFoundError("example.com@v1.1.0/go.mod: bad upstream"),
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
			wantError:     NotFoundError("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
	} {
		err := verifyModFile(g.SumdbClient, tt.modFile, tt.modulePath, tt.moduleVersion)
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
	}
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
			wantError:     NotFoundError(`invalid zip file: example.com@v1.0.0/go.mod: path does not have prefix "example.com@v1.1.0/"`),
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
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
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
	sumdbServer, setSUMDBHandler := newHTTPTestServer()
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
	setSUMDBHandler(sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
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
			wantError:     NotFoundError("example.com@v1.1.0: bad upstream"),
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
			wantError:     NotFoundError("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
	} {
		err := verifyZipFile(g.SumdbClient, tt.zipFile, tt.modulePath, tt.moduleVersion)
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
