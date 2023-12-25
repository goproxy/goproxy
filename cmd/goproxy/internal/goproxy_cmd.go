package internal

import (
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
