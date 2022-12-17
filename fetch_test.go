package goproxy

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"
	"time"
)

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
