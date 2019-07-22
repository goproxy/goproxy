package goproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
)

// modListResult is the result of
// `go list -json -m -versions <MODULE_PATH>@<MODULE_VERSION>`.
type modListResult struct {
	Version  string
	Time     string
	Versions []string
}

// modDownloadResult is the result of
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

	var (
		operation string
		args      []string
	)
	switch result.(type) {
	case *modListResult:
		operation = "list"
		args = []string{
			"list",
			"-json",
			"-m",
			"-versions",
			fmt.Sprint(modulePath, "@", moduleVersion),
		}
	case *modDownloadResult:
		operation = "download"
		args = []string{
			"mod",
			"download",
			"-json",
			fmt.Sprint(modulePath, "@", moduleVersion),
		}
	default:
		return errors.New("invalid result type")
	}

	cmd := exec.Command(goBinName, args...)
	cmd.Env = append(
		os.Environ(),
		"GO111MODULE=on",
		fmt.Sprint("GOCACHE=", goproxyRoot),
		fmt.Sprint("GOPATH=", goproxyRoot),
		fmt.Sprint("GOTMPDIR=", goproxyRoot),
	)
	if globsMatchPath(os.Getenv("GONOPROXY"), modulePath) {
		cmd.Env = append(cmd.Env, "GOPROXY=direct", "GONOPROXY=")
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
