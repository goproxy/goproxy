package goproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os/exec"
	"sort"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// modResult is an unified result of the `mod`.
type modResult struct {
	Version  string
	Versions []string
	Info     string
	GoMod    string
	Zip      string
}

// mod executes the Go modules related commands based on the operation.
func mod(
	workerChan chan struct{},
	goproxyRoot string,
	operation string,
	goBinName string,
	goBinEnv map[string]string,
	modulePath string,
	moduleVersion string,
) (*modResult, error) {
	if workerChan != nil {
		workerChan <- struct{}{}
		defer func() {
			<-workerChan
		}()
	}

	switch operation {
	case "lookup", "latest", "list", "download":
	default:
		return nil, errors.New("invalid mod operation")
	}

	var envGOPROXY string
	if globsMatchPath(goBinEnv["GONOPROXY"], modulePath) ||
		globsMatchPath(goBinEnv["GOPRIVATE"], modulePath) {
		envGOPROXY = "direct"
	} else {
		envGOPROXY = goBinEnv["GOPROXY"]
	}

	if envGOPROXY != "direct" && envGOPROXY != "off" {
		var goproxies []string
		if envGOPROXY != "" {
			goproxies = strings.Split(envGOPROXY, ",")
		} else {
			goproxies = []string{
				"https://proxy.golang.org",
				"direct",
			}
		}

		escapedModulePath, err := module.EscapePath(modulePath)
		if err != nil {
			return nil, err
		}

		escapedModuleVersion, err := module.EscapeVersion(moduleVersion)
		if err != nil {
			return nil, err
		}

		for _, goproxy := range goproxies {
			goproxy = strings.TrimSpace(goproxy)
			if goproxy == "" {
				continue
			}

			if goproxy == "direct" || goproxy == "off" {
				envGOPROXY = goproxy
				break
			}

			switch operation {
			case "lookup", "latest":
				var url string
				if operation == "lookup" {
					url = fmt.Sprintf(
						"%s/%s/@v/%s.info",
						goproxy,
						escapedModulePath,
						escapedModuleVersion,
					)
				} else {
					url = fmt.Sprintf(
						"%s/%s/@latest",
						goproxy,
						escapedModulePath,
					)
				}

				res, err := http.Get(url)
				if err != nil {
					return nil, err
				}
				defer res.Body.Close()

				switch res.StatusCode {
				case http.StatusOK:
				case http.StatusNotFound, http.StatusGone:
					continue
				default:
					return nil, fmt.Errorf(
						"mod %s %s@%s: %s",
						operation,
						modulePath,
						moduleVersion,
						http.StatusText(res.StatusCode),
					)
				}

				mr := modResult{}
				if err := json.NewDecoder(res.Body).Decode(
					&mr,
				); err != nil {
					return nil, err
				}

				return &mr, nil
			case "list":
				res, err := http.Get(fmt.Sprintf(
					"%s/%s/@v/list",
					goproxy,
					escapedModulePath,
				))
				if err != nil {
					return nil, err
				}
				defer res.Body.Close()

				switch res.StatusCode {
				case http.StatusOK:
				case http.StatusNotFound, http.StatusGone:
					continue
				default:
					return nil, fmt.Errorf(
						"mod list %s@%s: %s",
						modulePath,
						moduleVersion,
						http.StatusText(res.StatusCode),
					)
				}

				b, err := ioutil.ReadAll(res.Body)
				if err != nil {
					return nil, err
				}

				versions := []string{}
				for _, b := range bytes.Split(b, []byte{'\n'}) {
					if len(b) == 0 {
						continue
					}

					versions = append(versions, string(b))
				}

				sort.Slice(versions, func(i, j int) bool {
					return semver.Compare(
						versions[i],
						versions[j],
					) < 0
				})

				return &modResult{
					Versions: versions,
				}, nil
			case "download":
				infoFileRes, err := http.Get(fmt.Sprintf(
					"%s/%s/@v/%s.info",
					goproxy,
					escapedModulePath,
					escapedModuleVersion,
				))
				if err != nil {
					return nil, err
				}
				defer infoFileRes.Body.Close()

				switch infoFileRes.StatusCode {
				case http.StatusOK:
				case http.StatusNotFound, http.StatusGone:
					continue
				default:
					return nil, fmt.Errorf(
						"mod download %s@%s: %s",
						modulePath,
						moduleVersion,
						http.StatusText(
							infoFileRes.StatusCode,
						),
					)
				}

				infoFile, err := ioutil.TempFile(
					goproxyRoot,
					"info",
				)
				if err != nil {
					return nil, err
				}

				if _, err := io.Copy(
					infoFile,
					infoFileRes.Body,
				); err != nil {
					return nil, err
				}

				if err := infoFile.Close(); err != nil {
					return nil, err
				}

				modFileRes, err := http.Get(fmt.Sprintf(
					"%s/%s/@v/%s.mod",
					goproxy,
					escapedModulePath,
					escapedModuleVersion,
				))
				if err != nil {
					return nil, err
				}
				defer modFileRes.Body.Close()

				switch modFileRes.StatusCode {
				case http.StatusOK:
				case http.StatusNotFound, http.StatusGone:
					continue
				default:
					return nil, fmt.Errorf(
						"mod download %s@%s: %s",
						modulePath,
						moduleVersion,
						http.StatusText(
							modFileRes.StatusCode,
						),
					)
				}

				modFile, err := ioutil.TempFile(
					goproxyRoot,
					"mod",
				)
				if err != nil {
					return nil, err
				}

				if _, err := io.Copy(
					modFile,
					modFileRes.Body,
				); err != nil {
					return nil, err
				}

				if err := modFile.Close(); err != nil {
					return nil, err
				}

				zipFileRes, err := http.Get(fmt.Sprintf(
					"%s/%s/@v/%s.zip",
					goproxy,
					escapedModulePath,
					escapedModuleVersion,
				))
				if err != nil {
					return nil, err
				}
				defer zipFileRes.Body.Close()

				switch zipFileRes.StatusCode {
				case http.StatusOK:
				case http.StatusNotFound, http.StatusGone:
					continue
				default:
					return nil, fmt.Errorf(
						"mod download %s@%s: %s",
						modulePath,
						moduleVersion,
						http.StatusText(
							zipFileRes.StatusCode,
						),
					)
				}

				zipFile, err := ioutil.TempFile(
					goproxyRoot,
					"zip",
				)
				if err != nil {
					return nil, err
				}

				if _, err := io.Copy(
					zipFile,
					zipFileRes.Body,
				); err != nil {
					return nil, err
				}

				if err := zipFile.Close(); err != nil {
					return nil, err
				}

				return &modResult{
					Info:  infoFile.Name(),
					GoMod: modFile.Name(),
					Zip:   zipFile.Name(),
				}, nil
			}
		}

		if envGOPROXY != "direct" && envGOPROXY != "off" {
			return nil, fmt.Errorf(
				"mod %s %s@%s: 404 Not Found",
				operation,
				modulePath,
				moduleVersion,
			)
		}
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

	cmd := exec.Command(goBinName, args...)
	cmd.Env = make([]string, 0, len(goBinEnv)+9)
	for k, v := range goBinEnv {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Env = append(
		cmd.Env,
		fmt.Sprint("GOCACHE=", goproxyRoot),
		fmt.Sprint("GOPATH=", goproxyRoot),
		"GO111MODULE=on",
		fmt.Sprint("GOPROXY=", envGOPROXY),
		"GONOPROXY=",
		"GOSUMDB=off",
		"GONOSUMDB=",
		"GOPRIVATE=",
		fmt.Sprint("GOTMPDIR=", goproxyRoot),
	)

	cmd.Dir = goproxyRoot
	stdout, err := cmd.Output()
	if err != nil {
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
		}

		return nil, fmt.Errorf(
			"mod %s %s@%s: %s",
			operation,
			modulePath,
			moduleVersion,
			output,
		)
	}

	mr := modResult{}
	if err := json.Unmarshal(stdout, &mr); err != nil {
		return nil, err
	}

	return &mr, nil
}
