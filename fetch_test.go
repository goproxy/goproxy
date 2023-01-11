package goproxy

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/sumdb/note"
)

func TestNewFetch(t *testing.T) {
	g := &Goproxy{}
	g.init()
	name := "example.com/foo/bar/@latest"
	tempDir := "tempDir"
	f, err := newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.ops, fetchOpsResolve; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if f.name != name {
		t.Errorf("got %q, want %q", f.name, name)
	}
	if f.tempDir != tempDir {
		t.Errorf("got %q, want %q", f.tempDir, tempDir)
	}
	if got, want := f.modulePath, "example.com/foo/bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.moduleVersion, "latest"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.modAtVer, "example.com/foo/bar@latest"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.requiredToVerify, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	wantContentType := "application/json; charset=utf-8"
	if got := f.contentType; got != wantContentType {
		t.Errorf("got %q, want %q", got, wantContentType)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOSUMDB=off"}
	g.init()
	name = "example.com/foo/bar/@latest"
	f, err = newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.requiredToVerify, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GONOSUMDB=example.com"}
	g.init()
	name = "example.com/foo/bar/@latest"
	f, err = newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.requiredToVerify, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPRIVATE=example.com"}
	g.init()
	name = "example.com/foo/bar/@latest"
	f, err = newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.requiredToVerify, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	name = "example.com/foo/bar/@v/list"
	f, err = newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.ops, fetchOpsList; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.modulePath, "example.com/foo/bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.moduleVersion, "latest"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	wantContentType = "text/plain; charset=utf-8"
	if got := f.contentType; got != wantContentType {
		t.Errorf("got %q, want %q", got, wantContentType)
	}

	name = "example.com/foo/bar/@v/v1.0.0.info"
	f, err = newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.ops, fetchOpsDownloadInfo; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.modulePath, "example.com/foo/bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.moduleVersion, "v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	wantContentType = "application/json; charset=utf-8"
	if got := f.contentType; got != wantContentType {
		t.Errorf("got %q, want %q", got, wantContentType)
	}

	name = "example.com/foo/bar/@v/v1.0.0.mod"
	f, err = newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.ops, fetchOpsDownloadMod; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.modulePath, "example.com/foo/bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.moduleVersion, "v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	wantContentType = "text/plain; charset=utf-8"
	if got := f.contentType; got != wantContentType {
		t.Errorf("got %q, want %q", got, wantContentType)
	}

	name = "example.com/foo/bar/@v/v1.0.0.zip"
	f, err = newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.ops, fetchOpsDownloadZip; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.modulePath, "example.com/foo/bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.moduleVersion, "v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	wantContentType = "application/zip"
	if got := f.contentType; got != wantContentType {
		t.Errorf("got %q, want %q", got, wantContentType)
	}

	name = "example.com/foo/bar/@v/v1.0.0.ext"
	if _, err := newFetch(g, name, tempDir); err == nil {
		t.Fatal("expected error")
	} else if want := `unexpected extension ".ext"`; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}

	name = "example.com/foo/bar/@v/latest.info"
	if _, err := newFetch(g, name, tempDir); err == nil {
		t.Fatal("expected error")
	} else if want := "invalid version"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}

	name = "example.com/foo/bar/@v/master.info"
	f, err = newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.ops, fetchOpsResolve; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.modulePath, "example.com/foo/bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.moduleVersion, "master"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	wantContentType = "application/json; charset=utf-8"
	if got := f.contentType; got != wantContentType {
		t.Errorf("got %q, want %q", got, wantContentType)
	}

	name = "example.com/foo/bar/@v/master.mod"
	if _, err := newFetch(g, name, tempDir); err == nil {
		t.Fatal("expected error")
	} else if want := "unrecognized version"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}

	name = "example.com/foo/bar/@v/master.zip"
	if _, err := newFetch(g, name, tempDir); err == nil {
		t.Fatal("expected error")
	} else if want := "unrecognized version"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}

	name = "example.com/foo/bar"
	if _, err := newFetch(g, name, tempDir); err == nil {
		t.Fatal("expected error")
	} else if want := "missing /@v/"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}

	name = "example.com/foo/bar/@v/"
	if _, err := newFetch(g, name, tempDir); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		`no file extension in filename ""`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	name = "example.com/foo/bar/@v/main"
	if _, err := newFetch(g, name, tempDir); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		`no file extension in filename "main"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	name = "example.com/!foo/bar/@v/!v1.0.0.info"
	f, err = newFetch(g, name, tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := f.ops, fetchOpsResolve; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.modulePath, "example.com/Foo/bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := f.moduleVersion, "V1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	wantContentType = "application/json; charset=utf-8"
	if got := f.contentType; got != wantContentType {
		t.Errorf("got %q, want %q", got, wantContentType)
	}

	name = "example.com/!!foo/bar/@latest"
	if _, err := newFetch(g, name, tempDir); err == nil {
		t.Fatal("expected error")
	}

	name = "example.com/foo/bar/@v/!!v1.0.0.info"
	if _, err := newFetch(g, name, tempDir); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchDo(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestFetchDo")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	goproxyHandlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(
			rw,
			req,
			strings.NewReader(marshalInfo("v1.0.0", infoTime)),
			"application/json; charset=utf-8",
			60,
		)
	}
	goproxyServer := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		goproxyHandlerFunc(rw, req)
	}))
	defer goproxyServer.Close()

	g := &Goproxy{
		GoBinEnv: []string{
			"GOPROXY=" + goproxyServer.URL,
			"GOSUMDB=off",
		},
	}
	g.init()
	f, err := newFetch(g, "example.com/@latest", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err := f.do(context.Background())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := fr.Version, "v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := fr.Time.String(),
		infoTime.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		GoBinEnv: []string{
			"GOPATH=" + tempDir,
			"GOPROXY=off",
			"GONOPROXY=example.com",
			"GOSUMDB=off",
		},
	}
	g.init()
	g.goBinEnv = append(g.goBinEnv, "GOPROXY=off")
	f, err = newFetch(g, "example.com/@latest", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.do(context.Background()); err == nil {
		t.Fatal("expected error")
	}

	goproxyHandlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		responseNotFound(rw, req, 60)
	}
	g = &Goproxy{
		GoBinEnv: []string{
			"GOPATH=" + tempDir,
			"GOPROXY=" + goproxyServer.URL + ",direct",
			"GOSUMDB=off",
		},
	}
	g.init()
	g.goBinEnv = append(g.goBinEnv, "GOPROXY=off")
	f, err = newFetch(g, "example.com/@latest", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.do(context.Background()); err == nil {
		t.Fatal("expected error")
	}

	g = &Goproxy{
		GoBinEnv: []string{
			"GOPROXY=off",
			"GOSUMDB=off",
		},
	}
	g.init()
	f, err = newFetch(g, "example.com/@latest", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.do(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchDoProxy(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestFetchDoProxy")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	now := time.Now()

	handlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(
			rw,
			req,
			strings.NewReader(marshalInfo("v1.0.0", now)),
			"application/json; charset=utf-8",
			60,
		)
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		handlerFunc(rw, req)
	}))
	defer server.Close()

	g := &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err := newFetch(g, "example.com/@latest", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err := f.doProxy(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := fr.Version, "v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := fr.Time, now; !got.Equal(want) {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(
			rw,
			req,
			strings.NewReader(marshalInfo("v1.0.0", time.Time{})),
			"application/json; charset=utf-8",
			60,
		)
	}
	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@latest", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doProxy(context.Background(), server.URL); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"invalid info response: zero time"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(
			rw,
			req,
			strings.NewReader(`v1.0.0
