/*
Package goproxy implements a minimalist Go module proxy handler.
*/
package goproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/note"
)

// Goproxy is the top-level struct of this project.
//
// Note that Goproxy will still adhere to your environment variables. This means
// you can set GOPROXY to serve Goproxy itself under other proxies. By setting
// GONOPROXY and GOPRIVATE, you can instruct Goproxy on which modules to fetch
// directly, rather than using those proxies. Additionally, you can set GOSUMDB,
// GONOSUMDB, and GOPRIVATE to specify how Goproxy should verify the modules it
// has just fetched. Importantly, all of these mentioned environment variables
// are built-in supported, resulting in fewer external command calls and a
// significant performance boost.
//
// For requests involving the download of a large number of modules (e.g., for
// bulk static analysis), Goproxy supports a non-standard header,
// "Disable-Module-Fetch: true", which instructs it to return only cached
// content.
//
// Make sure that all fields of Goproxy have been finalized before calling any
// of its methods.
type Goproxy struct {
	// Env is the environment. Each entry is in the form "key=value".
	//
	// If Env is nil, [os.Environ] is used.
	//
	// If Env contains duplicate environment keys, only the last value in
	// the slice for each duplicate key is used.
	Env []string

	// GoBinName is the name of the Go binary that is used to execute direct
	// fetches.
	//
	// If GoBinName is empty, "go" is used.
	GoBinName string

	// MaxDirectFetches is the maximum number of concurrent direct fetches.
	//
	// If MaxDirectFetches is zero, there is no limit.
	MaxDirectFetches int

	// ProxiedSumDBs is a list of proxied checksum databases (see
	// https://go.dev/design/25530-sumdb#proxying-a-checksum-database). Each
	// entry is in the form "<sumdb-name>" or "<sumdb-name> <sumdb-URL>".
	// The first form is a shorthand for the second, where the corresponding
	// <sumdb-URL> will be the <sumdb-name> itself as a host with an "https"
	// scheme.
	//
	// If ProxiedSumDBs contains duplicate checksum database names, only the
	// last value in the slice for each duplicate checksum database name is
	// used.
	ProxiedSumDBs []string

	// Cacher is used to cache module files.
	//
	// If Cacher is nil, module files will be temporarily stored on the
	// local disk and discarded when the request ends.
	Cacher Cacher

	// TempDir is the directory for storing temporary files.
	//
	// If TempDir is empty, [os.TempDir] is used.
	TempDir string

	// Transport is used to execute outgoing requests, excluding those
	// initiated by direct fetches.
	//
	// If Transport is nil, [http.DefaultTransport] is used.
	Transport http.RoundTripper

	// ErrorLogger is used to log errors that occur during proxying.
	//
	// If ErrorLogger is nil, [log.Default] is used.
	ErrorLogger *log.Logger

	initOnce              sync.Once
	env                   []string
	envGOPROXY            string
	envGONOPROXY          string
	envGOSUMDB            string
	envGONOSUMDB          string
	goBinName             string
	directFetchWorkerPool chan struct{}
	proxiedSumDBs         map[string]*url.URL
	httpClient            *http.Client
	sumdbClient           *sumdb.Client
}

