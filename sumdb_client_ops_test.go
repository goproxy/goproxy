package goproxy

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestNewSumDBClientOps(t *testing.T) {
	for _, tt := range []struct {
		n          int
		envGOSUMDB string
		wantErr    error
	}{
		{1, defaultEnvGOSUMDB, nil},
		{2, defaultEnvGOSUMDB + " https://example.com", nil},
		{3, "", errors.New("missing GOSUMDB")},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			sco, err := newSumdbClientOps(defaultEnvGOPROXY, tt.envGOSUMDB, http.DefaultClient)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := sco.name, defaultEnvGOSUMDB; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := sco.key, sumGolangOrgKey; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSumDBClientOpsURL(t *testing.T) {
	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		envGOPROXY   func(proxyServerURL string) string
		envGOSUMDB   string
		wantURL      func(proxyServerURL string) string
		wantErr      error
		doubleCheck  bool
	}{
		{
			n:          1,
			envGOPROXY: func(_ string) string { return "direct" },
			envGOSUMDB: defaultEnvGOSUMDB,
			wantURL:    func(_ string) string { return "https://" + defaultEnvGOSUMDB },
		},
		{
			n:          2,
			envGOPROXY: func(_ string) string { return "direct" },
			envGOSUMDB: defaultEnvGOSUMDB + " https://example.com",
			wantURL:    func(_ string) string { return "https://example.com" },
		},
		{
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      func(proxyServerURL string) string { return proxyServerURL + "/sumdb/" + defaultEnvGOSUMDB },
		},
		{
			n:            4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      func(_ string) string { return "https://" + defaultEnvGOSUMDB },
		},
		{
			n:            5,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL + ",direct" },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      func(_ string) string { return "https://" + defaultEnvGOSUMDB },
		},
		{
			n:            6,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL + ",off" },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      func(_ string) string { return "https://" + defaultEnvGOSUMDB },
		},
		{
			n:            7,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseInternalServerError(rw, req) },
			envGOPROXY:   func(proxyServerURL string) string { return proxyServerURL },
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantErr:      errBadUpstream,
			doubleCheck:  true,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)
			envGOPROXY := tt.envGOPROXY(proxyServer.URL)

			sco, err := newSumdbClientOps(envGOPROXY, tt.envGOSUMDB, http.DefaultClient)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			u, err := sco.url()
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := u.String(), tt.wantURL(proxyServer.URL); got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}

			if tt.doubleCheck {
				u2, err2 := sco.url()
				if got, want := err2, err; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
				if got, want := u2, u; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSumDBClientOpsReadRemote(t *testing.T) {
	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		wantContent  string
		wantErr      error
	}{
		{
			n:            1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { fmt.Fprint(rw, "foobar") },
			wantContent:  "foobar",
		},
		{
			n: 2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if !strings.HasSuffix(req.URL.Path, "/supported") {
					responseInternalServerError(rw, req)
				}
			},
			wantErr: errBadUpstream,
		},
		{
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseInternalServerError(rw, req) },
			wantErr:      errBadUpstream,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			proxyServer := newHTTPTestServer(t, tt.proxyHandler)

			sco, err := newSumdbClientOps(proxyServer.URL, defaultEnvGOSUMDB, http.DefaultClient)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			b, err := sco.ReadRemote("file")
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := string(b), tt.wantContent; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSumDBClientOpsReadConfig(t *testing.T) {
	for _, tt := range []struct {
		n           int
		file        string
		wantContent string
		wantErr     error
	}{
		{
			n:           1,
			file:        "key",
			wantContent: sumGolangOrgKey,
		},
		{
			n:    2,
			file: "/latest",
		},
		{
			n:       3,
			file:    "file",
			wantErr: errors.New("unknown config file"),
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			sco, err := newSumdbClientOps("direct", defaultEnvGOSUMDB, http.DefaultClient)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			b, err := sco.ReadConfig(tt.file)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if got, want := string(b), tt.wantContent; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSumDBClientOpsExtraCalls(t *testing.T) {
	for _, tt := range []struct {
		n       int
		call    func(sco *sumdbClientOps) error
		wantErr error
	}{
		{
			n: 1,
			call: func(sco *sumdbClientOps) error {
				return sco.WriteConfig("", nil, nil)
			},
		},
		{
			n: 2,
			call: func(sco *sumdbClientOps) error {
				sco.WriteCache("", nil)
				return nil
			},
		},
		{
			n: 3,
			call: func(sco *sumdbClientOps) error {
				sco.Log("")
				return nil
			},
		},
		{
			n: 4,
			call: func(sco *sumdbClientOps) error {
				sco.SecurityError("")
				return nil
			},
		},
		{
			n: 5,
			call: func(sco *sumdbClientOps) error {
				_, err := sco.ReadCache("")
				return err
			},
			wantErr: fs.ErrNotExist,
		},
	} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			err := tt.call(&sumdbClientOps{})
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if got, want := err, tt.wantErr; !compareErrors(got, want) {
					t.Errorf("got %v, want %v", got, want)
				}
			} else if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}
