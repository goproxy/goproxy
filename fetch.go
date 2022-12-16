package goproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
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

// fetch is a module fetch. All its fields are populated only by the [newFetch].
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

// newFetch returns a new instance of the [fetch].
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
		nameParts := strings.Split(name, "/@v/")
		if len(nameParts) != 2 {
			return nil, errors.New("missing /@v/")
		}

		escapedModulePath = nameParts[0]

		nameExt := path.Ext(nameParts[1])
		switch nameExt {
		case ".info":
			f.ops = fetchOpsDownloadInfo
			f.contentType = "application/json; charset=utf-8"
		case ".mod":
			f.ops = fetchOpsDownloadMod
			f.contentType = "text/plain; charset=utf-8"
		case ".zip":
			f.ops = fetchOpsDownloadZip
			f.contentType = "application/zip"
		default:
			return nil, fmt.Errorf(
				"unexpected extension %q",
				nameExt,
			)
		}

		var err error
		f.moduleVersion, err = module.UnescapeVersion(
			strings.TrimSuffix(nameParts[1], nameExt),
		)
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

	f.modAtVer = fmt.Sprint(f.modulePath, "@", f.moduleVersion)
	f.requiredToVerify = g.goBinEnv["GOSUMDB"] != "off" &&
		!globsMatchPath(g.goBinEnv["GONOSUMDB"], f.modulePath)

	return f, nil
}

// do executes the f.
func (f *fetch) do(ctx context.Context) (*fetchResult, error) {
	tryDirect := globsMatchPath(f.g.goBinEnv["GONOPROXY"], f.modulePath)

	var proxyError error
	for goproxy := f.g.goBinEnv["GOPROXY"]; goproxy != "" && !tryDirect; {
		var (
			proxy           string
			fallBackOnError bool
		)

		if i := strings.IndexAny(goproxy, ",|"); i >= 0 {
			proxy = goproxy[:i]
			fallBackOnError = goproxy[i] == '|'
			goproxy = goproxy[i+1:]
		} else {
			proxy = goproxy
			goproxy = ""
		}

		switch proxy {
		case "direct":
			tryDirect = true
			continue
		case "off":
			// go/src/cmd/go/internal/modfetch.errProxyOff
			return nil, notFoundError(
				"module lookup disabled by GOPROXY=off",
			)
		}

		r, err := f.doProxy(ctx, proxy)
		if err != nil {
			if fallBackOnError || errors.Is(err, errNotFound) {
				proxyError = err
				continue
			}

			return nil, err
		}

		return r, nil
	}

	if !tryDirect {
		if proxyError != nil {
			return nil, proxyError
		}

		return nil, notFoundError(fmt.Sprintf(
			"%s: invalid version: unknown revision %s",
			f.modAtVer,
			f.moduleVersion,
		))
	}

	return f.doDirect(ctx)
}

// doProxy executes the f via the proxy.
func (f *fetch) doProxy(
	ctx context.Context,
	proxy string,
) (*fetchResult, error) {
	proxyURL, err := parseRawURL(proxy)
	if err != nil {
		return nil, err
	}

	tempFile, err := ioutil.TempFile(f.tempDir, "")
	if err != nil {
		return nil, err
	}

	if err := httpGet(
		ctx,
		f.g.httpClient,
		appendURL(proxyURL, f.name).String(),
		tempFile,
	); err != nil {
		return nil, err
	}

	if err := tempFile.Close(); err != nil {
		return nil, err
	}

	r := &fetchResult{f: f}
	switch f.ops {
	case fetchOpsResolve:
		b, err := ioutil.ReadFile(tempFile.Name())
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(b, r); err != nil {
			return nil, notFoundError(fmt.Sprintf(
				"invalid info response: %v",
				err,
			))
		}

		if !semver.IsValid(r.Version) || r.Time.IsZero() {
			return nil, notFoundError("invalid info response")
		}

		r.Time = r.Time.UTC()
	case fetchOpsList:
		b, err := ioutil.ReadFile(tempFile.Name())
		if err != nil {
			return nil, err
		}

		for _, version := range strings.Split(string(b), "\n") {
			if semver.IsValid(version) {
				r.Versions = append(r.Versions, version)
			}
		}

		sort.Slice(r.Versions, func(i, j int) bool {
			return semver.Compare(r.Versions[i], r.Versions[j]) < 0
		})
	case fetchOpsDownloadInfo:
		if _, err := checkAndFormatInfoFile(
			tempFile.Name(),
			"",
		); err != nil {
			return nil, err
		}

		r.Info = tempFile.Name()
	case fetchOpsDownloadMod:
		if err := checkModFile(tempFile.Name()); err != nil {
			return nil, err
		}

		if f.requiredToVerify {
			if err := verifyModFile(
				f.g.sumdbClient,
				tempFile.Name(),
				f.modulePath,
				f.moduleVersion,
			); err != nil {
				return nil, err
			}
		}

		r.GoMod = tempFile.Name()
	case fetchOpsDownloadZip:
		if err := checkZipFile(
			tempFile.Name(),
			f.modulePath,
			f.moduleVersion,
		); err != nil {
			return nil, err
		}

		if f.requiredToVerify {
			if err := verifyZipFile(
				f.g.sumdbClient,
				tempFile.Name(),
				f.modulePath,
				f.moduleVersion,
			); err != nil {
				return nil, err
			}
		}

		r.Zip = tempFile.Name()
	}

	return r, nil
}

