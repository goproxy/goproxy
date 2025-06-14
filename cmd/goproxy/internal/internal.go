package internal

import "runtime/debug"

// Execute executes the goproxy command and returns exit code.
func Execute() int {
	if err := newGoproxyCmd().Execute(); err != nil {
		return 1
	}
	return 0
}

// VersionOverride is the version set by the Go linker to override automatic detection.
var VersionOverride string

// Version returns the version of the running binary.
func Version() string {
	if VersionOverride != "" {
		return VersionOverride
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(unknown)"
}
