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
	"net/url"
	"syscall"
	"testing"
	"time"
)

func TestNotFoundError(t *testing.T) {
	for _, tt := range []struct {
		n         int
		err       error
		wantError error
	}{
		{1, notFoundErrorf(""), errors.New("")},
		{2, notFoundErrorf("foobar"), errors.New("foobar")},
		{3, notFoundErrorf("foobar"), errNotFound},
	} {
		if got, want := tt.err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}

	nfe := &notFoundError{err: errors.New("foobar")}
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
	server, setHandler := newHTTPTestServer()
	defer server.Close()

	savedBackoffRand := backoffRand
	backoffRand = rand.New(rand.NewSource(1))
	defer func() { backoffRand = savedBackoffRand }()

	for _, tt := range []struct {
		n             int
		ctxTimeout    time.Duration
		clientTimeout time.Duration
		handler       http.HandlerFunc
		wantContent   string
		wantError     error
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
			wantError: errNotFound,
		},
		{
			n: 3,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantError: errBadUpstream,
		},
		{
			n: 4,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusGatewayTimeout)
				fmt.Fprint(rw, "gateway timeout")
			},
			wantError: errFetchTimedOut,
		},
		{
			n: 5,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotImplemented)
				fmt.Fprint(rw, "not implemented")
			},
			wantError: fmt.Errorf("GET %s: 501 Not Implemented: not implemented", server.URL),
		},
		{
			n:          6,
			ctxTimeout: 450 * time.Millisecond,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantError: errBadUpstream,
		},
		{
			n:          7,
			ctxTimeout: -1,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantError: context.Canceled,
		},
		{
			n: 8,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				rw.(http.Flusher).Flush()
				server.CloseClientConnections()
			},
			wantError: io.ErrUnexpectedEOF,
		},
		{
			n:             9,
			clientTimeout: 50 * time.Millisecond,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				time.Sleep(50 * time.Millisecond)
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantError: fmt.Errorf("Get %q: context deadline exceeded (Client.Timeout exceeded while awaiting headers)", server.URL),
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
		client := http.DefaultClient
		if tt.clientTimeout > 0 {
			client2 := *client
			client2.Timeout = tt.clientTimeout
			client = &client2
		}
		setHandler(tt.handler)
		var content bytes.Buffer
		err := httpGet(ctx, client, server.URL, &content)
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
