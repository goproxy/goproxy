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
	"time"

	"golang.org/x/mod/sumdb"
)

// sumdbClientOps implements [golang.org/x/mod/sumdb.ClientOps].
type sumdbClientOps struct {
	name            string
	key             string
	directURL       *url.URL
	urlMu           sync.Mutex
	urlValue        *url.URL
	urlDeterminedAt time.Time
	urlDetermineErr error
	latestMu        sync.Mutex
	latest          []byte
	cache           sync.Map
	envGOPROXY      string
	httpClient      *http.Client
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
		sco.urlValue = u
	}
	return sco, nil
}

// url returns the URL for connecting to the checksum database.
func (sco *sumdbClientOps) url() (*url.URL, error) {
	sco.urlMu.Lock()
	defer sco.urlMu.Unlock()

	if sco.urlValue != nil {
		return sco.urlValue, nil
	}
	if time.Since(sco.urlDeterminedAt) < 10*time.Second && sco.urlDetermineErr != nil {
		return nil, sco.urlDetermineErr
	}

	u := sco.directURL
	err := walkEnvGOPROXY(sco.envGOPROXY, func(proxy *url.URL) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		pu := proxy.JoinPath("sumdb", sco.name)
		if _, err := httpGet(ctx, sco.httpClient, pu.JoinPath("/supported").String(), nil); err != nil {
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

	sco.urlValue = u
	return u, nil
}

// ReadRemote implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) ReadRemote(path string) ([]byte, error) {
	u, err := sco.url()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var buf bytes.Buffer
	if _, err := httpGet(ctx, sco.httpClient, u.JoinPath(path).String(), &buf); err != nil {
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
		sco.latestMu.Lock()
		defer sco.latestMu.Unlock()
		return bytes.Clone(sco.latest), nil
	}
	return nil, fmt.Errorf("unknown config %s", file)
}

// WriteConfig implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) WriteConfig(_ string, old, new []byte) error {
	sco.latestMu.Lock()
	defer sco.latestMu.Unlock()
	if !bytes.Equal(sco.latest, old) {
		return sumdb.ErrWriteConflict
	}
	sco.latest = bytes.Clone(new)
	return nil
}

// ReadCache implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) ReadCache(file string) ([]byte, error) {
	data, ok := sco.cache.Load(file)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return bytes.Clone(data.([]byte)), nil
}

// WriteCache implements [golang.org/x/mod/sumdb.ClientOps].
func (sco *sumdbClientOps) WriteCache(file string, data []byte) {
	sco.cache.Store(file, bytes.Clone(data))
}

// Log implements [golang.org/x/mod/sumdb.ClientOps].
func (*sumdbClientOps) Log(msg string) {}

// SecurityError implements [golang.org/x/mod/sumdb.ClientOps].
func (*sumdbClientOps) SecurityError(msg string) {}

// sumdbClientOpsCache buffers cache writes until checksum verification succeeds.
type sumdbClientOpsCache struct {
	*sumdbClientOps
	cache sync.Map
}

// ReadCache implements [golang.org/x/mod/sumdb.ClientOps].
func (scoc *sumdbClientOpsCache) ReadCache(file string) ([]byte, error) {
	if data, ok := scoc.cache.Load(file); ok {
		return bytes.Clone(data.([]byte)), nil
	}
	return scoc.sumdbClientOps.ReadCache(file)
}

// WriteCache implements [golang.org/x/mod/sumdb.ClientOps].
func (scoc *sumdbClientOpsCache) WriteCache(file string, data []byte) {
	scoc.cache.Store(file, bytes.Clone(data))
}

// save publishes the buffered cache writes.
func (scoc *sumdbClientOpsCache) save() {
	scoc.cache.Range(func(file, data any) bool {
		scoc.sumdbClientOps.WriteCache(file.(string), data.([]byte))
		return true
	})
}
