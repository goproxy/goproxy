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

// DiskCacher implements the `Cacher` by using the disk.
type DiskCacher struct {
	// Root is the root of the caches.
	//
	// If the `Root` is empty, the `os.TempDir` is used.
	//
	// Note that the `Root` must be a UNIX-style path.
	Root string
}

// Get implements the `Cacher`.
func (dc *DiskCacher) Get(ctx context.Context, name string) (Cache, error) {
	file, err := os.Open(dc.filename(name))
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

	return &diskCache{
		file:    file,
		name:    name,
		modTime: fileInfo.ModTime(),
	}, nil
}

// Set implements the `Cacher`.
func (dc *DiskCacher) Set(ctx context.Context, name string, r io.Reader) error {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}

	filename := dc.filename(name)
	if err := os.MkdirAll(
		filepath.Dir(filename),
		os.ModePerm,
	); err != nil {
		return err
	}

	return ioutil.WriteFile(filename, b, os.ModePerm)
}

// filename returns the disk file representation of the name.
func (dc *DiskCacher) filename(name string) string {
	name = filepath.FromSlash(name)
	if dc.Root != "" {
		return filepath.Join(filepath.FromSlash(dc.Root), name)
	}

	return filepath.Join(os.TempDir(), name)
}

// diskCache implements the `Cache`. It is the cache unit of the `DiskCacher`.
type diskCache struct {
	file    *os.File
	name    string
	modTime time.Time
}

// Read implements the `Cache`.
func (dc *diskCache) Read(b []byte) (int, error) {
	return dc.file.Read(b)
}

// Seek implements the `Cache`.
func (dc *diskCache) Seek(offset int64, whence int) (int64, error) {
	return dc.file.Seek(offset, whence)
}

// Close implements the `Cache`.
func (dc *diskCache) Close() error {
	return dc.file.Close()
}

// Name implements the `Cache`.
func (dc *diskCache) Name() string {
	return dc.name
}

// ModTime implements the `Cache`.
func (dc *diskCache) ModTime() time.Time {
	return dc.modTime
}
