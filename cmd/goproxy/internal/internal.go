package internal

import "runtime/debug"

// Execute executes the goproxy command and returns exit code.
func Execute() int {
	if err := newGoproxyCmd().Execute(); err != nil {
		return 1
	}
	return 0
}

// binaryVersion returns the version of the running binary.
func binaryVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
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
