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
	"os"
	"reflect"
	"strconv"
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
		{3, -1, "no-store"},
		{4, -2, ""},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			rec := httptest.NewRecorder()
			setResponseCacheControlHeader(rec, tt.maxAge)
			recr := rec.Result()
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
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
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			rec := httptest.NewRecorder()
			responseString(rec, httptest.NewRequest(tt.method, "/", nil), http.StatusOK, 60, tt.content)
			recr := rec.Result()
			if got, want := recr.StatusCode, http.StatusOK; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
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
		{5, []any{fs.ErrNotExist}, "not found: file does not exist"},
		{6, []any{"foobar"}, "not found: foobar"},
		{7, []any{"foo", "bar"}, "not found: foobar"},
		{8, []any{errors.New("foo"), "bar"}, "not found: foobar"},
		{9, []any{"not found: foobar"}, "not found: foobar"},
		{10, []any{"bad request: foobar"}, "not found: foobar"},
		{11, []any{"gone: foobar"}, "not found: foobar"},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			rec := httptest.NewRecorder()
			responseNotFound(rec, httptest.NewRequest("", "/", nil), 60, tt.msgs...)
			recr := rec.Result()
			if got, want := recr.StatusCode, http.StatusNotFound; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestResponseMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	responseMethodNotAllowed(rec, httptest.NewRequest("", "/", nil), 60)
	recr := rec.Result()
	if got, want := recr.StatusCode, http.StatusMethodNotAllowed; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if got, want := recr.Header.Get("Allow"), "GET, HEAD"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if b, err := io.ReadAll(recr.Body); err != nil {
		t.Errorf("unexpected error %v", err)
	} else if got, want := string(b), "method not allowed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResponseInternalServerError(t *testing.T) {
	for _, tt := range []struct {
		name         string
		cacheControl string
	}{
		{"NoCacheControl", ""},
		{"PublicCacheControl", "public, max-age=604800"},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(tt.name+"/"+method, func(t *testing.T) {
				rec := httptest.NewRecorder()
				if tt.cacheControl != "" {
					rec.Header().Set("Cache-Control", tt.cacheControl)
				}
				responseInternalServerError(rec, httptest.NewRequest(method, "/", nil))
				recr := rec.Result()
				if got, want := recr.StatusCode, http.StatusInternalServerError; got != want {
					t.Errorf("got %d, want %d", got, want)
				}
				if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := recr.Header.Get("Cache-Control"), "no-store"; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				wantContent := "internal server error"
				if method == http.MethodHead {
					wantContent = ""
				}
				if b, err := io.ReadAll(recr.Body); err != nil {
					t.Errorf("unexpected error %v", err)
				} else if got, want := string(b), wantContent; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			})
		}
	}
}

type successResponseBody_Size struct {
	io.Reader
	size int64
}

func (srb successResponseBody_Size) Size() int64 { return srb.size }

type successResponseBody_LastModified struct {
	io.Reader
	lastModified time.Time
}

func (srb successResponseBody_LastModified) LastModified() time.Time { return srb.lastModified }

type successResponseBody_ModTime struct {
	io.Reader
	modTime time.Time
}

func (srb successResponseBody_ModTime) ModTime() time.Time { return srb.modTime }

type successResponseBody_ETag struct {
	io.Reader
	etag string
}

func (srb successResponseBody_ETag) ETag() string { return srb.etag }

