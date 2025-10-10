package internal

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewServerHandler(t *testing.T) {
	for _, tt := range []struct {
		name              string
		cfg               serverCmdConfig
		base              http.Handler
		method            string
		path              string
		wantStatusCode    int
		wantContentLength int64
		wantHandledPath   string
	}{
		{
			name:           "HealthzGET",
			path:           "/healthz",
			wantStatusCode: http.StatusNoContent,
		},
		{
			name:           "HealthzHEAD",
			method:         http.MethodHead,
			path:           "/healthz",
			wantStatusCode: http.StatusNoContent,
		},
		{
			name:            "Passthrough",
			path:            "/anything",
			wantStatusCode:  http.StatusTeapot,
			wantHandledPath: "/anything",
		},
		{
			name:           "HealthzWithPrefix",
			cfg:            serverCmdConfig{pathPrefix: "/proxy"},
			path:           "/proxy/healthz",
			wantStatusCode: http.StatusNoContent,
		},
		{
			name:            "PassthroughWithPrefix",
			cfg:             serverCmdConfig{pathPrefix: "/proxy"},
			path:            "/proxy/anything",
			wantStatusCode:  http.StatusTeapot,
			wantHandledPath: "/anything",
		},
		{
			name: "FetchTimeout",
			cfg:  serverCmdConfig{fetchTimeout: 20 * time.Millisecond},
			base: http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				<-req.Context().Done()
				rw.WriteHeader(http.StatusGatewayTimeout)
			}),
			path:            "/slow",
			wantStatusCode:  http.StatusGatewayTimeout,
			wantHandledPath: "/slow",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var handledPath string
			handler := newServerHandler(&tt.cfg, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				handledPath = req.URL.Path
				if tt.base != nil {
					tt.base.ServeHTTP(rw, req)
				} else {
					rw.WriteHeader(http.StatusTeapot)
				}
			}))

			req := httptest.NewRequest(tt.method, "https://example.com"+tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			recr := rec.Result()
			if got, want := recr.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := int64(rec.Body.Len()), tt.wantContentLength; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := handledPath, tt.wantHandledPath; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}
