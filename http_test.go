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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
	"testing/synctest"
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
	t.Run("ResponseHeader", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			statusCode   int
			wantAttempts int
		}{
			{"Direct", http.StatusOK, 1},
			{"Redirect", http.StatusFound, 2},
			{"Retry", http.StatusServiceUnavailable, 2},
			{"LastRetry", http.StatusServiceUnavailable, 3},
		} {
			for _, policy := range []struct {
				name         string
				cacheControl string
			}{
				{"Public", "public"},
				{"NoStore", "no-store"},
			} {
				t.Run(tt.name+"/"+policy.name, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						wantHeader := http.Header{"Cache-Control": {"max-age=60", policy.cacheControl}}
						attempts := 0
						client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
							attempts++
							if attempts < tt.wantAttempts {
								return &http.Response{
									StatusCode: tt.statusCode,
									Header: http.Header{
										"Cache-Control": {"private"},
										"Vary":          {"*"},
										"Location":      {"/redirected"},
									},
									Body: io.NopCloser(strings.NewReader("intermediate response")),
								}, nil
							}
							return &http.Response{
								StatusCode: http.StatusOK,
								Header:     wantHeader.Clone(),
								Body:       io.NopCloser(strings.NewReader("foobar")),
							}, nil
						})}
						var content bytes.Buffer
						header, err := httpGet(t.Context(), client, "https://example.com", &content)
						if err != nil {
							t.Fatal(err)
						}
						if !maps.EqualFunc(header, wantHeader, slices.Equal) {
							t.Errorf("got header %v, want %v", header, wantHeader)
						}
						if got, want := content.String(), "foobar"; got != want {
							t.Errorf("got content %q, want %q", got, want)
						}
						if got, want := attempts, tt.wantAttempts; got != want {
							t.Errorf("got attempts %d, want %d", got, want)
						}
					})
				})
			}
		}
	})

	t.Run("CopyErrors", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		for _, tt := range []struct {
			name    string
			content io.Reader
			dst     io.Writer
			wantErr error
		}{
			{"Read", iotest.ErrReader(io.ErrUnexpectedEOF), io.Discard, io.ErrUnexpectedEOF},
			{"PartialRead", io.MultiReader(strings.NewReader("foo"), iotest.ErrReader(io.ErrUnexpectedEOF)), io.Discard, io.ErrUnexpectedEOF},
			{"Write", strings.NewReader("foobar"), f, os.ErrClosed},
		} {
			t.Run(tt.name, func(t *testing.T) {
				body := &testHTTPResponseBody{Reader: tt.content}
				client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Cache-Control": {"no-store"}},
						Body:       body,
					}, nil
				})}
				header, err := httpGet(t.Context(), client, "https://example.com", tt.dst)
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("got error %v, want %v", err, tt.wantErr)
				}
				if header != nil {
					t.Errorf("got header %v, want nil", header)
				}
				if !body.closed {
					t.Error("response body was not closed")
				}
			})
		}
	})

	t.Run("ResponseErrors", func(t *testing.T) {
		for _, tt := range []struct {
			name             string
			statusCode       int
			wantAttempts     int
			wantErr          error
			wantStatusCode   int
			wantCacheControl string
		}{
			{"BadRequest", http.StatusBadRequest, 1, nil, http.StatusInternalServerError, "no-store"},
			{"RequestTimeout", http.StatusRequestTimeout, 1, nil, http.StatusInternalServerError, "no-store"},
			{"NotFound", http.StatusNotFound, 1, fs.ErrNotExist, http.StatusNotFound, "public, max-age=600"},
			{"Gone", http.StatusGone, 1, fs.ErrNotExist, http.StatusNotFound, "public, max-age=600"},
			{"TooManyRequests", http.StatusTooManyRequests, 3, errBadUpstream, http.StatusNotFound, "no-store"},
			{"InternalServerError", http.StatusInternalServerError, 3, errBadUpstream, http.StatusNotFound, "no-store"},
			{"BadGateway", http.StatusBadGateway, 3, errBadUpstream, http.StatusNotFound, "no-store"},
			{"ServiceUnavailable", http.StatusServiceUnavailable, 3, errBadUpstream, http.StatusNotFound, "no-store"},
			{"GatewayTimeout", http.StatusGatewayTimeout, 3, errFetchTimedOut, http.StatusNotFound, "no-store"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					attempts := 0
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						attempts++
						return &http.Response{
							StatusCode: tt.statusCode,
							Status:     strconv.Itoa(tt.statusCode) + " " + http.StatusText(tt.statusCode),
							Header:     http.Header{"Cache-Control": {"public"}},
							Body:       io.NopCloser(strings.NewReader("upstream details")),
							Request:    req,
						}, nil
					})}
					header, err := httpGet(t.Context(), client, "https://example.com", nil)
					if err == nil {
						t.Fatal("expected error")
					}
					if header != nil {
						t.Errorf("got header %v, want nil", header)
					}
					if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
						t.Errorf("got error %v, want an error matching %v", err, tt.wantErr)
					}
					httpErr, ok := errors.AsType[*httpError](err)
					if tt.wantErr == fs.ErrNotExist {
						if ok {
							t.Errorf("unexpected HTTP failure %v", httpErr)
						}
					} else if !ok || httpErr.statusCode != tt.statusCode {
						t.Errorf("got error %#v, want HTTP status %d", httpErr, tt.statusCode)
					}
					if got, want := attempts, tt.wantAttempts; got != want {
						t.Errorf("got attempts %d, want %d", got, want)
					}
					rec := httptest.NewRecorder()
					responseError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err, false)
					if got, want := rec.Code, tt.wantStatusCode; got != want {
						t.Errorf("got module status %d, want %d", got, want)
					}
					if got, want := rec.Header().Get("Cache-Control"), tt.wantCacheControl; got != want {
						t.Errorf("got module cache control %q, want %q", got, want)
					}
				})
			})
		}
	})

	t.Run("TransportRetries", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			failures int
			wantErr  error
		}{
			{"LastAttemptSuccess", 2, nil},
			{"Exhausted", 3, syscall.ECONNRESET},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					attempts := 0
					var lastAttempt time.Time
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						attempts++
						lastAttempt = time.Now()
						if attempts <= tt.failures {
							return nil, syscall.ECONNRESET
						}
						return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
					})}
					_, err := httpGet(t.Context(), client, "https://example.com", nil)
					if !errors.Is(err, tt.wantErr) {
						t.Errorf("got error %v, want %v", err, tt.wantErr)
					}
					if got, want := attempts, 3; got != want {
						t.Errorf("got attempts %d, want %d", got, want)
					}
					if got := time.Since(lastAttempt); got != 0 {
						t.Errorf("got delay after final attempt %v, want 0", got)
					}
				})
			})
		}
	})

	t.Run("RetryAfter", func(t *testing.T) {
		for _, statusCode := range []int{
			http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		} {
			for _, tt := range []struct {
				name         string
				values       []string
				date         bool
				wantAttempts int
				wantDelay    time.Duration
			}{
				{"Seconds", []string{"1"}, false, 2, time.Second},
				{"LeadingZeros", []string{"0001"}, false, 2, time.Second},
				{"Whitespace", []string{" \t1\t "}, false, 2, time.Second},
				{"Date", []string{http.TimeFormat}, true, 2, time.Second},
				{"RFC850Date", []string{time.RFC850}, true, 2, time.Second},
				{"ANSICDate", []string{time.ANSIC}, true, 2, time.Second},
				{"OverLimit", []string{"2"}, false, 1, 0},
				{"LongDelay", []string{"60"}, false, 1, 0},
				{"DurationOverflow", []string{"9223372037"}, false, 1, 0},
				{"IntegerOverflow", []string{strings.Repeat("9", 128)}, false, 1, 0},
				{"DistantDate", []string{"Fri, 31 Dec 9999 23:59:59 GMT"}, false, 1, 0},
				{"Absent", nil, false, 2, -1},
				{"Zero", []string{"0"}, false, 2, -1},
				{"PastDate", []string{"Sun, 06 Nov 1994 08:49:37 GMT"}, false, 2, -1},
				{"Invalid", []string{"invalid"}, false, 2, -1},
				{"Negative", []string{"-1"}, false, 2, -1},
				{"Signed", []string{"+1"}, false, 2, -1},
				{"Fractional", []string{"0.5"}, false, 2, -1},
				{"Quoted", []string{"\"1\""}, false, 2, -1},
				{"InvalidOverflowSuffix", []string{strings.Repeat("9", 128) + "x"}, false, 2, -1},
				{"RepeatedFields", []string{"1", "60"}, false, 2, -1},
				{"CombinedFields", []string{"1, 60"}, false, 2, -1},
			} {
				t.Run(strconv.Itoa(statusCode)+"/"+tt.name, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						started := time.Now()
						values := tt.values
						if tt.date {
							values = []string{started.Add(time.Second).UTC().Format(tt.values[0])}
						}
						body := &testHTTPResponseBody{Reader: strings.NewReader("upstream details")}
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
								StatusCode: statusCode,
								Header:     http.Header{"Retry-After": values},
								Body:       body,
							}, nil
						})}
						_, err := httpGet(t.Context(), client, "https://example.com", nil)
						if tt.wantAttempts == 1 {
							httpErr, ok := errors.AsType[*httpError](err)
							if !ok || httpErr.statusCode != statusCode {
								t.Errorf("got error %#v, want HTTP status %d", err, statusCode)
							}
							wantErr := errBadUpstream
							if statusCode == http.StatusGatewayTimeout {
								wantErr = errFetchTimedOut
							}
							if !errors.Is(err, wantErr) {
								t.Errorf("got error %v, want %v", err, wantErr)
							}
							if t.Context().Err() != nil {
								t.Error("request context was canceled")
							}
						} else if err != nil {
							t.Errorf("unexpected error %v", err)
						}
						if got, want := attempts, tt.wantAttempts; got != want {
							t.Errorf("got attempts %d, want %d", got, want)
						}
						if got := time.Since(started); tt.wantDelay >= 0 {
							if got != tt.wantDelay {
								t.Errorf("got delay %v, want %v", got, tt.wantDelay)
							}
						} else if got >= 100*time.Millisecond {
							t.Errorf("got delay %v, want less than 100ms", got)
						}
						if !body.closed {
							t.Error("response body was not closed")
						}
					})
				})
			}
		}
	})

	t.Run("RetryAfterContext", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			timeout   time.Duration
			cancel    bool
			wantErr   error
			wantDelay time.Duration
		}{
			{"InsufficientTime", 500 * time.Millisecond, false, errBadUpstream, 0},
			{"DeadlineBoundary", time.Second, false, errBadUpstream, 0},
			{"CanceledWhileWaiting", 0, true, context.Canceled, 200 * time.Millisecond},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					if tt.timeout > 0 {
						var stop context.CancelFunc
						ctx, stop = context.WithTimeout(ctx, tt.timeout)
						defer stop()
					}
					if tt.cancel {
						go func() {
							time.Sleep(tt.wantDelay)
							cancel()
						}()
					}
					attempts := 0
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						attempts++
						return &http.Response{
							StatusCode: http.StatusServiceUnavailable,
							Header:     http.Header{"Retry-After": {"1"}},
							Body:       http.NoBody,
						}, nil
					})}
					started := time.Now()
					_, err := httpGet(ctx, client, "https://example.com", nil)
					if !errors.Is(err, tt.wantErr) {
						t.Errorf("got error %v, want %v", err, tt.wantErr)
					}
					if got, want := attempts, 1; got != want {
						t.Errorf("got attempts %d, want %d", got, want)
					}
					if got, want := time.Since(started), tt.wantDelay; got != want {
						t.Errorf("got delay %v, want %v", got, want)
					}
					if !tt.cancel && ctx.Err() != nil {
						t.Errorf("unexpected context error %v", ctx.Err())
					}
				})
			})
		}
	})

	t.Run("RetryAfterAttempts", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			attempts := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     http.Header{"Retry-After": {"1"}},
					Body:       http.NoBody,
				}, nil
			})}
			started := time.Now()
			if _, err := httpGet(t.Context(), client, "https://example.com", nil); !errors.Is(err, errBadUpstream) {
				t.Errorf("got error %v, want %v", err, errBadUpstream)
			}
			if got, want := attempts, 3; got != want {
				t.Errorf("got attempts %d, want %d", got, want)
			}
			if got, want := time.Since(started), 2*time.Second; got != want {
				t.Errorf("got delay %v, want %v", got, want)
			}
		})
	})

	t.Run("RetryAfterBody", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			bodyDelay time.Duration
			timeout   time.Duration
			wantErr   error
			minDelay  time.Duration
			maxDelay  time.Duration
		}{
			{"ShortRead", 200 * time.Millisecond, 0, nil, time.Second, time.Second},
			{"ExpiredAfterRead", 2 * time.Second, 0, nil, 2 * time.Second, 2100 * time.Millisecond},
			{"DeadlineDuringRead", 600 * time.Millisecond, 500 * time.Millisecond, context.DeadlineExceeded, 600 * time.Millisecond, 600 * time.Millisecond},
		} {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx := t.Context()
					if tt.timeout > 0 {
						var cancel context.CancelFunc
						ctx, cancel = context.WithTimeout(ctx, tt.timeout)
						defer cancel()
					}
					r, w := io.Pipe()
					defer r.Close()
					go func() {
						time.Sleep(tt.bodyDelay)
						w.Close()
					}()
					body := &testHTTPResponseBody{Reader: r}
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
							StatusCode: http.StatusServiceUnavailable,
							Header:     http.Header{"Retry-After": {"1"}},
							Body:       body,
						}, nil
					})}
					started := time.Now()
					_, err := httpGet(ctx, client, "https://example.com", nil)
					if !errors.Is(err, tt.wantErr) {
						t.Errorf("got error %v, want %v", err, tt.wantErr)
					}
					wantAttempts := 2
					if tt.wantErr != nil {
						wantAttempts = 1
					}
					if got, want := attempts, wantAttempts; got != want {
						t.Errorf("got attempts %d, want %d", got, want)
					}
					if got := time.Since(started); got < tt.minDelay || got > tt.maxDelay {
						t.Errorf("got delay %v, want between %v and %v", got, tt.minDelay, tt.maxDelay)
					}
					if !body.closed {
						t.Error("response body was not closed")
					}
				})
			})
		}
	})

	t.Run("RetryAfterNonRetryable", func(t *testing.T) {
		for _, statusCode := range []int{
			http.StatusOK, http.StatusBadRequest, http.StatusNotFound, http.StatusGone, http.StatusNotImplemented,
		} {
			t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					attempts := 0
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						attempts++
						return &http.Response{
							StatusCode: statusCode,
							Header:     http.Header{"Retry-After": {"60"}},
							Body:       http.NoBody,
							Request:    req,
						}, nil
					})}
					started := time.Now()
					_, err := httpGet(t.Context(), client, "https://example.com", nil)
					if got, want := err == nil, statusCode == http.StatusOK; got != want {
						t.Errorf("got error %v for status %d", err, statusCode)
					}
					if got, want := attempts, 1; got != want {
						t.Errorf("got attempts %d, want %d", got, want)
					}
					if got := time.Since(started); got != 0 {
						t.Errorf("got delay %v, want 0", got)
					}
				})
			})
		}
	})

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
					_, err := httpGet(t.Context(), client, "https://example.com", nil)
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
				{"MustRevalidate", http.Header{"Cache-Control": {"max-age=60, must-revalidate"}}, "module unavailable", "no-store"},
				{"ProxyRevalidate", http.Header{"Cache-Control": {"proxy-revalidate"}}, "module unavailable", "no-store"},
				{"ZeroMaxAge", http.Header{"Cache-Control": {"max-age=0"}}, "module unavailable", "no-store"},
				{"QuotedZeroMaxAge", http.Header{"Cache-Control": {`max-age="\0"`}}, "module unavailable", "no-store"},
				{"ZeroSharedMaxAge", http.Header{"Cache-Control": {"s-maxage=0"}}, "module unavailable", "no-store"},
				{"PositiveSharedMaxAge", http.Header{"Cache-Control": {"s-maxage=60"}}, "module unavailable", "no-store"},
				{"PositiveMaxAge", http.Header{"Cache-Control": {"max-age=60"}}, "module unavailable", "public, max-age=600"},
				{"InvalidMaxAge", http.Header{"Cache-Control": {"max-age=invalid"}}, "module unavailable", "no-store"},
				{"DuplicateMaxAge", http.Header{"Cache-Control": {"max-age=60, max-age=120"}}, "module unavailable", "no-store"},
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
					_, err := httpGet(t.Context(), http.DefaultClient, server.URL, nil)
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
					<-req.Context().Done()
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
				header, err := httpGet(ctx, client, server.URL, &content)
				if wantErr != nil {
					if err == nil {
						t.Fatal("expected error")
					}
					if header != nil {
						t.Errorf("got header %v, want nil", header)
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
		if _, err := httpGet(t.Context(), http.DefaultClient, "::", nil); err == nil {
			t.Fatal("expected error")
		} else if _, ok := errors.AsType[*internalError](err); !ok {
			t.Errorf("got error %v, want an internal error", err)
		}
	})
}

