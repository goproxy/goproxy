package goproxy

import (
	archivezip "archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"testing/synctest"
	"time"

	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/zip"
)

func TestGoFetcherInit(t *testing.T) {
	for _, tt := range []struct {
		name             string
		env              []string
		wantEnvGOPROXY   string
		wantEnvGONOPROXY string
		wantEnvGOSUMDB   string
		wantEnvGONOSUMDB string
		wantInitErr      error
	}{
		{
			name:           "DefaultEnv",
			wantEnvGOPROXY: defaultEnvGOPROXY,
			wantEnvGOSUMDB: defaultEnvGOSUMDB,
		},
		{
			name:           "CustomGOPROXY",
			env:            append(os.Environ(), "GOPROXY=https://example.com"),
			wantEnvGOPROXY: "https://example.com",
			wantEnvGOSUMDB: defaultEnvGOSUMDB,
		},
		{
			name:             "GOPRIVATE",
			env:              append(os.Environ(), "GOPRIVATE=example.com"),
			wantEnvGOPROXY:   defaultEnvGOPROXY,
			wantEnvGONOPROXY: "example.com",
			wantEnvGOSUMDB:   defaultEnvGOSUMDB,
			wantEnvGONOSUMDB: "example.com",
		},
		{
			name: "GOPRIVATEWithOverrides",
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
		{
			name:        "InvalidGOPROXY",
			env:         append(os.Environ(), "GOPROXY=,"),
			wantInitErr: errors.New("GOPROXY list is not the empty string, but contains no entries"),
		},
		{
			name:        "InvalidGOSUMDB",
			env:         append(os.Environ(), "GOSUMDB=foobar"),
			wantInitErr: errors.New("invalid GOSUMDB: malformed verifier id"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gf := &GoFetcher{
				Env:                        tt.env,
				MaxConcurrentDirectFetches: 10,
				TempDir:                    t.TempDir(),
				Transport:                  http.DefaultTransport,
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
				if got, want := getenv(gf.env, "GOSUMDB"), tt.wantEnvGOSUMDB; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := getenv(gf.env, "GONOSUMDB"), tt.wantEnvGONOSUMDB; got != want {
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
				if gf.sumdbClientState == nil {
					t.Error("unexpected nil")
				}
			}
		})
	}

	t.Run("InvalidSumDBName", func(t *testing.T) {
		_, key, err := note.GenerateKey(nil, "example.com%")
		if err != nil {
			t.Fatal(err)
		}
		for _, tt := range []struct {
			name string
			call func(context.Context, *GoFetcher) error
		}{
			{"Query", func(ctx context.Context, gf *GoFetcher) error {
				_, _, err := gf.Query(ctx, "example.com", "latest")
				return err
			}},
			{"List", func(ctx context.Context, gf *GoFetcher) error {
				_, err := gf.List(ctx, "example.com")
				return err
			}},
			{"Download", func(ctx context.Context, gf *GoFetcher) error {
				_, _, _, err := gf.Download(ctx, "example.com", "v1.0.0")
				return err
			}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				gf := &GoFetcher{
					Env: []string{"GOSUMDB=" + key},
					Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						t.Error("unexpected upstream request")
						return nil, context.Canceled
					}),
				}
				err := tt.call(t.Context(), gf)
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, gf.initErr; got != want {
					t.Errorf("got %v, want initialization error %v", got, want)
				}
				if e, ok := errors.AsType[*url.Error](err); !ok || e.URL != "https://example.com%" {
					t.Errorf("got error %v, want URL parse error", err)
				}
			})
		}
	})
}

