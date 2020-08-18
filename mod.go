package goproxy

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// modResult is an unified result of the `mod`.
type modResult struct {
	Version  string
	Time     time.Time
	Versions []string
	Info     string
	GoMod    string
	Zip      string
}

// mod executes the Go modules related commands based on the operation.
func (g *Goproxy) mod(
	ctx context.Context,
	operation string,
	goproxyRoot string,
	modulePath string,
	moduleVersion string,
) (*modResult, error) {
	switch operation {
	case "lookup", "latest", "list", "download":
	default:
		return nil, errors.New("invalid mod operation")
	}

	escapedModulePath, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, err
	}

	escapedModuleVersion, err := module.EscapeVersion(moduleVersion)
	if err != nil {
		return nil, err
	}

	// Try proxies.

	tryDirect := globsMatchPath(g.goBinEnv["GONOPROXY"], modulePath)

	var proxyError error
	for goproxy := g.goBinEnv["GOPROXY"]; goproxy != "" && !tryDirect; {
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
			return nil, errors.New("disabled by GOPROXY=off")
		}

		proxyURL, err := parseRawURL(proxy)
		if err != nil {
			if fallBackOnError {
				proxyError = err
				continue
			}

			return nil, err
		}

		switch operation {
		case "lookup", "latest":
			var operationURL *url.URL
			if operation == "lookup" {
				operationURL = appendURL(
					proxyURL,
					escapedModulePath,
					"@v",
					fmt.Sprint(
						escapedModuleVersion,
						".info",
					),
				)
			} else {
				operationURL = appendURL(
					proxyURL,
					escapedModulePath,
					"@latest",
				)
			}

			var buf bytes.Buffer
			if err := httpGet(
				ctx,
				g.httpClient,
				operationURL.String(),
				&buf,
			); err != nil {
				if fallBackOnError ||
					errors.Is(err, errNotFound) {
					proxyError = err
					continue
				}

				return nil, err
			}

			mr := modResult{}
			if err := json.Unmarshal(buf.Bytes(), &mr); err != nil {
				errNotFound = &notFoundError{fmt.Errorf(
					"invalid info response: %v",
					err,
				)}
				continue
			}

			if !semver.IsValid(mr.Version) || mr.Time.IsZero() {
				errNotFound = &notFoundError{errors.New(
					"invalid info response",
				)}
				continue
			}

			mr.Time = mr.Time.UTC()

			return &mr, nil
		case "list":
			var buf bytes.Buffer
			if err := httpGet(
				ctx,
				g.httpClient,
				appendURL(
					proxyURL,
					escapedModulePath,
					"@v",
					"list",
				).String(),
				&buf,
			); err != nil {
				if fallBackOnError ||
					errors.Is(err, errNotFound) {
					proxyError = err
					continue
				}

				return nil, err
			}

			mr := modResult{}
			for _, s := range strings.Split(buf.String(), "\n") {
				if semver.IsValid(s) {
					mr.Versions = append(mr.Versions, s)
				}
			}

			sort.Slice(mr.Versions, func(i, j int) bool {
				return semver.Compare(
					mr.Versions[i],
					mr.Versions[j],
				) < 0
			})

			return &mr, nil
		case "download":
			infoFile, err := ioutil.TempFile(goproxyRoot, "info")
			if err != nil {
				if fallBackOnError {
					proxyError = err
					continue
				}

				return nil, err
			}

			if err := httpGet(
				ctx,
				g.httpClient,
				appendURL(
					proxyURL,
					escapedModulePath,
					"@v",
					fmt.Sprint(
						escapedModuleVersion,
						".info",
					),
				).String(),
				infoFile,
			); err != nil {
				infoFile.Close()
				if fallBackOnError ||
					errors.Is(err, errNotFound) {
					proxyError = err
					continue
				}

				return nil, err
			}

			if err := infoFile.Close(); err != nil {
				if fallBackOnError {
					proxyError = err
					continue
				}

				return nil, err
			}

			if err := checkInfoFile(infoFile.Name()); err != nil {
				if fallBackOnError ||
					errors.Is(err, errNotFound) {
					proxyError = err
					continue
				}

				return nil, err
			}

			if _, err := formatInfoFile(
				infoFile.Name(),
				"",
			); err != nil {
				if fallBackOnError {
					proxyError = err
					continue
				}

				return nil, err
			}

			modFile, err := ioutil.TempFile(goproxyRoot, "mod")
			if err != nil {
				if fallBackOnError {
					proxyError = err
					continue
				}

				return nil, err
			}

			if err := httpGet(
				ctx,
				g.httpClient,
				appendURL(
					proxyURL,
					escapedModulePath,
					"@v",
					fmt.Sprint(
						escapedModuleVersion,
						".mod",
					),
				).String(),
				modFile,
			); err != nil {
				modFile.Close()
				if fallBackOnError ||
					errors.Is(err, errNotFound) {
					proxyError = err
					continue
				}

				return nil, err
			}

			if err := modFile.Close(); err != nil {
				if fallBackOnError {
					proxyError = err
					continue
				}

				return nil, err
			}

			if err := checkModFile(modFile.Name()); err != nil {
				if fallBackOnError ||
					errors.Is(err, errNotFound) {
					proxyError = err
					continue
				}

				return nil, err
			}

			zipFile, err := ioutil.TempFile(goproxyRoot, "zip")
			if err != nil {
				if fallBackOnError {
					proxyError = err
					continue
				}

				return nil, err
			}

			if err := httpGet(
				ctx,
				g.httpClient,
				appendURL(
					proxyURL,
					escapedModulePath,
					"@v",
					fmt.Sprint(
						escapedModuleVersion,
						".zip",
					),
				).String(),
				zipFile,
			); err != nil {
				zipFile.Close()
				if fallBackOnError ||
					errors.Is(err, errNotFound) {
					proxyError = err
					continue
				}

				return nil, err
			}

			if err := zipFile.Close(); err != nil {
				if fallBackOnError {
					proxyError = err
					continue
				}

				return nil, err
			}

			if err := checkZipFile(
				zipFile.Name(),
				modulePath,
				moduleVersion,
			); err != nil {
				if fallBackOnError ||
					errors.Is(err, errNotFound) {
					proxyError = err
					continue
				}

				return nil, err
			}

			return &modResult{
				Info:  infoFile.Name(),
				GoMod: modFile.Name(),
				Zip:   zipFile.Name(),
			}, nil
		}
	}

	if !tryDirect {
		if proxyError != nil {
			return nil, proxyError
		}

		return nil, &notFoundError{fmt.Errorf(
			"%s@%s: invalid version: unknown revision %s",
			modulePath,
			moduleVersion,
			moduleVersion,
		)}
	}

	// Try direct.

	if g.goBinWorkerChan != nil {
		g.goBinWorkerChan <- struct{}{}
		defer func() {
			<-g.goBinWorkerChan
		}()
	}

	var args []string
	switch operation {
	case "lookup", "latest":
		args = []string{
			"list",
			"-json",
			"-m",
			fmt.Sprint(modulePath, "@", moduleVersion),
		}
	case "list":
		args = []string{
			"list",
			"-json",
			"-m",
			"-versions",
			fmt.Sprint(modulePath, "@", moduleVersion),
		}
	case "download":
		args = []string{
			"mod",
			"download",
			"-json",
			fmt.Sprint(modulePath, "@", moduleVersion),
		}
	}

	if g.GoBinFetchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.GoBinFetchTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, g.GoBinName, args...)
	cmd.Env = make([]string, 0, len(g.goBinEnv)+6)
	for k, v := range g.goBinEnv {
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

	cmd.Dir = goproxyRoot
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

		return nil, &notFoundError{errors.New(errorMessage)}
	}

	mr := modResult{}
	if err := json.Unmarshal(stdout, &mr); err != nil {
		return nil, err
	}

	switch operation {
	case "lookup", "latest":
		mr.Time = mr.Time.UTC()
	case "list":
		sort.Slice(mr.Versions, func(i, j int) bool {
			return semver.Compare(
				mr.Versions[i],
				mr.Versions[j],
			) < 0
		})
	case "download":
		mr.Info, err = formatInfoFile(mr.Info, goproxyRoot)
		if err != nil {
			return nil, err
		}
	}

	return &mr, nil
}

