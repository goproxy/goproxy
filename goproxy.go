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
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/sumdb"
)

// Goproxy is the top-level struct of this project.
//
// Note that the Goproxy will still follow your environment variables. Which
// means you can set GOPROXY to serve the Goproxy itself under other proxies,
// and by setting GONOPROXY and GOPRIVATE to instruct which modules the Goproxy
// should fetch directly instead of using those proxies. And you can also set
// GOSUMDB, GONOSUMDB and GOPRIVATE to instruct how the Goproxy should verify
// the modules it just fetched. All of the above environment variables have been
// built-in support, which means less external command calls and a significant
// performance boost.
//
// For requests downloading large numbers of modules (e.g. for bulk static
// analysis), the Goproxy supports a non-standard header, "Disable-Module-Fetch:
// true" that instructs it to return only cached content.
//
// Make sure that all fields of the Goproxy have been finalized before calling
// any of its methods.
type Goproxy struct {
	// GoBinName is the name of the Go binary.
	//
	// If the GoBinName is empty, the "go" is used.
	//
	// Note that the version of the Go binary targeted by the GoBinName must
	// be at least v1.11.
	GoBinName string

	// GoBinEnv is the environment of the Go binary. Each entry is of the
	// form "key=value".
	//
	// If the GoBinEnv is nil, the [os.Environ] is used.
	//
	// If the GoBinEnv contains duplicate environment keys, only the last
	// value in the slice for each duplicate key is used.
	//
	// Note that GOPROXY, GONOPROXY, GOSUMDB, GONOSUMDB and GOPRIVATE have
	// been built-in support. Which means they can be set even the version
	// of the Go binary targeted by the [Goproxy.GoBinName] is before v1.13.
	GoBinEnv []string

	// GoBinMaxWorkers is the maximum number of commands allowed for the Go
	// binary to execute at the same time.
	//
	// If the GoBinMaxWorkers is zero, there is no limit.
	GoBinMaxWorkers int

	// PathPrefix is the prefix of all request paths. It will be used to
	// trim the request paths via the [strings.TrimPrefix].
	//
	// If the PathPrefix is not empty, it must start with "/", and usually
	// should also end with "/".
	PathPrefix string

	// Cacher is the [Cacher] that used to cache module files.
	//
	// If the Cacher is nil, the module files will be temporarily stored on
	// the local disk and discarded as the request ends.
	Cacher Cacher

	// CacherMaxCacheBytes is the maximum number of bytes allowed for the
	// [Goproxy.Cacher] to store a cache.
	//
	// If the CacherMaxCacheBytes is zero, there is no limit.
	CacherMaxCacheBytes int

	// ProxiedSUMDBs is the list of proxied checksum databases (see
	// https://go.dev/design/25530-sumdb#proxying-a-checksum-database). Each
	// entry is of the form "<sumdb-name>" or "<sumdb-name> <sumdb-URL>".
	// The first form is a shorthand for the second form, in which case the
	// corresponding <sumdb-URL> will be the <sumdb-name> itself as a host
	// with an "https" scheme.
	//
	// If the ProxiedSUMDBs contains duplicate checksum database names, only
	// the last value in the slice for each duplicate checksum database name
	// is used.
	ProxiedSUMDBs []string

	// Transport is used to perform all requests except those started by
	// calling the Go binary targeted by the [Goproxy.GoBinName].
	//
	// If the Transport is nil, the [http.DefaultTransport] is used.
	Transport http.RoundTripper

	// TempDir is the directory for storing temporary files.
	//
	// If the TempDir is empty, the [os.TempDir] is used.
	TempDir string

	// ErrorLogger is the [log.Logger] that logs errors that occur while
	// proxying.
	//
	// If the ErrorLogger is nil, logging is done via the [log] package's
	// standard logger.
	ErrorLogger *log.Logger

	initOnce          sync.Once
	goBinName         string
	goBinEnv          []string
	goBinEnvGOPROXY   string
	goBinEnvGONOPROXY string
	goBinEnvGOSUMDB   string
	goBinEnvGONOSUMDB string
	goBinWorkerChan   chan struct{}
	proxiedSUMDBs     map[string]*url.URL
	httpClient        *http.Client
	sumdbClient       *sumdb.Client
}

