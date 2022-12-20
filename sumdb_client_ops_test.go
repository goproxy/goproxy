package goproxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSUMDBClientOps(t *testing.T) {
	sco := &sumdbClientOps{}
	sco.load()
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	wantLoadError := "missing GOSUMDB"
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	sco = &sumdbClientOps{envGOSUMDB: "example.com foo bar"}
	sco.load()
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	wantLoadError = "invalid GOSUMDB: too many fields"
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	sco = &sumdbClientOps{
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.org",
	}
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	wantKey := "sum.golang.org" +
		"+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8"
	if got := string(sco.key); got != wantKey {
		t.Errorf("got %q, want %q", got, wantKey)
	}

	sco = &sumdbClientOps{
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.google.cn",
	}
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	if got := string(sco.key); got != wantKey {
		t.Errorf("got %q, want %q", got, wantKey)
	}

	sco = &sumdbClientOps{envGOSUMDB: "example.com foobar"}
	sco.load()
	if sco.loadError == nil {
		t.Fatal("expected error")
	}

	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		handlerFunc(rw, req)
	}))
	defer server.Close()
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL,
		envGOSUMDB: "example.com",
	}
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	wantEndpointURL := server.URL + "/sumdb/example.com"
	if got := sco.endpointURL.String(); got != wantEndpointURL {
		t.Errorf("got %q, want %q", got, wantEndpointURL)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL,
		envGOSUMDB: "example.com",
	}
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	wantEndpointURL = "https://example.com"
	if got := sco.endpointURL.String(); got != wantEndpointURL {
		t.Errorf("got %q, want %q", got, wantEndpointURL)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL + ",direct",
		envGOSUMDB: "example.com",
	}
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	wantEndpointURL = "https://example.com"
	if got := sco.endpointURL.String(); got != wantEndpointURL {
		t.Errorf("got %q, want %q", got, wantEndpointURL)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL + ",off",
		envGOSUMDB: "example.com",
	}
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	wantEndpointURL = "https://example.com"
	if got := sco.endpointURL.String(); got != wantEndpointURL {
		t.Errorf("got %q, want %q", got, wantEndpointURL)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL,
		envGOSUMDB: "example.com",
	}
	sco.load()
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	wantLoadError = "bad upstream"
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: "foobar",
		envGOSUMDB: "example.com",
	}
	sco.load()
	if sco.loadError == nil {
		t.Fatal("expected error")
	}

	sco = &sumdbClientOps{}
	sco.ReadRemote("")
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	wantLoadError = "missing GOSUMDB"
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: "foobar",
		envGOSUMDB: "example.com",
	}
	sco.load()
	if sco.loadError == nil {
		t.Fatal("expected error")
	}

	sco = &sumdbClientOps{}
	sco.ReadConfig("")
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	sco = &sumdbClientOps{}
	if err := sco.WriteConfig("", nil, nil); err == nil {
		t.Fatal("expected error")
	} else if got := err.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	sco = &sumdbClientOps{}
	if _, err := sco.ReadCache(""); err == nil {
		t.Fatal("expected error")
	} else if got := err.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	sco = &sumdbClientOps{}
	sco.WriteCache("", nil)
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	sco = &sumdbClientOps{}
	sco.Log("")
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	sco = &sumdbClientOps{}
	sco.SecurityError("")
	if sco.loadError == nil {
		t.Fatal("expected error")
	}
	if got := sco.loadError.Error(); got != wantLoadError {
		t.Errorf("got %q, want %q", got, wantLoadError)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		fmt.Fprint(rw, "foobar")
	}
	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: server.URL,
		envGOSUMDB: "sum.golang.org",
	}
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	if b, err := sco.ReadRemote(""); err != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
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
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
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
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	if b, err := sco.ReadConfig("key"); err != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	} else if got := string(b); got != wantKey {
		t.Errorf("got %q, want %q", got, wantKey)
	}

	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.org",
	}
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	if b, err := sco.ReadConfig("/latest"); err != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	} else if got, want := string(b), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	sco = &sumdbClientOps{
		httpClient: http.DefaultClient,
		envGOPROXY: "direct",
		envGOSUMDB: "sum.golang.org",
	}
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
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
	sco.load()
	if sco.loadError != nil {
		t.Fatalf("unexpected error %q", sco.loadError)
	}
	if _, err := sco.ReadCache(""); err == nil {
		t.Fatal("expected error")
	} else if got, want := err, os.ErrNotExist; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}
