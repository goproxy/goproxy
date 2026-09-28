package goproxy

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aofei/backoff"
)

// httpGet gets the content from the given url, writes it to the dst, and
// returns the response header on success.
func httpGet(ctx context.Context, client *http.Client, url string, dst io.Writer) (http.Header, error) {
	const (
		maxAttempts      = 10
		backoffBase      = 100 * time.Millisecond
		backoffCap       = time.Second
		maxErrorBodySize = 4 << 10
	)

	var lastErr error
	for range backoff.Attempts(ctx, maxAttempts, backoffBase, backoffCap) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, &internalError{err: err}
		}

		resp, err := client.Do(req)
		if err != nil {
			if isRetryableHTTPClientDoError(err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		if resp.StatusCode == http.StatusOK {
			if dst != nil {
				_, err = io.Copy(dst, resp.Body)
			}
			resp.Body.Close()
			if err != nil {
				return nil, err
			}
			return resp.Header, nil
		}

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize+1))
		resp.Body.Close()
		if err != nil {
			return nil, &httpError{err: err, statusCode: resp.StatusCode}
		}
		if len(respBody) > maxErrorBodySize {
			// Avoid splitting a UTF-8 sequence at the truncation boundary.
			end := maxErrorBodySize
			for end > 0 && !utf8.RuneStart(respBody[end]) {
				end--
			}
			respBody = append(respBody[:end], "... (truncated)"...)
		}
		switch resp.StatusCode {
		case http.StatusNotFound, http.StatusGone:
			err := notExistErrorf("%s", respBody)
			if isCacheRestrictedHTTPResponse(resp.Header) {
				return nil, &uncacheableError{err: err}
			}
			return nil, err
		case http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable:
			lastErr = &httpError{err: errBadUpstream, statusCode: resp.StatusCode}
		case http.StatusGatewayTimeout:
			lastErr = &httpError{err: errFetchTimedOut, statusCode: resp.StatusCode}
		default:
			return nil, &httpError{
				err:        fmt.Errorf("GET %s: %s: %s", resp.Request.URL.Redacted(), resp.Status, respBody),
				statusCode: resp.StatusCode,
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, lastErr
}

// httpGetTempWriter marks temporary file write errors as [internalError].
type httpGetTempWriter struct{ io.Writer }

// Write implements [io.Writer].
func (w httpGetTempWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err != nil {
		return n, &internalError{err: err}
	}
	return n, nil
}

// httpGetTemp is like [httpGet] but writes the content to a new temporary file
// in tempDir.
func httpGetTemp(ctx context.Context, client *http.Client, url, tempDir string) (tempFile string, header http.Header, err error) {
	f, err := os.CreateTemp(tempDir, "")
	if err != nil {
		return "", nil, &internalError{err: err}
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	header, err = httpGet(ctx, client, url, httpGetTempWriter{f})
	if err != nil {
		f.Close()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		return "", nil, &internalError{err: err}
	}
	return f.Name(), header, nil
}

// isRetryableHTTPClientDoError reports whether the err is a retryable error
// returned by [http.Client.Do].
func isRetryableHTTPClientDoError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if ue, ok := err.(*url.Error); ok {
		e := ue.Unwrap()
		switch e.(type) {
		case x509.UnknownAuthorityError:
			return false
		}
		if errors.Is(e, http.ErrSchemeMismatch) {
			return false
		}
	}
	return true
}

// isCacheRestrictedHTTPResponse reports whether Cache-Control or Vary prevents
// reuse with the proxy's fixed cache lifetimes. Revalidation requirements are
// treated as restrictions because cached content does not retain upstream
// freshness or validation metadata.
func isCacheRestrictedHTTPResponse(header http.Header) bool {
	var hasMaxAge bool
	for value := strings.Join(header.Values("Cache-Control"), ","); value != ""; {
		// Find the next comma outside a quoted directive argument.
		end, quoted := 0, false
		for end < len(value) {
			if value[end] == ',' && !quoted {
				break
			}
			switch value[end] {
			case '"':
				quoted = !quoted
			case '\\':
				if quoted && end+1 < len(value) {
					end++
				}
			}
			end++
		}
		name, arg, _ := strings.Cut(value[:end], "=")
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "no-store", "no-cache", "private", "must-revalidate", "proxy-revalidate", "s-maxage":
			// s-maxage also requires shared caches to revalidate stale responses.
			return true
		case "max-age":
			if hasMaxAge {
				return true
			}
			hasMaxAge = true
			arg = strings.TrimSpace(arg)
			quoted := len(arg) >= 2 && arg[0] == '"' && arg[len(arg)-1] == '"'
			if quoted {
				arg = arg[1 : len(arg)-1]
			}
			// Check decimal digits directly to avoid integer overflow.
			positive := false
			for i := 0; i < len(arg); i++ {
				if quoted && arg[i] == '\\' {
					i++
					if i == len(arg) {
						return true
					}
				}
				if arg[i] < '0' || arg[i] > '9' {
					return true
				}
				positive = positive || arg[i] != '0'
			}
			if !positive {
				return true
			}
		}
		if end == len(value) {
			break
		}
		value = value[end+1:]
	}
	for _, value := range header.Values("Vary") {
		for name := range strings.SplitSeq(value, ",") {
			if strings.TrimSpace(name) == "*" {
				return true
			}
		}
	}
	return false
}
