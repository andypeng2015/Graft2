package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestGrepWarningUsesCommandErrorWriter(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\n// no_such_literal\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := newGrepCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"no_such_literal"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("grep: %v", err)
	}

	if !strings.Contains(errOut.String(), "warning: no structural matches, falling back to line grep") {
		t.Fatalf("stderr = %q, want structural fallback warning", errOut.String())
	}
	if !strings.Contains(out.String(), "main.go:3:// no_such_literal") {
		t.Fatalf("stdout = %q, want fallback line grep result", out.String())
	}
}

func TestGrepCmdUsesCommandContext(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := newGrepCmd()
	cmd.SilenceUsage = true
	cmd.SetContext(ctx)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"func $NAME()"})
	err = cmd.Execute()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("grep error = %v, want context.Canceled\nstderr: %s\nstdout: %s", err, errOut.String(), out.String())
	}
}

func TestGrepJSONOutputsAreVersioned(t *testing.T) {
	t.Run("line", func(t *testing.T) {
		dir := t.TempDir()
		r, err := repo.Init(dir)
		if err != nil {
			t.Fatalf("repo.Init: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello world\n"), 0o644); err != nil {
			t.Fatalf("WriteFile notes.txt: %v", err)
		}
		if err := r.Add([]string{"notes.txt"}); err != nil {
			t.Fatalf("Add notes.txt: %v", err)
		}

		raw := executeGrepCommand(t, dir, "--line", "--json", "hello")

		var result JSONLineGrepOutput
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("json.Unmarshal: %v\nraw: %s", err, string(raw))
		}
		if result.SchemaVersion != JSONSchemaVersion {
			t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
		}
		if len(result.Results) != 1 || result.Results[0].Path != "notes.txt" {
			t.Fatalf("unexpected results: %+v", result.Results)
		}
	})

	t.Run("structural", func(t *testing.T) {
		dir := t.TempDir()
		r, err := repo.Init(dir)
		if err != nil {
			t.Fatalf("repo.Init: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc Hello(name string) string { return name }\n"), 0o644); err != nil {
			t.Fatalf("WriteFile main.go: %v", err)
		}
		if err := r.Add([]string{"main.go"}); err != nil {
			t.Fatalf("Add main.go: %v", err)
		}

		raw := executeGrepCommand(t, dir, "--json", "func $NAME($$$PARAMS) string")

		var result JSONStructuralGrepOutput
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("json.Unmarshal: %v\nraw: %s", err, string(raw))
		}
		if result.SchemaVersion != JSONSchemaVersion {
			t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
		}
		if len(result.Results) != 1 || result.Results[0].Path != "main.go" {
			t.Fatalf("unexpected results: %+v", result.Results)
		}
	})

	t.Run("history", func(t *testing.T) {
		dir := t.TempDir()
		r, err := repo.Init(dir)
		if err != nil {
			t.Fatalf("repo.Init: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc Hello(name string) string { return name }\n"), 0o644); err != nil {
			t.Fatalf("WriteFile main.go: %v", err)
		}
		if err := r.Add([]string{"main.go"}); err != nil {
			t.Fatalf("Add main.go: %v", err)
		}
		if _, err := r.Commit("initial", "alice"); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		raw := executeGrepCommand(t, dir, "--history", "--json", "--max-commits", "5", "func $NAME($$$PARAMS) string")

		var result JSONHistoryGrepOutput
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("json.Unmarshal: %v\nraw: %s", err, string(raw))
		}
		if result.SchemaVersion != JSONSchemaVersion {
			t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
		}
		if len(result.Results) != 1 || result.Results[0].Path != "main.go" {
			t.Fatalf("unexpected results: %+v", result.Results)
		}
	})
}

func executeGrepCommand(t *testing.T, dir string, args ...string) []byte {
	t.Helper()

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := newGrepCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("grep %v: %v\nstderr: %s\nstdout: %s", args, err, errOut.String(), out.String())
	}
	return append([]byte(nil), out.Bytes()...)
}
