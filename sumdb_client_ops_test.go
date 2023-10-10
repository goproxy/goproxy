package goproxy

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSUMDBClientOps(t *testing.T) {
	var proxyHandler http.HandlerFunc
	proxyServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) { proxyHandler(rw, req) }))
	defer proxyServer.Close()

	for _, tt := range []struct {
		n               int
		proxyHandler    http.HandlerFunc
		sco             *sumdbClientOps
		wantInitError   string
		wantKey         string
		wantEndpointURL string
	}{
		{
			n:             1,
			sco:           &sumdbClientOps{},
			wantInitError: "missing GOSUMDB",
		},
		{
			n:             2,
			sco:           &sumdbClientOps{envGOSUMDB: "example.com foo bar"},
			wantInitError: "invalid GOSUMDB: too many fields",
		},
		{
			n: 3,
			sco: &sumdbClientOps{
				envGOPROXY: "direct",
				envGOSUMDB: "sum.golang.org",
			},
			wantKey:         sumGolangOrgKey,
			wantEndpointURL: "https://sum.golang.org",
		},
		{
			n: 4,
			sco: &sumdbClientOps{
				envGOPROXY: "direct",
				envGOSUMDB: "sum.golang.google.cn",
			},
			wantKey:         sumGolangOrgKey,
			wantEndpointURL: "https://sum.golang.google.cn",
		},
		{
			n:            5,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {},
			sco: &sumdbClientOps{
				httpClient: http.DefaultClient,
				envGOPROXY: proxyServer.URL,
				envGOSUMDB: "example.com",
			},
			wantKey:         "example.com",
			wantEndpointURL: proxyServer.URL + "/sumdb/example.com",
		},
		{
			n: 6,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotFound)
			},
			sco: &sumdbClientOps{
				httpClient: http.DefaultClient,
				envGOPROXY: proxyServer.URL,
				envGOSUMDB: "example.com",
			},
			wantKey:         "example.com",
			wantEndpointURL: "https://example.com",
		},
		{
			n: 7,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotFound)
			},
			sco: &sumdbClientOps{
				httpClient: http.DefaultClient,
				envGOPROXY: proxyServer.URL + ",direct",
				envGOSUMDB: "example.com",
			},
			wantKey:         "example.com",
			wantEndpointURL: "https://example.com",
		},
		{
			n: 8,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotFound)
			},
			sco: &sumdbClientOps{
				httpClient: http.DefaultClient,
				envGOPROXY: proxyServer.URL + ",off",
				envGOSUMDB: "example.com",
			},
			wantKey:         "example.com",
			wantEndpointURL: "https://example.com",
		},
		{
			n: 9,
			proxyHandler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
			},
			sco: &sumdbClientOps{
				httpClient: http.DefaultClient,
				envGOPROXY: proxyServer.URL,
				envGOSUMDB: "example.com",
			},
			wantInitError: "bad upstream",
		},
	} {
		proxyHandler = tt.proxyHandler
		tt.sco.init()
		if tt.wantInitError != "" {
			if tt.sco.initError == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := tt.sco.initError.Error(), tt.wantInitError; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
		} else {
			if tt.sco.initError != nil {
				t.Fatalf("test(%d): unexpected error %q", tt.n, tt.sco.initError)
			}
			if got, want := string(tt.sco.key), tt.wantKey; got != want {
				t.Errorf("test(%d): got %q, want %q", tt.n, got, want)
			}
			if got, want := tt.sco.endpointURL.String(), tt.wantEndpointURL; got != want {
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
		wantError    string
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
			wantError: "bad upstream",
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
			wantError: "unknown config /",
		},
		{
			n:          6,
			envGOPROXY: "direct",
			read: func(sco *sumdbClientOps) (string, error) {
				b, err := sco.ReadCache("")
				return string(b), err
			},
			wantError: fs.ErrNotExist.Error(),
		},
	} {
		proxyHandler = tt.proxyHandler
		sco := &sumdbClientOps{
			httpClient: http.DefaultClient,
			envGOPROXY: tt.envGOPROXY,
			envGOSUMDB: "sum.golang.org",
		}
		sco.init()
		if sco.initError != nil {
			t.Fatalf("test(%d): unexpected error %q", tt.n, sco.initError)
		}
		b, err := tt.read(sco)
		if tt.wantError != "" {
			if err == nil {
				t.Fatalf("test(%d): expected error", tt.n)
			}
			if got, want := err.Error(), tt.wantError; got != want {
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
