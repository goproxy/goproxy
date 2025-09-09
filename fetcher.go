package goproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/zip"
)

// Fetcher defines a set of intuitive methods used to fetch module files for
// [Goproxy].
//
// Note that any error returned by Fetcher that matches [fs.ErrNotExist]
// indicates that the module cannot be fetched.
type Fetcher interface {
	// Query performs the version query for the given module path.
	//
	// The version query can be one of the following:
	//   - A fully-specified semantic version, such as "v1.2.3", which
	//     selects a specific version.
	//   - A semantic version prefix, such as "v1" or "v1.2", which selects
	//     the highest available version with that prefix.
	//   - A revision identifier for the underlying source repository, such
	//     as a commit hash prefix, revision tag, or branch name. If the
	//     revision is tagged with a semantic version, this query selects
	//     that version. Otherwise, this query selects a pseudo-version for
	//     the underlying commit. Note that branches and tags with names
	//     matched by other version queries cannot be selected this way. For
	//     example, the query "v2" selects the latest version starting with
	//     "v2", not the branch named "v2".
	//   - The string "latest", which selects the highest available release
	//     version. If there are no release versions, "latest" selects the
	//     highest pre-release version. If there are no tagged versions,
	//     "latest" selects a pseudo-version for the commit at the tip of
	//     the repository's default branch.
	Query(ctx context.Context, path, query string) (version string, time time.Time, err error)

	// List lists the available versions for the given module path.
	//
	// The returned versions contains only tagged versions, not
	// pseudo-versions. Versions covered by "retract" directives in the
	// "go.mod" file from the "latest" version of the same module are also
	// ignored.
	List(ctx context.Context, path string) (versions []string, err error)

	// Download downloads the module files for the given module path and
	// version.
	//
	// The returned error is nil only if all three kinds of module files
	// are successfully downloaded.
	Download(ctx context.Context, path, version string) (info, mod, zip io.ReadSeekCloser, err error)
}

// GoFetcher implements [Fetcher] using the local Go binary.
//
// Make sure that the Go binary and the version control systems (such as Git)
// that need to be supported are installed and properly configured in your
// environment, as they are required for direct module fetching.
//
// During a direct module fetch, the Go binary is called while holding a lock
// file in the module cache directory (specified by GOMODCACHE) to prevent
// potential conflicts. Misuse of a shared GOMODCACHE may lead to deadlocks.
//
// Note that GoFetcher will still adhere to your environment variables. This
// means you can set GOPROXY to run GoFetcher itself under other proxies. By
// setting GONOPROXY and GOPRIVATE, you can instruct GoFetcher on which modules
// to fetch directly, rather than using those proxies. Additionally, you can set
// GOSUMDB, GONOSUMDB, and GOPRIVATE to specify how GoFetcher should verify the
// modules it has just fetched. Importantly, all of these mentioned environment
// variables are built-in supported, resulting in fewer external command calls
// and a significant performance boost.
type GoFetcher struct {
	// Env is the environment. Each entry is in the form "key=value".
	//
	// If Env is nil, [os.Environ] is used.
	//
	// If Env contains duplicate environment keys, only the last value in
	// the slice for each duplicate key is used.
	//
	// Make sure that all environment values are valid, particularly for
	// GOPROXY and GOSUMDB, to prevent constant fetch failures.
	Env []string

	// GoBin is the path to the Go binary that is used to execute direct
	// fetches.
	//
	// If GoBin is empty, "go" is used.
	GoBin string

	// MaxDirectFetches is the maximum number of concurrent direct fetches.
	//
	// If MaxDirectFetches is zero, there is no limit.
	MaxDirectFetches int

	// TempDir is the directory for storing temporary files.
	//
	// If TempDir is empty, [os.TempDir] is used.
	TempDir string

	// Transport is used to execute outgoing requests, excluding those
	// initiated by direct fetches.
	//
	// If Transport is nil, [http.DefaultTransport] is used.
	Transport http.RoundTripper

	initOnce              sync.Once
	initErr               error
	env                   []string
	envGOPROXY            string
	envGONOPROXY          string
	directFetchWorkerPool chan struct{}
	httpClient            *http.Client
	sumdbClient           *sumdb.Client
}

