package internal

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestSiteHandlerServeHTTP(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"index.html":          "<h1>Home</h1>",
		"setup/index.html":    "<h1>Setup</h1>",
		"guide #1/index.html": "<h1>Guide</h1>",
		"help.html":           "<h1>Help</h1>",
		"assets/site.css":     "a{color:red}",
		"assets/@v.css":       "a{color:red}",
	} {
		writeTestSiteFile(t, dir, name, content)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets", "index.html"), 0o755); err != nil {
		t.Fatal(err)
	}
	modified := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "assets", "site.css"), modified, modified); err != nil {
		t.Fatal(err)
	}
	root := openTestSiteRoot(t, dir)

	for _, tt := range []struct {
		name             string
		prefix           string
		method           string
		target           string
		headers          http.Header
		wantStatus       int
		wantBody         string
		wantType         string
		wantContentRange string
		wantLocation     string
	}{
		{name: "Home", target: "/", wantStatus: http.StatusOK, wantBody: "<h1>Home</h1>", wantType: "text/html"},
		{name: "NestedIndex", target: "/setup/", wantStatus: http.StatusOK, wantBody: "<h1>Setup</h1>", wantType: "text/html"},
		{name: "PrefixedHome", prefix: "/@v/", target: "/@v/", wantStatus: http.StatusOK, wantBody: "<h1>Home</h1>"},
		{name: "Help", target: "/help.html", wantStatus: http.StatusOK, wantBody: "<h1>Help</h1>"},
		{name: "AssetQuery", target: "/assets/site.css?v=1", wantStatus: http.StatusOK, wantBody: "a{color:red}", wantType: "text/css"},
		{name: "NonReservedSegment", target: "/assets/@v.css", wantStatus: http.StatusOK, wantBody: "a{color:red}"},
		{name: "HEAD", method: http.MethodHead, target: "/assets/site.css", wantStatus: http.StatusOK, wantType: "text/css"},
		{
			name:       "HEADWithRange",
			method:     http.MethodHead,
			target:     "/assets/site.css",
			headers:    http.Header{"Range": {"bytes=2-6"}},
			wantStatus: http.StatusOK,
			wantType:   "text/css",
		},
		{
			name:             "Range",
			target:           "/assets/site.css",
			headers:          http.Header{"Range": {"bytes=2-6"}},
			wantStatus:       http.StatusPartialContent,
			wantBody:         "color",
			wantContentRange: "bytes 2-6/12",
		},
		{
			name:       "NotModified",
			target:     "/assets/site.css",
			headers:    http.Header{"If-Modified-Since": {modified.Format(http.TimeFormat)}},
			wantStatus: http.StatusNotModified,
		},
		{
			name:       "Modified",
			target:     "/assets/site.css",
			headers:    http.Header{"If-Modified-Since": {modified.Add(-time.Second).Format(http.TimeFormat)}},
			wantStatus: http.StatusOK,
			wantBody:   "a{color:red}",
		},
		{
			name:       "PreconditionFailed",
			target:     "/assets/site.css",
			headers:    http.Header{"If-Unmodified-Since": {modified.Add(-time.Second).Format(http.TimeFormat)}},
			wantStatus: http.StatusPreconditionFailed,
		},
		{
			name:             "UnsatisfiableRange",
			target:           "/assets/site.css",
			headers:          http.Header{"Range": {"bytes=20-21"}},
			wantStatus:       http.StatusRequestedRangeNotSatisfiable,
			wantBody:         "invalid range: failed to overlap\n",
			wantContentRange: "bytes */12",
		},
		{name: "RootRedirect", target: "/index.html", wantStatus: http.StatusMovedPermanently, wantLocation: "/"},
		{
			name:         "IndexRedirect",
			target:       "/setup/index.html?lang=zh&lang=en&theme=%2f%20A+Z&x",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/setup/?lang=zh&lang=en&theme=%2f%20A+Z&x",
		},
		{
			name:         "RawUTF8Query",
			target:       "/setup/index.html?lang=\xe4\xb8\xad",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/setup/?lang=\xe4\xb8\xad",
		},
		{name: "SlashRedirect", target: "/setup?lang=zh", wantStatus: http.StatusMovedPermanently, wantLocation: "/setup/?lang=zh"},
		{
			name:         "PrefixedRedirect",
			prefix:       "/proxy",
			target:       "/proxy/setup/index.html?lang=zh",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/proxy/setup/?lang=zh",
		},
		{
			name:         "EscapedRedirect",
			target:       "/guide%20%231/index.html?",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/guide%20%231/?",
		},
		{name: "DirectoryWithoutIndex", target: "/assets/", wantStatus: http.StatusNotFound},
		{name: "DirectoryWithoutIndexOrSlash", target: "/assets", wantStatus: http.StatusNotFound},
		{name: "NonRegularIndex", target: "/assets/index.html", wantStatus: http.StatusNotFound},
		{name: "MissingIndex", target: "/missing/index.html", wantStatus: http.StatusNotFound},
		{name: "MissingAsset", target: "/assets/missing.css", wantStatus: http.StatusNotFound},
		{name: "NoExtensionAlias", target: "/help", wantStatus: http.StatusNotFound},
		{name: "SlashAfterFile", target: "/help.html/", wantStatus: http.StatusNotFound},
		{name: "EscapingPath", target: "/%2e%2e/help.html", wantStatus: http.StatusNotFound},
		{name: "EncodedDotSegment", target: "/%2e/", wantStatus: http.StatusNotFound},
		{name: "EncodedLeadingSlash", target: "/%2Fsetup/index.html", wantStatus: http.StatusNotFound},
		{name: "OutsideMount", prefix: "/proxy", target: "/help.html", wantStatus: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("unexpected call to the proxy")
			})
			handler := newServerHandler(&serverCmdConfig{pathPrefix: tt.prefix}, base, root)
			req := httptest.NewRequest(tt.method, tt.target, nil)
			req.Header = tt.headers.Clone()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			resp := rec.Result()
			if got, want := resp.StatusCode, tt.wantStatus; got != want {
				t.Errorf("got status %d, want %d", got, want)
			}
			if got := resp.Header.Get("Location"); got != tt.wantLocation {
				t.Errorf("got Location %q, want %q", got, tt.wantLocation)
			}
			wantCacheControl := "no-store"
			switch tt.wantStatus {
			case http.StatusOK, http.StatusPartialContent, http.StatusNotModified:
				wantCacheControl = "public, max-age=60"
			}
			if got := resp.Header.Get("Cache-Control"); got != wantCacheControl {
				t.Errorf("got Cache-Control %q, want %q", got, wantCacheControl)
			}
			if got := resp.Header.Get("Content-Range"); got != tt.wantContentRange {
				t.Errorf("got Content-Range %q, want %q", got, tt.wantContentRange)
			}
			if got := resp.Header.Get("Content-Type"); tt.wantType != "" && !strings.HasPrefix(got, tt.wantType) {
				t.Errorf("got Content-Type %q, want %s", got, tt.wantType)
			}
			if tt.wantLocation == "" {
				wantBody := tt.wantBody
				if tt.wantStatus == http.StatusNotFound {
					wantBody = "404 page not found\n"
				}
				if got := rec.Body.String(); got != wantBody {
					t.Errorf("got body %q, want %q", got, wantBody)
				}
			}
		})
	}

	t.Run("CustomNotFound", func(t *testing.T) {
		const page = "<h1>Page not found</h1>"
		dir := t.TempDir()
		writeTestSiteFile(t, dir, "404.html", page)
		writeTestSiteFile(t, dir, "assets/404.html", "nested page")
		root := openTestSiteRoot(t, dir)
		for _, tt := range []struct {
			name       string
			prefix     string
			method     string
			target     string
			wantStatus int
			wantBody   string
		}{
			{"GET", "", http.MethodGet, "/assets/missing?lang=zh&lang=en", http.StatusNotFound, page},
			{"HEAD", "", http.MethodHead, "/assets/missing?lang=zh&lang=en", http.StatusNotFound, ""},
			{"WithPrefix", "/proxy", http.MethodGet, "/proxy/assets/missing", http.StatusNotFound, page},
			{"DirectRequest", "/proxy", http.MethodGet, "/proxy/404.html", http.StatusOK, page},
		} {
			t.Run(tt.name, func(t *testing.T) {
				handler := newServerHandler(&serverCmdConfig{pathPrefix: tt.prefix}, http.NotFoundHandler(), root)
				req := httptest.NewRequest(tt.method, tt.target, nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != tt.wantStatus || rec.Body.String() != tt.wantBody {
					t.Errorf("got (%d, %q), want (%d, %q)", rec.Code, rec.Body, tt.wantStatus, tt.wantBody)
				}
				header := rec.Result().Header
				if got, want := header.Get("Content-Type"), "text/html; charset=utf-8"; got != want {
					t.Errorf("got Content-Type %q, want %q", got, want)
				}
				if got, want := header.Get("Content-Length"), strconv.Itoa(len(page)); got != want {
					t.Errorf("got Content-Length %q, want %q", got, want)
				}
				wantCacheControl := "no-store"
				if tt.wantStatus == http.StatusOK {
					wantCacheControl = "public, max-age=60"
				}
				if got := header.Get("Cache-Control"); got != wantCacheControl {
					t.Errorf("got Cache-Control %q, want %q", got, wantCacheControl)
				}
				if got := header.Get("Location"); got != "" {
					t.Errorf("got Location %q, want empty", got)
				}
				if got := req.URL.RequestURI(); got != tt.target {
					t.Errorf("got request URI %q, want %q", got, tt.target)
				}
			})
		}
	})

	t.Run("NotFoundFallback", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			setup func(t *testing.T, dir string)
		}{
			{name: "Absent"},
			{
				name: "Unreadable",
				setup: func(t *testing.T, dir string) {
					writeTestSiteFile(t, dir, "404.html", "unreadable page")
					name := filepath.Join(dir, "404.html")
					if err := os.Chmod(name, 0); err != nil {
						t.Fatal(err)
					}
					if f, err := os.Open(name); err == nil {
						f.Close()
						t.Skip("file is still readable with mode 000")
					}
				},
			},
			{
				name: "Directory",
				setup: func(t *testing.T, dir string) {
					writeTestSiteFile(t, dir, "404.html/index.html", "directory index")
				},
			},
			{
				name: "EscapingSymlink",
				setup: func(t *testing.T, dir string) {
					writeTestSiteFile(t, filepath.Dir(dir), "secret.html", "outside page")
					if err := os.Symlink(filepath.Join("..", "secret.html"), filepath.Join(dir, "404.html")); err != nil {
						t.Skipf("cannot create symlink: %v", err)
					}
				},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "web")
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if tt.setup != nil {
					tt.setup(t, dir)
				}
				handler := newServerHandler(&serverCmdConfig{}, http.NotFoundHandler(), openTestSiteRoot(t, dir))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
				if rec.Code != http.StatusNotFound || rec.Body.String() != "404 page not found\n" {
					t.Errorf("got (%d, %q), want the standard 404", rec.Code, rec.Body)
				}
				if got, want := rec.Result().Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
					t.Errorf("got Content-Type %q, want %q", got, want)
				}
			})
		}
	})

	t.Run("Symlinks", func(t *testing.T) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "web")
		writeTestSiteFile(t, parent, "secret.txt", "outside")
		writeTestSiteFile(t, dir, "public.txt", "inside")
		for _, tt := range []struct {
			name       string
			target     string
			wantStatus int
			wantBody   string
		}{
			{"Inside", "public.txt", http.StatusOK, "inside"},
			{"Outside", filepath.Join("..", "secret.txt"), http.StatusNotFound, "404 page not found\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				link := tt.name + ".txt"
				if err := os.Symlink(tt.target, filepath.Join(dir, link)); err != nil {
					t.Skipf("cannot create symlink: %v", err)
				}
				handler := newServerHandler(&serverCmdConfig{}, http.NotFoundHandler(), openTestSiteRoot(t, dir))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+link, nil))
				if rec.Code != tt.wantStatus || rec.Body.String() != tt.wantBody {
					t.Errorf("got (%d, %q), want (%d, %q)", rec.Code, rec.Body, tt.wantStatus, tt.wantBody)
				}
			})
		}
	})
}

