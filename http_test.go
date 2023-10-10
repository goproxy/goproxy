package goproxy

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNotFoundError(t *testing.T) {
	for _, tt := range []struct {
		n         int
		nfe       notFoundError
		wantError string
	}{
		{1, notFoundError(""), ""},
		{2, notFoundError("foobar"), "foobar"},
	} {
		if got, want := tt.nfe.Error(), tt.wantError; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}

	var nfe notFoundError
	for _, tt := range []struct {
		n      int
		err    error
		wantIs bool
	}{
		{1, errNotFound, true},
		{2, io.EOF, false},
	} {
		if got, want := nfe.Is(tt.err), tt.wantIs; got != want {
			t.Errorf("test(%d): got %v, want %v", tt.n, got, want)
		}
		if got, want := errors.Is(nfe, tt.err), tt.wantIs; got != want {
			t.Errorf("test(%d): got %v, want %v", tt.n, got, want)
		}
	}
}

func TestHTTPGet(t *testing.T) {
	savedBackoffRand := backoffRand
	backoffRand = rand.New(rand.NewSource(1))
	defer func() { backoffRand = savedBackoffRand }()

	var handler http.HandlerFunc
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) { handler(rw, req) }))
	defer server.Close()

	for _, tt := range []struct {
		n                  int
		ctxTimeout         time.Duration
		httpClient         *http.Client
		handler            http.HandlerFunc
		wantContent        string
		wantError          string
		isWantErrorPartial bool
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
			wantError: "not found",
		},
		{
			n: 3,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantError: "bad upstream",
		},
		{
			n: 4,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusGatewayTimeout)
				fmt.Fprint(rw, "gateway timeout")
			},
			wantError: "fetch timed out",
		},
		{
			n: 5,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotImplemented)
				fmt.Fprint(rw, "not implemented")
			},
			wantError: fmt.Sprintf("GET %s: 501 Not Implemented: not implemented", server.URL),
		},
		{
			n:          6,
			ctxTimeout: 450 * time.Millisecond,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantError: "bad upstream",
		},
		{
			n:          7,
			ctxTimeout: -1,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantError:          context.Canceled.Error(),
			isWantErrorPartial: true,
		},
		{
			n: 8,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				rw.(http.Flusher).Flush()
				server.CloseClientConnections()
			},
			wantError: io.ErrUnexpectedEOF.Error(),
		},
		{
			n:          9,
			httpClient: &http.Client{Timeout: 50 * time.Millisecond},
			handler: func(rw http.ResponseWriter, req *http.Request) {
				time.Sleep(50 * time.Millisecond)
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantError:          "Client.Timeout exceeded while awaiting headers",
			isWantErrorPartial: true,
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
		if tt.httpClient == nil {
			tt.httpClient = http.DefaultClient
		}
		handler = tt.handler
		var content bytes.Buffer
		err := httpGet(ctx, tt.httpClient, server.URL, &content)
		if tt.wantError != "" {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if tt.isWantErrorPartial {
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("test(%d): missing %q in %q", tt.n, tt.wantError, err.Error())
				}
			} else if got, want := err.Error(), tt.wantError; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := content.String(), tt.wantContent; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}

	if err := httpGet(context.Background(), http.DefaultClient, "::", nil); err == nil {
		t.Fatal("expected error")
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
		{7, &url.Error{Err: errors.New("http: server gave HTTP response to HTTPS client")}, false},
	} {
		if got, want := isRetryableHTTPClientDoError(tt.err), tt.wantIsRetryable; got != want {
			t.Errorf("test(%d): got %v, want %v", tt.n, got, want)
		}
	}
}

func TestParseRawURL(t *testing.T) {
	for _, tt := range []struct {
		n       int
		rawURL  string
		wantURL string
	}{
		{1, "example.com", "https://example.com"},
		{2, "http://example.com", "http://example.com"},
		{3, "https://example.com", "https://example.com"},
		{4, "file:///passwd", "file:///passwd"},
		{5, "scheme://example.com", "scheme://example.com"},
	} {
		u, err := parseRawURL(tt.rawURL)
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		if got, want := u.String(), tt.wantURL; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}

	if _, err := parseRawURL("\n"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAppendURL(t *testing.T) {
	u := &url.URL{Scheme: "https", Host: "example.com"}
	for _, tt := range []struct {
		n       int
		au      *url.URL
		wantURL string
	}{
		{1, appendURL(u, "foobar"), "https://example.com/foobar"},
		{2, appendURL(u, "foo", "bar"), "https://example.com/foo/bar"},
		{3, appendURL(u, "", "foo", "", "bar"), "https://example.com/foo/bar"},
		{4, appendURL(u, "foo/bar"), "https://example.com/foo/bar"},
		{5, appendURL(u, "/foo/bar"), "https://example.com/foo/bar"},
		{6, appendURL(u, "/foo/bar/"), "https://example.com/foo/bar/"},
	} {
		if got, want := tt.au.String(), tt.wantURL; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}
