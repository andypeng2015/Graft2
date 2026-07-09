package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoordAnalysisCommandsJSONAreVersioned(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/coordjson\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	agentID, err := c.RegisterAgent(coord.AgentInfo{Name: "cedar", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	req := coord.ClaimRequest{
		EntityKey: "decl:function_declaration::Analyzed:func Analyzed():0",
		File:      "main.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(agentID, req); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	t.Run("impact", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "impact"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord impact --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordImpactOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal impact: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion {
			t.Fatalf("impact schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
		}
	})

	t.Run("diff", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "diff", agentID})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord diff --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordDiffOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal diff: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion || result.Agent == nil || result.Agent.ID != agentID || len(result.Claims) != 1 {
			t.Fatalf("diff result = %+v, want versioned agent diff for %s", result, agentID)
		}
	})

	t.Run("xrefs", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "xrefs", "example.com/coordjson.DoesNotExist"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord xrefs --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordXrefsOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal xrefs: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion || len(result.References) != 0 {
			t.Fatalf("xrefs result = %+v, want versioned empty references", result)
		}
	})

	t.Run("graph", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "graph"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord graph --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordGraphOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal graph: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion || len(result.Workspaces) != 0 || len(result.Edges) != 0 {
			t.Fatalf("graph result = %+v, want versioned empty graph", result)
		}
	})
}

func TestCoordAnalysisCommandsHumanOutputUsesCommandWriter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/coordhuman\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	agentID, err := c.RegisterAgent(coord.AgentInfo{Name: "cedar", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	t.Run("impact", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"impact"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord impact: %v", err)
		}
		if !strings.Contains(out.String(), "No entity changes to analyze.") {
			t.Fatalf("impact output = %q", out.String())
		}
	})

	t.Run("diff", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"diff", agentID})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord diff: %v", err)
		}
		if !strings.Contains(out.String(), "Agent: cedar") || !strings.Contains(out.String(), "No active claims.") {
			t.Fatalf("diff output = %q", out.String())
		}
	})

	t.Run("xrefs", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"xrefs", "example.com/coordhuman.DoesNotExist"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord xrefs: %v", err)
		}
		if !strings.Contains(out.String(), "No references found") {
			t.Fatalf("xrefs output = %q", out.String())
		}
	})

	t.Run("graph", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"graph"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord graph: %v", err)
		}
		if !strings.Contains(out.String(), "No workspaces configured.") {
			t.Fatalf("graph output = %q", out.String())
		}
	})
}
