package goproxy

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var (
	// errNotFound means something was not found.
	errNotFound = notFoundError("not found")

	// errBadUpstream means an upstream is bad.
	errBadUpstream = errors.New("bad upstream")

	// errFetchTimedOut means a fetch operation has timed out.
	errFetchTimedOut = errors.New("fetch timed out")
)

// notFoundError is an error indicating that something was not found.
type notFoundError string

// Error implements the error.
func (nfe notFoundError) Error() string {
	return string(nfe)
}

// Is reports whether the target is [errNotFound] or [fs.ErrNotExist].
func (notFoundError) Is(target error) bool {
	switch target {
	case errNotFound, fs.ErrNotExist:
		return true
	}
	return false
}

// httpGet gets the content targeted by the url into the dst.
func httpGet(ctx context.Context, client *http.Client, url string, dst io.Writer) error {
	var lastError error
	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(backoffSleep(100*time.Millisecond, time.Second, attempt)):
			case <-ctx.Done():
				return lastError
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			if isRetryableHTTPClientDoError(err) {
				lastError = err
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
			return notFoundError(respBody)
		case http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable:
			lastError = errBadUpstream
		case http.StatusGatewayTimeout:
			lastError = errFetchTimedOut
		default:
			return fmt.Errorf("GET %s: %s: %s", resp.Request.URL.Redacted(), resp.Status, respBody)
		}
	}
	return lastError
}

// isRetryableHTTPClientDoError reports whether the err is a retryable error
// returned by the [http.Client.Do].
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
		switch e.Error() {
		case "http: server gave HTTP response to HTTPS client":
			return false
		}
	}
	return true
}

// parseRawURL parses the rawURL.
func parseRawURL(rawURL string) (*url.URL, error) {
	if strings.ContainsAny(rawURL, ".:/") &&
		!strings.Contains(rawURL, ":/") &&
		!filepath.IsAbs(rawURL) &&
		!path.IsAbs(rawURL) {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// appendURL appends the extraPaths to the u safely and reutrns a new instance
// of the [url.URL].
//
// TODO: Remove the appendURL when the minimum supported Go version is 1.19. See
// https://go.dev/doc/go1.19#net/url.
func appendURL(u *url.URL, extraPaths ...string) *url.URL {
	nu := *u
	u = &nu
	for _, ep := range extraPaths {
		if ep == "" {
			continue
		}
		u.Path = path.Join(u.Path, ep)
		u.RawPath = path.Join(u.RawPath, strings.ReplaceAll(url.PathEscape(ep), "%2F", "/"))
		if ep[len(ep)-1] == '/' {
			u.Path += "/"
			u.RawPath += "/"
		}
	}
	return u
}