v1.1.0
v1.1.1-0.20200101000000-0123456789ab
v1.2.0 foo bar
invalid
`),
			"text/plain; charset=utf-8",
			60,
		)
	}
	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@v/list", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doProxy(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if got, want := strings.Join(fr.Versions, "\n"),
		"v1.0.0\nv1.1.0\nv1.2.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(
			rw,
			req,
			strings.NewReader(marshalInfo("v1.0.0", now)),
			"application/json; charset=utf-8",
			60,
		)
	}
	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doProxy(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if info, err := ioutil.ReadFile(fr.Info); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(info), fmt.Sprintf(
		`{"Version":"v1.0.0","Time":%q}`,
		now.UTC().Format(time.RFC3339Nano),
	); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(
			rw,
			req,
			strings.NewReader(marshalInfo("v1.0.0", time.Time{})),
			"application/json; charset=utf-8",
			60,
		)
	}
	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doProxy(context.Background(), server.URL); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"invalid info file: zero time"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(
			rw,
			req,
			strings.NewReader("module example.com"),
			"text/plain; charset=utf-8",
			60,
		)
	}
	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@v/v1.0.0.mod", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doProxy(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if mod, err := ioutil.ReadFile(fr.GoMod); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(mod), "module example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	modFile := filepath.Join(tempDir, "go.mod")
	if err := ioutil.WriteFile(
		modFile,
		[]byte("module example.com"),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	dirHash, err := dirhash.HashDir(
		tempDir,
		"example.com@v1.0.0",
		dirhash.DefaultHash,
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	modHash, err := dirhash.DefaultHash(
		[]string{"go.mod"},
		func(string) (io.ReadCloser, error) {
			return os.Open(modFile)
		},
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	skey, vkey, err := note.GenerateKey(nil, "sumdb.example.com")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	gosum := func(modulePath, moduleVersion string) ([]byte, error) {
		return []byte(fmt.Sprintf(
			"%s %s %s\n%s %s/go.mod %s\n",
			modulePath,
			moduleVersion,
			dirHash,
			modulePath,
			moduleVersion,
			modHash,
		)), nil
	}
	sumdbServer := httptest.NewServer(sumdb.NewServer(sumdb.NewTestServer(
		skey,
		func(modulePath, moduleVersion string) ([]byte, error) {
			return gosum(modulePath, moduleVersion)
		},
	)))
	defer sumdbServer.Close()

	g = &Goproxy{GoBinEnv: []string{
		"GOPROXY=off",
		"GOSUMDB=" + vkey + " " + sumdbServer.URL,
	}}
	g.init()
	f, err = newFetch(g, "example.com/@v/v1.0.0.mod", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doProxy(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if mod, err := ioutil.ReadFile(fr.GoMod); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(mod), "module example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	gosum = func(modulePath, moduleVersion string) ([]byte, error) {
		return []byte(fmt.Sprintf(
			"%s %s %s\n%s %s/go.mod %s\n",
			modulePath,
			"v1.0.0",
			dirHash,
			modulePath,
			"v1.0.0",
			modHash,
		)), nil
	}
	f, err = newFetch(g, "example.com/@v/v1.1.0.mod", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doProxy(context.Background(), server.URL); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"example.com@v1.1.0: invalid version: untrusted revision "+
			"v1.1.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(
			rw,
			req,
			strings.NewReader("Go 1.13\n"),
			"text/plain; charset=utf-8",
			60,
		)
	}
	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@v/v1.0.0.mod", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doProxy(context.Background(), server.URL); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"invalid mod file: missing module directive"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	tempFile, err := ioutil.TempFile(tempDir, "")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipWriter := zip.NewWriter(tempFile)
	if zfw, err := zipWriter.Create(
		"example.com@v1.2.0/go.mod",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := zfw.Write(
		[]byte("module example.com"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := zipWriter.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := tempFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		tempFile, err := os.Open(tempFile.Name())
		if err != nil {
			t.Fatalf("unexpected error %q", err)
		}
		defer tempFile.Close()
		responseSuccess(rw, req, tempFile, "application/zip", 60)
	}
	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@v/v1.2.0.zip", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doProxy(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if fr.Zip == "" {
		t.Fatal("unexpected empty")
	}

	dirHash, err = dirhash.HashZip(tempFile.Name(), dirhash.DefaultHash)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	gosum = func(modulePath, moduleVersion string) ([]byte, error) {
		return []byte(fmt.Sprintf(
			"%s %s %s\n%s %s/go.mod %s\n",
			modulePath,
			moduleVersion,
			dirHash,
			modulePath,
			moduleVersion,
			modHash,
		)), nil
	}
	g = &Goproxy{GoBinEnv: []string{
		"GOPROXY=off",
		"GOSUMDB=" + vkey + " " + sumdbServer.URL,
	}}
	g.init()
	f, err = newFetch(g, "example.com/@v/v1.2.0.zip", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doProxy(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if fr.Zip == "" {
		t.Fatal("unexpected empty")
	}
	gosum = func(modulePath, moduleVersion string) ([]byte, error) {
		return []byte(fmt.Sprintf(
			"%s %s %s\n%s %s/go.mod %s\n",
			modulePath,
			"v1.2.0",
			dirHash,
			modulePath,
			"v1.2.0",
			modHash,
		)), nil
	}
	tempFile, err = os.OpenFile(
		tempFile.Name(),
		os.O_WRONLY|os.O_TRUNC,
		0600,
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipWriter = zip.NewWriter(tempFile)
	if zfw, err := zipWriter.Create(
		"example.com@v1.3.0/go.mod",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := zfw.Write(
		[]byte("module example.com"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := zipWriter.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := tempFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	f, err = newFetch(g, "example.com/@v/v1.3.0.zip", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doProxy(context.Background(), server.URL); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"example.com@v1.3.0: invalid version: untrusted revision "+
			"v1.3.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		responseSuccess(
			rw,
			req,
			strings.NewReader("I'm a ZIP file!"),
			"application/zip",
			60,
		)
	}
	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@v/v1.0.0.zip", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doProxy(context.Background(), server.URL); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"invalid zip file: zip: not a valid zip file"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@latest", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doProxy(context.Background(), "://invalid"); err == nil {
		t.Fatal("expected error")
	}

	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@latest", filepath.Join(tempDir, "_"))
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doProxy(context.Background(), server.URL); err == nil {
		t.Fatal("expected error")
	}

	handlerFunc = func(rw http.ResponseWriter, req *http.Request) {
		responseNotFound(rw, req, 60)
	}
	g = &Goproxy{
		GoBinEnv: []string{"GOSUMDB=off"},
	}
	g.init()
	f, err = newFetch(g, "example.com/@latest", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doProxy(context.Background(), server.URL); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFetchDoDirect(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestFetchDoDirect")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	gopathDir := filepath.Join(tempDir, "gopath")

	staticGOPROXYDir := filepath.Join(tempDir, "static-goproxy")
	if err := os.MkdirAll(
		filepath.Join(staticGOPROXYDir, "example.com", "@v"),
		0700,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	infoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := ioutil.WriteFile(
		filepath.Join(staticGOPROXYDir, "example.com", "@latest"),
		[]byte(marshalInfo("v1.1.0", infoTime)),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if err := ioutil.WriteFile(
		filepath.Join(staticGOPROXYDir, "example.com", "@v", "list"),
		[]byte("v1.1.0\nv1.0.0"),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if err := ioutil.WriteFile(
		filepath.Join(
			staticGOPROXYDir,
			"example.com",
			"@v",
			"v1.0.0.info",
		),
		[]byte(marshalInfo("v1.0.0", infoTime)),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if err := ioutil.WriteFile(
		filepath.Join(
			staticGOPROXYDir,
			"example.com",
			"@v",
			"v1.1.0.info",
		),
		[]byte(marshalInfo("v1.1.0", infoTime.Add(time.Hour))),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	mod := "module example.com"
	if err := ioutil.WriteFile(
		filepath.Join(
			staticGOPROXYDir,
			"example.com",
			"@v",
			"v1.0.0.mod",
		),
		[]byte(mod),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if err := ioutil.WriteFile(
		filepath.Join(
			staticGOPROXYDir,
			"example.com",
			"@v",
			"v1.1.0.mod",
		),
		[]byte(mod),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipFile, err := os.Create(filepath.Join(
		staticGOPROXYDir,
		"example.com",
		"@v",
		"v1.0.0.zip",
	))
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipWriter := zip.NewWriter(zipFile)
	if zfw, err := zipWriter.Create(
		"example.com@v1.0.0/go.mod",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := zfw.Write([]byte(mod)); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := zipWriter.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := zipFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipFileBytes, err := ioutil.ReadFile(zipFile.Name())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if err := ioutil.WriteFile(
		filepath.Join(
			staticGOPROXYDir,
			"example.com",
			"@v",
			"v1.1.0.zip",
		),
		zipFileBytes,
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	goproxyHandlerFunc := func(rw http.ResponseWriter, req *http.Request) {
		http.FileServer(http.Dir(staticGOPROXYDir)).ServeHTTP(rw, req)
	}
	goproxyServer := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		goproxyHandlerFunc(rw, req)
	}))
	defer goproxyServer.Close()

	g := &Goproxy{
		GoBinMaxWorkers: 1,
		GoBinEnv: append(
			os.Environ(),
			"GOPATH="+gopathDir,
			"GOSUMDB=off",
		),
	}
	g.init()
	g.goBinEnv = append(g.goBinEnv, "GOPROXY="+goproxyServer.URL)
	f, err := newFetch(g, "example.com/@latest", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err := f.doDirect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := fr.Version, "v1.1.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := fr.Time.String(),
		infoTime.Add(time.Hour).String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	f, err = newFetch(g, "example.com/@v/list", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doDirect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := strings.Join(fr.Versions, "\n"),
		"v1.0.0\nv1.1.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	f, err = newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doDirect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if fr.Info == "" {
		t.Fatal("unexpected empty")
	}
	f, err = newFetch(g, "example.com/@v/v1.0.0.mod", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doDirect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if fr.GoMod == "" {
		t.Fatal("unexpected empty")
	}
	f, err = newFetch(g, "example.com/@v/v1.0.0.zip", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doDirect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if fr.Zip == "" {
		t.Fatal("unexpected empty")
	}
	f, err = newFetch(g, "example.com/@v/v1.1.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doDirect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	f, err = newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.doDirect(canceledCtx); err == nil {
		t.Fatal("expected error")
	}
	f, err = newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	timedOutCtx, cancel := context.WithDeadline(
		context.Background(),
		time.Time{},
	)
	defer cancel()
	if _, err := f.doDirect(timedOutCtx); err == nil {
		t.Fatal("expected error")
	}
	f, err = newFetch(g, "example.com/@v/v1.2.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doDirect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	f, err = newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	f.modAtVer = "invalid"
	if _, err := f.doDirect(context.Background()); err == nil {
		t.Fatal("expected error")
	}

	dirHash, err := dirhash.HashZip(zipFile.Name(), dirhash.DefaultHash)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	modHash, err := dirhash.DefaultHash(
		[]string{"go.mod"},
		func(string) (io.ReadCloser, error) {
			return &nopCloser{strings.NewReader(mod)}, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	skey, vkey, err := note.GenerateKey(nil, "sumdb.example.com")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	sumdbHandler := sumdb.NewServer(sumdb.NewTestServer(
		skey,
		func(modulePath, moduleVersion string) ([]byte, error) {
			return []byte(fmt.Sprintf(
				"%s %s %s\n%s %s/go.mod %s\n",
				modulePath,
				moduleVersion,
				dirHash,
				modulePath,
				moduleVersion,
				modHash,
			)), nil
		},
	))
	sumdbServer := httptest.NewServer(http.HandlerFunc(func(
		rw http.ResponseWriter,
		req *http.Request,
	) {
		sumdbHandler.ServeHTTP(rw, req)
	}))
	defer sumdbServer.Close()

	g = &Goproxy{
		GoBinMaxWorkers: 1,
		GoBinEnv: append(
			os.Environ(),
			"GOPATH="+gopathDir,
			"GOPROXY=off",
			"GOSUMDB="+vkey+" "+sumdbServer.URL,
		),
	}
	g.init()
	g.goBinEnv = append(g.goBinEnv, "GOPROXY="+goproxyServer.URL)
	f, err = newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr, err = f.doDirect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if fr.Info == "" {
		t.Fatal("unexpected empty")
	}

	sumdbHandler = sumdb.NewServer(sumdb.NewTestServer(
		skey,
		func(modulePath, moduleVersion string) ([]byte, error) {
			return []byte(fmt.Sprintf(
				"%s %s %s\n%s %s/go.mod %s\n",
				modulePath,
				moduleVersion,
				modHash,
				modulePath,
				moduleVersion,
				dirHash,
			)), nil
		},
	))
	g = &Goproxy{
		GoBinMaxWorkers: 1,
		GoBinEnv: append(
			os.Environ(),
			"GOPATH="+gopathDir,
			"GOPROXY=off",
			"GOSUMDB="+vkey+" "+sumdbServer.URL,
		),
	}
	g.init()
	g.goBinEnv = append(g.goBinEnv, "GOPROXY="+goproxyServer.URL)
	f, err = newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doDirect(context.Background()); err == nil {
		t.Fatal("expected error")
	}

	sumdbHandler = sumdb.NewServer(sumdb.NewTestServer(
		skey,
		func(modulePath, moduleVersion string) ([]byte, error) {
			return []byte(fmt.Sprintf(
				"%s %s %s\n%s %s/go.mod %s\n",
				modulePath,
				moduleVersion,
				modHash,
				modulePath,
				moduleVersion,
				modHash,
			)), nil
		},
	))
	g = &Goproxy{
		GoBinMaxWorkers: 1,
		GoBinEnv: append(
			os.Environ(),
			"GOPATH="+gopathDir,
			"GOPROXY=off",
			"GOSUMDB="+vkey+" "+sumdbServer.URL,
		),
	}
	g.init()
	g.goBinEnv = append(g.goBinEnv, "GOPROXY="+goproxyServer.URL)
	f, err = newFetch(g, "example.com/@v/v1.0.0.info", tempDir)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	if _, err := f.doDirect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchOpsString(t *testing.T) {
	fo := fetchOpsResolve
	if got, want := fo.String(), "resolve"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	fo = fetchOpsList
	if got, want := fo.String(), "list"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	fo = fetchOpsDownloadInfo
	if got, want := fo.String(), "download info"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	fo = fetchOpsDownloadMod
	if got, want := fo.String(), "download mod"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	fo = fetchOpsDownloadZip
	if got, want := fo.String(), "download zip"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	fo = fetchOpsInvalid
	if got, want := fo.String(), "invalid"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	fo = fetchOps(255)
	if got, want := fo.String(), "invalid"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFetchResultOpen(t *testing.T) {
	resolvedInfo := `{"Version":"v1.0.0","Time":"0001-01-01T00:00:00Z"}`
	versionList := "v1.0.0\nv1.1.0"
	goMod := "module goproxy.local\n\nGo 1.13\n"

	fr := &fetchResult{f: &fetch{ops: fetchOpsInvalid}}
	if rsc, err := fr.Open(); err == nil {
		t.Fatal("expected error")
	} else if want := "invalid fetch operation"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	} else if rsc != nil {
		t.Errorf("got %v, want nil", rsc)
	}

	fr = &fetchResult{f: &fetch{ops: fetchOpsResolve}, Version: "v1.0.0"}
	if rsc, err := fr.Open(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if rsc == nil {
		t.Fatal("unexpected nil")
	} else if got, err := ioutil.ReadAll(rsc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if string(got) != resolvedInfo {
		t.Errorf("got %q, want %q", got, resolvedInfo)
	}

	fr = &fetchResult{
		f:        &fetch{ops: fetchOpsList},
		Versions: []string{"v1.0.0", "v1.1.0"},
	}
	if rsc, err := fr.Open(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if rsc == nil {
		t.Fatal("unexpected nil")
	} else if got, err := ioutil.ReadAll(rsc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if string(got) != versionList {
		t.Errorf("got %q, want %q", got, versionList)
	}

	tempFile, err := ioutil.TempFile("", "goproxy-test")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.Remove(tempFile.Name())
	if _, err := tempFile.WriteString(resolvedInfo); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := tempFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr = &fetchResult{
		f:    &fetch{ops: fetchOpsDownloadInfo},
		Info: tempFile.Name(),
	}
	if rsc, err := fr.Open(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if rsc == nil {
		t.Fatal("unexpected nil")
	} else if got, err := ioutil.ReadAll(rsc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if string(got) != resolvedInfo {
		t.Errorf("got %q, want %q", got, resolvedInfo)
	}

	tempFile, err = os.OpenFile(tempFile.Name(), os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := tempFile.WriteString(goMod); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := tempFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr = &fetchResult{
		f:     &fetch{ops: fetchOpsDownloadMod},
		GoMod: tempFile.Name(),
	}
	if rsc, err := fr.Open(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if rsc == nil {
		t.Fatal("unexpected nil")
	} else if got, err := ioutil.ReadAll(rsc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if string(got) != goMod {
		t.Errorf("got %q, want %q", got, goMod)
	}

	tempFile, err = os.OpenFile(tempFile.Name(), os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := tempFile.WriteString("zip"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := tempFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	fr = &fetchResult{
		f:   &fetch{ops: fetchOpsDownloadZip},
		Zip: tempFile.Name(),
	}
	if rsc, err := fr.Open(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if rsc == nil {
		t.Fatal("unexpected nil")
	} else if got, err := ioutil.ReadAll(rsc); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if string(got) != "zip" {
		t.Errorf("got %q, want %q", got, goMod)
	}
}

func TestMarshalInfo(t *testing.T) {
	info := struct {
		Version string
		Time    time.Time
	}{"v1.0.0", time.Now()}

	got := marshalInfo(info.Version, info.Time)

	info.Time = info.Time.UTC()

	want, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	if got != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnmarshalInfo(t *testing.T) {
	if _, _, err := unmarshalInfo(""); err == nil {
		t.Fatal("expected error")
	}

	if _, _, err := unmarshalInfo("{}"); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "empty version"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if _, _, err := unmarshalInfo(`{"Version":"v1.0.0"}`); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "zero time"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	infoVersion, infoTime, err := unmarshalInfo(
		`{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`,
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := infoVersion, "v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	wantInfoTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if !infoTime.Equal(wantInfoTime) {
		t.Errorf("got %q, want %q", infoTime, wantInfoTime)
	}
}

func TestCheckAndFormatInfoFile(t *testing.T) {
	tempFile, err := ioutil.TempFile(
		"",
		"goproxy.TestCheckAndFormatInfoFile",
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.Remove(tempFile.Name())
	if _, err := tempFile.WriteString("{}"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := tempFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkAndFormatInfoFile(tempFile.Name()); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"invalid info file: empty version"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	wantInfo := `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`
	if err := ioutil.WriteFile(
		tempFile.Name(),
		[]byte(wantInfo),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkAndFormatInfoFile(tempFile.Name()); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if b, err := ioutil.ReadFile(tempFile.Name()); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), wantInfo; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := ioutil.WriteFile(
		tempFile.Name(),
		[]byte(`{"Version":"v1.0.0",`+
			`"Time":"2000-01-01T01:00:00+01:00"}`),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkAndFormatInfoFile(tempFile.Name()); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if b, err := ioutil.ReadFile(tempFile.Name()); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if got, want := string(b), wantInfo; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := os.Remove(tempFile.Name()); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkAndFormatInfoFile(tempFile.Name()); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, os.ErrNotExist),
		true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCheckModFile(t *testing.T) {
	tempFile, err := ioutil.TempFile("", "goproxy.TestCheckModFile")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.Remove(tempFile.Name())
	if err := tempFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkModFile(tempFile.Name()); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"invalid mod file: missing module directive"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	if err := ioutil.WriteFile(
		tempFile.Name(),
		[]byte("module"),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkModFile(tempFile.Name()); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	if err := ioutil.WriteFile(
		tempFile.Name(),
		[]byte("// foobar\nmodule foobar"),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkModFile(tempFile.Name()); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	if err := os.Remove(tempFile.Name()); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkModFile(tempFile.Name()); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, os.ErrNotExist),
		true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestVerifyModFile(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestVerifyModFile")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.RemoveAll(tempDir)

	modFile := filepath.Join(tempDir, "go.mod")
	if err := ioutil.WriteFile(
		modFile,
		[]byte("module example.com/foo/bar"),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	modFileWrong := filepath.Join(tempDir, "go.mod.wrong")
	if err := ioutil.WriteFile(
		modFileWrong,
		[]byte("module example.com/foo/bar/v2"),
		0600,
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	dirHash, err := dirhash.HashDir(
		tempDir,
		"example.com/foo/bar@v1.0.0",
		dirhash.DefaultHash,
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	modHash, err := dirhash.DefaultHash(
		[]string{"go.mod"},
		func(string) (io.ReadCloser, error) {
			return os.Open(modFile)
		},
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	skey, vkey, err := note.GenerateKey(nil, "example.com")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	server := httptest.NewServer(sumdb.NewServer(sumdb.NewTestServer(
		skey,
		func(modulePath, moduleVersion string) ([]byte, error) {
			if modulePath == "example.com/foo/bar" &&
				moduleVersion == "v1.0.0" {
				return []byte(fmt.Sprintf(
					"%s %s %s\n%s %s/go.mod %s\n",
					modulePath,
					moduleVersion,
					dirHash,
					modulePath,
					moduleVersion,
					modHash,
				)), nil
			}
			return nil, errors.New("unknown module version")
		},
	)))
	defer server.Close()

	g := &Goproxy{GoBinEnv: []string{
		"GOPROXY=off",
		"GOSUMDB=" + vkey + " " + server.URL,
	}}
	g.init()

	if err := verifyModFile(
		g.sumdbClient,
		modFile,
		"example.com/foo/bar",
		"v1.0.0",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	if err := verifyModFile(
		g.sumdbClient,
		modFile,
		"example.com/foo/bar",
		"v1.1.0",
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"example.com/foo/bar@v1.1.0/go.mod: bad upstream"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := verifyModFile(
		g.sumdbClient,
		"",
		"example.com/foo/bar",
		"v1.0.0",
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, os.ErrNotExist),
		true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	if err := verifyModFile(
		g.sumdbClient,
		modFileWrong,
		"example.com/foo/bar",
		"v1.0.0",
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"example.com/foo/bar@v1.0.0: invalid version: "+
			"untrusted revision v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCheckZipFile(t *testing.T) {
	tempFile, err := ioutil.TempFile("", "goproxy.TestCheckZipFile")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.Remove(tempFile.Name())
	if err := tempFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkZipFile(tempFile.Name(), "", ""); err == nil {
		t.Fatal("expected error")
	}

	tempFile, err = os.OpenFile(
		tempFile.Name(),
		os.O_WRONLY|os.O_TRUNC,
		0600,
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	zipWriter := zip.NewWriter(tempFile)
	if zfw, err := zipWriter.Create(
		"example.com@v1.0.0/go.mod",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := zfw.Write(
		[]byte("module example.com"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := zipWriter.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := tempFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := checkZipFile(
		tempFile.Name(),
		"example.com",
		"v1.0.0",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}
}

func TestVerifyZipFile(t *testing.T) {
	zipFile, err := ioutil.TempFile("", "goproxy.TestVerifyZipFile")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.Remove(zipFile.Name())
	zipWriter := zip.NewWriter(zipFile)
	if zfw, err := zipWriter.Create(
		"example.com/foo/bar@v1.0.0/go.mod",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := zfw.Write(
		[]byte("module example.com/foo/bar"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := zipWriter.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := zipFile.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	zipFileWrong, err := ioutil.TempFile("", "goproxy.TestVerifyZipFile")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}
	defer os.Remove(zipFileWrong.Name())
	zipWrongWriter := zip.NewWriter(zipFileWrong)
	if zfw, err := zipWrongWriter.Create(
		"example.com/foo/bar/v2@v2.0.0/go.mod",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := zfw.Write(
		[]byte("module example.com/foo/bar/v2"),
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := zipWrongWriter.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if err := zipFileWrong.Close(); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	dirHash, err := dirhash.HashZip(zipFile.Name(), dirhash.DefaultHash)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	modHash, err := dirhash.DefaultHash(
		[]string{"go.mod"},
		func(string) (io.ReadCloser, error) {
			return nopCloser{strings.NewReader(
				"example.com/foo/bar@v1.0.0/go.mod",
			)}, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	skey, vkey, err := note.GenerateKey(nil, "example.com")
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	server := httptest.NewServer(sumdb.NewServer(sumdb.NewTestServer(
		skey,
		func(modulePath, moduleVersion string) ([]byte, error) {
			if modulePath == "example.com/foo/bar" &&
				moduleVersion == "v1.0.0" {
				return []byte(fmt.Sprintf(
					"%s %s %s\n%s %s/go.mod %s\n",
					modulePath,
					moduleVersion,
					dirHash,
					modulePath,
					moduleVersion,
					modHash,
				)), nil
			}
			return nil, errors.New("unknown module version")
		},
	)))
	defer server.Close()

	g := &Goproxy{GoBinEnv: []string{
		"GOPROXY=off",
		"GOSUMDB=" + vkey + " " + server.URL,
	}}
	g.init()

	if err := verifyZipFile(
		g.sumdbClient,
		zipFile.Name(),
		"example.com/foo/bar",
		"v1.0.0",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	if err := verifyZipFile(
		g.sumdbClient,
		zipFile.Name(),
		"example.com/foo/bar",
		"v1.1.0",
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"example.com/foo/bar@v1.1.0: bad upstream"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := verifyZipFile(
		g.sumdbClient,
		"",
		"example.com/foo/bar",
		"v1.0.0",
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := errors.Is(err, os.ErrNotExist),
		true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	if err := verifyZipFile(
		g.sumdbClient,
		zipFileWrong.Name(),
		"example.com/foo/bar",
		"v1.0.0",
	); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(),
		"example.com/foo/bar@v1.0.0: invalid version: "+
			"untrusted revision v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
