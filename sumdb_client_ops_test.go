package goproxy

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSUMDBClientOps(t *testing.T) {
	sco := &sumdbClientOps{}
	sco.init()
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{envGOSUMDB: "example.com foo bar"}
	sco.init()
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "invalid GOSUMDB: too many fields"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.org",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if got := string(sco.key); got != sumGolangOrgKey {
		t.Errorf("got %q, want %q", got, sumGolangOrgKey)
	}

	sco = &sumdbClientOps{
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.google.cn",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if got := string(sco.key); got != sumGolangOrgKey {
		t.Errorf("got %q, want %q", got, sumGolangOrgKey)
	}

	sco = &sumdbClientOps{envGOSUMDB: "example.com ://invalid"}
	sco.init()
	if sco.initError == nil {
		t.Fatal("expected error")
	}

	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		handlerFunc(rw, req)
	}))
	defer server.Close()
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL,
		envGOSUMDB: "example.com",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if got, want := sco.endpointURL.String(), server.URL+"/sumdb/example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL,
		envGOSUMDB: "example.com",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if got, want := sco.endpointURL.String(), "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL + ",direct",
		envGOSUMDB: "example.com",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if got, want := sco.endpointURL.String(), "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL + ",off",
		envGOSUMDB: "example.com",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if got, want := sco.endpointURL.String(), "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL,
		envGOSUMDB: "example.com",
	}
	sco.init()
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "bad upstream"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: "://invalid",
		envGOSUMDB: "example.com",
	}
	sco.init()
	if sco.initError == nil {
		t.Fatal("expected error")
	}

	sco = &sumdbClientOps{}
	if _, err := sco.ReadRemote(""); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{}
	if _, err := sco.ReadConfig(""); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{}
	if err := sco.WriteConfig("", nil, nil); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{}
	if _, err := sco.ReadCache(""); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{}
	sco.WriteCache("", nil)
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{}
	sco.Log("")
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{}
	sco.SecurityError("")
	if sco.initError == nil {
		t.Fatal("expected error")
	}
	if got, want := sco.initError.Error(), "missing GOSUMDB"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		fmt.Fprint(rw, "foobar")
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL,
		envGOSUMDB: "sum.golang.org",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if b, err := sco.ReadRemote(""); err != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	} else if got, want := string(b), "foobar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/sumdb/sum.golang.org/supported" {
			rw.WriteHeader(http.StatusOK)
		} else {
			rw.WriteHeader(http.StatusInternalServerError)
		}
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL,
		envGOSUMDB: "sum.golang.org",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if _, err := sco.ReadRemote(""); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "bad upstream"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.org",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if b, err := sco.ReadConfig("key"); err != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	} else if got := string(b); got != sumGolangOrgKey {
		t.Errorf("got %q, want %q", got, sumGolangOrgKey)
	}

	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.org",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if b, err := sco.ReadConfig("/latest"); err != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	} else if got, want := string(b), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.org",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if _, err := sco.ReadConfig("/"); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "unknown config /"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.org",
	}
	sco.init()
	if sco.initError != nil {
		t.Fatalf("unexpected error %q", sco.initError)
	}
	if _, err := sco.ReadCache(""); err == nil {
		t.Fatal("expected error")
	} else if got, want := err, fs.ErrNotExist; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}
