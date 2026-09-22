package goproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
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

type testResponseWriter struct {
	http.ResponseWriter
	write func([]byte) (int, error)
}

func (rw testResponseWriter) Write(p []byte) (int, error) { return rw.write(p) }

func TestResponseSuccess(t *testing.T) {
	t.Run("CopyError", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method, func(t *testing.T) {
				readErr := errors.New("read failed")
				for _, tt := range []struct {
					name        string
					content     io.Reader
					write       func([]byte) (int, error)
					readerFrom  bool
					wantContent string
				}{
					{name: "ReadError", content: iotest.ErrReader(readErr)},
					{name: "PartialReadError", content: io.MultiReader(strings.NewReader("foo"), iotest.ErrReader(readErr)), wantContent: "foo"},
					{name: "DataAndReadError", content: iotest.DataErrReader(io.MultiReader(strings.NewReader("foo"), iotest.ErrReader(readErr))), wantContent: "foo"},
					{name: "WriteError", content: strings.NewReader("foobar"), write: func([]byte) (int, error) { return 0, errors.New("write failed") }},
					{name: "ShortWrite", content: strings.NewReader("foobar"), write: func([]byte) (int, error) { return 1, nil }},
					{name: "ReaderFromError", content: io.MultiReader(strings.NewReader("foo"), iotest.ErrReader(readErr)), readerFrom: true, wantContent: "foo"},
				} {
					t.Run(tt.name, func(t *testing.T) {
						rec := httptest.NewRecorder()
						var rw http.ResponseWriter = rec
						if tt.write != nil {
							rw = testResponseWriter{rw, tt.write}
						}
						if tt.readerFrom {
							rw = serveContentResponseWriter{rw}
						}
						var wantPanic any = http.ErrAbortHandler
						wantContent := tt.wantContent
						if method == http.MethodHead {
							wantPanic, wantContent = nil, ""
						}
						func() {
							defer func() {
								if got := recover(); got != wantPanic {
									t.Errorf("got panic %v, want %v", got, wantPanic)
								}
							}()
							responseSuccess(rw, httptest.NewRequest(method, "/", nil), struct{ io.Reader }{tt.content}, "text/plain; charset=utf-8", 60)
						}()
						if got, want := rec.Body.String(), wantContent; got != want {
							t.Errorf("got content %q, want %q", got, want)
						}
					})
				}
			})
		}
	})

	t.Run("Streaming", func(t *testing.T) {
		for _, protoMajor := range []int{1, 2} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for _, tt := range []struct {
					name    string
					content string
					readErr error
					flush   bool
				}{
					{"FullContent", "foobar", nil, false},
					{"FlushedFullContent", "foobar", nil, true},
					{"EmptyContent", "", nil, false},
					{"ReadError", "", errors.New("read failed"), false},
					{"PartialReadError", "foo", errors.New("read failed"), false},
					{"FlushedReadError", "foo", errors.New("read failed"), true},
				} {
					t.Run("HTTP"+strconv.Itoa(protoMajor)+"/"+method+"/"+tt.name, func(t *testing.T) {
						server := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
							var content io.Reader = strings.NewReader(tt.content)
							if tt.readErr != nil {
								content = io.MultiReader(content, iotest.ErrReader(tt.readErr))
							}
							var writer http.ResponseWriter = rw
							if tt.flush {
								writer = testResponseWriter{rw, func(p []byte) (int, error) {
									n, err := rw.Write(p)
									rw.(http.Flusher).Flush()
									return n, err
								}}
							}
							responseSuccess(writer, req, struct{ io.Reader }{content}, "text/plain; charset=utf-8", 60)
						}))
						server.EnableHTTP2 = protoMajor == 2
						server.StartTLS()
						t.Cleanup(server.Close)
						req, err := http.NewRequest(method, server.URL, nil)
						if err != nil {
							t.Fatal(err)
						}
						wantError := method == http.MethodGet && tt.readErr != nil
						resp, err := server.Client().Do(req)
						if err != nil {
							if !wantError {
								t.Fatal(err)
							}
							return
						}
						defer resp.Body.Close()
						if got, want := resp.ProtoMajor, protoMajor; got != want {
							t.Errorf("got protocol major %d, want %d", got, want)
						}
						if got, want := resp.StatusCode, http.StatusOK; got != want {
							t.Errorf("got status %d, want %d", got, want)
						}
						if got, want := resp.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
							t.Errorf("got content type %q, want %q", got, want)
						}
						if got, want := resp.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
							t.Errorf("got cache control %q, want %q", got, want)
						}
						content, err := io.ReadAll(resp.Body)
						if got, want := err != nil, wantError; got != want {
							t.Errorf("got read error %v, want error %t", err, want)
						}
						if !wantError {
							wantContent := tt.content
							if method == http.MethodHead {
								wantContent = ""
							}
							if got, want := string(content), wantContent; got != want {
								t.Errorf("got content %q, want %q", got, want)
							}
						}
					})
				}
			}
		}
	})

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

