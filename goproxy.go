/*
Package goproxy implements a minimalist Go module proxy handler.
*/
package goproxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb"
	"golang.org/x/net/idna"
)

// Goproxy is the top-level struct of this project.
//
// Note that the Goproxy will not mess with your environment variables, it will
// still follow your GOPROXY, GONOPROXY, GOSUMDB, GONOSUMDB and GOPRIVATE. It
// means that you can set GOPROXY to serve the Goproxy itself under other
// proxies, and by setting GONOPROXY and GOPRIVATE to indicate which modules the
// Goproxy should download directly instead of using those proxies. And of
// course, you can also set GOSUMDB, GONOSUMDB and GOPRIVATE to indicate how
// the Goproxy should verify the modules.
//
// Since GOPROXY with comma-separated list support, GONOPROXY, GOSUMDB,
// GONOSUMDB and GOPRIVATE were first introduced in Go 1.13, so we implemented a
// built-in support for them. Now, you can set them even the version of the Go
// binary targeted by the [Goproxy.GoBinName] is before v1.13.
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
	// Note that GOPROXY (with comma-separated list support), GONOPROXY,
	// GOSUMDB, GONOSUMDB and GOPRIVATE are built-in supported. It means
	// that they can be set even the version of the Go binary targeted by
	// the [Goproxy.GoBinName] is before v1.13.
	GoBinEnv []string

	// GoBinMaxWorkers is the maximum number of commands allowed for the Go
	// binary to execute at the same time.
	//
	// If the GoBinMaxWorkers is zero, there is no limitation.
	GoBinMaxWorkers int

	// PathPrefix is the prefix of all request paths. It will be used to
	// trim the request paths via the [strings.TrimPrefix].
	//
	// If the PathPrefix is not empty, it must start with "/", and usually
	// should also end with "/".
	PathPrefix string

	// Cacher is the [Cacher] that used to cache module files.
	//
	// If the Cacher is nil, the module files will be temporarily stored
	// in the local disk and discarded as the request ends.
	Cacher Cacher

	// CacherMaxCacheBytes is the maximum number of bytes allowed for the
	// [Goproxy.Cacher] to store a cache.
	//
	// If the CacherMaxCacheBytes is zero, there is no limitation.
	CacherMaxCacheBytes int

	// ProxiedSUMDBs is the list of proxied checksum databases. See
	// https://go.dev/design/25530-sumdb#proxying-a-checksum-database.
	//
	// If the ProxiedSUMDBs is not nil, each value should be given the
	// format of "<sumdb-name>" or "<sumdb-name> <sumdb-URL>". The first
	// format can be seen as a shorthand for the second format. In the case
	// of the first format, the corresponding checksum database URL will be
	// the checksum database name itself as a host with an "https" scheme.
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

	loadOnce        sync.Once
	goBinName       string
	goBinEnv        map[string]string
	goBinWorkerChan chan struct{}
	proxiedSUMDBs   map[string]*url.URL
	httpClient      *http.Client
	sumdbClient     *sumdb.Client
}

