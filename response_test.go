package goproxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSetResponseCacheControlHeader(t *testing.T) {
	for _, tt := range []struct {
		n                int
		maxAge           int
		wantCacheControl string
	}{
		{1, 60, "public, max-age=60"},
		{2, 0, "public, max-age=0"},
		{3, -1, "must-revalidate, no-cache, no-store"},
		{4, -2, ""},
	} {
		rec := httptest.NewRecorder()
		setResponseCacheControlHeader(rec, tt.maxAge)
		recr := rec.Result()
		if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestResponseString(t *testing.T) {
	for _, tt := range []struct {
		n           int
		method      string
		content     string
		wantContent string
	}{
		{
			n:           1,
			content:     "foobar",
			wantContent: "foobar",
		},
		{
			n:       2,
			method:  http.MethodHead,
			content: "foobar",
		},
	} {
		rec := httptest.NewRecorder()
		responseString(rec, httptest.NewRequest(tt.method, "/", nil), http.StatusOK, 60, tt.content)
		recr := rec.Result()
		if got, want := recr.StatusCode, http.StatusOK; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestResponseNotFound(t *testing.T) {
	for _, tt := range []struct {
		n           int
		msgs        []any
		wantContent string
	}{
		{1, nil, "not found"},
		{2, []any{}, "not found"},
		{3, []any{""}, "not found"},
		{4, []any{"not found"}, "not found"},
		{5, []any{"not found"}, "not found"},
		{6, []any{errNotFound}, "not found"},
		{7, []any{"foobar"}, "not found: foobar"},
		{8, []any{"foo", "bar"}, "not found: foobar"},
		{9, []any{errors.New("foo"), "bar"}, "not found: foobar"},
		{10, []any{"not found: foobar"}, "not found: foobar"},
		{11, []any{"bad request: foobar"}, "not found: foobar"},
		{12, []any{"gone: foobar"}, "not found: foobar"},
	} {
		rec := httptest.NewRecorder()
		responseNotFound(rec, httptest.NewRequest("", "/", nil), 60, tt.msgs...)
		recr := rec.Result()
		if got, want := recr.StatusCode, http.StatusNotFound; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestResponseMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	responseMethodNotAllowed(rec, httptest.NewRequest("", "/", nil), 60)
	recr := rec.Result()
	if got, want := recr.StatusCode, http.StatusMethodNotAllowed; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if b, err := io.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "method not allowed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResponseInternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()
	responseInternalServerError(rec, httptest.NewRequest("", "/", nil))
	recr := rec.Result()
	if got, want := recr.StatusCode, http.StatusInternalServerError; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := recr.Header.Get("Cache-Control"), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if b, err := io.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "internal server error"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

type successResponseBody_LastModified struct {
	io.Reader
	lastModified time.Time
}

func (srb successResponseBody_LastModified) LastModified() time.Time {
	return srb.lastModified
}

type successResponseBody_ModTime struct {
	io.Reader
	modTime time.Time
}

func (srb successResponseBody_ModTime) ModTime() time.Time {
	return srb.modTime
}

type successResponseBody_ETag struct {
	io.Reader
	etag string
}

func (srb successResponseBody_ETag) ETag() string {
	return srb.etag
}

func TestResponseSuccess(t *testing.T) {
	for _, tt := range []struct {
		n                int
		method           string
		content          io.Reader
		wantLastModified string
		wantETag         string
		wantContent      string
	}{
		{
			n:           1,
			content:     strings.NewReader("foobar"),
			wantContent: "foobar",
		},
		{
			n:       2,
			method:  http.MethodHead,
			content: strings.NewReader("foobar"),
		},
		{
			n: 3,
			content: successResponseBody_LastModified{
				Reader:       strings.NewReader("foobar"),
				lastModified: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantLastModified: "Sat, 01 Jan 2000 00:00:00 GMT",
			wantContent:      "foobar",
		},
		{
			n: 4,
			content: successResponseBody_ModTime{
				Reader:  strings.NewReader("foobar"),
				modTime: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantLastModified: "Sat, 01 Jan 2000 00:00:00 GMT",
			wantContent:      "foobar",
		},
		{
			n: 5,
			content: successResponseBody_ETag{
				Reader: strings.NewReader("foobar"),
				etag:   `"foobar"`,
			},
			wantETag:    `"foobar"`,
			wantContent: "foobar",
		},
		{
			n: 6,
			content: struct {
				io.Reader
				successResponseBody_LastModified
				successResponseBody_ModTime
				successResponseBody_ETag
			}{
				strings.NewReader("foobar"),
				successResponseBody_LastModified{lastModified: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
				successResponseBody_ModTime{modTime: time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)},
				successResponseBody_ETag{etag: `"foobar"`},
			},
			wantLastModified: "Sat, 01 Jan 2000 00:00:00 GMT",
			wantETag:         `"foobar"`,
			wantContent:      "foobar",
		},
	} {
		rec := httptest.NewRecorder()
		responseSuccess(rec, httptest.NewRequest(tt.method, "/", nil), tt.content, "text/plain; charset=utf-8", 60)
		recr := rec.Result()
		if got, want := recr.StatusCode, http.StatusOK; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Last-Modified"), tt.wantLastModified; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("ETag"), tt.wantETag; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}

func TestResponseError(t *testing.T) {
	for _, tt := range []struct {
		n                int
		err              error
		cacheSensitive   bool
		wantStatusCode   int
		wantCacheControl string
		wantContent      string
	}{
		{
			n:                1,
			err:              errNotFound,
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "public, max-age=600",
			wantContent:      "not found",
		},
		{
			n:                2,
			err:              errBadUpstream,
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "must-revalidate, no-cache, no-store",
			wantContent:      "not found: bad upstream",
		},
		{
			n:                3,
			err:              errFetchTimedOut,
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "must-revalidate, no-cache, no-store",
			wantContent:      "not found: fetch timed out",
		},
		{
			n:                4,
			err:              notFoundErrorf("cache sensitive"),
			cacheSensitive:   true,
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found: cache sensitive",
		},
		{
			n:                5,
			err:              notFoundErrorf("not found: bad upstream"),
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "must-revalidate, no-cache, no-store",
			wantContent:      "not found: bad upstream",
		},
		{
			n:                6,
			err:              notFoundErrorf("not found: fetch timed out"),
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "must-revalidate, no-cache, no-store",
			wantContent:      "not found: fetch timed out",
		},
		{
			n:              7,
			err:            errors.New("internal server error"),
			wantStatusCode: http.StatusInternalServerError,
			wantContent:    "internal server error",
		},
	} {
		rec := httptest.NewRecorder()
		responseError(rec, httptest.NewRequest("", "/", nil), tt.err, tt.cacheSensitive)
		recr := rec.Result()
		if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
			t.Errorf("test(%d): got %d, want %d", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
		if b, err := io.ReadAll(recr.Body); err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		} else if got, want := string(b), tt.wantContent; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
		}
	}
}
