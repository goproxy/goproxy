/*
Package goproxy implements a minimalist Go module proxy handler.
*/
package goproxy

import (
	"context"
	"encoding/base64"
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
	"path/filepath"
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
// will still follow your GOPROXY, GONOPROXY, GOSUMDB, GONOSUMDB, and GOPRIVATE.
// It means that you can set GOPROXY to serve the `Goproxy` itself under other
// proxies, and by setting GONOPROXY and GOPRIVATE to indicate which modules the
// `Goproxy` should download directly instead of using those proxies. And of
// course, you can also set GOSUMDB, GONOSUMDB, and GOPRIVATE to indicate how
// the `Goproxy` should verify the modules.
//
// ATTENTION: Since GONOPROXY, GOSUMDB, GONOSUMDB, and GOPRIVATE were first
// introduced in Go 1.13, so we implemented a built-in support for them. Now,
// you can set them even before Go 1.13.
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
	// Default value: "go"
	GoBinName string `mapstructure:"go_bin_name"`

	// GoBinEnv is the environment of the Go binary. Each entry is of the
	// form "key=value".
	//
	// If the `GoBinEnv` contains duplicate environment keys, only the last
	// value in the slice for each duplicate key is used.
	//
	// Default value: `os.Environ()`
	GoBinEnv []string `mapstructure:"go_bin_env"`

	// MaxGoBinWorkers is the maximum number of the Go binary commands that
	// are allowed to execute at the same time.
	//
	// If the `MaxGoBinWorkers` is zero, then there will be no limitations.
	//
	// Default value: 0
	MaxGoBinWorkers int `mapstructure:"max_go_bin_workers"`

	// PathPrefix is the prefix of all request paths. It will be used to
	// trim the request paths via `strings.TrimPrefix`.
	//
	// Note that when the `PathPrefix` is not empty, then it should start
	// with "/".
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

	// MaxZIPCacheBytes is the maximum number of bytes of the ZIP cache that
	// will be stored in the `Cacher`.
	//
	// If the `MaxZIPCacheBytes` is zero, then there will be no limitations.
	//
	// Default value: 0
	MaxZIPCacheBytes int `mapstructure:"max_zip_cache_bytes"`

	// ProxiedSUMDBNames is the proxied checksum database names.
	//
	// Default value: nil
	ProxiedSUMDBNames []string `mapstructure:"proxied_sumdb_names"`

	// ErrorLogger is the `log.Logger` that logs errors that occur while
	// proxying.
	//
	// If the `ErrorLogger` is nil, logging is done via the "log" package's
	// standard logger.
	//
	// Default value: nil
	ErrorLogger *log.Logger `mapstructure:"-"`

	// DisableNotFoundLog is a switch that disables "not found" log.
	//
	// Default value: false
	DisableNotFoundLog bool `mapstructure:"disable_not_found_log"`

	loadOnce          *sync.Once
	httpClient        *http.Client
	goBinEnv          map[string]string
	goBinWorkerChan   chan struct{}
	sumdbClient       *sumdb.Client
	proxiedSUMDBNames map[string]bool
}

// New returns a new instance of the `Goproxy` with default field values.
//
// The `New` is the only function that creates new instances of the `Goproxy`
// and keeps everything working.
func New() *Goproxy {
	return &Goproxy{
		GoBinName: "go",
		GoBinEnv:  os.Environ(),
		loadOnce:  &sync.Once{},
		httpClient: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
					DualStack: true,
				}).DialContext,
				MaxIdleConnsPerHost:   1024,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
			Timeout: time.Minute,
		},
		goBinEnv:          map[string]string{},
		proxiedSUMDBNames: map[string]bool{},
	}
}

