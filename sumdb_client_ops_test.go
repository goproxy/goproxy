package goproxy

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
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
		sco, err := newSumdbClientOps(defaultEnvGOPROXY, tt.envGOSUMDB, http.DefaultClient)
		if tt.wantErr != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			} else if got, want := err, tt.wantErr; !compareErrors(got, want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := sco.name, defaultEnvGOSUMDB; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := sco.key, sumGolangOrgKey; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}
}

func TestSumDBClientOpsURL(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		envGOPROXY   string
		envGOSUMDB   string
		wantURL      string
		wantErr      error
		doubleCheck  bool
	}{
		{
			n:          1,
			envGOPROXY: "direct",
			envGOSUMDB: defaultEnvGOSUMDB,
			wantURL:    "https://" + defaultEnvGOSUMDB,
		},
		{
			n:          2,
			envGOPROXY: "direct",
			envGOSUMDB: defaultEnvGOSUMDB + " https://example.com",
			wantURL:    "https://example.com",
		},
		{
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
			envGOPROXY:   proxyServer.URL,
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      proxyServer.URL + "/sumdb/" + defaultEnvGOSUMDB,
		},
		{
			n:            4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			envGOPROXY:   proxyServer.URL,
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      "https://" + defaultEnvGOSUMDB,
		},
		{
			n:            5,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			envGOPROXY:   proxyServer.URL + ",direct",
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      "https://" + defaultEnvGOSUMDB,
		},
		{
			n:            6,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseNotFound(rw, req, -2) },
			envGOPROXY:   proxyServer.URL + ",off",
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      "https://" + defaultEnvGOSUMDB,
		},
		{
			n:            7,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { responseInternalServerError(rw, req) },
			envGOPROXY:   proxyServer.URL,
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantErr:      errBadUpstream,
			doubleCheck:  true,
		},
	} {
		setProxyHandler(tt.proxyHandler)
		sco, err := newSumdbClientOps(tt.envGOPROXY, tt.envGOSUMDB, http.DefaultClient)
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		u, err := sco.url()
		if tt.wantErr != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			} else if got, want := err, tt.wantErr; !compareErrors(got, want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := u.String(), tt.wantURL; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
		if tt.doubleCheck {
			u2, err2 := sco.url()
			if got, want := err2, err; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := u2, u; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}
}

func TestSumDBClientOpsReadRemote(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()
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
		setProxyHandler(tt.proxyHandler)
		sco, err := newSumdbClientOps(proxyServer.URL, defaultEnvGOSUMDB, http.DefaultClient)
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		b, err := sco.ReadRemote("file")
		if tt.wantErr != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			} else if got, want := err, tt.wantErr; !compareErrors(got, want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
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
		sco, err := newSumdbClientOps("direct", defaultEnvGOSUMDB, http.DefaultClient)
		if err != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, err)
		}
		b, err := sco.ReadConfig(tt.file)
		if tt.wantErr != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			} else if got, want := err, tt.wantErr; !compareErrors(got, want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
			if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
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
		err := tt.call(&sumdbClientOps{})
		if tt.wantErr != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			} else if got, want := err, tt.wantErr; !compareErrors(got, want) {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
	}
}
