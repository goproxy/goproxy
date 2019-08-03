package goproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// modListResult is a simplified result of
// `go list -json -m <MODULE_PATH>@<MODULE_VERSION>`.
type modListResult struct {
	Version string
}

// modListAllResult is a simplified result of
// `go list -json -m -versions <MODULE_PATH>@<MODULE_VERSION>`.
type modListAllResult struct {
	Versions []string
}

// modDownloadResult is a simplified result of
// `go mod download -json <MODULE_PATH>@<MODULE_VERSION>`.
type modDownloadResult struct {
	Info  string
	GoMod string
	Zip   string
}

// mod executes the Go modules related commands based on the type of the result.
func mod(
	workerChan chan struct{},
	goproxyRoot string,
	goBinName string,
	modulePath string,
	moduleVersion string,
	result interface{},
) error {
	if workerChan != nil {
		workerChan <- struct{}{}
		defer func() {
			<-workerChan
		}()
	}

	var operation string
	switch result.(type) {
	case *modListResult:
		operation = "list"
	case *modListAllResult:
		operation = "list all"
	case *modDownloadResult:
		operation = "download"
	default:
		return errors.New("invalid result type")
	}

	explicitDirect := globsMatchPath(os.Getenv("GONOPROXY"), modulePath) ||
		globsMatchPath(os.Getenv("GOPRIVATE"), modulePath)

OuterSwitch:
	switch envGOPROXY := os.Getenv("GOPROXY"); envGOPROXY {
	case "", "direct":
	default:
		if explicitDirect {
			break
		}

		escapedModulePath, err := module.EscapePath(modulePath)
		if err != nil {
			return err
		}

		escapedModuleVersion, err := module.EscapeVersion(moduleVersion)
		if err != nil {
			return err
		}

		for _, goproxy := range strings.Split(envGOPROXY, ",") {
			if goproxy == "direct" {
				explicitDirect = true
				break OuterSwitch
			}

			switch operation {
			case "list":
				var url string
				if moduleVersion == "latest" {
					url = fmt.Sprintf(
						"%s/%s/@latest",
						goproxy,
						escapedModulePath,
					)
				} else {
					url = fmt.Sprintf(
						"%s/%s/@v/%s.info",
						goproxy,
						escapedModulePath,
						escapedModuleVersion,
					)
				}

				res, err := http.Get(url)
				if err != nil {
					return err
				}
				defer res.Body.Close()

				switch res.StatusCode {
				case http.StatusOK:
				case http.StatusBadRequest,
					http.StatusNotFound,
					http.StatusGone:
					continue
				default:
					return fmt.Errorf(
						"mod list %s@%s: %s",
						modulePath,
						moduleVersion,
						http.StatusText(res.StatusCode),
					)
				}

				return json.NewDecoder(res.Body).Decode(result)
			case "list all":
				res, err := http.Get(fmt.Sprintf(
					"%s/%s/@v/list",
					goproxy,
					escapedModulePath,
				))
				if err != nil {
					return err
				}
				defer res.Body.Close()

				switch res.StatusCode {
				case http.StatusOK:
				case http.StatusBadRequest,
					http.StatusNotFound,
					http.StatusGone:
					continue
				default:
					return fmt.Errorf(
						"mod list all %s@%s: %s",
						modulePath,
						moduleVersion,
						http.StatusText(res.StatusCode),
					)
				}

				b, err := ioutil.ReadAll(res.Body)
				if err != nil {
					return err
				}

				mlar := result.(*modListAllResult)
				for _, b := range bytes.Split(b, []byte{'\n'}) {
					if len(b) == 0 {
						continue
					}

					mlar.Versions = append(
						mlar.Versions,
						string(b),
					)
				}

				sort.Slice(mlar.Versions, func(i, j int) bool {
					return semver.Compare(
						mlar.Versions[i],
						mlar.Versions[j],
					) < 0
				})

				return nil
			case "download":
				infoFileRes, err := http.Get(fmt.Sprintf(
					"%s/%s/@v/%s.info",
					goproxy,
					escapedModulePath,
					escapedModuleVersion,
				))
				if err != nil {
					return err
				}
				defer infoFileRes.Body.Close()

				switch infoFileRes.StatusCode {
				case http.StatusOK:
				case http.StatusBadRequest,
					http.StatusNotFound,
					http.StatusGone:
					continue
				default:
					return fmt.Errorf(
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
					return err
				}

				if _, err := io.Copy(
					infoFile,
					infoFileRes.Body,
				); err != nil {
					return err
				}

				if err := infoFile.Close(); err != nil {
					return err
				}

				modFileRes, err := http.Get(fmt.Sprintf(
					"%s/%s/@v/%s.mod",
					goproxy,
					escapedModulePath,
					escapedModuleVersion,
				))
				if err != nil {
					return err
				}
				defer modFileRes.Body.Close()

				switch modFileRes.StatusCode {
				case http.StatusOK:
				case http.StatusBadRequest,
					http.StatusNotFound,
					http.StatusGone:
					continue
				default:
					return fmt.Errorf(
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
					return err
				}

				if _, err := io.Copy(
					modFile,
					modFileRes.Body,
				); err != nil {
					return err
				}

				if err := modFile.Close(); err != nil {
					return err
				}

				zipFileRes, err := http.Get(fmt.Sprintf(
					"%s/%s/@v/%s.zip",
					goproxy,
					escapedModulePath,
					escapedModuleVersion,
				))
				if err != nil {
					return err
				}
				defer zipFileRes.Body.Close()

				switch zipFileRes.StatusCode {
				case http.StatusOK:
				case http.StatusBadRequest,
					http.StatusNotFound,
					http.StatusGone:
					continue
				default:
					return fmt.Errorf(
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
					return err
				}

				if _, err := io.Copy(
					zipFile,
					zipFileRes.Body,
				); err != nil {
					return err
				}

				if err := zipFile.Close(); err != nil {
					return err
				}

				mdr := result.(*modDownloadResult)
				mdr.Info = infoFile.Name()
				mdr.GoMod = modFile.Name()
				mdr.Zip = zipFile.Name()

				return nil
			}
		}

		return fmt.Errorf(
			"mod %s %s@%s: 404 Not Found",
			operation,
			modulePath,
			moduleVersion,
		)
	}

	var args []string
	switch operation {
	case "list":
		args = []string{
			"list",
			"-json",
			"-m",
			fmt.Sprint(modulePath, "@", moduleVersion),
		}
	case "list all":
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
	cmd.Env = append(
		os.Environ(),
		"GO111MODULE=on",
		fmt.Sprint("GOCACHE=", goproxyRoot),
		fmt.Sprint("GOPATH=", goproxyRoot),
		fmt.Sprint("GOTMPDIR=", goproxyRoot),
	)
	if explicitDirect {
		cmd.Env = append(cmd.Env, "GOPROXY=direct")
	}

	cmd.Dir = goproxyRoot
	stdout, err := cmd.Output()
	if err != nil {
		output := stdout
		if len(output) > 0 {
			m := map[string]interface{}{}
			if err := json.Unmarshal(output, &m); err != nil {
				return err
			}

			if es, ok := m["Error"].(string); ok {
				output = []byte(es)
			}
		} else if ee, ok := err.(*exec.ExitError); ok {
			output = ee.Stderr
		}

		return fmt.Errorf(
			"mod %s %s@%s: %s",
			operation,
			modulePath,
			moduleVersion,
			output,
		)
	}

	return json.Unmarshal(stdout, result)
}

// globsMatchPath reports whether any path prefix of target matches one of the
// glob patterns (as defined by the `path.Match`) in the comma-separated globs
// list. It ignores any empty or malformed patterns in the list.
func globsMatchPath(globs, target string) bool {
	for globs != "" {
		// Extract next non-empty glob in comma-separated list.
		var glob string
		if i := strings.Index(globs, ","); i >= 0 {
			glob, globs = globs[:i], globs[i+1:]
		} else {
			glob, globs = globs, ""
		}

		if glob == "" {
			continue
		}

		// A glob with N+1 path elements (N slashes) needs to be matched
		// against the first N+1 path elements of target, which end just
		// before the N+1'th slash.
		n := strings.Count(glob, "/")
		prefix := target

		// Walk target, counting slashes, truncating at the N+1'th
		// slash.
		for i := 0; i < len(target); i++ {
			if target[i] == '/' {
				if n == 0 {
					prefix = target[:i]
					break
				}

				n--
			}
		}

		if n > 0 {
			// Not enough prefix elements.
			continue
		}

		if matched, _ := path.Match(glob, prefix); matched {
			return true
		}
	}

	return false
}