func TestGoFetcherSkipProxy(t *testing.T) {
	for _, tt := range []struct {
		name          string
		envGONOPROXY  string
		path          string
		wantSkipProxy bool
	}{
		{
			name:          "NoGONOPROXY",
			path:          "example.com/foobar",
			wantSkipProxy: false,
		},
		{
			name:          "PrefixMatch",
			envGONOPROXY:  "example.com",
			path:          "example.com/foobar",
			wantSkipProxy: true,
		},
		{
			name:          "WildcardMatch",
			envGONOPROXY:  "*.example.com",
			path:          "corp.example.com/foobar",
			wantSkipProxy: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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

	t.Run("RetryAfterFallback", func(t *testing.T) {
		for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			for _, tt := range []struct {
				name       string
				separator  string
				retryAfter string
				wantHosts  []string
				wantDelay  time.Duration
				wantErr    error
			}{
				{"Comma", ",", "60", []string{"proxy.example.com"}, 0, errBadUpstream},
				{"Pipe", "|", "60", []string{"proxy.example.com", "fallback.example.com"}, 0, nil},
				{"CommaExhausted", ",", "1", []string{"proxy.example.com", "proxy.example.com", "proxy.example.com"}, 2 * time.Second, errBadUpstream},
				{"PipeExhausted", "|", "1", []string{"proxy.example.com", "proxy.example.com", "proxy.example.com", "fallback.example.com"}, 2 * time.Second, nil},
			} {
				t.Run(strconv.Itoa(statusCode)+"/"+tt.name, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						var hosts []string
						gf := &GoFetcher{
							Env: []string{"GOPROXY=https://proxy.example.com" + tt.separator + "https://fallback.example.com", "GOSUMDB=off"},
							Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
								hosts = append(hosts, req.URL.Host)
								if req.URL.Host == "proxy.example.com" {
									return &http.Response{
										StatusCode: statusCode,
										Header:     http.Header{"Retry-After": {tt.retryAfter}},
										Body:       http.NoBody,
									}, nil
								}
								return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(info))}, nil
							}),
						}
						started := time.Now()
						version, timestamp, err := gf.Query(t.Context(), "example.com", "latest")
						if !errors.Is(err, tt.wantErr) {
							t.Errorf("got error %v, want %v", err, tt.wantErr)
						}
						if tt.wantErr == nil && (version != infoVersion || !timestamp.Equal(infoTime)) {
							t.Errorf("got version %q at %v, want %q at %v", version, timestamp, infoVersion, infoTime)
						}
						if !slices.Equal(hosts, tt.wantHosts) {
							t.Errorf("got hosts %q, want %q", hosts, tt.wantHosts)
						}
						if got, want := time.Since(started), tt.wantDelay; got != want {
							t.Errorf("got delay %v, want %v", got, want)
						}
					})
				})
			}
		}
	})

	t.Run("TransportErrorFallback", func(t *testing.T) {
		dnsErr := &net.DNSError{Err: "no such host", Name: "proxy.example.com", IsNotFound: true}
		for _, tt := range []struct {
			name      string
			separator string
			wantHosts []string
			wantErr   error
		}{
			{"Comma", ",", []string{"proxy.example.com"}, dnsErr},
			{"Pipe", "|", []string{"proxy.example.com", "fallback.example.com"}, nil},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var hosts []string
					gf := &GoFetcher{
						Env: []string{"GOPROXY=https://proxy.example.com" + tt.separator + "https://fallback.example.com", "GOSUMDB=off"},
						Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
							hosts = append(hosts, req.URL.Host)
							if req.URL.Host == "proxy.example.com" {
								return nil, &net.OpError{Op: "dial", Net: "tcp", Err: dnsErr}
							}
							return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(info))}, nil
						}),
					}
					started := time.Now()
					version, timestamp, err := gf.Query(t.Context(), "example.com", "latest")
					if !errors.Is(err, tt.wantErr) {
						t.Errorf("got error %v, want %v", err, tt.wantErr)
					}
					if tt.wantErr == nil && (version != infoVersion || !timestamp.Equal(infoTime)) {
						t.Errorf("got version %q at %v, want %q at %v", version, timestamp, infoVersion, infoTime)
					}
					if !slices.Equal(hosts, tt.wantHosts) {
						t.Errorf("got hosts %q, want %q", hosts, tt.wantHosts)
					}
					if got := time.Since(started); got != 0 {
						t.Errorf("got delay %v, want 0", got)
					}
				})
			})
		}
	})

	t.Run("InvalidInfoFallback", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			info string
		}{
			{"Empty", ""},
			{"MalformedJSON", "{"},
			{"NoncanonicalVersion", marshalInfo("v1", infoTime)},
			{"BuildMetadata", marshalInfo("v1.0.0+build", infoTime)},
			{"WrongMajor", marshalInfo("v2.0.0", infoTime)},
			{"WrongVersion", marshalInfo("v1.1.0", infoTime)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, fallback := range []struct {
					name      string
					separator string
				}{
					{"Comma", ","},
					{"Pipe", "|"},
				} {
					t.Run(fallback.name, func(t *testing.T) {
						var requests []string
						gf := &GoFetcher{
							Env: []string{"GOPROXY=https://proxy.example.com" + fallback.separator + "https://fallback.example.com", "GOSUMDB=off"},
							Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
								requests = append(requests, req.URL.Host)
								content := tt.info
								if req.URL.Host == "fallback.example.com" {
									content = info
								}
								return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(content))}, nil
							}),
						}
						version, time, err := gf.Query(t.Context(), "example.com", infoVersion)
						wantRequests := []string{"proxy.example.com"}
						if fallback.separator == "," {
							if err == nil {
								t.Fatal("expected error")
							}
							if !errors.Is(err, errBadUpstream) {
								t.Errorf("got error %v, want an error matching %v", err, errBadUpstream)
							}
							if errors.Is(err, fs.ErrNotExist) {
								t.Errorf("unexpected error matching %v: %v", fs.ErrNotExist, err)
							}
							if tt.name == "MalformedJSON" {
								if _, ok := errors.AsType[*json.SyntaxError](err); !ok {
									t.Errorf("got error %v, want a JSON syntax error", err)
								}
							}
						} else {
							wantRequests = append(wantRequests, "fallback.example.com")
							if err != nil {
								t.Fatalf("unexpected error %v", err)
							}
							if got, want := version, infoVersion; got != want {
								t.Errorf("got %q, want %q", got, want)
							}
							if got, want := time, infoTime; !got.Equal(want) {
								t.Errorf("got %q, want %q", got, want)
							}
						}
						if got, want := requests, wantRequests; !slices.Equal(got, want) {
							t.Errorf("got requests %q, want %q", got, want)
						}
					})
				}
			})
		}
	})

	for _, tt := range []struct {
		name         string
		proxyHandler http.HandlerFunc
		env          func(proxyServerURL string) []string
		path         string
		wantVersion  string
		wantTime     time.Time
		wantErr      error
	}{
		{
			name: "Proxy",
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL)
			},
			path:        "example.com",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			name: "SkipProxy",
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL, "GONOPROXY=example.com", "GOSUMDB=off")
			},
			path:        "example.com",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			name: "DirectFallback",
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
				return append(os.Environ(), "GOPROXY="+proxyServerURL+",direct", "GOSUMDB=off")
			},
			path:        "example.com",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			name:    "InvalidPath",
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			name: "InvalidGOSUMDB",
			env: func(_ string) []string {
				return append(os.Environ(), "GOSUMDB=foobar")
			},
			wantErr: errors.New("invalid GOSUMDB: malformed verifier id"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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

			version, time, err := gf.Query(t.Context(), tt.path, "latest")
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

	t.Run("LargeResponse", func(t *testing.T) {
		content := strings.TrimSuffix(info, "}") + `,"Extra":"` + strings.Repeat("x", 2<<20) + `"}`
		for _, tt := range []struct {
			name  string
			query string
		}{
			{"Version", infoVersion},
			{"VersionPrefix", "v1"},
			{"Branch", "main"},
			{"Latest", "latest"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				body := &testHTTPResponseBody{Reader: strings.NewReader(content)}
				gf := &GoFetcher{
					Env: []string{"GOSUMDB=off"},
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: body}, nil
					}),
				}
				gf.initOnce.Do(gf.init)
				if gf.initErr != nil {
					t.Fatal(gf.initErr)
				}
				proxy := &url.URL{Scheme: "https", Host: "proxy.example.com"}
				version, timestamp, err := gf.proxyQuery(t.Context(), "example.com", tt.query, proxy)
				if err != nil {
					t.Fatal(err)
				}
				if version != infoVersion || !timestamp.Equal(infoTime) {
					t.Errorf("got version %q at %v, want %q at %v", version, timestamp, infoVersion, infoTime)
				}
				if got, want := body.bytesRead, len(content); got != want {
					t.Errorf("read %d bytes, want %d", got, want)
				}
				if !body.closed {
					t.Error("response body was not closed")
				}
			})
		}
	})

	t.Run("InfoVersion", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			path    string
			query   string
			version string
			wantErr bool
		}{
			{"Latest", "example.com", "latest", "v1.0.0", false},
			{"MajorZero", "example.com", "latest", "v0.1.0", false},
			{"VersionPrefix", "example.com", "v1.2", "v1.2.3", false},
			{"Branch", "example.com", "main", "v1.2.3", false},
			{"PseudoVersion", "example.com", "main", "v0.0.0-20000101000000-0123456789ab", false},
			{"Prerelease", "example.com", "latest", "v1.0.0-RC.1", false},
			{"ExactVersion", "example.com", "v1.0.0", "v1.0.0", false},
			{"MajorSuffix", "example.com/v2", "latest", "v2.0.0", false},
			{"Incompatible", "example.com", "latest", "v2.0.0+incompatible", false},
			{"ExactIncompatible", "example.com", "v2.0.0+incompatible", "v2.0.0+incompatible", false},
			{"IncompatibleQuery", "example.com", "v2.0.0", "v2.0.0+incompatible", false},
			{"MetadataQuery", "example.com", "v1.0.0+build", "v1.0.0", false},
			{"GopkgIn", "gopkg.in/yaml.v2", "latest", "v2.4.0", false},
			{"GopkgInUnstable", "gopkg.in/yaml.v2-unstable", "latest", "v2.4.0", false},
			{"GopkgInLegacyPseudoVersion", "gopkg.in/check.v1", "latest", "v0.0.0-20161208181325-20d25e280405", false},
			{"MajorOnly", "example.com", "latest", "v1", true},
			{"MinorOnly", "example.com", "latest", "v1.2", true},
			{"BuildMetadata", "example.com", "latest", "v1.2.3+build", true},
			{"WrongMajor", "example.com", "latest", "v2.0.0", true},
			{"WrongMajorSuffix", "example.com/v2", "latest", "v1.0.0", true},
			{"WrongGopkgInMajor", "gopkg.in/yaml.v2", "latest", "v1.0.0", true},
			{"WrongVersion", "example.com", "v1.0.0", "v1.1.0", true},
			{"WrongIncompatibleVersion", "example.com", "v2.0.0+incompatible", "v2.1.0+incompatible", true},
			{"AddedIncompatibleSuffix", "example.com", "v1.0.0", "v1.0.0+incompatible", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					responseSuccess(rw, req, strings.NewReader(marshalInfo(tt.version, infoTime)), "application/json; charset=utf-8", -2)
				}))
				gf := &GoFetcher{TempDir: t.TempDir()}
				gf.initOnce.Do(gf.init)
				if gf.initErr != nil {
					t.Fatalf("unexpected error %v", gf.initErr)
				}
				proxy, err := url.Parse(proxyServer.URL)
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				version, time, err := gf.proxyQuery(t.Context(), tt.path, tt.query, proxy)
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error")
					}
					if !errors.Is(err, errBadUpstream) {
						t.Errorf("got error %v, want an error matching %v", err, errBadUpstream)
					}
					if errors.Is(err, fs.ErrNotExist) {
						t.Errorf("unexpected error matching %v: %v", fs.ErrNotExist, err)
					}
					if !strings.HasPrefix(err.Error(), "bad upstream: invalid info response: ") {
						t.Errorf("unexpected error %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := version, tt.version; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := time, infoTime; !got.Equal(want) {
					t.Errorf("got %q, want %q", got, want)
				}
			})
		}
	})

	t.Run("EscapedURLs", func(t *testing.T) {
		for _, prefix := range []struct {
			name     string
			path     string
			wantPath string
		}{
			{"Root", "", ""},
			{"Prefix", "/proxy", "/proxy"},
			{"TrailingSlash", "/proxy/", "/proxy"},
			{"EscapedPrefix", "/proxy%20path/%25/%2F", "/proxy path/%//"},
		} {
			t.Run(prefix.name, func(t *testing.T) {
				for _, tt := range []struct {
					name     string
					query    string
					wantPath string
				}{
					{"Latest", "latest", "/example.com/!project/@latest"},
					{"Version", "v1.0.0", "/example.com/!project/@v/v1.0.0.info"},
					{"ShortVersion", "v1", "/example.com/!project/@v/v1.info"},
					{"Branch", "main", "/example.com/!project/@v/main.info"},
					{"Uppercase", "Release", "/example.com/!project/@v/!release.info"},
					{"LiteralPercent", "release%", "/example.com/!project/@v/release%.info"},
					{"PercentOnly", "%", "/example.com/!project/@v/%.info"},
					{"RepeatedPercent", "release%%", "/example.com/!project/@v/release%%.info"},
					{"InvalidEscape", "release%zz", "/example.com/!project/@v/release%zz.info"},
					{"EscapedSlash", "release%2fchild", "/example.com/!project/@v/release%2fchild.info"},
					{"EscapedTraversal", "release%2f..%2f..%2fprivate", "/example.com/!project/@v/release%2f..%2f..%2fprivate.info"},
					{"EscapedNUL", "release%00", "/example.com/!project/@v/release%00.info"},
					{"DoubleEscape", "release%252fchild", "/example.com/!project/@v/release%252fchild.info"},
					{"UppercaseEscape", "Release%2Fchild", "/example.com/!project/@v/!release%2!fchild.info"},
					{"Fragment", "release#next", "/example.com/!project/@v/release#next.info"},
					{"Plus", "release+next", "/example.com/!project/@v/release+next.info"},
					{"Space", "release next", "/example.com/!project/@v/release next.info"},
				} {
					t.Run(tt.name, func(t *testing.T) {
						proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
							if got, want := req.URL.Path, prefix.wantPath+tt.wantPath; got != want {
								t.Errorf("got path %q, want %q", got, want)
							}
							if got, want := req.URL.EscapedPath(), strings.TrimSuffix(prefix.path, "/")+"/"; !strings.HasPrefix(got, want) {
								t.Errorf("got escaped path %q, want prefix %q", got, want)
							}
							if got, want := req.URL.RawQuery, "source=test"; got != want {
								t.Errorf("got query %q, want %q", got, want)
							}
							responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
						}))

						gf := &GoFetcher{TempDir: t.TempDir()}
						gf.initOnce.Do(gf.init)
						if gf.initErr != nil {
							t.Fatalf("unexpected error %v", gf.initErr)
						}

						proxyURL := proxyServer.URL + prefix.path + "?source=test"
						proxy, err := url.Parse(proxyURL)
						if err != nil {
							t.Fatalf("unexpected error %v", err)
						}
						version, time, err := gf.proxyQuery(t.Context(), "example.com/Project", tt.query, proxy)
						if err != nil {
							t.Fatalf("unexpected error %v", err)
						}
						if got, want := version, infoVersion; got != want {
							t.Errorf("got %q, want %q", got, want)
						}
						if got, want := time, infoTime; !got.Equal(want) {
							t.Errorf("got %q, want %q", got, want)
						}
						if got, want := proxy.String(), proxyURL; got != want {
							t.Errorf("got proxy URL %q, want %q", got, want)
						}
					})
				}
			})
		}
	})

	proxyHandler := func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(rw, req, strings.NewReader(info), "application/json; charset=utf-8", -2)
	}
	for _, tt := range []struct {
		name         string
		proxyHandler http.HandlerFunc
		path         string
		query        string
		wantVersion  string
		wantTime     time.Time
		wantErr      error
	}{
		{
			name:        "Latest",
			path:        "example.com",
			query:       "latest",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			name:        "VersionPrefix",
			path:        "example.com",
			query:       "v1",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			name:         "NotFound",
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			path:         "example.com",
			query:        "latest",
			wantErr:      notExistErrorf("not found"),
		},
		{
			name:         "EmptyResponse",
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
			path:         "example.com",
			query:        "latest",
			wantErr:      fmt.Errorf("%w: invalid info response: unexpected end of JSON input", errBadUpstream),
		},
		{
			name:    "InvalidPath",
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			name:    "EmptyQuery",
			path:    "example.com",
			wantErr: errors.New(`version "" invalid: disallowed version string`),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
			version, time, err := gf.proxyQuery(t.Context(), tt.path, tt.query, proxy)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if got, want := errors.Is(err, errBadUpstream), errors.Is(tt.wantErr, errBadUpstream); got != want {
					t.Errorf("got bad upstream %t, want %t", got, want)
				}
				if got, want := errors.Is(err, fs.ErrNotExist), errors.Is(tt.wantErr, fs.ErrNotExist); got != want {
					t.Errorf("got not exist %t, want %t", got, want)
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
		name        string
		path        string
		wantVersion string
		wantTime    time.Time
		wantErr     error
	}{
		{
			name:        "Success",
			path:        "example.com",
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			name:    "InvalidPath",
			path:    "foobar",
			wantErr: errors.New(`foobar@latest: malformed module path "foobar": missing dot in first path element`),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gf := &GoFetcher{Env: append(os.Environ(), "GOSUMDB=off"), TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}
			gf.env = append(gf.env, "GOPROXY="+proxyServer.URL)

			version, time, err := gf.directQuery(t.Context(), tt.path, "latest")
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
		name         string
		proxyHandler http.HandlerFunc
		env          func(proxyServerURL string) []string
		path         string
		wantVersions []string
		wantErr      error
	}{
		{
			name: "Proxy",
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL)
			},
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			name: "SkipProxy",
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL, "GONOPROXY=example.com", "GOSUMDB=off")
			},
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			name: "DirectFallback",
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if !strings.HasPrefix(req.URL.Path, "/direct") {
					responseNotFound(rw, req, -2)
				} else {
					proxyHandler(rw, req)
				}
			},
			env: func(proxyServerURL string) []string {
				return append(os.Environ(), "GOPROXY="+proxyServerURL+",direct", "GOSUMDB=off")
			},
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			name: "FilteredVersions",
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
			name:    "InvalidPath",
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			name: "InvalidGOSUMDB",
			env: func(_ string) []string {
				return append(os.Environ(), "GOSUMDB=foobar")
			},
			wantErr: errors.New("invalid GOSUMDB: malformed verifier id"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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

			versions, err := gf.List(t.Context(), tt.path)
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
		name         string
		proxyHandler http.HandlerFunc
		path         string
		wantVersions []string
		wantErr      error
	}{
		{
			name:         "Success",
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			name:         "NotFound",
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			path:         "example.com",
			wantErr:      notExistErrorf("not found"),
		},
		{
			name:    "InvalidPath",
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
			versions, err := gf.proxyList(t.Context(), tt.path, proxy)
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
		name         string
		path         string
		wantVersions []string
		wantErr      error
	}{
		{
			name:         "Success",
			path:         "example.com",
			wantVersions: []string{"v1.0.0", "v1.1.0"},
		},
		{
			name:    "InvalidPath",
			path:    "foobar",
			wantErr: errors.New(`foobar@latest: malformed module path "foobar": missing dot in first path element`),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gf := &GoFetcher{Env: append(os.Environ(), "GOSUMDB=off"), TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}
			gf.env = append(gf.env, "GOPROXY="+proxyServer.URL)

			versions, err := gf.directList(t.Context(), tt.path)
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
	zipContent, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")})
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
			responseSuccess(rw, req, bytes.NewReader(zipContent), "application/zip", -2)
		default:
			responseNotFound(rw, req, -2)
		}
	}

	t.Run("LargeInfo", func(t *testing.T) {
		content := info + strings.Repeat(" ", 2<<20)
		body := &testHTTPResponseBody{Reader: strings.NewReader(content)}
		gf := &GoFetcher{
			Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=off"},
			TempDir: t.TempDir(),
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, ".info") {
					return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: body}, nil
				}
				rec := httptest.NewRecorder()
				proxyHandler(rec, req)
				return rec.Result(), nil
			}),
		}
		infoContent, modContent, zipFileContent, err := gf.Download(t.Context(), "example.com", infoVersion)
		if err != nil {
			t.Fatal(err)
		}
		for _, tt := range []struct {
			content io.ReadSeekCloser
			want    string
		}{
			{infoContent, info},
			{modContent, mod},
			{zipFileContent, string(zipContent)},
		} {
			content, err := io.ReadAll(tt.content)
			if err != nil {
				t.Error(err)
			} else if got, want := string(content), tt.want; got != want {
				t.Errorf("got content %q, want %q", got, want)
			}
			if err := tt.content.Close(); err != nil {
				t.Error(err)
			}
		}
		if got, want := body.bytesRead, len(content); got != want {
			t.Errorf("read %d bytes, want %d", got, want)
		}
		if !body.closed {
			t.Error("response body was not closed")
		}
		if entries, err := os.ReadDir(gf.TempDir); err != nil {
			t.Fatal(err)
		} else if len(entries) != 0 {
			t.Errorf("unexpected temporary files %v", entries)
		}
	})

	t.Run("MissingDownloadedFile", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			ext  string
		}{
			{"Info", ".info"},
			{"Mod", ".mod"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var missing string
				gf := &GoFetcher{
					Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=off"},
					TempDir: t.TempDir(),
				}
				gf.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
					ext := filepath.Ext(req.URL.Path)
					if ext == tt.ext {
						files, err := filepath.Glob(filepath.Join(gf.TempDir, "*", "*"))
						if err != nil {
							return nil, err
						}
						for _, name := range files {
							fi, err := os.Stat(name)
							if err != nil {
								return nil, err
							}
							if fi.Size() == 0 {
								missing = name
							}
						}
						if missing == "" {
							t.Error("temporary file not found")
						}
					}
					if ext == ".zip" {
						if err := os.Remove(missing); err != nil {
							return nil, err
						}
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(map[string]string{".info": info, ".mod": mod, ".zip": string(zipContent)}[ext])),
						Request:    req,
					}, nil
				})
				info, mod, zip, err := gf.Download(t.Context(), "example.com", infoVersion)
				for _, content := range []io.ReadSeekCloser{info, mod, zip} {
					if content != nil {
						content.Close()
						t.Error("unexpected module content")
					}
				}
				if _, ok := errors.AsType[*internalError](err); !ok {
					t.Errorf("got error %v, want an internal error", err)
				}
				if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != missing {
					t.Errorf("got error %v, want a file error for %q", err, missing)
				}
				if !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("got error %v, want an error matching %v", err, fs.ErrNotExist)
				}
				if entries, err := os.ReadDir(gf.TempDir); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if len(entries) != 0 {
					t.Errorf("unexpected temporary files %v", entries)
				}
			})
		}
	})

	t.Run("InfoVersion", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			version string
		}{
			{"MajorOnly", "v1"},
			{"MinorOnly", "v1.0"},
			{"BuildMetadata", "v1.0.0+build"},
			{"WrongVersion", "v1.1.0"},
			{"WrongMajor", "v2.0.0"},
			{"Prerelease", "v1.0.0-rc.1"},
			{"IncompatibleSuffix", "v1.0.0+incompatible"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				proxyServer := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					if req.URL.Path == "/example.com/@v/v1.0.0.info" {
						responseSuccess(rw, req, strings.NewReader(marshalInfo(tt.version, time.Time{})), "application/json; charset=utf-8", -2)
					} else {
						proxyHandler(rw, req)
					}
				}))
				gf := &GoFetcher{
					Env:     append(os.Environ(), "GOPROXY="+proxyServer.URL, "GOSUMDB=off"),
					TempDir: t.TempDir(),
				}
				info, mod, zip, err := gf.Download(t.Context(), "example.com", infoVersion)
				for _, content := range []io.ReadSeekCloser{info, mod, zip} {
					if content != nil {
						content.Close()
						t.Error("unexpected module content")
					}
				}
				if err == nil {
					t.Fatal("expected error")
				}
				if !errors.Is(err, errBadUpstream) {
					t.Errorf("got error %v, want an error matching %v", err, errBadUpstream)
				}
				if errors.Is(err, fs.ErrNotExist) {
					t.Errorf("unexpected error matching %v: %v", fs.ErrNotExist, err)
				}
				if got, want := err.Error(), fmt.Sprintf("bad upstream: invalid info file: version %s does not match requested version %s", tt.version, infoVersion); got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if des, err := os.ReadDir(gf.TempDir); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := len(des), 0; got != want {
					t.Errorf("got %d, want %d", got, want)
				}
			})
		}
	})

	zipFile, err := makeTempFile(t, zipContent)
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

	t.Run("SumDBRecovery", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			pathPrefix string
			malformed  bool
		}{
			{"Lookup", "/lookup/", false},
			{"Tile", "/tile/", false},
			{"MalformedLookup", "/lookup/", true},
			{"MalformedTile", "/tile/", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var failing atomic.Bool
				failing.Store(true)
				var requests atomic.Int32
				gf := &GoFetcher{
					Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=" + vkey + " https://sumdb.example.com"},
					TempDir: t.TempDir(),
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						rec := httptest.NewRecorder()
						if req.URL.Host == "sumdb.example.com" {
							requests.Add(1)
							if failing.Load() && strings.HasPrefix(req.URL.Path, tt.pathPrefix) {
								if tt.malformed {
									if strings.HasPrefix(req.URL.Path, "/tile/") {
										sumdbHandler(rec, req)
										rec.Body.Bytes()[0] ^= 0x80
									} else {
										fmt.Fprint(rec, "invalid lookup response")
									}
								} else {
									rec.WriteHeader(http.StatusBadRequest)
								}
							} else {
								sumdbHandler(rec, req)
							}
						} else {
							proxyHandler(rec, req)
						}
						resp := rec.Result()
						resp.Request = req
						return resp, nil
					}),
				}
				download := func(wantErr bool) {
					t.Helper()
					info, mod, zip, err := gf.Download(t.Context(), "example.com", infoVersion)
					for _, content := range []io.ReadSeekCloser{info, mod, zip} {
						if content != nil {
							content.Close()
							if wantErr {
								t.Error("unexpected module content")
							}
						} else if !wantErr {
							t.Error("missing module content")
						}
					}
					if wantErr {
						if err == nil {
							t.Fatal("expected error")
						}
						if !errors.Is(err, errBadUpstream) {
							t.Errorf("got error %v, want a bad upstream error", err)
						}
						if errors.Is(err, fs.ErrNotExist) {
							t.Errorf("unexpected error matching %v: %v", fs.ErrNotExist, err)
						}
					} else if err != nil {
						t.Fatalf("unexpected error %v", err)
					}
					if entries, err := os.ReadDir(gf.TempDir); err != nil {
						t.Fatal(err)
					} else if len(entries) != 0 {
						t.Errorf("unexpected temporary files %v", entries)
					}
				}
				download(true)
				before := requests.Load()
				failing.Store(false)
				download(false)
				if got := requests.Load(); got <= before {
					t.Errorf("got requests %d, want more than %d", got, before)
				}
				before = requests.Load()
				failing.Store(true)
				download(false)
				if got := requests.Load(); got != before {
					t.Errorf("got requests %d, want %d", got, before)
				}
			})
		}
	})

	t.Run("SumDBConcurrentDownloads", func(t *testing.T) {
		gf := &GoFetcher{
			Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=" + vkey + " https://sumdb.example.com"},
			TempDir: t.TempDir(),
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				rec := httptest.NewRecorder()
				if req.URL.Host == "sumdb.example.com" {
					sumdbHandler(rec, req)
				} else {
					proxyHandler(rec, req)
				}
				return rec.Result(), nil
			}),
		}
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				info, mod, zip, err := gf.Download(t.Context(), "example.com", infoVersion)
				for _, content := range []io.ReadSeekCloser{info, mod, zip} {
					if content != nil {
						content.Close()
					} else {
						t.Error("missing module content")
					}
				}
				if err != nil {
					t.Errorf("unexpected error %v", err)
				}
			})
		}
		wg.Wait()
		if entries, err := os.ReadDir(gf.TempDir); err != nil {
			t.Fatal(err)
		} else if len(entries) != 0 {
			t.Errorf("unexpected temporary files %v", entries)
		}
	})

	t.Run("SumDBCancellation", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			envGOSUMDB string
			pathPrefix string
			bodyStatus int
		}{
			{"Supported", vkey, "/sumdb/sumdb.example.com/supported", 0},
			{"Lookup", vkey + " https://sumdb.example.com", "/lookup/", 0},
			{"LookupBody", vkey + " https://sumdb.example.com", "/lookup/", http.StatusOK},
			{"LookupErrorBody", vkey + " https://sumdb.example.com", "/lookup/", http.StatusServiceUnavailable},
			{"Tile", vkey + " https://sumdb.example.com", "/tile/", 0},
			{"TileBody", vkey + " https://sumdb.example.com", "/tile/", http.StatusOK},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, mode := range []struct {
					name    string
					wantErr error
				}{
					{"Canceled", context.Canceled},
					{"DeadlineExceeded", context.DeadlineExceeded},
				} {
					t.Run(mode.name, func(t *testing.T) {
						synctest.Test(t, func(t *testing.T) {
							var ctx context.Context
							var cancel context.CancelFunc
							if mode.wantErr == context.DeadlineExceeded {
								ctx, cancel = context.WithTimeout(t.Context(), time.Second)
							} else {
								ctx, cancel = context.WithCancel(t.Context())
								go func() {
									time.Sleep(time.Second)
									cancel()
								}()
							}
							defer cancel()
							var blocking atomic.Bool
							blocking.Store(true)
							var attempts atomic.Int32
							var body *testHTTPResponseBody
							gf := &GoFetcher{
								Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=" + tt.envGOSUMDB},
								TempDir: t.TempDir(),
								Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
									if blocking.Load() && strings.HasPrefix(req.URL.Path, tt.pathPrefix) {
										attempts.Add(1)
										if tt.bodyStatus != 0 {
											r, w := io.Pipe()
											t.Cleanup(func() { r.Close() })
											body = &testHTTPResponseBody{Reader: r}
											go func() {
												io.WriteString(w, "partial")
												<-req.Context().Done()
												w.CloseWithError(req.Context().Err())
											}()
											return &http.Response{StatusCode: tt.bodyStatus, Body: body}, nil
										}
										<-req.Context().Done()
										return nil, req.Context().Err()
									}
									rec := httptest.NewRecorder()
									if req.URL.Host == "sumdb.example.com" {
										sumdbHandler(rec, req)
									} else {
										proxyHandler(rec, req)
									}
									return rec.Result(), nil
								}),
							}
							download := func(ctx context.Context, wantErr error) {
								t.Helper()
								info, mod, zip, err := gf.Download(ctx, "example.com", infoVersion)
								if !errors.Is(err, wantErr) {
									t.Errorf("got error %v, want %v", err, wantErr)
								}
								for _, content := range []io.ReadSeekCloser{info, mod, zip} {
									if content != nil {
										content.Close()
										if wantErr != nil {
											t.Error("unexpected module content")
										}
									} else if wantErr == nil {
										t.Error("missing module content")
									}
								}
								if entries, err := os.ReadDir(gf.TempDir); err != nil {
									t.Fatal(err)
								} else if len(entries) != 0 {
									t.Errorf("unexpected temporary files %v", entries)
								}
							}
							started := time.Now()
							download(ctx, mode.wantErr)
							if got, want := time.Since(started), time.Second; got != want {
								t.Errorf("got duration %v, want %v", got, want)
							}
							if got, want := attempts.Load(), int32(1); got != want {
								t.Errorf("got attempts %d, want %d", got, want)
							}
							if body != nil && !body.closed {
								t.Error("response body was not closed")
							}
							blocking.Store(false)
							download(t.Context(), nil)
						})
					})
				}
			})
		}
	})

	t.Run("SumDBCanceledVerification", func(t *testing.T) {
		for _, file := range []struct {
			name string
			path string
		}{
			{"Mod", "go.mod"},
			{"Zip", "example.com@v1.0.0/go.mod"},
		} {
			t.Run(file.name, func(t *testing.T) {
				for _, tt := range []struct {
					name  string
					stage string
					err   error
				}{
					{"BeforeOpenCanceled", "BeforeOpen", context.Canceled},
					{"BeforeOpenDeadlineExceeded", "BeforeOpen", context.DeadlineExceeded},
					{"BeforeReadCanceled", "BeforeRead", context.Canceled},
					{"BeforeReadDeadlineExceeded", "BeforeRead", context.DeadlineExceeded},
					{"DuringReadCanceled", "DuringRead", context.Canceled},
					{"DuringReadDeadlineExceeded", "DuringRead", context.DeadlineExceeded},
					{"AfterHashCanceled", "AfterHash", context.Canceled},
					{"AfterHashDeadlineExceeded", "AfterHash", context.DeadlineExceeded},
				} {
					t.Run(tt.name, func(t *testing.T) {
						synctest.Test(t, func(t *testing.T) {
							ctx, cancel := context.WithTimeout(t.Context(), time.Second)
							defer cancel()
							stop := func() {
								if tt.err == context.Canceled {
									cancel()
								} else {
									<-ctx.Done()
								}
							}
							gf := &GoFetcher{
								Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=" + vkey + " https://sumdb.example.com"},
								TempDir: t.TempDir(),
								Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
									rec := httptest.NewRecorder()
									if req.URL.Host == "sumdb.example.com" {
										sumdbHandler(rec, req)
									} else {
										proxyHandler(rec, req)
									}
									return rec.Result(), nil
								}),
							}
							defaultHash := dirhash.DefaultHash
							t.Cleanup(func() { dirhash.DefaultHash = defaultHash })
							var stopped bool
							dirhash.DefaultHash = func(files []string, open func(string) (io.ReadCloser, error)) (string, error) {
								if !slices.Contains(files, file.path) {
									return defaultHash(files, open)
								}
								stopped = true
								if tt.stage == "BeforeOpen" {
									stop()
								}
								hash, err := defaultHash(files, func(name string) (io.ReadCloser, error) {
									r, err := open(name)
									if err != nil {
										return nil, err
									}
									if tt.stage == "DuringRead" {
										if _, err := io.ReadFull(r, make([]byte, 1)); err != nil {
											r.Close()
											return nil, err
										}
									}
									if tt.stage == "BeforeRead" || tt.stage == "DuringRead" {
										stop()
										if n, err := r.Read(make([]byte, 1)); n != 0 || err != tt.err {
											t.Errorf("got read %d, %v, want 0, %v", n, err, tt.err)
										}
									}
									return r, nil
								})
								if tt.stage == "AfterHash" {
									stop()
								} else if err != tt.err {
									t.Errorf("got hashing error %v, want %v", err, tt.err)
								}
								return hash, err
							}
							info, mod, zip, err := gf.Download(ctx, "example.com", infoVersion)
							if err != tt.err {
								t.Errorf("got error %v, want %v", err, tt.err)
							}
							if !stopped {
								t.Error("checksum hashing was not reached")
							}
							for _, content := range []io.ReadSeekCloser{info, mod, zip} {
								if content != nil {
									content.Close()
									t.Error("unexpected module content")
								}
							}
							gf.sumdbClientState.cache.Range(func(key, value any) bool {
								t.Errorf("unexpected shared cache entry %v", key)
								return true
							})
							if entries, err := os.ReadDir(gf.TempDir); err != nil {
								t.Fatal(err)
							} else if len(entries) != 0 {
								t.Errorf("unexpected temporary files %v", entries)
							}
						})
					})
				}
			})
		}
	})

	t.Run("SumDBConcurrentCancellation", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			envGOSUMDB string
			pathPrefix string
		}{
			{"Supported", vkey, "/sumdb/sumdb.example.com/supported"},
			{"Lookup", vkey + " https://sumdb.example.com", "/lookup/"},
			{"Tile", vkey + " https://sumdb.example.com", "/tile/"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					type contextKey struct{}
					ctx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, true))
					defer cancel()
					started, release := make(chan struct{}), make(chan struct{})
					gf := &GoFetcher{
						Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=" + tt.envGOSUMDB},
						TempDir: t.TempDir(),
						Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
							if strings.HasPrefix(req.URL.Path, tt.pathPrefix) {
								if req.Context().Value(contextKey{}) == true {
									close(started)
									<-req.Context().Done()
									return nil, req.Context().Err()
								}
								<-release
								if err := req.Context().Err(); err != nil {
									return nil, err
								}
							}
							rec := httptest.NewRecorder()
							if req.URL.Host == "sumdb.example.com" {
								sumdbHandler(rec, req)
							} else {
								proxyHandler(rec, req)
							}
							return rec.Result(), nil
						}),
					}
					download := func(ctx context.Context, done chan<- error) {
						info, mod, zip, err := gf.Download(ctx, "example.com", infoVersion)
						for _, content := range []io.ReadSeekCloser{info, mod, zip} {
							if content != nil {
								content.Close()
								if err != nil {
									t.Error("unexpected module content")
								}
							} else if err == nil {
								t.Error("missing module content")
							}
						}
						done <- err
					}
					canceledDone, activeDone := make(chan error, 1), make(chan error, 1)
					go download(ctx, canceledDone)
					<-started
					go download(t.Context(), activeDone)
					synctest.Wait()
					cancel()
					if err := <-canceledDone; !errors.Is(err, context.Canceled) {
						t.Errorf("got error %v, want cancellation", err)
					}
					close(release)
					if err := <-activeDone; err != nil {
						t.Errorf("unexpected error from active download %v", err)
					}
					if entries, err := os.ReadDir(gf.TempDir); err != nil {
						t.Fatal(err)
					} else if len(entries) != 0 {
						t.Errorf("unexpected temporary files %v", entries)
					}
				})
			})
		}
	})

	t.Run("SumDBTimeout", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			envGOSUMDB   string
			pathPrefix   string
			wantAttempts int32
		}{
			{"Supported", vkey, "/sumdb/sumdb.example.com/supported", 1},
			{"Lookup", vkey + " https://sumdb.example.com", "/lookup/", 1},
			{"Tile", vkey + " https://sumdb.example.com", "/tile/", 2},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var failing atomic.Bool
					failing.Store(true)
					var attempts atomic.Int32
					gf := &GoFetcher{
						Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=" + tt.envGOSUMDB},
						TempDir: t.TempDir(),
						Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
							if failing.Load() && strings.HasPrefix(req.URL.Path, tt.pathPrefix) {
								attempts.Add(1)
								if _, ok := req.Context().Deadline(); !ok {
									t.Error("request has no deadline")
									return nil, context.DeadlineExceeded
								}
								<-req.Context().Done()
								return nil, req.Context().Err()
							}
							rec := httptest.NewRecorder()
							if req.URL.Host == "sumdb.example.com" {
								sumdbHandler(rec, req)
							} else {
								proxyHandler(rec, req)
							}
							return rec.Result(), nil
						}),
					}
					started := time.Now()
					info, mod, zip, err := gf.Download(t.Context(), "example.com", infoVersion)
					if err == nil {
						t.Error("expected error")
					}
					if !errors.Is(err, errBadUpstream) {
						t.Errorf("got error %v, want a bad upstream error", err)
					}
					for _, content := range []io.ReadSeekCloser{info, mod, zip} {
						if content != nil {
							content.Close()
							t.Error("unexpected module content")
						}
					}
					if got := attempts.Load(); got != tt.wantAttempts {
						t.Errorf("got attempts %d, want %d", got, tt.wantAttempts)
					}
					if got, want := time.Since(started), time.Duration(tt.wantAttempts)*time.Minute; got != want {
						t.Errorf("got duration %v, want %v", got, want)
					}
					if entries, err := os.ReadDir(gf.TempDir); err != nil {
						t.Fatal(err)
					} else if len(entries) != 0 {
						t.Errorf("unexpected temporary files %v", entries)
					}
					failing.Store(false)
					if tt.name == "Supported" {
						time.Sleep(10 * time.Second)
					}
					info, mod, zip, err = gf.Download(t.Context(), "example.com", infoVersion)
					for _, content := range []io.ReadSeekCloser{info, mod, zip} {
						if content != nil {
							content.Close()
						} else {
							t.Error("missing module content after recovery")
						}
					}
					if err != nil {
						t.Fatalf("unexpected error after recovery %v", err)
					}
				})
			})
		}
	})

	t.Run("ReopenDownloadedFile", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			ext  string
		}{
			{"Mod", ".mod"},
			{"Zip", ".zip"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				if runtime.GOOS == "windows" && tt.ext == ".zip" {
					t.Skip("Windows does not allow removing an open ZIP file")
				}
				var missing string
				gf := &GoFetcher{
					Env:     []string{"GOPROXY=https://proxy.example.com", "GOSUMDB=" + vkey + " https://sumdb.example.com"},
					TempDir: t.TempDir(),
				}
				gf.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
					rec := httptest.NewRecorder()
					if req.URL.Host == "sumdb.example.com" {
						sumdbHandler(rec, req)
					} else {
						proxyHandler(rec, req)
					}
					resp := rec.Result()
					if filepath.Ext(req.URL.Path) == tt.ext {
						body := resp.Body
						resp.Body = struct {
							io.ReadCloser
							io.WriterTo
						}{body, writerToFunc(func(w io.Writer) (int64, error) {
							missing = w.(*httpGetTempWriter).Writer.(*os.File).Name()
							return io.Copy(w, body)
						})}
					}
					return resp, nil
				})
				defaultHash := dirhash.DefaultHash
				t.Cleanup(func() { dirhash.DefaultHash = defaultHash })
				dirhash.DefaultHash = func(files []string, open func(string) (io.ReadCloser, error)) (string, error) {
					hash, err := defaultHash(files, open)
					if err != nil {
						return "", err
					}
					if len(files) == 1 && files[0] == "example.com@v1.0.0/go.mod" {
						// Remove the file after hashing so validation succeeds before reopening it.
						if err := os.Remove(missing); err != nil {
							t.Fatal(err)
						}
					}
					return hash, nil
				}
				info, mod, zip, err := gf.Download(t.Context(), "example.com", infoVersion)
				for _, content := range []io.ReadSeekCloser{info, mod, zip} {
					if content != nil {
						content.Close()
						t.Error("unexpected module content")
					}
				}
				if _, ok := errors.AsType[*internalError](err); !ok {
					t.Errorf("got error %v, want an internal error", err)
				}
				if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Op != "open" || pe.Path != missing {
					t.Errorf("got error %v, want an open error for %q", err, missing)
				}
				if !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("got error %v, want an error matching %v", err, fs.ErrNotExist)
				}
				if entries, err := os.ReadDir(gf.TempDir); err != nil {
					t.Fatal(err)
				} else if len(entries) != 0 {
					t.Errorf("unexpected temporary files %v", entries)
				}
			})
		}
	})

	for _, tt := range []struct {
		name         string
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
			name: "Proxy",
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
			wantZip:  string(zipContent),
		},
		{
			name: "SkipProxy",
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
			wantZip:  string(zipContent),
		},
		{
			name: "DirectFallback",
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
			wantZip:  string(zipContent),
		},
		{
			name: "EmptyInfo",
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
			wantErr: fmt.Errorf("%w: invalid info file: unexpected end of JSON input", errBadUpstream),
		},
		{
			name: "EmptyMod",
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
			wantErr: fmt.Errorf("%w: invalid mod file: missing module directive", errBadUpstream),
		},
		{
			name: "EmptyZip",
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
			wantErr: fmt.Errorf("%w: invalid zip file: zip: not a valid zip file", errBadUpstream),
		},
		{
			name: "MissingModChecksum",
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
			wantErr: fmt.Errorf("%w: example.com@v1.0.0: invalid version: untrusted revision v1.0.0", errBadUpstream),
		},
		{
			name: "MissingZipChecksum",
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
			wantErr: fmt.Errorf("%w: example.com@v1.0.0: invalid version: untrusted revision v1.0.0", errBadUpstream),
		},
		{
			name:    "MajorVersionQuery",
			path:    "example.com",
			version: "v1",
			wantErr: errors.New("example.com@v1: invalid version: not a canonical version"),
		},
		{
			name:    "MinorVersionQuery",
			path:    "example.com",
			version: "v1.0",
			wantErr: errors.New("example.com@v1.0: invalid version: not a canonical version"),
		},
		{
			name:    "BranchQuery",
			path:    "example.com",
			version: "master",
			wantErr: errors.New("example.com@master: invalid version: not a semantic version"),
		},
		{
			name:    "WrongMajor",
			path:    "example.com",
			version: "v2.0.0",
			wantErr: errors.New("example.com@v2.0.0: invalid version: should be v0 or v1, not v2"),
		},
		{
			name:    "InvalidPath",
			path:    "foobar",
			version: infoVersion,
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			name: "InvalidGOSUMDB",
			env: func(_, _ string) []string {
				return append(os.Environ(), "GOSUMDB=foobar")
			},
			wantErr: errors.New("invalid GOSUMDB: malformed verifier id"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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

			info, mod, zip, err := gf.Download(t.Context(), tt.path, tt.version)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if got, want := errors.Is(err, errBadUpstream), errors.Is(tt.wantErr, errBadUpstream); got != want {
					t.Errorf("got bad upstream %t, want %t", got, want)
				}
				if got, want := errors.Is(err, fs.ErrNotExist), errors.Is(tt.wantErr, fs.ErrNotExist); got != want {
					t.Errorf("got not exist %t, want %t", got, want)
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

	t.Run("BodyLimitFallback", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			proxies      string
			wantErr      bool
			wantRequests []string
		}{
			{"SingleProxy", "", true, []string{"proxy.example.com.info", "proxy.example.com.mod"}},
			{"CommaSeparated", ",https://next.example.com", true, []string{"proxy.example.com.info", "proxy.example.com.mod"}},
			{"PipeSeparated", "|https://next.example.com", false, []string{"proxy.example.com.info", "proxy.example.com.mod", "next.example.com.info", "next.example.com.mod", "next.example.com.zip"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				body := &testHTTPResponseBody{Reader: io.LimitReader(zeroReader{}, zip.MaxGoMod+64<<10)}
				var requests []string
				gf := &GoFetcher{
					Env:     []string{"GOPROXY=https://proxy.example.com" + tt.proxies, "GOSUMDB=off"},
					TempDir: t.TempDir(),
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						ext := filepath.Ext(req.URL.Path)
						requests = append(requests, req.URL.Host+ext)
						if req.URL.Host == "proxy.example.com" && ext == ".mod" {
							return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: body, Request: req}, nil
						}
						rec := httptest.NewRecorder()
						proxyHandler(rec, req)
						return rec.Result(), nil
					}),
				}
				info, mod, zip, err := gf.Download(t.Context(), "example.com", infoVersion)
				for _, content := range []io.ReadSeekCloser{info, mod, zip} {
					if content != nil {
						content.Close()
						if tt.wantErr {
							t.Error("unexpected module content")
						}
					} else if !tt.wantErr {
						t.Error("missing module content")
					}
				}
				if tt.wantErr {
					if !errors.Is(err, errBadUpstream) || errors.Is(err, fs.ErrNotExist) {
						t.Errorf("got error %v, want a bad upstream error", err)
					}
				} else if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if !slices.Equal(requests, tt.wantRequests) {
					t.Errorf("got requests %v, want %v", requests, tt.wantRequests)
				}
				if !body.closed {
					t.Error("response body was not closed")
				}
				if entries, err := os.ReadDir(gf.TempDir); err != nil {
					t.Fatal(err)
				} else if len(entries) != 0 {
					t.Errorf("unexpected temporary files %v", entries)
				}
			})
		}
	})
}

