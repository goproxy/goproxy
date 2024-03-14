package internal

import "runtime/debug"

// Execute executes the goproxy command and returns exit code.
func Execute() int {
	if err := newGoproxyCmd().Execute(); err != nil {
		return 1
	}
	return 0
}

// Version is the version of the running binary set by the Go linker.
var Version string

// binaryVersion returns the version of the running binary.
func binaryVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if Version != "" {
		info.Main.Version = Version
	}
	version := "Version: " + info.Main.Version + "\n"
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			version += "Revision: " + setting.Value + "\n"
		case "vcs.time":
			version += "Build Time: " + setting.Value + "\n"
		}
	}
	return version
}
