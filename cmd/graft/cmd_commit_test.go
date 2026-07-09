package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestCommitCmdRoutesHookOutputThroughCommandWriters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go: %v", err)
	}

	hooksDir := filepath.Join(r.GraftDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("MkdirAll hooks: %v", err)
	}
	preCommitPath := filepath.Join(hooksDir, "pre-commit")
	if err := os.WriteFile(preCommitPath, []byte("#!/bin/sh\necho pre-commit-out\necho pre-commit-err >&2\n"), 0o755); err != nil {
		t.Fatalf("WriteFile pre-commit: %v", err)
	}

	analysisPath := filepath.Join(dir, "analysis-hook.sh")
	if err := os.WriteFile(analysisPath, []byte("#!/bin/sh\necho analysis-out\necho analysis-err >&2\n"), 0o755); err != nil {
		t.Fatalf("WriteFile analysis hook: %v", err)
	}
	hooksToml := "[pre-commit-analysis.capture]\nrun = " + strconv.Quote(analysisPath) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "hooks.toml"), []byte(hooksToml), 0o644); err != nil {
		t.Fatalf("WriteFile hooks.toml: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := newCommitCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"-m", "hook writer routing", "--author", "tester", "--no-sign"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("commit: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	for _, want := range []string{"pre-commit-out", "analysis-out", "hook writer routing"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	for _, want := range []string{"pre-commit-err", "analysis-err"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}
