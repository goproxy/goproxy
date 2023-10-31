package internal

import (
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// newGoproxyCmd creates a new goproxy command.
func newGoproxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "goproxy",
		Long: strings.TrimSpace(`
A minimalist Go module proxy handler.
`),
		Version:      binaryVersion(),
		SilenceUsage: true,
	}
	cmd.SetVersionTemplate("{{.Version}}")
	cmd.SetHelpCommand(&cobra.Command{Hidden: true})
	cmd.AddCommand(newServerCmd())
	return cmd
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