// init initializes the g.
func (g *Goproxy) init() {
	env := g.Env
	if env == nil {
		env = os.Environ()
	}
	var envGOPRIVATE string
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			switch k {
			case "GO111MODULE":
			case "GOPROXY":
				g.envGOPROXY = cleanEnvGOPROXY(v)
			case "GONOPROXY":
				g.envGONOPROXY = v
			case "GOSUMDB":
				g.envGOSUMDB = cleanEnvGOSUMDB(v)
			case "GONOSUMDB":
				g.envGONOSUMDB = v
			case "GOPRIVATE":
				envGOPRIVATE = v
			default:
				g.env = append(g.env, e)
			}
		}
	}
	if g.envGONOPROXY == "" {
		g.envGONOPROXY = envGOPRIVATE
	}
	g.envGONOPROXY = cleanCommaSeparatedList(g.envGONOPROXY)
	if g.envGONOSUMDB == "" {
		g.envGONOSUMDB = envGOPRIVATE
	}
	g.envGONOSUMDB = cleanCommaSeparatedList(g.envGONOSUMDB)
	g.env = append(
		g.env,
		"GO111MODULE=on",
		"GOPROXY=direct",
		"GONOPROXY=",
		"GOSUMDB=off",
		"GONOSUMDB=",
		"GOPRIVATE=",
	)

	g.goBinName = g.GoBinName
	if g.goBinName == "" {
		g.goBinName = "go"
	}

	if g.MaxDirectFetches > 0 {
		g.directFetchWorkerPool = make(chan struct{}, g.MaxDirectFetches)
	}

	g.proxiedSumDBs = map[string]*url.URL{}
	for _, sumdb := range g.ProxiedSumDBs {
		parts := strings.Fields(sumdb)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		rawURL := "https://" + name
		if len(parts) > 1 {
			rawURL = parts[1]
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		g.proxiedSumDBs[name] = u
	}

	g.httpClient = &http.Client{Transport: g.Transport}
	g.sumdbClient = sumdb.NewClient(&sumdbClientOps{
		envGOPROXY: g.envGOPROXY,
		envGOSUMDB: g.envGOSUMDB,
		httpClient: g.httpClient,
	})
}

// ServeHTTP implements [http.Handler].
func (g *Goproxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	g.initOnce.Do(g.init)

	switch req.Method {
	case http.MethodGet, http.MethodHead:
	default:
		responseMethodNotAllowed(rw, req, 86400)
		return
	}

	path := cleanPath(req.URL.Path)
	if path != req.URL.Path || path[len(path)-1] == '/' {
		responseNotFound(rw, req, 86400)
		return
	}
	name := path[1:]

	tempDir, err := os.MkdirTemp(g.TempDir, "goproxy.tmp.*")
	if err != nil {
		g.logErrorf("failed to create temporary directory: %v", err)
		responseInternalServerError(rw, req)
		return
	}
	defer os.RemoveAll(tempDir)

	if strings.HasPrefix(name, "sumdb/") {
		g.serveSumDB(rw, req, name, tempDir)
		return
	}
	g.serveFetch(rw, req, name, tempDir)
}

// serveFetch serves fetch requests.
func (g *Goproxy) serveFetch(rw http.ResponseWriter, req *http.Request, name, tempDir string) {
	f, err := newFetch(g, name, tempDir)
	if err != nil {
		responseNotFound(rw, req, 86400, err)
		return
	}
	noFetch, _ := strconv.ParseBool(req.Header.Get("Disable-Module-Fetch"))
	switch f.ops {
	case fetchOpsQuery:
		g.serveFetchQuery(rw, req, f, noFetch)
	case fetchOpsList:
		g.serveFetchList(rw, req, f, noFetch)
	case fetchOpsDownload:
		g.serveFetchDownload(rw, req, f, noFetch)
	}
}

// serveFetchQuery serves fetch query requests.
func (g *Goproxy) serveFetchQuery(rw http.ResponseWriter, req *http.Request, f *fetch, noFetch bool) {
	const cacheControlMaxAge = 60 // 1 minute
	if noFetch {
		g.serveCache(rw, req, f.name, f.contentType, cacheControlMaxAge, nil)
		return
	}
	fr, err := f.do(req.Context())
	if err != nil {
		g.serveCache(rw, req, f.name, f.contentType, cacheControlMaxAge, func() {
			g.logErrorf("failed to query module version: %s: %v", f.name, err)
			responseError(rw, req, err, true)
		})
		return
	}
	g.servePutCache(rw, req, f.name, f.contentType, cacheControlMaxAge, strings.NewReader(marshalInfo(fr.Version, fr.Time)))
}

