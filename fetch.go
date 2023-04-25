package goproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/zip"
)

// Fetcher is an interface for performing fetch operations from another source such as
// an upstream proxy, a private VCS, or direct via Go tools
// The Builtin implementation faithfully follows the GOPROXY protocol

type Fetch interface {
	Name() string
	ContentType() string
	Do(ctx context.Context) (*FetchResult, error)
	Ops() FetchOps
}
type Fetcher interface {
	NewFetch(*Goproxy, string, string) (Fetch, error)
}

// FetchOps is the operation of the [Fetch].
type FetchOps uint8

// The BuiltinFetch operations.
const (
	FetchOpsInvalid FetchOps = iota
	FetchOpsResolve
	FetchOpsList
	FetchOpsDownloadInfo
	FetchOpsDownloadMod
	FetchOpsDownloadZip
)

// String implements the [fmt.Stringer].
func (fo FetchOps) String() string {
	switch fo {
	case FetchOpsResolve:
		return "resolve"
	case FetchOpsList:
		return "list"
	case FetchOpsDownloadInfo:
		return "download info"
	case FetchOpsDownloadMod:
		return "download mod"
	case FetchOpsDownloadZip:
		return "download zip"
	}

	return "invalid"
}

// FetchResult is a unified result for the [Fetch].
type FetchResult struct {
	F Fetch

	Version  string
	Time     time.Time
	Versions []string
	Info     string
	GoMod    string
	Zip      string
}

// Open opens the content of the fr.
func (fr *FetchResult) Open() (io.ReadSeekCloser, error) {
	switch fr.F.Ops() {
	case FetchOpsResolve:
		content := strings.NewReader(marshalInfo(fr.Version, fr.Time))
		return struct {
			io.ReadCloser
			io.Seeker
		}{io.NopCloser(content), content}, nil
	case FetchOpsList:
		content := strings.NewReader(strings.Join(fr.Versions, "\n"))
		return struct {
			io.ReadCloser
			io.Seeker
		}{io.NopCloser(content), content}, nil
	case FetchOpsDownloadInfo:
		return os.Open(fr.Info)
	case FetchOpsDownloadMod:
		return os.Open(fr.GoMod)
	case FetchOpsDownloadZip:
		return os.Open(fr.Zip)
	}

	return nil, errors.New("invalid BuiltinFetch operation")
}

// BuiltinFetch is a module BuiltinFetch. All its fields are populated only by the [NewFetch].
type BuiltinFetch struct {
	g                *Goproxy
	ops              FetchOps
	name             string
	tempDir          string
	modulePath       string
	moduleVersion    string
	modAtVer         string
	requiredToVerify bool
	contentType      string
}

type BuiltinFetcher string

// newFetch returns a new instance of the [Fetch].
func (fetcher BuiltinFetcher) NewFetch(g *Goproxy, name, tempDir string) (Fetch, error) {
	f := &BuiltinFetch{
		g:       g,
		name:    name,
		tempDir: tempDir,
	}
	var escapedModulePath string
	if strings.HasSuffix(name, "/@latest") {
		escapedModulePath = strings.TrimSuffix(name, "/@latest")
		f.ops = FetchOpsResolve
		f.moduleVersion = "latest"
		f.contentType = "application/json; charset=utf-8"
	} else if strings.HasSuffix(name, "/@v/list") {
		escapedModulePath = strings.TrimSuffix(name, "/@v/list")
		f.ops = FetchOpsList
		f.moduleVersion = "latest"
		f.contentType = "text/plain; charset=utf-8"
	} else {
		var (
			base string
			ok   bool
		)
		escapedModulePath, base, ok = strings.Cut(name, "/@v/")
		if !ok {
			return nil, errors.New("missing /@v/")
		}

		ext := path.Ext(base)
		escapedModuleVersion := strings.TrimSuffix(base, ext)
		switch ext {
		case ".info":
			f.ops = FetchOpsDownloadInfo
			f.contentType = "application/json; charset=utf-8"
		case ".mod":
			f.ops = FetchOpsDownloadMod
			f.contentType = "text/plain; charset=utf-8"
		case ".zip":
			f.ops = FetchOpsDownloadZip
			f.contentType = "application/zip"
		case "":
			return nil, fmt.Errorf("no file extension in filename %q", escapedModuleVersion)
		default:
			return nil, fmt.Errorf("unexpected extension %q", ext)
		}

		var err error
		f.moduleVersion, err = module.UnescapeVersion(escapedModuleVersion)
		if err != nil {
			return nil, err
		}

		if f.moduleVersion == "latest" {
			return nil, errors.New("invalid version")
		} else if !semver.IsValid(f.moduleVersion) {
			if f.ops == FetchOpsDownloadInfo {
				f.ops = FetchOpsResolve
			} else {
				return nil, errors.New("unrecognized version")
			}
		}
	}
	var err error
	f.modulePath, err = module.UnescapePath(escapedModulePath)
	if err != nil {
		return nil, err
	}
	f.modAtVer = f.modulePath + "@" + f.moduleVersion
	f.requiredToVerify = g.envGOSUMDB != "off" && !globsMatchPath(g.envGONOSUMDB, f.modulePath)
	return f, nil
}

