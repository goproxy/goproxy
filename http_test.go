package goproxy

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestHTTPGet(t *testing.T) {
	t.Run("CacheRestrictions", func(t *testing.T) {
		for _, statusCode := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusGone} {
			for _, tt := range []struct {
				name             string
				header           http.Header
				body             string
				wantCacheControl string
			}{
				{"OrdinaryAbsence", nil, "module unavailable", "public, max-age=600"},
				{"NoStore", http.Header{"Cache-Control": {"no-store"}}, "module unavailable", "no-store"},
				{"NoCache", http.Header{"Cache-Control": {"no-cache"}}, "module unavailable", "no-store"},
				{"Private", http.Header{"Cache-Control": {"private"}}, "module unavailable", "no-store"},
				{"VaryAll", http.Header{"Vary": {"*"}}, "module unavailable", "no-store"},
				{"BadUpstreamText", nil, "unknown revision bad upstream", "public, max-age=600"},
				{"TimeoutText", nil, "unknown revision fetch timed out", "public, max-age=600"},
				{"RestrictedTimeoutText", http.Header{"Cache-Control": {"no-store"}}, "request timed out", "no-store"},
				{"RestrictionAfterSplitQuotedArgument", http.Header{"Cache-Control": {`extension="a`, `b", no-store`}}, "module unavailable", "no-store"},
				{"SplitQuotedArgument", http.Header{"Cache-Control": {`extension="a`, "no-store", `b", public`}}, "module unavailable", "public, max-age=600"},
			} {
				t.Run(strconv.Itoa(statusCode)+"/"+tt.name, func(t *testing.T) {
					server := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
						maps.Copy(rw.Header(), tt.header)
						rw.WriteHeader(statusCode)
						fmt.Fprint(rw, tt.body)
					}))
					err := httpGet(t.Context(), http.DefaultClient, server.URL, nil)
					if !errors.Is(err, fs.ErrNotExist) {
						t.Fatalf("got %v, want %v", err, fs.ErrNotExist)
					}
					if got, want := err.Error(), tt.body; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					rec := httptest.NewRecorder()
					responseError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err, false)
					if got, want := rec.Code, http.StatusNotFound; got != want {
						t.Errorf("got %d, want %d", got, want)
					}
					if got, want := rec.Header().Get("Cache-Control"), tt.wantCacheControl; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					if got, want := rec.Body.String(), "not found: "+tt.body; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
				})
			}
		}
	})

	t.Run("Normal", func(t *testing.T) {
		for _, tt := range []struct {
			n             int
			ctxTimeout    time.Duration
			clientTimeout time.Duration
			handler       http.HandlerFunc
			configServer  func(server *httptest.Server)
			wantContent   string
			wantErr       func(serverURL string) error
		}{
			{
				n:           1,
				handler:     func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, "foobar") },
				wantContent: "foobar",
			},
			{
				n: 2,
				handler: func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusNotFound)
					fmt.Fprint(rw, "not found")
				},
				wantErr: func(_ string) error { return fs.ErrNotExist },
			},
			{
				n: 3,
				handler: func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(rw, "internal server error")
				},
				wantErr: func(_ string) error { return errBadUpstream },
			},
			{
				n: 4,
				handler: func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusGatewayTimeout)
					fmt.Fprint(rw, "gateway timeout")
				},
				wantErr: func(_ string) error { return errFetchTimedOut },
			},
			{
				n: 5,
				handler: func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusNotImplemented)
					fmt.Fprint(rw, "not implemented")
				},
				wantErr: func(serverURL string) error {
					return fmt.Errorf("GET %s: 501 Not Implemented: not implemented", serverURL)
				},
			},
			{
				n:          6,
				ctxTimeout: 450 * time.Millisecond,
				handler: func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(rw, "internal server error")
				},
				wantErr: func(_ string) error { return context.DeadlineExceeded },
			},
			{
				n:          7,
				ctxTimeout: -1,
				handler: func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(rw, "internal server error")
				},
				wantErr: func(_ string) error { return context.Canceled },
			},
			{
				n: 8,
				handler: func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusInternalServerError)
					rw.(http.Flusher).Flush()
				},
				configServer: func(server *httptest.Server) {
					handler := server.Config.Handler
					server.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
						handler.ServeHTTP(rw, req)
						server.CloseClientConnections()
					})
				},
				wantErr: func(_ string) error { return io.ErrUnexpectedEOF },
			},
			{
				n:             9,
				clientTimeout: 50 * time.Millisecond,
				handler: func(rw http.ResponseWriter, req *http.Request) {
					time.Sleep(50 * time.Millisecond)
					rw.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(rw, "internal server error")
				},
				wantErr: func(serverURL string) error {
					return fmt.Errorf("Get %q: context deadline exceeded (Client.Timeout exceeded while awaiting headers)", serverURL)
				},
			},
		} {
			t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
				ctx := t.Context()
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

				client := http.DefaultClient
				if tt.clientTimeout > 0 {
					client2 := *client
					client2.Timeout = tt.clientTimeout
					client = &client2
				}

				server := newHTTPTestServer(t, tt.handler)
				if tt.configServer != nil {
					tt.configServer(server)
				}

				var wantErr error
				if tt.wantErr != nil {
					wantErr = tt.wantErr(server.URL)
				}

				var content bytes.Buffer
				err := httpGet(ctx, client, server.URL, &content)
				if wantErr != nil {
					if err == nil {
						t.Fatal("expected error")
					}
					if got, want := err, wantErr; !compareErrors(got, want) {
						t.Errorf("got %v, want %v", got, want)
					}
				} else {
					if err != nil {
						t.Fatalf("unexpected error %v", err)
					}
					if got, want := content.String(), tt.wantContent; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
				}
			})
		}
	})

	t.Run("InvalidURL", func(t *testing.T) {
		if err := httpGet(t.Context(), http.DefaultClient, "::", nil); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestHTTPGetTemp(t *testing.T) {
	for _, tt := range []struct {
		n           int
		handler     http.HandlerFunc
		tempDir     string
		wantContent string
		wantErr     error
	}{
		{
			n:           1,
			handler:     func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, "foobar") },
			wantContent: "foobar",
		},
		{
			n: 2,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotFound)
				fmt.Fprint(rw, "not found")
			},
			wantErr: fs.ErrNotExist,
		},
		{
			n:       3,
			tempDir: filepath.Join(os.TempDir(), "404"),
			wantErr: fs.ErrNotExist,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			server := newHTTPTestServer(t, tt.handler)
			if tt.tempDir == "" {
				tt.tempDir = t.TempDir()
			}

			tempFile, err := httpGetTemp(t.Context(), http.DefaultClient, server.URL, tt.tempDir)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
				if _, err := os.Stat(tempFile); err == nil {
					t.Error("expected error")
				} else if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if b, err := os.ReadFile(tempFile); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), tt.wantContent; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestIsRetryableHTTPClientDoError(t *testing.T) {
	for _, tt := range []struct {
		n               int
		err             error
		wantIsRetryable bool
	}{
		{1, syscall.ECONNRESET, true},
		{2, errors.New("oops"), true},
		{3, context.Canceled, false},
		{4, context.DeadlineExceeded, false},
		{5, &url.Error{Err: errors.New("oops")}, true},
		{6, &url.Error{Err: x509.UnknownAuthorityError{}}, false},
		{7, &url.Error{Err: http.ErrSchemeMismatch}, false},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			if got, want := isRetryableHTTPClientDoError(tt.err), tt.wantIsRetryable; got != want {
				t.Errorf("got %t, want %t", got, want)
			}
		})
	}
}

