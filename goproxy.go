/*
Package goproxy implements a minimalist Go module proxy handler.
*/
package goproxy

import (
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/net/idna"
)

// regModuleVersionNotFound is a regular expression that used to report whether
// a message means a module version is not found.
var regModuleVersionNotFound = regexp.MustCompile(
	`(404 Not Found)|` +
		`(410 Gone)|` +
		`(could not read Username)|` +
		`(does not contain package)|` +
		`(go.mod has non-\.\.\.(\.v1|/v[2-9][0-9]*) module path)|` +
		`(go.mod has post-v0 module path)|` +
		`(invalid .* import path)|` +
		`(invalid version)|` +
		`(missing .*/go.mod and .*/go.mod at revision)|` +
		`(no matching versions)|` +
		`(unknown revision)|` +
		`(unrecognized import path)`,
)

// Goproxy is the top-level struct of this project.
//
// Note that the `Goproxy` will not mess with your environment variables, it
// will still follow your GOPROXY, GONOPROXY, GOSUMDB, GONOSUMDB, and GOPRIVATE.
// It means that you can set GOPROXY to serve the `Goproxy` itself under other
// proxies, and by setting GONOPROXY and GOPRIVATE to indicate which modules the
// `Goproxy` should download directly instead of using GOPROXY. And of course,
// you can also set GOSUMDB, GONOSUMDB, and GOPRIVATE to indicate how the
// `Goproxy` should verify the modules.
//
// ATTENTION: Since GONOPROXY and GOPRIVATE have not yet been released (they
// will be introduced in Go 1.13), so we implemented a built-in support for
// them. Now, you can set them even before Go 1.13.
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
	// Default: "go"
	GoBinName string `mapstructure:"go_bin_name"`

	// MaxGoBinWorkers is the maximum number of the Go binary commands that
	// are allowed to execute at the same time.
	//
	// If the `MaxGoBinWorkers` is zero, then there will be no limitations.
	//
	// Default: 0
	MaxGoBinWorkers int `mapstructure:"max_go_bin_workers"`

	// PathPrefix is the prefix of all request paths. It will be used to
	// trim the request paths via `strings.TrimPrefix`.
	//
	// Note that when the `PathPrefix` is not empty, then it should start
	// with "/".
	//
	// Default: ""
	PathPrefix string `mapstructure:"path_prefix"`

	// Cacher is the `Cacher` that used to cache module files.
	//
	// If the `Cacher` is nil, the module files will be temporarily stored
	// in runtime memory and discarded as the request ends.
	//
	// Default: nil
	Cacher Cacher `mapstructure:"cacher"`

	// SupportedSUMDBHosts is the supported checksum database hosts.
	//
	// Default: ["sum.golang.org"]
	SupportedSUMDBHosts []string `mapstructure:"supported_sumdb_hosts"`

	// ErrorLogger is the `log.Logger` that logs errors that occur while
	// proxing.
	//
	// If the `ErrorLogger` is nil, logging is done via the log package's
	// standard logger.
	//
	// Default: nil
	ErrorLogger *log.Logger `mapstructure:"-"`

	loadOnce            *sync.Once
	goBinWorkerChan     chan struct{}
	supportedSUMDBHosts map[string]bool
	osRemoveAllFailures *sync.Map
}

// New returns a new instance of the `Goproxy` with default field values.
//
// The `New` is the only function that creates new instances of the `Goproxy`
// and keeps everything working.
func New() *Goproxy {
	return &Goproxy{
		GoBinName:           "go",
		SupportedSUMDBHosts: []string{"sum.golang.org"},
		loadOnce:            &sync.Once{},
		supportedSUMDBHosts: map[string]bool{},
		osRemoveAllFailures: &sync.Map{},
	}
}