// doDirect executes the f directly using the local go command.
func (f *fetch) doDirect(ctx context.Context) (*fetchResult, error) {
	if f.g.goBinWorkerChan != nil {
		f.g.goBinWorkerChan <- struct{}{}
		defer func() {
			<-f.g.goBinWorkerChan
		}()
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
	cmd.Env = make([]string, 0, len(f.g.goBinEnv)+6)
	for k, v := range f.g.goBinEnv {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Env = append(
		cmd.Env,
		"GO111MODULE=on",
		"GOPROXY=direct",
		"GONOPROXY=",
		"GOSUMDB=off",
		"GONOSUMDB=",
		"GOPRIVATE=",
	)

	cmd.Dir = f.tempDir
	stdout, err := cmd.Output()
	if err != nil {
		if err := ctx.Err(); errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("command %v: %w", cmd.Args, err)
		}

		output := stdout
		if len(output) > 0 {
			m := map[string]interface{}{}
			if err := json.Unmarshal(output, &m); err != nil {
				return nil, err
			}

			if es, ok := m["Error"].(string); ok {
				output = []byte(es)
			}
		} else if ee, ok := err.(*exec.ExitError); ok {
			output = ee.Stderr
		} else {
			return nil, err
		}

		var errorMessage string
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "go: finding") ||
				strings.HasPrefix(line, "\tserver response:") {
				continue
			}

			errorMessage = fmt.Sprint(errorMessage, line, "\n")
		}

		errorMessage = strings.TrimPrefix(errorMessage, "go list -m: ")
		errorMessage = strings.TrimRight(errorMessage, "\n")

		return nil, notFoundError(errorMessage)
	}

	r := &fetchResult{f: f}
	if err := json.Unmarshal(stdout, r); err != nil {
		return nil, err
	}

	switch f.ops {
	case fetchOpsResolve:
		r.Time = r.Time.UTC()
	case fetchOpsList:
		sort.Slice(r.Versions, func(i, j int) bool {
			return semver.Compare(r.Versions[i], r.Versions[j]) < 0
		})
	case fetchOpsDownloadInfo, fetchOpsDownloadMod, fetchOpsDownloadZip:
		r.Info, err = checkAndFormatInfoFile(r.Info, f.tempDir)
		if err != nil {
			return nil, err
		}

		if f.requiredToVerify {
			if err := verifyModFile(
				f.g.sumdbClient,
				r.GoMod,
				f.modulePath,
				f.moduleVersion,
			); err != nil {
				return nil, err
			}

			if err := verifyZipFile(
				f.g.sumdbClient,
				r.Zip,
				f.modulePath,
				f.moduleVersion,
			); err != nil {
				return nil, err
			}
		}
	}

	return r, nil
}

// fetchOps is the operation of the [fetch].
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

// String implements the [fmt.Stringer].
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

// fetchResult is an unified result for the [fetch].
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
func (fr *fetchResult) Open() (readSeekCloser, error) {
	switch fr.f.ops {
	case fetchOpsResolve:
		info, err := json.Marshal(struct {
			Version string
			Time    time.Time
		}{
			fr.Version,
			fr.Time,
		})
		if err != nil {
			return nil, err
		}
		content := bytes.NewReader(info)
		return struct {
			io.ReadCloser
			io.Seeker
		}{
			nopCloser{content},
			content,
		}, nil
	case fetchOpsList:
		list := strings.Join(fr.Versions, "\n")
		content := strings.NewReader(list)
		return struct {
			io.ReadCloser
			io.Seeker
		}{
			nopCloser{content},
			content,
		}, nil
	case fetchOpsDownloadInfo:
		return os.Open(fr.Info)
	case fetchOpsDownloadMod:
		return os.Open(fr.GoMod)
	case fetchOpsDownloadZip:
		return os.Open(fr.Zip)
	}

	return nil, errors.New("invalid fetch operation")
}

