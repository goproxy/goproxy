package goproxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetResponseCacheControlHeader(t *testing.T) {
	rec := httptest.NewRecorder()

	assert.Empty(t, rec.Header().Get("Cache-Control"))

	setResponseCacheControlHeader(rec, 60)
	assert.Equal(t, "public, max-age=60", rec.Header().Get("Cache-Control"))

	setResponseCacheControlHeader(rec, -1)
	assert.Equal(
		t,
		"must-revalidate, no-cache, no-store",
		rec.Header().Get("Cache-Control"),
	)
}

func TestResponseString(t *testing.T) {
	rec := httptest.NewRecorder()

	responseString(rec, "Foobar")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Foobar", rec.Body.String())
}

func TestResponseStatusCode(t *testing.T) {
	rec := httptest.NewRecorder()

	responseStatusCode(rec, http.StatusOK)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "OK", rec.Body.String())
}

func TestResponseNotFound(t *testing.T) {
	rec := httptest.NewRecorder()

	responseStatusCode(rec, http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Not Found", rec.Body.String())
}

func TestResponseMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()

	responseStatusCode(rec, http.StatusMethodNotAllowed)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Method Not Allowed", rec.Body.String())
}

func TestResponseInternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()

	responseStatusCode(rec, http.StatusInternalServerError)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Internal Server Error", rec.Body.String())
}

func TestResponseBadGateway(t *testing.T) {
	rec := httptest.NewRecorder()

	responseStatusCode(rec, http.StatusBadGateway)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Bad Gateway", rec.Body.String())
}
