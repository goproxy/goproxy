package goproxy

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

func TestNotExistError(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		for _, tt := range []struct {
			n       int
			err     error
			wantErr error
		}{
			{1, notExistErrorf(""), errors.New("")},
			{2, notExistErrorf("foobar"), errors.New("foobar")},
			{3, notExistErrorf("foobar"), fs.ErrNotExist},
		} {
			t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
				if got, want := tt.err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			})
		}
	})

	t.Run("ErrorIs", func(t *testing.T) {
		e := &notExistError{err: errors.New("foobar")}
		for _, tt := range []struct {
			n      int
			err    error
			wantIs bool
		}{
			{1, fs.ErrNotExist, true},
			{2, io.EOF, false},
		} {
			t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
				if got, want := e.Is(tt.err), tt.wantIs; got != want {
					t.Errorf("got %t, want %t", got, want)
				}
				if got, want := errors.Is(e, tt.err), tt.wantIs; got != want {
					t.Errorf("got %t, want %t", got, want)
				}
			})
		}
	})
}

func TestHTTPGet(t *testing.T) {
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
