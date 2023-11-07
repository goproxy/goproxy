package goproxy

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"testing"
)

func TestSumDBClientOps(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()

	for _, tt := range []struct {
		n             int
		proxyHandler  http.HandlerFunc
		envGOPROXY    string
		envGOSUMDB    string
		wantInitError error
	}{
		{
			n:          1,
			envGOPROXY: "direct",
			envGOSUMDB: defaultEnvGOSUMDB,
		},
		{
			n:          2,
			envGOPROXY: "direct",
			envGOSUMDB: defaultEnvGOSUMDB + " https://example.com",
		},
		{
			n:            3,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
			envGOPROXY:   proxyServer.URL,
			envGOSUMDB:   defaultEnvGOSUMDB,
		},
		{
			n:             4,
			wantInitError: errors.New("missing GOSUMDB"),
		},
	} {
		setProxyHandler(tt.proxyHandler)
		sco := &sumdbClientOps{
			envGOPROXY: tt.envGOPROXY,
			envGOSUMDB: tt.envGOSUMDB,
			httpClient: http.DefaultClient,
		}
		sco.init()
		if tt.wantInitError != nil {
			if sco.initError == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := sco.initError, tt.wantInitError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if sco.initError != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, sco.initError)
			}
			if got, want := sco.name, defaultEnvGOSUMDB; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := sco.key, sumGolangOrgKey; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}

	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		envGOPROXY   string
		envGOSUMDB   string
		wantURL      string
		wantError    error
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
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { rw.WriteHeader(http.StatusNotFound) },
			envGOPROXY:   proxyServer.URL,
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      "https://" + defaultEnvGOSUMDB,
		},
		{
			n:            5,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { rw.WriteHeader(http.StatusNotFound) },
			envGOPROXY:   proxyServer.URL + ",direct",
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      "https://" + defaultEnvGOSUMDB,
		},
		{
			n:            6,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { rw.WriteHeader(http.StatusNotFound) },
			envGOPROXY:   proxyServer.URL + ",off",
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantURL:      "https://" + defaultEnvGOSUMDB,
		},
		{
			n:            7,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) { rw.WriteHeader(http.StatusInternalServerError) },
			envGOPROXY:   proxyServer.URL,
			envGOSUMDB:   defaultEnvGOSUMDB,
			wantError:    errBadUpstream,
			doubleCheck:  true,
		},
		{
			n:         8,
			wantError: errors.New("missing GOSUMDB"),
		},
	} {
		setProxyHandler(tt.proxyHandler)
		sco := &sumdbClientOps{
			envGOPROXY: tt.envGOPROXY,
			envGOSUMDB: tt.envGOSUMDB,
			httpClient: http.DefaultClient,
		}
		u, err := sco.url()
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
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

	for _, tt := range []struct {
		n            int
		proxyHandler http.HandlerFunc
		envGOPROXY   string
		read         func(sco *sumdbClientOps) (string, error)
		wantContent  string
		wantError    error
	}{
		{
			n: 1,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				fmt.Fprint(rw, "foobar")
			},
			envGOPROXY: proxyServer.URL,
			read: func(sco *sumdbClientOps) (string, error) {
				b, err := sco.ReadRemote("")
				return string(b), err
			},
			wantContent: "foobar",
		},
		{
			n:          2,
			envGOPROXY: "direct",
			read: func(sco *sumdbClientOps) (string, error) {
				b, err := sco.ReadConfig("key")
				return string(b), err
			},
			wantContent: sumGolangOrgKey,
		},
		{
			n:          3,
			envGOPROXY: "direct",
			read: func(sco *sumdbClientOps) (string, error) {
				b, err := sco.ReadConfig("/latest")
				return string(b), err
			},
			wantContent: "",
		},
		{
			n: 4,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/sumdb/"+defaultEnvGOSUMDB+"/supported" {
					rw.WriteHeader(http.StatusOK)
				} else {
					rw.WriteHeader(http.StatusInternalServerError)
				}
			},
			envGOPROXY: proxyServer.URL,
			read: func(sco *sumdbClientOps) (string, error) {
				b, err := sco.ReadRemote("")
				return string(b), err
			},
			wantError: errBadUpstream,
		},
		{
			n:          5,
			envGOPROXY: "direct",
			read: func(sco *sumdbClientOps) (string, error) {
				b, err := sco.ReadConfig("/")
				return string(b), err
			},
			wantError: errors.New("unknown config /"),
		},
		{
			n:          6,
			envGOPROXY: "direct",
			read: func(sco *sumdbClientOps) (string, error) {
				b, err := sco.ReadCache("")
				return string(b), err
			},
			wantError: fs.ErrNotExist,
		},
	} {
		setProxyHandler(tt.proxyHandler)
		sco := &sumdbClientOps{
			envGOPROXY: tt.envGOPROXY,
			envGOSUMDB: defaultEnvGOSUMDB,
			httpClient: http.DefaultClient,
		}
		sco.init()
		if sco.initError != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, sco.initError)
		}
		b, err := tt.read(sco)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, sco.initError)
			}
			if got, want := string(b), tt.wantContent; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}

	for _, tt := range []struct {
		n         int
		call      func(sco *sumdbClientOps) error
		wantError error
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
				_, err := sco.ReadRemote("")
				return err
			},
			wantError: errors.New("missing GOSUMDB"),
		},
		{
			n: 6,
			call: func(sco *sumdbClientOps) error {
				_, err := sco.ReadConfig("")
				return err
			},
			wantError: errors.New("missing GOSUMDB"),
		},
		{
			n: 7,
			call: func(sco *sumdbClientOps) error {
				_, err := sco.ReadCache("")
				return err
			},
			wantError: fs.ErrNotExist,
		},
	} {
		sco := &sumdbClientOps{}
		err := tt.call(sco)
		if tt.wantError != nil {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := err, tt.wantError; !errors.Is(got, want) && got.Error() != want.Error() {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if err != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, err)
			}
		}
	}
}
