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
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/net/idna"
)

// regModuleVersionNotFound is a regular expression that used to report whether
// a message means a module version is not found.
var regModuleVersionNotFound = regexp.MustCompile(
	`(400 Bad Request)|` +
		`(403 Forbidden)|` +
		`(404 Not Found)|` +
		`(410 Gone)|` +
		`(^not found: .*)|` +
		`(could not read Username)|` +
		`(does not contain package)|` +
		`(go.mod has non-.* module path)|` +
		`(go.mod has post-.* module path)|` +
		`(invalid .* import path)|` +
		`(invalid pseudo-version)|` +
		`(invalid version)|` +
		`(missing .*/go.mod and .*/go.mod at revision)|` +
		`(no matching versions)|` +
		`(repository .* not found)|` +
		`(unable to connect to)|` +
		`(unknown revision)|` +
		`(unrecognized import path)`,
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
	GoBinEnv []string

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

	// SupportedSUMDBHosts is the supported checksum database hosts.
	//
	// Default value: ["sum.golang.org"]
	SupportedSUMDBHosts []string `mapstructure:"supported_sumdb_hosts"`

	// ErrorLogger is the `log.Logger` that logs errors that occur while
	// proxing.
	//
	// If the `ErrorLogger` is nil, logging is done via the "log" package's
	// standard logger.
	//
	// Default value: nil
	ErrorLogger *log.Logger `mapstructure:"-"`

	// DisableNotFoundLog is a switch that disables "Not Found" log.
	//
	// Default value: false
	DisableNotFoundLog bool `mapstructure:"disable_not_found_log"`

	loadOnce            *sync.Once
	goBinEnv            map[string]string
	goBinWorkerChan     chan struct{}
	sumdbClient         *sumdb.Client
	supportedSUMDBHosts map[string]bool
}

