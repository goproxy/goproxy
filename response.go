package goproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// setResponseCacheControlHeader sets the Cache-Control header based on the maxAge.
func setResponseCacheControlHeader(rw http.ResponseWriter, maxAge int) {
	if maxAge < -1 {
		return
	}
	cacheControl := ""
	if maxAge == -1 {
		cacheControl = "must-revalidate, no-cache, no-store"
	} else {
		cacheControl = fmt.Sprintf("public, max-age=%d", maxAge)
	}
	rw.Header().Set("Cache-Control", cacheControl)
}

// responseString responses the s as a "text/plain" content to the client with
// the statusCode and cacheControlMaxAge.
func responseString(rw http.ResponseWriter, req *http.Request, statusCode, cacheControlMaxAge int, s string) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	setResponseCacheControlHeader(rw, cacheControlMaxAge)
	rw.WriteHeader(statusCode)
	if req.Method != http.MethodHead {
		rw.Write([]byte(s))
	}
}

// responseNotFound responses "not found" to the client with the
// cacheControlMaxAge and optional msgs.
func responseNotFound(rw http.ResponseWriter, req *http.Request, cacheControlMaxAge int, msgs ...any) {
	var msg string
	if len(msgs) > 0 {
		msg = strings.TrimPrefix(fmt.Sprint(msgs...), "bad request: ")
		msg = strings.TrimPrefix(msg, "gone: ")
		if msg != "" && msg != "not found" && !strings.HasPrefix(msg, "not found: ") {
			msg = "not found: " + msg
		}
	}
	if msg == "" {
		msg = "not found"
	}
	responseString(rw, req, http.StatusNotFound, cacheControlMaxAge, msg)
}

// responseMethodNotAllowed responses "method not allowed" to the client with
// the cacheControlMaxAge.
func responseMethodNotAllowed(rw http.ResponseWriter, req *http.Request, cacheControlMaxAge int) {
	responseString(rw, req, http.StatusMethodNotAllowed, cacheControlMaxAge, "method not allowed")
}

// responseInternalServerError responses "internal server error" to the client.
func responseInternalServerError(rw http.ResponseWriter, req *http.Request) {
	responseString(rw, req, http.StatusInternalServerError, -2, "internal server error")
}

// responseSuccess responses success to the client with the content, contentType
// , and cacheControlMaxAge.
func responseSuccess(rw http.ResponseWriter, req *http.Request, content io.Reader, contentType string, cacheControlMaxAge int) {
	rw.Header().Set("Content-Type", contentType)
	setResponseCacheControlHeader(rw, cacheControlMaxAge)

	var lastModified time.Time
	if lm, ok := content.(interface{ LastModified() time.Time }); ok {
		lastModified = lm.LastModified()
	} else if mt, ok := content.(interface{ ModTime() time.Time }); ok {
		lastModified = mt.ModTime()
	}

	if et, ok := content.(interface{ ETag() string }); ok {
		if etag := et.ETag(); etag != "" {
			rw.Header().Set("ETag", etag)
		}
	}

	if content, ok := content.(io.ReadSeeker); ok {
		http.ServeContent(rw, req, "", lastModified, content)
		return
	}

	if !lastModified.IsZero() {
		rw.Header().Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	}

	rw.WriteHeader(http.StatusOK)
	if req.Method != http.MethodHead {
		io.Copy(rw, content)
	}
}

// responseError responses error to the client with the err and cacheSensitive.
func responseError(rw http.ResponseWriter, req *http.Request, err error, cacheSensitive bool) {
	if errors.Is(err, errNotFound) {
		cacheControlMaxAge := -1
		msg := err.Error()
		if strings.Contains(msg, errBadUpstream.Error()) {
			msg = errBadUpstream.Error()
		} else if strings.Contains(msg, errFetchTimedOut.Error()) {
			msg = errFetchTimedOut.Error()
		} else if cacheSensitive {
			cacheControlMaxAge = 60
		} else {
			cacheControlMaxAge = 600
		}
		responseNotFound(rw, req, cacheControlMaxAge, msg)
	} else if errors.Is(err, errBadUpstream) {
		responseNotFound(rw, req, -1, errBadUpstream)
	} else if t, ok := err.(interface{ Timeout() bool }); (ok && t.Timeout()) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errFetchTimedOut) ||
		strings.Contains(err.Error(), errFetchTimedOut.Error()) {
		responseNotFound(rw, req, -1, errFetchTimedOut)
	} else {
		responseInternalServerError(rw, req)
	}
}
