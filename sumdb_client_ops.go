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

// sumdbClientOps implements [sumdb.ClientOps] for one download. It buffers
// cache writes until checksum verification succeeds.
type sumdbClientOps struct {
	*sumdbClientState
	ctx   context.Context
	cache sync.Map
}

// ReadRemote implements [sumdb.ClientOps].
func (sco *sumdbClientOps) ReadRemote(path string) ([]byte, error) {
	return sco.sumdbClientState.ReadRemote(sco.ctx, path)
}

// ReadCache implements [sumdb.ClientOps].
func (sco *sumdbClientOps) ReadCache(file string) ([]byte, error) {
	if data, ok := sco.cache.Load(file); ok {
		return bytes.Clone(data.([]byte)), nil
	}
	return sco.sumdbClientState.ReadCache(file)
}

// WriteCache implements [sumdb.ClientOps].
func (sco *sumdbClientOps) WriteCache(file string, data []byte) {
	sco.cache.Store(file, bytes.Clone(data))
}

// saveCache publishes the buffered cache writes.
func (sco *sumdbClientOps) saveCache() {
	sco.cache.Range(func(file, data any) bool {
		sco.sumdbClientState.WriteCache(file.(string), data.([]byte))
		return true
	})
}

// sumdbClientState holds shared checksum database client state.
type sumdbClientState struct {
	name            string
	key             string
	directURL       *url.URL
	urlLock         chan struct{}
	urlValue        *url.URL
	urlDeterminedAt time.Time
	urlDetermineErr error
	latestMu        sync.Mutex
	latest          []byte
	cache           sync.Map
	envGOPROXY      string
	httpClient      *http.Client
}

// newSumdbClientState creates a new [sumdbClientState].
func newSumdbClientState(envGOPROXY, envGOSUMDB string, httpClient *http.Client) (*sumdbClientState, error) {
	var (
		scs = &sumdbClientState{
			urlLock:    make(chan struct{}, 1),
			envGOPROXY: envGOPROXY,
			httpClient: httpClient,
		}

		u           *url.URL
		isDirectURL bool
		err         error
	)
	scs.name, scs.key, u, isDirectURL, err = parseEnvGOSUMDB(envGOSUMDB)
	if err != nil {
		return nil, err
	}
	if isDirectURL {
		scs.directURL = u
	} else {
		scs.urlValue = u
	}
	return scs, nil
}

// url returns the URL for connecting to the checksum database.
func (scs *sumdbClientState) url(ctx context.Context) (*url.URL, error) {
	select {
	case scs.urlLock <- struct{}{}:
		defer func() { <-scs.urlLock }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if scs.urlValue != nil {
		return scs.urlValue, nil
	}
	if time.Since(scs.urlDeterminedAt) < 10*time.Second && scs.urlDetermineErr != nil {
		return nil, scs.urlDetermineErr
	}

	u := scs.directURL
	err := walkEnvGOPROXY(scs.envGOPROXY, func(proxy *url.URL) error {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		pu := proxy.JoinPath("sumdb", scs.name)
		if _, err := httpGet(ctx, scs.httpClient, pu.JoinPath("/supported").String(), nil); err != nil {
			return err
		}
		u = pu
		return nil
	}, func() error { return nil })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scs.urlDeterminedAt = time.Now()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		scs.urlDetermineErr = err
		return nil, err
	}
	scs.urlDetermineErr = nil

	scs.urlValue = u
	return u, nil
}

// ReadRemote reads the content at the given path from the checksum database.
func (scs *sumdbClientState) ReadRemote(ctx context.Context, path string) ([]byte, error) {
	u, err := scs.url(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var buf bytes.Buffer
	if _, err := httpGet(ctx, scs.httpClient, u.JoinPath(path).String(), &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ReadConfig implements [sumdb.ClientOps].
func (scs *sumdbClientState) ReadConfig(file string) ([]byte, error) {
	if file == "key" {
		return []byte(scs.key), nil
	}
	if strings.HasSuffix(file, "/latest") {
		scs.latestMu.Lock()
		defer scs.latestMu.Unlock()
		return bytes.Clone(scs.latest), nil
	}
	return nil, fmt.Errorf("unknown config %s", file)
}

// WriteConfig implements [sumdb.ClientOps].
func (scs *sumdbClientState) WriteConfig(_ string, old, new []byte) error {
	scs.latestMu.Lock()
	defer scs.latestMu.Unlock()
	if !bytes.Equal(scs.latest, old) {
		return sumdb.ErrWriteConflict
	}
	scs.latest = bytes.Clone(new)
	return nil
}

// ReadCache implements [sumdb.ClientOps].
func (scs *sumdbClientState) ReadCache(file string) ([]byte, error) {
	data, ok := scs.cache.Load(file)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return bytes.Clone(data.([]byte)), nil
}

// WriteCache implements [sumdb.ClientOps].
func (scs *sumdbClientState) WriteCache(file string, data []byte) {
	scs.cache.Store(file, bytes.Clone(data))
}

// Log implements [sumdb.ClientOps].
func (*sumdbClientState) Log(msg string) {}

// SecurityError implements [sumdb.ClientOps].
func (*sumdbClientState) SecurityError(msg string) {}
