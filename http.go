package goproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
)

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

	if res.StatusCode != http.StatusOK {
		b, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return err
		}

		switch res.StatusCode {
		case http.StatusNotFound, http.StatusGone:
			return &notFoundError{errors.New(string(b))}
		}

		return fmt.Errorf(
			"GET %s: %s: %s",
			redactedURL(req.URL),
			res.Status,
			b,
		)
	}

	if dst != nil {
		_, err := io.Copy(dst, res.Body)
		return err
	}

	return nil
}
