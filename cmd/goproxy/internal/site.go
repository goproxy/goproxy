package internal

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// siteHandler serves public files and directory indexes from a site root. It
// expects the mount prefix to be stripped from request paths.
type siteHandler struct {
	root   *os.Root
	prefix string
}

// ServeHTTP implements [http.Handler].
func (h siteHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	const index = "index.html"

	requestPath := req.URL.Path
	trailingSlash := strings.HasSuffix(requestPath, "/")

	name := strings.TrimSuffix(strings.TrimPrefix(requestPath, "/"), "/")
	if requestPath == "/" {
		name = "."
	} else if name == "." {
		h.serveNotFound(rw, req)
		return
	}
	name, err := filepath.Localize(name)
	if err != nil {
		h.serveNotFound(rw, req)
		return
	}

	fi, err := h.root.Stat(name)
	if err != nil {
		h.serveNotFound(rw, req)
		return
	}
	var redirectPath string
	if fi.IsDir() {
		name = filepath.Join(name, index)
		fi, err = h.root.Stat(name)
		if !trailingSlash {
			redirectPath = requestPath + "/"
		}
	} else if trailingSlash {
		h.serveNotFound(rw, req)
		return
	} else if filepath.Base(name) == index {
		redirectPath = strings.TrimSuffix(requestPath, index)
	}
	if err != nil {
		h.serveNotFound(rw, req)
		return
	}
	f, fi := h.openRegularFile(name, fi)
	if f == nil {
		h.serveNotFound(rw, req)
		return
	}
	defer f.Close()

	if redirectPath != "" {
		location := url.URL{
			Path:       h.prefix + redirectPath,
			RawQuery:   req.URL.RawQuery,
			ForceQuery: req.URL.ForceQuery,
		}
		// Set Location directly to preserve the raw query bytes.
		rw.Header().Set("Location", location.RequestURI())
		rw.WriteHeader(http.StatusMovedPermanently)
		return
	}

	// Ignore Range on HEAD requests as required by RFC 9110, Section 14.2.
	if req.Method == http.MethodHead && req.Header.Get("Range") != "" {
		req = req.Clone(req.Context())
		req.Header.Del("Range")
	}

	http.ServeContent(siteFileResponseWriter{rw}, req, filepath.Base(name), fi.ModTime(), f)
}

// serveNotFound serves the site error page when available and otherwise returns
// a plain-text 404.
func (h siteHandler) serveNotFound(rw http.ResponseWriter, req *http.Request) {
	const name = "404.html"

	fi, err := h.root.Stat(name)
	if err != nil {
		http.NotFound(rw, req)
		return
	}
	f, _ := h.openRegularFile(name, fi)
	if f == nil {
		http.NotFound(rw, req)
		return
	}
	defer f.Close()

	writeSiteNotFound(rw, req, f)
}

// writeSiteNotFound writes a custom 404 page, falling back to plain text if
// the page cannot be read.
func writeSiteNotFound(rw http.ResponseWriter, req *http.Request, page io.Reader) {
	// Read the page before committing headers so a read failure can fall
	// back to plain text.
	content, err := io.ReadAll(page)
	if err != nil {
		http.NotFound(rw, req)
		return
	}

	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Content-Length", strconv.Itoa(len(content)))
	rw.WriteHeader(http.StatusNotFound)
	if req.Method != http.MethodHead {
		rw.Write(content)
	}
}

// openRegularFile opens a regular file in the site root and returns its current
// metadata. It returns nil values if the file cannot be opened or is not
// regular.
func (h siteHandler) openRegularFile(name string, fi os.FileInfo) (*os.File, os.FileInfo) {
	// Check the type before opening to avoid blocking on special files such
	// as named pipes.
	if !fi.Mode().IsRegular() {
		return nil, nil
	}
	f, err := h.root.Open(name)
	if err != nil {
		return nil, nil
	}
	fi, err = f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, nil
	}
	return f, fi
}

// siteFileResponseWriter sets the cache policy for responses from [http.ServeContent].
type siteFileResponseWriter struct {
	http.ResponseWriter
}

// WriteHeader implements [http.ResponseWriter].
func (rw siteFileResponseWriter) WriteHeader(statusCode int) {
	cacheControl := "public, max-age=60"
	if statusCode >= http.StatusBadRequest {
		cacheControl = "no-store"
	}
	rw.Header().Set("Cache-Control", cacheControl)
	rw.ResponseWriter.WriteHeader(statusCode)
}

// ReadFrom implements [io.ReaderFrom].
func (rw siteFileResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(rw.ResponseWriter, r)
}
