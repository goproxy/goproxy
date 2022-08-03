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
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/zip"
)

// modResult is an unified result for the [Goproxy.mod].
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
	case "lookup",
		"latest",
		"list",
		"download info",
		"download mod",
		"download zip":
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

	verify := g.goBinEnv["GOSUMDB"] != "off" &&
		!globsMatchPath(g.goBinEnv["GONOSUMDB"], modulePath)

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
				proxyError = notFoundError(fmt.Sprintf(
					"invalid info response: %v",
					err,
				))
				continue
			}

			if !semver.IsValid(mr.Version) || mr.Time.IsZero() {
				proxyError = notFoundError(
					"invalid info response",
				)
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
		case "download info":
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

			if _, err := checkAndFormatInfoFile(
				infoFile.Name(),
				"",
			); err != nil {
				if fallBackOnError ||
					errors.Is(err, errNotFound) {
					proxyError = err
					continue
				}

				return nil, err
			}

			return &modResult{
				Info: infoFile.Name(),
			}, nil
		case "download mod":
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

			if verify {
				if err := verifyModFile(
					g.sumdbClient,
					modFile.Name(),
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
			}

			return &modResult{
				GoMod: modFile.Name(),
			}, nil
		case "download zip":
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

			if verify {
				if err := verifyZipFile(
					g.sumdbClient,
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
			}

			return &modResult{
				Zip: zipFile.Name(),
			}, nil
		}
	}

	if !tryDirect {
		if proxyError != nil {
			return nil, proxyError
		}

		return nil, notFoundError(fmt.Sprintf(
			"%s@%s: invalid version: unknown revision %s",
			modulePath,
			moduleVersion,
			moduleVersion,
		))
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
	case "download info", "download mod", "download zip":
		args = []string{
			"mod",
			"download",
			"-json",
			fmt.Sprint(modulePath, "@", moduleVersion),
		}
	}

	cmd := exec.CommandContext(ctx, g.goBinName, args...)
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

		return nil, notFoundError(errorMessage)
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
	case "download info", "download mod", "download zip":
		mr.Info, err = checkAndFormatInfoFile(mr.Info, goproxyRoot)
		if err != nil {
			return nil, err
		}

		if verify {
			if err := verifyModFile(
				g.sumdbClient,
				mr.GoMod,
				modulePath,
				moduleVersion,
			); err != nil {
				return nil, err
			}

			if err := verifyZipFile(
				g.sumdbClient,
				mr.Zip,
				modulePath,
				moduleVersion,
			); err != nil {
				return nil, err
			}
		}
	}

	return &mr, nil
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
		f, err := ioutil.TempFile(tempDir, "info")
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