func TestGoFetcherProxyDownload(t *testing.T) {
	infoVersion := "v1.0.0"
	info := marshalInfo(infoVersion, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mod := "module example.com"
	zipContent, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")})
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
			responseSuccess(rw, req, bytes.NewReader(zipContent), "application/zip", -2)
		default:
			responseNotFound(rw, req, -2)
		}
	}
	for _, tt := range []struct {
		name         string
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
			name:     "Success",
			path:     "example.com",
			version:  infoVersion,
			wantInfo: info,
			wantMod:  mod,
			wantZip:  string(zipContent),
		},
		{
			name:         "InfoNotFound",
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			path:         "example.com",
			version:      infoVersion,
			wantErr:      notExistErrorf("not found"),
		},
		{
			name: "ModNotFound",
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
			name: "ZipNotFound",
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
			name:    "InvalidPath",
			path:    "foobar",
			wantErr: errors.New(`malformed module path "foobar": missing dot in first path element`),
		},
		{
			name:    "EmptyVersion",
			path:    "example.com",
			wantErr: errors.New(`version "" invalid: disallowed version string`),
		},
		{
			name:    "MissingTempDir",
			tempDir: filepath.Join(t.TempDir(), "404"),
			path:    "example.com",
			version: infoVersion,
			wantErr: fs.ErrNotExist,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
			infoFile, modFile, zipFile, cleanup, err := gf.proxyDownload(t.Context(), tt.path, tt.version, proxy)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if tt.wantErr == fs.ErrNotExist {
					if _, ok := errors.AsType[*internalError](err); !ok {
						t.Errorf("got error %v, want an internal error", err)
					}
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

	t.Run("BodyLimit", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			ext          string
			maxBytes     int64
			wantRequests []string
		}{
			{"Mod", ".mod", zip.MaxGoMod, []string{".info", ".mod"}},
			{"Zip", ".zip", zip.MaxZipFile, []string{".info", ".mod", ".zip"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const extraBytes = 1 << 20
				body := &testHTTPResponseBody{Reader: io.LimitReader(zeroReader{}, tt.maxBytes+extraBytes)}
				var requests []string
				gf := &GoFetcher{
					Env:     []string{"GOSUMDB=off"},
					TempDir: t.TempDir(),
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						ext := filepath.Ext(req.URL.Path)
						requests = append(requests, ext)
						if ext == tt.ext {
							return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: body, Request: req}, nil
						}
						rec := httptest.NewRecorder()
						proxyHandler(rec, req)
						return rec.Result(), nil
					}),
				}
				gf.initOnce.Do(gf.init)
				if gf.initErr != nil {
					t.Fatal(gf.initErr)
				}
				proxy, err := url.Parse("https://proxy.example.com")
				if err != nil {
					t.Fatal(err)
				}
				_, _, _, cleanup, err := gf.proxyDownload(t.Context(), "example.com", infoVersion, proxy)
				if cleanup != nil {
					defer cleanup()
					t.Error("unexpected cleanup function")
				}
				if !errors.Is(err, errBadUpstream) {
					t.Fatalf("got error %v, want a bad upstream error", err)
				}
				if got, want := err.Error(), fmt.Sprintf("bad upstream: response body exceeds %d bytes", tt.maxBytes); got != want {
					t.Errorf("got error %q, want %q", got, want)
				}
				if errors.Is(err, fs.ErrNotExist) {
					t.Errorf("unexpected error matching %v", fs.ErrNotExist)
				}
				if _, ok := errors.AsType[*internalError](err); ok {
					t.Errorf("unexpected internal error %v", err)
				}
				if !slices.Equal(requests, tt.wantRequests) {
					t.Errorf("got requests %v, want %v", requests, tt.wantRequests)
				}
				if !body.closed {
					t.Error("response body was not closed")
				}
				if got := int64(body.bytesRead); got <= tt.maxBytes || got >= tt.maxBytes+extraBytes {
					t.Errorf("read %d bytes, want an early stop after %d bytes", got, tt.maxBytes)
				}
				if entries, err := os.ReadDir(gf.TempDir); err != nil {
					t.Fatal(err)
				} else if len(entries) != 0 {
					t.Errorf("unexpected temporary files %v", entries)
				}
			})
		}
	})
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
		name     string
		path     string
		wantInfo string
		wantMod  string
		wantZip  string
		wantErr  error
	}{
		{
			name:     "Success",
			path:     "example.com",
			wantInfo: info,
			wantMod:  mod,
			wantZip:  string(zip),
		},
		{
			name:    "InvalidPath",
			path:    "foobar",
			wantErr: errors.New(`foobar@v1.0.0: malformed module path "foobar": missing dot in first path element`),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gf := &GoFetcher{Env: append(os.Environ(), "GOSUMDB=off"), TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}
			gf.env = append(gf.env, "GOPROXY="+proxyServer.URL)

			infoFile, modFile, zipFile, err := gf.directDownload(t.Context(), tt.path, infoVersion)
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

func TestGoFetcherExecGo(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())

	ctxCanceled, cancel := context.WithCancel(t.Context())
	cancel()

	for _, tt := range []struct {
		name        string
		ctx         context.Context
		env         []string
		goBin       string
		tempDir     string
		args        []string
		wantOutput  string
		wantErr     error
		wantNoStore bool
	}{
		{
			name:       "Success",
			ctx:        t.Context(),
			args:       []string{"env", "GOPROXY"},
			wantOutput: "direct\n",
		},
		{
			name:        "UnknownCommand",
			ctx:         t.Context(),
			args:        []string{"foobar"},
			wantErr:     errors.New("go foobar: unknown command\nRun 'go help' for usage."),
			wantNoStore: true,
		},
		{
			name:        "InvalidModulePath",
			ctx:         t.Context(),
			args:        []string{"mod", "download", "-json", "foobar@latest"},
			wantErr:     errors.New(`foobar@latest: malformed module path "foobar": missing dot in first path element`),
			wantNoStore: true,
		},
		{
			name:    "Canceled",
			ctx:     ctxCanceled,
			wantErr: context.Canceled,
		},
		{
			name:    "MissingGoBinary",
			ctx:     t.Context(),
			goBin:   filepath.Join(t.TempDir(), "404"),
			wantErr: fs.ErrNotExist,
		},
		{
			name:    "MissingTempDir",
			ctx:     t.Context(),
			tempDir: filepath.Join(t.TempDir(), "404"),
			wantErr: fs.ErrNotExist,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.tempDir == "" {
				tt.tempDir = t.TempDir()
			}

			gf := &GoFetcher{
				GoBin:                      tt.goBin,
				MaxConcurrentDirectFetches: 1,
				TempDir:                    tt.tempDir,
			}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			output, err := gf.execGo(tt.ctx, tt.args...)
			if got := len(gf.directFetchWorkerPool); got != 0 {
				t.Errorf("got occupied slots %d, want 0", got)
			}
			if tt.wantNoStore {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("got %v, want %v", err, fs.ErrNotExist)
				}
				rec := httptest.NewRecorder()
				responseError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err)
				if got, want := rec.Code, http.StatusNotFound; got != want {
					t.Errorf("got %d, want %d", got, want)
				}
				if got, want := rec.Header().Get("Cache-Control"), "no-store"; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if tt.wantErr == fs.ErrNotExist {
					if _, ok := errors.AsType[*internalError](err); !ok {
						t.Errorf("got error %v, want an internal error", err)
					}
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

	t.Run("CanceledWithoutLimit", func(t *testing.T) {
		gf := &GoFetcher{TempDir: t.TempDir()}
		gf.initOnce.Do(gf.init)
		if gf.initErr != nil {
			t.Fatal(gf.initErr)
		}
		if output, err := gf.execGo(ctxCanceled); output != nil || err != context.Canceled {
			t.Errorf("got %q, %v, want no output and %v", output, err, context.Canceled)
		}
	})

	t.Run("Queued", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			wantErr error
		}{
			{"Released", fs.ErrNotExist},
			{"AlreadyCanceled", context.Canceled},
			{"CanceledWhileWaiting", context.Canceled},
			{"DeadlineExceeded", context.DeadlineExceeded},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					gf := &GoFetcher{
						GoBin:                      filepath.Join(t.TempDir(), "404"),
						MaxConcurrentDirectFetches: 1,
						TempDir:                    t.TempDir(),
					}
					gf.initOnce.Do(gf.init)
					if gf.initErr != nil {
						t.Fatal(gf.initErr)
					}
					gf.directFetchWorkerPool <- struct{}{}
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					if tt.name == "AlreadyCanceled" {
						cancel()
					}
					done := make(chan error, 1)
					go func() {
						output, err := gf.execGo(ctx)
						if output != nil {
							t.Errorf("unexpected output %q", output)
						}
						done <- err
					}()
					synctest.Wait()
					if tt.name != "AlreadyCanceled" && len(done) != 0 {
						t.Error("command completed before a slot became available")
					}
					switch tt.name {
					case "Released":
						<-gf.directFetchWorkerPool
					case "CanceledWhileWaiting":
						cancel()
					case "DeadlineExceeded":
						time.Sleep(time.Second)
					}
					synctest.Wait()
					var err error
					select {
					case err = <-done:
					default:
						t.Error("command is still waiting for a slot")
						<-gf.directFetchWorkerPool
						err = <-done
					}
					if !errors.Is(err, tt.wantErr) {
						t.Errorf("got error %v, want %v", err, tt.wantErr)
					}
					wantOccupied := 1
					if tt.name == "Released" {
						wantOccupied = 0
					}
					if got := len(gf.directFetchWorkerPool); got != wantOccupied {
						t.Errorf("got occupied slots %d, want %d", got, wantOccupied)
					}
					if entries, err := os.ReadDir(gf.TempDir); err != nil {
						t.Fatal(err)
					} else if len(entries) != 0 {
						t.Errorf("unexpected temporary files %v", entries)
					}
				})
			})
		}
	})
}

