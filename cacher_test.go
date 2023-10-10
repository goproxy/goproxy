package goproxy

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

type errorReadSeeker struct{}

func (errorReadSeeker) Read([]byte) (int, error) {
	return 0, errors.New("cannot read")
}

func (errorReadSeeker) Seek(int64, int) (int64, error) {
	return 0, errors.New("cannot seek")
}

func TestDirCacher(t *testing.T) {
	dirCacher := DirCacher(t.TempDir())

	if rc, err := dirCacher.Get(context.Background(), "a/b/c"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got error %q, want error %q", err, fs.ErrNotExist)
	} else if rc != nil {
		t.Errorf("got %v, want nil", rc)
	}

	if err := dirCacher.Put(context.Background(), "a/b/c", strings.NewReader("foobar")); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	if rc, err := dirCacher.Get(context.Background(), "a/b/c"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if rc == nil {
		t.Fatal("unexpected nil")
	} else if b, err := io.ReadAll(rc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := rc.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), "foobar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := dirCacher.Put(context.Background(), "d/e/f", &errorReadSeeker{}); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "cannot read"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	dirCacher = DirCacher(filepath.Join(string(dirCacher), filepath.FromSlash("a/b/c")))
	if err := dirCacher.Put(context.Background(), "d/e/f", strings.NewReader("foobar")); err == nil {
		t.Fatal("expected error")
	}
}