// load loads the stuff of the g up.
func (g *Goproxy) load() {
	if g.MaxGoBinWorkers != 0 {
		g.goBinWorkerChan = make(chan struct{}, g.MaxGoBinWorkers)
	}

	for _, host := range g.SupportedSUMDBHosts {
		if h, err := idna.Lookup.ToASCII(host); err == nil {
			g.supportedSUMDBHosts[h] = true
		}
	}

	go func() {
		ticker := time.NewTicker(time.Minute)
		gracefulStopChan := make(chan os.Signal, 1)
		signal.Notify(gracefulStopChan, os.Interrupt, syscall.SIGTERM)
		for {
			select {
			case <-ticker.C:
				g.retryOSRemoveAllFailures()
			case <-gracefulStopChan:
				ticker.Stop()
				return
			}
		}
	}()
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

	trimedPath := path.Clean(r.URL.Path)
	trimedPath = strings.TrimPrefix(trimedPath, g.PathPrefix)
	trimedPath = strings.TrimLeft(trimedPath, "/")

	name, err := url.PathUnescape(trimedPath)
	if err != nil {
		setResponseCacheControlHeader(rw, 3600)
		responseNotFound(rw)
		return
	}

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
		isLatest := false
		switch {
		case sumdbPath == "/supported":
			setResponseCacheControlHeader(rw, 60)
			rw.Write(nil) // 200 OK
			return
		case sumdbPath == "/latest":
			isLatest = true
		case strings.HasPrefix(sumdbPath, "/lookup/"),
			strings.HasPrefix(sumdbPath, "/tile/"):
		default:
			setResponseCacheControlHeader(rw, 3600)
			responseNotFound(rw)
			return
		}

		sumdbReq, err := http.NewRequest(
			http.MethodGet,
			fmt.Sprint("https://", sumdbHost, sumdbPath),
			nil,
		)
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

		switch sumdbRes.StatusCode {
		case http.StatusOK:
		case http.StatusBadRequest,
			http.StatusNotFound,
			http.StatusGone:
			setResponseCacheControlHeader(rw, 60)
			responseNotFound(rw)
			return
		default:
			responseBadGateway(rw)
			return
		}

		rw.Header().Set(
			"Content-Type",
			sumdbRes.Header.Get("Content-Type"),
		)
		rw.Header().Set(
			"Content-Length",
			sumdbRes.Header.Get("Content-Length"),
		)

		if isLatest {
			setResponseCacheControlHeader(rw, 60)
		} else {
			setResponseCacheControlHeader(rw, 365*24*3600)
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
	defer func() {
		if err := os.RemoveAll(goproxyRoot); err != nil {
			g.osRemoveAllFailures.Store(goproxyRoot, time.Now())
		}
	}()

	if isLatest || isList || !semver.IsValid(moduleVersion) {
		mlr := &modListResult{}
		if err := mod(
			g.goBinWorkerChan,
			goproxyRoot,
			g.GoBinName,
			modulePath,
			moduleVersion,
			mlr,
		); err != nil {
			if regModuleVersionNotFound.MatchString(err.Error()) {
				setResponseCacheControlHeader(rw, 60)
				responseNotFound(rw)
			} else {
				g.logError(err)
				responseInternalServerError(rw)
			}

			return
		}

		if isList {
			setResponseCacheControlHeader(rw, 60)
			responseString(rw, strings.Join(mlr.Versions, "\n"))
			return
		}

		moduleVersion = mlr.Version
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
		mdr := &modDownloadResult{}
		if err := mod(
			g.goBinWorkerChan,
			goproxyRoot,
			g.GoBinName,
			modulePath,
			moduleVersion,
			mdr,
		); err != nil {
			if regModuleVersionNotFound.MatchString(err.Error()) {
				setResponseCacheControlHeader(rw, 60)
				responseNotFound(rw)
			} else {
				g.logError(err)
				responseInternalServerError(rw)
			}

			return
		}

		namePrefix := strings.TrimSuffix(name, nameExt)

		infoCache, err := newTempCache(
			mdr.Info,
			fmt.Sprint(namePrefix, ".info"),
			cacher.NewHash(),
		)
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}
		defer infoCache.Close()

		if err := cacher.SetCache(r.Context(), infoCache); err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		modCache, err := newTempCache(
			mdr.GoMod,
			fmt.Sprint(namePrefix, ".mod"),
			cacher.NewHash(),
		)
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}
		defer modCache.Close()

		if err := cacher.SetCache(r.Context(), modCache); err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		zipCache, err := newTempCache(
			mdr.Zip,
			fmt.Sprint(namePrefix, ".zip"),
			cacher.NewHash(),
		)
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}
		defer zipCache.Close()

		if err := cacher.SetCache(r.Context(), zipCache); err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		var filename string
		switch nameExt {
		case ".info":
			filename = mdr.Info
		case ".mod":
			filename = mdr.GoMod
		case ".zip":
			filename = mdr.Zip
		}

		// Note that we need to create a new instance of the `tempCache`
		// here instead of reusing the above instances to avoid them
		// being accidentally closed.
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

	contentType := ""
	switch nameExt {
	case ".info":
		contentType = "application/json; charset=utf-8"
	case ".mod":
		contentType = "text/plain; charset=utf-8"
	case ".zip":
		contentType = "application/zip"
	}

	rw.Header().Set("Content-Type", contentType)
	rw.Header().Set(
		"ETag",
		fmt.Sprintf(
			"%q",
			base64.StdEncoding.EncodeToString(cache.Checksum()),
		),
	)

	if isLatest || isList {
		setResponseCacheControlHeader(rw, 60)
	} else {
		setResponseCacheControlHeader(rw, 365*24*3600)
	}

	http.ServeContent(rw, r, "", cache.ModTime(), cache)
}

// retryOSRemoveAllFailures retries the `osRemoveAllFailures`.
func (g *Goproxy) retryOSRemoveAllFailures() {
	g.osRemoveAllFailures.Range(func(key, value interface{}) bool {
		path := key.(string)
		failedAt := value.(time.Time)
		if time.Now().Sub(failedAt) >= time.Minute {
			if err := os.RemoveAll(path); err != nil {
				g.osRemoveAllFailures.Store(path, time.Now())
			} else {
				g.osRemoveAllFailures.Delete(path)
			}
		}

		return true
	})
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
