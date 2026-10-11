package internal

import (
	"os"
	"runtime/debug"
	"testing"
)

func TestExecute(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want int
	}{
		{"Version", []string{"--version"}, 0},
		{"UnknownCommand", []string{"unknown"}, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			args, stdout, stderr := os.Args, os.Stdout, os.Stderr
			t.Cleanup(func() {
				os.Args, os.Stdout, os.Stderr = args, stdout, stderr
				output.Close()
			})
			os.Args = append([]string{"goproxy"}, tt.args...)
			os.Stdout, os.Stderr = output, output
			if got := Execute(); got != tt.want {
				t.Errorf("got exit code %d, want %d", got, tt.want)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	for _, tt := range []struct {
		name     string
		override string
	}{
		{"Automatic", ""},
		{"Override", "v1.2.3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := versionOverride
			t.Cleanup(func() { versionOverride = original })
			versionOverride = tt.override
			want := tt.override
			if want == "" {
				want = "(unknown)"
				if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
					want = info.Main.Version
				}
			}
			if got := Version(); got != want {
				t.Errorf("got version %q, want %q", got, want)
			}
		})
	}
}