func TestIsCacheRestrictedHTTPResponse(t *testing.T) {
	for _, tt := range []struct {
		name   string
		header http.Header
		want   bool
	}{
		{"Absent", nil, false},
		{"Empty", http.Header{"Cache-Control": {""}}, false},
		{"Public", http.Header{"Cache-Control": {"public, max-age=60"}}, false},
		{"NoStore", http.Header{"Cache-Control": {"no-store"}}, true},
		{"NoCache", http.Header{"Cache-Control": {"no-cache"}}, true},
		{"Private", http.Header{"Cache-Control": {"private"}}, true},
		{"MixedCase", http.Header{"Cache-Control": {"Public, No-StOrE"}}, true},
		{"Whitespace", http.Header{"Cache-Control": {" \tno-store \t"}}, true},
		{"MultipleFields", http.Header{"Cache-Control": {"public", "max-age=60", "no-store"}}, true},
		{"EmptyDirectives", http.Header{"Cache-Control": {",, public, , no-store,"}}, true},
		{"TrailingComma", http.Header{"Cache-Control": {"public,"}}, false},
		{"QualifiedNoCache", http.Header{"Cache-Control": {`no-cache="ETag, Last-Modified"`}}, true},
		{"QualifiedPrivate", http.Header{"Cache-Control": {`private = "X-Private"`}}, true},
		{"DirectivePrefixes", http.Header{"Cache-Control": {"no-store-extra, x-no-cache, private-extra"}}, false},
		{"UnquotedArgument", http.Header{"Cache-Control": {"extension=no-store"}}, false},
		{"QuotedArgument", http.Header{"Cache-Control": {`extension="no-store"`}}, false},
		{"QuotedCommas", http.Header{"Cache-Control": {`extension="a,no-store,no-cache,private,b", public`}}, false},
		{"RestrictionAfterQuotedCommas", http.Header{"Cache-Control": {`extension="a,b", no-store`}}, true},
		{"RestrictionAfterSplitQuotedArgument", http.Header{"Cache-Control": {`extension="a`, `b", no-store`}}, true},
		{"SplitQuotedArgument", http.Header{"Cache-Control": {`extension="a`, "no-store", "no-cache", "private", `b", public`}}, false},
		{"EscapedQuote", http.Header{"Cache-Control": {`extension="a\",no-store,b", public`}}, false},
		{"EscapedBackslash", http.Header{"Cache-Control": {`extension="a\\", no-store`}}, true},
		{"EscapedBackslashAndQuote", http.Header{"Cache-Control": {`extension="a\\\",no-store,b", public`}}, false},
		{"VaryAll", http.Header{"Vary": {"*"}}, true},
		{"VaryFields", http.Header{"Vary": {"Accept-Encoding, Accept"}}, false},
		{"VaryMultipleFields", http.Header{"Vary": {"Accept-Encoding", "Accept, \t* "}}, true},
		{"VaryName", http.Header{"Vary": {"X-No-Store"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := isCacheRestrictedHTTPResponse(tt.header), tt.want; got != want {
				t.Errorf("got %t, want %t", got, want)
			}
		})
	}
}
