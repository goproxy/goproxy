package goproxy

import (
	"context"
	"errors"
	"hash"
	"io"
	"mime"
	"os"
	"path"
	"strings"
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
	// checksums of the caches.
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

	// Name returns the unique Unix path style name.
	Name() string

	// MIMEType returns the MIME type.
	MIMEType() string

	// Size returns the length in bytes.
	Size() int64

	// ModTime returns the modification time.
	ModTime() time.Time

	// Checksum returns the checksum.
	Checksum() []byte
}

// tempCache implements the `Cache`.
type tempCache struct {
	file     *os.File
	name     string
	mimeType string
	size     int64
	modTime  time.Time
	checksum []byte
}

// newTempCache returns a new instance of the `tempCache` with the filename,
// name, and fileHash.
func newTempCache(filename, name string, fileHash hash.Hash) (Cache, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var mimeType string
	switch ext := strings.ToLower(path.Ext(name)); ext {
	case ".info":
		mimeType = "application/json; charset=utf-8"
	case ".mod":
		mimeType = "text/plain; charset=utf-8"
	case ".zip":
		mimeType = "application/zip"
	default:
		mimeType = mime.TypeByExtension(ext)
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
		mimeType: mimeType,
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

// MIMEType implements the `Cache`.
func (tc *tempCache) MIMEType() string {
	return tc.mimeType
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
