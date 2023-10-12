package goproxy

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"testing"
)

func TestSUMDBClientOps(t *testing.T) {
	proxyServer, setProxyHandler := newHTTPTestServer()
	defer proxyServer.Close()

	for _, tt := range []struct {
		n               int
		proxyHandler    http.HandlerFunc
		envGOPROXY      string
		envGOSUMDB      string
		wantInitError   string
		wantKey         string
		wantEndpointURL string
	}{
		{
			n:             1,
			wantInitError: "missing GOSUMDB",
		},
		{
			n:             2,
			envGOSUMDB:    "example.com foo bar",
			wantInitError: "invalid GOSUMDB: too many fields",
		},
		{
			n:               3,
			envGOPROXY:      "direct",
			envGOSUMDB:      "sum.golang.org",
			wantKey:         sumGolangOrgKey,
			wantEndpointURL: "https://sum.golang.org",
		},
		{
			n:               4,
			envGOPROXY:      "direct",
			envGOSUMDB:      "sum.golang.google.cn",
			wantKey:         sumGolangOrgKey,
			wantEndpointURL: "https://sum.golang.google.cn",
		},
		{
			n:               5,
			proxyHandler:    func(rw http.ResponseWriter, req *http.Request) {},
			envGOPROXY:      proxyServer.URL,
			envGOSUMDB:      "example.com",
			wantKey:         "example.com",
			wantEndpointURL: proxyServer.URL + "/sumdb/example.com",
		},
		{
			n: 6,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotFound)
			},
			envGOPROXY:      proxyServer.URL,
			envGOSUMDB:      "example.com",
			wantKey:         "example.com",
			wantEndpointURL: "https://example.com",
		},
		{
			n: 7,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotFound)
			},
			envGOPROXY:      proxyServer.URL + ",direct",
			envGOSUMDB:      "example.com",
			wantKey:         "example.com",
			wantEndpointURL: "https://example.com",
		},
		{
			n: 8,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotFound)
			},
			envGOPROXY:      proxyServer.URL + ",off",
			envGOSUMDB:      "example.com",
			wantKey:         "example.com",
			wantEndpointURL: "https://example.com",
		},
		{
			n: 9,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
			},
			envGOPROXY:    proxyServer.URL,
			envGOSUMDB:    "example.com",
			wantInitError: "bad upstream",
		},
	} {
		setProxyHandler(tt.proxyHandler)
		sco := &sumdbClientOps{
			envGOPROXY: tt.envGOPROXY,
			envGOSUMDB: tt.envGOSUMDB,
			httpClient: http.DefaultClient,
		}
		sco.init()
		if tt.wantInitError != "" {
			if sco.initError == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := sco.initError.Error(), tt.wantInitError; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if sco.initError != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, sco.initError)
			}
			if got, want := string(sco.key), tt.wantKey; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := sco.endpointURL.String(), tt.wantEndpointURL; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
	}

	for _, tt := range []struct {
		n   int
		sco *sumdbClientOps
	}{
		{1, &sumdbClientOps{envGOSUMDB: "example.com ://invalid"}},
		{2, &sumdbClientOps{envGOPROXY: "://invalid", envGOSUMDB: "example.com"}},
	} {
		tt.sco.init()
		if tt.sco.initError == nil {
			t.Fatalf("test(%d): expected error", tt.n)
		}
	}

	for _, tt := range []struct {
		n               int
		call            func(sco *sumdbClientOps) error
		ignoreCallError bool
	}{
		{
			n: 1,
			call: func(sco *sumdbClientOps) error {
				_, err := sco.ReadRemote("")
				return err
			},
		},
		{
			n: 2,
			call: func(sco *sumdbClientOps) error {
				_, err := sco.ReadConfig("")
				return err
			},
		},
		{
			n: 3,
			call: func(sco *sumdbClientOps) error {
				return sco.WriteConfig("", nil, nil)
			},
		},
		{
			n: 4,
			call: func(sco *sumdbClientOps) error {
				_, err := sco.ReadCache("")
				return err
			},
		},
		{
			n: 5,
			call: func(sco *sumdbClientOps) error {
				sco.WriteCache("", nil)
				return nil
			},
			ignoreCallError: true,
		},
		{
			n: 6,
			call: func(sco *sumdbClientOps) error {
				sco.Log("")
				return nil
			},
			ignoreCallError: true,
		},
		{
			n: 7,
			call: func(sco *sumdbClientOps) error {
				sco.SecurityError("")
				return nil
			},
			ignoreCallError: true,
		},
	} {
		sco := &sumdbClientOps{}
		err := tt.call(sco)
		if !tt.ignoreCallError {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := err.Error(), "missing GOSUMDB"; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		}
		if sco.initError == nil {
			t.Fatalf("test(%d): expected error", tt.n)
		}
		if got, want := sco.initError.Error(), "missing GOSUMDB"; got != want {
			t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
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
			n: 2,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/sumdb/sum.golang.org/supported" {
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
			n:          3,
			envGOPROXY: "direct",
			read: func(sco *sumdbClientOps) (string, error) {
				b, err := sco.ReadConfig("key")
				return string(b), err
			},
			wantContent: sumGolangOrgKey,
		},
		{
			n:          4,
			envGOPROXY: "direct",
			read: func(sco *sumdbClientOps) (string, error) {
				b, err := sco.ReadConfig("/latest")
				return string(b), err
			},
			wantContent: "",
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
			envGOSUMDB: "sum.golang.org",
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
}
