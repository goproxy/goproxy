package goproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/sumdb/note"
)

func TestGoFetcherInit(t *testing.T) {
	for _, tt := range []struct {
		n                int
		env              []string
		wantEnvGOPROXY   string
		wantEnvGONOPROXY string
		wantInitErr      error
	}{
		{
			n:              1,
			wantEnvGOPROXY: defaultEnvGOPROXY,
		},
		{
			n:              2,
			env:            append(os.Environ(), "GOPROXY=https://example.com"),
			wantEnvGOPROXY: "https://example.com",
		},
		{
			n:                3,
			env:              append(os.Environ(), "GOPRIVATE=example.com"),
			wantEnvGOPROXY:   defaultEnvGOPROXY,
			wantEnvGONOPROXY: "example.com",
		},
		{
			n: 4,
			env: append(
				os.Environ(),
				"GOPRIVATE=example.com",
				"GONOPROXY=alt1.example.com",
				"GONOSUMDB=alt2.example.com",
			),
			wantEnvGOPROXY:   defaultEnvGOPROXY,
			wantEnvGONOPROXY: "alt1.example.com",
		},
		{
			n:           5,
			env:         append(os.Environ(), "GOPROXY=,"),
			wantInitErr: errors.New("GOPROXY list is not the empty string, but contains no entries"),
		},
		{
			n:           6,
			env:         append(os.Environ(), "GOSUMDB=foobar"),
			wantInitErr: errors.New("invalid GOSUMDB: malformed verifier id"),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			gf := &GoFetcher{
				Env:              tt.env,
				MaxDirectFetches: 10,
				TempDir:          t.TempDir(),
				Transport:        http.DefaultTransport,
			}
			gf.initOnce.Do(gf.init)
			if tt.wantInitErr != nil {
				if gf.initErr == nil {
					t.Fatal("expected error")
				}
				if got, want := gf.initErr, tt.wantInitErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if gf.initErr != nil {
					t.Fatalf("unexpected error %v", gf.initErr)
				}
				if got, want := getenv(gf.env, "PATH"), os.Getenv("PATH"); got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := gf.envGOPROXY, tt.wantEnvGOPROXY; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := gf.envGONOPROXY, tt.wantEnvGONOPROXY; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := getenv(gf.env, "GOSUMDB"), "off"; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := getenv(gf.env, "GONOSUMDB"), ""; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := getenv(gf.env, "GOPRIVATE"), ""; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if gf.directFetchWorkerPool == nil {
					t.Error("unexpected nil")
				} else if got, want := cap(gf.directFetchWorkerPool), 10; got != want {
					t.Errorf("got %d, want %d", got, want)
				}
				if gf.httpClient == nil {
					t.Error("unexpected nil")
				} else if got, want := gf.httpClient.Transport, http.DefaultTransport; got != want {
					t.Errorf("got %#v, want %#v", got, want)
				}
				if gf.sumdbClient == nil {
					t.Error("unexpected nil")
				}
			}
		})
	}
}

func TestGoFetcherSkipProxy(t *testing.T) {
	for _, tt := range []struct {
		n             int
		envGONOPROXY  string
		path          string
		wantSkipProxy bool
	}{
		{
			n:             1,
			path:          "example.com/foobar",
			wantSkipProxy: false,
		},
		{
			n:             2,
			envGONOPROXY:  "example.com",
			path:          "example.com/foobar",
			wantSkipProxy: true,
		},
		{
			n:             3,
			envGONOPROXY:  "*.example.com",
			path:          "corp.example.com/foobar",
			wantSkipProxy: true,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			gf := &GoFetcher{Env: append(os.Environ(), "GONOPROXY="+tt.envGONOPROXY), TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			if got, want := gf.skipProxy(tt.path), tt.wantSkipProxy; got != want {
				t.Errorf("got %t, want %t", got, want)
			}
		})
	}
}

