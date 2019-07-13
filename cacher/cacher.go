package cacher

import (
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
