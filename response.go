package goproxy

import (
	"encoding/json"
	"net/http"
)

// setResponseCacheControlHeader sets the Cache-Control header based on the
// cacheable.
func setResponseCacheControlHeader(rw http.ResponseWriter, cacheable bool) {
	cacheControl := ""
	if cacheable {
		cacheControl = "max-age=31536000"
	} else {
		cacheControl = "must-revalidate, no-cache, no-store"
	}

	rw.Header().Set("Cache-Control", cacheControl)
}

// responseJSON responses the JSON marshaled from the v to the client.
func responseJSON(rw http.ResponseWriter, v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		responseInternalServerError(rw)
		return
	}

	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.Write(b)
}

// responseString responses the s to the client.
func responseString(rw http.ResponseWriter, s string) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.Write([]byte(s))
}

// responseStatusCode responses the sc to the client.
func responseStatusCode(rw http.ResponseWriter, sc int) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.WriteHeader(sc)
	rw.Write([]byte(http.StatusText(sc)))
}

// responseNotFound responses "Not Found" to the client.
func responseNotFound(rw http.ResponseWriter) {
	responseStatusCode(rw, http.StatusNotFound)
}

// responseMethodNotAllowed responses "Method Not Allowed" to the client.
func responseMethodNotAllowed(rw http.ResponseWriter) {
	responseStatusCode(rw, http.StatusMethodNotAllowed)
}

// responseGone responses "Gone" to the client.
func responseGone(rw http.ResponseWriter) {
	responseStatusCode(rw, http.StatusGone)
}

// responseInternalServerError responses "Internal Server Error" to the client.
func responseInternalServerError(rw http.ResponseWriter) {
	responseStatusCode(rw, http.StatusInternalServerError)
}

// responseBadGateway responses "Status Bad Gateway" to the client.
func responseBadGateway(rw http.ResponseWriter) {
	responseStatusCode(rw, http.StatusBadGateway)
}
