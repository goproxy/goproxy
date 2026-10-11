package goproxy

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirCacherGet(t *testing.T) {
	for _, tt := range []struct {
		name    string
		file    string
		content string
		wantErr error
	}{
		{"Content", "file", "foobar", nil},
		{"Empty", "file", "", nil},
		{"NestedPath", "a/b/c", "foobar", nil},
		{"MissingFile", "file", "", fs.ErrNotExist},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dirCacher := DirCacher(t.TempDir())
			if tt.wantErr == nil {
				file := filepath.Join(string(dirCacher), filepath.FromSlash(tt.file))
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if err := os.WriteFile(file, []byte(tt.content), 0o644); err != nil {
					t.Fatalf("unexpected error %v", err)
				}
			}

			rc, err := dirCacher.Get(t.Context(), tt.file)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got error %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if rc != nil {
					t.Errorf("got content %v, want nil", rc)
				}
				return
			}
			if rc == nil {
				t.Fatal("unexpected nil content")
			}
			defer rc.Close()
			content, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if got, want := string(content), tt.content; got != want {
				t.Errorf("got content %q, want %q", got, want)
			}
		})
	}
}

func TestDirCacherPut(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		replace bool
	}{
		{"Content", "foobar", false},
		{"Empty", "", false},
		{"Replace", "updated", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dirCacher := DirCacher(t.TempDir())
			file := filepath.Join(string(dirCacher), filepath.FromSlash("a/b/c"))
			if tt.replace {
				if err := dirCacher.Put(t.Context(), "a/b/c", strings.NewReader("original")); err != nil {
					t.Fatalf("unexpected error %v", err)
				}
			}
			if err := dirCacher.Put(t.Context(), "a/b/c", strings.NewReader(tt.content)); err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if content, err := os.ReadFile(file); err != nil {
				t.Fatalf("unexpected error %v", err)
			} else if got, want := string(content), tt.content; got != want {
				t.Errorf("got content %q, want %q", got, want)
			}
			if fi, err := os.Stat(file); err != nil {
				t.Fatalf("unexpected error %v", err)
			} else if got, want := fi.Mode().Perm(), os.FileMode(0o644); runtime.GOOS != "windows" && got != want {
				t.Errorf("got file permissions %#o, want %#o", got, want)
			}
			// Directory permissions depend on the process umask.
			dir := filepath.Join(t.TempDir(), "directory")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			fi, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if got, err := os.Stat(filepath.Dir(file)); err != nil {
				t.Fatalf("unexpected error %v", err)
			} else if got, want := got.Mode().Perm(), fi.Mode().Perm(); got != want {
				t.Errorf("got directory permissions %#o, want %#o", got, want)
			}
			if entries, err := os.ReadDir(filepath.Dir(file)); err != nil {
				t.Fatalf("unexpected error %v", err)
			} else if len(entries) != 1 || entries[0].Name() != "c" {
				t.Errorf("got directory entries %v, want only c", entries)
			}
		})
	}

	t.Run("ReadError", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			content string
		}{
			{"BeforeWriting", ""},
			{"AfterWriting", "partial"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				dirCacher := DirCacher(t.TempDir())
				if err := dirCacher.Put(t.Context(), "file", strings.NewReader("original")); err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				errRead := errors.New("cannot read")
				err := dirCacher.Put(t.Context(), "file", &testReadSeeker{
					ReadSeeker: strings.NewReader(tt.content),
					read: func(rs io.ReadSeeker, p []byte) (int, error) {
						n, _ := rs.Read(p)
						return n, errRead
					},
				})
				if !errors.Is(err, errRead) {
					t.Errorf("got error %v, want %v", err, errRead)
				}
				if content, err := os.ReadFile(filepath.Join(string(dirCacher), "file")); err != nil {
					t.Fatalf("unexpected error %v", err)
				} else if got, want := string(content), "original"; got != want {
					t.Errorf("got content %q, want %q", got, want)
				}
				if entries, err := os.ReadDir(string(dirCacher)); err != nil {
					t.Fatalf("unexpected error %v", err)
				} else if len(entries) != 1 || entries[0].Name() != "file" {
					t.Errorf("got directory entries %v, want only file", entries)
				}
			})
		}
	})

	t.Run("InvalidDirectory", func(t *testing.T) {
		cacheDir := t.TempDir()
		file := filepath.Join(cacheDir, "file")
		if err := os.WriteFile(file, []byte("foobar"), 0o644); err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		dirCacher := DirCacher(file)
		if err := dirCacher.Put(t.Context(), "a/b/c", strings.NewReader("foobar")); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("RenameError", func(t *testing.T) {
		dirCacher := DirCacher(t.TempDir())
		if err := os.Mkdir(filepath.Join(string(dirCacher), "directory"), 0o755); err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		if err := dirCacher.Put(t.Context(), "directory", strings.NewReader("foobar")); err == nil {
			t.Fatal("expected error")
		}
		if entries, err := os.ReadDir(string(dirCacher)); err != nil {
			t.Fatalf("unexpected error %v", err)
		} else if len(entries) != 1 || entries[0].Name() != "directory" || !entries[0].IsDir() {
			t.Errorf("got directory entries %v, want only directory", entries)
		}
	})
}
