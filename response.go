package goproxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// setResponseCacheControlHeader sets the Cache-Control header based on the
// maxAge.
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
func responseString(
	rw http.ResponseWriter,
	statusCode int,
	cacheControlMaxAge int,
	s string,
) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	setResponseCacheControlHeader(rw, cacheControlMaxAge)
	rw.WriteHeader(statusCode)
	rw.Write([]byte(s))
}

// responseJSON responses the b as a "application/json" content to the client
// with the statusCode and cacheControlMaxAge.
func responseJSON(
	rw http.ResponseWriter,
	statusCode int,
	cacheControlMaxAge int,
	b []byte,
) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	setResponseCacheControlHeader(rw, cacheControlMaxAge)
	rw.WriteHeader(statusCode)
	rw.Write(b)
}

// responseNotFound responses "not found" to the client with the
// cacheControlMaxAge and optional msgs.
func responseNotFound(
	rw http.ResponseWriter,
	cacheControlMaxAge int,
	msgs ...interface{},
) {
	var msg string
	if len(msgs) > 0 {
		msg = strings.TrimPrefix(fmt.Sprint(msgs...), "bad request: ")
		msg = strings.TrimPrefix(msg, "gone: ")
		if msg != "" && !strings.HasPrefix(msg, "not found: ") {
			msg = fmt.Sprint("not found: ", msg)
		}
	}

	if msg == "" {
		msg = "not found"
	}

	responseString(rw, http.StatusNotFound, cacheControlMaxAge, msg)
}

// responseMethodNotAllowed responses "method not allowed" to the client with
// the cacheControlMaxAge.
func responseMethodNotAllowed(rw http.ResponseWriter, cacheControlMaxAge int) {
	responseString(
		rw,
		http.StatusMethodNotAllowed,
		cacheControlMaxAge,
		"method not allowed",
	)
}

// responseInternalServerError responses "internal server error" to the client.
func responseInternalServerError(rw http.ResponseWriter) {
	responseString(
		rw,
		http.StatusInternalServerError,
		-2,
		"internal server error",
	)
}

// responseModError responses the err as a mod operation error to the client.
func responseModError(rw http.ResponseWriter, err error, cacheSensitive bool) {
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

		responseNotFound(rw, cacheControlMaxAge, msg)
	} else if errors.Is(err, errBadUpstream) {
		responseNotFound(rw, -1, errBadUpstream)
	} else if t, ok := err.(interface {
		Timeout() bool
	}); (ok && t.Timeout()) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errFetchTimedOut) ||
		strings.Contains(err.Error(), errFetchTimedOut.Error()) {
		responseNotFound(rw, -1, errFetchTimedOut)
	} else {
		responseInternalServerError(rw)
	}
}