// init initializes the f.
func (gf *GoFetcher) init() {
	env := gf.Env
	if env == nil {
		env = os.Environ()
	}
	var envGOSUMDB, envGONOSUMDB, envGOPRIVATE string
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			switch k {
			case "GO111MODULE":
			case "GOPROXY":
				gf.envGOPROXY = v
			case "GONOPROXY":
				gf.envGONOPROXY = v
			case "GOSUMDB":
				envGOSUMDB = v
			case "GONOSUMDB":
				envGONOSUMDB = v
			case "GOPRIVATE":
				envGOPRIVATE = v
			default:
				gf.env = append(gf.env, e)
			}
		}
	}
	gf.envGOPROXY, gf.initErr = cleanEnvGOPROXY(gf.envGOPROXY)
	if gf.initErr != nil {
		return
	}
	if gf.envGONOPROXY == "" {
		gf.envGONOPROXY = envGOPRIVATE
	}
	gf.envGONOPROXY = cleanCommaSeparatedList(gf.envGONOPROXY)
	envGOSUMDB = cleanEnvGOSUMDB(envGOSUMDB)
	if envGONOSUMDB == "" {
		envGONOSUMDB = envGOPRIVATE
	}
	envGONOSUMDB = cleanCommaSeparatedList(envGONOSUMDB)
	gf.env = append(
		gf.env,
		"GO111MODULE=on",
		"GOPROXY=direct",
		"GONOPROXY=",
		"GOSUMDB=off",
		"GONOSUMDB=",
		"GOPRIVATE=",
	)

	if gf.MaxDirectFetches > 0 {
		gf.directFetchWorkerPool = make(chan struct{}, gf.MaxDirectFetches)
	}

	gf.httpClient = &http.Client{Transport: gf.Transport}
	if envGOSUMDB != "off" {
		sco, err := newSumdbClientOps(gf.envGOPROXY, envGOSUMDB, gf.httpClient)
		if err != nil {
			gf.initErr = err
			return
		}
		gf.sumdbClient = sumdb.NewClient(sco)
		gf.sumdbClient.SetGONOSUMDB(envGONOSUMDB)
	}
}

// skipProxy reports whether the module path should be fetched directly rather
// than using a proxy.
func (gf *GoFetcher) skipProxy(path string) bool {
	return module.MatchPrefixPatterns(gf.envGONOPROXY, path)
}

// Query implements [Fetcher].
func (gf *GoFetcher) Query(ctx context.Context, path, query string) (version string, time time.Time, err error) {
	if gf.initOnce.Do(gf.init); gf.initErr != nil {
		err = gf.initErr
		return
	}
	if gf.skipProxy(path) {
		version, time, err = gf.directQuery(ctx, path, query)
	} else {
		err = walkEnvGOPROXY(gf.envGOPROXY, func(proxy *url.URL) error {
			version, time, err = gf.proxyQuery(ctx, path, query, proxy)
			return err
		}, func() error {
			version, time, err = gf.directQuery(ctx, path, query)
			return err
		})
	}
	return
}

// proxyQuery performs the version query for the given module path using the
// given proxy.
func (gf *GoFetcher) proxyQuery(ctx context.Context, path, query string, proxy *url.URL) (version string, time time.Time, err error) {
	escapedPath, err := module.EscapePath(path)
	if err != nil {
		return
	}
	escapedQuery, err := module.EscapeVersion(query)
	if err != nil {
		return
	}
	var u *url.URL
	if escapedQuery == "latest" {
		u = proxy.JoinPath(escapedPath + "/@latest")
	} else {
		u = proxy.JoinPath(escapedPath + "/@v/" + escapedQuery + ".info")
	}
	var info bytes.Buffer
	err = httpGet(ctx, gf.httpClient, u.String(), &info)
	if err != nil {
		return
	}
	version, time, err = unmarshalInfo(info.String())
	if err != nil {
		err = notExistErrorf("invalid info response: %w", err)
		return
	}
	return
}

// directQuery performs the version query for the given module path using the
// local Go binary.
func (gf *GoFetcher) directQuery(ctx context.Context, path, query string) (version string, t time.Time, err error) {
	output, err := gf.execGo(ctx, "list", "-json", "-m", path+"@"+query)
	if err != nil {
		return
	}
	var info struct {
		Version string
		Time    time.Time
	}
	return info.Version, info.Time, json.Unmarshal(output, &info)
}

