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

// responseNotFound responses "Not Found" to the client with the optional msgs.
func responseNotFound(rw http.ResponseWriter, msgs ...interface{}) {
	var msg string
	if len(msgs) > 0 {
		msg = strings.TrimPrefix(fmt.Sprint(msgs...), "bad request: ")
		msg = strings.TrimPrefix(msg, "not found: ")
		msg = strings.TrimPrefix(msg, "gone: ")
		if msg != "" && !strings.HasPrefix(msg, "Not Found: ") {
			msg = fmt.Sprint("Not Found: ", msg)
		}
	}

	if msg == "" {
		msg = "Not Found"
	}

	responseString(rw, http.StatusNotFound, msg)
}

// responseMethodNotAllowed responses "Method Not Allowed" to the client.
func responseMethodNotAllowed(rw http.ResponseWriter) {
	responseString(rw, http.StatusMethodNotAllowed, "Method Not Allowed")
}

// responseInternalServerError responses "Internal Server Error" to the client.
func responseInternalServerError(rw http.ResponseWriter) {
	responseString(
		rw,
		http.StatusInternalServerError,
		"Internal Server Error",
	)
}

// responseBadGateway responses "Status Bad Gateway" to the client.
func responseBadGateway(rw http.ResponseWriter) {
	responseString(rw, http.StatusBadGateway, "Bad Gateway")
}