func TestResponseError(t *testing.T) {
	t.Run("Uncacheable", func(t *testing.T) {
		for _, tt := range []struct {
			name           string
			err            error
			wantStatusCode int
			wantContent    string
		}{
			{"Direct", &uncacheableError{err: notExistErrorf("module unavailable")}, http.StatusNotFound, "not found: module unavailable"},
			{"Wrapped", fmt.Errorf("fetch failed: %w", &uncacheableError{err: notExistErrorf("module unavailable")}), http.StatusNotFound, "not found: fetch failed: module unavailable"},
			{"Nested", notExistErrorf("fetch failed: %w", &uncacheableError{err: errors.New("module unavailable")}), http.StatusNotFound, "not found: fetch failed: module unavailable"},
			{"Joined", errors.Join(notExistErrorf("module unavailable"), &uncacheableError{err: errors.New("fetch failed")}), http.StatusNotFound, "not found: module unavailable\nfetch failed"},
			{"Timeout", &uncacheableError{err: &notExistError{err: testTimeoutError{errors.New("operation timed out"), true}}}, http.StatusNotFound, "not found: operation timed out"},
			{"Internal", &uncacheableError{err: errors.New("operation failed")}, http.StatusInternalServerError, "internal server error"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					t.Run(method, func(t *testing.T) {
						for _, cacheSensitive := range []bool{false, true} {
							rec := httptest.NewRecorder()
							responseError(rec, httptest.NewRequest(method, "/", nil), tt.err, cacheSensitive)
							if got, want := rec.Code, tt.wantStatusCode; got != want {
								t.Errorf("cache sensitive %t: got status %d, want %d", cacheSensitive, got, want)
							}
							if got, want := rec.Header().Get("Cache-Control"), "no-store"; got != want {
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

	t.Run("Timeout", func(t *testing.T) {
		timeoutErr := testTimeoutError{errors.New("operation timed out"), true}
		nonTimeoutErr := testTimeoutError{errors.New("operation failed"), false}
		nonTimeoutDNSErr := &net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true}
		urlTimeoutErr := &url.Error{Op: "Get", URL: "https://example.com", Err: fmt.Errorf("read failed: %w", os.ErrDeadlineExceeded)}
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
			{"JoinedNonTimeoutFirst", errors.Join(nonTimeoutDNSErr, os.ErrDeadlineExceeded), http.StatusNotFound, "not found: fetch timed out"},
			{"JoinedTimeoutFirst", errors.Join(os.ErrDeadlineExceeded, nonTimeoutDNSErr), http.StatusNotFound, "not found: fetch timed out"},
			{"URLWrappedTimeout", urlTimeoutErr, http.StatusNotFound, "not found: fetch timed out"},
			{"IODeadline", os.ErrDeadlineExceeded, http.StatusNotFound, "not found: fetch timed out"},
			{"WrappedIODeadline", fmt.Errorf("read failed: %w", os.ErrDeadlineExceeded), http.StatusNotFound, "not found: fetch timed out"},
			{"ContextDeadline", context.DeadlineExceeded, http.StatusNotFound, "not found: fetch timed out"},
			{"WrappedContextDeadline", fmt.Errorf("fetch failed: %w", context.DeadlineExceeded), http.StatusNotFound, "not found: fetch timed out"},
			{"WrappedFetchTimeout", fmt.Errorf("fetch failed: %w", errFetchTimedOut), http.StatusNotFound, "not found: fetch timed out"},
			{"NotExistTimeout", notExistErrorf("fetch failed: %w", timeoutErr), http.StatusNotFound, "not found: fetch failed: operation timed out"},
			{"NotExistDeadline", &notExistError{err: context.DeadlineExceeded}, http.StatusNotFound, "not found: context deadline exceeded"},
			{"NotExistFetchTimeout", &notExistError{err: errFetchTimedOut}, http.StatusNotFound, "not found: fetch timed out"},
			{"JoinedNotExistTimeout", errors.Join(fs.ErrNotExist, timeoutErr), http.StatusNotFound, "not found: " + fs.ErrNotExist.Error() + "\noperation timed out"},
			{"JoinedNotExistNonTimeoutFirst", errors.Join(fs.ErrNotExist, nonTimeoutDNSErr, os.ErrDeadlineExceeded), http.StatusNotFound, "not found: " + fs.ErrNotExist.Error() + "\n" + nonTimeoutDNSErr.Error() + "\n" + os.ErrDeadlineExceeded.Error()},
			{"JoinedNotExistTimeoutFirst", errors.Join(fs.ErrNotExist, os.ErrDeadlineExceeded, nonTimeoutDNSErr), http.StatusNotFound, "not found: " + fs.ErrNotExist.Error() + "\n" + os.ErrDeadlineExceeded.Error() + "\n" + nonTimeoutDNSErr.Error()},
			{"NotExistURLWrappedTimeout", &notExistError{err: urlTimeoutErr}, http.StatusNotFound, "not found: " + urlTimeoutErr.Error()},
			{"NotExistBadUpstream", notExistErrorf("fetch failed: %w", errBadUpstream), http.StatusNotFound, "not found: fetch failed: bad upstream"},
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
			wantCacheControl: "public, max-age=600",
			wantContent:      "not found: bad upstream",
		},
		{
			n:                6,
			err:              notExistErrorf("not found: fetch timed out"),
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "public, max-age=600",
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
			err:              &notExistError{err: testTimeoutError{errors.New("operation timed out"), true}},
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "no-store",
			wantContent:      "not found: operation timed out",
		},
		{
			n:                9,
			err:              notExistErrorf("unknown revision %q", "fetch timed out"),
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "public, max-age=600",
			wantContent:      `not found: unknown revision "fetch timed out"`,
		},
		{
			n:                10,
			err:              notExistErrorf("unknown revision %q", "bad upstream"),
			cacheSensitive:   true,
			wantStatusCode:   http.StatusNotFound,
			wantCacheControl: "public, max-age=60",
			wantContent:      `not found: unknown revision "bad upstream"`,
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
