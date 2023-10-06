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
	nfes := "something not found"
	nfe := notFoundError(nfes)
	if got, want := nfe.Error(), nfes; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := nfe.Is(errNotFound), true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := nfe.Is(io.EOF), false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := errors.Is(nfe, errNotFound), true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := errors.Is(nfe, io.EOF), false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestHTTPGet(t *testing.T) {
	savedExponentialBackoffRand := exponentialBackoffRand
	exponentialBackoffRand = rand.New(rand.NewSource(1))
	defer func() { exponentialBackoffRand = savedExponentialBackoffRand }()

	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		fmt.Fprint(rw, "foobar")
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		handlerFunc(rw, req)
	}))
	defer server.Close()
	var buf bytes.Buffer
	if err := httpGet(
		context.Background(),
		http.DefaultClient,
		server.URL,
		&buf,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := buf.String(), "foobar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, "not found")
	}
	buf.Reset()
	if err := httpGet(
		context.Background(),
		http.DefaultClient,
		server.URL,
		&buf,
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := buf.Len(), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(rw, "internal server error")
	}
	buf.Reset()
	if err := httpGet(
		context.Background(),
		http.DefaultClient,
		server.URL,
		&buf,
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "bad upstream"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := buf.Len(), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusGatewayTimeout)
		fmt.Fprint(rw, "gateway timeout")
	}
	buf.Reset()
	if err := httpGet(
		context.Background(),
		http.DefaultClient,
		server.URL,
		&buf,
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "fetch timed out"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := buf.Len(), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotImplemented)
		fmt.Fprint(rw, "not implemented")
	}
	buf.Reset()
	if err := httpGet(
		context.Background(),
		http.DefaultClient,
		server.URL,
		&buf,
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), fmt.Sprintf(
		"GET %s: 501 Not Implemented: not implemented",
		server.URL,
	); got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := buf.Len(), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		450*time.Millisecond,
	)
	defer cancel()
	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(rw, "internal server error")
	}
	buf.Reset()
	if err := httpGet(
		ctx,
		http.DefaultClient,
		server.URL,
		&buf,
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "bad upstream"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := buf.Len(), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(rw, "internal server error")
	}
	buf.Reset()
	if err := httpGet(
		ctx,
		http.DefaultClient,
		server.URL,
		&buf,
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := strings.Contains(
		err.Error(),
		context.Canceled.Error(),
	), true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := buf.Len(), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	if err := httpGet(
		context.Background(),
		http.DefaultClient,
		"::",
		nil,
	); err == nil {
		t.Fatal("expected error")
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
		rw.(http.Flusher).Flush()
		server.CloseClientConnections()
	}
	buf.Reset()
	if err := httpGet(
		context.Background(),
		http.DefaultClient,
		server.URL,
		&buf,
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err, io.ErrUnexpectedEOF; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := buf.Len(), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		time.Sleep(50 * time.Millisecond)
		rw.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(rw, "internal server error")
	}
	buf.Reset()
	if err := httpGet(
		context.Background(),
		&http.Client{Timeout: 50 * time.Millisecond},
		server.URL,
		&buf,
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := strings.Contains(
		err.Error(),
		"Client.Timeout exceeded while awaiting headers",
	), true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := buf.Len(), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestIsRetryableHTTPClientDoError(t *testing.T) {
	got := isRetryableHTTPClientDoError(syscall.ECONNRESET)
	want := true
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	got = isRetryableHTTPClientDoError(errors.New("oops"))
	want = true
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	got = isRetryableHTTPClientDoError(context.Canceled)
	want = false
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	got = isRetryableHTTPClientDoError(context.DeadlineExceeded)
	want = false
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	got = isRetryableHTTPClientDoError(&url.Error{
		Err: errors.New("oops"),
	})
	want = true
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	got = isRetryableHTTPClientDoError(&url.Error{
		Err: x509.UnknownAuthorityError{},
	})
	want = false
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	got = isRetryableHTTPClientDoError(&url.Error{
		Err: errors.New(
			"http: server gave HTTP response to HTTPS client",
		),
	})
	want = false
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseRawURL(t *testing.T) {
	if u, err := parseRawURL("example.com"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if u == nil {
		t.Fatal("unexpected nil")
	} else if got, want := u.String(), "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if u, err := parseRawURL("http://example.com"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if u == nil {
		t.Fatal("unexpected nil")
	} else if got, want := u.String(), "http://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if u, err := parseRawURL("https://example.com"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if u == nil {
		t.Fatal("unexpected nil")
	} else if got, want := u.String(), "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if u, err := parseRawURL("file:///passwd"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if u == nil {
		t.Fatal("unexpected nil")
	} else if got, want := u.String(), "file:///passwd"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if u, err := parseRawURL("scheme://example.com"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if u == nil {
		t.Fatal("unexpected nil")
	} else if got, want := u.String(), "scheme://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if u, err := parseRawURL("\n"); err == nil {
		t.Fatal("expected error")
	} else if u != nil {
		t.Errorf("got %v, want nil", u)
	}
}

func TestAppendURL(t *testing.T) {
	us := appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"foobar",
	).String()
	if want := "https://example.com/foobar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"foo",
		"bar",
	).String()
	if want := "https://example.com/foo/bar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"",
		"foo",
		"",
		"bar",
	).String()
	if want := "https://example.com/foo/bar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"foo/bar",
	).String()
	if want := "https://example.com/foo/bar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"/foo/bar",
	).String()
	if want := "https://example.com/foo/bar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"/foo/bar/",
	).String()
	if want := "https://example.com/foo/bar/"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}
}
