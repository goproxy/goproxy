package goproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aofei/backoff"
)

// httpGet gets the content from the given url, writes it to the dst, and
// returns the response header on success.
func httpGet(ctx context.Context, client *http.Client, url string, dst io.Writer) (http.Header, error) {
	const (
		maxAttempts      = 3
		backoffBase      = 100 * time.Millisecond
		backoffCap       = time.Second
		maxErrorBodySize = 4 << 10
	)

	var (
		lastErr    error
		retryAfter time.Time
	)
	for range backoff.Attempts(ctx, maxAttempts, backoffBase, backoffCap) {
		if delay := time.Until(retryAfter); delay > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(delay):
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

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

		// Include response body reading time in the retry delay.
		retryAfter = parseHTTPRetryAfter(strings.Join(resp.Header.Values("Retry-After"), ","), time.Now())

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
			if isCacheRestrictedHTTPResponse(resp.Header) {
				return nil, uncacheableNotExistErrorf("%s", respBody)
			}
			return nil, notExistErrorf("%s", respBody)
		case http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable:
			lastErr = &httpError{err: errBadUpstream, statusCode: resp.StatusCode}
		case http.StatusGatewayTimeout:
			lastErr = &httpError{err: errFetchTimedOut, statusCode: resp.StatusCode}
		default:
			u := req.URL
			if resp.Request != nil && resp.Request.URL != nil {
				u = resp.Request.URL
			}
			return nil, &httpError{
				err:        fmt.Errorf("GET %s: %s: %s", u.Redacted(), resp.Status, respBody),
				statusCode: resp.StatusCode,
			}
		}

		// Stop rather than retry early or extend the maximum retry delay.
		if time.Until(retryAfter) > backoffCap {
			break
		}
		if deadline, ok := ctx.Deadline(); ok && !retryAfter.Before(deadline) {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, lastErr
}

// limitedWriter limits the total bytes written to its underlying [io.Writer]. A
// positive maxBytes enables the limit.
type limitedWriter struct {
	io.Writer
	maxBytes int64
	written  int64
}

// Write implements [io.Writer].
func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.maxBytes > 0 && int64(len(p)) > w.maxBytes-w.written {
		return 0, fmt.Errorf("%w: response body exceeds %d bytes", errBadUpstream, w.maxBytes)
	}
	n, err := w.Writer.Write(p)
	w.written += int64(n)
	return n, err
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
// in tempDir. A positive maxBytes limits the file size.
func httpGetTemp(ctx context.Context, client *http.Client, url, tempDir string, maxBytes int64) (tempFile string, header http.Header, err error) {
	f, err := os.CreateTemp(tempDir, "")
	if err != nil {
		return "", nil, &internalError{err: err}
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	header, err = httpGet(ctx, client, url, &limitedWriter{Writer: httpGetTempWriter{f}, maxBytes: maxBytes})
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, http.ErrSchemeMismatch) {
		return false
	}
	if e, ok := errors.AsType[*net.DNSError](err); ok && e.IsNotFound {
		return false
	}
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return false
	}
	if _, ok := errors.AsType[x509.SystemRootsError](err); ok {
		return false
	}
	if _, ok := errors.AsType[x509.CertificateInvalidError](err); ok {
		return false
	}
	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return false
	}
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return false
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

// parseHTTPRetryAfter parses an HTTP Retry-After value relative to now,
// returning the zero time for invalid values.
func parseHTTPRetryAfter(value string, now time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			t, _ := http.ParseTime(value)
			return t
		}
	}
	// Saturate large delays to avoid overflowing and retrying early.
	seconds, _ := strconv.ParseUint(value, 10, 64)
	return now.Add(time.Duration(min(seconds, math.MaxInt64/uint64(time.Second))) * time.Second)
}
