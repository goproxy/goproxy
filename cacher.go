package goproxy

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// Cacher defines a set of intuitive methods used to cache module files for the
// `Goproxy`.
type Cacher interface {
	// Get gets the matched cache for the name. It returns the
	// `os.ErrNotExist` if not found.
	//
	// It is the caller's responsibility to close the returned
	// `io.ReadCloser`.
	Get(ctx context.Context, name string) (io.ReadCloser, error)

	// Set sets the content as a cache with the name.
	Set(ctx context.Context, name string, content io.Reader) error
}

// DirCacher implements the `Cacher` using a directory on the local filesystem.
// If the directory does not exist, it will be created with 0700 permissions.
type DirCacher string

// Get implements the `Cacher`.
func (dc DirCacher) Get(
	ctx context.Context,
	name string,
) (io.ReadCloser, error) {
	return os.Open(filepath.Join(string(dc), filepath.FromSlash(name)))
}

// Set implements the `Cacher`.
func (dc DirCacher) Set(
	ctx context.Context,
	name string,
	content io.Reader,
) error {
	filename := filepath.Join(string(dc), filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}

	file, err := os.OpenFile(
		filename,
		os.O_RDWR|os.O_CREATE|os.O_EXCL,
		0600,
	)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := io.Copy(file, content); err != nil {
		os.Remove(file.Name())
		return err
	}

	return nil
}