// serveFetchList serves fetch list requests.
func (g *Goproxy) serveFetchList(rw http.ResponseWriter, req *http.Request, f *fetch, noFetch bool) {
	const cacheControlMaxAge = 60 // 1 minute
	if noFetch {
		g.serveCache(rw, req, f.name, f.contentType, cacheControlMaxAge, nil)
		return
	}
	fr, err := f.do(req.Context())
	if err != nil {
		g.serveCache(rw, req, f.name, f.contentType, cacheControlMaxAge, func() {
			g.logErrorf("failed to list module versions: %s: %v", f.name, err)
			responseError(rw, req, err, true)
		})
		return
	}
	g.servePutCache(rw, req, f.name, f.contentType, cacheControlMaxAge, strings.NewReader(strings.Join(fr.Versions, "\n")))
}

// serveFetchDownload serves fetch download requests.
func (g *Goproxy) serveFetchDownload(rw http.ResponseWriter, req *http.Request, f *fetch, noFetch bool) {
	const cacheControlMaxAge = 604800 // 7 days

	if noFetch {
		g.serveCache(rw, req, f.name, f.contentType, cacheControlMaxAge, nil)
		return
	}

	content, err := g.cache(req.Context(), f.name)
	if err == nil {
		responseSuccess(rw, req, content, f.contentType, cacheControlMaxAge)
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		g.logErrorf("failed to get cached module file: %s: %v", f.name, err)
		responseInternalServerError(rw, req)
		return
	}

	fr, err := f.do(req.Context())
	if err != nil {
		g.logErrorf("failed to download module version: %s: %v", f.name, err)
		responseError(rw, req, err, false)
		return
	}

	nameWithoutExt := strings.TrimSuffix(f.name, path.Ext(f.name))
	for _, cache := range []struct{ nameExt, localFile string }{
		{".info", fr.Info},
		{".mod", fr.GoMod},
		{".zip", fr.Zip},
	} {
		if err := g.putCacheFile(req.Context(), nameWithoutExt+cache.nameExt, cache.localFile); err != nil {
			g.logErrorf("failed to cache module file: %s: %v", f.name, err)
			responseInternalServerError(rw, req)
			return
		}
	}

	var file string
	switch path.Ext(f.name) {
	case ".info":
		file = fr.Info
	case ".mod":
		file = fr.GoMod
	case ".zip":
		file = fr.Zip
	}
	content, err = os.Open(file)
	if err != nil {
		g.logErrorf("failed to open file: %v", err)
		responseInternalServerError(rw, req)
		return
	}
	defer content.Close()

	responseSuccess(rw, req, content, f.contentType, 604800)
}

// serveSumDB serves checksum database proxy requests.
func (g *Goproxy) serveSumDB(rw http.ResponseWriter, req *http.Request, name, tempDir string) {
	sumdbName, path, ok := strings.Cut(strings.TrimPrefix(name, "sumdb/"), "/")
	if !ok {
		responseNotFound(rw, req, 86400)
		return
	}
	path = "/" + path // Add the leading slash back.
	sumdbURL, ok := g.proxiedSumDBs[sumdbName]
	if !ok {
		responseNotFound(rw, req, 86400)
		return
	}

	var (
		contentType        string
		cacheControlMaxAge int
	)
	switch {
	case path == "/supported":
		setResponseCacheControlHeader(rw, 86400)
		rw.WriteHeader(http.StatusOK)
		return
	case path == "/latest":
		contentType = "text/plain; charset=utf-8"
		cacheControlMaxAge = 3600
	case strings.HasPrefix(path, "/lookup/"):
		contentType = "text/plain; charset=utf-8"
		cacheControlMaxAge = 86400
	case strings.HasPrefix(path, "/tile/"):
		contentType = "application/octet-stream"
		cacheControlMaxAge = 86400
	default:
		responseNotFound(rw, req, 86400)
		return
	}

	file, err := httpGetTemp(req.Context(), g.httpClient, appendURL(sumdbURL, path).String(), tempDir)
	if err != nil {
		g.serveCache(rw, req, name, contentType, cacheControlMaxAge, func() {
			g.logErrorf("failed to proxy checksum database: %s: %v", name, err)
			responseError(rw, req, err, true)
		})
		return
	}
	g.servePutCacheFile(rw, req, name, contentType, cacheControlMaxAge, file)
}

