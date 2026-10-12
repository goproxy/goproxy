package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("GOPROXY_TEST_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func TestMainProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name       string
		args       []string
		wantCode   int
		wantOutput string
	}{
		{"Version", []string{"--version"}, 0, "goproxy version "},
		{"UnknownCommand", []string{"unknown"}, 1, `unknown command "unknown" for "goproxy"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), executable, tt.args...)
			cmd.Env = append(os.Environ(), "GOPROXY_TEST_MAIN=1")
			output, err := cmd.CombinedOutput()
			if err != nil {
				if _, ok := errors.AsType[*exec.ExitError](err); !ok {
					t.Fatalf("unexpected process error %v", err)
				}
			}
			if got, want := cmd.ProcessState.ExitCode(), tt.wantCode; got != want {
				t.Errorf("got exit code %d, want %d: %s", got, want, output)
			}
			if got := string(output); !strings.Contains(got, tt.wantOutput) {
				t.Errorf("got output %q, want content %q", got, tt.wantOutput)
			}
		})
	}
}
