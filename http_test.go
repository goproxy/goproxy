package goproxy

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestNotExistError(t *testing.T) {
	for _, tt := range []struct {
		n       int
		err     error
		wantErr error
	}{
		{1, notExistErrorf(""), errors.New("")},
		{2, notExistErrorf("foobar"), errors.New("foobar")},
		{3, notExistErrorf("foobar"), fs.ErrNotExist},
	} {
		if got, want := tt.err, tt.wantErr; !compareErrors(got, want) {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}

	e := &notExistError{err: errors.New("foobar")}
	for _, tt := range []struct {
		n      int
		err    error
		wantIs bool
	}{
		{1, fs.ErrNotExist, true},
		{2, io.EOF, false},
	} {
		if got, want := e.Is(tt.err), tt.wantIs; got != want {
			t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
		}
		if got, want := errors.Is(e, tt.err), tt.wantIs; got != want {
			t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
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
		wantErr       error
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
			n: 3,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantErr: errBadUpstream,
		},
		{
			n: 4,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusGatewayTimeout)
				fmt.Fprint(rw, "gateway timeout")
			},
			wantErr: errFetchTimedOut,
		},
		{
			n: 5,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotImplemented)
				fmt.Fprint(rw, "not implemented")
			},
			wantErr: fmt.Errorf("GET %s: 501 Not Implemented: not implemented", server.URL),
		},
		{
			n:          6,
			ctxTimeout: 450 * time.Millisecond,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantErr: errBadUpstream,
		},
		{
			n:          7,
			ctxTimeout: -1,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantErr: context.Canceled,
		},
		{
			n: 8,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
				rw.(http.Flusher).Flush()
				server.CloseClientConnections()
			},
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			n:             9,
			clientTimeout: 50 * time.Millisecond,
			handler: func(rw http.ResponseWriter, req *http.Request) {
				time.Sleep(50 * time.Millisecond)
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(rw, "internal server error")
			},
			wantErr: fmt.Errorf("Get %q: context deadline exceeded (Client.Timeout exceeded while awaiting headers)", server.URL),
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
		if tt.wantErr != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			} else if got, want := err, tt.wantErr; !compareErrors(got, want) {
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

func TestHTTPGetTemp(t *testing.T) {
	server, setHandler := newHTTPTestServer()
	defer server.Close()
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
		setHandler(tt.handler)
		if tt.tempDir == "" {
			tt.tempDir = t.TempDir()
		}
		tempFile, err := httpGetTemp(context.Background(), http.DefaultClient, server.URL, tt.tempDir)
		if tt.wantErr != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			} else if got, want := err, tt.wantErr; !compareErrors(got, want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if _, err := os.Stat(tempFile); err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			} else if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if b, err := os.ReadFile(tempFile); err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
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
			t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
		}
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

func TestBackoffSleep(t *testing.T) {
	for _, tt := range []struct {
		n       int
		base    time.Duration
		cap     time.Duration
		attempt int
	}{
		{1, 100 * time.Millisecond, time.Second, 0},
		{2, time.Minute, time.Hour, 100},
	} {
		if got, want := backoffSleep(tt.base, tt.cap, tt.attempt) <= tt.cap, true; got != want {
			t.Errorf("test(%d): got %t, want %t", tt.n, got, want)
		}
	}
}
