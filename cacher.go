package goproxy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Cacher defines a set of intuitive methods used to cache module files for [Goproxy].
type Cacher interface {
	// Get gets the matched cache for the name. It returns [fs.ErrNotExist]
	// if not found.
	//
	// The returned [io.ReadCloser] may optionally implement the following
	// interfaces:
	//  1. [io.Seeker], mainly for the Range request header.
	//  2. interface{ Size() int64 }, mainly for the Content-Length response
	//     header. It takes effect only if 1 is not implemented.
	//  3. interface{ LastModified() time.Time }, mainly for the
	//     Last-Modified response header. Also for the If-Unmodified-Since,
	//     If-Modified-Since, and If-Range request headers when 1 is
	//     implemented.
	//  4. interface{ ModTime() time.Time }, same as 2 but with lower
	//     priority.
	//  5. interface{ ETag() string }, mainly for the ETag response header.
	//     Also for the If-Match, If-None-Match, and If-Range request
	//     headers when 1 is implemented. Note that the return value will be
	//     assumed to have complied with RFC 7232, section 2.3, so it will
	//     be used directly without further processing.
	Get(ctx context.Context, name string) (io.ReadCloser, error)

	// Put puts a cache for the name with the content.
	Put(ctx context.Context, name string, content io.ReadSeeker) error
}

// DirCacher implements [Cacher] using a directory on the local disk. If the
// directory does not exist, it will be created with 0755 permissions. Cache
// files will be created with 0644 permissions.
type DirCacher string

// Get implements [Cacher].
func (dc DirCacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(string(dc), filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return &struct {
		*os.File
		os.FileInfo
	}{f, fi}, nil
}

// Put implements [Cacher].
func (dc DirCacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	file := filepath.Join(string(dc), filepath.FromSlash(name))
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, fmt.Sprintf(".%s.tmp.*", filepath.Base(file)))
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := io.Copy(f, content); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	if err := os.Chmod(f.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(f.Name(), file)
}
