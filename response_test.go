package goproxy

import (
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetResponseCacheControlHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	setResponseCacheControlHeader(rec, 60)
	recr := rec.Result()
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))

	// ---

	rec = httptest.NewRecorder()
	setResponseCacheControlHeader(rec, 0)
	recr = rec.Result()
	assert.Equal(t, "public, max-age=0", recr.Header.Get("Cache-Control"))

	// ---

	rec = httptest.NewRecorder()
	setResponseCacheControlHeader(rec, -1)
	recr = rec.Result()
	assert.Equal(
		t,
		"must-revalidate, no-cache, no-store",
		recr.Header.Get("Cache-Control"),
	)

	// ---

	rec = httptest.NewRecorder()
	setResponseCacheControlHeader(rec, -2)
	recr = rec.Result()
	assert.Empty(t, recr.Header.Get("Cache-Control"))
}

func TestResponseString(t *testing.T) {
	rec := httptest.NewRecorder()

	responseString(rec, http.StatusOK, 60, "foobar")

	recr := rec.Result()
	recrb, _ := ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusOK, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))
	assert.Equal(t, "foobar", string(recrb))
}

func TestResponseJSON(t *testing.T) {
	rec := httptest.NewRecorder()

	responseJSON(rec, http.StatusOK, 60, []byte(`{"foo":"bar"}`))

	recr := rec.Result()
	recrb, _ := ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusOK, recr.StatusCode)
	assert.Equal(
		t,
		"application/json; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))
	assert.Equal(t, `{"foo":"bar"}`, string(recrb))
}

func TestResponseNotFound(t *testing.T) {
	rec := httptest.NewRecorder()

	responseNotFound(rec, 60)

	recr := rec.Result()
	recrb, _ := ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))
	assert.Equal(t, "not found", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseNotFound(rec, 60, "foobar")

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))
	assert.Equal(t, "not found: foobar", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseNotFound(rec, 60, "bad request: foobar")

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))
	assert.Equal(t, "not found: foobar", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseNotFound(rec, 60, "not found: foobar")

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))
	assert.Equal(t, "not found: foobar", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseNotFound(rec, 60, "gone: foobar")

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))
	assert.Equal(t, "not found: foobar", string(recrb))
}

func TestResponseMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()

	responseMethodNotAllowed(rec, 60)

	recr := rec.Result()
	recrb, _ := ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusMethodNotAllowed, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))
	assert.Equal(t, "method not allowed", string(recrb))
}

func TestResponseInternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()

	responseInternalServerError(rec)

	recr := rec.Result()
	recrb, _ := ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusInternalServerError, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Empty(t, recr.Header.Get("Cache-Control"))
	assert.Equal(t, "internal server error", string(recrb))
}

func TestResponseModError(t *testing.T) {
	rec := httptest.NewRecorder()

	responseModError(rec, notFoundError("cache insensitive"), false)

	recr := rec.Result()
	recrb, _ := ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=600", recr.Header.Get("Cache-Control"))
	assert.Equal(t, "not found: cache insensitive", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseModError(rec, notFoundError("cache sensitive"), true)

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(t, "public, max-age=60", recr.Header.Get("Cache-Control"))
	assert.Equal(t, "not found: cache sensitive", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseModError(rec, notFoundError("not found: bad upstream"), false)

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(
		t,
		"must-revalidate, no-cache, no-store",
		recr.Header.Get("Cache-Control"),
	)
	assert.Equal(t, "not found: bad upstream", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseModError(
		rec,
		notFoundError("not found: fetch timed out"),
		false,
	)

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(
		t,
		"must-revalidate, no-cache, no-store",
		recr.Header.Get("Cache-Control"),
	)
	assert.Equal(t, "not found: fetch timed out", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseModError(rec, errBadUpstream, false)

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(
		t,
		"must-revalidate, no-cache, no-store",
		recr.Header.Get("Cache-Control"),
	)
	assert.Equal(t, "not found: bad upstream", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseModError(rec, errFetchTimedOut, false)

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusNotFound, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Equal(
		t,
		"must-revalidate, no-cache, no-store",
		recr.Header.Get("Cache-Control"),
	)
	assert.Equal(t, "not found: fetch timed out", string(recrb))

	// ---

	rec = httptest.NewRecorder()

	responseModError(rec, errors.New("internal server error"), false)

	recr = rec.Result()
	recrb, _ = ioutil.ReadAll(recr.Body)

	assert.Equal(t, http.StatusInternalServerError, recr.StatusCode)
	assert.Equal(
		t,
		"text/plain; charset=utf-8",
		recr.Header.Get("Content-Type"),
	)
	assert.Empty(t, recr.Header.Get("Cache-Control"))
	assert.Equal(t, "internal server error", string(recrb))
}