// List implements [Fetcher].
func (gf *GoFetcher) List(ctx context.Context, path string) (versions []string, err error) {
	if gf.initOnce.Do(gf.init); gf.initErr != nil {
		err = gf.initErr
		return
	}

	if gf.skipProxy(path) {
		versions, err = gf.directList(ctx, path)
	} else {
		err = walkEnvGOPROXY(gf.envGOPROXY, func(proxy *url.URL) error {
			versions, err = gf.proxyList(ctx, path, proxy)
			return err
		}, func() error {
			versions, err = gf.directList(ctx, path)
			return err
		})
	}
	if err != nil {
		return
	}

	for i, version := range versions {
		parts := strings.Fields(version)
		if len(parts) > 0 && semver.IsValid(parts[0]) && !module.IsPseudoVersion(parts[0]) {
			versions[i] = parts[0]
		} else {
			versions[i] = ""
		}
	}
	versions = slices.DeleteFunc(versions, func(version string) bool {
		return version == ""
	})
	semver.Sort(versions)
	return
}

// proxyList lists the available versions for the given module path using the
// given proxy.
func (gf *GoFetcher) proxyList(ctx context.Context, path string, proxy *url.URL) (versions []string, err error) {
	escapedPath, err := module.EscapePath(path)
	if err != nil {
		return
	}
	var list bytes.Buffer
	err = httpGet(ctx, gf.httpClient, proxy.JoinPath(escapedPath+"/@v/list").String(), &list)
	if err != nil {
		return
	}
	versions = strings.Split(list.String(), "\n")
	return
}

// directList lists the available versions for the given module path using the
// local Go binary.
func (gf *GoFetcher) directList(ctx context.Context, path string) (versions []string, err error) {
	output, err := gf.execGo(ctx, "list", "-json", "-m", "-versions", path+"@latest")
	if err != nil {
		return
	}
	var list struct{ Versions []string }
	return list.Versions, json.Unmarshal(output, &list)
}

// Download implements [Fetcher].
func (gf *GoFetcher) Download(ctx context.Context, path, version string) (info, mod, zip io.ReadSeekCloser, err error) {
	if gf.initOnce.Do(gf.init); gf.initErr != nil {
		err = gf.initErr
		return
	}

	if err = checkCanonicalVersion(path, version); err != nil {
		return
	}

	var (
		infoFile, modFile, zipFile string

		// cleanup is the cleanup function that will be called when the
		// infoFile, modFile, and zipFile are no longer needed, or when
		// an error occurs.
		cleanup func()
	)
	if gf.skipProxy(path) {
		infoFile, modFile, zipFile, err = gf.directDownload(ctx, path, version)
	} else {
		err = walkEnvGOPROXY(gf.envGOPROXY, func(proxy *url.URL) error {
			infoFile, modFile, zipFile, cleanup, err = gf.proxyDownload(ctx, path, version, proxy)
			return err
		}, func() error {
			infoFile, modFile, zipFile, err = gf.directDownload(ctx, path, version)
			return err
		})
	}
	if err != nil {
		return
	}
	if cleanup != nil {
		defer func() {
			if err != nil {
				cleanup()
			}
		}()
	} else {
		cleanup = func() {} // Avoid nil cleanup.
	}

	infoVersion, infoTime, err := unmarshalInfoFile(infoFile)
	if err != nil {
		return
	}
	err = checkModFile(modFile)
	if err != nil {
		return
	}
	err = checkZipFile(zipFile, path, version)
	if err != nil {
		return
	}
	if gf.sumdbClient != nil {
		err = verifyModFile(gf.sumdbClient, modFile, path, version)
		if err != nil {
			return
		}
		err = verifyZipFile(gf.sumdbClient, zipFile, path, version)
		if err != nil {
			return
		}
	}

	infoContent := strings.NewReader(marshalInfo(infoVersion, infoTime))
	modContent, err := os.Open(modFile)
	if err != nil {
		return
	}
	zipContent, err := os.Open(zipFile)
	if err != nil {
		modContent.Close()
		return
	}

	var (
		closers int32 = 3
		closed        = func() {
			if atomic.AddInt32(&closers, -1) == 0 {
				cleanup()
			}
		}
	)
	infoClosedOnce := sync.OnceFunc(closed)
	info = struct {
		io.ReadSeeker
		io.Closer
	}{infoContent, closerFunc(func() error {
		infoClosedOnce()
		return nil
	})}
	modClosedOnce := sync.OnceFunc(closed)
	mod = struct {
		io.ReadSeeker
		io.Closer
	}{modContent, closerFunc(func() error {
		defer modClosedOnce()
		return modContent.Close()
	})}
	zipClosedOnce := sync.OnceFunc(closed)
	zip = struct {
		io.ReadSeeker
		io.Closer
	}{zipContent, closerFunc(func() error {
		defer zipClosedOnce()
		return zipContent.Close()
	})}
	return
}

