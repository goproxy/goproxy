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

	responseString(rec, http.StatusOK, "Foobar")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Foobar", rec.Body.String())
}

func TestResponseNotFound(t *testing.T) {
	rec := httptest.NewRecorder()

	responseNotFound(rec)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Not Found", rec.Body.String())

	rec = httptest.NewRecorder()

	responseNotFound(rec, "foobar")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Not Found: foobar", rec.Body.String())

	rec = httptest.NewRecorder()

	responseNotFound(rec, "not found: foobar")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Not Found: foobar", rec.Body.String())

	rec = httptest.NewRecorder()

	responseNotFound(rec, "Gone: foobar")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Not Found: foobar", rec.Body.String())

	rec = httptest.NewRecorder()

	responseNotFound(rec, "gone: foobar")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Not Found: foobar", rec.Body.String())
}

func TestResponseMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()

	responseMethodNotAllowed(rec)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Method Not Allowed", rec.Body.String())

	rec = httptest.NewRecorder()

	responseMethodNotAllowed(rec, "foobar")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Method Not Allowed: foobar", rec.Body.String())
}

func TestResponseInternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()

	responseInternalServerError(rec)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Internal Server Error", rec.Body.String())

	rec = httptest.NewRecorder()

	responseInternalServerError(rec, "foobar")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Internal Server Error: foobar", rec.Body.String())
}

func TestResponseBadGateway(t *testing.T) {
	rec := httptest.NewRecorder()

	responseBadGateway(rec)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Bad Gateway", rec.Body.String())

	rec = httptest.NewRecorder()

	responseBadGateway(rec, "foobar")
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.HeaderMap.Get("Content-Type"),
	)
	assert.Equal(t, "Bad Gateway: foobar", rec.Body.String())
}
