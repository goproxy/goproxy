package goproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/note"
)

func TestNewSumDBClientOps(t *testing.T) {
	for _, tt := range []struct {
		n          int
		envGOSUMDB string
		wantErr    error
	}{
		{1, defaultEnvGOSUMDB, nil},
		{2, defaultEnvGOSUMDB + " https://example.com", nil},
		{3, "", errors.New("missing GOSUMDB")},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			sco, err := newSumdbClientOps(defaultEnvGOPROXY, tt.envGOSUMDB, http.DefaultClient)
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
				if got, want := sco.name, defaultEnvGOSUMDB; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := sco.key, sumGolangOrgKey; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSumDBClientOpsURL(t *testing.T) {
	t.Run("ConcurrentDiscovery", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			firstStatus int
			nextStatus  int
			wantURL     string
		}{
			{"Proxy", http.StatusOK, http.StatusOK, "https://example.com/sumdb/" + defaultEnvGOSUMDB},
			{"ProxyThenError", http.StatusOK, http.StatusBadRequest, "https://example.com/sumdb/" + defaultEnvGOSUMDB},
			{"ProxyThenNotFound", http.StatusOK, http.StatusNotFound, "https://example.com/sumdb/" + defaultEnvGOSUMDB},
			{"DirectThenProxy", http.StatusNotFound, http.StatusOK, "https://" + defaultEnvGOSUMDB},
			{"DirectThenError", http.StatusNotFound, http.StatusBadRequest, "https://" + defaultEnvGOSUMDB},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const callers = 16
				var started, finished sync.WaitGroup
				started.Add(callers)
				var attempts atomic.Int32
				client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					code := tt.nextStatus
					if attempts.Add(1) == 1 {
						started.Wait()
						code = tt.firstStatus
					}
					return &http.Response{
						StatusCode: code,
						Status:     strconv.Itoa(code) + " " + http.StatusText(code),
						Body:       http.NoBody,
						Request:    req,
					}, nil
				})}
				sco, err := newSumdbClientOps("https://example.com", defaultEnvGOSUMDB, client)
				if err != nil {
					t.Fatal(err)
				}
				lookup := func() {
					u, err := sco.url()
					if err != nil {
						t.Errorf("unexpected error %v", err)
						return
					}
					if got := u.String(); got != tt.wantURL {
						t.Errorf("got URL %q, want %q", got, tt.wantURL)
					}
				}
				for range callers {
					finished.Go(func() {
						started.Done()
						lookup()
					})
				}
				finished.Wait()
				lookup()
				if got, want := attempts.Load(), int32(1); got != want {
					t.Errorf("got attempts %d, want %d", got, want)
				}
			})
		}
	})

	t.Run("FailureCacheExpiry", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			attempts := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				code := http.StatusOK
				if attempts == 1 {
					code = http.StatusBadRequest
				}
				return &http.Response{
					StatusCode: code,
					Status:     strconv.Itoa(code) + " " + http.StatusText(code),
					Body:       http.NoBody,
					Request:    req,
				}, nil
			})}
			sco, err := newSumdbClientOps("https://example.com", defaultEnvGOSUMDB, client)
			if err != nil {
				t.Fatal(err)
			}
			u, err := sco.url()
			if err == nil || u != nil {
				t.Fatalf("got (%v, %v), want an error and no URL", u, err)
			}
			time.Sleep(10*time.Second - time.Nanosecond)
			if nextURL, nextErr := sco.url(); nextURL != u || nextErr != err {
				t.Errorf("got (%v, %v), want (%v, %v)", nextURL, nextErr, u, err)
			}
			if got, want := attempts, 1; got != want {
				t.Errorf("got attempts %d, want %d", got, want)
			}
			time.Sleep(time.Nanosecond)
			u, err = sco.url()
			if err != nil {
				t.Fatal(err)
			}
			if got, want := u.String(), "https://example.com/sumdb/"+defaultEnvGOSUMDB; got != want {
				t.Errorf("got URL %q, want %q", got, want)
			}
			if nextURL, nextErr := sco.url(); nextURL != u || nextErr != nil {
				t.Errorf("got (%v, %v), want (%v, nil)", nextURL, nextErr, u)
			}
			if got, want := attempts, 2; got != want {
				t.Errorf("got attempts %d, want %d", got, want)
			}
		})
	})

	t.Run("TimeoutFallback", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			suffix    string
			wantHosts []string
			wantURL   string
		}{
			{"SingleProxy", "", []string{"example.com"}, ""},
			{"CommaProxy", ",https://alt.example.com", []string{"example.com"}, ""},
			{"PipeProxy", "|https://alt.example.com", []string{"example.com", "alt.example.com"}, "https://alt.example.com/sumdb/" + defaultEnvGOSUMDB},
			{"CommaDirect", ",direct", []string{"example.com"}, ""},
			{"PipeDirect", "|direct", []string{"example.com"}, "https://" + defaultEnvGOSUMDB},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var hosts []string
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						hosts = append(hosts, req.URL.Host)
						if _, ok := req.Context().Deadline(); !ok {
							t.Error("request has no deadline")
							return nil, context.DeadlineExceeded
						}
						if req.URL.Host == "example.com" {
							<-req.Context().Done()
							return nil, req.Context().Err()
						}
						return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
					})}
					sco, err := newSumdbClientOps("https://example.com"+tt.suffix, defaultEnvGOSUMDB, client)
					if err != nil {
						t.Fatal(err)
					}
					started := time.Now()
					u, err := sco.url()
					if tt.wantURL == "" {
						if !errors.Is(err, context.DeadlineExceeded) || u != nil {
							t.Errorf("got (%v, %v), want a timeout and no URL", u, err)
						}
					} else if err != nil {
						t.Fatalf("unexpected error %v", err)
					} else if got := u.String(); got != tt.wantURL {
						t.Errorf("got URL %q, want %q", got, tt.wantURL)
					}
					if nextURL, nextErr := sco.url(); nextURL != u || nextErr != err {
						t.Errorf("got (%v, %v), want (%v, %v)", nextURL, nextErr, u, err)
					}
					if !slices.Equal(hosts, tt.wantHosts) {
						t.Errorf("got hosts %q, want %q", hosts, tt.wantHosts)
					}
					if got, want := time.Since(started), time.Minute; got != want {
						t.Errorf("got duration %v, want %v", got, want)
					}
				})
			})
		}
	})

	t.Run("HTTPStatusFallback", func(t *testing.T) {
		for _, statusCode := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusGone} {
			for _, tt := range []struct {
				name    string
				suffix  string
				wantURL string
			}{
				{"SingleProxy", "", "https://" + defaultEnvGOSUMDB},
				{"CommaProxy", ",https://alt.example.com", "https://alt.example.com/sumdb/" + defaultEnvGOSUMDB},
				{"PipeProxy", "|https://alt.example.com", "https://alt.example.com/sumdb/" + defaultEnvGOSUMDB},
				{"CommaDirect", ",direct", "https://" + defaultEnvGOSUMDB},
				{"PipeDirect", "|direct", "https://" + defaultEnvGOSUMDB},
			} {
				t.Run(strconv.Itoa(statusCode)+"/"+tt.name, func(t *testing.T) {
					attempts := 0
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						attempts++
						if got, want := req.URL.Path, "/sumdb/"+defaultEnvGOSUMDB+"/supported"; got != want {
							t.Errorf("got path %q, want %q", got, want)
						}
						code := statusCode
						if req.URL.Host == "alt.example.com" {
							code = http.StatusOK
						}
						return &http.Response{
							StatusCode: code,
							Status:     strconv.Itoa(code) + " " + http.StatusText(code),
							Body:       http.NoBody,
							Request:    req,
						}, nil
					})}
					sco, err := newSumdbClientOps("https://example.com"+tt.suffix, defaultEnvGOSUMDB, client)
					if err != nil {
						t.Fatal(err)
					}
					u, err := sco.url()
					wantAttempts := 1
					if statusCode == http.StatusBadRequest && !strings.HasPrefix(tt.suffix, "|") {
						if err == nil || errors.Is(err, fs.ErrNotExist) {
							t.Errorf("got %v, want a non-absence error", err)
						}
						if u != nil {
							t.Errorf("got URL %v, want nil", u)
						}
					} else {
						if err != nil {
							t.Fatalf("unexpected error %v", err)
						}
						if got, want := u.String(), tt.wantURL; got != want {
							t.Errorf("got URL %q, want %q", got, want)
						}
						if strings.Contains(tt.suffix, "alt.example.com") {
							wantAttempts = 2
						}
					}
					if nextURL, nextErr := sco.url(); nextURL != u || nextErr != err {
						t.Errorf("got (%v, %v), want (%v, %v)", nextURL, nextErr, u, err)
					}
					if got, want := attempts, wantAttempts; got != want {
						t.Errorf("got attempts %d, want %d", got, want)
					}
				})
			}
		}
	})

	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		envGOPROXY   func(proxyServerURL string) string
		envGOSUMDB   string
		wantURL      func(proxyServerURL string) string
		wantErr      error
		doubleCheck  bool
	}{
		{
			n:          1,
			envGOPROXY: func(_ string) string { return "direct" },
			envGOSUMDB: defaultEnvGOSUMDB,
			wantURL:    func(_ string) string { return "https://" + defaultEnvGOSUMDB },
		},
		{
			n:          2,
			envGOPROXY: func(_ string) string { return "direct" },
			envGOSUMDB: defaultEnvGOSUMDB + " https://example.com",
			wantURL:    func(_ string) string { return "https://example.com" },
		},
		{
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      func(proxyServerURL string) string { return proxyServerURL + "/sumdb/" + defaultEnvGOSUMDB },
		},
		{
			n:            4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -1) },
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      func(_ string) string { return "https://" + defaultEnvGOSUMDB },
		},
		{
			n:            5,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -1) },
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL + ",direct" },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      func(_ string) string { return "https://" + defaultEnvGOSUMDB },
		},
		{
			n:            6,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL + ",off" },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      func(_ string) string { return "https://" + defaultEnvGOSUMDB },
		},
		{
			n:            7,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseInternalServerError(rw, req) },
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantErr:      errBadUpstream,
			doubleCheck:  true,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)
			envGOPROXY := tt.envGOPROXY(proxyServer.URL)

			sco, err := newSumdbClientOps(envGOPROXY, tt.envGOSUMDB, http.DefaultClient)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			u, err := sco.url()
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
				if got, want := u.String(), tt.wantURL(proxyServer.URL); got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}

			if tt.doubleCheck {
				u2, err2 := sco.url()
				if got, want := err2, err; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := u2, u; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSumDBClientOpsReadRemote(t *testing.T) {
	t.Run("Timeout", func(t *testing.T) {
		for _, tt := range []struct {
			name           string
			firstStatus    int
			bodyStatus     int
			discoveryDelay time.Duration
			clientTimeout  time.Duration
			wantAttempts   int
			wantDuration   time.Duration
		}{
			{"Headers", 0, 0, 0, 0, 1, time.Minute},
			{"Body", 0, http.StatusOK, 0, 0, 1, time.Minute},
			{"ErrorBody", 0, http.StatusServiceUnavailable, 0, 0, 1, time.Minute},
			{"Retry", http.StatusServiceUnavailable, 0, 0, 0, 2, time.Minute},
			{"Redirect", http.StatusFound, 0, 0, 0, 2, time.Minute},
			{"AfterDiscovery", 0, 0, 40 * time.Second, 0, 1, 100 * time.Second},
			{"ShorterClientTimeout", 0, 0, 0, 5 * time.Second, 1, 5 * time.Second},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					attempts := 0
					var body *testHTTPResponseBody
					client := &http.Client{Timeout: tt.clientTimeout, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						if _, ok := req.Context().Deadline(); !ok {
							t.Error("request has no deadline")
							return nil, context.DeadlineExceeded
						}
						if strings.HasSuffix(req.URL.Path, "/supported") {
							time.Sleep(tt.discoveryDelay)
							return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
						}
						attempts++
						if attempts == 1 && tt.firstStatus != 0 {
							time.Sleep(40 * time.Second)
							return &http.Response{
								StatusCode: tt.firstStatus,
								Header:     http.Header{"Location": {"/redirected"}, "Retry-After": {"1"}},
								Body:       http.NoBody,
							}, nil
						}
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
					})}
					envGOSUMDB := defaultEnvGOSUMDB + " https://example.com"
					if tt.discoveryDelay > 0 {
						envGOSUMDB = defaultEnvGOSUMDB
					}
					sco, err := newSumdbClientOps("https://example.com", envGOSUMDB, client)
					if err != nil {
						t.Fatal(err)
					}
					started := time.Now()
					b, err := sco.ReadRemote("/lookup/example.com@v1.0.0")
					if !errors.Is(err, context.DeadlineExceeded) || !isFetchTimedOutError(err) {
						t.Errorf("got error %v, want a timeout", err)
					}
					if b != nil {
						t.Errorf("got content %q, want nil", b)
					}
					if got := attempts; got != tt.wantAttempts {
						t.Errorf("got attempts %d, want %d", got, tt.wantAttempts)
					}
					if got := time.Since(started); got != tt.wantDuration {
						t.Errorf("got duration %v, want %v", got, tt.wantDuration)
					}
					if body != nil && !body.closed {
						t.Error("response body was not closed")
					}
				})
			})
		}
	})

	t.Run("ConcurrentTimeouts", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if _, ok := req.Context().Deadline(); !ok {
					t.Error("request has no deadline")
					return nil, context.DeadlineExceeded
				}
				<-req.Context().Done()
				return nil, req.Context().Err()
			})}
			sco, err := newSumdbClientOps("direct", defaultEnvGOSUMDB+" https://example.com", client)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := sco.ReadRemote("/lookup/first.example.com@v1.0.0")
				done <- err
			}()
			synctest.Wait()
			time.Sleep(30 * time.Second)
			started := time.Now()
			_, err = sco.ReadRemote("/lookup/second.example.com@v1.0.0")
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got error %v, want a timeout", err)
			}
			if got, want := time.Since(started), time.Minute; got != want {
				t.Errorf("got duration %v, want %v", got, want)
			}
			if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got error %v, want a timeout", err)
			}
		})
	})

	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		wantContent  string
		wantErr      error
	}{
		{
			n:            1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, "foobar") },
			wantContent:  "foobar",
		},
		{
			n: 2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if !strings.HasSuffix(req.URL.Path, "/supported") {
					responseInternalServerError(rw, req)
				}
			},
			wantErr: errBadUpstream,
		},
		{
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseInternalServerError(rw, req) },
			wantErr:      errBadUpstream,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)

			sco, err := newSumdbClientOps(proxyServer.URL, defaultEnvGOSUMDB, http.DefaultClient)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			b, err := sco.ReadRemote("file")
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
				if got, want := string(b), tt.wantContent; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSumDBClientOpsReadConfig(t *testing.T) {
	t.Run("ClientContinuity", func(t *testing.T) {
		skey, vkey, err := note.GenerateKey(nil, "sumdb.example.com")
		if err != nil {
			t.Fatal(err)
		}
		gosum := func(path, vers string) ([]byte, error) {
			return fmt.Appendf(nil, "%s %s h1:checksum\n", path, vers), nil
		}
		original := sumdb.NewServer(sumdb.NewTestServer(skey, gosum))
		fork := sumdb.NewTestServer(skey, gosum)
		if _, err := fork.Lookup(t.Context(), module.Version{Path: "example.net", Version: "v1.0.0"}); err != nil {
			t.Fatal(err)
		}
		forkHandler := sumdb.NewServer(fork)
		var useFork atomic.Bool
		httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			if useFork.Load() {
				forkHandler.ServeHTTP(rec, req)
			} else {
				original.ServeHTTP(rec, req)
			}
			resp := rec.Result()
			resp.Request = req
			return resp, nil
		})}
		sco, err := newSumdbClientOps("direct", vkey+" https://sumdb.example.com", httpClient)
		if err != nil {
			t.Fatal(err)
		}
		lookup := func(vers string) error {
			cache := &sumdbClientOpsCache{sumdbClientOps: sco}
			_, err := sumdb.NewClient(cache).Lookup("example.com", vers)
			if err == nil {
				cache.save()
			}
			return err
		}
		if err := lookup("v1.0.0"); err != nil {
			t.Fatal(err)
		}
		latest, err := sco.ReadConfig("sumdb.example.com/latest")
		if err != nil {
			t.Fatal(err)
		}
		if len(latest) == 0 {
			t.Fatal("missing signed tree")
		}
		useFork.Store(true)
		err = lookup("v1.1.0")
		if err == nil || err.Error() != "example.com@v1.1.0: "+sumdb.ErrSecurity.Error() {
			t.Fatalf("got error %v, want a security error", err)
		}
		if got, err := sco.ReadConfig("sumdb.example.com/latest"); err != nil || !bytes.Equal(got, latest) {
			t.Errorf("got signed tree %q and error %v, want %q and no error", got, err, latest)
		}
		useFork.Store(false)
		if err := lookup("v1.1.0"); err != nil {
			t.Fatal(err)
		}
	})

	for _, tt := range []struct {
		n           int
		file        string
		wantContent string
		wantErr     error
	}{
		{
			n:           1,
			file:        "key",
			wantContent: sumGolangOrgKey,
		},
		{
			n:    2,
			file: "/latest",
		},
		{
			n:       3,
			file:    "file",
			wantErr: errors.New("unknown config file"),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			sco, err := newSumdbClientOps("direct", defaultEnvGOSUMDB, http.DefaultClient)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			b, err := sco.ReadConfig(tt.file)
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
				if got, want := string(b), tt.wantContent; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSumDBClientOpsWriteConfig(t *testing.T) {
	t.Run("CompareAndSwap", func(t *testing.T) {
		sco := &sumdbClientOps{}
		new := []byte("first tree")
		if err := sco.WriteConfig("/latest", nil, new); err != nil {
			t.Fatal(err)
		}
		new[0] = 'x'
		old, err := sco.ReadConfig("/latest")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(old), "first tree"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		old[0] = 'x'
		if err := sco.WriteConfig("/latest", old, []byte("second tree")); err != sumdb.ErrWriteConflict {
			t.Fatalf("got error %v, want %v", err, sumdb.ErrWriteConflict)
		}
		old, err = sco.ReadConfig("/latest")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(old), "first tree"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if err := sco.WriteConfig("/latest", old, []byte("second tree")); err != nil {
			t.Fatal(err)
		}
		if got, err := sco.ReadConfig("/latest"); err != nil || string(got) != "second tree" {
			t.Errorf("got (%q, %v), want (%q, nil)", got, err, "second tree")
		}
	})

	t.Run("ConcurrentWrites", func(t *testing.T) {
		sco := &sumdbClientOps{}
		var wg sync.WaitGroup
		var written atomic.Int32
		for i := range 16 {
			wg.Go(func() {
				if err := sco.WriteConfig("/latest", nil, []byte{byte(i)}); err == nil {
					written.Add(1)
				} else if err != sumdb.ErrWriteConflict {
					t.Errorf("unexpected error %v", err)
				}
			})
		}
		wg.Wait()
		if got, want := written.Load(), int32(1); got != want {
			t.Errorf("got writes %d, want %d", got, want)
		}
	})
}

func TestSumDBClientOpsWriteCache(t *testing.T) {
	sco := &sumdbClientOps{}
	data := []byte("cached data")
	sco.WriteCache("file", data)
	data[0] = 'x'
	got, err := sco.ReadCache("file")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "cached data" {
		t.Fatalf("got %q, want %q", got, "cached data")
	}
	got[0] = 'x'
	got, err = sco.ReadCache("file")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "cached data" {
		t.Errorf("got %q, want %q", got, "cached data")
	}
}

func TestSumDBClientOpsExtraCalls(t *testing.T) {
	for _, tt := range []struct {
		n       int
		call    func(sco *sumdbClientOps) error
		wantErr error
	}{
		{
			n: 1,
			call: func(sco *sumdbClientOps) error {
				return sco.WriteConfig("", nil, nil)
			},
		},
		{
			n: 2,
			call: func(sco *sumdbClientOps) error {
				sco.WriteCache("", nil)
				return nil
			},
		},
		{
			n: 3,
			call: func(sco *sumdbClientOps) error {
				sco.Log("")
				return nil
			},
		},
		{
			n: 4,
			call: func(sco *sumdbClientOps) error {
				sco.SecurityError("")
				return nil
			},
		},
		{
			n: 5,
			call: func(sco *sumdbClientOps) error {
				_, err := sco.ReadCache("")
				return err
			},
			wantErr: fs.ErrNotExist,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			err := tt.call(&sumdbClientOps{})
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

func TestSumDBClientOpsCacheSave(t *testing.T) {
	sco := &sumdbClientOps{}
	sco.WriteCache("existing", []byte("original"))
	cache := &sumdbClientOpsCache{sumdbClientOps: sco}
	if got, err := cache.ReadCache("existing"); err != nil || string(got) != "original" {
		t.Fatalf("got (%q, %v), want (%q, nil)", got, err, "original")
	}
	data := []byte("buffered")
	cache.WriteCache("new", data)
	data[0] = 'x'
	if _, err := sco.ReadCache("new"); err != fs.ErrNotExist {
		t.Errorf("got error %v, want %v", err, fs.ErrNotExist)
	}
	got, err := cache.ReadCache("new")
	if err != nil || string(got) != "buffered" {
		t.Fatalf("got (%q, %v), want (%q, nil)", got, err, "buffered")
	}
	got[0] = 'x'
	cache.WriteCache("existing", []byte("updated"))
	if got, err := sco.ReadCache("existing"); err != nil || string(got) != "original" {
		t.Errorf("got (%q, %v), want (%q, nil)", got, err, "original")
	}
	cache.save()
	for _, tt := range []struct {
		file string
		want string
	}{
		{"existing", "updated"},
		{"new", "buffered"},
	} {
		if got, err := sco.ReadCache(tt.file); err != nil || string(got) != tt.want {
			t.Errorf("got (%q, %v), want (%q, nil)", got, err, tt.want)
		}
	}
}