// proxyDownload downloads the module files for the given module path and
// version using the given proxy.
func (gf *GoFetcher) proxyDownload(ctx context.Context, path, version string, proxy *url.URL) (infoFile, modFile, zipFile string, cleanup func(), err error) {
	escapedPath, err := module.EscapePath(path)
	if err != nil {
		return
	}
	escapedVersion, err := module.EscapeVersion(version)
	if err != nil {
		return
	}
	urlWithoutExt := proxy.JoinPath(escapedPath + "/@v/" + escapedVersion).String()

	tempDir, err := os.MkdirTemp(gf.TempDir, tempDirPattern)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tempDir)
		}
	}()

	infoFile, err = httpGetTemp(ctx, gf.httpClient, urlWithoutExt+".info", tempDir)
	if err != nil {
		return
	}
	modFile, err = httpGetTemp(ctx, gf.httpClient, urlWithoutExt+".mod", tempDir)
	if err != nil {
		return
	}
	zipFile, err = httpGetTemp(ctx, gf.httpClient, urlWithoutExt+".zip", tempDir)
	if err != nil {
		return
	}
	cleanup = func() { os.RemoveAll(tempDir) }
	return
}

// directDownload downloads the module files for the given module path and
// version using the local Go binary.
func (gf *GoFetcher) directDownload(ctx context.Context, path, version string) (infoFile, modFile, zipFile string, err error) {
	output, err := gf.execGo(ctx, "mod", "download", "-json", path+"@"+version)
	if err != nil {
		return
	}
	var download struct{ Info, GoMod, Zip string }
	return download.Info, download.GoMod, download.Zip, json.Unmarshal(output, &download)
}

// execGo executes the local Go binary with the given args and returns the output.
func (gf *GoFetcher) execGo(ctx context.Context, args ...string) ([]byte, error) {
	if gf.directFetchWorkerPool != nil {
		gf.directFetchWorkerPool <- struct{}{}
		defer func() { <-gf.directFetchWorkerPool }()
	}

	tempDir, err := os.MkdirTemp(gf.TempDir, tempDirPattern)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	goBin := gf.GoBin
	if goBin == "" {
		goBin = "go"
	}
	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Env = gf.env
	cmd.Dir = tempDir
	output, err := cmd.Output()
	if err != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(output) > 0 {
			var goErr struct{ Error string }
			if err := json.Unmarshal(output, &goErr); err == nil && goErr.Error != "" {
				output = []byte(goErr.Error)
			}
		} else if ee, ok := err.(*exec.ExitError); ok {
			output = ee.Stderr
		}
		if len(output) == 0 {
			return nil, err
		}
		var msg string
		for line := range strings.Lines(string(output)) {
			if !strings.HasPrefix(line, "go: finding") {
				msg += line
			}
		}
		msg = strings.TrimPrefix(msg, "go: ")
		msg = strings.TrimPrefix(msg, "go list -m: ")
		msg = strings.TrimRight(msg, "\n")
		return nil, notExistErrorf("%s", msg)
	}
	return output, nil
}

const defaultEnvGOPROXY = "https://proxy.golang.org,direct"

// cleanEnvGOPROXY returns the cleaned envGOPROXY.
func cleanEnvGOPROXY(envGOPROXY string) (string, error) {
	if envGOPROXY == "" || envGOPROXY == defaultEnvGOPROXY {
		return defaultEnvGOPROXY, nil
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
		default:
			if _, err := url.Parse(proxy); err != nil {
				return "", fmt.Errorf("invalid GOPROXY URL: %w", err)
			}
		}
		cleaned += proxy + sep
	}
	if cleaned == "" {
		return "", errors.New("GOPROXY list is not the empty string, but contains no entries")
	}
	return cleaned, nil
}

