/*
Package goproxy implements a minimalist Go module proxy handler.
*/
package goproxy

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
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
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/net/idna"
)

// Goproxy is the top-level struct of this project.
//
// Note that the `Goproxy` will not mess with your environment variables, it
// will still follow your GOPROXY, GONOPROXY, GOSUMDB, GONOSUMDB and GOPRIVATE.
// It means that you can set GOPROXY to serve the `Goproxy` itself under other
// proxies, and by setting GONOPROXY and GOPRIVATE to indicate which modules the
// `Goproxy` should download directly instead of using those proxies. And of
// course, you can also set GOSUMDB, GONOSUMDB and GOPRIVATE to indicate how
// the `Goproxy` should verify the modules.
//
// Since GOPROXY (with comma-separated list support), GONOPROXY, GOSUMDB,
// GONOSUMDB and GOPRIVATE were first introduced in Go 1.13, so we implemented a
// built-in support for them. Now, you can set them even the version of the Go
// binary target by the `Goproxy.GoBinName` is before v1.13.
//
// It is highly recommended not to modify the value of any field of the
// `Goproxy` after calling the `Goproxy.ServeHTTP`, which will cause
// unpredictable problems.
//
// The new instances of the `Goproxy` should only be created by calling the
// `New`.
type Goproxy struct {
	// GoBinName is the name of the Go binary.
	//
	// The version of the Go binary target by the `GoBinName` must be at
	// least v1.11.
	//
	// Default value: "go"
	GoBinName string `mapstructure:"go_bin_name"`

	// GoBinEnv is the environment of the Go binary. Each entry is of the
	// form "key=value".
	//
	// Note that GOPROXY (with comma-separated list support), GONOPROXY,
	// GOSUMDB, GONOSUMDB and GOPRIVATE are built-in supported. It means
	// that they can be set even the version of the Go binary target by the
	// `GoBinName` is before v1.13.
	//
	// If the `GoBinEnv` contains duplicate environment keys, only the last
	// value in the slice for each duplicate key is used.
	//
	// Default value: `os.Environ()`
	GoBinEnv []string `mapstructure:"go_bin_env"`

	// GoBinMaxWorkers is the maximum number of commands allowed for the Go
	// binary to execute at the same time.
	//
	// If the `GoBinMaxWorkers` is zero, there is no limitation.
	//
	// Default value: 0
	GoBinMaxWorkers int `mapstructure:"go_bin_max_workers"`

	// GoBinFetchTimeout is the maximum duration allowed for the Go binary
	// to fetch a module.
	//
	// Default value: `time.Minute`
	GoBinFetchTimeout time.Duration `mapstructure:"go_bin_fetch_timeout"`

	// PathPrefix is the prefix of all request paths. It will be used to
	// trim the request paths via the `strings.TrimPrefix`.
	//
	// If the `PathPrefix` is not empty, it should start with "/".
	//
	// Default value: ""
	PathPrefix string `mapstructure:"path_prefix"`

	// Cacher is the `Cacher` that used to cache module files.
	//
	// If the `Cacher` is nil, the module files will be temporarily stored
	// in the local disk and discarded as the request ends.
	//
	// Default value: nil
	Cacher Cacher `mapstructure:"cacher"`

	// CacherMaxCacheBytes is the maximum number of bytes allowed for the
	// `Cacher` to store a cache.
	//
	// If the `CacherMaxCacheBytes` is zero, there is no limitation.
	//
	// Default value: 0
	CacherMaxCacheBytes int `mapstructure:"cacher_max_cache_bytes"`

	// ProxiedSUMDBs is the list of proxied checksum databases. Each value
	// should be given the format of "<sumdb-name>" or
	// "<sumdb-name> <sumdb-URL>". The first format can be seen as a
	// shorthand for the second format. In the case of the first format, the
	// corresponding checksum database URL will be the checksum database
	// name itself as a host with an "https" scheme.
	//
	// Default value: nil
	ProxiedSUMDBs []string `mapstructure:"proxied_sumdbs"`

	// InsecureMode indicates whether the insecure mode is enabled.
	//
	// If the `InsecureMode` is true, TLS accepts any certificate presented
	// by the server and any host name in that certificate.
	InsecureMode bool `mapstructure:"insecure_mode"`

	// ErrorLogger is the `log.Logger` that logs errors that occur while
	// proxying.
	//
	// If the `ErrorLogger` is nil, logging is done via the "log" package's
	// standard logger.
	//
	// Default value: nil
	ErrorLogger *log.Logger `mapstructure:"-"`

	loadOnce        *sync.Once
	httpClient      *http.Client
	goBinEnv        map[string]string
	goBinWorkerChan chan struct{}
	sumdbClient     *sumdb.Client
	proxiedSUMDBs   map[string]string
}

