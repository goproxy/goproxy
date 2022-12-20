package goproxy

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirCacher(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestDirCacher")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	dirCacher := DirCacher(tempDir)

	if rc, err := dirCacher.Get(
		nil,
		"a/b/c",
	); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got error %q, want error %q", err, os.ErrNotExist)
	} else if rc != nil {
		t.Errorf("got %v, want nil", rc)
	}

	if err := dirCacher.Set(
		nil,
		"a/b/c",
		strings.NewReader("foobar"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	rc, err := dirCacher.Get(nil, "a/b/c")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if rc == nil {
		t.Fatal("unexpected nil")
	}

	if b, err := ioutil.ReadAll(rc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "foobar"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	if err := rc.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	dirCacher = DirCacher(filepath.Join(
		tempDir,
		filepath.FromSlash("a/b/c"),
	))
	if err := dirCacher.Set(
		nil,
		"d/e/f",
		strings.NewReader("foobar"),
	); err == nil {
		t.Fatal("expected error")
	}
}
