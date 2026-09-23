package goproxy

import (
	"bytes"
	"compress/gzip"
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
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type writerToFunc func(io.Writer) (int64, error)

func (f writerToFunc) WriteTo(w io.Writer) (int64, error) { return f(w) }

type testHTTPResponseBody struct {
	io.Reader
	bytesRead int
	closed    bool
}

func (b *testHTTPResponseBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.bytesRead += n
	return n, err
}

func (b *testHTTPResponseBody) Close() error {
	b.closed = true
	return nil
}

func TestHTTPGet(t *testing.T) {
	t.Run("ErrorBodyLimit", func(t *testing.T) {
		const maxErrorBodySize = 4 << 10
		atLimit := strings.Repeat("x", maxErrorBodySize)
		aboveLimit := atLimit + "x"
		for _, statusCode := range []int{
			http.StatusBadRequest, http.StatusNotFound, http.StatusGone,
			http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusNotImplemented,
		} {
			for _, tt := range []struct {
				name        string
				content     io.Reader
				wantRead    int
				wantMessage string
				wantErr     error
			}{
				{"Empty", strings.NewReader(""), 0, "", nil},
				{"Small", strings.NewReader("not found"), 9, "not found", nil},
				{"BelowLimit", strings.NewReader(atLimit[:maxErrorBodySize-1]), maxErrorBodySize - 1, atLimit[:maxErrorBodySize-1], nil},
				{"AtLimit", strings.NewReader(atLimit), maxErrorBodySize, atLimit, nil},
				{"AboveLimit", strings.NewReader(aboveLimit), maxErrorBodySize + 1, atLimit + "... (truncated)", nil},
				{"BadUpstreamAfterLimit", strings.NewReader(aboveLimit + "bad upstream"), maxErrorBodySize + 1, atLimit + "... (truncated)", nil},
				{"TimeoutAfterLimit", strings.NewReader(aboveLimit + "fetch timed out"), maxErrorBodySize + 1, atLimit + "... (truncated)", nil},
				{"TwoByteRune", strings.NewReader(atLimit[:maxErrorBodySize-1] + "\u00e9"), maxErrorBodySize + 1, atLimit[:maxErrorBodySize-1] + "... (truncated)", nil},
				{"ThreeByteRune", strings.NewReader(atLimit[:maxErrorBodySize-2] + "\u4e2d"), maxErrorBodySize + 1, atLimit[:maxErrorBodySize-2] + "... (truncated)", nil},
				{"FourByteRune", strings.NewReader(atLimit[:maxErrorBodySize-3] + "\U0001f600"), maxErrorBodySize + 1, atLimit[:maxErrorBodySize-3] + "... (truncated)", nil},
				{"CompleteRuneAtLimit", strings.NewReader(atLimit[:maxErrorBodySize-3] + "\u4e2d" + "x"), maxErrorBodySize + 1, atLimit[:maxErrorBodySize-3] + "\u4e2d... (truncated)", nil},
				{"InvalidUTF8", strings.NewReader(strings.Repeat("\x80", maxErrorBodySize+1)), maxErrorBodySize + 1, "... (truncated)", nil},
				{"ReadError", iotest.ErrReader(io.ErrUnexpectedEOF), 0, "", io.ErrUnexpectedEOF},
				{"DataAndReadError", iotest.DataErrReader(io.MultiReader(strings.NewReader("foo"), iotest.ErrReader(io.ErrUnexpectedEOF))), 3, "", io.ErrUnexpectedEOF},
				{"AboveLimitAndReadError", iotest.DataErrReader(io.MultiReader(strings.NewReader(aboveLimit), iotest.ErrReader(io.ErrUnexpectedEOF))), maxErrorBodySize + 1, "", io.ErrUnexpectedEOF},
				{"ReadErrorAfterLimit", io.MultiReader(strings.NewReader(aboveLimit), iotest.ErrReader(io.ErrUnexpectedEOF)), maxErrorBodySize + 1, atLimit + "... (truncated)", nil},
			} {
				t.Run(strconv.Itoa(statusCode)+"/"+tt.name, func(t *testing.T) {
					body := &testHTTPResponseBody{Reader: tt.content}
					attempts := 0
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						attempts++
						if attempts > 1 {
							if !body.closed {
								t.Error("response body was not closed before retrying")
							}
							return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
						}
						return &http.Response{
							StatusCode:    statusCode,
							Status:        strconv.Itoa(statusCode) + " " + http.StatusText(statusCode),
							Body:          body,
							ContentLength: -1,
							Request:       req,
						}, nil
					})}
					err := httpGet(t.Context(), client, "https://example.com", nil)
					if got, want := body.bytesRead, tt.wantRead; got != want {
						t.Errorf("got bytes read %d, want %d", got, want)
					}
					if !body.closed {
						t.Error("response body was not closed")
					}
					wantAttempts := 1
					if tt.wantErr != nil {
						if !errors.Is(err, tt.wantErr) {
							t.Errorf("got %v, want %v", err, tt.wantErr)
						}
					} else {
						switch statusCode {
						case http.StatusNotFound, http.StatusGone:
							if !errors.Is(err, fs.ErrNotExist) {
								t.Fatalf("got %v, want %v", err, fs.ErrNotExist)
							}
							if got, want := err.Error(), tt.wantMessage; got != want {
								t.Errorf("got %q, want %q", got, want)
							}
							rec := httptest.NewRecorder()
							responseError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err, false)
							if got, want := rec.Header().Get("Cache-Control"), "public, max-age=600"; got != want {
								t.Errorf("got %q, want %q", got, want)
							}
						case http.StatusBadRequest, http.StatusNotImplemented:
							want := fmt.Sprintf("GET https://example.com: %d %s: %s", statusCode, http.StatusText(statusCode), tt.wantMessage)
							if err == nil || err.Error() != want {
								t.Errorf("got %v, want %s", err, want)
							}
							if errors.Is(err, fs.ErrNotExist) {
								t.Errorf("unexpected absence error %v", err)
							}
						default:
							wantAttempts = 2
							if err != nil {
								t.Errorf("unexpected error %v", err)
							}
						}
					}
					if got, want := attempts, wantAttempts; got != want {
						t.Errorf("got attempts %d, want %d", got, want)
					}
				})
			}
		}
	})

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
					if err == nil {
						t.Fatal("expected error")
					}
					wantMessage := tt.body
					wantStatusCode := http.StatusNotFound
					wantCacheControl := tt.wantCacheControl
					wantContent := "not found: " + tt.body
					if statusCode == http.StatusBadRequest {
						wantMessage = "GET " + server.URL + ": 400 Bad Request: " + tt.body
						wantStatusCode = http.StatusInternalServerError
						wantCacheControl = "no-store"
						wantContent = "internal server error"
					}
					if got, want := errors.Is(err, fs.ErrNotExist), statusCode != http.StatusBadRequest; got != want {
						t.Errorf("got absence %t, want %t", got, want)
					}
					if got, want := err.Error(), wantMessage; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					rec := httptest.NewRecorder()
					responseError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err, false)
					if got, want := rec.Code, wantStatusCode; got != want {
						t.Errorf("got %d, want %d", got, want)
					}
					if got, want := rec.Header().Get("Cache-Control"), wantCacheControl; got != want {
						t.Errorf("got %q, want %q", got, want)
					}
					if got, want := rec.Body.String(), wantContent; got != want {
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
			{
				n:           10,
				handler:     func(rw http.ResponseWriter, req *http.Request) { io.WriteString(rw, strings.Repeat("x", 8<<10)) },
				wantContent: strings.Repeat("x", 8<<10),
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
	t.Run("MissingTempDir", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		file, err := httpGetTemp(t.Context(), http.DefaultClient, "https://example.com", missing)
		if file != "" {
			t.Errorf("unexpected temporary file %q", file)
		}
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), missing) {
			t.Errorf("got error %v, want a file error for %q", err, missing)
		}
		if _, ok := errors.AsType[*internalError](err); !ok {
			t.Errorf("got error %v, want an internal error", err)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("got error %v, want an error matching %v", err, fs.ErrNotExist)
		}
	})

	t.Run("CloseError", func(t *testing.T) {
		body := &testHTTPResponseBody{Reader: strings.NewReader("foobar")}
		var tempFile *os.File
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: struct {
					io.ReadCloser
					io.WriterTo
				}{body, writerToFunc(func(w io.Writer) (int64, error) {
					tempFile = w.(*os.File)
					n, err := io.Copy(w, body)
					if err != nil {
						return n, err
					}
					return n, tempFile.Close()
				})},
				Request: req,
			}, nil
		})}
		tempDir := t.TempDir()
		file, err := httpGetTemp(t.Context(), client, "https://example.com", tempDir)
		if file != "" {
			t.Errorf("unexpected temporary file %q", file)
		}
		if _, ok := errors.AsType[*internalError](err); !ok {
			t.Errorf("got error %v, want an internal error", err)
		}
		if !errors.Is(err, os.ErrClosed) {
			t.Errorf("got error %v, want an error matching %v", err, os.ErrClosed)
		}
		if tempFile == nil {
			t.Fatal("temporary file not found")
		}
		if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Op != "close" || pe.Path != tempFile.Name() {
			t.Errorf("got error %v, want a close error for %q", err, tempFile.Name())
		}
		if !body.closed || body.bytesRead != len("foobar") {
			t.Errorf("got response body %+v, want fully read and closed", body)
		}
		if entries, err := os.ReadDir(tempDir); err != nil {
			t.Fatal(err)
		} else if len(entries) != 0 {
			t.Errorf("unexpected temporary files %v", entries)
		}
	})

	t.Run("ErrorBodyLimit", func(t *testing.T) {
		for _, tt := range []struct {
			name             string
			chunked          bool
			gzip             bool
			header           http.Header
			wantCacheControl string
		}{
			{"ContentLength", false, false, nil, "public, max-age=600"},
			{"Chunked", true, false, nil, "public, max-age=600"},
			{"Gzip", false, true, nil, "public, max-age=600"},
			{"NoStore", false, false, http.Header{"Cache-Control": {"no-store"}}, "no-store"},
			{"NoCache", true, false, http.Header{"Cache-Control": {"no-cache"}}, "no-store"},
			{"Private", false, true, http.Header{"Cache-Control": {"private"}}, "no-store"},
			{"VaryAll", false, false, http.Header{"Vary": {"*"}}, "no-store"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				body := []byte(strings.Repeat("x", 8<<10))
				if tt.gzip {
					var buf bytes.Buffer
					zw := gzip.NewWriter(&buf)
					if _, err := zw.Write(body); err != nil {
						t.Fatal(err)
					}
					if err := zw.Close(); err != nil {
						t.Fatal(err)
					}
					body = buf.Bytes()
				}
				server := newHTTPTestServer(t, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					maps.Copy(rw.Header(), tt.header)
					if tt.gzip {
						rw.Header().Set("Content-Encoding", "gzip")
					}
					if !tt.chunked {
						rw.Header().Set("Content-Length", strconv.Itoa(len(body)))
					}
					rw.WriteHeader(http.StatusNotFound)
					if tt.chunked {
						rw.(http.Flusher).Flush()
					}
					rw.Write(body)
				}))
				tempDir := t.TempDir()
				file, err := httpGetTemp(t.Context(), http.DefaultClient, server.URL, tempDir)
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("got %v, want %v", err, fs.ErrNotExist)
				}
				if got, want := err.Error(), strings.Repeat("x", 4<<10)+"... (truncated)"; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					rec := httptest.NewRecorder()
					responseError(rec, httptest.NewRequest(method, "/", nil), err, false)
					if got, want := rec.Code, http.StatusNotFound; got != want {
						t.Errorf("method %s: got status %d, want %d", method, got, want)
					}
					if got, want := rec.Header().Get("Cache-Control"), tt.wantCacheControl; got != want {
						t.Errorf("method %s: got cache control %q, want %q", method, got, want)
					}
					wantContent := "not found: " + err.Error()
					if method == http.MethodHead {
						wantContent = ""
					}
					if got, want := rec.Body.String(), wantContent; got != want {
						t.Errorf("method %s: got content %q, want %q", method, got, want)
					}
				}
				if file != "" {
					t.Errorf("got temporary file %q, want none", file)
				}
				if entries, err := os.ReadDir(tempDir); err != nil {
					t.Fatal(err)
				} else if len(entries) != 0 {
					t.Errorf("got %d temporary files, want none", len(entries))
				}
			})
		}
	})

	for _, tt := range []struct {
		n           int
		handler     http.HandlerFunc
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
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			server := newHTTPTestServer(t, tt.handler)

			tempFile, err := httpGetTemp(t.Context(), http.DefaultClient, server.URL, t.TempDir())
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