// checkInfoFile checks the info file targeted by the name.
func checkInfoFile(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	var info struct {
		Version string
		Time    time.Time
	}

	if err := json.NewDecoder(f).Decode(&info); err != nil {
		return &notFoundError{fmt.Errorf("invalid info file: %v", err)}
	}

	if !semver.IsValid(info.Version) || info.Time.IsZero() {
		return &notFoundError{errors.New("invalid info file")}
	}

	return nil
}

// formatInfoFile formats the info file targeted by the name.
//
// If the tempDir is not empty, a new temporary info file will be created in it.
// Otherwise, the info file targeted by the name will be replaced.
func formatInfoFile(name, tempDir string) (string, error) {
	b, err := ioutil.ReadFile(name)
	if err != nil {
		return "", err
	}

	var info struct {
		Version string
		Time    time.Time
	}

	if err := json.Unmarshal(b, &info); err != nil {
		return "", err
	}

	info.Time = info.Time.UTC()

	fb, err := json.Marshal(info)
	if err != nil {
		return "", err
	}

	if tempDir != "" {
		f, err := ioutil.TempFile(tempDir, "info")
		if err != nil {
			return "", err
		}

		if _, err := f.Write(fb); err != nil {
			f.Close()
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
		return &notFoundError{fmt.Errorf("invalid mod file: %v", err)}
	}

	return &notFoundError{errors.New("invalid mod file")}
}

// checkZipFile checks the zip file targeted by the name with the modulePath and
// moduleVersion.
func checkZipFile(name, modulePath, moduleVersion string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return &notFoundError{fmt.Errorf("invalid zip file: %v", err)}
	}

	namePrefix := fmt.Sprintf("%s@%s/", modulePath, moduleVersion)
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, namePrefix) {
			return &notFoundError{errors.New("invalid zip file")}
		}
	}

	return nil
}
