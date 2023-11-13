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
	name              string
	key               string
	directURL         *url.URL
	urlValue          atomic.Value
	urlDetermineMutex sync.Mutex
	urlDeterminedAt   time.Time
	urlDetermineErr   error
	envGOPROXY        string
	httpClient        *http.Client
}

// newSumdbClientOps creates a new [sumdbClientOps].
func newSumdbClientOps(envGOPROXY, envGOSUMDB string, httpClient *http.Client) (*sumdbClientOps, error) {
	var (
		sco         = &sumdbClientOps{envGOPROXY: envGOPROXY, httpClient: httpClient}
		u           *url.URL
		isDirectURL bool
		err         error
	)
	sco.name, sco.key, u, isDirectURL, err = parseEnvGOSUMDB(envGOSUMDB)
	if err != nil {
		return nil, err
	}
	if isDirectURL {
		sco.directURL = u
	} else {
		sco.urlValue.Store(u)
	}
	return sco, nil
}

// url returns the URL for connecting to the checksum database.
func (sco *sumdbClientOps) url() (*url.URL, error) {
	if v := sco.urlValue.Load(); v != nil {
		return v.(*url.URL), nil
	}

	sco.urlDetermineMutex.Lock()
	defer sco.urlDetermineMutex.Unlock()
	if time.Since(sco.urlDeterminedAt) < 10*time.Second && sco.urlDetermineErr != nil {
		return nil, sco.urlDetermineErr
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
		sco.urlDetermineErr = err
		return nil, err
	}
	sco.urlDetermineErr = nil

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
