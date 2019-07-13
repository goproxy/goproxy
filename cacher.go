package goproxy

import (
	"context"
	"crypto/md5"
	"errors"
	"hash"
	"io"
	"os"
	"time"
)

// ErrCacheNotFound is the error resulting if a path search failed to find a
// cache.
var ErrCacheNotFound = errors.New("cache not found")

// Cacher is the interface that defines a set of methods used to cache module
// files for the `Goproxy`.
//
// If you are looking for some useful implementations of the `Cacher`, simply
// visit the "github.com/goproxy/goproxy/cacher" package.
type Cacher interface {
	// NewHash returns a new instance of the `hash.Hash` used to compute the
	// checksums of the caches in the underlying cacher.
	NewHash() hash.Hash

	// Cache returns the matched `Cache` for the name from the underlying
	// cacher. It returns the `ErrCacheNotFound` if not found.
	//
	// It is the caller's responsibility to close the returned `Cache`.
	Cache(ctx context.Context, name string) (Cache, error)

	// SetCache sets the c to the underlying cacher.
	//
	// It is the caller's responsibility to close the c.
	SetCache(ctx context.Context, c Cache) error
}

// Cache is the cache unit of the `Cacher`.
type Cache interface {
	io.Reader
	io.Seeker
	io.Closer

	// Name returns the unique Unix path style name of the underlying cache.
	Name() string

	// Size returns the length in bytes of the underlying cache.
	Size() int64

	// ModTime returns the modification time of the underlying cache.
	ModTime() time.Time

	// Checksum returns the checksum of the underlying cache.
	Checksum() []byte
}

// tempCacher implements the `Cacher` without doing anything.
type tempCacher struct{}

// NewHash implements the `Cacher`.
func (tc *tempCacher) NewHash() hash.Hash {
	return md5.New()
}

// Cache implements the `Cacher`.
func (tc *tempCacher) Cache(ctx context.Context, name string) (Cache, error) {
	return nil, ErrCacheNotFound
}

// SetCache implements the `Cacher`.
func (tc *tempCacher) SetCache(ctx context.Context, c Cache) error {
	return nil
}

// tempCache implements the `Cache`. It is the cache unit of the `tempCacher`.
type tempCache struct {
	file     *os.File
	name     string
	size     int64
	modTime  time.Time
	checksum []byte
}

// newTempCache returns a new instance of the `tempCache` with the filename, the
// name and the fileHash.
func newTempCache(filename, name string, fileHash hash.Hash) (Cache, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(fileHash, file); err != nil {
		return nil, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	return &tempCache{
		file:     file,
		name:     name,
		size:     fileInfo.Size(),
		modTime:  fileInfo.ModTime(),
		checksum: fileHash.Sum(nil),
	}, nil
}

// Read implements the `Cache`.
func (tc *tempCache) Read(b []byte) (int, error) {
	return tc.file.Read(b)
}

// Seek implements the `Cache`.
func (tc *tempCache) Seek(offset int64, whence int) (int64, error) {
	return tc.file.Seek(offset, whence)
}

// Close implements the `Cache`.
func (tc *tempCache) Close() error {
	return tc.file.Close()
}

// Name implements the `Cache`.
func (tc *tempCache) Name() string {
	return tc.name
}

// Size implements the `Cache`.
func (tc *tempCache) Size() int64 {
	return tc.size
}

// ModTime implements the `Cache`.
func (tc *tempCache) ModTime() time.Time {
	return tc.modTime
}

// Checksum implements the `Cache`.
func (tc *tempCache) Checksum() []byte {
	return tc.checksum
}
