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

// sumdbClientOps implements [golang.org/x/mod/sumdb.ClientOps].
type sumdbClientOps struct {
	initOnce   sync.Once
	initError  error
	key        []byte
	endpoint   *url.URL
	envGOPROXY string
	envGOSUMDB string
	httpClient *http.Client
}

// init initializes the sco.
func (sco *sumdbClientOps) init() {
	var (
		name             string
		isDirectEndpoint bool
	)
	name, sco.key, sco.endpoint, isDirectEndpoint, sco.initError = parseEnvGOSUMDB(sco.envGOSUMDB)
	if sco.initError != nil {
		return
	}
	if isDirectEndpoint {
		if err := walkEnvGOPROXY(sco.envGOPROXY, func(proxy string) error {
			proxyURL, err := parseRawURL(proxy)
			if err != nil {
				return err
			}
			proxiedEndpoint := appendURL(proxyURL, "sumdb", name)
			if err := httpGet(context.Background(), sco.httpClient, appendURL(proxiedEndpoint, "/supported").String(), nil); err != nil {
				return err
			}
			sco.endpoint = proxiedEndpoint
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
}

// ReadRemote implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) ReadRemote(path string) ([]byte, error) {
	if sco.initOnce.Do(sco.init); sco.initError != nil {
		return nil, sco.initError
	}
	var buf bytes.Buffer
	if err := httpGet(context.Background(), sco.httpClient, appendURL(sco.endpoint, path).String(), &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ReadConfig implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) ReadConfig(file string) ([]byte, error) {
	if sco.initOnce.Do(sco.init); sco.initError != nil {
		return nil, sco.initError
	}
	if file == "key" {
		return sco.key, nil
	} else if strings.HasSuffix(file, "/latest") {
		return []byte{}, nil // Empty result means empty tree.
	}
	return nil, fmt.Errorf("unknown config %s", file)
}

// WriteConfig implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) WriteConfig(file string, old, new []byte) error {
	sco.initOnce.Do(sco.init)
	return sco.initError
}

// ReadCache implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) ReadCache(file string) ([]byte, error) {
	if sco.initOnce.Do(sco.init); sco.initError != nil {
		return nil, sco.initError
	}
	return nil, fs.ErrNotExist
}

// WriteCache implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) WriteCache(file string, data []byte) {
	sco.initOnce.Do(sco.init)
}

// Log implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) Log(msg string) {
	sco.initOnce.Do(sco.init)
}

// SecurityError implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) SecurityError(msg string) {
	sco.initOnce.Do(sco.init)
}