func TestCleanEnvGOPROXY(t *testing.T) {
	for _, tt := range []struct {
		name           string
		envGOPROXY     string
		wantEnvGOPROXY string
		wantErr        error
	}{
		{
			name:           "Empty",
			wantEnvGOPROXY: defaultEnvGOPROXY,
		},
		{
			name:           "Default",
			envGOPROXY:     defaultEnvGOPROXY,
			wantEnvGOPROXY: defaultEnvGOPROXY,
		},
		{
			name:           "SingleProxy",
			envGOPROXY:     "https://example.com",
			wantEnvGOPROXY: "https://example.com",
		},
		{
			name:           "TrailingComma",
			envGOPROXY:     "https://example.com,",
			wantEnvGOPROXY: "https://example.com",
		},
		{
			name:           "TrailingPipe",
			envGOPROXY:     "https://example.com|",
			wantEnvGOPROXY: "https://example.com",
		},
		{
			name:           "MixedSeparators",
			envGOPROXY:     "https://example.com|https://backup.example.com,direct",
			wantEnvGOPROXY: "https://example.com|https://backup.example.com,direct",
		},
		{
			name:           "DirectBeforeProxy",
			envGOPROXY:     "https://example.com,direct,https://backup.example.com",
			wantEnvGOPROXY: "https://example.com,direct",
		},
		{
			name:           "OffBeforeProxy",
			envGOPROXY:     "https://example.com,off,https://backup.example.com",
			wantEnvGOPROXY: "https://example.com,off",
		},
		{
			name:       "InvalidURL",
			envGOPROXY: "://invalid",
			wantErr:    errors.New(`invalid GOPROXY URL: parse "://invalid": missing protocol scheme`),
		},
		{
			name:       "CommaOnly",
			envGOPROXY: ",",
			wantErr:    errors.New("GOPROXY list is not the empty string, but contains no entries"),
		},
		{
			name:       "PipeOnly",
			envGOPROXY: "|",
			wantErr:    errors.New("GOPROXY list is not the empty string, but contains no entries"),
		},
		{
			name:       "WhitespaceOnly",
			envGOPROXY: " ",
			wantErr:    errors.New("GOPROXY list is not the empty string, but contains no entries"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
	t.Run("Off", func(t *testing.T) {
		for _, tt := range []struct {
			name           string
			envGOPROXY     string
			proxyErr       error
			wantProxyCalls int
		}{
			{"Only", "off", nil, 0},
			{"BeforeDirect", "off,direct", nil, 0},
			{"BeforeProxy", "off,https://example.com", nil, 0},
			{"AfterAbsence", "https://example.com,off", fs.ErrNotExist, 1},
			{"AfterFailure", "https://example.com|off", errBadUpstream, 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var proxyCalls int
				err := walkEnvGOPROXY(tt.envGOPROXY, func(*url.URL) error {
					proxyCalls++
					return tt.proxyErr
				}, func() error {
					t.Error("unexpected direct fetch")
					return nil
				})
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("got %v, want %v", err, fs.ErrNotExist)
				}
				if _, ok := errors.AsType[*uncacheableNotExistError](err); !ok {
					t.Errorf("got %v, want an uncacheable error", err)
				}
				if got, want := err.Error(), "module lookup disabled by GOPROXY=off"; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := proxyCalls, tt.wantProxyCalls; got != want {
					t.Errorf("got proxy calls %d, want %d", got, want)
				}
			})
		}
	})

	t.Run("InternalErrors", func(t *testing.T) {
		gf := &GoFetcher{TempDir: filepath.Join(t.TempDir(), "missing")}
		proxy := &url.URL{Scheme: "https", Host: "example.com"}
		_, _, _, _, localErr := gf.proxyDownload(t.Context(), "example.com", "v1.0.0", proxy)
		if _, ok := errors.AsType[*internalError](localErr); !ok {
			t.Fatalf("got error %v, want an internal error", localErr)
		}
		for _, tt := range []struct {
			name              string
			err               error
			wantCommaFallback bool
		}{
			{"Direct", localErr, false},
			{"Wrapped", fmt.Errorf("fetch failed: %w", localErr), false},
			{"Joined", errors.Join(fs.ErrNotExist, localErr), false},
			{"Unmarked", errors.Unwrap(localErr), true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, fallback := range []struct {
					name      string
					separator string
					next      string
				}{
					{"CommaProxy", ",", "https://alt.example.com"},
					{"PipeProxy", "|", "https://alt.example.com"},
					{"CommaDirect", ",", "direct"},
					{"PipeDirect", "|", "direct"},
				} {
					t.Run(fallback.name, func(t *testing.T) {
						var calls []string
						err := walkEnvGOPROXY("https://example.com"+fallback.separator+fallback.next, func(proxy *url.URL) error {
							calls = append(calls, proxy.String())
							if proxy.Host == "example.com" {
								return tt.err
							}
							return nil
						}, func() error {
							calls = append(calls, "direct")
							return nil
						})
						wantCalls := []string{"https://example.com"}
						wantErr := tt.err
						if fallback.separator == "|" || tt.wantCommaFallback {
							wantCalls = append(wantCalls, fallback.next)
							wantErr = nil
						}
						if !slices.Equal(calls, wantCalls) {
							t.Errorf("got calls %q, want %q", calls, wantCalls)
						}
						if err != wantErr {
							t.Errorf("got error %v, want %v", err, wantErr)
						}
					})
				}
			})
		}
	})

	t.Run("HTTPStatusFallback", func(t *testing.T) {
		for _, statusCode := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusGone} {
			for _, tt := range []struct {
				name      string
				separator string
				next      string
			}{
				{"CommaProxy", ",", "https://alt.example.com"},
				{"PipeProxy", "|", "https://alt.example.com"},
				{"CommaDirect", ",", "direct"},
				{"PipeDirect", "|", "direct"},
			} {
				t.Run(strconv.Itoa(statusCode)+"/"+tt.name, func(t *testing.T) {
					attempts, proxyCalls, directCalls := 0, 0, 0
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						attempts++
						return &http.Response{
							StatusCode: statusCode,
							Status:     strconv.Itoa(statusCode) + " " + http.StatusText(statusCode),
							Body:       http.NoBody,
							Request:    req,
						}, nil
					})}
					err := walkEnvGOPROXY("https://example.com"+tt.separator+tt.next, func(proxy *url.URL) error {
						proxyCalls++
						if proxy.Host == "example.com" {
							_, err := httpGet(t.Context(), client, proxy.String(), nil)
							return err
						}
						return nil
					}, func() error {
						directCalls++
						return nil
					})
					wantProxyCalls, wantDirectCalls := 1, 0
					if statusCode == http.StatusBadRequest && tt.separator == "," {
						if err == nil || errors.Is(err, fs.ErrNotExist) {
							t.Errorf("got %v, want a non-absence error", err)
						}
					} else {
						if err != nil {
							t.Errorf("unexpected error %v", err)
						}
						if tt.next == "direct" {
							wantDirectCalls = 1
						} else {
							wantProxyCalls = 2
						}
					}
					if got, want := attempts, 1; got != want {
						t.Errorf("got attempts %d, want %d", got, want)
					}
					if got, want := proxyCalls, wantProxyCalls; got != want {
						t.Errorf("got proxy calls %d, want %d", got, want)
					}
					if got, want := directCalls, wantDirectCalls; got != want {
						t.Errorf("got direct calls %d, want %d", got, want)
					}
				})
			}
		}
	})

	t.Run("CacheRestrictions", func(t *testing.T) {
		ordinaryErr := notExistErrorf("module unavailable")
		restrictedErr := &uncacheableNotExistError{notExistError{err: ordinaryErr}}
		for _, tt := range []struct {
			name     string
			firstErr error
			lastErr  error
		}{
			{"RestrictedThenSuccess", restrictedErr, nil},
			{"RestrictedThenOrdinary", restrictedErr, ordinaryErr},
			{"OrdinaryThenRestricted", ordinaryErr, restrictedErr},
			{"BothRestricted", restrictedErr, restrictedErr},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, separator := range []string{",", "|"} {
					var calls int
					err := walkEnvGOPROXY("https://example.com"+separator+"https://alt.example.com", func(proxy *url.URL) error {
						calls++
						if proxy.Host == "example.com" {
							return tt.firstErr
						}
						return tt.lastErr
					}, nil)
					if got, want := calls, 2; got != want {
						t.Errorf("separator %q: got calls %d, want %d", separator, got, want)
					}
					if got, want := err, tt.lastErr; got != want {
						t.Errorf("separator %q: got error %v, want %v", separator, got, want)
					}
				}
			})
		}
	})

	for _, tt := range []struct {
		name         string
		envGOPROXY   string
		onProxy      func(proxy *url.URL) error
		wantOnProxy  string
		wantOnDirect bool
		wantErr      error
	}{
		{
			name:        "ProxySuccess",
			envGOPROXY:  "https://example.com,direct",
			onProxy:     func(proxy *url.URL) error { return nil },
			wantOnProxy: "https://example.com",
		},
		{
			name:         "DirectFallback",
			envGOPROXY:   "https://example.com,direct",
			onProxy:      func(proxy *url.URL) error { return fs.ErrNotExist },
			wantOnProxy:  "https://example.com",
			wantOnDirect: true,
		},
		{
			name:       "PipeFallback",
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
			name:       "ProxyError",
			envGOPROXY: "https://example.com,direct",
			onProxy:    func(proxy *url.URL) error { return errors.New("foobar") },
			wantErr:    errors.New("foobar"),
		},
		{
			name:       "ProxyNotFound",
			envGOPROXY: "https://example.com",
			onProxy:    func(proxy *url.URL) error { return fs.ErrNotExist },
			wantErr:    fs.ErrNotExist,
		},
		{
			name:         "Direct",
			envGOPROXY:   "direct",
			wantOnDirect: true,
		},
		{
			name:         "DirectBeforeOff",
			envGOPROXY:   "direct,off",
			wantOnDirect: true,
		},
		{
			name:       "OffOnly",
			envGOPROXY: "off",
			wantErr:    errors.New("module lookup disabled by GOPROXY=off"),
		},
		{
			name:       "OffBeforeDirect",
			envGOPROXY: "off,direct",
			wantErr:    errors.New("module lookup disabled by GOPROXY=off"),
		},
		{
			name:    "Empty",
			wantErr: errors.New("missing GOPROXY"),
		},
		{
			name:       "InvalidURL",
			envGOPROXY: "://invalid",
			wantErr:    errors.New(`parse "://invalid": missing protocol scheme`),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
		name           string
		envGOSUMDB     string
		wantEnvGOSUMDB string
	}{
		{"Empty", "", defaultEnvGOSUMDB},
		{"Default", defaultEnvGOSUMDB, defaultEnvGOSUMDB},
		{"Custom", "example.com", "example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := cleanEnvGOSUMDB(tt.envGOSUMDB), tt.wantEnvGOSUMDB; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestParseEnvGOSUMDB(t *testing.T) {
	for _, tt := range []struct {
		name            string
		envGOSUMDB      string
		wantName        string
		wantKey         string
		wantURL         string
		wantIsDirectURL bool
		wantErr         error
	}{
		{
			name:            "Default",
			envGOSUMDB:      defaultEnvGOSUMDB,
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://" + defaultEnvGOSUMDB,
			wantIsDirectURL: true,
		},
		{
			name:            "VerifierKey",
			envGOSUMDB:      sumGolangOrgKey,
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://" + defaultEnvGOSUMDB,
			wantIsDirectURL: true,
		},
		{
			name:            "VerifierKeyWithURL",
			envGOSUMDB:      sumGolangOrgKey + " https://" + defaultEnvGOSUMDB,
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://" + defaultEnvGOSUMDB,
			wantIsDirectURL: false,
		},
		{
			name:            "ChinaMirror",
			envGOSUMDB:      "sum.golang.google.cn",
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://sum.golang.google.cn",
			wantIsDirectURL: false,
		},
		{
			name:            "ChinaMirrorWithURL",
			envGOSUMDB:      "sum.golang.google.cn https://example.com",
			wantName:        defaultEnvGOSUMDB,
			wantKey:         sumGolangOrgKey,
			wantURL:         "https://example.com",
			wantIsDirectURL: false,
		},
		{
			name:    "Empty",
			wantErr: errors.New("missing GOSUMDB"),
		},
		{
			name:       "WhitespaceOnly",
			envGOSUMDB: " ",
			wantErr:    errors.New("missing GOSUMDB"),
		},
		{
			name:       "TooManyFields",
			envGOSUMDB: "a b c",
			wantErr:    errors.New("invalid GOSUMDB: too many fields"),
		},
		{
			name:       "InvalidVerifierKey",
			envGOSUMDB: "example.com",
			wantErr:    errors.New("invalid GOSUMDB: malformed verifier id"),
		},
		{
			name:       "InvalidName",
			envGOSUMDB: "example.com/+1a6413ba+AW5WXiP8oUq7RI2AuI4Wh14FJrMqJqnAplQ0kcLbnbqK",
			wantErr:    fmt.Errorf("invalid sumdb name (must be host[/path]): example.com/ %+v", url.URL{Scheme: "https", Host: "example.com", Path: "/"}),
		},
		{
			name:       "InvalidURL",
			envGOSUMDB: defaultEnvGOSUMDB + " ://invalid",
			wantErr:    errors.New(`invalid GOSUMDB URL: parse "://invalid": missing protocol scheme`),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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

	t.Run("InvalidNames", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			sumdbName string
			wantErr   string
		}{
			{"InvalidHostEscape", "example.com%", `invalid sumdb name (must be host[/path]): parse "https://example.com%": invalid URL escape "%"`},
			{"InvalidPathEscape", "example.com/path%zz", `invalid sumdb name (must be host[/path]): parse "https://example.com/path%zz": invalid URL escape "%zz"`},
			{"InvalidHost", "[example.com", `invalid sumdb name (must be host[/path]): parse "https://[example.com": missing ']' in host`},
			{"InvalidPort", "example.com:invalid", `invalid sumdb name (must be host[/path]): parse "https://example.com:invalid": invalid port ":invalid" after host`},
		} {
			for _, mode := range []struct {
				name string
				url  string
			}{
				{"DefaultURL", ""},
				{"ExplicitURL", " https://sumdb.example.com"},
			} {
				t.Run(tt.name+"/"+mode.name, func(t *testing.T) {
					_, verifierKey, err := note.GenerateKey(nil, tt.sumdbName)
					if err != nil {
						t.Fatal(err)
					}
					name, key, u, isDirectURL, err := parseEnvGOSUMDB(verifierKey + mode.url)
					if err == nil {
						t.Fatal("expected error")
					}
					if got, want := err.Error(), tt.wantErr; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					if e, ok := errors.AsType[*url.Error](err); !ok || e.URL != "https://"+tt.sumdbName {
						t.Errorf("got error %v, want URL parse error", err)
					}
					if name != "" || key != "" || u != nil || isDirectURL {
						t.Errorf("got (%q, %q, %v, %t), want empty result", name, key, u, isDirectURL)
					}
				})
			}
		}
	})
}

func TestCleanCommaSeparatedList(t *testing.T) {
	for _, tt := range []struct {
		name     string
		list     string
		wantList string
	}{
		{"Empty", "", ""},
		{"CommaOnly", ",", ""},
		{"TrailingComma", "a,", "a"},
		{"LeadingComma", ",a", "a"},
		{"WhitespaceAndEmptyEntries", " , a", "a"},
		{"WhitespaceAroundEntries", "a , b", "a,b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := cleanCommaSeparatedList(tt.list), tt.wantList; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestCheckCanonicalVersion(t *testing.T) {
	for _, tt := range []struct {
		name    string
		path    string
		version string
		wantErr error
	}{
		{
			name:    "Canonical",
			path:    "example.com",
			version: "v1.0.0",
		},
		{
			name:    "MajorVersionQuery",
			path:    "example.com",
			version: "v1",
			wantErr: errors.New("example.com@v1: invalid version: not a canonical version"),
		},
		{
			name:    "MinorVersionQuery",
			path:    "example.com",
			version: "v1.0",
			wantErr: errors.New("example.com@v1.0: invalid version: not a canonical version"),
		},
		{
			name:    "BranchQuery",
			path:    "example.com",
			version: "master",
			wantErr: errors.New("example.com@master: invalid version: not a semantic version"),
		},
		{
			name:    "WrongMajor",
			path:    "example.com",
			version: "v2.0.0",
			wantErr: errors.New("example.com@v2.0.0: invalid version: should be v0 or v1, not v2"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
		name        string
		info        string
		wantVersion string
		wantTime    time.Time
		wantErr     error
	}{
		{
			name:        "UTC",
			info:        `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
			wantVersion: "v1.0.0",
			wantTime:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "TimeZoneOffset",
			info:        `{"Version":"v1.0.0","Time":"2000-01-01T01:00:00+01:00"}`,
			wantVersion: "v1.0.0",
			wantTime:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "ZeroTime",
			info:        `{"Version":"v1.0.0","Time":"0001-01-01T00:00:00Z"}`,
			wantVersion: "v1.0.0",
			wantTime:    time.Time{},
		},
		{
			name:    "MissingVersion",
			info:    "{}",
			wantErr: errors.New("invalid version"),
		},
		{
			name:    "EmptyInput",
			wantErr: errors.New("unexpected end of JSON input"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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

	t.Run("LargeInfo", func(t *testing.T) {
		file, err := makeTempFile(t, []byte(info+strings.Repeat(" ", 2<<20)))
		if err != nil {
			t.Fatal(err)
		}
		version, timestamp, err := unmarshalInfoFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if version != infoVersion || !timestamp.Equal(infoTime) {
			t.Errorf("got version %q at %v, want %q at %v", version, timestamp, infoVersion, infoTime)
		}
	})

	infoFile, err := makeTempFile(t, []byte(info))
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	infoFileInvalid, err := makeTempFile(t, nil)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	for _, tt := range []struct {
		name        string
		infoFile    string
		wantVersion string
		wantTime    time.Time
		wantErr     error
	}{
		{
			name:        "Valid",
			infoFile:    infoFile,
			wantVersion: infoVersion,
			wantTime:    infoTime,
		},
		{
			name:     "EmptyFile",
			infoFile: infoFileInvalid,
			wantErr:  fmt.Errorf("%w: invalid info file: unexpected end of JSON input", errBadUpstream),
		},
		{
			name:     "MissingFile",
			infoFile: filepath.Join(t.TempDir(), "404"),
			wantErr:  fs.ErrNotExist,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			infoVersion, infoTime, err := unmarshalInfoFile(tt.infoFile)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if tt.wantErr == fs.ErrNotExist {
					if _, ok := errors.AsType[*internalError](err); !ok {
						t.Errorf("got error %v, want an internal error", err)
					}
				}
				if got, want := errors.Is(err, errBadUpstream), errors.Is(tt.wantErr, errBadUpstream); got != want {
					t.Errorf("got bad upstream %t, want %t", got, want)
				}
				if got, want := errors.Is(err, fs.ErrNotExist), errors.Is(tt.wantErr, fs.ErrNotExist); got != want {
					t.Errorf("got not exist %t, want %t", got, want)
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
	t.Run("SizeLimit", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			size    int
			wantErr bool
		}{
			{"BelowLimit", zip.MaxGoMod - 1, false},
			{"AtLimit", zip.MaxGoMod, false},
			{"AboveLimit", zip.MaxGoMod + 1, true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const mod = "module example.com\n//"
				file, err := makeTempFile(t, []byte(mod+strings.Repeat("x", tt.size-len(mod))))
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				err = checkModFile(file)
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error")
					}
					if got, want := err.Error(), fmt.Sprintf("bad upstream: invalid mod file: size exceeds %d bytes", zip.MaxGoMod); got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					if !errors.Is(err, errBadUpstream) {
						t.Errorf("got error %v, want an error matching %v", err, errBadUpstream)
					}
					if errors.Is(err, fs.ErrNotExist) {
						t.Errorf("unexpected error matching %v: %v", fs.ErrNotExist, err)
					}
				} else if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
			})
		}
	})

	t.Run("Directory", func(t *testing.T) {
		err := checkModFile(t.TempDir())
		if _, ok := errors.AsType[*internalError](err); !ok {
			t.Errorf("got error %v, want an internal error", err)
		}
		if _, ok := errors.AsType[*fs.PathError](err); !ok {
			t.Errorf("got error %v, want a file error", err)
		}
		if errors.Is(err, errBadUpstream) {
			t.Errorf("unexpected error matching %v: %v", errBadUpstream, err)
		}
	})

	t.Run("MissingFile", func(t *testing.T) {
		err := checkModFile(filepath.Join(t.TempDir(), "missing.mod"))
		if _, ok := errors.AsType[*internalError](err); !ok {
			t.Errorf("got error %v, want an internal error", err)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("got error %v, want an error matching %v", err, fs.ErrNotExist)
		}
		if errors.Is(err, errBadUpstream) {
			t.Errorf("unexpected error matching %v: %v", errBadUpstream, err)
		}
	})

	for _, tt := range []struct {
		name    string
		mod     string
		wantErr bool
	}{
		{"Valid", "module example.com", false},
		{"Comments", "// comment\nmodule example.com // comment\n", false},
		{"Whitespace", "\r\n\tmodule\texample.com\r\n", false},
		{"QuotedPath", `module "example.com"`, false},
		{"RawQuotedPath", "module `example.com`", false},
		{"LongComment", "//" + strings.Repeat("x", bufio.MaxScanTokenSize) + "\nmodule example.com\n", false},
		{"LongModuleLine", "module " + strings.Repeat(" ", bufio.MaxScanTokenSize) + "example.com\n", false},
		{"OtherDirective", "unknown directive\nmodule example.com\n", false},
		{"Empty", "", true},
		{"CommentsOnly", "// module example.com\n", true},
		{"MissingDirective", "go 1.26\n", true},
		{"DirectivePrefix", "modulexxx example.com", true},
		{"MissingPath", "module", true},
		{"WhitespaceOnlyPath", "module \t\n", true},
		{"CommentInsteadOfPath", "module // example.com", true},
		{"EmptyQuotedPath", `module ""`, true},
		{"EmptyRawQuotedPath", "module ``", true},
		{"UnterminatedQuote", `module "example.com`, true},
		{"QuotedPathSuffix", `module "example.com"suffix`, true},
		{"MissingWhitespace", `module"example.com"`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file, err := makeTempFile(t, []byte(tt.mod))
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			err = checkModFile(file)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err.Error(), "bad upstream: invalid mod file: missing module directive"; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if !errors.Is(err, errBadUpstream) {
					t.Errorf("got error %v, want an error matching %v", err, errBadUpstream)
				}
				if errors.Is(err, fs.ErrNotExist) {
					t.Errorf("unexpected error matching %v: %v", fs.ErrNotExist, err)
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
		name          string
		env           []string
		modFile       string
		modulePath    string
		moduleVersion string
		wantErr       error
	}{
		{
			name:          "Valid",
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			modFile:       modFile,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			name: "SkipSumDB",
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
			name:          "ChecksumMismatch",
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			modFile:       modFileInvalid,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       fmt.Errorf("%w: example.com@v1.0.0: invalid version: untrusted revision v1.0.0", errBadUpstream),
		},
		{
			name:          "LookupError",
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			modFile:       modFile,
			modulePath:    "example.com",
			moduleVersion: "v1.1.0",
			wantErr:       fmt.Errorf("%w: example.com@v1.1.0/go.mod: bad upstream", errBadUpstream),
		},
		{
			name:          "MissingFile",
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			modFile:       filepath.Join(t.TempDir(), "404"),
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       fs.ErrNotExist,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gf := &GoFetcher{Env: tt.env, TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			sumdbClient := sumdb.NewClient(&sumdbClientOps{sumdbClientState: gf.sumdbClientState, ctx: t.Context()})
			sumdbClient.SetGONOSUMDB(gf.envGONOSUMDB)
			err := verifyModFile(t.Context(), sumdbClient, tt.modFile, tt.modulePath, tt.moduleVersion)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if tt.wantErr == fs.ErrNotExist {
					if _, ok := errors.AsType[*internalError](err); !ok {
						t.Errorf("got error %v, want an internal error", err)
					}
				}
				if got, want := errors.Is(err, errBadUpstream), errors.Is(tt.wantErr, errBadUpstream); got != want {
					t.Errorf("got bad upstream %t, want %t", got, want)
				}
				if got, want := errors.Is(err, fs.ErrNotExist), errors.Is(tt.wantErr, fs.ErrNotExist); got != want {
					t.Errorf("got not exist %t, want %t", got, want)
				}
			} else if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}

func TestCheckZipFile(t *testing.T) {
	t.Run("Directory", func(t *testing.T) {
		err := checkZipFile(t.TempDir(), "example.com", "v1.0.0")
		if _, ok := errors.AsType[*fs.PathError](err); !ok {
			t.Errorf("got error %v, want a file error", err)
		}
		if errors.Is(err, errBadUpstream) {
			t.Errorf("unexpected error matching %v: %v", errBadUpstream, err)
		}
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("unexpected error matching %v: %v", fs.ErrNotExist, err)
		}
	})

	zip, err := makeZip(map[string][]byte{"example.com@v1.0.0/go.mod": []byte("module example.com")})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	zipFile, err := makeTempFile(t, zip)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	for _, tt := range []struct {
		name          string
		zipFile       string
		modulePath    string
		moduleVersion string
		wantErr       error
	}{
		{
			name:          "Valid",
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			name:          "WrongVersion",
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.1.0",
			wantErr:       fmt.Errorf(`%w: invalid zip file: example.com@v1.0.0/go.mod: path does not have prefix "example.com@v1.1.0/"`, errBadUpstream),
		},
		{
			name:          "MissingFile",
			zipFile:       filepath.Join(t.TempDir(), "404"),
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       fs.ErrNotExist,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := checkZipFile(tt.zipFile, tt.modulePath, tt.moduleVersion)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if tt.wantErr == fs.ErrNotExist {
					if _, ok := errors.AsType[*internalError](err); !ok {
						t.Errorf("got error %v, want an internal error", err)
					}
				}
				if got, want := errors.Is(err, errBadUpstream), errors.Is(tt.wantErr, errBadUpstream); got != want {
					t.Errorf("got bad upstream %t, want %t", got, want)
				}
				if got, want := errors.Is(err, fs.ErrNotExist), errors.Is(tt.wantErr, fs.ErrNotExist); got != want {
					t.Errorf("got not exist %t, want %t", got, want)
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
		name          string
		env           []string
		zipFile       string
		modulePath    string
		moduleVersion string
		wantErr       error
	}{
		{
			name:          "Valid",
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
		},
		{
			name: "SkipSumDB",
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
			name:          "ChecksumMismatch",
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			zipFile:       zipFileInvalid,
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       fmt.Errorf("%w: example.com@v1.0.0: invalid version: untrusted revision v1.0.0", errBadUpstream),
		},
		{
			name:          "LookupError",
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			zipFile:       zipFile,
			modulePath:    "example.com",
			moduleVersion: "v1.1.0",
			wantErr:       fmt.Errorf("%w: example.com@v1.1.0: bad upstream", errBadUpstream),
		},
		{
			name:          "MissingFile",
			env:           []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL},
			zipFile:       filepath.Join(t.TempDir(), "404"),
			modulePath:    "example.com",
			moduleVersion: "v1.0.0",
			wantErr:       fs.ErrNotExist,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gf := &GoFetcher{Env: tt.env, TempDir: t.TempDir()}
			gf.initOnce.Do(gf.init)
			if gf.initErr != nil {
				t.Fatalf("unexpected error %v", gf.initErr)
			}

			sumdbClient := sumdb.NewClient(&sumdbClientOps{sumdbClientState: gf.sumdbClientState, ctx: t.Context()})
			sumdbClient.SetGONOSUMDB(gf.envGONOSUMDB)
			err := verifyZipFile(t.Context(), sumdbClient, tt.zipFile, tt.modulePath, tt.moduleVersion)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if tt.wantErr == fs.ErrNotExist {
					if _, ok := errors.AsType[*internalError](err); !ok {
						t.Errorf("got error %v, want an internal error", err)
					}
				}
				if got, want := errors.Is(err, errBadUpstream), errors.Is(tt.wantErr, errBadUpstream); got != want {
					t.Errorf("got bad upstream %t, want %t", got, want)
				}
				if got, want := errors.Is(err, fs.ErrNotExist), errors.Is(tt.wantErr, fs.ErrNotExist); got != want {
					t.Errorf("got not exist %t, want %t", got, want)
				}
			} else if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}

	t.Run("CanceledBeforeHash", func(t *testing.T) {
		gf := &GoFetcher{Env: []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL}}
		gf.initOnce.Do(gf.init)
		if gf.initErr != nil {
			t.Fatal(gf.initErr)
		}
		sumdbClient := sumdb.NewClient(&sumdbClientOps{sumdbClientState: gf.sumdbClientState, ctx: t.Context()})
		if _, err := sumdbClient.Lookup("example.com", "v1.0.0"); err != nil {
			t.Fatal(err)
		}
		defaultHash := dirhash.DefaultHash
		t.Cleanup(func() { dirhash.DefaultHash = defaultHash })
		dirhash.DefaultHash = func(files []string, open func(string) (io.ReadCloser, error)) (string, error) {
			t.Error("hashing was started after cancellation")
			return defaultHash(files, open)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := verifyZipFile(ctx, sumdbClient, zipFile, "example.com", "v1.0.0"); err != context.Canceled {
			t.Errorf("got error %v, want cancellation", err)
		}
	})

	t.Run("CorruptContent", func(t *testing.T) {
		var buf bytes.Buffer
		zw := archivezip.NewWriter(&buf)
		w, err := zw.CreateHeader(&archivezip.FileHeader{Name: "example.com@v1.0.0/go.mod", Method: archivezip.Store})
		if err != nil {
			t.Fatal(err)
		}
		content := []byte("module example.com")
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		buf.Bytes()[bytes.Index(buf.Bytes(), content)] ^= 1
		zipFile, err := makeTempFile(t, buf.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		gf := &GoFetcher{Env: []string{"GOPROXY=off", "GOSUMDB=" + vkey + " " + sumdbServer.URL}}
		gf.initOnce.Do(gf.init)
		if gf.initErr != nil {
			t.Fatal(gf.initErr)
		}
		sumdbClient := sumdb.NewClient(&sumdbClientOps{sumdbClientState: gf.sumdbClientState, ctx: t.Context()})
		err = verifyZipFile(t.Context(), sumdbClient, zipFile, "example.com", "v1.0.0")
		if !errors.Is(err, errBadUpstream) || !errors.Is(err, archivezip.ErrChecksum) {
			t.Errorf("got error %v, want an upstream checksum error", err)
		}
		if _, ok := errors.AsType[*internalError](err); ok {
			t.Errorf("got error %v, want an upstream error", err)
		}
	})
}

func TestHashFiles(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []string
	}{
		{"Empty", nil},
		{"SingleFile", []string{"go.mod"}},
		{"MultipleFiles", []string{"b.go", "go.mod", "a.go"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			open := func(name string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("content for " + name)), nil
			}
			want, err := dirhash.DefaultHash(tt.files, open)
			if err != nil {
				t.Fatal(err)
			}
			var closed int
			got, err := hashFiles(t.Context(), tt.files, func(name string) (io.ReadCloser, error) {
				return struct {
					io.Reader
					io.Closer
				}{strings.NewReader("content for " + name), closerFunc(func() error {
					closed++
					return nil
				})}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("got hash %q, want %q", got, want)
			}
			if got, want := closed, len(tt.files); got != want {
				t.Errorf("got closed files %d, want %d", got, want)
			}
		})
	}

	t.Run("FileErrors", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			read bool
			err  error
		}{
			{"Open", false, fs.ErrPermission},
			{"Read", true, io.ErrUnexpectedEOF},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var closed bool
				hash, err := hashFiles(t.Context(), []string{"go.mod"}, func(string) (io.ReadCloser, error) {
					if !tt.read {
						return nil, tt.err
					}
					return struct {
						io.Reader
						io.Closer
					}{iotest.ErrReader(tt.err), closerFunc(func() error {
						closed = true
						return nil
					})}, nil
				})
				if hash != "" || err != tt.err {
					t.Errorf("got %q, %v, want an empty hash and %v", hash, err, tt.err)
				}
				if closed != tt.read {
					t.Errorf("got closed %t, want %t", closed, tt.read)
				}
			})
		}
	})

	t.Run("CanceledBetweenFiles", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var opened, closed int
		hash, err := hashFiles(ctx, []string{"b", "a"}, func(name string) (io.ReadCloser, error) {
			opened++
			if name != "a" {
				t.Errorf("unexpected file opened %q", name)
			}
			return struct {
				io.Reader
				io.Closer
			}{strings.NewReader("content"), closerFunc(func() error {
				closed++
				cancel()
				return nil
			})}, nil
		})
		if hash != "" || err != context.Canceled {
			t.Errorf("got %q, %v, want an empty hash and cancellation", hash, err)
		}
		if opened != 1 || closed != 1 {
			t.Errorf("got opened %d, closed %d, want 1, 1", opened, closed)
		}
	})

	t.Run("DuringRead", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			err  error
		}{
			{"Canceled", context.Canceled},
			{"DeadlineExceeded", context.DeadlineExceeded},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					content := bytes.Repeat([]byte("content"), 10000)
					var bytesRead, reads int
					var closed bool
					hash, err := hashFiles(ctx, []string{"go.mod"}, func(string) (io.ReadCloser, error) {
						r := &testReadSeeker{
							ReadSeeker: bytes.NewReader(content),
							read: func(rs io.ReadSeeker, p []byte) (int, error) {
								reads++
								if reads != 1 {
									t.Error("unexpected read after cancellation")
								}
								n, err := rs.Read(p)
								bytesRead += n
								if tt.err == context.Canceled {
									cancel()
								} else {
									<-ctx.Done()
								}
								return n, err
							},
						}
						return struct {
							io.Reader
							io.Closer
							io.WriterTo
						}{r, closerFunc(func() error {
							closed = true
							return nil
						}), writerToFunc(func(w io.Writer) (int64, error) {
							t.Error("WriterTo bypassed cancellation checks")
							return io.Copy(w, bytes.NewReader(content))
						})}, nil
					})
					if hash != "" || err != tt.err {
						t.Errorf("got %q, %v, want an empty hash and %v", hash, err, tt.err)
					}
					if bytesRead == 0 || bytesRead >= len(content) {
						t.Errorf("got bytes read %d, want part of %d", bytesRead, len(content))
					}
					if !closed {
						t.Error("file was not closed")
					}
				})
			})
		}
	})
}

func TestContextReadCloserRead(t *testing.T) {
	content := []byte("module example.com")
	r := contextReadCloser{ReadCloser: io.NopCloser(bytes.NewReader(content)), ctx: t.Context()}
	if err := iotest.TestReader(r, content); err != nil {
		t.Fatal(err)
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
