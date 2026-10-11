package internal

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewGoproxyCmd(t *testing.T) {
	for _, tt := range []struct {
		name       string
		args       []string
		wantOutput string
		wantErr    string
	}{
		{name: "Help", args: []string{"--help"}, wantOutput: "A minimalist Go module proxy handler."},
		{name: "Version", args: []string{"--version"}, wantOutput: "goproxy version " + Version()},
		{name: "ServerHelp", args: []string{"server", "--help"}, wantOutput: "--address"},
		{name: "UnknownCommand", args: []string{"unknown"}, wantErr: `unknown command "unknown" for "goproxy"`},
		{name: "UnknownFlag", args: []string{"--unknown"}, wantErr: "unknown flag: --unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newGoproxyCmd()
			cmd.SetArgs(tt.args)
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			err := cmd.Execute()
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("got error %v, want %q", err, tt.wantErr)
				}
				if strings.Contains(output.String(), "Usage:") {
					t.Errorf("unexpected usage in error output %q", output.String())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := output.String(); !strings.Contains(got, tt.wantOutput) {
				t.Errorf("got output %q, want content %q", got, tt.wantOutput)
			}
		})
	}
}
