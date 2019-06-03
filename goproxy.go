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
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/net/idna"
)

// Goproxy is the top-level struct of this project.
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
	// Default: `&LocalCacher{}`
	Cacher Cacher `mapstructure:"-"`

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
}

// New returns a new instance of the `Goproxy` with default field values.
//
// The `New` is the only function that creates new instances of the `Goproxy`
// and keeps everything working.
func New() *Goproxy {
	return &Goproxy{
		GoBinName:           "go",
		MaxGoBinWorkers:     8,
		Cacher:              &LocalCacher{},
		SupportedSUMDBHosts: []string{"sum.golang.org"},
		loadOnce:            &sync.Once{},
		supportedSUMDBHosts: map[string]bool{},
	}
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

	trimedPath := strings.TrimPrefix(r.URL.Path, g.PathPrefix)
	trimedPath = strings.TrimLeft(trimedPath, "/")

	filename, err := url.PathUnescape(trimedPath)
	if err != nil {
		responseNotFound(rw)
		return
	}

	if strings.HasPrefix(filename, "sumdb/") {
		sumdbURL := strings.TrimPrefix(filename, "sumdb/")
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
		if sumdbPath == "/supported" {
			rw.Write(nil) // 200 OK
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

		sumdbReq = sumdbReq.WithContext(r.Context())

		sumdbRes, err := http.DefaultClient.Do(sumdbReq)
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
		case http.StatusNotFound, http.StatusGone:
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

	filenameParts := strings.Split(filename, "/@")
	if len(filenameParts) != 2 {
		responseNotFound(rw)
		return
	}

	escapedModulePath := filenameParts[0]
	switch filenameParts[1] {
	case "latest", "v/list":
		mlr, err := g.modList(
			r.Context(),
			escapedModulePath,
			"latest",
			filenameParts[1] == "v/list",
		)
		if err != nil {
			if err == errModuleVersionNotFound {
				responseNotFound(rw)
			} else {
				g.logError(err)
				responseInternalServerError(rw)
			}

			return
		}

		setResponseCacheControlHeader(rw, false)
		switch filenameParts[1] {
		case "latest":
			responseJSON(rw, mlr)
		case "v/list":
			responseString(rw, strings.Join(mlr.Versions, "\n"))
		}

		return
	}

	filenameBase := path.Base(filenameParts[1])
	filenameExt := path.Ext(filenameBase)
	switch filenameExt {
	case ".info", ".mod", ".zip":
	default:
		responseNotFound(rw)
		return
	}

	escapedModuleVersion := strings.TrimSuffix(filenameBase, filenameExt)
	moduleVersion, err := module.UnescapeVersion(escapedModuleVersion)
	if err != nil {
		responseNotFound(rw)
		return
	}

	isModuleVersionValid := semver.IsValid(moduleVersion)
	if !isModuleVersionValid {
		mlr, err := g.modList(
			r.Context(),
			escapedModulePath,
			escapedModuleVersion,
			false,
		)
		if err != nil {
			if err == errModuleVersionNotFound {
				responseNotFound(rw)
			} else {
				g.logError(err)
				responseInternalServerError(rw)
			}

			return
		}

		moduleVersion = mlr.Version
		escapedModuleVersion, err = module.EscapeVersion(moduleVersion)
		if err != nil {
			g.logError(err)
			responseInternalServerError(rw)
			return
		}

		filenameBase = fmt.Sprint(escapedModuleVersion, filenameExt)
		filename = path.Join(path.Dir(filename), filenameBase)
	}

	cache, err := g.Cacher.Get(r.Context(), filename)
	if err == ErrCacheNotFound {
		if _, err := g.modDownload(
			r.Context(),
			escapedModulePath,
			escapedModuleVersion,
		); err != nil {
			if err == errModuleVersionNotFound {
				responseNotFound(rw)
			} else {
				g.logError(err)
				responseInternalServerError(rw)
			}

			return
		}

		cache, err = g.Cacher.Get(r.Context(), filename)
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
	switch filenameExt {
	case ".info":
		contentType = "application/json; charset=utf-8"
	case ".mod":
		contentType = "text/plain; charset=utf-8"
	case ".zip":
		contentType = "application/zip"
	}

	rw.Header().Set("Content-Type", contentType)

	eTagHash := xxhash.New()
	if _, err := io.Copy(eTagHash, cache); err != nil {
		g.logError(err)
		responseInternalServerError(rw)
		return
	}

	if _, err := cache.Seek(0, io.SeekStart); err != nil {
		g.logError(err)
		responseInternalServerError(rw)
		return
	}

	rw.Header().Set(
		"ETag",
		fmt.Sprintf(
			"%q",
			base64.StdEncoding.EncodeToString(eTagHash.Sum(nil)),
		),
	)

	setResponseCacheControlHeader(rw, isModuleVersionValid)

	http.ServeContent(rw, r, "", cache.ModTime(), cache)
}

// logError logs the err.
func (g *Goproxy) logError(err error) {
	if err == nil {
		return
	}

	if g.ErrorLogger != nil {
		g.ErrorLogger.Output(2, err.Error())
		return
	}

	log.Output(2, err.Error())
}

var (
	modOutputNotFoundKeywords = [][]byte{
		[]byte("could not read username"),
		[]byte("invalid"),
		[]byte("malformed"),
		[]byte("no matching"),
		[]byte("not found"),
		[]byte("unknown"),
		[]byte("unrecognized"),
	}

	errModuleVersionNotFound = errors.New("module version not found")
)

// modListResult is the result of
// `go list -json -m -versions <MODULE_PATH>@<MODULE_VERSION>`.
type modListResult struct {
	Version  string   `json:"Version"`
	Time     string   `json:"Time"`
	Versions []string `json:"Versions,omitempty"`
}

// modList executes
// `go list -json -m -versions escapedModulePath@escapedModuleVersion`.
func (g *Goproxy) modList(
	ctx context.Context,
	escapedModulePath string,
	escapedModuleVersion string,
	allVersions bool,
) (*modListResult, error) {
	modulePath, err := module.UnescapePath(escapedModulePath)
	if err != nil {
		return nil, errModuleVersionNotFound
	}

	moduleVersion, err := module.UnescapeVersion(escapedModuleVersion)
	if err != nil {
		return nil, errModuleVersionNotFound
	}

	goproxyRoot, err := ioutil.TempDir("", "goproxy")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(goproxyRoot)

	args := []string{"list", "-json", "-m"}
	if allVersions {
		args = append(args, "-versions")
	}

	args = append(args, fmt.Sprint(modulePath, "@", moduleVersion))

	stdout, err := g.executeGoCommand(ctx, goproxyRoot, args...)
	if err != nil {
		return nil, err
	}

	mlr := &modListResult{}
	if err := json.Unmarshal(stdout, mlr); err != nil {
		return nil, err
	}

	return mlr, nil
}

// modDownloadResult is the result of
// `go mod download -json <MODULE_PATH>@<MODULE_VERSION>`.
type modDownloadResult struct {
	Info  string `json:"Info"`
	GoMod string `json:"GoMod"`
	Zip   string `json:"Zip"`
}

// modDownload executes
// `go mod download -json escapedModulePath@escapedModuleVersion`.
func (g *Goproxy) modDownload(
	ctx context.Context,
	escapedModulePath string,
	escapedModuleVersion string,
) (*modDownloadResult, error) {
	modulePath, err := module.UnescapePath(escapedModulePath)
	if err != nil {
		return nil, errModuleVersionNotFound
	}

	moduleVersion, err := module.UnescapeVersion(escapedModuleVersion)
	if err != nil {
		return nil, errModuleVersionNotFound
	}

	goproxyRoot, err := ioutil.TempDir("", "goproxy")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(goproxyRoot)

	stdout, err := g.executeGoCommand(
		ctx,
		goproxyRoot,
		"mod",
		"download",
		"-json",
		fmt.Sprint(modulePath, "@", moduleVersion),
	)
	if err != nil {
		return nil, err
	}

	mdr := &modDownloadResult{}
	if err := json.Unmarshal(stdout, mdr); err != nil {
		return nil, err
	}

	filenamePrefix := path.Join(
		escapedModulePath,
		"@v",
		escapedModuleVersion,
	)

	infoFile, err := os.Open(mdr.Info)
	if err != nil {
		return nil, err
	}
	defer infoFile.Close()

	if err := g.Cacher.Set(
		ctx,
		fmt.Sprint(filenamePrefix, ".info"),
		infoFile,
	); err != nil {
		return nil, err
	}

	modFile, err := os.Open(mdr.GoMod)
	if err != nil {
		return nil, err
	}
	defer modFile.Close()

	if err := g.Cacher.Set(
		ctx,
		fmt.Sprint(filenamePrefix, ".mod"),
		modFile,
	); err != nil {
		return nil, err
	}

	zipFile, err := os.Open(mdr.Zip)
	if err != nil {
		return nil, err
	}
	defer zipFile.Close()

	if err := g.Cacher.Set(
		ctx,
		fmt.Sprint(filenamePrefix, ".zip"),
		zipFile,
	); err != nil {
		return nil, err
	}

	return mdr, nil
}

// executeGoCommand executes go command with the args.
func (g *Goproxy) executeGoCommand(
	ctx context.Context,
	goproxyRoot string,
	args ...string,
) ([]byte, error) {
	g.goBinWorkerChan <- struct{}{}
	defer func() {
		<-g.goBinWorkerChan
	}()

	cmd := exec.CommandContext(ctx, g.GoBinName, args...)
	cmd.Env = append(
		os.Environ(),
		"GO111MODULE=on",
		fmt.Sprint("GOCACHE=", filepath.Join(goproxyRoot, "gocache")),
		fmt.Sprint("GOPATH=", filepath.Join(goproxyRoot, "gopath")),
	)
	cmd.Dir = goproxyRoot
	stdout, err := cmd.Output()
	if err != nil {
		output := stdout
		if ee, ok := err.(*exec.ExitError); ok {
			output = append(output, ee.Stderr...)
		}

		lowercasedOutput := bytes.ToLower(output)
		for _, k := range modOutputNotFoundKeywords {
			if bytes.Contains(lowercasedOutput, k) {
				return nil, errModuleVersionNotFound
			}
		}

		return nil, fmt.Errorf("modList: %v: %s", err, output)
	}

	return stdout, nil
}

// setResponseCacheControlHeader sets the Cache-Control header based on the
// cacheable.
func setResponseCacheControlHeader(rw http.ResponseWriter, cacheable bool) {
	cacheControl := ""
	if cacheable {
		cacheControl = "max-age=31536000"
	} else {
		cacheControl = "must-revalidate, no-cache, no-store"
	}

	rw.Header().Set("Cache-Control", cacheControl)
}

// responseJSON responses the JSON marshaled from the v to the client.
func responseJSON(rw http.ResponseWriter, v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		responseInternalServerError(rw)
		return
	}

	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.Write(b)
}

// responseString responses the s to the client.
func responseString(rw http.ResponseWriter, s string) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.Write([]byte(s))
}

// responseNotFound responses "404 Not Found" to the client.
func responseNotFound(rw http.ResponseWriter) {
	http.Error(rw, "404 Not Found", http.StatusNotFound)
}

// responseMethodNotAllowed responses "405 Method Not Allowed" to the client.
func responseMethodNotAllowed(rw http.ResponseWriter) {
	http.Error(rw, "405 Method Not Allowed", http.StatusMethodNotAllowed)
}

// responseGone responses "410 Gone" to the client.
func responseGone(rw http.ResponseWriter) {
	http.Error(rw, "410 Gone", http.StatusGone)
}

// responseInternalServerError responses "500 Internal Server Error" to the
// client.
func responseInternalServerError(rw http.ResponseWriter) {
	http.Error(
		rw,
		"500 Internal Server Error",
		http.StatusInternalServerError,
	)
}

// responseBadGateway responses "502 Status Bad Gateway" to the client.
func responseBadGateway(rw http.ResponseWriter) {
	http.Error(rw, "502 Status Bad Gateway", http.StatusBadGateway)
}