func TestGoFetcherQuery(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())

	infoVersion := "v1.0.0"
	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	info := marshalInfo(infoVersion, infoTime)
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
	}

	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		env          func(proxyServerURL string) []string
		path         string
		wantVersion  string
		wantTime     time.Time
		wantErr      error
	}{
		{
			n: 1,
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL)
			},
			path:        "example.com",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			n: 2,
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL, "GONOPROXY=example.com")
			},
			path:        "example.com",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			n: 3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/direct/example.com/@v/list":
					responseSuccess(rw, req, strings.NewReader(infoVersion), "text/plain; charset=utf-8", -2)
				case "/direct/example.com/@v/v1.0.0.info":
					responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
				default:
					responseNotFound(rw, req, -2)
				}
			},
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL+",direct")
			},
			path:        "example.com",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			n:       4,
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			n: 5,
			env: func(_ string) []string {
				return append(os.Environ(), "GOSUMDB=foobar")
			},
			wantErr: errors.New("invalid GOSUMDB: malformed verifier id"),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.proxyHandler == nil {
				tt.proxyHandler = proxyHandler
			}
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)

			var env []string
			if tt.env != nil {
				env = tt.env(proxyServer.URL)
			}

			gf := &GoFetcher{Env: env, TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			gf.env = append(gf.env, "GOPROXY="+proxyServer.URL+"/direct/")

			version, time, err := gf.Query(context.Background(), tt.path, "latest")
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := version, tt.wantVersion; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := time, tt.wantTime; !time.Equal(tt.wantTime) {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestGoFetcherProxyQuery(t *testing.T) {
	infoVersion := "v1.0.0"
	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	info := marshalInfo(infoVersion, infoTime)
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
	}
	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		path         string
		query        string
		wantVersion  string
		wantTime     time.Time
		wantErr      error
	}{
		{
			n:           1,
			path:        "example.com",
			query:       "latest",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			n:           2,
			path:        "example.com",
			query:       "v1",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			path:         "example.com",
			query:        "latest",
			wantErr:      notExistErrorf("not found"),
		},
		{
			n:            4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
			path:         "example.com",
			query:        "latest",
			wantErr:      notExistErrorf("invalid info response: unexpected end of JSON input"),
		},
		{
			n:       5,
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			n:       6,
			path:    "example.com",
			wantErr: errors.New(`version "" invalid: disallowed version string`),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.proxyHandler == nil {
				tt.proxyHandler = proxyHandler
			}
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)

			gf := &GoFetcher{TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			proxy, err := url.Parse(proxyServer.URL)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			version, time, err := gf.proxyQuery(context.Background(), tt.path, tt.query, proxy)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := version, tt.wantVersion; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := time, tt.wantTime; !time.Equal(tt.wantTime) {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestGoFetcherDirectQuery(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())

	infoVersion := "v1.0.0"
	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	info := marshalInfo(infoVersion, infoTime)
	proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
	}))

	for _, tt := range []struct {
		n           int
		path        string
		wantVersion string
		wantTime    time.Time
		wantErr     error
	}{
		{
			n:           1,
			path:        "example.com",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			n:       2,
			path:    "foobar",
			wantErr: errors.New(`foobar@latest: malformed module path "foobar": missing dot in first path element`),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			gf := &GoFetcher{TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}
			gf.env = append(gf.env, "GOPROXY="+proxyServer.URL)

			version, time, err := gf.directQuery(context.Background(), tt.path, "latest")
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := version, tt.wantVersion; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := time, tt.wantTime; !time.Equal(tt.wantTime) {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestGoFetcherList(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())

	list := "v1.0.0\nv1.1.0"
	info := marshalInfo("v1.1.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		switch strings.TrimPrefix(req.URL.Path, "/direct") {
		case "/example.com/@v/list":
			responseSuccess(rw, req, strings.NewReader(list), "text/plain; charset=utf-8", -2)
		case "/example.com/@v/v1.1.0.info":
			responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
		case "/example.com/@v/v1.1.0.mod":
			responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", -2)
		default:
			responseNotFound(rw, req, -2)
		}
	}

	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		env          func(proxyServerURL string) []string
		path         string
		wantVersions []string
		wantErr      error
	}{
		{
			n: 1,
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL)
			},
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			n: 2,
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL, "GONOPROXY=example.com")
			},
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			n: 3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if !strings.HasPrefix(req.URL.Path, "/direct") {
					responseNotFound(rw, req, -2)
				} else {
					proxyHandler(rw, req)
				}
			},
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL+",direct")
			},
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			n: 4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				responseSuccess(rw, req, strings.NewReader(`
v1.0.0
v1.1.0 foo bar
v1.1.1-0.20200101000000-0123456789ab
invalid
`), "text/plain; charset=utf-8", -2)
			},
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL)
			},
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			n:       5,
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			n: 6,
			env: func(_ string) []string {
				return append(os.Environ(), "GOSUMDB=foobar")
			},
			wantErr: errors.New("invalid GOSUMDB: malformed verifier id"),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.proxyHandler == nil {
				tt.proxyHandler = proxyHandler
			}
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)

			var env []string
			if tt.env != nil {
				env = tt.env(proxyServer.URL)
			}

			gf := &GoFetcher{Env: env, TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			gf.env = append(gf.env, "GOPROXY="+proxyServer.URL+"/direct/")

			versions, err := gf.List(context.Background(), tt.path)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := strings.Join(versions, "\n"), strings.Join(tt.wantVersions, "\n"); got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestGoFetcherProxyList(t *testing.T) {
	list := "v1.0.0\nv1.1.0"
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(list), "text/plain; charset=utf-8", -2)
	}
	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		path         string
		wantVersions []string
		wantErr      error
	}{
		{
			n:            1,
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			n:            2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			path:         "example.com",
			wantErr:      notExistErrorf("not found"),
		},
		{
			n:       3,
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.proxyHandler == nil {
				tt.proxyHandler = proxyHandler
			}
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)

			gf := &GoFetcher{TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			proxy, err := url.Parse(proxyServer.URL)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			versions, err := gf.proxyList(context.Background(), tt.path, proxy)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := strings.Join(versions, "\n"), strings.Join(tt.wantVersions, "\n"); got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestGoFetcherDirectList(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())

	list := "v1.0.0\nv1.1.0"
	info := marshalInfo("v1.1.0", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/example.com/@v/list":
			responseSuccess(rw, req, strings.NewReader(list), "text/plain; charset=utf-8", -2)
		case "/example.com/@v/v1.1.0.info":
			responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
		case "/example.com/@v/v1.1.0.mod":
			responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", -2)
		default:
			responseNotFound(rw, req, -2)
		}
	}))

	for _, tt := range []struct {
		n            int
		path         string
		wantVersions []string
		wantErr      error
	}{
		{
			n:            1,
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			n:       2,
			path:    "foobar",
			wantErr: errors.New(`foobar@latest: malformed module path "foobar": missing dot in first path element`),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			gf := &GoFetcher{TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}
			gf.env = append(gf.env, "GOPROXY="+proxyServer.URL)

			versions, err := gf.directList(context.Background(), tt.path)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := strings.Join(versions, "\n"), strings.Join(tt.wantVersions, "\n"); got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestGoFetcherDownload(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOFLAGS", "-modcacherw")

	infoVersion := "v1.0.0"
	info := marshalInfo(infoVersion, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		switch strings.TrimPrefix(req.URL.Path, "/direct") {
		case "/example.com/@v/v1.0.0.info":
			responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
		case "/example.com/@v/v1.0.0.mod":
			responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", -2)
		case "/example.com/@v/v1.0.0.zip":
			responseSuccess(rw, req, bytes.NewReader(zip), "application/zip", -2)
		default:
			responseNotFound(rw, req, -2)
		}
	}

	zipFile, err := makeTempFile(t, zip)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	dirHash, err := dirhash.HashZip(zipFile, dirhash.DefaultHash)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(mod)), nil })
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	skey, vkey, err := note.GenerateKey(nil, "sumdb.example.com")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	sumdbHandler := sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
		gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
		gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
		return []byte(gosum), nil
	})).ServeHTTP

	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		sumdbHandler http.HandlerFunc
		env          func(proxyServerURL, sumdbServerURL string) []string
		path         string
		version      string
		wantInfo     string
		wantMod      string
		wantZip      string
		wantErr      error
	}{
		{
			n: 1,
			env: func(proxyServerURL, sumdbServerURL string) []string {
				return append(
					os.Environ(),
					"GOPROXY="+proxyServerURL,
					"GOSUMDB="+vkey+" "+sumdbServerURL,
				)
			},
			path:     "example.com",
			version:  infoVersion,
			wantInfo: info,
			wantMod:  mod,
			wantZip:  string(zip),
		},
		{
			n: 2,
			env: func(proxyServerURL, sumdbServerURL string) []string {
				return append(
					os.Environ(),
					"GOPROXY="+proxyServerURL,
					"GONOPROXY=example.com",
					"GOSUMDB="+vkey+" "+sumdbServerURL,
				)
			},
			path:     "example.com",
			version:  infoVersion,
			wantInfo: info,
			wantMod:  mod,
			wantZip:  string(zip),
		},
		{
			n: 3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if !strings.HasPrefix(req.URL.Path, "/direct") {
					responseNotFound(rw, req, -2)
				} else {
					proxyHandler(rw, req)
				}
			},
			env: func(proxyServerURL, sumdbServerURL string) []string {
				return append(
					os.Environ(),
					"GOPROXY="+proxyServerURL+",direct",
					"GOSUMDB="+vkey+" "+sumdbServerURL,
				)
			},
			path:     "example.com",
			version:  infoVersion,
			wantInfo: info,
			wantMod:  mod,
			wantZip:  string(zip),
		},
		{
			n: 4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/example.com/@v/v1.0.0.info" {
					responseSuccess(rw, req, strings.NewReader(""), "application/json; charset=utf-8", -2)
				} else {
					proxyHandler(rw, req)
				}
			},
			env: func(proxyServerURL, _ string) []string {
				return append(
					os.Environ(),
					"GOPROXY="+proxyServerURL,
					"GOSUMDB=off",
				)
			},
			path:    "example.com",
			version: infoVersion,
			wantErr: notExistErrorf("invalid info file: unexpected end of JSON input"),
		},
		{
			n: 5,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/example.com/@v/v1.0.0.mod" {
					responseSuccess(rw, req, strings.NewReader(""), "text/plain; charset=utf-8", -2)
				} else {
					proxyHandler(rw, req)
				}
			},
			env: func(proxyServerURL, _ string) []string {
				return append(
					os.Environ(),
					"GOPROXY="+proxyServerURL,
					"GOSUMDB=off",
				)
			},
			path:    "example.com",
			version: infoVersion,
			wantErr: notExistErrorf("invalid mod file: missing module directive"),
		},
		{
			n: 6,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/example.com/@v/v1.0.0.zip" {
					responseSuccess(rw, req, strings.NewReader(""), "application/json", -2)
				} else {
					proxyHandler(rw, req)
				}
			},
			env: func(proxyServerURL, _ string) []string {
				return append(
					os.Environ(),
					"GOPROXY="+proxyServerURL,
					"GOSUMDB=off",
				)
			},
			path:    "example.com",
			version: infoVersion,
			wantErr: notExistErrorf("invalid zip file: zip: not a valid zip file"),
		},
		{
			n: 7,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, "v1.1.0", modHash)
				return []byte(gosum), nil
			})).ServeHTTP,
			env: func(proxyServerURL, sumdbServerURL string) []string {
				return append(
					os.Environ(),
					"GOPROXY="+proxyServerURL,
					"GOSUMDB="+vkey+" "+sumdbServerURL,
				)
			},
			path:    "example.com",
			version: infoVersion,
			wantErr: notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
		{
			n: 8,
			sumdbHandler: sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
				gosum := fmt.Sprintf("%s %s %s\n", modulePath, "v1.1.0", dirHash)
				gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
				return []byte(gosum), nil
			})).ServeHTTP,
			env: func(proxyServerURL, sumdbServerURL string) []string {
				return append(
					os.Environ(),
					"GOPROXY="+proxyServerURL,
					"GOSUMDB="+vkey+" "+sumdbServerURL,
				)
			},
			path:    "example.com",
			version: infoVersion,
			wantErr: notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
		{
			n:       9,
			path:    "example.com",
			version: "v1",
			wantErr: errors.New("example.com@v1: invalid version: not a canonical version"),
		},
		{
			n:       10,
			path:    "example.com",
			version: "v1.0",
			wantErr: errors.New("example.com@v1.0: invalid version: not a canonical version"),
		},
		{
			n:       11,
			path:    "example.com",
			version: "master",
			wantErr: errors.New("example.com@master: invalid version: not a semantic version"),
		},
		{
			n:       12,
			path:    "example.com",
			version: "v2.0.0",
			wantErr: errors.New("example.com@v2.0.0: invalid version: should be v0 or v1, not v2"),
		},
		{
			n:       13,
			path:    "foobar",
			version: infoVersion,
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			n: 14,
			env: func(_, _ string) []string {
				return append(os.Environ(), "GOSUMDB=foobar")
			},
			wantErr: errors.New("invalid GOSUMDB: malformed verifier id"),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.proxyHandler == nil {
				tt.proxyHandler = proxyHandler
			}
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)

			if tt.sumdbHandler == nil {
				tt.sumdbHandler = sumdbHandler
			}
			sumdbServer := newHTTPTestServer(t, tt.sumdbHandler)

			var env []string
			if tt.env != nil {
				env = tt.env(proxyServer.URL, sumdbServer.URL)
			}

			gf := &GoFetcher{Env: env, TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			gf.env = append(gf.env, "GOPROXY="+proxyServer.URL+"/direct/")

			info, mod, zip, err := gf.Download(context.Background(), tt.path, tt.version)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if b, err := io.ReadAll(info); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if err := info.Close(); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantInfo; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if b, err := io.ReadAll(mod); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if err := mod.Close(); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantMod; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if b, err := io.ReadAll(zip); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if err := zip.Close(); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantZip; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if des, err := os.ReadDir(gf.TempDir); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := len(des), 0; got != want {
					t.Errorf("got %d, want %d", got, want)
				}
			}
		})
	}
}