func TestWriteSiteNotFound(t *testing.T) {
	page := io.MultiReader(strings.NewReader("<h1>Partial page"), iotest.ErrReader(errors.New("read failed")))
	rec := httptest.NewRecorder()
	writeSiteNotFound(rec, httptest.NewRequest(http.MethodGet, "/missing", nil), page)
	resp := rec.Result()
	if got, want := resp.StatusCode, http.StatusNotFound; got != want {
		t.Errorf("got status %d, want %d", got, want)
	}
	if got, want := resp.Header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("got Content-Type %q, want %q", got, want)
	}
	if got, want := rec.Body.String(), "404 page not found\n"; got != want {
		t.Errorf("got body %q, want %q", got, want)
	}
}

func TestSiteHandlerOpenRegularFile(t *testing.T) {
	for _, tt := range []struct {
		name                string
		replacedByDirectory bool
	}{
		{name: "Removed"},
		{name: "ReplacedByDirectory", replacedByDirectory: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const name = "page.html"
			dir := t.TempDir()
			writeTestSiteFile(t, dir, name, "page")
			h := siteHandler{root: openTestSiteRoot(t, dir)}
			fi, err := h.root.Stat(name)
			if err != nil {
				t.Fatal(err)
			}

			filename := filepath.Join(dir, name)
			if err := os.Remove(filename); err != nil {
				t.Fatal(err)
			}
			if tt.replacedByDirectory {
				if err := os.Mkdir(filename, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			f, fi := h.openRegularFile(name, fi)
			if f != nil {
				f.Close()
			}
			if f != nil || fi != nil {
				t.Errorf("got (%v, %v), want (nil, nil)", f, fi)
			}
		})
	}
}

func writeTestSiteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	filename := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openTestSiteRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}
