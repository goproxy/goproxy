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
	"sync/atomic"
	"time"
)

// sumdbClientOps implements [golang.org/x/mod/sumdb.ClientOps].
type sumdbClientOps struct {
	initOnce          sync.Once
	initError         error
	name              string
	key               string
	directURL         *url.URL
	urlValue          atomic.Value
	urlDetermineMutex sync.Mutex
	urlDeterminedAt   time.Time
	urlDetermineError error
	envGOPROXY        string
	envGOSUMDB        string
	httpClient        *http.Client
}

// init initializes the sco.
func (sco *sumdbClientOps) init() {
	var isDirectURL bool
	sco.name, sco.key, sco.directURL, isDirectURL, sco.initError = parseEnvGOSUMDB(sco.envGOSUMDB)
	if sco.initError != nil {
		return
	}
	if !isDirectURL {
		sco.urlValue.Store(sco.directURL)
		sco.directURL = nil
	}
}

// url returns the URL for connecting to the checksum database.
func (sco *sumdbClientOps) url() (*url.URL, error) {
	if sco.initOnce.Do(sco.init); sco.initError != nil {
		return nil, sco.initError
	}
	if v := sco.urlValue.Load(); v != nil {
		return v.(*url.URL), nil
	}
	sco.urlDetermineMutex.Lock()
	defer sco.urlDetermineMutex.Unlock()
	if time.Since(sco.urlDeterminedAt) < 10*time.Second && sco.urlDetermineError != nil {
		return nil, sco.urlDetermineError
	}
	u := sco.directURL
	err := walkEnvGOPROXY(sco.envGOPROXY, func(proxy *url.URL) error {
		pu := appendURL(proxy, "sumdb", sco.name)
		if err := httpGet(context.Background(), sco.httpClient, appendURL(pu, "/supported").String(), nil); err != nil {
			return err
		}
		u = pu
		return nil
	}, func() error { return nil })
	sco.urlDeterminedAt = time.Now()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		sco.urlDetermineError = err
		return nil, err
	}
	sco.urlDetermineError = nil
	sco.urlValue.Store(u)
	return u, nil
}

// ReadRemote implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) ReadRemote(path string) ([]byte, error) {
	u, err := sco.url()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := httpGet(context.Background(), sco.httpClient, appendURL(u, path).String(), &buf); err != nil {
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
	}
	if strings.HasSuffix(file, "/latest") {
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
