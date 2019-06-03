package goproxy

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

// ErrCacheNotFound is the error resulting if a path search failed to find a
// cache.
var ErrCacheNotFound = errors.New("cache not found")

// Cacher is the interface that defines a set of methods used to cache module
// files for the `Goproxy`.
//
// Note that the cache names must be UNIX-style paths.
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

// LocalCacher implements the `Cacher` by using the local disk.
type LocalCacher struct {
	// Root is the root of the caches.
	//
	// If the `Root` is empty, the `os.TempDir` is used.
	//
	// Note that the `Root` must be a UNIX-style path.
	Root string
}

// Get implements the `Cacher`.
func (c *LocalCacher) Get(ctx context.Context, name string) (Cache, error) {
	file, err := os.Open(c.localName(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCacheNotFound
		}

		return nil, err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	return &LocalCache{
		file:    file,
		name:    name,
		modTime: fileInfo.ModTime(),
	}, nil
}

// Set implements the `Cacher`.
func (c *LocalCacher) Set(ctx context.Context, name string, r io.Reader) error {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}

	localName := c.localName(name)
	if err := os.MkdirAll(
		filepath.Dir(localName),
		os.ModePerm,
	); err != nil {
		return err
	}

	return ioutil.WriteFile(localName, b, os.ModePerm)
}

// localName returns the local representation of the name.
func (c *LocalCacher) localName(name string) string {
	name = filepath.FromSlash(name)
	if c.Root != "" {
		return filepath.Join(filepath.FromSlash(c.Root), name)
	}

	return filepath.Join(os.TempDir(), name)
}

// LocalCache implements the `Cache` by using the local disk.
type LocalCache struct {
	file    *os.File
	name    string
	modTime time.Time
}

// Read implements the `Cache`.
func (c *LocalCache) Read(b []byte) (int, error) {
	return c.file.Read(b)
}

// Seek implements the `Cache`.
func (c *LocalCache) Seek(offset int64, whence int) (int64, error) {
	return c.file.Seek(offset, whence)
}

// Close implements the `Cache`.
func (c *LocalCache) Close() error {
	return c.file.Close()
}

// Name implements the `Cache`.
func (c *LocalCache) Name() string {
	return c.name
}

// ModTime implements the `Cache`.
func (c *LocalCache) ModTime() time.Time {
	return c.modTime
}
