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

// fetch is a module fetch. All of its fields are populated only by [newFetch].
type fetch struct {
	g                *Goproxy
	ops              fetchOps
	name             string
	tempDir          string
	modulePath       string
	moduleVersion    string
	modAtVer         string
	requiredToVerify bool
	contentType      string
}

// newFetch parses the name and returns a new [fetch].
func newFetch(g *Goproxy, name, tempDir string) (*fetch, error) {
	f := &fetch{
		g:       g,
		name:    name,
		tempDir: tempDir,
	}
	var escapedModulePath string
	if strings.HasSuffix(name, "/@latest") {
		escapedModulePath = strings.TrimSuffix(name, "/@latest")
		f.ops = fetchOpsResolve
		f.moduleVersion = "latest"
		f.contentType = "application/json; charset=utf-8"
	} else if strings.HasSuffix(name, "/@v/list") {
		escapedModulePath = strings.TrimSuffix(name, "/@v/list")
		f.ops = fetchOpsList
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
			f.ops = fetchOpsDownloadInfo
			f.contentType = "application/json; charset=utf-8"
		case ".mod":
			f.ops = fetchOpsDownloadMod
			f.contentType = "text/plain; charset=utf-8"
		case ".zip":
			f.ops = fetchOpsDownloadZip
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
			if f.ops == fetchOpsDownloadInfo {
				f.ops = fetchOpsResolve
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

// do executes the f.
func (f *fetch) do(ctx context.Context) (*fetchResult, error) {
	if globsMatchPath(f.g.envGONOPROXY, f.modulePath) {
		return f.doDirect(ctx)
	}
	var r *fetchResult
	if err := walkEnvGOPROXY(f.g.envGOPROXY, func(proxy string) error {
		var err error
		r, err = f.doProxy(ctx, proxy)
		return err
	}, func() error {
		var err error
		r, err = f.doDirect(ctx)
		return err
	}, func() error {
		return notFoundErrorf("module lookup disabled by GOPROXY=off")
	}); err != nil {
		return nil, err
	}
	return r, nil
}

// doProxy executes the f via the proxy.
func (f *fetch) doProxy(ctx context.Context, proxy string) (*fetchResult, error) {
	proxyURL, err := parseRawURL(proxy)
	if err != nil {
		return nil, err
	}

	tempFile, err := os.CreateTemp(f.tempDir, "")
	if err != nil {
		return nil, err
	}
	if err := httpGet(ctx, f.g.httpClient, appendURL(proxyURL, f.name).String(), tempFile); err != nil {
		return nil, err
	}
	if err := tempFile.Close(); err != nil {
		return nil, err
	}

	r := &fetchResult{f: f}
	switch f.ops {
	case fetchOpsResolve:
		b, err := os.ReadFile(tempFile.Name())
		if err != nil {
			return nil, err
		}
		r.Version, r.Time, err = unmarshalInfo(string(b))
		if err != nil {
			return nil, notFoundErrorf("invalid info response: %w", err)
		}
	case fetchOpsList:
		b, err := os.ReadFile(tempFile.Name())
		if err != nil {
			return nil, err
		}

		lines := strings.Split(string(b), "\n")
		r.Versions = make([]string, 0, len(lines))
		for _, line := range lines {
			lineParts := strings.Fields(line)
			if len(lineParts) > 0 && semver.IsValid(lineParts[0]) && !module.IsPseudoVersion(lineParts[0]) {
				r.Versions = append(r.Versions, lineParts[0])
			}
		}

		sort.Slice(r.Versions, func(i, j int) bool {
			return semver.Compare(r.Versions[i], r.Versions[j]) < 0
		})
	case fetchOpsDownloadInfo:
		if err := checkAndFormatInfoFile(tempFile.Name()); err != nil {
			return nil, err
		}
		r.Info = tempFile.Name()
	case fetchOpsDownloadMod:
		if err := checkModFile(tempFile.Name()); err != nil {
			return nil, err
		}
		if f.requiredToVerify {
			if err := verifyModFile(f.g.sumdbClient, tempFile.Name(), f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}
		}
		r.GoMod = tempFile.Name()
	case fetchOpsDownloadZip:
		if err := checkZipFile(tempFile.Name(), f.modulePath, f.moduleVersion); err != nil {
			return nil, err
		}
		if f.requiredToVerify {
			if err := verifyZipFile(f.g.sumdbClient, tempFile.Name(), f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}
		}
		r.Zip = tempFile.Name()
	}
	return r, nil
}

// doDirect executes the f directly using the local go command.
func (f *fetch) doDirect(ctx context.Context) (*fetchResult, error) {
	if f.g.directFetchWorkerPool != nil {
		f.g.directFetchWorkerPool <- struct{}{}
		defer func() { <-f.g.directFetchWorkerPool }()
	}

	var args []string
	switch f.ops {
	case fetchOpsResolve:
		args = []string{"list", "-json", "-m", f.modAtVer}
	case fetchOpsList:
		args = []string{"list", "-json", "-m", "-versions", f.modAtVer}
	case fetchOpsDownloadInfo, fetchOpsDownloadMod, fetchOpsDownloadZip:
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
		return nil, notFoundErrorf(msg)
	}

	r := &fetchResult{f: f}
	if err := json.Unmarshal(stdout, r); err != nil {
		return nil, err
	}
	switch f.ops {
	case fetchOpsList:
		sort.Slice(r.Versions, func(i, j int) bool {
			return semver.Compare(r.Versions[i], r.Versions[j]) < 0
		})
	case fetchOpsDownloadInfo, fetchOpsDownloadMod, fetchOpsDownloadZip:
		if err := checkAndFormatInfoFile(r.Info); err != nil {
			return nil, err
		}
		if f.requiredToVerify {
			if err := verifyModFile(f.g.sumdbClient, r.GoMod, f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}
			if err := verifyZipFile(f.g.sumdbClient, r.Zip, f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

// fetchOps is the operation of [fetch].
type fetchOps uint8

// The fetch operations.
const (
	fetchOpsInvalid fetchOps = iota
	fetchOpsResolve
	fetchOpsList
	fetchOpsDownloadInfo
	fetchOpsDownloadMod
	fetchOpsDownloadZip
)

// String implements [fmt.Stringer].
func (fo fetchOps) String() string {
	switch fo {
	case fetchOpsResolve:
		return "resolve"
	case fetchOpsList:
		return "list"
	case fetchOpsDownloadInfo:
		return "download info"
	case fetchOpsDownloadMod:
		return "download mod"
	case fetchOpsDownloadZip:
		return "download zip"
	}
	return "invalid"
}

// fetchResult is a unified result for [fetch].
type fetchResult struct {
	f *fetch

	Version  string
	Time     time.Time
	Versions []string
	Info     string
	GoMod    string
	Zip      string
}

// Open opens the content of the fr.
func (fr *fetchResult) Open() (io.ReadSeekCloser, error) {
	switch fr.f.ops {
	case fetchOpsResolve:
		content := strings.NewReader(marshalInfo(fr.Version, fr.Time))
		return struct {
			io.ReadCloser
			io.Seeker
		}{io.NopCloser(content), content}, nil
	case fetchOpsList:
		content := strings.NewReader(strings.Join(fr.Versions, "\n"))
		return struct {
			io.ReadCloser
			io.Seeker
		}{io.NopCloser(content), content}, nil
	case fetchOpsDownloadInfo:
		return os.Open(fr.Info)
	case fetchOpsDownloadMod:
		return os.Open(fr.GoMod)
	case fetchOpsDownloadZip:
		return os.Open(fr.Zip)
	}
	return nil, errors.New("invalid fetch operation")
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
		return notFoundErrorf("invalid info file: %w", err)
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

	return notFoundErrorf("invalid mod file: missing module directive")
}

// verifyModFile uses the sumdbClient to verify the mod file targeted by the
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
		return notFoundErrorf("%s@%s: invalid version: untrusted revision %s", modulePath, moduleVersion, moduleVersion)
	}

	return nil
}

// checkZipFile checks the zip file targeted by the name with the modulePath and
// moduleVersion.
func checkZipFile(name, modulePath, moduleVersion string) error {
	if _, err := zip.CheckZip(module.Version{Path: modulePath, Version: moduleVersion}, name); err != nil {
		return notFoundErrorf("invalid zip file: %w", err)
	}
	return nil
}

// verifyZipFile uses the sumdbClient to verify the zip file targeted by the
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
		return notFoundErrorf("%s@%s: invalid version: untrusted revision %s", modulePath, moduleVersion, moduleVersion)
	}

	return nil
}