// load loads the stuff of the g up.
func (g *Goproxy) load() {
	if g.GoBinName != "" {
		g.goBinName = g.GoBinName
	} else {
		g.goBinName = "go"
	}

	goBinEnv := g.GoBinEnv
	if goBinEnv == nil {
		goBinEnv = os.Environ()
	}

	g.goBinEnv = map[string]string{}
	for _, env := range goBinEnv {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		g.goBinEnv[parts[0]] = parts[1]
	}

	var envGOPROXY string
	for goproxy := g.goBinEnv["GOPROXY"]; goproxy != ""; {
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

		envGOPROXY = fmt.Sprint(envGOPROXY, proxy, sep)
	}

	if envGOPROXY != "" {
		g.goBinEnv["GOPROXY"] = envGOPROXY
	} else if g.goBinEnv["GOPROXY"] == "" {
		g.goBinEnv["GOPROXY"] = "https://proxy.golang.org,direct"
	} else {
		g.goBinEnv["GOPROXY"] = "off"
	}

	g.goBinEnv["GOSUMDB"] = strings.TrimSpace(g.goBinEnv["GOSUMDB"])
	if g.goBinEnv["GOSUMDB"] == "" {
		g.goBinEnv["GOSUMDB"] = "sum.golang.org"
	}

	if g.goBinEnv["GONOPROXY"] == "" {
		g.goBinEnv["GONOPROXY"] = g.goBinEnv["GOPRIVATE"]
	}

	var noproxies []string
	for _, noproxy := range strings.Split(g.goBinEnv["GONOPROXY"], ",") {
		noproxy = strings.TrimSpace(noproxy)
		if noproxy == "" {
			continue
		}

		noproxies = append(noproxies, noproxy)
	}

	if len(noproxies) > 0 {
		g.goBinEnv["GONOPROXY"] = strings.Join(noproxies, ",")
	}

	if g.goBinEnv["GONOSUMDB"] == "" {
		g.goBinEnv["GONOSUMDB"] = g.goBinEnv["GOPRIVATE"]
	}

	var nosumdbs []string
	for _, nosumdb := range strings.Split(g.goBinEnv["GONOSUMDB"], ",") {
		nosumdb = strings.TrimSpace(nosumdb)
		if nosumdb == "" {
			continue
		}

		nosumdbs = append(nosumdbs, nosumdb)
	}

	if len(nosumdbs) > 0 {
		g.goBinEnv["GONOSUMDB"] = strings.Join(nosumdbs, ",")
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

		sumdbName, err := idna.Lookup.ToASCII(sumdbParts[0])
		if err != nil {
			continue
		}

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

	g.httpClient = &http.Client{
		Transport: g.Transport,
	}

	g.sumdbClient = sumdb.NewClient(&sumdbClientOps{
		envGOPROXY: g.goBinEnv["GOPROXY"],
		envGOSUMDB: g.goBinEnv["GOSUMDB"],
		httpClient: g.httpClient,
	})
}

// ServeHTTP implements the [http.Handler].
func (g *Goproxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	g.loadOnce.Do(g.load)

	switch req.Method {
	case http.MethodGet, http.MethodHead:
	default:
		responseMethodNotAllowed(rw, 86400)
		return
	}

	name, err := url.PathUnescape(req.URL.Path)
	if err != nil ||
		!strings.HasPrefix(name, "/") ||
		strings.HasSuffix(name, "/") {
		responseNotFound(rw, 86400)
		return
	}

	if strings.Contains(name, "..") {
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				responseNotFound(rw, 86400)
				return
			}
		}
	}

	name = path.Clean(name)
	if g.PathPrefix != "" {
		name = strings.TrimPrefix(name, g.PathPrefix)
	} else {
		name = strings.TrimPrefix(name, "/")
	}

	if strings.HasPrefix(name, "sumdb/") {
		name = strings.TrimPrefix(name, "sumdb/")

		sumdbURL, err := parseRawURL(name)
		if err != nil {
			responseNotFound(rw, 86400)
			return
		}

		sumdbName, err := idna.Lookup.ToASCII(sumdbURL.Host)
		if err != nil {
			responseNotFound(rw, 86400)
			return
		}

		if g.proxiedSUMDBs[sumdbName] == nil {
			responseNotFound(rw, 86400)
			return
		}

		var (
			contentType        string
			cacheControlMaxAge int
		)

		if sumdbURL.Path == "/supported" {
			setResponseCacheControlHeader(rw, 86400)
			rw.Write(nil) // 200 OK
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
			responseNotFound(rw, 86400)
			return
		}

		var buf bytes.Buffer
		if err := httpGet(
			req.Context(),
			g.httpClient,
			appendURL(
				g.proxiedSUMDBs[sumdbName],
				sumdbURL.Path,
			).String(),
			&buf,
		); err != nil {
			g.logErrorf(
				"failed to proxy checksum database request: %s",
				prefixToIfNotIn(err.Error(), name),
			)
			responseModError(rw, err, false)
			return
		}

		rw.Header().Set("Content-Type", contentType)
		rw.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		setResponseCacheControlHeader(rw, cacheControlMaxAge)
		buf.WriteTo(rw)

		return
	}

	var (
		nameExt = path.Ext(name)

		isLatest, isList                        bool
		escapedModulePath, escapedModuleVersion string
	)

	if strings.HasSuffix(name, "/@latest") {
		isLatest = true
		escapedModulePath = strings.TrimSuffix(name, "/@latest")
		escapedModuleVersion = "latest"
	} else if strings.HasSuffix(name, "/@v/list") {
		isList = true
		escapedModulePath = strings.TrimSuffix(name, "/@v/list")
		escapedModuleVersion = "latest"
	} else {
		switch nameExt {
		case ".info", ".mod", ".zip":
		default:
			responseNotFound(rw, 86400)
			return
		}

		nameParts := strings.Split(name, "/@v/")
		if len(nameParts) != 2 {
			responseNotFound(rw, 86400)
			return
		}

		escapedModulePath = nameParts[0]
		escapedModuleVersion = strings.TrimSuffix(nameParts[1], nameExt)
	}

	modulePath, err := module.UnescapePath(escapedModulePath)
	if err != nil {
		responseNotFound(rw, 86400)
		return
	}

	moduleVersion, err := module.UnescapeVersion(escapedModuleVersion)
	if err != nil {
		responseNotFound(rw, 86400)
		return
	}

	modAtVer := fmt.Sprint(modulePath, "@", moduleVersion)

	goproxyRoot, err := ioutil.TempDir(g.TempDir, "goproxy")
	if err != nil {
		g.logErrorf("failed to create temporary directory: %v", err)
		responseInternalServerError(rw)
		return
	}
	defer os.RemoveAll(goproxyRoot)

	if isLatest || isList || !semver.IsValid(moduleVersion) {
		var operation string
		if isLatest {
			operation = "latest"
		} else if isList {
			operation = "list"
		} else if nameExt == ".info" {
			operation = "lookup"
		} else {
			responseNotFound(rw, 86400)
			return
		}

		mr, err := g.mod(
			req.Context(),
			operation,
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			if content, err := g.cache(
				req.Context(),
				name,
			); err == nil {
				b, err := ioutil.ReadAll(content)
				content.Close()
				if err != nil {
					g.logErrorf(
						"failed to get cached module "+
							"file: %s",
						prefixToIfNotIn(
							err.Error(),
							modAtVer,
						),
					)
					responseInternalServerError(rw)
					return
				}

				if isList {
					responseString(
						rw,
						http.StatusOK,
						60,
						string(b),
					)
				} else {
					responseJSON(rw, http.StatusOK, 60, b)
				}

				return
			} else if !errors.Is(err, os.ErrNotExist) {
				g.logErrorf(
					"failed to get cached module file: %s",
					prefixToIfNotIn(err.Error(), modAtVer),
				)
				responseInternalServerError(rw)
				return
			}

			g.logErrorf(
				"failed to %s module version: %s",
				operation,
				prefixToIfNotIn(err.Error(), modAtVer),
			)
			responseModError(rw, err, true)

			return
		}

		var content []byte
		if isList {
			content = []byte(strings.Join(mr.Versions, "\n"))
		} else if content, err = json.Marshal(struct {
			Version string
			Time    time.Time
		}{
			mr.Version,
			mr.Time,
		}); err != nil {
			g.logErrorf(
				"failed to marshal module version info: %s",
				prefixToIfNotIn(err.Error(), modAtVer),
			)
			responseInternalServerError(rw)
			return
		}

		if err := g.setCache(
			req.Context(),
			name,
			bytes.NewReader(content),
		); err != nil {
			g.logErrorf(
				"failed to cache module file: %s",
				prefixToIfNotIn(err.Error(), modAtVer),
			)
			responseInternalServerError(rw)
			return
		}

		if isList {
			responseString(rw, http.StatusOK, 60, string(content))
		} else {
			responseJSON(rw, http.StatusOK, 60, content)
		}

		return
	}

	var content io.Reader
	if rc, err := g.cache(req.Context(), name); err == nil {
		defer rc.Close()
		content = rc
	} else if errors.Is(err, os.ErrNotExist) {
		mr, err := g.mod(
			req.Context(),
			fmt.Sprint("download ", nameExt[1:]),
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			g.logErrorf(
				"failed to download module file: %s",
				prefixToIfNotIn(err.Error(), modAtVer),
			)
			responseModError(rw, err, false)
			return
		}

		namePrefix := strings.TrimSuffix(name, nameExt)

		if mr.Info != "" {
			if err := g.setCacheFile(
				req.Context(),
				fmt.Sprint(namePrefix, ".info"),
				mr.Info,
			); err != nil {
				g.logErrorf(
					"failed to cache module file: %s",
					prefixToIfNotIn(err.Error(), modAtVer),
				)
				responseInternalServerError(rw)
				return
			}
		}

		if mr.GoMod != "" {
			if err := g.setCacheFile(
				req.Context(),
				fmt.Sprint(namePrefix, ".mod"),
				mr.GoMod,
			); err != nil {
				g.logErrorf(
					"failed to cache module file: %s",
					prefixToIfNotIn(err.Error(), modAtVer),
				)
				responseInternalServerError(rw)
				return
			}
		}

		if mr.Zip != "" {
			if err := g.setCacheFile(
				req.Context(),
				fmt.Sprint(namePrefix, ".zip"),
				mr.Zip,
			); err != nil {
				g.logErrorf(
					"failed to cache module file: %s",
					prefixToIfNotIn(err.Error(), modAtVer),
				)
				responseInternalServerError(rw)
				return
			}
		}

		switch nameExt {
		case ".info":
			content, err = os.Open(mr.Info)
		case ".mod":
			content, err = os.Open(mr.GoMod)
		case ".zip":
			content, err = os.Open(mr.Zip)
		}

		if err != nil {
			g.logErrorf(
				"failed to open module file: %s",
				prefixToIfNotIn(err.Error(), modAtVer),
			)
			responseInternalServerError(rw)
			return
		}
		defer content.(*os.File).Close()
	} else {
		g.logErrorf(
			"failed to get cached module file: %s",
			prefixToIfNotIn(err.Error(), modAtVer),
		)
		responseInternalServerError(rw)
		return
	}

	var contentType string
	switch nameExt {
	case ".info":
		contentType = "application/json; charset=utf-8"
	case ".mod":
		contentType = "text/plain; charset=utf-8"
	case ".zip":
		contentType = "application/zip"
	}

	rw.Header().Set("Content-Type", contentType)

	var modTime time.Time
	if mt, ok := content.(interface{ ModTime() time.Time }); ok {
		modTime = mt.ModTime()
	}

	if cs, ok := content.(interface{ Checksum() []byte }); ok {
		rw.Header().Set("ETag", fmt.Sprintf(
			"%q",
			base64.StdEncoding.EncodeToString(cs.Checksum()),
		))
	}

	setResponseCacheControlHeader(rw, 604800)

	if content, ok := content.(io.ReadSeeker); ok {
		http.ServeContent(rw, req, "", modTime, content)
		return
	}

	if !modTime.IsZero() {
		rw.Header().Set(
			"Last-Modified",
			modTime.UTC().Format(http.TimeFormat),
		)
	}

	io.Copy(rw, content)
}

