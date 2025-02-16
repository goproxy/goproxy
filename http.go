package goproxy

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"time"
)

var (
	// errBadUpstream indicates an upstream is in a bad state.
	errBadUpstream = errors.New("bad upstream")

	// errFetchTimedOut indicates a fetch operation has timed out.
	errFetchTimedOut = errors.New("fetch timed out")
)

// notExistError is like [fs.ErrNotExist] but with a custom underlying error.
//
// NOTE: Do not use [notExistError] directly, use [notExistErrorf] instead.
type notExistError struct{ err error }

// Error implements [error].
func (e *notExistError) Error() string { return e.err.Error() }

// Unwrap returns the underlying error.
func (e *notExistError) Unwrap() error { return e.err }

// Is reports whether the target is [fs.ErrNotExist].
func (notExistError) Is(target error) bool { return target == fs.ErrNotExist }

// notExistErrorf formats according to a format specifier and returns the string
// as a value that satisfies error that is equivalent to [fs.ErrNotExist].
func notExistErrorf(format string, v ...interface{}) error {
	return &notExistError{err: fmt.Errorf(format, v...)}
}

// httpGet gets the content from the given url and writes it to the dst.
func httpGet(ctx context.Context, client *http.Client, url string, dst io.Writer) error {
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(backoffSleep(100*time.Millisecond, time.Second, attempt)):
			case <-ctx.Done():
				return lastErr
			}
		}

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

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		switch resp.StatusCode {
		case http.StatusBadRequest,
			http.StatusNotFound,
			http.StatusGone:
			return notExistErrorf("%s", respBody)
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
	return lastErr
}

// httpGetTemp is like [httpGet] but writes the content to a new temporary file
// in tempDir.
func httpGetTemp(ctx context.Context, client *http.Client, url, tempDir string) (tempFile string, err error) {
	f, err := os.CreateTemp(tempDir, "")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	if err := httpGet(ctx, client, url, f); err != nil {
		return "", err
	}
	return f.Name(), f.Close()
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

// backoffSleep computes the exponential backoff sleep duration based on the
// algorithm described in https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/.
func backoffSleep(base, cap time.Duration, attempt int) time.Duration {
	var pow time.Duration
	if attempt < 63 {
		pow = 1 << attempt
	} else {
		pow = math.MaxInt64
	}

	sleep := base * pow
	if sleep > cap || sleep/pow != base {
		sleep = cap
	}
	sleep = rand.N(sleep)
	return sleep
}
