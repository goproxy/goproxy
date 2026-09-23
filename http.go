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

// httpGet gets the content from the given url and writes it to the dst.
func httpGet(ctx context.Context, client *http.Client, url string, dst io.Writer) error {
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
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			if isRetryableHTTPClientDoError(err) {
				lastErr = err
				continue
			}
			return err
		}
		if resp.StatusCode == http.StatusOK {
			if dst != nil {
				_, err = io.Copy(dst, resp.Body)
			}
			resp.Body.Close()
			return err
		}

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize+1))
		resp.Body.Close()
		if err != nil {
			return err
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
				return &uncacheableError{err: err}
			}
			return err
		case http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable:
			lastErr = errBadUpstream
		case http.StatusGatewayTimeout:
			lastErr = errFetchTimedOut
		default:
			return fmt.Errorf("GET %s: %s: %s", resp.Request.URL.Redacted(), resp.Status, respBody)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return lastErr
}

// httpGetTemp is like [httpGet] but writes the content to a new temporary file
// in tempDir.
func httpGetTemp(ctx context.Context, client *http.Client, url, tempDir string) (tempFile string, err error) {
	f, err := os.CreateTemp(tempDir, "")
	if err != nil {
		return "", &internalError{err: err}
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	if err := httpGet(ctx, client, url, f); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", &internalError{err: err}
	}
	return f.Name(), nil
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

// isCacheRestrictedHTTPResponse reports whether the response headers contain
// cache restrictions. It checks Cache-Control for "no-store", "no-cache", or
// "private", and Vary for "*".
func isCacheRestrictedHTTPResponse(header http.Header) bool {
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
		name, _, _ := strings.Cut(value[:end], "=")
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "no-store", "no-cache", "private":
			return true
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
