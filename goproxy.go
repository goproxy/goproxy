/*
Package goproxy implements a minimalist Go module proxy handler.
*/
package goproxy

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/net/idna"
)

// Goproxy is the top-level struct of this project.
//
// Note that the `Goproxy` will not mess with your environment variables, it
// will still follow your GOPROXY, GONOPROXY, GOSUMDB, and GONOSUMDB. This means
// that you can set GOPROXY to serve the `Goproxy` itself under other proxies,
// and by setting GONOPROXY to indicate which modules the `Goproxy` should
// download directly instead of using GOPROXY. And of course, you can also set
// GOSUMDB and GONOSUMDB to indicate how the `Goproxy` should verify the
// modules.
//
// ATTENTION: Since GONOPROXY has not yet been released (it will be introduced
// in Go 1.13), so we implemented a built-in GONOPROXY support for the
// `Goproxy`. Now, you can set GONOPROXY even before Go 1.13.
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
	// Default: 8
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
	errorLogger         *log.Logger
}

// New returns a new instance of the `Goproxy` with default field values.
//
// The `New` is the only function that creates new instances of the `Goproxy`
// and keeps everything working.
func New() *Goproxy {
	g := &Goproxy{
		GoBinName:           "go",
		MaxGoBinWorkers:     8,
		SupportedSUMDBHosts: []string{"sum.golang.org"},
		loadOnce:            &sync.Once{},
		supportedSUMDBHosts: map[string]bool{},
	}

	g.errorLogger = log.New(&errorLogWriter{g: g}, "", 0)

	return g
}

// ServeHTTP implements `http.Handler`.
func (g *Goproxy) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	g.loadOnce.Do(func() {
		g.goBinWorkerChan = make(chan struct{}, g.MaxGoBinWorkers)

		for _, host := range g.SupportedSUMDBHosts {
			if h, err := idna.Lookup.ToASCII(host); err == nil {
				g.supportedSUMDBHosts[h] = true
			}
		}
	})

	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		responseMethodNotAllowed(rw)
		return
	}

	if !strings.HasPrefix(r.URL.Path, "/") {
		responseNotFound(rw)
		return
	}

	trimedPath := path.Clean(r.URL.Path)
	trimedPath = strings.TrimPrefix(trimedPath, g.PathPrefix)
	trimedPath = strings.TrimLeft(trimedPath, "/")

	name, err := url.PathUnescape(trimedPath)
	if err != nil {
		responseNotFound(rw)
		return
	}

	if strings.HasPrefix(name, "sumdb/") {
		sumdbURL := strings.TrimPrefix(name, "sumdb/")
		sumdbPathOffset := strings.Index(sumdbURL, "/")
		if sumdbPathOffset < 0 {
			responseNotFound(rw)
			return
		}

		sumdbHost := sumdbURL[:sumdbPathOffset]
		sumdbHost, err := idna.Lookup.ToASCII(sumdbHost)
		if err != nil {
			responseNotFound(rw)
			return
		}

		if !g.supportedSUMDBHosts[sumdbHost] {
			responseNotFound(rw)
			return
		}

		sumdbPath := sumdbURL[sumdbPathOffset:]
		switch {
		case sumdbPath == "/supported":
			rw.Write(nil) // 200 OK
			return
		case sumdbPath == "/latest",
			strings.HasPrefix(sumdbPath, "/lookup/"),
			strings.HasPrefix(sumdbPath, "/tile/"):
		default:
			responseNotFound(rw)
			return
		}

		sumdbReq, err := http.NewRequest(
			http.MethodGet,
			fmt.Sprint("https://", sumdbHost, sumdbPath),
			nil,
		)
		if err != nil {
			g.logInternalServerError(err)
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
				g.logInternalServerError(err)
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

		setResponseCacheControlHeader(rw, false)

		io.Copy(rw, sumdbRes.Body)

		return
	}

	isList := false
	switch {
	case strings.HasSuffix(name, "/@latest"):
		name = fmt.Sprint(
			strings.TrimSuffix(name, "latest"),
			"v/latest.info",
		)
	case strings.HasSuffix(name, "/@v/list"):
		name = fmt.Sprint(
			strings.TrimSuffix(name, "list"),
			"latest.info",
		)
		isList = true
	}

	nameParts := strings.Split(name, "/@v/")
	if len(nameParts) != 2 {
		responseNotFound(rw)
		return
	}

	escapedModulePath := nameParts[0]
	nameBase := nameParts[1]
	nameExt := path.Ext(nameBase)
	switch nameExt {
	case ".info", ".mod", ".zip":
	default:
		responseNotFound(rw)
		return
	}

	escapedModuleVersion := strings.TrimSuffix(nameBase, nameExt)
	moduleVersion, err := module.UnescapeVersion(escapedModuleVersion)
	if err != nil {
		responseNotFound(rw)
		return
	}

	isModuleVersionValid := !isList && semver.IsValid(moduleVersion)
	if !isModuleVersionValid {
		mlr, err := modList(
			g.goBinWorkerChan,
			g.GoBinName,
			escapedModulePath,
			escapedModuleVersion,
			isList,
		)
		if err != nil {
			if err == errModuleVersionNotFound {
				responseNotFound(rw)
			} else {
				g.logInternalServerError(err)
				responseInternalServerError(rw)
			}

			return
		}

		if isList {
			setResponseCacheControlHeader(rw, false)
			responseString(rw, strings.Join(mlr.Versions, "\n"))
			return
		}

		moduleVersion = mlr.Version
		escapedModuleVersion, err = module.EscapeVersion(moduleVersion)
		if err != nil {
			g.logInternalServerError(err)
			responseInternalServerError(rw)
			return
		}

		nameBase = fmt.Sprint(escapedModuleVersion, nameExt)
		name = path.Join(path.Dir(name), nameBase)
	}

	cacher := g.Cacher
	if cacher == nil {
		cacher = &tempCacher{
			caches: make(map[string]*tempCache, 3),
		}
	}

	cache, err := cacher.Cache(r.Context(), name)
	if err == ErrCacheNotFound {
		if _, err := modDownload(
			g.goBinWorkerChan,
			g.GoBinName,
			cacher,
			escapedModulePath,
			escapedModuleVersion,
		); err != nil {
			if err == errModuleVersionNotFound {
				responseNotFound(rw)
			} else {
				g.logInternalServerError(err)
				responseInternalServerError(rw)
			}

			return
		}

		cache, err = cacher.Cache(r.Context(), name)
		if err != nil {
			g.logInternalServerError(err)
			responseInternalServerError(rw)
			return
		}
	} else if err != nil {
		g.logInternalServerError(err)
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

	setResponseCacheControlHeader(rw, isModuleVersionValid)

	http.ServeContent(rw, r, "", cache.ModTime(), cache)
}

// logInternalServerError logs the err as an internal server error.
func (g *Goproxy) logInternalServerError(err error) {
	g.errorLogger.Printf("goproxy: internal server error: %v", err)
}

// errorLogWriter is an error log writer
type errorLogWriter struct {
	g *Goproxy
}

// Write implements the `io.Writer`.
func (elw *errorLogWriter) Write(b []byte) (int, error) {
	s := *(*string)(unsafe.Pointer(&b))
	if !strings.HasPrefix(s, "goproxy: ") {
		s = fmt.Sprint("goproxy: ", s)
	}

	if elw.g.ErrorLogger != nil {
		return len(s), elw.g.ErrorLogger.Output(2, s)
	}

	return len(s), log.Output(2, s)
}
