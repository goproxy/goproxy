package goproxy

import (
	"fmt"
	"net/http"
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
	msg := "Not Found"
	if len(msgs) > 0 {
		msg = fmt.Sprint(msg, ": ", fmt.Sprint(msgs...))
	}

	responseString(rw, http.StatusNotFound, msg)
}

// responseMethodNotAllowed responses "Method Not Allowed" to the client with
// the optional msgs.
func responseMethodNotAllowed(rw http.ResponseWriter, msgs ...interface{}) {
	msg := "Method Not Allowed"
	if len(msgs) > 0 {
		msg = fmt.Sprint(msg, ": ", fmt.Sprint(msgs...))
	}

	responseString(rw, http.StatusMethodNotAllowed, msg)
}

// responseInternalServerError responses "Internal Server Error" to the client
// with the optional msgs.
func responseInternalServerError(rw http.ResponseWriter, msgs ...interface{}) {
	msg := "Internal Server Error"
	if len(msgs) > 0 {
		msg = fmt.Sprint(msg, ": ", fmt.Sprint(msgs...))
	}

	responseString(rw, http.StatusInternalServerError, msg)
}

// responseBadGateway responses "Status Bad Gateway" to the client with the
// optional msgs.
func responseBadGateway(rw http.ResponseWriter, msgs ...interface{}) {
	msg := "Bad Gateway"
	if len(msgs) > 0 {
		msg = fmt.Sprint(msg, ": ", fmt.Sprint(msgs...))
	}

	responseString(rw, http.StatusBadGateway, msg)
}