// cache returns the matched cache for the name from the g.Cacher.
func (g *Goproxy) cache(
	ctx context.Context,
	name string,
) (io.ReadCloser, error) {
	if g.Cacher == nil {
		return nil, os.ErrNotExist
	}

	return g.Cacher.Get(ctx, name)
}

// setCache sets the content as a cache with the name to the g.Cacher.
func (g *Goproxy) setCache(
	ctx context.Context,
	name string,
	content io.ReadSeeker,
) error {
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

	return g.Cacher.Set(ctx, name, content)
}

// setCacheFile sets the local file targeted by the file as a cache with the
// name to the g.Cacher.
func (g *Goproxy) setCacheFile(ctx context.Context, name, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	return g.setCache(ctx, name, f)
}

// logErrorf formats according to the format and logs the v as an error.
func (g *Goproxy) logErrorf(format string, v ...interface{}) {
	msg := fmt.Sprint("goproxy: ", fmt.Sprintf(format, v...))
	if g.ErrorLogger != nil {
		g.ErrorLogger.Output(2, msg)
	} else {
		log.Output(2, msg)
	}
}

// prefixToIfNotIn adds the prefix to the s if it is not in the s.
func prefixToIfNotIn(s, prefix string) string {
	if strings.Contains(s, prefix) {
		return s
	}

	return fmt.Sprint(prefix, ": ", s)
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

		// Walk target, counting slashes, truncating at the N+1'th
		// slash.
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