// serveCache serves requests with cached module files.
func (g *Goproxy) serveCache(rw http.ResponseWriter, req *http.Request, name, contentType string, cacheControlMaxAge int, onNotFound func()) {
	content, err := g.cache(req.Context(), name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if onNotFound != nil {
				onNotFound()
			} else {
				responseNotFound(rw, req, 60, "temporarily unavailable")
			}
			return
		}
		g.logErrorf("failed to get cached module file: %s: %v", name, err)
		responseInternalServerError(rw, req)
		return
	}
	defer content.Close()
	responseSuccess(rw, req, content, contentType, cacheControlMaxAge)
}

// servePutCache serves requests after putting the content to the g.Cacher.
func (g *Goproxy) servePutCache(rw http.ResponseWriter, req *http.Request, name, contentType string, cacheControlMaxAge int, content io.ReadSeeker) {
	if err := g.putCache(req.Context(), name, content); err != nil {
		g.logErrorf("failed to cache module file: %s: %v", name, err)
		responseInternalServerError(rw, req)
		return
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		g.logErrorf("failed to seek: %v", err)
		responseInternalServerError(rw, req)
		return
	}
	responseSuccess(rw, req, content, contentType, cacheControlMaxAge)
}

// servePutCacheFile is like [servePutCache] but reads the content from the local file.
func (g *Goproxy) servePutCacheFile(rw http.ResponseWriter, req *http.Request, name, contentType string, cacheControlMaxAge int, file string) {
	f, err := os.Open(file)
	if err != nil {
		g.logErrorf("failed to open file: %v", err)
		responseInternalServerError(rw, req)
		return
	}
	defer f.Close()
	g.servePutCache(rw, req, name, contentType, cacheControlMaxAge, f)
}

// cache returns the matched cache for the name from the g.Cacher.
func (g *Goproxy) cache(ctx context.Context, name string) (io.ReadCloser, error) {
	if g.Cacher == nil {
		return nil, fs.ErrNotExist
	}
	return g.Cacher.Get(ctx, name)
}

// putCache puts a cache to the g.Cacher for the name with the content.
func (g *Goproxy) putCache(ctx context.Context, name string, content io.ReadSeeker) error {
	if g.Cacher == nil {
		return nil
	}
	return g.Cacher.Put(ctx, name, content)
}

// putCacheFile is like [putCache] but reads the content from the local file.
func (g *Goproxy) putCacheFile(ctx context.Context, name, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return g.putCache(ctx, name, f)
}

// logErrorf formats according to a format specifier and writes to the g.ErrorLogger.
func (g *Goproxy) logErrorf(format string, v ...any) {
	msg := "goproxy: " + fmt.Sprintf(format, v...)
	if g.ErrorLogger != nil {
		g.ErrorLogger.Output(2, msg)
	} else {
		log.Output(2, msg)
	}
}

const defaultEnvGOPROXY = "https://proxy.golang.org,direct"

// cleanEnvGOPROXY returns the cleaned envGOPROXY.
func cleanEnvGOPROXY(envGOPROXY string) string {
	if envGOPROXY == "" || envGOPROXY == defaultEnvGOPROXY {
		return defaultEnvGOPROXY
	}
	var cleaned string
	for envGOPROXY != "" {
		var proxy, sep string
		if i := strings.IndexAny(envGOPROXY, ",|"); i >= 0 {
			proxy = envGOPROXY[:i]
			sep = string(envGOPROXY[i])
			envGOPROXY = envGOPROXY[i+1:]
			if envGOPROXY == "" {
				sep = ""
			}
		} else {
			proxy = envGOPROXY
			envGOPROXY = ""
		}
		proxy = strings.TrimSpace(proxy)
		switch proxy {
		case "":
			continue
		case "direct", "off":
			sep = ""
			envGOPROXY = ""
		}
		cleaned += proxy + sep
	}
	if cleaned == "" {
		// An error should probably be reported at this point. Refer to
		// https://go.dev/cl/234857 for more details.
		return "off"
	}
	return cleaned
}

