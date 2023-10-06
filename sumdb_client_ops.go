package goproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// sumdbClientOps implements the [golang.org/x/mod/sumdb.ClientOps].
type sumdbClientOps struct {
	initOnce    sync.Once
	initError   error
	key         []byte
	endpointURL *url.URL
	envGOPROXY  string
	envGOSUMDB  string
	httpClient  *http.Client
}

// init initializes the sco.
func (sco *sumdbClientOps) init() {
	sumdbParts := strings.Fields(sco.envGOSUMDB)
	if l := len(sumdbParts); l == 0 {
		sco.initError = errors.New("missing GOSUMDB")
		return
	} else if l > 2 {
		sco.initError = errors.New("invalid GOSUMDB: too many fields")
		return
	}

	if sumdbParts[0] == "sum.golang.google.cn" {
		sumdbParts[0] = "sum.golang.org"
		if len(sumdbParts) == 1 {
			sumdbParts = append(
				sumdbParts,
				"https://sum.golang.google.cn",
			)
		}
	}

	if sumdbParts[0] == "sum.golang.org" {
		sumdbParts[0] = "sum.golang.org" +
			"+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8"
	}

	sco.key = []byte(sumdbParts[0])

	sumdbName := sumdbParts[0]
	if i := strings.Index(sumdbName, "+"); i >= 0 {
		sumdbName = sumdbName[:i]
	}

	if len(sumdbParts) == 1 {
		sumdbParts = append(sumdbParts, sumdbName)
	}

	sco.endpointURL, sco.initError = parseRawURL(sumdbParts[1])
	if sco.initError != nil {
		return
	}

	if err := walkGOPROXY(sco.envGOPROXY, func(proxy string) error {
		proxyURL, err := parseRawURL(proxy)
		if err != nil {
			return err
		}

		endpointURL := appendURL(proxyURL, "sumdb", sumdbName)

		if err := httpGet(
			context.Background(),
			sco.httpClient,
			appendURL(endpointURL, "/supported").String(),
			nil,
		); err != nil {
			return err
		}

		sco.endpointURL = endpointURL

		return nil
	}, func() error {
		return nil
	}, func() error {
		return nil
	}); err != nil && !errors.Is(err, errNotFound) {
		sco.initError = err
		return
	}
}

// ReadRemote implements the [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) ReadRemote(path string) ([]byte, error) {
	if sco.initOnce.Do(sco.init); sco.initError != nil {
		return nil, sco.initError
	}

	var buf bytes.Buffer
	if err := httpGet(
		context.Background(),
		sco.httpClient,
		appendURL(sco.endpointURL, path).String(),
		&buf,
	); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ReadConfig implements the [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) ReadConfig(file string) ([]byte, error) {
	if sco.initOnce.Do(sco.init); sco.initError != nil {
		return nil, sco.initError
	}

	if file == "key" {
		return sco.key, nil
	} else if strings.HasSuffix(file, "/latest") {
		return []byte{}, nil // Empty result means empty tree
	}

	return nil, fmt.Errorf("unknown config %s", file)
}

// WriteConfig implements the [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) WriteConfig(file string, old, new []byte) error {
	sco.initOnce.Do(sco.init)
	return sco.initError
}

// ReadCache implements the [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) ReadCache(file string) ([]byte, error) {
	if sco.initOnce.Do(sco.init); sco.initError != nil {
		return nil, sco.initError
	}

	return nil, fs.ErrNotExist
}

// WriteCache implements the [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) WriteCache(file string, data []byte) {
	sco.initOnce.Do(sco.init)
}

// Log implements the [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) Log(msg string) {
	sco.initOnce.Do(sco.init)
}

// SecurityError implements the [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) SecurityError(msg string) {
	sco.initOnce.Do(sco.init)
}
