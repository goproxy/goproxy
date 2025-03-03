package goproxy

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirCacher(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		dirCacher := DirCacher(t.TempDir())

		if err := dirCacher.Put(context.Background(), "a/b/c", strings.NewReader("foobar")); err != nil {
			t.Fatalf("unexpected error %q", err)
		}

		if fi, err := os.Stat(filepath.Join(string(dirCacher), filepath.FromSlash("a/b"))); err != nil {
			t.Errorf("unexpected error %q", err)
		} else if got, want := fi.Mode().Perm(), os.FileMode(0o755).Perm(); got != want {
			t.Errorf("got %d, want %d", got, want)
		}

		if fi, err := os.Stat(filepath.Join(string(dirCacher), filepath.FromSlash("a/b/c"))); err != nil {
			t.Errorf("unexpected error %q", err)
		} else if got, want := fi.Mode().Perm(), os.FileMode(0o644).Perm(); got != want {
			t.Errorf("got %d, want %d", got, want)
		}

		if rc, err := dirCacher.Get(context.Background(), "a/b/c"); err != nil {
			t.Errorf("unexpected error %q", err)
		} else if rc == nil {
			t.Error("unexpected nil")
		} else if b, err := io.ReadAll(rc); err != nil {
			t.Errorf("unexpected error %q", err)
		} else if err := rc.Close(); err != nil {
			t.Errorf("unexpected error %q", err)
		} else if got, want := string(b), "foobar"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("GetNonExistentFile", func(t *testing.T) {
		dirCacher := DirCacher(t.TempDir())

		rc, err := dirCacher.Get(context.Background(), "a/b/c")
		if err == nil {
			t.Fatal("expected error")
		}
		if got, want := err, fs.ErrNotExist; !compareErrors(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
		if got := rc; got != nil {
			t.Errorf("got %#v, want nil", got)
		}
	})

	t.Run("PutWithReadError", func(t *testing.T) {
		dirCacher := DirCacher(t.TempDir())
		errRead := errors.New("cannot read")

		err := dirCacher.Put(context.Background(), "d/e/f", &testReadSeeker{
			ReadSeeker: strings.NewReader("foobar"),
			read: func(rs io.ReadSeeker, p []byte) (n int, err error) {
				return 0, errRead
			},
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if got, want := err, errRead; !compareErrors(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("PutWithInvalidDirectory", func(t *testing.T) {
		cacheDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(cacheDir, filepath.FromSlash("a/b")), 0o755); err != nil {
			t.Fatalf("unexpected error %q", err)
		}
		if err := os.WriteFile(filepath.Join(cacheDir, filepath.FromSlash("a/b/c")), []byte("foobar"), 0o644); err != nil {
			t.Fatalf("unexpected error %q", err)
		}
		dirCacher := DirCacher(filepath.Join(cacheDir, filepath.FromSlash("a/b/c")))

		err := dirCacher.Put(context.Background(), "d/e/f", strings.NewReader("foobar"))
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