func (f *BuiltinFetch) Name() string {
	return f.name
}

func (f *BuiltinFetch) ContentType() string {
	return f.contentType
}

func (f *BuiltinFetch) Ops() FetchOps {
	return f.ops
}

// Do executes the f.
func (f *BuiltinFetch) Do(ctx context.Context) (*FetchResult, error) {
	if globsMatchPath(f.g.envGONOPROXY, f.modulePath) {
		return f.doDirect(ctx)
	}
	var r *FetchResult
	if err := walkGOPROXY(f.g.envGOPROXY, func(proxy string) error {
		var err error
		r, err = f.doProxy(ctx, proxy)
		return err
	}, func() error {
		var err error
		r, err = f.doDirect(ctx)
		return err
	}, func() error {
		// go/src/cmd/go/internal/modfetch.errProxyOff
		return notFoundError("module lookup disabled by GOPROXY=off")
	}); err != nil {
		return nil, err
	}
	return r, nil
}

// doProxy executes the f via the proxy.
func (f *BuiltinFetch) doProxy(ctx context.Context, proxy string) (*FetchResult, error) {
	proxyURL, err := parseRawURL(proxy)
	if err != nil {
		return nil, err
	}

	tempFile, err := os.CreateTemp(f.tempDir, "")
	if err != nil {
		return nil, err
	}
	if err := httpGet(ctx, f.g.HttpClient, appendURL(proxyURL, f.name).String(), tempFile); err != nil {
		return nil, err
	}
	if err := tempFile.Close(); err != nil {
		return nil, err
	}

	r := &FetchResult{F: f}
	switch f.ops {
	case FetchOpsResolve:
		b, err := os.ReadFile(tempFile.Name())
		if err != nil {
			return nil, err
		}
		r.Version, r.Time, err = unmarshalInfo(string(b))
		if err != nil {
			return nil, notFoundError(fmt.Sprintf("invalid info response: %v", err))
		}
	case FetchOpsList:
		b, err := os.ReadFile(tempFile.Name())
		if err != nil {
			return nil, err
		}

		lines := strings.Split(string(b), "\n")
		r.Versions = make([]string, 0, len(lines))
		for _, line := range lines {
			// go/src/cmd/go/internal/modfetch.proxyRepo.Versions
			lineParts := strings.Fields(line)
			if len(lineParts) > 0 && semver.IsValid(lineParts[0]) && !module.IsPseudoVersion(lineParts[0]) {
				r.Versions = append(r.Versions, lineParts[0])
			}
		}

		sort.Slice(r.Versions, func(i, j int) bool {
			return semver.Compare(r.Versions[i], r.Versions[j]) < 0
		})
	case FetchOpsDownloadInfo:
		if err := checkAndFormatInfoFile(tempFile.Name()); err != nil {
			return nil, err
		}
		r.Info = tempFile.Name()
	case FetchOpsDownloadMod:
		if err := checkModFile(tempFile.Name()); err != nil {
			return nil, err
		}
		if f.requiredToVerify {
			if err := verifyModFile(f.g.SumdbClient, tempFile.Name(), f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}
		}
		r.GoMod = tempFile.Name()
	case FetchOpsDownloadZip:
		if err := checkZipFile(tempFile.Name(), f.modulePath, f.moduleVersion); err != nil {
			return nil, err
		}
		if f.requiredToVerify {
			if err := verifyZipFile(f.g.SumdbClient, tempFile.Name(), f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}
		}
		r.Zip = tempFile.Name()
	}
	return r, nil
}

