package internal

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/goproxy/goproxy"
)

func TestNewServerHandler(t *testing.T) {
	for _, tt := range []struct {
		name              string
		cfg               serverCmdConfig
		base              http.Handler
		method            string
		path              string
		wantStatusCode    int
		wantCacheControl  string
		wantContentLength int64
		wantHandledPath   string
	}{
		{
			name:             "HealthzGET",
			path:             "/healthz",
			wantStatusCode:   http.StatusNoContent,
			wantCacheControl: "no-store",
		},
		{
			name:             "HealthzHEAD",
			method:           http.MethodHead,
			path:             "/healthz",
			wantStatusCode:   http.StatusNoContent,
			wantCacheControl: "no-store",
		},
		{
			name:             "Passthrough",
			path:             "/anything",
			wantStatusCode:   http.StatusTeapot,
			wantCacheControl: "no-store",
			wantHandledPath:  "/anything",
		},
		{
			name: "PassthroughCacheControl",
			base: http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				rw.Header().Set("Cache-Control", "private, max-age=30")
				rw.WriteHeader(http.StatusOK)
			}),
			path:             "/anything",
			wantStatusCode:   http.StatusOK,
			wantCacheControl: "private, max-age=30",
			wantHandledPath:  "/anything",
		},
		{
			name:             "HealthzWithPrefix",
			cfg:              serverCmdConfig{pathPrefix: "/proxy"},
			path:             "/proxy/healthz",
			wantStatusCode:   http.StatusNoContent,
			wantCacheControl: "no-store",
		},
		{
			name:             "HealthzHEADWithPrefix",
			cfg:              serverCmdConfig{pathPrefix: "/proxy"},
			method:           http.MethodHead,
			path:             "/proxy/healthz",
			wantStatusCode:   http.StatusNoContent,
			wantCacheControl: "no-store",
		},
		{
			name:             "PassthroughWithPrefix",
			cfg:              serverCmdConfig{pathPrefix: "/proxy"},
			path:             "/proxy/anything",
			wantStatusCode:   http.StatusTeapot,
			wantCacheControl: "no-store",
			wantHandledPath:  "/anything",
		},
		{
			name: "FetchTimeout",
			cfg:  serverCmdConfig{fetchTimeout: 20 * time.Millisecond},
			base: http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				<-req.Context().Done()
				rw.WriteHeader(http.StatusGatewayTimeout)
			}),
			path:             "/slow",
			wantStatusCode:   http.StatusGatewayTimeout,
			wantCacheControl: "no-store",
			wantHandledPath:  "/slow",
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
			if got, want := recr.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			if got, want := int64(rec.Body.Len()), tt.wantContentLength; got != want {
				t.Errorf("got %d, want %d", got, want)
			}
			if got, want := handledPath, tt.wantHandledPath; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}

	t.Run("RoutingCacheControl", func(t *testing.T) {
		for _, tt := range []struct {
			name             string
			prefix           string
			path             string
			wantStatusCode   int
			wantCacheControl string
			wantLocation     string
		}{
			{"RepeatedSlash", "", "/example.com//@latest", http.StatusTemporaryRedirect, "no-store", "/example.com/@latest"},
			{"DotSegment", "", "/./healthz", http.StatusTemporaryRedirect, "no-store", "/healthz"},
			{"ParentSegment", "", "/other/../healthz", http.StatusTemporaryRedirect, "no-store", "/healthz"},
			{"QueryString", "", "/other/../healthz?x=1", http.StatusTemporaryRedirect, "no-store", "/healthz?x=1"},
			{"RedirectWithPrefix", "/proxy", "/proxy/example.com//@latest", http.StatusTemporaryRedirect, "no-store", "/example.com/@latest"},
			{"EmptyPathWithPrefix", "/proxy", "/proxy", http.StatusTemporaryRedirect, "no-store", "/"},
			{"PartialSegmentPrefix", "/proxy", "/proxyx/healthz", http.StatusTemporaryRedirect, "no-store", "/x/healthz"},
			{"MissingPrefix", "/proxy", "/healthz", http.StatusNotFound, "no-store", ""},
			{"EscapedPrefix", "/proxy", "/%70roxy/healthz", http.StatusNotFound, "no-store", ""},
			{"SupportedWithPrefix", "/proxy", "/proxy/sumdb/sumdb.example.com/supported", http.StatusOK, "public, max-age=86400", ""},
			{"UnknownDatabaseWithPrefix", "/proxy", "/proxy/sumdb/other.example.com/supported", http.StatusNotFound, "public, max-age=60", ""},
			{"InvalidResourceWithPrefix", "/proxy", "/proxy/sumdb/sumdb.example.com/unknown", http.StatusNotFound, "public, max-age=86400", ""},
			{"HealthzWithPrefix", "/proxy", "/proxy/healthz", http.StatusNoContent, "no-store", ""},
		} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				t.Run(tt.name+"/"+method, func(t *testing.T) {
					server := httptest.NewServer(newServerHandler(
						&serverCmdConfig{pathPrefix: tt.prefix},
						&goproxy.Goproxy{ProxiedSumDBs: []string{"sumdb.example.com"}},
					))
					defer server.Close()
					client := server.Client()
					client.CheckRedirect = func(*http.Request, []*http.Request) error {
						return http.ErrUseLastResponse
					}
					req, err := http.NewRequest(method, server.URL+tt.path, nil)
					if err != nil {
						t.Fatal(err)
					}
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					if got, want := resp.StatusCode, tt.wantStatusCode; got != want {
						t.Errorf("got status %d, want %d", got, want)
					}
					if got, want := resp.Header.Get("Cache-Control"), tt.wantCacheControl; got != want {
						t.Errorf("got cache control %q, want %q", got, want)
					}
					if got, want := resp.Header.Get("Location"), tt.wantLocation; got != want {
						t.Errorf("got location %q, want %q", got, want)
					}
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					if method == http.MethodHead && len(body) != 0 {
						t.Errorf("got HEAD body %q, want empty", body)
					}
				})
			}
		}
	})
}