// init initializes the g.
func (g *Goproxy) init() {
	g.goBinName = g.GoBinName
	if g.goBinName == "" {
		g.goBinName = "go"
	}

	goBinEnv := g.GoBinEnv
	if goBinEnv == nil {
		goBinEnv = os.Environ()
	}
	var goBinEnvGOPRIVATE string
	for _, env := range goBinEnv {
		if k, v, ok := strings.Cut(env, "="); ok {
			switch strings.TrimSpace(k) {
			case "GO111MODULE":
			case "GOPROXY":
				g.goBinEnvGOPROXY = v
			case "GONOPROXY":
				g.goBinEnvGONOPROXY = v
			case "GOSUMDB":
				g.goBinEnvGOSUMDB = v
			case "GONOSUMDB":
				g.goBinEnvGONOSUMDB = v
			case "GOPRIVATE":
				goBinEnvGOPRIVATE = v
			default:
				g.goBinEnv = append(g.goBinEnv, k+"="+v)
			}
		}
	}
	g.goBinEnv = append(
		g.goBinEnv,
		"GO111MODULE=on",
		"GOPROXY=direct",
		"GONOPROXY=",
		"GOSUMDB=off",
		"GONOSUMDB=",
		"GOPRIVATE=",
	)

	var goBinEnvGOPROXY string
	for goproxy := g.goBinEnvGOPROXY; goproxy != ""; {
		var proxy, sep string
		if i := strings.IndexAny(goproxy, ",|"); i >= 0 {
			proxy = goproxy[:i]
			sep = string(goproxy[i])
			goproxy = goproxy[i+1:]
			if goproxy == "" {
				sep = ""
			}
		} else {
			proxy = goproxy
			goproxy = ""
		}
		proxy = strings.TrimSpace(proxy)
		switch proxy {
		case "":
			continue
		case "direct", "off":
			sep = ""
			goproxy = ""
		}
		goBinEnvGOPROXY += proxy + sep
	}
	if goBinEnvGOPROXY != "" {
		g.goBinEnvGOPROXY = goBinEnvGOPROXY
	} else if g.goBinEnvGOPROXY == "" {
		g.goBinEnvGOPROXY = "https://proxy.golang.org,direct"
	} else {
		g.goBinEnvGOPROXY = "off"
	}

	if g.goBinEnvGONOPROXY == "" {
		g.goBinEnvGONOPROXY = goBinEnvGOPRIVATE
	}
	var goBinEnvGONOPROXYParts []string
	for _, noproxy := range strings.Split(g.goBinEnvGONOPROXY, ",") {
		if noproxy = strings.TrimSpace(noproxy); noproxy != "" {
			goBinEnvGONOPROXYParts = append(goBinEnvGONOPROXYParts, noproxy)
		}
	}
	if len(goBinEnvGONOPROXYParts) > 0 {
		g.goBinEnvGONOPROXY = strings.Join(goBinEnvGONOPROXYParts, ",")
	}

	g.goBinEnvGOSUMDB = strings.TrimSpace(g.goBinEnvGOSUMDB)
	if g.goBinEnvGOSUMDB == "" {
		g.goBinEnvGOSUMDB = "sum.golang.org"
	}

	if g.goBinEnvGONOSUMDB == "" {
		g.goBinEnvGONOSUMDB = goBinEnvGOPRIVATE
	}
	var goBinEnvGONOSUMDBParts []string
	for _, nosumdb := range strings.Split(g.goBinEnvGONOSUMDB, ",") {
		if nosumdb = strings.TrimSpace(nosumdb); nosumdb != "" {
			goBinEnvGONOSUMDBParts = append(goBinEnvGONOSUMDBParts, nosumdb)
		}
	}
	if len(goBinEnvGONOSUMDBParts) > 0 {
		g.goBinEnvGONOSUMDB = strings.Join(goBinEnvGONOSUMDBParts, ",")
	}

	if g.GoBinMaxWorkers != 0 {
		g.goBinWorkerChan = make(chan struct{}, g.GoBinMaxWorkers)
	}

	g.proxiedSUMDBs = map[string]*url.URL{}
	for _, proxiedSUMDB := range g.ProxiedSUMDBs {
		sumdbParts := strings.Fields(proxiedSUMDB)
		if len(sumdbParts) == 0 {
			continue
		}
		sumdbName := sumdbParts[0]
		rawSUMDBURL := sumdbName
		if len(sumdbParts) > 1 {
			rawSUMDBURL = sumdbParts[1]
		}
		sumdbURL, err := parseRawURL(rawSUMDBURL)
		if err != nil {
			continue
		}
		g.proxiedSUMDBs[sumdbName] = sumdbURL
	}

	g.httpClient = &http.Client{Transport: g.Transport}
	g.sumdbClient = sumdb.NewClient(&sumdbClientOps{
		envGOPROXY: g.goBinEnvGOPROXY,
		envGOSUMDB: g.goBinEnvGOSUMDB,
		httpClient: g.httpClient,
	})
}