func TestResponseSuccess(t *testing.T) {
	t.Run("CacheControl", func(t *testing.T) {
		for _, keepHeaders := range []string{"0", "1"} {
			t.Run("KeepHeaders"+keepHeaders, func(t *testing.T) {
				t.Setenv("GODEBUG", "httpservecontentkeepheaders="+keepHeaders)
				for _, tt := range []struct {
					name             string
					header           http.Header
					seekError        bool
					wantStatusCode   int
					wantCacheControl string
					wantContentRange string
					wantContent      string
				}{
					{
						name:             "FullContent",
						wantStatusCode:   http.StatusOK,
						wantCacheControl: "public, max-age=604800",
						wantContent:      "foobar",
					},
					{
						name:             "PartialContent",
						header:           http.Header{"Range": {"bytes=0-2"}},
						wantStatusCode:   http.StatusPartialContent,
						wantCacheControl: "public, max-age=604800",
						wantContentRange: "bytes 0-2/6",
						wantContent:      "foo",
					},
					{
						name:             "NotModifiedETag",
						header:           http.Header{"If-None-Match": {`"foobar"`}},
						wantStatusCode:   http.StatusNotModified,
						wantCacheControl: "public, max-age=604800",
					},
					{
						name:             "NotModifiedDate",
						header:           http.Header{"If-Modified-Since": {"Sat, 01 Jan 2000 00:00:00 GMT"}},
						wantStatusCode:   http.StatusNotModified,
						wantCacheControl: "public, max-age=604800",
					},
					{
						name:             "FailedIfMatch",
						header:           http.Header{"If-Match": {`"other"`}},
						wantStatusCode:   http.StatusPreconditionFailed,
						wantCacheControl: "no-store",
					},
					{
						name:             "FailedIfUnmodifiedSince",
						header:           http.Header{"If-Unmodified-Since": {"Fri, 31 Dec 1999 00:00:00 GMT"}},
						wantStatusCode:   http.StatusPreconditionFailed,
						wantCacheControl: "no-store",
					},
					{
						name:             "InvalidRange",
						header:           http.Header{"Range": {"bytes=invalid"}},
						wantStatusCode:   http.StatusRequestedRangeNotSatisfiable,
						wantCacheControl: "no-store",
						wantContent:      "invalid range\n",
					},
					{
						name:             "UnsatisfiableRange",
						header:           http.Header{"Range": {"bytes=6-"}},
						wantStatusCode:   http.StatusRequestedRangeNotSatisfiable,
						wantCacheControl: "no-store",
						wantContentRange: "bytes */6",
						wantContent:      "invalid range: failed to overlap\n",
					},
					{
						name:             "SeekError",
						seekError:        true,
						wantStatusCode:   http.StatusInternalServerError,
						wantCacheControl: "no-store",
						wantContent:      "seeker can't seek\n",
					},
				} {
					t.Run(tt.name, func(t *testing.T) {
						server := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
							content := struct {
								io.ReadSeeker
								successResponseBody_ModTime
								successResponseBody_ETag
							}{
								strings.NewReader("foobar"),
								successResponseBody_ModTime{modTime: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
								successResponseBody_ETag{etag: `"foobar"`},
							}
							if tt.seekError {
								content.ReadSeeker = &testReadSeeker{
									ReadSeeker: content.ReadSeeker,
									seek: func(rs io.ReadSeeker, offset int64, whence int) (int64, error) {
										return 0, errors.New("cannot seek")
									},
								}
							}
							responseSuccess(rw, req, content, "text/plain; charset=utf-8", 604800)
						}))
						for _, method := range []string{http.MethodGet, http.MethodHead} {
							t.Run(method, func(t *testing.T) {
								req, err := http.NewRequest(method, server.URL, nil)
								if err != nil {
									t.Fatal(err)
								}
								req.Header = tt.header.Clone()
								resp, err := server.Client().Do(req)
								if err != nil {
									t.Fatal(err)
								}
								defer resp.Body.Close()
								wantStatusCode, wantCacheControl, wantContentRange := tt.wantStatusCode, tt.wantCacheControl, tt.wantContentRange
								if method == http.MethodHead && (wantStatusCode == http.StatusPartialContent || wantStatusCode == http.StatusRequestedRangeNotSatisfiable) {
									wantStatusCode, wantCacheControl, wantContentRange = http.StatusOK, "public, max-age=604800", ""
								}
								if got, want := resp.StatusCode, wantStatusCode; got != want {
									t.Errorf("got status %d, want %d", got, want)
								}
								if got, want := resp.Header.Get("Cache-Control"), wantCacheControl; got != want {
									t.Errorf("got cache control %q, want %q", got, want)
								}
								if got, want := resp.Header.Get("Content-Range"), wantContentRange; got != want {
									t.Errorf("got content range %q, want %q", got, want)
								}
								if resp.StatusCode == http.StatusNotModified {
									if got, want := resp.Header.Get("ETag"), `"foobar"`; got != want {
										t.Errorf("got ETag %q, want %q", got, want)
									}
								}
								wantContent := tt.wantContent
								if method == http.MethodHead {
									wantContent = ""
								}
								if b, err := io.ReadAll(resp.Body); err != nil {
									t.Errorf("unexpected error %v", err)
								} else if got, want := string(b), wantContent; got != want {
									t.Errorf("got content %q, want %q", got, want)
								}
							})
						}
					})
				}
			})
		}
	})

	t.Run("HeadRange", func(t *testing.T) {
		for _, tt := range []struct {
			name           string
			header         http.Header
			wantStatusCode int
		}{
			{"SingleRange", http.Header{"Range": {"bytes=0-2"}}, http.StatusOK},
			{"MultipleRanges", http.Header{"Range": {"bytes=0-0,2-2"}}, http.StatusOK},
			{"InvalidRange", http.Header{"Range": {"bytes=invalid"}}, http.StatusOK},
			{"UnsatisfiableRange", http.Header{"Range": {"bytes=6-"}}, http.StatusOK},
			{"MatchingIfRangeETag", http.Header{"Range": {"bytes=0-2"}, "If-Range": {`"foobar"`}}, http.StatusOK},
			{"MismatchedIfRangeETag", http.Header{"Range": {"bytes=0-2"}, "If-Range": {`"other"`}}, http.StatusOK},
			{"MatchingIfRangeDate", http.Header{"Range": {"bytes=0-2"}, "If-Range": {"Sat, 01 Jan 2000 00:00:00 GMT"}}, http.StatusOK},
			{"MismatchedIfRangeDate", http.Header{"Range": {"bytes=0-2"}, "If-Range": {"Fri, 31 Dec 1999 00:00:00 GMT"}}, http.StatusOK},
			{"MatchingIfNoneMatch", http.Header{"Range": {"bytes=invalid"}, "If-None-Match": {`"foobar"`}}, http.StatusNotModified},
			{"MismatchedIfNoneMatch", http.Header{"Range": {"bytes=0-2"}, "If-None-Match": {`"other"`}}, http.StatusOK},
			{"NotModifiedSince", http.Header{"Range": {"bytes=invalid"}, "If-Modified-Since": {"Sat, 01 Jan 2000 00:00:00 GMT"}}, http.StatusNotModified},
			{"ModifiedSince", http.Header{"Range": {"bytes=0-2"}, "If-Modified-Since": {"Fri, 31 Dec 1999 00:00:00 GMT"}}, http.StatusOK},
			{"MatchingIfMatch", http.Header{"Range": {"bytes=0-2"}, "If-Match": {`"foobar"`}}, http.StatusOK},
			{"MismatchedIfMatch", http.Header{"Range": {"bytes=invalid"}, "If-Match": {`"other"`}}, http.StatusPreconditionFailed},
			{"UnmodifiedSince", http.Header{"Range": {"bytes=0-2"}, "If-Unmodified-Since": {"Sat, 01 Jan 2000 00:00:00 GMT"}}, http.StatusOK},
			{"NotUnmodifiedSince", http.Header{"Range": {"bytes=invalid"}, "If-Unmodified-Since": {"Fri, 31 Dec 1999 00:00:00 GMT"}}, http.StatusPreconditionFailed},
		} {
			t.Run(tt.name, func(t *testing.T) {
				content := struct {
					io.ReadSeeker
					successResponseBody_ModTime
					successResponseBody_ETag
				}{
					strings.NewReader("foobar"),
					successResponseBody_ModTime{modTime: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
					successResponseBody_ETag{etag: `"foobar"`},
				}
				req := httptest.NewRequest(http.MethodHead, "/", nil)
				req.Header = tt.header.Clone()
				rec := httptest.NewRecorder()
				responseSuccess(rec, req, content, "text/plain; charset=utf-8", 604800)
				resp := rec.Result()
				if got, want := resp.StatusCode, tt.wantStatusCode; got != want {
					t.Errorf("got status %d, want %d", got, want)
				}
				if got, want := req.Header, tt.header; !reflect.DeepEqual(got, want) {
					t.Errorf("got request headers %v, want %v", got, want)
				}
				if got, want := req.Method, http.MethodHead; got != want {
					t.Errorf("got method %q, want %q", got, want)
				}
				if got, want := rec.Body.String(), ""; got != want {
					t.Errorf("got content %q, want %q", got, want)
				}
				if got, want := resp.Header.Get("Content-Range"), ""; got != want {
					t.Errorf("got content range %q, want %q", got, want)
				}
				if got, want := resp.Header.Get("ETag"), `"foobar"`; got != want {
					t.Errorf("got ETag %q, want %q", got, want)
				}
				wantCacheControl := "public, max-age=604800"
				if tt.wantStatusCode == http.StatusPreconditionFailed {
					wantCacheControl = "no-store"
				}
				if got, want := resp.Header.Get("Cache-Control"), wantCacheControl; got != want {
					t.Errorf("got cache control %q, want %q", got, want)
				}
				if tt.wantStatusCode == http.StatusOK {
					for key, want := range map[string]string{
						"Content-Length": "6",
						"Content-Type":   "text/plain; charset=utf-8",
						"Last-Modified":  "Sat, 01 Jan 2000 00:00:00 GMT",
						"Accept-Ranges":  "bytes",
					} {
						if got := resp.Header.Get(key); got != want {
							t.Errorf("got %s %q, want %q", key, got, want)
						}
					}
				}
			})
		}
	})

	t.Run("ReaderFrom", func(t *testing.T) {
		rec := httptest.NewRecorder()
		var buf bytes.Buffer
		rw := struct {
			http.ResponseWriter
			io.ReaderFrom
		}{rec, &buf}
		responseSuccess(rw, httptest.NewRequest(http.MethodGet, "/", nil), strings.NewReader("foobar"), "text/plain; charset=utf-8", 60)
		if got, want := buf.String(), "foobar"; got != want {
			t.Errorf("got content %q, want %q", got, want)
		}
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Errorf("got status %d, want %d", got, want)
		}
		if got, want := rec.Result().Header.Get("Cache-Control"), "public, max-age=60"; got != want {
			t.Errorf("got cache control %q, want %q", got, want)
		}
	})

	for _, tt := range []struct {
		n                 int
		method            string
		content           io.Reader
		wantContentLength int64
		wantLastModified  string
		wantETag          string
		wantContent       string
	}{
		{
			n:                 1,
			content:           strings.NewReader("foobar"),
			wantContentLength: 6,
			wantContent:       "foobar",
		},
		{
			n:                 2,
			method:            http.MethodHead,
			wantContentLength: 6,
			content:           strings.NewReader("foobar"),
		},
		{
			n: 3,
			content: successResponseBody_Size{
				Reader: strings.NewReader("foobar"),
				size:   6,
			},
			wantContentLength: 6,
			wantContent:       "foobar",
		},
		{
			n: 4,
			content: successResponseBody_LastModified{
				Reader:       strings.NewReader("foobar"),
				lastModified: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantContentLength: -1,
			wantLastModified:  "Sat, 01 Jan 2000 00:00:00 GMT",
			wantContent:       "foobar",
		},
		{
			n: 5,
			content: successResponseBody_ModTime{
				Reader:  strings.NewReader("foobar"),
				modTime: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantContentLength: -1,
			wantLastModified:  "Sat, 01 Jan 2000 00:00:00 GMT",
			wantContent:       "foobar",
		},
		{
			n: 6,
			content: successResponseBody_ETag{
				Reader: strings.NewReader("foobar"),
				etag:   `"foobar"`,
			},
			wantContentLength: -1,
			wantETag:          `"foobar"`,
			wantContent:       "foobar",
		},
		{
			n: 7,
			content: struct {
				io.Reader
				successResponseBody_Size
				successResponseBody_LastModified
				successResponseBody_ModTime
				successResponseBody_ETag
			}{
				strings.NewReader("foobar"),
				successResponseBody_Size{size: 6},
				successResponseBody_LastModified{lastModified: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
				successResponseBody_ModTime{modTime: time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)},
				successResponseBody_ETag{etag: `"foobar"`},
			},
			wantContentLength: 6,
			wantLastModified:  "Sat, 01 Jan 2000 00:00:00 GMT",
			wantETag:          `"foobar"`,
			wantContent:       "foobar",
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			rec := httptest.NewRecorder()
			responseSuccess(rec, httptest.NewRequest(tt.method, "/", nil), tt.content, "text/plain; charset=utf-8", 60)
			recr := rec.Result()
			if got, want := recr.StatusCode, http.StatusOK; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.ContentLength, tt.wantContentLength; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Last-Modified"), tt.wantLastModified; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("ETag"), tt.wantETag; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

type testTimeoutError struct {
	error
	timeout bool
}

func (e testTimeoutError) Timeout() bool { return e.timeout }

func TestResponseError(t *testing.T) {
	t.Run("Timeout", func(t *testing.T) {
		timeoutErr := testTimeoutError{errors.New("operation timed out"), true}
		nonTimeoutErr := testTimeoutError{errors.New("operation failed"), false}
		for _, tt := range []struct {
			name           string
			err            error
			wantStatusCode int
			wantContent    string
		}{
			{"Direct", timeoutErr, http.StatusNotFound, "not found: fetch timed out"},
			{"Wrapped", fmt.Errorf("fetch failed: %w", timeoutErr), http.StatusNotFound, "not found: fetch timed out"},
			{"Nested", fmt.Errorf("fetch failed: %w", fmt.Errorf("read failed: %w", timeoutErr)), http.StatusNotFound, "not found: fetch timed out"},
			{"Joined", errors.Join(errors.New("operation failed"), timeoutErr), http.StatusNotFound, "not found: fetch timed out"},
			{"IODeadline", os.ErrDeadlineExceeded, http.StatusNotFound, "not found: fetch timed out"},
			{"WrappedIODeadline", fmt.Errorf("read failed: %w", os.ErrDeadlineExceeded), http.StatusNotFound, "not found: fetch timed out"},
			{"ContextDeadline", context.DeadlineExceeded, http.StatusNotFound, "not found: fetch timed out"},
			{"WrappedContextDeadline", fmt.Errorf("fetch failed: %w", context.DeadlineExceeded), http.StatusNotFound, "not found: fetch timed out"},
			{"WrappedFetchTimeout", fmt.Errorf("fetch failed: %w", errFetchTimedOut), http.StatusNotFound, "not found: fetch timed out"},
			{"BadUpstream", errors.Join(errBadUpstream, timeoutErr), http.StatusNotFound, "not found: bad upstream"},
			{"NonTimeout", nonTimeoutErr, http.StatusInternalServerError, "internal server error"},
			{"WrappedNonTimeout", fmt.Errorf("fetch failed: %w", nonTimeoutErr), http.StatusInternalServerError, "internal server error"},
			{"ContextCanceled", context.Canceled, http.StatusInternalServerError, "internal server error"},
			{"WrappedContextCanceled", fmt.Errorf("fetch failed: %w", context.Canceled), http.StatusInternalServerError, "internal server error"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					t.Run(method, func(t *testing.T) {
						for _, cacheSensitive := range []bool{false, true} {
							rec := httptest.NewRecorder()
							responseError(rec, httptest.NewRequest(method, "/", nil), tt.err, cacheSensitive)
							resp := rec.Result()
							if got, want := resp.StatusCode, tt.wantStatusCode; got != want {
								t.Errorf("cache sensitive %t: got status %d, want %d", cacheSensitive, got, want)
							}
							if got, want := resp.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
								t.Errorf("cache sensitive %t: got content type %q, want %q", cacheSensitive, got, want)
							}
							if got, want := resp.Header.Get("Cache-Control"), "no-store"; got != want {
								t.Errorf("cache sensitive %t: got cache control %q, want %q", cacheSensitive, got, want)
							}
							wantContent := tt.wantContent
							if method == http.MethodHead {
								wantContent = ""
							}
							if got, want := rec.Body.String(), wantContent; got != want {
								t.Errorf("cache sensitive %t: got content %q, want %q", cacheSensitive, got, want)
							}
						}
					})
				}
			})
		}
	})

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
			err:              fs.ErrNotExist,
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "public, max-age=600",
			wantContent:      "not found",
		},
		{
			n:                2,
			err:              errBadUpstream,
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "no-store",
			wantContent:      "not found: bad upstream",
		},
		{
			n:                3,
			err:              errFetchTimedOut,
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "no-store",
			wantContent:      "not found: fetch timed out",
		},
		{
			n:                4,
			err:              notExistErrorf("cache sensitive"),
			cacheSensitive:   true,
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "public, max-age=60",
			wantContent:      "not found: cache sensitive",
		},
		{
			n:                5,
			err:              notExistErrorf("not found: bad upstream"),
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "no-store",
			wantContent:      "not found: bad upstream",
		},
		{
			n:                6,
			err:              notExistErrorf("not found: fetch timed out"),
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "no-store",
			wantContent:      "not found: fetch timed out",
		},
		{
			n:                7,
			err:              errors.New("internal server error"),
			wantStatusCode:   http.StatusInternalServerError,
			wantCacheControl: "no-store",
			wantContent:      "internal server error",
		},
		{
			n:                8,
			err:              notExistErrorf("%w", testTimeoutError{errors.New("operation timed out"), true}),
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "public, max-age=600",
			wantContent:      "not found: operation timed out",
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			rec := httptest.NewRecorder()
			responseError(rec, httptest.NewRequest("", "/", nil), tt.err, tt.cacheSensitive)
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := recr.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if b, err := io.ReadAll(recr.Body); err != nil {
				t.Errorf("unexpected error %v", err)
			} else if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}