// New returns a new instance of the `Goproxy` with default field values.
//
// The `New` is the only function that creates new instances of the `Goproxy`
// and keeps everything working.
func New() *Goproxy {
	return &Goproxy{
		GoBinName:         "go",
		GoBinEnv:          os.Environ(),
		GoBinFetchTimeout: time.Minute,
		loadOnce:          &sync.Once{},
		httpClient: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
					DualStack: true,
				}).DialContext,
				TLSClientConfig:       &tls.Config{},
				MaxIdleConnsPerHost:   200,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				ForceAttemptHTTP2:     true,
			},
		},
		goBinEnv:      map[string]string{},
		proxiedSUMDBs: map[string]string{},
	}
}

// load loads the stuff of the g up.
func (g *Goproxy) load() {
	g.httpClient.Transport.(*http.Transport).
		TLSClientConfig.InsecureSkipVerify = g.InsecureMode
	g.httpClient.Timeout = g.GoBinFetchTimeout

	for _, env := range g.GoBinEnv {
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

	g.sumdbClient = sumdb.NewClient(&sumdbClientOps{
		envGOPROXY:  g.goBinEnv["GOPROXY"],
		envGOSUMDB:  g.goBinEnv["GOSUMDB"],
		httpClient:  g.httpClient,
		errorLogger: g.ErrorLogger,
	})

	for _, proxiedSUMDB := range g.ProxiedSUMDBs {
		sumdbParts := strings.Fields(proxiedSUMDB)

		sumdbName, err := idna.Lookup.ToASCII(sumdbParts[0])
		if err != nil {
			continue
		}

		if len(sumdbParts) > 1 {
			g.proxiedSUMDBs[sumdbName] = sumdbParts[1]
		} else {
			g.proxiedSUMDBs[sumdbName] = sumdbName
		}
	}
}

// ServeHTTP implements the `http.Handler`.
func (g *Goproxy) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	g.loadOnce.Do(g.load)

	ctx := r.Context()

	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		setResponseCacheControlHeader(rw, 86400)
		responseMethodNotAllowed(rw)
		return
	}

	if !strings.HasPrefix(r.URL.Path, "/") {
		setResponseCacheControlHeader(rw, 86400)
		responseNotFound(rw)
		return
	}

	trimmedPath := path.Clean(r.URL.Path)
	if g.PathPrefix != "" {
		trimmedPath = strings.TrimPrefix(trimmedPath, g.PathPrefix)
	} else {
		trimmedPath = strings.TrimPrefix(trimmedPath, "/")
	}

	name, err := url.PathUnescape(trimmedPath)
	if err != nil {
		setResponseCacheControlHeader(rw, 86400)
		responseNotFound(rw)
		return
	}

	if strings.HasPrefix(name, "sumdb/") {
		sumdbURL, err := parseRawURL(strings.TrimPrefix(name, "sumdb/"))
		if err != nil {
			setResponseCacheControlHeader(rw, 86400)
			responseNotFound(rw)
			return
		}

		sumdbName, err := idna.Lookup.ToASCII(sumdbURL.Host)
		if err != nil {
			setResponseCacheControlHeader(rw, 86400)
			responseNotFound(rw)
			return
		}

		if g.proxiedSUMDBs[sumdbName] == "" {
			setResponseCacheControlHeader(rw, 86400)
			responseNotFound(rw)
			return
		}

		var (
			contentType string
			maxAge      int
		)

		if sumdbURL.Path == "/supported" {
			setResponseCacheControlHeader(rw, 86400)
			rw.Write(nil) // 200 OK
			return
		} else if sumdbURL.Path == "/latest" {
			contentType = "text/plain; charset=utf-8"
			maxAge = 3600
		} else if strings.HasPrefix(sumdbURL.Path, "/lookup/") {
			contentType = "text/plain; charset=utf-8"
			maxAge = 86400
		} else if strings.HasPrefix(sumdbURL.Path, "/tile/") {
			contentType = "application/octet-stream"
			maxAge = 86400
		} else {
			setResponseCacheControlHeader(rw, 86400)
			responseNotFound(rw)
			return
		}

		sumdbURL, err = parseRawURL(fmt.Sprint(
			g.proxiedSUMDBs[sumdbName],
			sumdbURL.Path,
		))
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		var buf bytes.Buffer
		if err := httpGet(
			ctx,
			g.httpClient,
			sumdbURL.String(),
			&buf,
		); err != nil {
			g.logError(err)
			responseModError(rw, err, false)
			return
		}

		rw.Header().Set("Content-Type", contentType)
		rw.Header().Set("Content-Length", strconv.Itoa(buf.Len()))

		setResponseCacheControlHeader(rw, maxAge)
		buf.WriteTo(rw)

		return
	}

	var isLatest, isList bool
	if isLatest = strings.HasSuffix(name, "/@latest"); isLatest {
		name = fmt.Sprint(
			strings.TrimSuffix(name, "latest"),
			"v/latest.info",
		)
	} else if isList = strings.HasSuffix(name, "/@v/list"); isList {
		name = fmt.Sprint(
			strings.TrimSuffix(name, "list"),
			"latest.info",
		)
	}

	nameParts := strings.Split(name, "/@v/")
	if len(nameParts) != 2 {
		setResponseCacheControlHeader(rw, 86400)
		responseNotFound(rw)
		return
	}

	escapedModulePath := nameParts[0]
	modulePath, err := module.UnescapePath(escapedModulePath)
	if err != nil {
		setResponseCacheControlHeader(rw, 86400)
		responseNotFound(rw)
		return
	}

	nameBase := nameParts[1]
	nameExt := path.Ext(nameBase)
	switch nameExt {
	case ".info", ".mod", ".zip":
	default:
		setResponseCacheControlHeader(rw, 86400)
		responseNotFound(rw)
		return
	}

	escapedModuleVersion := strings.TrimSuffix(nameBase, nameExt)
	moduleVersion, err := module.UnescapeVersion(escapedModuleVersion)
	if err != nil {
		setResponseCacheControlHeader(rw, 86400)
		responseNotFound(rw)
		return
	}

	goproxyRoot, err := ioutil.TempDir("", "goproxy")
	if err != nil {
		g.logError(err)
		responseInternalServerError(rw)
		return
	}
	defer func() {
		go func() {
			<-ctx.Done()
			os.RemoveAll(goproxyRoot)
		}()
	}()

	if isList {
		mr, err := g.mod(
			ctx,
			"list",
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			g.logError(err)
			responseModError(rw, err, true)
			return
		}

		setResponseCacheControlHeader(rw, 60)
		responseString(
			rw,
			http.StatusOK,
			strings.Join(mr.Versions, "\n"),
		)

		return
	} else if !semver.IsValid(moduleVersion) {
		var operation string
		if isLatest {
			operation = "latest"
		} else if nameExt == ".info" {
			operation = "lookup"
		} else {
			setResponseCacheControlHeader(rw, 86400)
			responseNotFound(rw, fmt.Sprintf(
				"%s@%s: invalid version: unknown revision %s",
				modulePath,
				moduleVersion,
				moduleVersion,
			))
			return
		}

		mr, err := g.mod(
			ctx,
			operation,
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			g.logError(err)
			responseModError(rw, err, true)
			return
		}

		b, err := json.Marshal(struct {
			Version string
			Time    time.Time
		}{
			mr.Version,
			mr.Time,
		})
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		setResponseCacheControlHeader(rw, 60)
		responseJSON(rw, http.StatusOK, b)

		return
	}

	cache, err := g.cache(ctx, name)
	if errors.Is(err, ErrCacheNotFound) {
		mr, err := g.mod(
			ctx,
			"download",
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			g.logError(err)
			responseModError(rw, err, false)
			return
		}

		if g.goBinEnv["GOSUMDB"] != "off" &&
			!globsMatchPath(g.goBinEnv["GONOSUMDB"], modulePath) {
			zipLines, err := g.sumdbClient.Lookup(
				modulePath,
				moduleVersion,
			)
			if err != nil {
				g.logError(err)
				responseModError(rw, err, false)
				return
			}

			zipHash, err := dirhash.HashZip(
				mr.Zip,
				dirhash.DefaultHash,
			)
			if err != nil {
				g.logError(err)
				responseInternalServerError(rw)
				return
			}

			if !stringSliceContains(
				zipLines,
				fmt.Sprintf(
					"%s %s %s",
					modulePath,
					moduleVersion,
					zipHash,
				),
			) {
				setResponseCacheControlHeader(rw, 86400)
				responseNotFound(rw, fmt.Sprintf(
					"%s@%s: invalid version: untrusted "+
						"revision %s",
					modulePath,
					moduleVersion,
					moduleVersion,
				))
				return
			}

			modLines, err := g.sumdbClient.Lookup(
				modulePath,
				fmt.Sprint(moduleVersion, "/go.mod"),
			)
			if err != nil {
				g.logError(err)
				responseModError(rw, err, false)
				return
			}

			modHash, err := dirhash.Hash1(
				[]string{"go.mod"},
				func(string) (io.ReadCloser, error) {
					return os.Open(mr.GoMod)
				},
			)
			if err != nil {
				g.logError(err)
				responseInternalServerError(rw)
				return
			}

			if !stringSliceContains(
				modLines,
				fmt.Sprintf(
					"%s %s/go.mod %s",
					modulePath,
					moduleVersion,
					modHash,
				),
			) {
				setResponseCacheControlHeader(rw, 86400)
				responseNotFound(rw, fmt.Sprintf(
					"%s@%s: invalid version: untrusted "+
						"revision %s",
					modulePath,
					moduleVersion,
					moduleVersion,
				))
				return
			}
		}

		namePrefix := strings.TrimSuffix(name, nameExt)

		newHash := md5.New
		if g.Cacher != nil {
			newHash = g.Cacher.NewHash
		}

		// Using a new `context.Context` instead of the `Context` of the
		// r to avoid early timeouts.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(
			context.Background(),
			2*time.Minute,
		)

		go func() {
			defer cancel()

			infoCache, err := newTempCache(
				mr.Info,
				fmt.Sprint(namePrefix, ".info"),
				newHash(),
			)
			if err != nil {
				g.logError(err)
				return
			}
			defer infoCache.Close()

			if err := g.setCache(ctx, infoCache); err != nil {
				g.logError(err)
				return
			}

			modCache, err := newTempCache(
				mr.GoMod,
				fmt.Sprint(namePrefix, ".mod"),
				newHash(),
			)
			if err != nil {
				g.logError(err)
				return
			}
			defer modCache.Close()

			if err := g.setCache(ctx, modCache); err != nil {
				g.logError(err)
				return
			}

			zipCache, err := newTempCache(
				mr.Zip,
				fmt.Sprint(namePrefix, ".zip"),
				newHash(),
			)
			if err != nil {
				g.logError(err)
				return
			}
			defer zipCache.Close()

			if err := g.setCache(ctx, zipCache); err != nil {
				g.logError(err)
				return
			}
		}()

		var filename string
		switch nameExt {
		case ".info":
			filename = mr.Info
		case ".mod":
			filename = mr.GoMod
		case ".zip":
			filename = mr.Zip
		}

		cache, err = newTempCache(filename, name, newHash())
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}
	} else if err != nil {
		g.logError(err)
		responseModError(rw, err, false)
		return
	}
	defer cache.Close()

	rw.Header().Set("Content-Type", cache.MIMEType())
	rw.Header().Set("ETag", fmt.Sprintf(
		"%q",
		base64.StdEncoding.EncodeToString(cache.Checksum()),
	))

	setResponseCacheControlHeader(rw, 604800)
	http.ServeContent(rw, r, "", cache.ModTime(), cache)
}

// cache returns the matched `Cache` for the name from the `Cacher` of the g.
func (g *Goproxy) cache(ctx context.Context, name string) (Cache, error) {
	if g.Cacher == nil {
		return nil, ErrCacheNotFound
	}

	return g.Cacher.Cache(ctx, name)
}

// setCache sets the c to the `Cacher` of the g.
func (g *Goproxy) setCache(ctx context.Context, c Cache) error {
	if g.Cacher == nil {
		return nil
	}

	if g.CacherMaxCacheBytes != 0 &&
		c.Size() > int64(g.CacherMaxCacheBytes) {
		return nil
	}

	return g.Cacher.SetCache(ctx, c)
}

// logErrorf logs the v as an error in the format.
func (g *Goproxy) logErrorf(format string, v ...interface{}) {
	s := fmt.Sprintf(format, v...)
	if g.ErrorLogger != nil {
		g.ErrorLogger.Output(2, s)
	} else {
		log.Output(2, s)
	}
}

// logError logs the err.
func (g *Goproxy) logError(err error) {
	g.logErrorf("%v", err)
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
// glob patterns (as defined by the `path.Match`) in the comma-separated globs
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
