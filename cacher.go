package goproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"time"
)

// ErrCacheNotFound is the error resulting if a path search failed to find a
// cache.
var ErrCacheNotFound = errors.New("cache not found")

// Cacher is the interface that defines a set of methods used to cache module
// files for the `Goproxy`.
//
// Note that the cache names must be UNIX-style paths.
//
// If you are looking for some useful implementations of the `Cacher`, simply
// visit the "github.com/goproxy/goproxy/cachers" package.
type Cacher interface {
	// Get gets a `Cache` targeted by the name from the underlying cacher.
	//
	// The `ErrCacheNotFound` must be returned if the target cache cannot be
	// found.
	Get(ctx context.Context, name string) (Cache, error)

	// Set sets the r to the underlying cacher with the name.
	Set(ctx context.Context, name string, r io.Reader) error
}

// Cache is the cache unit of the `Cacher`.
type Cache interface {
	io.Reader
	io.Seeker
	io.Closer

	// Name returns the name of the underlying cache.
	//
	// Note that the returned name must be a UNIX-style path.
	Name() string

	// ModTime returns the modification time of the underlying cache.
	ModTime() time.Time
}

// mapCacher implements the `Cacher` by using the `map[string]mapCache`.
type mapCacher struct {
	caches map[string]*mapCache
}

// Get implements the `Cacher`.
func (mc *mapCacher) Get(ctx context.Context, name string) (Cache, error) {
	c, ok := mc.caches[name]
	if !ok {
		return nil, ErrCacheNotFound
	}

	return c, nil
}

// Set implements the `Cacher`.
func (mc *mapCacher) Set(ctx context.Context, name string, r io.Reader) error {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}

	mc.caches[name] = &mapCache{
		reader:  bytes.NewReader(b),
		name:    name,
		modTime: time.Now(),
	}

	return nil
}

// mapCache implements the `Cache`. It is the cache unit of the `mapCacher`.
type mapCache struct {
	reader  *bytes.Reader
	name    string
	modTime time.Time
}

// Read implements the `Cache`.
func (mc *mapCache) Read(b []byte) (int, error) {
	return mc.reader.Read(b)
}

// Seek implements the `Cache`.
func (mc *mapCache) Seek(offset int64, whence int) (int64, error) {
	return mc.reader.Seek(offset, whence)
}

// Close implements the `Cache`.
func (mc *mapCache) Close() error {
	return nil
}

// Name implements the `Cache`.
func (mc *mapCache) Name() string {
	return mc.name
}

// ModTime implements the `Cache`.
func (mc *mapCache) ModTime() time.Time {
	return mc.modTime
}
