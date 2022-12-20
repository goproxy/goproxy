package goproxy

import (
	"errors"
	"os"
	"testing"
)

func TestGoproxyLoad(t *testing.T) {
	for _, key := range []string{
		"GOPROXY",
		"GONOPROXY",
		"GOSUMDB",
		"GONOSUMDB",
		"GOPRIVATE",
	} {
		if err := os.Setenv(key, ""); err != nil {
			t.Fatalf("unexpected error %v", err)
		}
	}

	g := &Goproxy{}
	g.load()
	if got, want := g.goBinName, "go"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.goBinEnv["PATH"], os.Getenv("PATH"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	gotEnvGOPROXY := g.goBinEnv["GOPROXY"]
	wantEnvGOPROXY := "https://proxy.golang.org,direct"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}
	if got, want := g.goBinEnv["GONOPROXY"], ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	gotEnvGOSUMDB := g.goBinEnv["GOSUMDB"]
	wantEnvGOSUMDB := "sum.golang.org"
	if gotEnvGOSUMDB != wantEnvGOSUMDB {
		t.Errorf("got %q, want %q", gotEnvGOSUMDB, wantEnvGOSUMDB)
	}
	if got, want := g.goBinEnv["GONOSUMDB"], ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := g.goBinEnv["GOPRIVATE"], ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	g = &Goproxy{}
	wantEnvGOPROXY = "https://example.com|https://backup.example.com,direct"
	g.GoBinEnv = []string{"GOPROXY=" + wantEnvGOPROXY}
	g.load()
	gotEnvGOPROXY = g.goBinEnv["GOPROXY"]
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{
		"GOPROXY=https://example.com,direct,https://backup.example.com",
	}
	g.load()
	gotEnvGOPROXY = g.goBinEnv["GOPROXY"]
	wantEnvGOPROXY = "https://example.com,direct"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{
		"GOPROXY=https://example.com,off,https://backup.example.com",
	}
	g.load()
	gotEnvGOPROXY = g.goBinEnv["GOPROXY"]
	wantEnvGOPROXY = "https://example.com,off"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPROXY=https://example.com|"}
	g.load()
	gotEnvGOPROXY = g.goBinEnv["GOPROXY"]
	wantEnvGOPROXY = "https://example.com"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPROXY=,"}
	g.load()
	gotEnvGOPROXY = g.goBinEnv["GOPROXY"]
	wantEnvGOPROXY = "off"
	if gotEnvGOPROXY != wantEnvGOPROXY {
		t.Errorf("got %q, want %q", gotEnvGOPROXY, wantEnvGOPROXY)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOSUMDB=example.com"}
	g.load()
	gotEnvGOSUMDB = g.goBinEnv["GOSUMDB"]
	wantEnvGOSUMDB = "example.com"
	if gotEnvGOSUMDB != wantEnvGOSUMDB {
		t.Errorf("got %q, want %q", gotEnvGOSUMDB, wantEnvGOSUMDB)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{"GOPRIVATE=example.com"}
	g.load()
	gotEnvGONOPROXY := g.goBinEnv["GONOPROXY"]
	wantEnvGONOPROXY := "example.com"
	if gotEnvGONOPROXY != wantEnvGONOPROXY {
		t.Errorf("got %q, want %q", gotEnvGONOPROXY, wantEnvGONOPROXY)
	}
	gotEnvGONOSUMDB := g.goBinEnv["GONOSUMDB"]
	wantEnvGONOSUMDB := "example.com"
	if gotEnvGONOSUMDB != wantEnvGONOSUMDB {
		t.Errorf("got %q, want %q", gotEnvGONOSUMDB, wantEnvGONOSUMDB)
	}

	g = &Goproxy{}
	g.GoBinEnv = []string{
		"GOPRIVATE=example.com",
		"GONOPROXY=alt1.example.com",
		"GONOSUMDB=alt2.example.com",
	}
	g.load()
	gotEnvGONOPROXY = g.goBinEnv["GONOPROXY"]
	wantEnvGONOPROXY = "alt1.example.com"
	if gotEnvGONOPROXY != wantEnvGONOPROXY {
		t.Errorf("got %q, want %q", gotEnvGONOPROXY, wantEnvGONOPROXY)
	}
	gotEnvGONOSUMDB = g.goBinEnv["GONOSUMDB"]
	wantEnvGONOSUMDB = "alt2.example.com"
	if gotEnvGONOSUMDB != wantEnvGONOSUMDB {
		t.Errorf("got %q, want %q", gotEnvGONOSUMDB, wantEnvGONOSUMDB)
	}

	g = &Goproxy{}
	g.GoBinMaxWorkers = 1
	g.load()
	if g.goBinWorkerChan == nil {
		t.Fatal("unexpected nil")
	}

	g = &Goproxy{}
	g.ProxiedSUMDBs = []string{
		"sum.golang.google.cn",
		"sum.golang.org https://sum.golang.google.cn",
		"",
		"example.com wrongurl",
	}
	g.load()
	if got, want := len(g.proxiedSUMDBs), 2; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	gotSUMDBURL := g.proxiedSUMDBs["sum.golang.google.cn"].String()
	wantSUMDBURL := "https://sum.golang.google.cn"
	if gotSUMDBURL != wantSUMDBURL {
		t.Errorf("got %q, want %q", gotSUMDBURL, wantSUMDBURL)
	}
	gotSUMDBURL = g.proxiedSUMDBs["sum.golang.org"].String()
	wantSUMDBURL = "https://sum.golang.google.cn"
	if gotSUMDBURL != wantSUMDBURL {
		t.Errorf("got %q, want %q", gotSUMDBURL, wantSUMDBURL)
	}
	if got := g.proxiedSUMDBs["example.com"]; got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestWalkGOPROXY(t *testing.T) {
	if err := walkGOPROXY("", nil, nil, nil); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "missing GOPROXY"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	var (
		onProxy  string
		onDirect bool
		onOff    bool
	)
	if err := walkGOPROXY("direct", func(proxy string) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %v", err)
	} else if got, want := onProxy, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("off", func(proxy string) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %v", err)
	} else if got, want := onProxy, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("direct,off", func(proxy string) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %v", err)
	} else if got, want := onProxy, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("off,direct", func(proxy string) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %v", err)
	} else if got, want := onProxy, ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("https://example.com,direct", func(
		proxy string,
	) error {
		onProxy = proxy
		return nil
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %v", err)
	} else if got, want := onProxy, "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("https://example.com,direct", func(
		proxy string,
	) error {
		onProxy = proxy
		return notFoundError("not found")
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err != nil {
		t.Fatalf("unexpected error %v", err)
	} else if got, want := onProxy, "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, true; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY(
		"https://example.com|https://alt.example.com",
		func(proxy string) error {
			onProxy = proxy
			if proxy == "https://alt.example.com" {
				return nil
			}
			return errors.New("foobar")
		},
		func() error {
			onDirect = true
			return nil
		},
		func() error {
			onOff = true
			return nil
		},
	); err != nil {
		t.Fatalf("unexpected error %v", err)
	} else if got, want := onProxy, "https://alt.example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("https://example.com,direct", func(
		proxy string,
	) error {
		onProxy = proxy
		return errors.New("foobar")
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "foobar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onProxy, "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	onProxy = ""
	onDirect = false
	onOff = false
	if err := walkGOPROXY("https://example.com", func(proxy string) error {
		onProxy = proxy
		return notFoundError("not found")
	}, func() error {
		onDirect = true
		return nil
	}, func() error {
		onOff = true
		return nil
	}); err == nil {
		t.Fatal("expected error")
	} else if got, want := err.Error(), "not found"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onProxy, "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	} else if got, want := onDirect, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	} else if got, want := onOff, false; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStringSliceContains(t *testing.T) {
	if !stringSliceContains([]string{"foo", "bar"}, "foo") {
		t.Error("want true")
	}

	if stringSliceContains([]string{"foo", "bar"}, "foobar") {
		t.Error("want false")
	}
}

func TestGlobsMatchPath(t *testing.T) {
	if !globsMatchPath("foobar", "foobar") {
		t.Error("want true")
	}

	if !globsMatchPath("foo", "foo/bar") {
		t.Error("want true")
	}

	if globsMatchPath("foo", "bar/foo") {
		t.Error("want false")
	}

	if globsMatchPath("foo", "foobar") {
		t.Error("want false")
	}

	if !globsMatchPath("foo/bar", "foo/bar") {
		t.Error("want true")
	}

	if globsMatchPath("foo/bar", "foobar") {
		t.Error("want false")
	}

	if !globsMatchPath("foo,bar", "foo") {
		t.Error("want true")
	}

	if !globsMatchPath("foo,", "foo") {
		t.Error("want true")
	}

	if !globsMatchPath(",bar", "bar") {
		t.Error("want true")
	}

	if globsMatchPath("", "foobar") {
		t.Error("want false")
	}
}
