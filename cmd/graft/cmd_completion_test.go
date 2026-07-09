package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestCompletionCmdGeneratesSupportedShells(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{shell: "bash", want: "__start_graft"},
		{shell: "zsh", want: "#compdef graft"},
		{shell: "fish", want: "complete -c graft"},
		{shell: "powershell", want: "Register-ArgumentCompleter"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			var out bytes.Buffer
			cmd := newRootCmd()
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"completion", tt.shell})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			raw := out.String()
			if !strings.Contains(raw, tt.want) {
				t.Fatalf("%s completion missing %q", tt.shell, tt.want)
			}
		})
	}
}

func TestCompletionCmdUnsupportedShellUsesUsageExitCode(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"completion", "nu"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want usage error")
	}
	if got := commandExitCode(err); got != exitUsageError {
		t.Fatalf("exit code = %d, want %d", got, exitUsageError)
	}
}
