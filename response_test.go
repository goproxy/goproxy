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
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, "Foobar", rec.Body.String())
}

func TestResponseJSON(t *testing.T) {
	rec := httptest.NewRecorder()

	responseJSON(rec, http.StatusOK, []byte(`{"Foo":"Bar"}`))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(
		t,
		"application/json; charset=utf-8",
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, `{"Foo":"Bar"}`, rec.Body.String())
}

func TestResponseNotFound(t *testing.T) {
	rec := httptest.NewRecorder()

	responseNotFound(rec)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, "not found", rec.Body.String())

	rec = httptest.NewRecorder()

	responseNotFound(rec, "foobar")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, "not found: foobar", rec.Body.String())

	rec = httptest.NewRecorder()

	responseNotFound(rec, "bad request: foobar")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, "not found: foobar", rec.Body.String())

	rec = httptest.NewRecorder()

	responseNotFound(rec, "not found: foobar")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, "not found: foobar", rec.Body.String())

	rec = httptest.NewRecorder()

	responseNotFound(rec, "gone: foobar")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, "not found: foobar", rec.Body.String())
}

func TestResponseMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()

	responseMethodNotAllowed(rec)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, "method not allowed", rec.Body.String())
}

func TestResponseInternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()

	responseInternalServerError(rec)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, "internal server error", rec.Body.String())
}

func TestResponseBadGateway(t *testing.T) {
	rec := httptest.NewRecorder()

	responseBadGateway(rec)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		rec.Result().Header.Get("Content-Type"),
	)
	assert.Equal(t, "bad gateway", rec.Body.String())
}