// walkEnvGOPROXY walks through the proxy list parsed from the envGOPROXY.
func walkEnvGOPROXY(envGOPROXY string, onProxy func(proxy *url.URL) error, onDirect func() error) error {
	if envGOPROXY == "" {
		return errors.New("missing GOPROXY")
	}
	var lastErr error
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
			return notExistErrorf("module lookup disabled by GOPROXY=off")
		}
		u, err := url.Parse(proxy)
		if err != nil {
			return err
		}
		if err := onProxy(u); err != nil {
			if fallBackOnError || errors.Is(err, fs.ErrNotExist) {
				lastErr = err
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
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
	ss := strings.Split(list, ",")
	for i, s := range ss {
		ss[i] = strings.TrimSpace(s)
	}
	return strings.Join(slices.DeleteFunc(ss, func(s string) bool {
		return s == ""
	}), ",")
}

// checkCanonicalVersion is like [module.Check] but also checks whether the
// version is canonical.
func checkCanonicalVersion(path, version string) error {
	if err := module.Check(path, version); err != nil {
		return err
	}
	if version != module.CanonicalVersion(version) {
		return &module.ModuleError{
			Path: path,
			Err:  &module.InvalidVersionError{Version: version, Err: errors.New("not a canonical version")},
		}
	}
	return nil
}

// marshalInfo marshals the version and t as info.
func marshalInfo(version string, t time.Time) string {
	return fmt.Sprintf(`{"Version":%q,"Time":%q}`, version, t.UTC().Format(time.RFC3339Nano))
}

// unmarshalInfo unmarshals the s as info and returns version and time.
func unmarshalInfo(s string) (string, time.Time, error) {
	var info struct {
		Version string
		Time    time.Time
	}
	if err := json.Unmarshal([]byte(s), &info); err != nil {
		return "", time.Time{}, err
	}
	if !semver.IsValid(info.Version) {
		return "", time.Time{}, errors.New("invalid version")
	}
	return info.Version, info.Time.UTC(), nil
}

// unmarshalInfoFile is like [unmarshalInfo] but reads the info from the file
// targeted by the name.
func unmarshalInfoFile(name string) (string, time.Time, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		return "", time.Time{}, err
	}
	version, t, err := unmarshalInfo(string(b))
	if err != nil {
		return "", time.Time{}, notExistErrorf("invalid info file: %w", err)
	}
	return version, t, nil
}

// checkModFile checks the mod file targeted by the name.
func checkModFile(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.HasPrefix(strings.TrimSpace(scanner.Text()), "module") {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return notExistErrorf("invalid mod file: missing module directive")
}

// verifyModFile uses the sumdbClient to verify the mod file targeted by the
// name with the modulePath and moduleVersion.
func verifyModFile(sumdbClient *sumdb.Client, name, modulePath, moduleVersion string) error {
	sumLines, err := sumdbClient.Lookup(modulePath, moduleVersion+"/go.mod")
	if err != nil {
		if errors.Is(err, sumdb.ErrGONOSUMDB) {
			return nil
		}
		return err
	}
	modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) { return os.Open(name) })
	if err != nil {
		return err
	}
	modSumLine := fmt.Sprintf("%s %s/go.mod %s", modulePath, moduleVersion, modHash)
	if !slices.Contains(sumLines, modSumLine) {
		return notExistErrorf("%s@%s: invalid version: untrusted revision %s", modulePath, moduleVersion, moduleVersion)
	}
	return nil
}

// checkZipFile checks the zip file targeted by the name with the modulePath and
// moduleVersion.
func checkZipFile(name, modulePath, moduleVersion string) error {
	if _, err := zip.CheckZip(module.Version{Path: modulePath, Version: moduleVersion}, name); err != nil {
		return notExistErrorf("invalid zip file: %w", err)
	}
	return nil
}

// verifyZipFile uses the sumdbClient to verify the zip file targeted by the
// name with the modulePath and moduleVersion.
func verifyZipFile(sumdbClient *sumdb.Client, name, modulePath, moduleVersion string) error {
	sumLines, err := sumdbClient.Lookup(modulePath, moduleVersion)
	if err != nil {
		if errors.Is(err, sumdb.ErrGONOSUMDB) {
			return nil
		}
		return err
	}
	zipHash, err := dirhash.HashZip(name, dirhash.DefaultHash)
	if err != nil {
		return err
	}
	zipSumLine := fmt.Sprintf("%s %s %s", modulePath, moduleVersion, zipHash)
	if !slices.Contains(sumLines, zipSumLine) {
		return notExistErrorf("%s@%s: invalid version: untrusted revision %s", modulePath, moduleVersion, moduleVersion)
	}
	return nil
}

// closerFunc is an adapter to allow the use of an ordinary function as an [io.Closer].
type closerFunc func() error

// Close implements [io.Closer].
func (f closerFunc) Close() error { return f() }
