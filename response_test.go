package goproxy

import (
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
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
	rec := httptest.NewRecorder()
	responseString(rec, http.StatusOK, 60, "foobar")
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

func TestResponseJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	responseJSON(rec, http.StatusOK, 60, []byte(`{"foo":"bar"}`))
	recr := rec.Result()
	if want := http.StatusOK; recr.StatusCode != want {
		t.Errorf("got %d, want %d", recr.StatusCode, want)
	}

	recrCT := recr.Header.Get("Content-Type")
	if want := "application/json; charset=utf-8"; recrCT != want {
		t.Errorf("got %q, want %q", recrCT, want)
	}

	recrCC := recr.Header.Get("Cache-Control")
	if want := "public, max-age=60"; recrCC != want {
		t.Errorf("got %q, want %q", recrCC, want)
	}

	if b, err := ioutil.ReadAll(recr.Body); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if want := `{"foo":"bar"}`; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}
}

func TestResponseNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	responseNotFound(rec, 60)
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
	responseNotFound(rec, 60, "foobar")
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
	responseNotFound(rec, 60, "bad request: foobar")
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
	responseNotFound(rec, 60, "not found: foobar")
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
	responseNotFound(rec, 60, "gone: foobar")
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
}

func TestResponseMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	responseMethodNotAllowed(rec, 60)
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
	rec := httptest.NewRecorder()
	responseInternalServerError(rec)
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

func TestResponseModError(t *testing.T) {
	rec := httptest.NewRecorder()
	responseModError(rec, notFoundError("cache insensitive"), false)
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
	responseModError(rec, notFoundError("cache sensitive"), true)
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
	responseModError(rec, notFoundError("not found: bad upstream"), false)
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
	responseModError(
		rec,
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
	responseModError(rec, errBadUpstream, false)
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
	responseModError(rec, errFetchTimedOut, false)
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
	responseModError(rec, errors.New("internal server error"), false)
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