// doDirect executes the f directly using the local go command.
func (f *BuiltinFetch) doDirect(ctx context.Context) (*FetchResult, error) {
	if f.g.directFetchWorkerPool != nil {
		f.g.directFetchWorkerPool <- struct{}{}
		defer func() { <-f.g.directFetchWorkerPool }()
	}

	var args []string
	switch f.ops {
	case FetchOpsResolve:
		args = []string{"list", "-json", "-m", f.modAtVer}
	case FetchOpsList:
		args = []string{"list", "-json", "-m", "-versions", f.modAtVer}
	case FetchOpsDownloadInfo, FetchOpsDownloadMod, FetchOpsDownloadZip:
		args = []string{"mod", "download", "-json", f.modAtVer}
	}

	cmd := exec.CommandContext(ctx, f.g.goBinName, args...)
	cmd.Env = f.g.env
	cmd.Dir = f.tempDir
	stdout, err := cmd.Output()
	if err != nil {
		if err := ctx.Err(); errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("command %v: %w", cmd.Args, err)
		}

		output := stdout
		if len(output) > 0 {
			var goError struct{ Error string }
			if err := json.Unmarshal(output, &goError); err != nil {
				return nil, err
			}
			if goError.Error != "" {
				output = []byte(goError.Error)
			}
		} else if ee, ok := err.(*exec.ExitError); ok {
			output = ee.Stderr
		} else {
			return nil, err
		}

		var msg string
		for _, line := range strings.Split(string(output), "\n") {
			if !strings.HasPrefix(line, "go: finding") {
				msg += line + "\n"
			}
		}
		msg = strings.TrimPrefix(msg, "go: ")
		msg = strings.TrimPrefix(msg, "go list -m: ")
		msg = strings.TrimRight(msg, "\n")
		return nil, notFoundError(msg)
	}

	r := &FetchResult{F: f}
	if err := json.Unmarshal(stdout, r); err != nil {
		return nil, err
	}
	switch f.ops {
	case FetchOpsList:
		sort.Slice(r.Versions, func(i, j int) bool {
			return semver.Compare(r.Versions[i], r.Versions[j]) < 0
		})
	case FetchOpsDownloadInfo, FetchOpsDownloadMod, FetchOpsDownloadZip:
		if err := checkAndFormatInfoFile(r.Info); err != nil {
			return nil, err
		}
		if f.requiredToVerify {
			if err := verifyModFile(f.g.SumdbClient, r.GoMod, f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}

			if err := verifyZipFile(f.g.SumdbClient, r.Zip, f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
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
	} else if !semver.IsValid(info.Version) {
		return "", time.Time{}, errors.New("empty version")
	} else if info.Time.IsZero() {
		return "", time.Time{}, errors.New("zero time")
	}
	return info.Version, info.Time, nil
}

// checkAndFormatInfoFile checks and formats the info file targeted by the name.
func checkAndFormatInfoFile(name string) error {
	b, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	infoVersion, infoTime, err := unmarshalInfo(string(b))
	if err != nil {
		return notFoundError(fmt.Sprintf("invalid info file: %v", err))
	}
	if info := marshalInfo(infoVersion, infoTime); info != string(b) {
		return os.WriteFile(name, []byte(info), 0o644)
	}
	return nil
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

	return notFoundError("invalid mod file: missing module directive")
}

// verifyModFile uses the SumdbClient to verify the mod file targeted by the
// name with the modulePath and moduleVersion.
func verifyModFile(sumdbClient *sumdb.Client, name, modulePath, moduleVersion string) error {
	gosumLines, err := sumdbClient.Lookup(modulePath, moduleVersion+"/go.mod")
	if err != nil {
		return err
	}

	modHash, err := dirhash.DefaultHash([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
		return os.Open(name)
	})
	if err != nil {
		return err
	}
	if !stringSliceContains(gosumLines, fmt.Sprintf("%s %s/go.mod %s", modulePath, moduleVersion, modHash)) {
		return notFoundError(fmt.Sprintf("%s@%s: invalid version: untrusted revision %s", modulePath, moduleVersion, moduleVersion))
	}

	return nil
}

// checkZipFile checks the zip file targeted by the name with the modulePath and
// moduleVersion.
func checkZipFile(name, modulePath, moduleVersion string) error {
	if _, err := zip.CheckZip(module.Version{Path: modulePath, Version: moduleVersion}, name); err != nil {
		return notFoundError(fmt.Sprintf("invalid zip file: %v", err))
	}
	return nil
}

// verifyZipFile uses the SumdbClient to verify the zip file targeted by the
// name with the modulePath and moduleVersion.
func verifyZipFile(sumdbClient *sumdb.Client, name, modulePath, moduleVersion string) error {
	gosumLines, err := sumdbClient.Lookup(modulePath, moduleVersion)
	if err != nil {
		return err
	}

	zipHash, err := dirhash.HashZip(name, dirhash.DefaultHash)
	if err != nil {
		return err
	}
	if !stringSliceContains(gosumLines, fmt.Sprintf("%s %s %s", modulePath, moduleVersion, zipHash)) {
		return notFoundError(fmt.Sprintf("%s@%s: invalid version: untrusted revision %s", modulePath, moduleVersion, moduleVersion))
	}

	return nil
}
