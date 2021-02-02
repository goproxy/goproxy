package cacher

import (
	"context"
	"crypto/md5"
	"fmt"
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
	filename := filepath.Join(d.Root, filepath.FromSlash(name))
	file, err := os.Open(filename)
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

	fileMIMEType, err := ioutil.ReadFile(fmt.Sprint(filename, ".mime-type"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, goproxy.ErrCacheNotFound
		}

		return nil, err
	}

	fileChecksum, err := ioutil.ReadFile(fmt.Sprint(filename, ".checksum"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, goproxy.ErrCacheNotFound
		}

		return nil, err
	}

	return &diskCache{
		Reader:   file,
		Seeker:   file,
		Closer:   file,
		name:     name,
		mimeType: string(fileMIMEType),
		size:     fileInfo.Size(),
		modTime:  fileInfo.ModTime(),
		checksum: fileChecksum,
	}, nil
}

// SetCache implements the `goproxy.Cacher`.
func (d *Disk) SetCache(ctx context.Context, c goproxy.Cache) error {
	filename := filepath.Join(d.Root, filepath.FromSlash(c.Name()))
	if err := os.MkdirAll(
		filepath.Dir(filename),
		os.ModePerm,
	); err != nil {
		return err
	}

	if err := ioutil.WriteFile(
		fmt.Sprint(filename, ".mime-type"),
		[]byte(c.MIMEType()),
		os.ModePerm,
	); err != nil {
		return err
	}

	if err := ioutil.WriteFile(
		fmt.Sprint(filename, ".checksum"),
		c.Checksum(),
		os.ModePerm,
	); err != nil {
		return err
	}

	b, err := ioutil.ReadAll(c)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filename, b, os.ModePerm)
}

// diskCache implements the `goproxy.Cache`. It is the cache unit of the `Disk`.
type diskCache struct {
	io.Reader
	io.Seeker
	io.Closer

	name     string
	mimeType string
	size     int64
	modTime  time.Time
	checksum []byte
}

// Name implements the `goproxy.Cache`.
func (dc *diskCache) Name() string {
	return dc.name
}

// MIMEType implements the `goproxy.Cache`.
func (dc *diskCache) MIMEType() string {
	return dc.mimeType
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
