package goproxy

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"testing"
	"time"
)

func TestNewFetch(t *testing.T) {
	g := &Goproxy{}
	g.load()
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
	g.load()
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
	g.load()
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
	g.load()
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
	f, err = newFetch(g, name, tempDir)
	if err == nil {
		t.Fatal("expected error")
	} else if want := `unexpected extension ".ext"`; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	} else if f != nil {
		t.Errorf("got %v, want nil", f)
	}

	name = "example.com/foo/bar/@v/latest.info"
	f, err = newFetch(g, name, tempDir)
	if err == nil {
		t.Fatal("expected error")
	} else if want := "invalid version"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	} else if f != nil {
		t.Errorf("got %v, want nil", f)
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
	f, err = newFetch(g, name, tempDir)
	if err == nil {
		t.Fatal("expected error")
	} else if want := "unrecognized version"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	} else if f != nil {
		t.Errorf("got %v, want nil", f)
	}

	name = "example.com/foo/bar/@v/master.zip"
	f, err = newFetch(g, name, tempDir)
	if err == nil {
		t.Fatal("expected error")
	} else if want := "unrecognized version"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	} else if f != nil {
		t.Errorf("got %v, want nil", f)
	}

	name = "example.com/foo/bar"
	f, err = newFetch(g, name, tempDir)
	if err == nil {
		t.Fatal("expected error")
	} else if want := "missing /@v/"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	} else if f != nil {
		t.Errorf("got %v, want nil", f)
	}

	name = "example.com/foo/bar/@v/"
	f, err = newFetch(g, name, tempDir)
	if err == nil {
		t.Fatal("expected error")
	}
	if want := `no file extension in filename ""`; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	} else if f != nil {
		t.Errorf("got %v, want nil", f)
	}

	name = "example.com/foo/bar/@v/main"
	f, err = newFetch(g, name, tempDir)
	if err == nil {
		t.Fatal("expected error")
	}
	if want := `no file extension in filename "main"`; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	} else if f != nil {
		t.Errorf("got %v, want nil", f)
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
	f, err = newFetch(g, name, tempDir)
	if err == nil {
		t.Fatal("expected error")
	}

	name = "example.com/foo/bar/@v/!!v1.0.0.info"
	f, err = newFetch(g, name, tempDir)
	if err == nil {
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
	if zipFileWriter, err := zipWriter.Create(
		"example.com@v1.0.0/go.mod",
	); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if _, err := zipFileWriter.Write(
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
