package goproxy

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSetResponseCacheControlHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	setResponseCacheControlHeader(rec, 60)
	recr := rec.Result()
	recrCC := recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	rec = httptest.NewRecorder()
	setResponseCacheControlHeader(rec, 0)
	recr = rec.Result()
	recrCC = recr.Header.Get("Cache-Control")
	if want := "public, max-age=0"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	rec = httptest.NewRecorder()
	setResponseCacheControlHeader(rec, -1)
	recr = rec.Result()
	recrCC = recr.Header.Get("Cache-Control")
	if want := "must-revalidate, no-cache, no-store"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	rec = httptest.NewRecorder()
	setResponseCacheControlHeader(rec, -2)
	recr = rec.Result()
	recrCC = recr.Header.Get("Cache-Control")
	if want := ""; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}
}

func TestResponseString(t *testing.T) {
	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	responseString(rec, req, http.StatusOK, 60, "foobar")
	recr := rec.Result()
	if want := http.StatusOK; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT := recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC := recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "foobar"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}
}

func TestResponseNotFound(t *testing.T) {
	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	responseNotFound(rec, req, 60)
	recr := rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT := recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC := recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseNotFound(rec, req, 60, "foobar")
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: foobar"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseNotFound(rec, req, 60, "bad request: foobar")
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: foobar"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseNotFound(rec, req, 60, "not found: foobar")
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: foobar"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseNotFound(rec, req, 60, "gone: foobar")
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: foobar"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseNotFound(rec, req, 60, "not found")
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}
}

func TestResponseMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	responseMethodNotAllowed(rec, req, 60)
	recr := rec.Result()
	if want := http.StatusMethodNotAllowed; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT := recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC := recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "method not allowed"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}
}

func TestResponseInternalServerError(t *testing.T) {
	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	responseInternalServerError(rec, req)
	recr := rec.Result()
	if want := http.StatusInternalServerError; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT := recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC := recr.Header.Get("Cache-Control")
	if want := ""; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "internal server error"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}
}

type successResponseBody struct {
	io.Reader

	checksum []byte
	modTime  time.Time
}

func (srb successResponseBody) Checksum() []byte {
	return srb.checksum
}

func (srb successResponseBody) ModTime() time.Time {
	return srb.modTime
}

func TestResponseSuccess(t *testing.T) {
	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	responseSuccess(
		rec,
		req,
		strings.NewReader("foobar"),
		"text/plain; charset=utf-8",
		60,
	)
	recr := rec.Result()
	if want := http.StatusOK; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT := recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC := recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	recrLM := recr.Header.Get("Last-Modified")
	if want := ""; recrLM != want {
		t.Errorf("got %q, want %q", recrLM, want)
	}

	recrET := recr.Header.Get("ETag")
	if want := ""; recrET != want {
		t.Errorf("got %q, want %q", recrET, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "foobar"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	req = httptest.NewRequest(http.MethodHead, "/", nil)
	rec = httptest.NewRecorder()
	responseSuccess(
		rec,
		req,
		bytes.NewBuffer([]byte("foobar")),
		"text/plain; charset=utf-8",
		60,
	)
	recr = rec.Result()
	if want := http.StatusOK; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	recrLM = recr.Header.Get("Last-Modified")
	if want := ""; recrLM != want {
		t.Errorf("got %q, want %q", recrLM, want)
	}

	recrET = recr.Header.Get("ETag")
	if want := ""; recrET != want {
		t.Errorf("got %q, want %q", recrET, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := ""; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	req = httptest.NewRequest("", "/", nil)
	rec = httptest.NewRecorder()
	responseSuccess(
		rec,
		req,
		successResponseBody{
			Reader:   strings.NewReader("foobar"),
			checksum: []byte{0, 1, 2, 3},
			modTime:  time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		"text/plain; charset=utf-8",
		60,
	)
	recr = rec.Result()
	if want := http.StatusOK; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	recrLM = recr.Header.Get("Last-Modified")
	if want := "Sat, 01 Jan 2000 00:00:00 GMT"; recrLM != want {
		t.Errorf("got %q, want %q", recrLM, want)
	}

	recrET = recr.Header.Get("ETag")
	if want := `"AAECAw=="`; recrET != want {
		t.Errorf("got %q, want %q", recrET, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "foobar"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}
}

func TestResponseError(t *testing.T) {
	req := httptest.NewRequest("", "/", nil)
	rec := httptest.NewRecorder()
	responseError(rec, req, notFoundError("cache insensitive"), false)
	recr := rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT := recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC := recr.Header.Get("Cache-Control")
	if want := "public, max-age=600"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: cache insensitive"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseError(rec, req, notFoundError("cache sensitive"), true)
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: cache sensitive"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseError(rec, req, notFoundError("not found: bad upstream"), false)
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "must-revalidate, no-cache, no-store"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: bad upstream"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseError(
		rec,
		req,
		notFoundError("not found: fetch timed out"),
		false,
	)
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "must-revalidate, no-cache, no-store"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: fetch timed out"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseError(rec, req, errBadUpstream, false)
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "must-revalidate, no-cache, no-store"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: bad upstream"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseError(rec, req, errFetchTimedOut, false)
	recr = rec.Result()
	if want := http.StatusNotFound; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := "must-revalidate, no-cache, no-store"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "not found: fetch timed out"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}

	rec = httptest.NewRecorder()
	responseError(rec, req, errors.New("internal server error"), false)
	recr = rec.Result()
	if want := http.StatusInternalServerError; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT = recr.Header.Get("Content-Type")
	if want := "text/plain; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC = recr.Header.Get("Cache-Control")
	if want := ""; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := "internal server error"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}
}