func TestHTTPGetTemp(t *testing.T) {
	t.Run("MissingTempDir", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		file, _, err := httpGetTemp(t.Context(), http.DefaultClient, "https://example.com", missing)
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

	for _, tt := range []struct {
		name            string
		op              string
		closeBeforeCopy bool
	}{
		{"CloseError", "close", false},
		{"WriteError", "write", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &testHTTPResponseBody{Reader: strings.NewReader("foobar")}
			var tempFile *os.File
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Cache-Control": {"no-store"}},
					Body: struct {
						io.ReadCloser
						io.WriterTo
					}{body, writerToFunc(func(w io.Writer) (int64, error) {
						tempFile = w.(httpGetTempWriter).Writer.(*os.File)
						if tt.closeBeforeCopy {
							if err := tempFile.Close(); err != nil {
								return 0, err
							}
						}
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
			file, header, err := httpGetTemp(t.Context(), client, "https://example.com", tempDir)
			if file != "" {
				t.Errorf("unexpected temporary file %q", file)
			}
			if header != nil {
				t.Errorf("got header %v, want nil", header)
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
			if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Op != tt.op || pe.Path != tempFile.Name() {
				t.Errorf("got error %v, want a %s error for %q", err, tt.op, tempFile.Name())
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
	}

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
				file, _, err := httpGetTemp(t.Context(), http.DefaultClient, server.URL, tempDir)
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

			tempFile, _, err := httpGetTemp(t.Context(), http.DefaultClient, server.URL, t.TempDir())
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
		{"MustRevalidate", http.Header{"Cache-Control": {"must-revalidate"}}, true},
		{"ProxyRevalidate", http.Header{"Cache-Control": {"proxy-revalidate"}}, true},
		{"FreshMustRevalidate", http.Header{"Cache-Control": {"max-age=60, must-revalidate"}}, true},
		{"FreshProxyRevalidate", http.Header{"Cache-Control": {"max-age=60, proxy-revalidate"}}, true},
		{"MixedCaseRevalidation", http.Header{"Cache-Control": {"public, MuSt-ReVaLiDaTe"}}, true},
		{"RevalidationArgument", http.Header{"Cache-Control": {"proxy-revalidate=invalid"}}, true},
		{"ZeroMaxAge", http.Header{"Cache-Control": {"max-age=0"}}, true},
		{"LeadingZeroMaxAge", http.Header{"Cache-Control": {"max-age=000"}}, true},
		{"LongZeroMaxAge", http.Header{"Cache-Control": {"max-age=" + strings.Repeat("0", 128)}}, true},
		{"QuotedZeroMaxAge", http.Header{"Cache-Control": {`max-age="0"`}}, true},
		{"EscapedZeroMaxAge", http.Header{"Cache-Control": {`max-age="\0\00"`}}, true},
		{"MixedCaseMaxAge", http.Header{"Cache-Control": {"MAX-AGE \t= \t0 \t"}}, true},
		{"PositiveMaxAge", http.Header{"Cache-Control": {"max-age=60"}}, false},
		{"LeadingZerosPositiveMaxAge", http.Header{"Cache-Control": {"max-age=00060"}}, false},
		{"QuotedPositiveMaxAge", http.Header{"Cache-Control": {`max-age="60"`}}, false},
		{"EscapedPositiveMaxAge", http.Header{"Cache-Control": {`max-age="\60"`}}, false},
		{"OverflowMaxAge", http.Header{"Cache-Control": {"max-age=" + strings.Repeat("9", 128)}}, false},
		{"LongLeadingZerosPositiveMaxAge", http.Header{"Cache-Control": {"max-age=" + strings.Repeat("0", 128) + "1"}}, false},
		{"MissingMaxAge", http.Header{"Cache-Control": {"max-age"}}, true},
		{"EmptyMaxAge", http.Header{"Cache-Control": {"max-age="}}, true},
		{"EmptyQuotedMaxAge", http.Header{"Cache-Control": {`max-age=""`}}, true},
		{"NegativeMaxAge", http.Header{"Cache-Control": {"max-age=-1"}}, true},
		{"SignedMaxAge", http.Header{"Cache-Control": {"max-age=+1"}}, true},
		{"FractionalMaxAge", http.Header{"Cache-Control": {"max-age=0.5"}}, true},
		{"InvalidMaxAge", http.Header{"Cache-Control": {"max-age=invalid"}}, true},
		{"InvalidMaxAgeSuffix", http.Header{"Cache-Control": {"max-age=60x"}}, true},
		{"OverflowMaxAgeWithInvalidSuffix", http.Header{"Cache-Control": {"max-age=" + strings.Repeat("9", 128) + "x"}}, true},
		{"UnterminatedMaxAge", http.Header{"Cache-Control": {`max-age="60`}}, true},
		{"UnmatchedMaxAgeQuote", http.Header{"Cache-Control": {`max-age=60"`}}, true},
		{"QuotedMaxAgeWhitespace", http.Header{"Cache-Control": {`max-age=" 60 "`}}, true},
		{"EscapedMaxAgeBackslash", http.Header{"Cache-Control": {`max-age="\\60"`}}, true},
		{"UnterminatedMaxAgeEscape", http.Header{"Cache-Control": {`max-age="60\"`}}, true},
		{"NonDecimalMaxAgeEscape", http.Header{"Cache-Control": {`max-age="\x30"`}}, true},
		{"DuplicateMaxAge", http.Header{"Cache-Control": {"max-age=60, max-age=60"}}, true},
		{"DuplicateMaxAgeFields", http.Header{"Cache-Control": {"max-age=60", "max-age=120"}}, true},
		{"DuplicateZeroMaxAgeFirst", http.Header{"Cache-Control": {"max-age=0, max-age=60"}}, true},
		{"DuplicateZeroMaxAgeLast", http.Header{"Cache-Control": {"max-age=60, max-age=0"}}, true},
		{"ZeroSharedMaxAge", http.Header{"Cache-Control": {"s-maxage=0"}}, true},
		{"PositiveSharedMaxAge", http.Header{"Cache-Control": {"s-maxage=60"}}, true},
		{"QuotedSharedMaxAge", http.Header{"Cache-Control": {`S-MAXAGE="60"`}}, true},
		{"MissingSharedMaxAge", http.Header{"Cache-Control": {"s-maxage"}}, true},
		{"InvalidSharedMaxAge", http.Header{"Cache-Control": {"s-maxage=invalid"}}, true},
		{"SharedMaxAgeAfterZeroMaxAge", http.Header{"Cache-Control": {"max-age=0, s-maxage=60"}}, true},
		{"SharedMaxAgeBeforeZeroMaxAge", http.Header{"Cache-Control": {"s-maxage=60, max-age=0"}}, true},
		{"SharedMaxAgeOverridesPositiveMaxAge", http.Header{"Cache-Control": {"max-age=60", "s-maxage=0"}}, true},
		{"RevalidationPrefixes", http.Header{"Cache-Control": {"x-must-revalidate, proxy-revalidate-extra, x-s-maxage=0, max-age-extra=0"}}, false},
		{"QuotedRevalidation", http.Header{"Cache-Control": {`extension="must-revalidate, proxy-revalidate, s-maxage=0, max-age=0"`}}, false},
		{"RevalidationAfterQuotedArgument", http.Header{"Cache-Control": {`extension="a,b", must-revalidate`}}, true},
		{"RevalidationAfterSplitQuotedArgument", http.Header{"Cache-Control": {`extension="a`, `b", proxy-revalidate`}}, true},
		{"SplitQuotedRevalidation", http.Header{"Cache-Control": {`extension="a`, "must-revalidate", "s-maxage=0", `b", max-age=60`}}, false},
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

func TestParseHTTPRetryAfter(t *testing.T) {
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name  string
		value string
		want  time.Time
	}{
		{"Empty", "", time.Time{}},
		{"Whitespace", " \t", time.Time{}},
		{"Zero", "0", now},
		{"Seconds", "60", now.Add(time.Minute)},
		{"LeadingZeros", "00060", now.Add(time.Minute)},
		{"LongLeadingZeros", strings.Repeat("0", 128) + "1", now.Add(time.Second)},
		{"SurroundingWhitespace", " \t60\t ", now.Add(time.Minute)},
		{"Date", "Tue, 29 Sep 2026 12:01:00 GMT", now.Add(time.Minute)},
		{"RFC850Date", "Tuesday, 29-Sep-26 12:01:00 GMT", now.Add(time.Minute)},
		{"ANSICDate", "Tue Sep 29 12:01:00 2026", now.Add(time.Minute)},
		{"PastDate", "Tue, 29 Sep 2026 11:59:00 GMT", now.Add(-time.Minute)},
		{"MaximumDurationSeconds", "9223372036", now.Add(9223372036 * time.Second)},
		{"DurationOverflow", "9223372037", now.Add(9223372036 * time.Second)},
		{"MaximumInteger", "18446744073709551615", now.Add(9223372036 * time.Second)},
		{"IntegerOverflow", "18446744073709551616", now.Add(9223372036 * time.Second)},
		{"LongInteger", strings.Repeat("9", 128), now.Add(9223372036 * time.Second)},
		{"Negative", "-1", time.Time{}},
		{"Signed", "+1", time.Time{}},
		{"Fractional", "0.5", time.Time{}},
		{"Hexadecimal", "0x10", time.Time{}},
		{"Quoted", "\"1\"", time.Time{}},
		{"InternalWhitespace", "1 0", time.Time{}},
		{"Invalid", "invalid", time.Time{}},
		{"InvalidOverflowSuffix", strings.Repeat("9", 128) + "x", time.Time{}},
		{"MultipleSeconds", "1, 60", time.Time{}},
		{"MultipleDates", "Tue, 29 Sep 2026 12:01:00 GMT,Tue, 29 Sep 2026 12:02:00 GMT", time.Time{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseHTTPRetryAfter(tt.value, now); !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
