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
	key        string
	url        *url.URL
	envGOPROXY string
	envGOSUMDB string
	httpClient *http.Client
}

// init initializes the sco.
func (sco *sumdbClientOps) init() {
	var (
		name        string
		isDirectURL bool
	)
	name, sco.key, sco.url, isDirectURL, sco.initError = parseEnvGOSUMDB(sco.envGOSUMDB)
	if sco.initError != nil {
		return
	}
	if isDirectURL {
		if err := walkEnvGOPROXY(sco.envGOPROXY, func(proxy *url.URL) error {
			u := appendURL(proxy, "sumdb", name)
			if err := httpGet(context.Background(), sco.httpClient, appendURL(u, "/supported").String(), nil); err != nil {
				return err
			}
			sco.url = u
			return nil
		}, func() error {
			return nil
		}, func() error {
			return nil
		}); err != nil && !errors.Is(err, fs.ErrNotExist) {
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
	if err := httpGet(context.Background(), sco.httpClient, appendURL(sco.url, path).String(), &buf); err != nil {
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
		return []byte(sco.key), nil
	} else if strings.HasSuffix(file, "/latest") {
		return []byte{}, nil // Empty result means empty tree.
	}
	return nil, fmt.Errorf("unknown config %s", file)
}

// WriteConfig implements [golang.org/x/mod/sumdb.ClientOps].
func (*sumdbClientOps) WriteConfig(file string, old, new []byte) error { return nil }

// ReadCache implements [golang.org/x/mod/sumdb.ClientOps].
func (*sumdbClientOps) ReadCache(file string) ([]byte, error) { return nil, fs.ErrNotExist }

// WriteCache implements [golang.org/x/mod/sumdb.ClientOps].
func (*sumdbClientOps) WriteCache(file string, data []byte) {}

// Log implements [golang.org/x/mod/sumdb.ClientOps].
func (*sumdbClientOps) Log(msg string) {}

// SecurityError implements [golang.org/x/mod/sumdb.ClientOps].
func (*sumdbClientOps) SecurityError(msg string) {}