// checkAndFormatInfoFile checks and formats the info file targeted by the name.
//
// If the tempDir is not empty, a new temporary info file will be created in it.
// Otherwise, the info file targeted by the name will be replaced.
func checkAndFormatInfoFile(name, tempDir string) (string, error) {
	b, err := ioutil.ReadFile(name)
	if err != nil {
		return "", err
	}

	var info struct {
		Version string
		Time    string
	}

	if err := json.Unmarshal(b, &info); err != nil {
		return "", notFoundError(fmt.Sprintf(
			"invalid info file: %v",
			err,
		))
	}

	if !semver.IsValid(info.Version) || info.Time == "" {
		return "", notFoundError("invalid info file")
	}

	t, err := time.Parse(time.RFC3339Nano, info.Time)
	if err != nil {
		return "", notFoundError(fmt.Sprintf(
			"invalid info file: %v",
			err,
		))
	}

	fb, err := json.Marshal(struct {
		Version string
		Time    time.Time
	}{
		Version: info.Version,
		Time:    t.UTC(),
	})
	if err != nil {
		return "", err
	}

	if bytes.Equal(fb, b) {
		return name, nil
	}

	if tempDir != "" {
		f, err := ioutil.TempFile(tempDir, "")
		if err != nil {
			return "", err
		}

		if _, err := f.Write(fb); err != nil {
			return "", err
		}

		if err := f.Close(); err != nil {
			return "", err
		}

		return f.Name(), nil
	}

	fi, err := os.Stat(name)
	if err != nil {
		return "", err
	}

	if err := ioutil.WriteFile(name, fb, fi.Mode()); err != nil {
		return "", err
	}

	return name, nil
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
		if strings.Contains(scanner.Text(), "module") {
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return notFoundError(fmt.Sprintf("invalid mod file: %v", err))
	}

	return notFoundError("invalid mod file")
}

// verifyModFile uses the sumdbClient to verify the mod file targeted by the
// name with the modulePath and moduleVersion.
func verifyModFile(
	sumdbClient *sumdb.Client,
	name string,
	modulePath string,
	moduleVersion string,
) error {
	modLines, err := sumdbClient.Lookup(
		modulePath,
		fmt.Sprint(moduleVersion, "/go.mod"),
	)
	if err != nil {
		return err
	}

	modHash, err := dirhash.Hash1(
		[]string{"go.mod"},
		func(string) (io.ReadCloser, error) {
			return os.Open(name)
		},
	)
	if err != nil {
		return err
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
		return notFoundError(fmt.Sprintf(
			"%s@%s: invalid version: untrusted revision %s",
			modulePath,
			moduleVersion,
			moduleVersion,
		))
	}

	return nil
}

// checkZipFile checks the zip file targeted by the name with the modulePath and
// moduleVersion.
func checkZipFile(name, modulePath, moduleVersion string) error {
	if _, err := zip.CheckZip(
		module.Version{
			Path:    modulePath,
			Version: moduleVersion,
		},
		name,
	); err != nil {
		return notFoundError(fmt.Sprintf("invalid zip file: %v", err))
	}

	return nil
}

// verifyZipFile uses the sumdbClient to verify the zip file targeted by the
// name with the modulePath and moduleVersion.
func verifyZipFile(
	sumdbClient *sumdb.Client,
	name string,
	modulePath string,
	moduleVersion string,
) error {
	zipLines, err := sumdbClient.Lookup(modulePath, moduleVersion)
	if err != nil {
		return err
	}

	zipHash, err := dirhash.HashZip(name, dirhash.DefaultHash)
	if err != nil {
		return err
	}

	if !stringSliceContains(
		zipLines,
		fmt.Sprintf("%s %s %s", modulePath, moduleVersion, zipHash),
	) {
		return notFoundError(fmt.Sprintf(
			"%s@%s: invalid version: untrusted revision %s",
			modulePath,
			moduleVersion,
			moduleVersion,
		))
	}

	return nil
}
