package goproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
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
		f.ops = fetchOpsQuery
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

		f.ops = fetchOpsDownload
		ext := path.Ext(base)
		escapedModuleVersion := strings.TrimSuffix(base, ext)
		switch ext {
		case ".info":
			f.contentType = "application/json; charset=utf-8"
		case ".mod":
			f.contentType = "text/plain; charset=utf-8"
		case ".zip":
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

		switch f.moduleVersion {
		case "latest", "upgrade", "patch":
			return nil, errors.New("invalid version")
		}
		if !semver.IsValid(f.moduleVersion) {
			if ext == ".info" {
				f.ops = fetchOpsQuery
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
	f.requiredToVerify = g.envGOSUMDB != "off" && !module.MatchPrefixPatterns(g.envGONOSUMDB, f.modulePath)
	return f, nil
}

// do executes the f.
func (f *fetch) do(ctx context.Context) (*fetchResult, error) {
	if module.MatchPrefixPatterns(f.g.envGONOPROXY, f.modulePath) {
		return f.doDirect(ctx)
	}
	var r *fetchResult
	if err := walkEnvGOPROXY(f.g.envGOPROXY, func(proxy *url.URL) error {
		var err error
		r, err = f.doProxy(ctx, proxy)
		return err
	}, func() error {
		var err error
		r, err = f.doDirect(ctx)
		return err
	}, func() error {
		return notExistErrorf("module lookup disabled by GOPROXY=off")
	}); err != nil {
		return nil, err
	}
	return r, nil
}

// doProxy executes the f via the proxy.
func (f *fetch) doProxy(ctx context.Context, proxy *url.URL) (*fetchResult, error) {
	u := appendURL(proxy, f.name).String()
	r := &fetchResult{}
	switch f.ops {
	case fetchOpsQuery:
		var info bytes.Buffer
		if err := httpGet(ctx, f.g.httpClient, u, &info); err != nil {
			return nil, err
		}
		var err error
		r.Version, r.Time, err = unmarshalInfo(info.String())
		if err != nil {
			return nil, notExistErrorf("invalid info response: %w", err)
		}
	case fetchOpsList:
		var list bytes.Buffer
		if err := httpGet(ctx, f.g.httpClient, u, &list); err != nil {
			return nil, err
		}
		r.Versions = strings.Split(list.String(), "\n")
		for i := range r.Versions {
			parts := strings.Fields(r.Versions[i])
			if len(parts) > 0 && semver.IsValid(parts[0]) && !module.IsPseudoVersion(parts[0]) {
				r.Versions[i] = parts[0]
			} else {
				r.Versions[i] = ""
			}
		}
		semver.Sort(r.Versions)
		firstNotEmptyIndex := 0
		for ; firstNotEmptyIndex < len(r.Versions) && r.Versions[firstNotEmptyIndex] == ""; firstNotEmptyIndex++ {
		}
		r.Versions = r.Versions[firstNotEmptyIndex:]
	case fetchOpsDownload:
		urlWithoutExt := strings.TrimSuffix(u, path.Ext(u))

		var err error
		r.Info, err = httpGetTemp(ctx, f.g.httpClient, urlWithoutExt+".info", f.tempDir)
		if err != nil {
			return nil, err
		}
		if err := checkAndFormatInfoFile(r.Info); err != nil {
			return nil, err
		}

		r.GoMod, err = httpGetTemp(ctx, f.g.httpClient, urlWithoutExt+".mod", f.tempDir)
		if err != nil {
			return nil, err
		}
		if err := checkModFile(r.GoMod); err != nil {
			return nil, err
		}
		if f.requiredToVerify {
			if err := verifyModFile(f.g.sumdbClient, r.GoMod, f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}
		}

		r.Zip, err = httpGetTemp(ctx, f.g.httpClient, urlWithoutExt+".zip", f.tempDir)
		if err != nil {
			return nil, err
		}
		if err := checkZipFile(r.Zip, f.modulePath, f.moduleVersion); err != nil {
			return nil, err
		}
		if f.requiredToVerify {
			if err := verifyZipFile(f.g.sumdbClient, r.Zip, f.modulePath, f.moduleVersion); err != nil {
				return nil, err
			}
		}
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
	case fetchOpsQuery:
		args = []string{"list", "-json", "-m", f.modAtVer}
	case fetchOpsList:
		args = []string{"list", "-json", "-m", "-versions", f.modAtVer}
	case fetchOpsDownload:
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
		return nil, notExistErrorf(msg)
	}

	r := &fetchResult{}
	if err := json.Unmarshal(stdout, r); err != nil {
		return nil, err
	}
	switch f.ops {
	case fetchOpsList:
		semver.Sort(r.Versions)
	case fetchOpsDownload:
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
	fetchOpsQuery
	fetchOpsList
	fetchOpsDownload
)

// String implements [fmt.Stringer].
func (fo fetchOps) String() string {
	switch fo {
	case fetchOpsQuery:
		return "query"
	case fetchOpsList:
		return "list"
	case fetchOpsDownload:
		return "download"
	}
	return "invalid"
}

// fetchResult is a unified result for [fetch].
type fetchResult struct {
	Version  string
	Time     time.Time
	Versions []string
	Info     string
	GoMod    string
	Zip      string
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
		return notExistErrorf("invalid info file: %w", err)
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

	return notExistErrorf("invalid mod file: missing module directive")
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
	gosumLines, err := sumdbClient.Lookup(modulePath, moduleVersion)
	if err != nil {
		return err
	}

	zipHash, err := dirhash.HashZip(name, dirhash.DefaultHash)
	if err != nil {
		return err
	}
	if !stringSliceContains(gosumLines, fmt.Sprintf("%s %s %s", modulePath, moduleVersion, zipHash)) {
		return notExistErrorf("%s@%s: invalid version: untrusted revision %s", modulePath, moduleVersion, moduleVersion)
	}

	return nil
}