// ServeHTTP implements the [http.Handler].
func (g *Goproxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	g.initOnce.Do(g.init)

	switch req.Method {
	case http.MethodGet, http.MethodHead:
	default:
		responseMethodNotAllowed(rw, req, 86400)
		return
	}

	name, _ := url.PathUnescape(req.URL.Path)
	if name == "" || name[0] != '/' || name[len(name)-1] == '/' || strings.Contains(name, "..") {
		responseNotFound(rw, req, 86400)
		return
	}
	name = path.Clean(name)
	if g.PathPrefix != "" {
		name = strings.TrimPrefix(name, g.PathPrefix)
	} else {
		name = strings.TrimPrefix(name, "/")
	}

	tempDir, err := os.MkdirTemp(g.TempDir, "goproxy")
	if err != nil {
		g.logErrorf("failed to create temporary directory: %v", err)
		responseInternalServerError(rw, req)
		return
	}
	defer os.RemoveAll(tempDir)

	if strings.HasPrefix(name, "sumdb/") {
		g.serveSUMDB(rw, req, name, tempDir)
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

	var isDownload bool
	switch f.ops {
	case fetchOpsDownloadInfo, fetchOpsDownloadMod, fetchOpsDownloadZip:
		isDownload = true
	}

	noFetch, _ := strconv.ParseBool(req.Header.Get("Disable-Module-Fetch"))
	if noFetch {
		var cacheControlMaxAge int
		if isDownload {
			cacheControlMaxAge = 604800
		} else {
			cacheControlMaxAge = 60
		}
		g.serveCache(rw, req, f.name, f.contentType, cacheControlMaxAge, func() {
			responseNotFound(rw, req, 60, "temporarily unavailable")
		})
		return
	}

	if isDownload {
		g.serveCache(rw, req, f.name, f.contentType, 604800, func() {
			g.serveFetchDownload(rw, req, f)
		})
		return
	}

	fr, err := f.do(req.Context())
	if err != nil {
		g.serveCache(rw, req, f.name, f.contentType, 60, func() {
			g.logErrorf("failed to %s module version: %s: %v", f.ops, f.name, err)
			responseError(rw, req, err, true)
		})
		return
	}

	content, err := fr.Open()
	if err != nil {
		g.logErrorf("failed to open fetch result: %s: %v", f.name, err)
		responseInternalServerError(rw, req)
		return
	}
	defer content.Close()

	if err := g.putCache(req.Context(), f.name, content); err != nil {
		g.logErrorf("failed to cache module file: %s: %v", f.name, err)
		responseInternalServerError(rw, req)
		return
	} else if _, err := content.Seek(0, io.SeekStart); err != nil {
		g.logErrorf("failed to seek fetch result content: %s: %v", f.name, err)
		responseInternalServerError(rw, req)
		return
	}

	responseSuccess(rw, req, content, f.contentType, 60)
}

// serveFetchDownload serves fetch download requests.
func (g *Goproxy) serveFetchDownload(rw http.ResponseWriter, req *http.Request, f *fetch) {
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
		if cache.localFile == "" {
			continue
		}
		if err := g.putCacheFile(req.Context(), nameWithoutExt+cache.nameExt, cache.localFile); err != nil {
			g.logErrorf("failed to cache module file: %s: %v", f.name, err)
			responseInternalServerError(rw, req)
			return
		}
	}

	content, err := fr.Open()
	if err != nil {
		g.logErrorf("failed to open fetch result: %s: %v", f.name, err)
		responseInternalServerError(rw, req)
		return
	}
	defer content.Close()

	responseSuccess(rw, req, content, f.contentType, 604800)
}

