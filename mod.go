package goproxy

import (
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
	"path/filepath"
	"strings"

	"golang.org/x/mod/module"
)

var (
	modOutputNotFoundKeywords = [][]byte{
		[]byte("could not read username"),
		[]byte("invalid"),
		[]byte("malformed"),
		[]byte("no matching"),
		[]byte("not found"),
		[]byte("unknown"),
		[]byte("unrecognized"),
	}

	errModuleVersionNotFound = errors.New("module version not found")
)

// modListResult is the result of
// `go list -json -m -versions <MODULE_PATH>@<MODULE_VERSION>`.
type modListResult struct {
	Version  string   `json:"Version"`
	Time     string   `json:"Time"`
	Versions []string `json:"Versions,omitempty"`
}

// modList executes
// `go list -json -m -versions escapedModulePath@escapedModuleVersion`.
func modList(
	ctx context.Context,
	workerChan chan struct{},
	goBinName string,
	escapedModulePath string,
	escapedModuleVersion string,
	allVersions bool,
) (*modListResult, error) {
	modulePath, err := module.UnescapePath(escapedModulePath)
	if err != nil {
		return nil, errModuleVersionNotFound
	}

	moduleVersion, err := module.UnescapeVersion(escapedModuleVersion)
	if err != nil {
		return nil, errModuleVersionNotFound
	}

	goproxyRoot, err := ioutil.TempDir("", "goproxy")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(goproxyRoot)

	var env []string
	if globsMatchPath(os.Getenv("GONOPROXY"), modulePath) {
		env = []string{"GOPROXY=direct", "GONOPROXY="}
	}

	args := []string{"list", "-json", "-m"}
	if allVersions {
		args = append(args, "-versions")
	}

	args = append(args, fmt.Sprint(modulePath, "@", moduleVersion))

	stdout, err := executeGoCommand(
		ctx,
		workerChan,
		goBinName,
		args,
		env,
		goproxyRoot,
	)
	if err != nil {
		return nil, err
	}

	mlr := &modListResult{}
	if err := json.Unmarshal(stdout, mlr); err != nil {
		return nil, err
	}

	return mlr, nil
}

// modDownloadResult is the result of
// `go mod download -json <MODULE_PATH>@<MODULE_VERSION>`.
type modDownloadResult struct {
	Info  string `json:"Info"`
	GoMod string `json:"GoMod"`
	Zip   string `json:"Zip"`
}

// modDownload executes
// `go mod download -json escapedModulePath@escapedModuleVersion`.
func modDownload(
	ctx context.Context,
	workerChan chan struct{},
	goBinName string,
	cacher Cacher,
	escapedModulePath string,
	escapedModuleVersion string,
) (*modDownloadResult, error) {
	modulePath, err := module.UnescapePath(escapedModulePath)
	if err != nil {
		return nil, errModuleVersionNotFound
	}

	moduleVersion, err := module.UnescapeVersion(escapedModuleVersion)
	if err != nil {
		return nil, errModuleVersionNotFound
	}

	goproxyRoot, err := ioutil.TempDir("", "goproxy")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(goproxyRoot)

	var env []string
	if globsMatchPath(os.Getenv("GONOPROXY"), modulePath) {
		env = []string{"GOPROXY=direct", "GONOPROXY="}
	}

	stdout, err := executeGoCommand(
		ctx,
		workerChan,
		goBinName,
		[]string{
			"mod",
			"download",
			"-json",
			fmt.Sprint(modulePath, "@", moduleVersion),
		},
		env,
		goproxyRoot,
	)
	if err != nil {
		return nil, err
	}

	mdr := &modDownloadResult{}
	if err := json.Unmarshal(stdout, mdr); err != nil {
		return nil, err
	}

	namePrefix := path.Join(escapedModulePath, "@v", escapedModuleVersion)

	infoFile, err := os.Open(mdr.Info)
	if err != nil {
		return nil, err
	}
	defer infoFile.Close()

	infoFileInfo, err := infoFile.Stat()
	if err != nil {
		return nil, err
	}

	infoFileHash := cacher.NewHash()
	if _, err := io.Copy(infoFileHash, infoFile); err != nil {
		return nil, err
	}

	if _, err := infoFile.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	if err := cacher.SetCache(ctx, &tempCache{
		readSeeker: infoFile,
		name:       fmt.Sprint(namePrefix, ".info"),
		size:       infoFileInfo.Size(),
		modTime:    infoFileInfo.ModTime(),
		checksum:   infoFileHash.Sum(nil),
	}); err != nil {
		return nil, err
	}

	modFile, err := os.Open(mdr.GoMod)
	if err != nil {
		return nil, err
	}
	defer modFile.Close()

	modFileInfo, err := modFile.Stat()
	if err != nil {
		return nil, err
	}

	modFileHash := cacher.NewHash()
	if _, err := io.Copy(modFileHash, modFile); err != nil {
		return nil, err
	}

	if _, err := modFile.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	if err := cacher.SetCache(ctx, &tempCache{
		readSeeker: modFile,
		name:       fmt.Sprint(namePrefix, ".mod"),
		size:       modFileInfo.Size(),
		modTime:    modFileInfo.ModTime(),
		checksum:   modFileHash.Sum(nil),
	}); err != nil {
		return nil, err
	}

	zipFile, err := os.Open(mdr.Zip)
	if err != nil {
		return nil, err
	}
	defer zipFile.Close()

	zipFileInfo, err := zipFile.Stat()
	if err != nil {
		return nil, err
	}

	zipFileHash := cacher.NewHash()
	if _, err := io.Copy(zipFileHash, zipFile); err != nil {
		return nil, err
	}

	if _, err := zipFile.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	if err := cacher.SetCache(ctx, &tempCache{
		readSeeker: zipFile,
		name:       fmt.Sprint(namePrefix, ".zip"),
		size:       zipFileInfo.Size(),
		modTime:    zipFileInfo.ModTime(),
		checksum:   zipFileHash.Sum(nil),
	}); err != nil {
		return nil, err
	}

	return mdr, nil
}

// executeGoCommand executes go command with the args.
func executeGoCommand(
	ctx context.Context,
	workerChan chan struct{},
	goBinName string,
	args []string,
	env []string,
	goproxyRoot string,
) ([]byte, error) {
	workerChan <- struct{}{}
	defer func() {
		<-workerChan
	}()

	cmd := exec.CommandContext(ctx, goBinName, args...)
	cmd.Env = append(
		append(os.Environ(), env...),
		"GO111MODULE=on",
		fmt.Sprint("GOCACHE=", filepath.Join(goproxyRoot, "gocache")),
		fmt.Sprint("GOPATH=", filepath.Join(goproxyRoot, "gopath")),
	)
	cmd.Dir = goproxyRoot
	stdout, err := cmd.Output()
	if err != nil {
		output := stdout
		if ee, ok := err.(*exec.ExitError); ok {
			output = append(output, ee.Stderr...)
		}

		lowercasedOutput := bytes.ToLower(output)
		for _, k := range modOutputNotFoundKeywords {
			if bytes.Contains(lowercasedOutput, k) {
				return nil, errModuleVersionNotFound
			}
		}

		return nil, fmt.Errorf("go command: %v: %s", err, output)
	}

	return stdout, nil
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