func TestGoFetcherProxyDownload(t *testing.T) {
	infoVersion := "v1.0.0"
	info := marshalInfo(infoVersion, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/example.com/@v/v1.0.0.info":
			responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
		case "/example.com/@v/v1.0.0.mod":
			responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", -2)
		case "/example.com/@v/v1.0.0.zip":
			responseSuccess(rw, req, bytes.NewReader(zip), "application/zip", -2)
		default:
			responseNotFound(rw, req, -2)
		}
	}
	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		tempDir      string
		path         string
		version      string
		wantInfo     string
		wantMod      string
		wantZip      string
		wantErr      error
	}{
		{
			n:        1,
			path:     "example.com",
			version:  infoVersion,
			wantInfo: info,
			wantMod:  mod,
			wantZip:  string(zip),
		},
		{
			n:            2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			path:         "example.com",
			version:      infoVersion,
			wantErr:      notExistErrorf("not found"),
		},
		{
			n: 3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/example.com/@v/v1.0.0.mod" {
					responseNotFound(rw, req, -2)
				} else {
					proxyHandler(rw, req)
				}
			},
			path:    "example.com",
			version: infoVersion,
			wantErr: notExistErrorf("not found"),
		},
		{
			n: 4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/example.com/@v/v1.0.0.zip" {
					responseNotFound(rw, req, -2)
				} else {
					proxyHandler(rw, req)
				}
			},
			path:    "example.com",
			version: infoVersion,
			wantErr: notExistErrorf("not found"),
		},
		{
			n:       5,
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			n:       6,
			path:    "example.com",
			wantErr: errors.New(`version "" invalid: disallowed version string`),
		},
		{
			n:       7,
			tempDir: filepath.Join(t.TempDir(), "404"),
			path:    "example.com",
			version: infoVersion,
			wantErr: fs.ErrNotExist,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.proxyHandler == nil {
				tt.proxyHandler = proxyHandler
			}
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)
			if tt.tempDir == "" {
				tt.tempDir = t.TempDir()
			}

			gf := &GoFetcher{TempDir: tt.tempDir}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			proxy, err := url.Parse(proxyServer.URL)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			infoFile, modFile, zipFile, cleanup, err := gf.proxyDownload(context.Background(), tt.path, tt.version, proxy)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if b, err := os.ReadFile(infoFile); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantInfo; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if b, err := os.ReadFile(modFile); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantMod; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if b, err := os.ReadFile(zipFile); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantZip; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if cleanup == nil {
					t.Fatal("unexpected nil")
				}
				cleanup()
				if _, err := os.Stat(infoFile); err == nil {
					t.Error("expected error")
				} else if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if _, err := os.Stat(modFile); err == nil {
					t.Error("expected error")
				} else if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if _, err := os.Stat(zipFile); err == nil {
					t.Error("expected error")
				} else if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			}
		})
	}
}

