package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestContextCmdJSONIsVersioned(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/context\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	source := []byte("package main\n\nfunc helper() int { return 1 }\n\nfunc target() int { return helper() }\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), source, 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newContextCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"main.go::target", "--budget", "1000", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("context --json: %v\nraw: %s", err, out.String())
	}

	var result JSONContextOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, out.String())
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if result.Target.Name != "main.go::target" {
		t.Fatalf("target name = %q, want main.go::target", result.Target.Name)
	}
	if result.Target.Body == "" {
		t.Fatalf("target body is empty")
	}
	if result.BudgetTokens != 1000 {
		t.Fatalf("budget_tokens = %d, want 1000", result.BudgetTokens)
	}
}
