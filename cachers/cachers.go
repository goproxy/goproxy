package cachers

import (
	"io"
	"mime"
	"strings"
)

// mimeTypeByExtension returns the MIME type associated with the ext.
func mimeTypeByExtension(ext string) string {
	switch strings.ToLower(ext) {
	case ".info":
		return "application/json; charset=utf-8"
	case ".mod":
		return "text/plain; charset=utf-8"
	case ".zip":
		return "application/zip"
	}

	return mime.TypeByExtension(ext)
}

// fakeWriterAt wraps the `io.Writer` to implement the `io.WriteAt`.
type fakeWriterAt struct {
	w io.Writer
}

// WriteAt implements the `io.WriteAt`.
func (fwa *fakeWriterAt) WriteAt(b []byte, offset int64) (int, error) {
	return fwa.w.Write(b)
}