func TestGoFetcherDirectDownload(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOFLAGS", "-modcacherw")

	infoVersion := "v1.0.0"
	info := marshalInfo(infoVersion, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/example.com/@v/v1.0.0.info":
			responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
		case "/example.com/@v/v1.0.0.mod":
			responseSuccess(rw, req, strings.NewReader(mod), "text/plain; charset=utf-8", -2)
		case "/example.com/@v/v1.0.0.zip":
			responseSuccess(rw, req, bytes.NewReader(zip), "application/zip", -2)
		default:
			responseNotFound(rw, req, -2)
		}
	}))

	for _, tt := range []struct {
		n        int
		path     string
		wantInfo string
		wantMod  string
		wantZip  string
		wantErr  error
	}{
		{
			n:        1,
			path:     "example.com",
			wantInfo: info,
			wantMod:  mod,
			wantZip:  string(zip),
		},
		{
			n:       2,
			path:    "foobar",
			wantErr: errors.New(`foobar@v1.0.0: malformed module path "foobar": missing dot in first path element`),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			gf := &GoFetcher{TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}
			gf.env = append(gf.env, "GOPROXY="+proxyServer.URL)

			infoFile, modFile, zipFile, err := gf.directDownload(context.Background(), tt.path, infoVersion)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if b, err := os.ReadFile(infoFile); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantInfo; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if b, err := os.ReadFile(modFile); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantMod; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if b, err := os.ReadFile(zipFile); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantZip; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

type misbehavingDoneContext struct{}

func (misbehavingDoneContext) Deadline() (deadline time.Time, ok bool) { return time.Time{}, false }
func (misbehavingDoneContext) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (misbehavingDoneContext) Err() error        { return nil }
func (misbehavingDoneContext) Value(key any) any { return nil }

func TestGoFetcherExecGo(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())

	ctxCanceled, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tt := range []struct {
		n          int
		ctx        context.Context
		env        []string
		goBin      string
		tempDir    string
		args       []string
		wantOutput string
		wantErr    error
	}{
		{
			n:          1,
			ctx:        context.Background(),
			args:       []string{"env", "GOPROXY"},
			wantOutput: "direct\n",
		},
		{
			n:       2,
			ctx:     context.Background(),
			args:    []string{"foobar"},
			wantErr: errors.New("go foobar: unknown command\nRun 'go help' for usage."),
		},
		{
			n:       3,
			ctx:     context.Background(),
			args:    []string{"mod", "download", "-json", "foobar@latest"},
			wantErr: errors.New(`foobar@latest: malformed module path "foobar": missing dot in first path element`),
		},
		{
			n:       4,
			ctx:     ctxCanceled,
			wantErr: context.Canceled,
		},
		{
			n:       5,
			ctx:     &misbehavingDoneContext{},
			wantErr: errors.New("exec: not started"),
		},
		{
			n:       6,
			ctx:     context.Background(),
			tempDir: filepath.Join(t.TempDir(), "404"),
			wantErr: fs.ErrNotExist,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if tt.tempDir == "" {
				tt.tempDir = t.TempDir()
			}

			gf := &GoFetcher{
				GoBin:            tt.goBin,
				MaxDirectFetches: 1,
				TempDir:          tt.tempDir,
			}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			output, err := gf.execGo(tt.ctx, tt.args...)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := string(output), tt.wantOutput; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestCleanEnvGOPROXY(t *testing.T) {
	for _, tt := range []struct {
		n              int
		envGOPROXY     string
		wantEnvGOPROXY string
		wantErr        error
	}{
		{
			n:              1,
			wantEnvGOPROXY: defaultEnvGOPROXY,
		},
		{
			n:              2,
			envGOPROXY:     defaultEnvGOPROXY,
			wantEnvGOPROXY: defaultEnvGOPROXY,
		},
		{
			n:              3,
			envGOPROXY:     "https://example.com",
			wantEnvGOPROXY: "https://example.com",
		},
		{
			n:              4,
			envGOPROXY:     "https://example.com,",
			wantEnvGOPROXY: "https://example.com",
		},
		{
			n:              5,
			envGOPROXY:     "https://example.com|",
			wantEnvGOPROXY: "https://example.com",
		},
		{
			n:              6,
			envGOPROXY:     "https://example.com|https://backup.example.com,direct",
			wantEnvGOPROXY: "https://example.com|https://backup.example.com,direct",
		},
		{
			n:              7,
			envGOPROXY:     "https://example.com,direct,https://backup.example.com",
			wantEnvGOPROXY: "https://example.com,direct",
		},
		{
			n:              8,
			envGOPROXY:     "https://example.com,off,https://backup.example.com",
			wantEnvGOPROXY: "https://example.com,off",
		},
		{
			n:          9,
			envGOPROXY: "://invalid",
			wantErr:    errors.New(`invalid GOPROXY URL: parse "://invalid": missing protocol scheme`),
		},
		{
			n:          10,
			envGOPROXY: ",",
			wantErr:    errors.New("GOPROXY list is not the empty string, but contains no entries"),
		},
		{
			n:          11,
			envGOPROXY: "|",
			wantErr:    errors.New("GOPROXY list is not the empty string, but contains no entries"),
		},
		{
			n:          12,
			envGOPROXY: " ",
			wantErr:    errors.New("GOPROXY list is not the empty string, but contains no entries"),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			envGOPROXY, err := cleanEnvGOPROXY(tt.envGOPROXY)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := envGOPROXY, tt.wantEnvGOPROXY; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestWalkEnvGOPROXY(t *testing.T) {
	for _, tt := range []struct {
		n            int
		envGOPROXY   string
		onProxy      func(proxy *url.URL) error
		wantOnProxy  string
		wantOnDirect bool
		wantErr      error
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
			n:          4,
			envGOPROXY: "https://example.com,direct",
			onProxy:    func(proxy *url.URL) error { return errors.New("foobar") },
			wantErr:    errors.New("foobar"),
		},
		{
			n:          5,
			envGOPROXY: "https://example.com",
			onProxy:    func(proxy *url.URL) error { return fs.ErrNotExist },
			wantErr:    fs.ErrNotExist,
		},
		{
			n:            6,
			envGOPROXY:   "direct",
			wantOnDirect: true,
		},
		{
			n:            7,
			envGOPROXY:   "direct,off",
			wantOnDirect: true,
		},
		{
			n:          8,
			envGOPROXY: "off",
			wantErr:    errors.New("module lookup disabled by GOPROXY=off"),
		},
		{
			n:          9,
			envGOPROXY: "off,direct",
			wantErr:    errors.New("module lookup disabled by GOPROXY=off"),
		},
		{
			n:       10,
			wantErr: errors.New("missing GOPROXY"),
		},
		{
			n:          11,
			envGOPROXY: "://invalid",
			wantErr:    errors.New(`parse "://invalid": missing protocol scheme`),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
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
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := onProxy, tt.wantOnProxy; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := onDirect, tt.wantOnDirect; got != want {
					t.Errorf("got %t, want %t", got, want)
				}
			}
		})
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
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if got, want := cleanEnvGOSUMDB(tt.envGOSUMDB), tt.wantEnvGOSUMDB; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
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
		wantErr         error
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
			n:       6,
			wantErr: errors.New("missing GOSUMDB"),
		},
		{
			n:          7,
			envGOSUMDB: " ",
			wantErr:    errors.New("missing GOSUMDB"),
		},
		{
			n:          8,
			envGOSUMDB: "a b c",
			wantErr:    errors.New("invalid GOSUMDB: too many fields"),
		},
		{
			n:          9,
			envGOSUMDB: "example.com",
			wantErr:    errors.New("invalid GOSUMDB: malformed verifier id"),
		},
		{
			n:          10,
			envGOSUMDB: "example.com/+1a6413ba+AW5WXiP8oUq7RI2AuI4Wh14FJrMqJqnAplQ0kcLbnbqK",
			wantErr:    fmt.Errorf("invalid sumdb name (must be host[/path]): example.com/ %+v", url.URL{Scheme: "https", Host: "example.com", Path: "/"}),
		},
		{
			n:          11,
			envGOSUMDB: defaultEnvGOSUMDB + " ://invalid",
			wantErr:    errors.New(`invalid GOSUMDB URL: parse "://invalid": missing protocol scheme`),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			name, key, u, isDirectURL, err := parseEnvGOSUMDB(tt.envGOSUMDB)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := name, tt.wantName; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := key, tt.wantKey; got != want {
					t.Errorf("got %x, want %x", got, want)
				}
				if got, want := u.String(), tt.wantURL; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := isDirectURL, tt.wantIsDirectURL; got != want {
					t.Errorf("got %t, want %t", got, want)
				}
			}
		})
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
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if got, want := cleanCommaSeparatedList(tt.list), tt.wantList; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestCheckCanonicalVersion(t *testing.T) {
	for _, tt := range []struct {
		n       int
		path    string
		version string
		wantErr error
	}{
		{
			n:       1,
			path:    "example.com",
			version: "v1.0.0",
		},
		{
			n:       2,
			path:    "example.com",
			version: "v1",
			wantErr: errors.New("example.com@v1: invalid version: not a canonical version"),
		},
		{
			n:       3,
			path:    "example.com",
			version: "v1.0",
			wantErr: errors.New("example.com@v1.0: invalid version: not a canonical version"),
		},
		{
			n:       4,
			path:    "example.com",
			version: "master",
			wantErr: errors.New("example.com@master: invalid version: not a semantic version"),
		},
		{
			n:       5,
			path:    "example.com",
			version: "v2.0.0",
			wantErr: errors.New("example.com@v2.0.0: invalid version: should be v0 or v1, not v2"),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			err := checkCanonicalVersion(tt.path, tt.version)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
		})
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
		wantErr     error
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
			n:           3,
			info:        `{"Version":"v1.0.0","Time":"0001-01-01T00:00:00Z"}`,
			wantVersion: "v1.0.0",
			wantTime:    time.Time{},
		},
		{
			n:       4,
			info:    "{}",
			wantErr: errors.New("invalid version"),
		},
		{
			n:       5,
			wantErr: errors.New("unexpected end of JSON input"),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			infoVersion, infoTime, err := unmarshalInfo(tt.info)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := infoVersion, tt.wantVersion; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := infoTime, tt.wantTime; !infoTime.Equal(tt.wantTime) {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestUnmarshalInfoFile(t *testing.T) {
	infoVersion := "v1.0.0"
	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	info := marshalInfo(infoVersion, infoTime)
	infoFile, err := makeTempFile(t, []byte(info))
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	infoFileInvalid, err := makeTempFile(t, nil)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	for _, tt := range []struct {
		n           int
		infoFile    string
		wantVersion string
		wantTime    time.Time
		wantErr     error
	}{
		{
			n:           1,
			infoFile:    infoFile,
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			n:        2,
			infoFile: infoFileInvalid,
			wantErr:  notExistErrorf("invalid info file: unexpected end of JSON input"),
		},
		{
			n:        3,
			infoFile: filepath.Join(t.TempDir(), "404"),
			wantErr:  fs.ErrNotExist,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			infoVersion, infoTime, err := unmarshalInfoFile(tt.infoFile)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := infoVersion, tt.wantVersion; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := infoTime, tt.wantTime; !infoTime.Equal(tt.wantTime) {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestCheckModFile(t *testing.T) {
	for _, tt := range []struct {
		n       int
		mod     string
		wantErr error
	}{
		{1, "module", nil},
		{2, "// foobar\nmodule foobar", nil},
		{3, "foobar", notExistErrorf("invalid mod file: missing module directive")},
		{4, "", fs.ErrNotExist},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			var modFile string
			if tt.mod != "" {
				var err error
				modFile, err = makeTempFile(t, []byte(tt.mod))
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
			}

			err := checkModFile(modFile)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}

func TestVerifyModFile(t *testing.T) {
	modFile, err := makeTempFile(t, []byte("module example.com"))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	modFileInvalid, err := makeTempFile(t, nil)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	dirHash, err := dirhash.DefaultHash([]string{"example.com@v1.0.0/go.mod"}, func(string) (io.ReadCloser, error) { return os.Open(modFile) })
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) { return os.Open(modFile) })
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	skey, vkey, err := note.GenerateKey(nil, "example.com")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	sumdbServer := newHTTPTestServer(t, sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
		if modulePath == "example.com" && moduleVersion == "v1.0.0" {
			gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
			gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
			return []byte(gosum), nil
		}
		return nil, errors.New("unknown module version")
	})))
	for _, tt := range []struct {
		n             int
		env           []string
		modFile       string
		modulePath    string
		moduleVersion string
		wantErr       error
	}{
		{
			n:             1,
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			modFile:       modFile,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			n: 2,
			env: []string{
				"GOPROXY=off",
				"GOSUMDB=" + vkey + " " + sumdbServer.URL,
				"GONOSUMDB=example.com",
			},
			modFile:       modFileInvalid,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			n:             3,
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			modFile:       modFileInvalid,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
		{
			n:             4,
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			modFile:       modFile,
			modulePath:    "example.com",
			moduleVersion: "v1.1.0",
			wantErr:       errors.New("example.com@v1.1.0/go.mod: bad upstream"),
		},
		{
			n:             5,
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			modFile:       filepath.Join(t.TempDir(), "404"),
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       fs.ErrNotExist,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			gf := &GoFetcher{Env: tt.env, TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			err := verifyModFile(gf.sumdbClient, tt.modFile, tt.modulePath, tt.moduleVersion)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}

func TestCheckZipFile(t *testing.T) {
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	zipFile, err := makeTempFile(t, zip)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	for _, tt := range []struct {
		n             int
		zipFile       string
		modulePath    string
		moduleVersion string
		wantErr       error
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
			wantErr:       notExistErrorf(`invalid zip file: example.com@v1.0.0/go.mod: path does not have prefix "example.com@v1.1.0/"`),
		},
		{
			n:             3,
			zipFile:       filepath.Join(t.TempDir(), "404"),
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       fs.ErrNotExist,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			err := checkZipFile(tt.zipFile, tt.modulePath, tt.moduleVersion)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}

func TestVerifyZipFile(t *testing.T) {
	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	zipFile, err := makeTempFile(t, zip)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	zipInvalid, err := makeZip(map[string][]byte{"foo": []byte("bar")})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	zipFileInvalid, err := makeTempFile(t, zipInvalid)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	dirHash, err := dirhash.HashZip(zipFile, dirhash.DefaultHash)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("example.com@v1.0.0/go.mod")), nil
	})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	skey, vkey, err := note.GenerateKey(nil, "example.com")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	sumdbServer := newHTTPTestServer(t, sumdb.NewServer(sumdb.NewTestServer(skey, func(modulePath, moduleVersion string) ([]byte, error) {
		if modulePath == "example.com" && moduleVersion == "v1.0.0" {
			gosum := fmt.Sprintf("%s %s %s\n", modulePath, moduleVersion, dirHash)
			gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, moduleVersion, modHash)
			return []byte(gosum), nil
		}
		return nil, errors.New("unknown module version")
	})))
	for _, tt := range []struct {
		n             int
		env           []string
		zipFile       string
		modulePath    string
		moduleVersion string
		wantErr       error
	}{
		{
			n:             1,
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			n: 2,
			env: []string{
				"GOPROXY=off",
				"GOSUMDB=" + vkey + " " + sumdbServer.URL,
				"GONOSUMDB=example.com",
			},
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			n:             3,
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			zipFile:       zipFileInvalid,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       notExistErrorf("example.com@v1.0.0: invalid version: untrusted revision v1.0.0"),
		},
		{
			n:             4,
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.1.0",
			wantErr:       errors.New("example.com@v1.1.0: bad upstream"),
		},
		{
			n:             5,
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			zipFile:       filepath.Join(t.TempDir(), "404"),
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       fs.ErrNotExist,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			gf := &GoFetcher{Env: tt.env, TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			err := verifyZipFile(gf.sumdbClient, tt.zipFile, tt.modulePath, tt.moduleVersion)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}

func TestCloserFunc(t *testing.T) {
	var closed bool
	var closer io.Closer = closerFunc(func() error {
		closed = true
		return nil
	})
	if err := closer.Close(); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if got, want := closed, true; got != want {
		t.Errorf("got %t, want %t", got, want)
	}
}
