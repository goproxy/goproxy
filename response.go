package goproxy

import (
	"fmt"
	"net/http"
	"strings"
)

// setResponseCacheControlHeader sets the Cache-Control header based on the
// maxAge.
func setResponseCacheControlHeader(rw http.ResponseWriter, maxAge int) {
	cacheControl := ""
	if maxAge >= 0 {
		cacheControl = fmt.Sprintf("public, max-age=%d", maxAge)
	} else {
		cacheControl = "must-revalidate, no-cache, no-store"
	}

	rw.Header().Set("Cache-Control", cacheControl)
}

// responseString responses the s as a "text/plain" content to the client with
// the statusCode.
func responseString(rw http.ResponseWriter, statusCode int, s string) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.WriteHeader(statusCode)
	rw.Write([]byte(s))
}

// responseJSON responses the b as a "application/json" content to the client
// with the statusCode.
func responseJSON(rw http.ResponseWriter, statusCode int, b []byte) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(statusCode)
	rw.Write(b)
}

// responseNotFound responses "not found" to the client with the optional msgs.
func responseNotFound(rw http.ResponseWriter, msgs ...interface{}) {
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

	responseString(rw, http.StatusNotFound, msg)
}

// responseMethodNotAllowed responses "method not allowed" to the client.
func responseMethodNotAllowed(rw http.ResponseWriter) {
	responseString(rw, http.StatusMethodNotAllowed, "method not allowed")
}

// responseInternalServerError responses "internal server error" to the client.
func responseInternalServerError(rw http.ResponseWriter) {
	responseString(
		rw,
		http.StatusInternalServerError,
		"internal server error",
	)
}

// responseModError responses the err as an mod operation error to the client.
func responseModError(rw http.ResponseWriter, err error, cacheSensitive bool) {
	if isNotFoundError(err) {
		if cacheSensitive {
			setResponseCacheControlHeader(rw, 60)
		} else {
			setResponseCacheControlHeader(rw, 600)
		}

		responseNotFound(rw, err)
	} else if isTimeoutError(err) {
		setResponseCacheControlHeader(rw, -1)
		responseNotFound(rw, "fetch timed out")
	} else {
		responseInternalServerError(rw)
	}
}