// walkEnvGOPROXY walks through the proxy list parsed from the envGOPROXY.
func walkEnvGOPROXY(envGOPROXY string, onProxy func(proxy *url.URL) error, onDirect, onOff func() error) error {
	if envGOPROXY == "" {
		return errors.New("missing GOPROXY")
	}
	var lastError error
	for envGOPROXY != "" {
		var (
			proxy           string
			fallBackOnError bool
		)
		if i := strings.IndexAny(envGOPROXY, ",|"); i >= 0 {
			proxy = envGOPROXY[:i]
			fallBackOnError = envGOPROXY[i] == '|'
			envGOPROXY = envGOPROXY[i+1:]
		} else {
			proxy = envGOPROXY
			envGOPROXY = ""
		}
		switch proxy {
		case "direct":
			return onDirect()
		case "off":
			return onOff()
		}
		u, err := url.Parse(proxy)
		if err != nil {
			return err
		}
		if err := onProxy(u); err != nil {
			if fallBackOnError || errors.Is(err, fs.ErrNotExist) {
				lastError = err
				continue
			}
			return err
		}
		return nil
	}
	return lastError
}

const defaultEnvGOSUMDB = "sum.golang.org"

// cleanEnvGOSUMDB returns the cleaned envGOSUMDB.
func cleanEnvGOSUMDB(envGOSUMDB string) string {
	if envGOSUMDB == "" || envGOSUMDB == defaultEnvGOSUMDB {
		return defaultEnvGOSUMDB
	}
	return envGOSUMDB
}

const sumGolangOrgKey = "sum.golang.org+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8"

// parseEnvGOSUMDB parses the envGOSUMDB.
func parseEnvGOSUMDB(envGOSUMDB string) (name string, key string, u *url.URL, isDirectURL bool, err error) {
	parts := strings.Fields(envGOSUMDB)
	if l := len(parts); l == 0 {
		return "", "", nil, false, errors.New("missing GOSUMDB")
	} else if l > 2 {
		return "", "", nil, false, errors.New("invalid GOSUMDB: too many fields")
	}

	switch parts[0] {
	case "sum.golang.google.cn":
		if len(parts) == 1 {
			parts = append(parts, "https://"+parts[0])
		}
		fallthrough
	case defaultEnvGOSUMDB:
		parts[0] = sumGolangOrgKey
	}
	verifier, err := note.NewVerifier(parts[0])
	if err != nil {
		return "", "", nil, false, fmt.Errorf("invalid GOSUMDB: %w", err)
	}
	name = verifier.Name()
	key = parts[0]

	u, err = url.Parse("https://" + name)
	if err != nil ||
		strings.HasSuffix(name, "/") ||
		u.Host == "" ||
		u.RawPath != "" ||
		*u != (url.URL{Scheme: "https", Host: u.Host, Path: u.Path, RawPath: u.RawPath}) {
		return "", "", nil, false, fmt.Errorf("invalid sumdb name (must be host[/path]): %s %+v", name, *u)
	}
	isDirectURL = true
	if len(parts) > 1 {
		u, err = url.Parse(parts[1])
		if err != nil {
			return "", "", nil, false, fmt.Errorf("invalid GOSUMDB URL: %w", err)
		}
		isDirectURL = false
	}
	return
}

// cleanCommaSeparatedList returns the cleaned comma-separated list.
func cleanCommaSeparatedList(list string) string {
	var ss []string
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			ss = append(ss, s)
		}
	}
	return strings.Join(ss, ",")
}

// cleanPath returns the canonical path for the p.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		if len(p) == len(np)+1 && strings.HasPrefix(p, np) {
			return p
		}
		return np + "/"
	}
	return np
}