// load loads the stuff of the g up.
func (g *Goproxy) load() {
	for _, env := range g.GoBinEnv {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		g.goBinEnv[parts[0]] = parts[1]
	}

	if g.MaxGoBinWorkers != 0 {
		g.goBinWorkerChan = make(chan struct{}, g.MaxGoBinWorkers)
	}

	var proxies []string
	for _, proxy := range strings.Split(g.goBinEnv["GOPROXY"], ",") {
		proxy = strings.TrimSpace(proxy)
		if proxy == "" {
			continue
		}

		proxies = append(proxies, proxy)
		if proxy == "direct" || proxy == "off" {
			break
		}
	}

	if len(proxies) > 0 {
		g.goBinEnv["GOPROXY"] = strings.Join(proxies, ",")
	} else if g.goBinEnv["GOPROXY"] == "" {
		g.goBinEnv["GOPROXY"] = "https://proxy.golang.org,direct"
	} else {
		g.goBinEnv["GOPROXY"] = "off"
	}

	g.goBinEnv["GOSUMDB"] = strings.TrimSpace(g.goBinEnv["GOSUMDB"])
	switch g.goBinEnv["GOSUMDB"] {
	case "", "sum.golang.org":
		g.goBinEnv["GOSUMDB"] = "sum.golang.org" +
			"+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8"
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

	g.sumdbClient = sumdb.NewClient(&sumdbClientOps{
		envGOPROXY:  g.goBinEnv["GOPROXY"],
		envGOSUMDB:  g.goBinEnv["GOSUMDB"],
		httpClient:  g.httpClient,
		errorLogger: g.ErrorLogger,
	})

	for _, sumdbName := range g.ProxiedSUMDBNames {
		if n, err := idna.Lookup.ToASCII(sumdbName); err == nil {
			g.proxiedSUMDBNames[n] = true
		}
	}
}

// ServeHTTP implements the `http.Handler`.
func (g *Goproxy) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	g.loadOnce.Do(g.load)

	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		setResponseCacheControlHeader(rw, 3600)
		responseMethodNotAllowed(rw)
		return
	}

	if !strings.HasPrefix(r.URL.Path, "/") {
		setResponseCacheControlHeader(rw, 3600)
		responseNotFound(rw)
		return
	}

	trimmedPath := path.Clean(r.URL.Path)
	trimmedPath = strings.TrimPrefix(trimmedPath, g.PathPrefix)
	trimmedPath = strings.TrimLeft(trimmedPath, "/")

	name, err := url.PathUnescape(trimmedPath)
	if err != nil {
		setResponseCacheControlHeader(rw, 3600)
		responseNotFound(rw)
		return
	}

	if strings.HasPrefix(name, "sumdb/") {
		sumdbURL, err := parseRawURL(strings.TrimPrefix(name, "sumdb/"))
		if err != nil {
			setResponseCacheControlHeader(rw, 3600)
			responseNotFound(rw)
			return
		}

		sumdbName, err := idna.Lookup.ToASCII(sumdbURL.Host)
		if err != nil {
			setResponseCacheControlHeader(rw, 3600)
			responseNotFound(rw)
			return
		}

		if !g.proxiedSUMDBNames[sumdbName] {
			setResponseCacheControlHeader(rw, 3600)
			responseNotFound(rw)
			return
		}

		var contentType string
		switch {
		case sumdbURL.Path == "/supported":
			setResponseCacheControlHeader(rw, 3600)
			rw.Write(nil) // 200 OK
			return
		case sumdbURL.Path == "/latest":
			contentType = "text/plain; charset=utf-8"
		case strings.HasPrefix(sumdbURL.Path, "/lookup/"):
			contentType = "text/plain; charset=utf-8"
		case strings.HasPrefix(sumdbURL.Path, "/tile/"):
			contentType = "application/octet-stream"
		default:
			setResponseCacheControlHeader(rw, 3600)
			responseNotFound(rw)
			return
		}

		sumdbReq, err := http.NewRequest(
			http.MethodGet,
			sumdbURL.String(),
			nil,
		)
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		sumdbReq = sumdbReq.WithContext(r.Context())

		sumdbRes, err := g.httpClient.Do(sumdbReq)
		if err != nil {
			if !isTimeoutError(err) {
				g.logError(err)
				responseInternalServerError(rw)
				return
			}

			if !g.DisableNotFoundLog {
				g.logError(err)
			}

			setResponseCacheControlHeader(rw, 0)
			responseNotFound(rw, "fetch timed out")

			return
		}
		defer sumdbRes.Body.Close()

		if sumdbRes.StatusCode != http.StatusOK {
			b, err := ioutil.ReadAll(sumdbRes.Body)
			if err != nil {
				g.logError(err)
				responseInternalServerError(rw)
				return
			}

			switch sumdbRes.StatusCode {
			case http.StatusBadRequest,
				http.StatusNotFound,
				http.StatusGone:
				if !g.DisableNotFoundLog {
					g.logErrorf("%s", b)
				}

				setResponseCacheControlHeader(rw, 60)
				responseNotFound(rw, string(b))

				return
			}

			g.logError(fmt.Errorf(
				"GET %s: %s: %s",
				redactedURL(sumdbURL),
				sumdbRes.Status,
				b,
			))

			responseBadGateway(rw)

			return
		}

		rw.Header().Set("Content-Type", contentType)
		rw.Header().Set(
			"Content-Length",
			sumdbRes.Header.Get("Content-Length"),
		)

		setResponseCacheControlHeader(rw, 3600)

		io.Copy(rw, sumdbRes.Body)

		return
	}

	isLatest := false
	isList := false
	switch {
	case strings.HasSuffix(name, "/@latest"):
		name = fmt.Sprint(
			strings.TrimSuffix(name, "latest"),
			"v/latest.info",
		)
		isLatest = true
	case strings.HasSuffix(name, "/@v/list"):
		name = fmt.Sprint(
			strings.TrimSuffix(name, "list"),
			"latest.info",
		)
		isList = true
	}

	nameParts := strings.Split(name, "/@v/")
	if len(nameParts) != 2 {
		setResponseCacheControlHeader(rw, 3600)
		responseNotFound(rw)
		return
	}

	escapedModulePath := nameParts[0]
	modulePath, err := module.UnescapePath(escapedModulePath)
	if err != nil {
		setResponseCacheControlHeader(rw, 3600)
		responseNotFound(rw)
		return
	}

	nameBase := nameParts[1]
	nameExt := path.Ext(nameBase)
	switch nameExt {
	case ".info", ".mod", ".zip":
	default:
		setResponseCacheControlHeader(rw, 3600)
		responseNotFound(rw)
		return
	}

	escapedModuleVersion := strings.TrimSuffix(nameBase, nameExt)
	moduleVersion, err := module.UnescapeVersion(escapedModuleVersion)
	if err != nil {
		setResponseCacheControlHeader(rw, 3600)
		responseNotFound(rw)
		return
	}

	goproxyRoot, err := ioutil.TempDir("", "goproxy")
	if err != nil {
		g.logError(err)
		responseInternalServerError(rw)
		return
	}
	defer os.RemoveAll(goproxyRoot)
	defer modClean(g.GoBinName, g.goBinEnv, goproxyRoot)

	if isList {
		mr, err := mod(
			r.Context(),
			"list",
			g.httpClient,
			g.GoBinName,
			g.goBinEnv,
			g.goBinWorkerChan,
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			if !isNotFoundError(err) {
				g.logError(err)
				responseInternalServerError(rw)
				return
			}

			if !g.DisableNotFoundLog {
				g.logError(err)
			}

			setResponseCacheControlHeader(rw, 0)
			if isTimeoutError(err) {
				responseNotFound(rw, "fetch timed out")
			} else {
				responseNotFound(rw, err)
			}

			return
		}

		versions := strings.Join(mr.Versions, "\n")

		setResponseCacheControlHeader(rw, 60)
		responseString(rw, http.StatusOK, versions)

		return
	}

	isOriginalModuleVersionValid := semver.IsValid(moduleVersion)
	if !isOriginalModuleVersionValid {
		var operation string
		if isLatest {
			operation = "latest"
		} else if nameExt == ".info" {
			operation = "lookup"
		} else {
			setResponseCacheControlHeader(rw, 3600)
			responseNotFound(rw, "unrecognized version")
			return
		}

		mr, err := mod(
			r.Context(),
			operation,
			g.httpClient,
			g.GoBinName,
			g.goBinEnv,
			g.goBinWorkerChan,
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			if !isNotFoundError(err) {
				g.logError(err)
				responseInternalServerError(rw)
				return
			}

			if !g.DisableNotFoundLog {
				g.logError(err)
			}

			setResponseCacheControlHeader(rw, 0)
			if isTimeoutError(err) {
				responseNotFound(rw, "fetch timed out")
			} else {
				responseNotFound(rw, err)
			}

			return
		}

		moduleVersion = mr.Version
		escapedModuleVersion, err = module.EscapeVersion(moduleVersion)
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		nameBase = fmt.Sprint(escapedModuleVersion, nameExt)
		name = path.Join(path.Dir(name), nameBase)
	}

	cacher := g.Cacher
	if cacher == nil {
		cacher = &tempCacher{}
	}

	cache, err := cacher.Cache(r.Context(), name)
	if err == ErrCacheNotFound {
		mr, err := mod(
			r.Context(),
			"download",
			g.httpClient,
			g.GoBinName,
			g.goBinEnv,
			g.goBinWorkerChan,
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			if !isNotFoundError(err) {
				g.logError(err)
				responseInternalServerError(rw)
				return
			}

			if !g.DisableNotFoundLog {
				g.logError(err)
			}

			setResponseCacheControlHeader(rw, 0)
			if isTimeoutError(err) {
				responseNotFound(rw, "fetch timed out")
			} else {
				responseNotFound(rw, err)
			}

			return
		}

		if g.goBinEnv["GOSUMDB"] != "off" &&
			!globsMatchPath(g.goBinEnv["GONOSUMDB"], modulePath) {
			zipLines, err := g.sumdbClient.Lookup(
				modulePath,
				moduleVersion,
			)
			if err != nil {
				trimmedError := errors.New(strings.TrimPrefix(
					err.Error(),
					fmt.Sprintf(
						"%s@%s: ",
						modulePath,
						moduleVersion,
					),
				))

				if !isNotFoundError(err) {
					g.logError(trimmedError)
					responseInternalServerError(rw)
					return
				}

				if !g.DisableNotFoundLog {
					g.logError(trimmedError)
				}

				setResponseCacheControlHeader(rw, 0)
				if isTimeoutError(err) {
					responseNotFound(rw, "fetch timed out")
				} else {
					responseNotFound(rw, trimmedError)
				}

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
				setResponseCacheControlHeader(rw, 3600)
				responseNotFound(rw, fmt.Sprintf(
					"untrusted revision %s",
					moduleVersion,
				))
				return
			}

			modLines, err := g.sumdbClient.Lookup(
				modulePath,
				fmt.Sprint(moduleVersion, "/go.mod"),
			)
			if err != nil {
				trimmedError := errors.New(strings.TrimPrefix(
					err.Error(),
					fmt.Sprintf(
						"%s@%s: ",
						modulePath,
						moduleVersion,
					),
				))

				if !isNotFoundError(err) {
					g.logError(trimmedError)
					responseInternalServerError(rw)
					return
				}

				if !g.DisableNotFoundLog {
					g.logError(trimmedError)
				}

				setResponseCacheControlHeader(rw, 0)
				if isTimeoutError(err) {
					responseNotFound(rw, "fetch timed out")
				} else {
					responseNotFound(rw, trimmedError)
				}

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
				setResponseCacheControlHeader(rw, 3600)
				responseNotFound(rw, fmt.Sprintf(
					"untrusted revision %s",
					moduleVersion,
				))
				return
			}
		}

		namePrefix := strings.TrimSuffix(name, nameExt)

		// Using a new `context.Context` instead of the `r.Context` to
		// avoid early timeouts.
		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Minute,
		)
		defer cancel()

		infoCache, err := newTempCache(
			mr.Info,
			fmt.Sprint(namePrefix, ".info"),
			cacher.NewHash(),
		)
		if err != nil {
			g.logError(err)
			return
		}
		defer infoCache.Close()

		if err := cacher.SetCache(ctx, infoCache); err != nil {
			g.logError(err)
			return
		}

		modCache, err := newTempCache(
			mr.GoMod,
			fmt.Sprint(namePrefix, ".mod"),
			cacher.NewHash(),
		)
		if err != nil {
			g.logError(err)
			return
		}
		defer modCache.Close()

		if err := cacher.SetCache(ctx, modCache); err != nil {
			g.logError(err)
			return
		}

		zipCache, err := newTempCache(
			mr.Zip,
			fmt.Sprint(namePrefix, ".zip"),
			cacher.NewHash(),
		)
		if err != nil {
			g.logError(err)
			return
		}
		defer zipCache.Close()

		if g.MaxZIPCacheBytes == 0 ||
			zipCache.Size() <= int64(g.MaxZIPCacheBytes) {
			if err := cacher.SetCache(ctx, zipCache); err != nil {
				g.logError(err)
				return
			}
		}

		var filename string
		switch nameExt {
		case ".info":
			filename = mr.Info
		case ".mod":
			filename = mr.GoMod
		case ".zip":
			filename = mr.Zip
		}

		cache, err = newTempCache(filename, name, cacher.NewHash())
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}
	} else if err != nil {
		if !isTimeoutError(err) {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		if !g.DisableNotFoundLog {
			g.logError(err)
		}

		setResponseCacheControlHeader(rw, 0)
		responseNotFound(rw, "fetch timed out")

		return
	}
	defer cache.Close()

	rw.Header().Set("Content-Type", cache.MIMEType())
	rw.Header().Set("ETag", fmt.Sprintf(
		"%q",
		base64.StdEncoding.EncodeToString(cache.Checksum()),
	))

	if isOriginalModuleVersionValid {
		setResponseCacheControlHeader(rw, 3600)
	} else {
		setResponseCacheControlHeader(rw, 60)
	}

	http.ServeContent(rw, r, "", cache.ModTime(), cache)
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

// notFoundError is an error indicating that something was not found.
type notFoundError error

// isNotFoundError reports whether the err means something was not found.
func isNotFoundError(err error) bool {
	_, ok := err.(notFoundError)
	return ok || isTimeoutError(err)
}

// isTimeoutError reports whether the err means an operation has timed out.
func isTimeoutError(err error) bool {
	if ue, ok := err.(*url.Error); ok && ue.Timeout() {
		return true
	}

	return err == context.DeadlineExceeded
}

// parseRawURL parses the rawURL.
func parseRawURL(rawURL string) (*url.URL, error) {
	if strings.ContainsAny(rawURL, ".:/") &&
		!strings.Contains(rawURL, ":/") &&
		!filepath.IsAbs(rawURL) &&
		!path.IsAbs(rawURL) {
		rawURL = fmt.Sprint("https://", rawURL)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf(
			"invalid URL scheme (must be http or https): %s",
			redactedURL(u),
		)
	}

	return u, nil
}

// appendURL appends the extraPaths to the u safely and reutrns a new instance
// of the `url.URL`.
func appendURL(u *url.URL, extraPaths ...string) *url.URL {
	nu := *u
	u = &nu
	for _, ep := range extraPaths {
		u.Path = path.Join(u.Path, ep)
		u.RawPath = path.Join(
			u.RawPath,
			strings.Replace(url.PathEscape(ep), "%2F", "/", -1),
		)
	}

	return u
}

// redactedURL returns a redacted string form of the u, suitable for printing in
// error messages. The string form replaces any non-empty password in the u with
// "[redacted]".
func redactedURL(u *url.URL) string {
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			nu := *u
			u = &nu
			u.User = url.UserPassword(
				u.User.Username(),
				"[redacted]",
			)
		}
	}

	return u.String()
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