// serveSUMDB serves checksum database proxy requests.
func (g *Goproxy) serveSUMDB(rw http.ResponseWriter, req *http.Request, name, tempDir string) {
	sumdbURL, err := parseRawURL(strings.TrimPrefix(name, "sumdb/"))
	if err != nil {
		responseNotFound(rw, req, 86400)
		return
	}
	proxiedSUMDBURL, ok := g.proxiedSUMDBs[sumdbURL.Host]
	if !ok {
		responseNotFound(rw, req, 86400)
		return
	}

	var (
		contentType        string
		cacheControlMaxAge int
	)
	if sumdbURL.Path == "/supported" {
		setResponseCacheControlHeader(rw, 86400)
		rw.WriteHeader(http.StatusOK)
		return
	} else if sumdbURL.Path == "/latest" {
		contentType = "text/plain; charset=utf-8"
		cacheControlMaxAge = 3600
	} else if strings.HasPrefix(sumdbURL.Path, "/lookup/") {
		contentType = "text/plain; charset=utf-8"
		cacheControlMaxAge = 86400
	} else if strings.HasPrefix(sumdbURL.Path, "/tile/") {
		contentType = "application/octet-stream"
		cacheControlMaxAge = 86400
	} else {
		responseNotFound(rw, req, 86400)
		return
	}

	tempFile, err := os.CreateTemp(tempDir, "")
	if err != nil {
		g.logErrorf("failed to create temporary file: %v", err)
		responseInternalServerError(rw, req)
		return
	}
	if err := httpGet(req.Context(), g.httpClient, appendURL(proxiedSUMDBURL, sumdbURL.Path).String(), tempFile); err != nil {
		g.serveCache(rw, req, name, contentType, cacheControlMaxAge, func() {
			g.logErrorf("failed to proxy checksum database: %s: %v", name, err)
			responseError(rw, req, err, true)
		})
		return
	}
	if err := tempFile.Close(); err != nil {
		g.logErrorf("failed to close temporary file: %v", err)
		responseInternalServerError(rw, req)
		return
	}

	if err := g.putCacheFile(req.Context(), name, tempFile.Name()); err != nil {
		g.logErrorf("failed to cache module file: %s: %v", name, err)
		responseInternalServerError(rw, req)
		return
	}

	content, err := os.Open(tempFile.Name())
	if err != nil {
		g.logErrorf("failed to open temporary file: %s: %v", name, err)
		responseInternalServerError(rw, req)
		return
	}
	defer content.Close()

	responseSuccess(rw, req, content, contentType, cacheControlMaxAge)
}

