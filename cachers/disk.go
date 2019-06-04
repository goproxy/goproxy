package cachers

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/goproxy/goproxy"
)

// Disk implements the `goproxy.Cacher` by using the disk.
type Disk struct {
	// Root is the root of the caches.
	//
	// If the `Root` is empty, the `os.TempDir` is used.
	//
	// Note that the `Root` must be a UNIX-style path.
	Root string
}

// Get implements the `goproxy.Cacher`.
func (d *Disk) Get(ctx context.Context, name string) (goproxy.Cache, error) {
	file, err := os.Open(d.filename(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, goproxy.ErrCacheNotFound
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

// Set implements the `goproxy.Cacher`.
func (d *Disk) Set(ctx context.Context, name string, r io.Reader) error {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}

	filename := d.filename(name)
	if err := os.MkdirAll(
		filepath.Dir(filename),
		os.ModePerm,
	); err != nil {
		return err
	}

	return ioutil.WriteFile(filename, b, os.ModePerm)
}

// filename returns the disk file representation of the name.
func (d *Disk) filename(name string) string {
	name = filepath.FromSlash(name)
	if d.Root != "" {
		return filepath.Join(filepath.FromSlash(d.Root), name)
	}

	return filepath.Join(os.TempDir(), name)
}

// diskCache implements the `goproxy.Cache`. It is the cache unit of the `Disk`.
type diskCache struct {
	file    *os.File
	name    string
	modTime time.Time
}

// Read implements the `goproxy.Cache`.
func (dc *diskCache) Read(b []byte) (int, error) {
	return dc.file.Read(b)
}

// Seek implements the `goproxy.Cache`.
func (dc *diskCache) Seek(offset int64, whence int) (int64, error) {
	return dc.file.Seek(offset, whence)
}

// Close implements the `goproxy.Cache`.
func (dc *diskCache) Close() error {
	return dc.file.Close()
}

// Name implements the `goproxy.Cache`.
func (dc *diskCache) Name() string {
	return dc.name
}

// ModTime implements the `goproxy.Cache`.
func (dc *diskCache) ModTime() time.Time {
	return dc.modTime
}
