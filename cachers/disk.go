package cachers

import (
	"context"
	"crypto/md5"
	"hash"
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
	Root string `mapstructure:"root"`
}

// NewHash implements the `goproxy.Cacher`.
func (d *Disk) NewHash() hash.Hash {
	return md5.New()
}

// Cache implements the `goproxy.Cacher`.
func (d *Disk) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	file, err := os.Open(filepath.Join(d.Root, filepath.FromSlash(name)))
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

	fileHash := d.NewHash()
	if _, err := io.Copy(fileHash, file); err != nil {
		return nil, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	return &diskCache{
		file:     file,
		name:     name,
		size:     fileInfo.Size(),
		modTime:  fileInfo.ModTime(),
		checksum: fileHash.Sum(nil),
	}, nil
}

// SetCache implements the `goproxy.Cacher`.
func (d *Disk) SetCache(ctx context.Context, c goproxy.Cache) error {
	b, err := ioutil.ReadAll(c)
	if err != nil {
		return err
	}

	filename := filepath.Join(d.Root, filepath.FromSlash(c.Name()))
	if err := os.MkdirAll(
		filepath.Dir(filename),
		os.ModePerm,
	); err != nil {
		return err
	}

	return ioutil.WriteFile(filename, b, os.ModePerm)
}

// diskCache implements the `goproxy.Cache`. It is the cache unit of the `Disk`.
type diskCache struct {
	file     *os.File
	name     string
	size     int64
	modTime  time.Time
	checksum []byte
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

// Size implements the `goproxy.Cache`.
func (dc *diskCache) Size() int64 {
	return dc.size
}

// ModTime implements the `goproxy.Cache`.
func (dc *diskCache) ModTime() time.Time {
	return dc.modTime
}

// Checksum implements the `goproxy.Cache`.
func (dc *diskCache) Checksum() []byte {
	return dc.checksum
}
