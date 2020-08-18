package goproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

var (
	// errNotFound means something was not found.
	errNotFound = errors.New("not found")

	// errBadUpstream means an upstream is bad.
	errBadUpstream = errors.New("bad upstream")

	// errFetchTimedOut means a fetch operation has timed out.
	errFetchTimedOut = errors.New("fetch timed out")
)

// notFoundError is an error indicating that something was not found.
type notFoundError struct {
	error
}

// Is reports whether the err is `errNotFound`.
func (notFoundError) Is(err error) bool {
	return err == errNotFound
}

// httpGet gets the content targeted by the url into the dst.
func httpGet(
	ctx context.Context,
	httpClient *http.Client,
	url string,
	dst io.Writer,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusOK {
		if dst != nil {
			_, err := io.Copy(dst, res.Body)
			return err
		}

		return nil
	}

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}

	switch res.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusGone:
		return &notFoundError{errors.New(string(b))}
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return errBadUpstream
	case http.StatusGatewayTimeout:
		return errFetchTimedOut
	}

	return fmt.Errorf("GET %s: %s: %s", redactedURL(req.URL), res.Status, b)
}

// parseRawURL parses the rawURL.
func parseRawURL(rawURL string) (*url.URL, error) {
	if strings.ContainsAny(rawURL, ".:/") &&
		!strings.Contains(rawURL, ":/") &&
		!filepath.IsAbs(rawURL) &&
		!path.IsAbs(rawURL) {
		rawURL = fmt.Sprint("https://", rawURL)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf(
			"invalid URL scheme (must be http or https): %s",
			redactedURL(u),
		)
	}

	return u, nil
}

// appendURL appends the extraPaths to the u safely and reutrns a new instance
// of the `url.URL`.
func appendURL(u *url.URL, extraPaths ...string) *url.URL {
	nu := *u
	u = &nu
	for _, ep := range extraPaths {
		if ep == "" {
			continue
		}

		u.Path = path.Join(u.Path, ep)
		u.RawPath = path.Join(
			u.RawPath,
			strings.Replace(url.PathEscape(ep), "%2F", "/", -1),
		)
		if ep[len(ep)-1] == '/' {
			u.Path = fmt.Sprint(u.Path, "/")
			u.RawPath = fmt.Sprint(u.RawPath, "/")
		}
	}

	return u
}

// redactedURL returns a redacted string form of the u, suitable for printing in
// error messages. The string form replaces any non-empty password in the u with
// "xxxxx".
//
// TODO: Remove the `redactedURL` when the minimum supported Go version is 1.15.
// See https://golang.org/doc/go1.15#net/url.
func redactedURL(u *url.URL) string {
	if _, ok := u.User.Password(); ok {
		ru := *u
		u = &ru
		u.User = url.UserPassword(u.User.Username(), "xxxxx")
	}

	return u.String()
}
