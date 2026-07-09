package main

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceCommandsJSONAreVersioned(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wsDir := t.TempDir()
	absWorkspace, err := filepath.Abs(wsDir)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}

	var addOut bytes.Buffer
	addCmd := newWorkspaceCmd()
	addCmd.SilenceUsage = true
	addCmd.SetOut(&addOut)
	addCmd.SetErr(io.Discard)
	addCmd.SetArgs([]string{"--json", "add", "graft", wsDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("workspace add --json: %v\nraw: %s", err, addOut.String())
	}
	var added JSONWorkspaceMutationOutput
	if err := json.Unmarshal(addOut.Bytes(), &added); err != nil {
		t.Fatalf("json.Unmarshal add: %v\nraw: %s", err, addOut.String())
	}
	if added.SchemaVersion != JSONSchemaVersion || added.Status != "added" || added.Name != "graft" || added.Path != absWorkspace {
		t.Fatalf("added workspace = %+v, want versioned added output for %s", added, absWorkspace)
	}

	var listOut bytes.Buffer
	listCmd := newWorkspaceCmd()
	listCmd.SilenceUsage = true
	listCmd.SetOut(&listOut)
	listCmd.SetErr(io.Discard)
	listCmd.SetArgs([]string{"--json", "list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("workspace list --json: %v\nraw: %s", err, listOut.String())
	}
	var listed JSONWorkspacesOutput
	if err := json.Unmarshal(listOut.Bytes(), &listed); err != nil {
		t.Fatalf("json.Unmarshal list: %v\nraw: %s", err, listOut.String())
	}
	if listed.SchemaVersion != JSONSchemaVersion || listed.Workspaces["graft"] != absWorkspace {
		t.Fatalf("listed workspaces = %+v, want graft=%s", listed, absWorkspace)
	}

	var removeOut bytes.Buffer
	removeCmd := newWorkspaceCmd()
	removeCmd.SilenceUsage = true
	removeCmd.SetOut(&removeOut)
	removeCmd.SetErr(io.Discard)
	removeCmd.SetArgs([]string{"--json", "remove", "graft"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("workspace remove --json: %v\nraw: %s", err, removeOut.String())
	}
	var removed JSONWorkspaceMutationOutput
	if err := json.Unmarshal(removeOut.Bytes(), &removed); err != nil {
		t.Fatalf("json.Unmarshal remove: %v\nraw: %s", err, removeOut.String())
	}
	if removed.SchemaVersion != JSONSchemaVersion || removed.Status != "removed" || removed.Name != "graft" {
		t.Fatalf("removed workspace = %+v, want versioned removed output", removed)
	}
}

func TestWorkspaceHumanOutputUsesCommandWriter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wsDir := t.TempDir()

	var addOut bytes.Buffer
	addCmd := newWorkspaceCmd()
	addCmd.SilenceUsage = true
	addCmd.SetOut(&addOut)
	addCmd.SetErr(io.Discard)
	addCmd.SetArgs([]string{"add", "graft", wsDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("workspace add: %v", err)
	}
	if !strings.Contains(addOut.String(), `Workspace "graft" added`) {
		t.Fatalf("add output = %q, want added message", addOut.String())
	}

	var listOut bytes.Buffer
	listCmd := newWorkspaceCmd()
	listCmd.SilenceUsage = true
	listCmd.SetOut(&listOut)
	listCmd.SetErr(io.Discard)
	listCmd.SetArgs([]string{"list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("workspace list: %v", err)
	}
	if !strings.Contains(listOut.String(), "graft") || !strings.Contains(listOut.String(), wsDir) {
		t.Fatalf("list output = %q, want registered workspace", listOut.String())
	}

	var removeOut bytes.Buffer
	removeCmd := newWorkspaceCmd()
	removeCmd.SilenceUsage = true
	removeCmd.SetOut(&removeOut)
	removeCmd.SetErr(io.Discard)
	removeCmd.SetArgs([]string{"remove", "graft"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("workspace remove: %v", err)
	}
	if !strings.Contains(removeOut.String(), `Workspace "graft" removed`) {
		t.Fatalf("remove output = %q, want removed message", removeOut.String())
	}
}