// serveCache serves requests with cached module files.
func (g *Goproxy) serveCache(rw http.ResponseWriter, req *http.Request, name, contentType string, cacheControlMaxAge int, onNotFound func()) {
	content, err := g.cache(req.Context(), name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			onNotFound()
			return
		}
		g.logErrorf("failed to get cached module file: %s: %v", name, err)
		responseInternalServerError(rw, req)
		return
	}
	defer content.Close()
	responseSuccess(rw, req, content, contentType, cacheControlMaxAge)
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
	if g.CacherMaxCacheBytes != 0 {
		if size, err := content.Seek(0, io.SeekEnd); err != nil {
			return err
		} else if size > int64(g.CacherMaxCacheBytes) {
			return nil
		} else if _, err := content.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	return g.Cacher.Put(ctx, name, content)
}

// putCacheFile puts a cache to the g.Cacher for the name with the targeted local file.
func (g *Goproxy) putCacheFile(ctx context.Context, name, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return g.putCache(ctx, name, f)
}

// logErrorf formats according to the format and logs the v as an error.
func (g *Goproxy) logErrorf(format string, v ...any) {
	msg := "goproxy: " + fmt.Sprintf(format, v...)
	if g.ErrorLogger != nil {
		g.ErrorLogger.Output(2, msg)
	} else {
		log.Output(2, msg)
	}
}

// walkGOPROXY walks the proxy list parsed from the goproxy.
func walkGOPROXY(goproxy string, onProxy func(proxy string) error, onDirect, onOff func() error) error {
	if goproxy == "" {
		return errors.New("missing GOPROXY")
	}
	var proxyError error
	for goproxy != "" {
		var (
			proxy           string
			fallBackOnError bool
		)
		if i := strings.IndexAny(goproxy, ",|"); i >= 0 {
			proxy = goproxy[:i]
			fallBackOnError = goproxy[i] == '|'
			goproxy = goproxy[i+1:]
		} else {
			proxy = goproxy
			goproxy = ""
		}
		switch proxy {
		case "direct":
			return onDirect()
		case "off":
			return onOff()
		}
		if err := onProxy(proxy); err != nil {
			if fallBackOnError || errors.Is(err, errNotFound) {
				proxyError = err
				continue
			}
			return err
		}
		return nil
	}
	return proxyError
}

var (
	backoffRand      = rand.New(rand.NewSource(time.Now().UnixNano()))
	backoffRandMutex sync.Mutex
)

// backoffSleep computes the exponential backoff sleep according to
// https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/.
func backoffSleep(base, cap time.Duration, attempt int) time.Duration {
	var pow time.Duration
	if attempt < 63 {
		pow = 1 << attempt
	} else {
		pow = math.MaxInt64
	}

	sleep := base * pow
	if sleep > cap || sleep/pow != base {
		sleep = cap
	}

	backoffRandMutex.Lock()
	sleep = time.Duration(backoffRand.Int63n(int64(sleep)))
	backoffRandMutex.Unlock()

	return sleep
}

// stringSliceContains reports whether the ss contains the s.
func stringSliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// globsMatchPath reports whether any path prefix of target matches one of the
// glob patterns (as defined by the [path.Match]) in the comma-separated globs
// list. It ignores any empty or malformed patterns in the list.
func globsMatchPath(globs, target string) bool {
	for globs != "" {
		// Extract next non-empty glob in comma-separated list.
		var glob string
		if i := strings.Index(globs, ","); i >= 0 {
			glob, globs = globs[:i], globs[i+1:]
		} else {
			glob, globs = globs, ""
		}
		if glob == "" {
			continue
		}

		// A glob with N+1 path elements (N slashes) needs to be matched
		// against the first N+1 path elements of target, which end just
		// before the N+1'th slash.
		n := strings.Count(glob, "/")
		prefix := target

		// Walk target, counting slashes, truncating at the N+1'th slash.
		for i := 0; i < len(target); i++ {
			if target[i] == '/' {
				if n == 0 {
					prefix = target[:i]
					break
				}
				n--
			}
		}
		if n > 0 {
			// Not enough prefix elements.
			continue
		}

		if matched, _ := path.Match(glob, prefix); matched {
			return true
		}
	}
	return false
}