// New returns a new instance of the `Goproxy` with default field values.
//
// The `New` is the only function that creates new instances of the `Goproxy`
// and keeps everything working.
func New() *Goproxy {
	return &Goproxy{
		GoBinName:           "go",
		GoBinEnv:            os.Environ(),
		SupportedSUMDBHosts: []string{"sum.golang.org"},
		loadOnce:            &sync.Once{},
		goBinEnv:            map[string]string{},
		supportedSUMDBHosts: map[string]bool{},
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

	envGOSUMDB := g.goBinEnv["GOSUMDB"]
	switch envGOSUMDB {
	case "", "sum.golang.org":
		envGOSUMDB = "sum.golang.org" +
			"+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8"
	}

	g.sumdbClient = sumdb.NewClient(&sumdbClientOps{
		envGOPROXY:  g.goBinEnv["GOPROXY"],
		envGOSUMDB:  envGOSUMDB,
		errorLogger: g.ErrorLogger,
	})

	for _, host := range g.SupportedSUMDBHosts {
		if h, err := idna.Lookup.ToASCII(host); err == nil {
			g.supportedSUMDBHosts[h] = true
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

	cachingForever := false
	if strings.HasPrefix(name, "sumdb/") {
		sumdbURL := strings.TrimPrefix(name, "sumdb/")
		sumdbPathOffset := strings.Index(sumdbURL, "/")
		if sumdbPathOffset < 0 {
			setResponseCacheControlHeader(rw, 3600)
			responseNotFound(rw)
			return
		}

		sumdbHost := sumdbURL[:sumdbPathOffset]
		sumdbHost, err := idna.Lookup.ToASCII(sumdbHost)
		if err != nil {
			setResponseCacheControlHeader(rw, 3600)
			responseNotFound(rw)
			return
		}

		if !g.supportedSUMDBHosts[sumdbHost] {
			setResponseCacheControlHeader(rw, 60)
			responseNotFound(rw)
			return
		}

		sumdbPath := sumdbURL[sumdbPathOffset:]
		var contentType string
		switch {
		case sumdbPath == "/supported":
			setResponseCacheControlHeader(rw, 60)
			rw.Write(nil) // 200 OK
			return
		case sumdbPath == "/latest":
			contentType = "text/plain; charset=utf-8"
		case strings.HasPrefix(sumdbPath, "/lookup/"):
			cachingForever = true
			contentType = "text/plain; charset=utf-8"
		case strings.HasPrefix(sumdbPath, "/tile/"):
			cachingForever = true
			contentType = "application/octet-stream"
		default:
			setResponseCacheControlHeader(rw, 3600)
			responseNotFound(rw)
			return
		}

		sumdbURL = fmt.Sprint("https://", sumdbHost, sumdbPath)

		sumdbReq, err := http.NewRequest(http.MethodGet, sumdbURL, nil)
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		sumdbRes, err := http.DefaultClient.Do(
			sumdbReq.WithContext(r.Context()),
		)
		if err != nil {
			if ue, ok := err.(*url.Error); ok && ue.Timeout() {
				responseBadGateway(rw)
			} else {
				g.logError(err)
				responseInternalServerError(rw)
			}

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

				if sumdbRes.StatusCode == http.StatusNotFound {
					setResponseCacheControlHeader(rw, 60)
				} else {
					setResponseCacheControlHeader(rw, 3600)
				}

				responseNotFound(rw, string(b))

				return
			}

			g.logError(fmt.Errorf(
				"GET %s: %s: %s",
				sumdbURL,
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

		if cachingForever {
			setResponseCacheControlHeader(rw, 365*24*3600)
		} else {
			setResponseCacheControlHeader(rw, 60)
		}

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

	hijackedGoproxyRootRemoval := false
	defer func() {
		if !hijackedGoproxyRootRemoval {
			os.RemoveAll(goproxyRoot)
		}
	}()

	if isList {
		mr, err := mod(
			"list",
			g.GoBinName,
			g.goBinEnv,
			g.goBinWorkerChan,
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			if regModuleVersionNotFound.MatchString(err.Error()) {
				if !g.DisableNotFoundLog {
					g.logError(err)
				}

				setResponseCacheControlHeader(rw, 60)
				responseNotFound(rw, err)
			} else {
				g.logError(err)
				responseInternalServerError(rw)
			}

			return
		}

		versions := strings.Join(mr.Versions, "\n")

		setResponseCacheControlHeader(rw, 60)
		responseString(rw, http.StatusOK, versions)

		return
	} else if isLatest || !semver.IsValid(moduleVersion) {
		var operation string
		if isLatest {
			operation = "latest"
		} else {
			operation = "lookup"
		}

		mr, err := mod(
			operation,
			g.GoBinName,
			g.goBinEnv,
			g.goBinWorkerChan,
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			if regModuleVersionNotFound.MatchString(err.Error()) {
				if !g.DisableNotFoundLog {
					g.logError(err)
				}

				setResponseCacheControlHeader(rw, 60)
				responseNotFound(rw, err)
			} else {
				g.logError(err)
				responseInternalServerError(rw)
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
	} else {
		cachingForever = true
	}

	cacher := g.Cacher
	if cacher == nil {
		cacher = &tempCacher{}
	}

	cache, err := cacher.Cache(r.Context(), name)
	if err == ErrCacheNotFound {
		mr, err := mod(
			"download",
			g.GoBinName,
			g.goBinEnv,
			g.goBinWorkerChan,
			goproxyRoot,
			modulePath,
			moduleVersion,
		)
		if err != nil {
			if regModuleVersionNotFound.MatchString(err.Error()) {
				if !g.DisableNotFoundLog {
					g.logError(err)
				}

				setResponseCacheControlHeader(rw, 60)
				responseNotFound(rw, err)
			} else {
				g.logError(err)
				responseInternalServerError(rw)
			}

			return
		}

		var envGOSUMDB string
		if globsMatchPath(g.goBinEnv["GONOSUMDB"], modulePath) ||
			globsMatchPath(g.goBinEnv["GOPRIVATE"], modulePath) {
			envGOSUMDB = "off"
		} else {
			envGOSUMDB = g.goBinEnv["GOSUMDB"]
		}

		if envGOSUMDB != "off" {
			zipHash, err := dirhash.HashZip(
				mr.Zip,
				dirhash.DefaultHash,
			)
			if err != nil {
				g.logError(err)
				responseInternalServerError(rw)
				return
			}

			zipHashLine := fmt.Sprintf(
				"%s %s %s",
				modulePath,
				moduleVersion,
				zipHash,
			)

			lines, err := g.sumdbClient.Lookup(
				modulePath,
				moduleVersion,
			)
			if err != nil {
				err := errors.New(strings.TrimPrefix(
					err.Error(),
					fmt.Sprintf(
						"%s@%s: ",
						modulePath,
						moduleVersion,
					),
				))

				if regModuleVersionNotFound.MatchString(
					err.Error(),
				) {
					if !g.DisableNotFoundLog {
						g.logError(err)
					}

					setResponseCacheControlHeader(rw, 60)
					responseNotFound(rw, err)
				} else {
					g.logError(err)
					responseInternalServerError(rw)
				}

				return
			}

			if !stringSliceContains(lines, zipHashLine) {
				setResponseCacheControlHeader(rw, 3600)
				responseNotFound(rw, fmt.Sprintf(
					"untrusted revision %s",
					moduleVersion,
				))
				return
			}
		}

		// Setting the caches asynchronously to avoid timeouts in
		// response.
		hijackedGoproxyRootRemoval = true
		go func() {
			defer os.RemoveAll(goproxyRoot)

			namePrefix := strings.TrimSuffix(name, nameExt)

			// Using a new `context.Context` instead of the
			// `r.Context` to avoid early timeouts.
			ctx, cancel := context.WithTimeout(
				context.Background(),
				2*time.Minute,
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

			if err := cacher.SetCache(ctx, zipCache); err != nil {
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

		cache, err = newTempCache(filename, name, cacher.NewHash())
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}
	} else if err != nil {
		g.logError(err)
		responseInternalServerError(rw)
		return
	}
	defer cache.Close()

	rw.Header().Set("Content-Type", cache.MIMEType())
	rw.Header().Set(
		"ETag",
		fmt.Sprintf(
			"%q",
			base64.StdEncoding.EncodeToString(cache.Checksum()),
		),
	)

	if cachingForever {
		setResponseCacheControlHeader(rw, 365*24*3600)
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
