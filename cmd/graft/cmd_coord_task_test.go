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

func TestCoordTaskCommandsJSONAreVersioned(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	agentID, err := c.RegisterAgent(coord.AgentInfo{Name: "cedar", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	coordDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(coordDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(agentID), 0o644); err != nil {
		t.Fatalf("WriteFile agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var createOut bytes.Buffer
	createCmd := newCoordCmd()
	createCmd.SilenceUsage = true
	createCmd.SetOut(&createOut)
	createCmd.SetErr(io.Discard)
	createCmd.SetArgs([]string{"--json", "task", "create", "Ship contract", "--description", "stabilize task JSON", "--workspace", "graft", "--priority", "7", "--tags", "json,coord"})
	if err := createCmd.Execute(); err != nil {
		t.Fatalf("coord task create --json: %v\nraw: %s", err, createOut.String())
	}
	var created JSONCoordTaskOutput
	if err := json.Unmarshal(createOut.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal create: %v\nraw: %s", err, createOut.String())
	}
	if created.SchemaVersion != JSONSchemaVersion || created.Task == nil || created.Task.ID == "" {
		t.Fatalf("created task output = %+v, want versioned task", created)
	}
	taskID := created.Task.ID

	var listOut bytes.Buffer
	listCmd := newCoordCmd()
	listCmd.SilenceUsage = true
	listCmd.SetOut(&listOut)
	listCmd.SetErr(io.Discard)
	listCmd.SetArgs([]string{"--json", "task", "list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("coord task list --json: %v\nraw: %s", err, listOut.String())
	}
	var listed JSONCoordTasksOutput
	if err := json.Unmarshal(listOut.Bytes(), &listed); err != nil {
		t.Fatalf("json.Unmarshal list: %v\nraw: %s", err, listOut.String())
	}
	if listed.SchemaVersion != JSONSchemaVersion || len(listed.Tasks) != 1 || listed.Tasks[0].ID != taskID {
		t.Fatalf("listed tasks = %+v, want created task %s", listed, taskID)
	}

	var updateOut bytes.Buffer
	updateCmd := newCoordCmd()
	updateCmd.SilenceUsage = true
	updateCmd.SetOut(&updateOut)
	updateCmd.SetErr(io.Discard)
	updateCmd.SetArgs([]string{"--json", "task", "update", taskID, "--status", "in_progress", "--assign", "cedar"})
	if err := updateCmd.Execute(); err != nil {
		t.Fatalf("coord task update --json: %v\nraw: %s", err, updateOut.String())
	}
	var updated JSONCoordTaskOutput
	if err := json.Unmarshal(updateOut.Bytes(), &updated); err != nil {
		t.Fatalf("json.Unmarshal update: %v\nraw: %s", err, updateOut.String())
	}
	if updated.SchemaVersion != JSONSchemaVersion || updated.Task == nil || updated.Task.Status != "in_progress" {
		t.Fatalf("updated task output = %+v, want in_progress task", updated)
	}

	var getOut bytes.Buffer
	getCmd := newCoordCmd()
	getCmd.SilenceUsage = true
	getCmd.SetOut(&getOut)
	getCmd.SetErr(io.Discard)
	getCmd.SetArgs([]string{"--json", "task", "get", taskID})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("coord task get --json: %v\nraw: %s", err, getOut.String())
	}
	var got JSONCoordTaskOutput
	if err := json.Unmarshal(getOut.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal get: %v\nraw: %s", err, getOut.String())
	}
	if got.SchemaVersion != JSONSchemaVersion || got.Task == nil || got.Task.ID != taskID {
		t.Fatalf("got task output = %+v, want task %s", got, taskID)
	}

	var claimOut bytes.Buffer
	claimCmd := newCoordCmd()
	claimCmd.SilenceUsage = true
	claimCmd.SetOut(&claimOut)
	claimCmd.SetErr(io.Discard)
	claimCmd.SetArgs([]string{"--json", "task", "claim", taskID})
	if err := claimCmd.Execute(); err != nil {
		t.Fatalf("coord task claim --json: %v\nraw: %s", err, claimOut.String())
	}
	var claimed JSONCoordTaskClaimOutput
	if err := json.Unmarshal(claimOut.Bytes(), &claimed); err != nil {
		t.Fatalf("json.Unmarshal claim: %v\nraw: %s", err, claimOut.String())
	}
	if claimed.SchemaVersion != JSONSchemaVersion || claimed.Status != "claimed" || claimed.TaskID != taskID || claimed.AssignedTo != "cedar" {
		t.Fatalf("claimed task output = %+v, want cedar claim for %s", claimed, taskID)
	}

	var deleteOut bytes.Buffer
	deleteCmd := newCoordCmd()
	deleteCmd.SilenceUsage = true
	deleteCmd.SetOut(&deleteOut)
	deleteCmd.SetErr(io.Discard)
	deleteCmd.SetArgs([]string{"--json", "task", "delete", taskID})
	if err := deleteCmd.Execute(); err != nil {
		t.Fatalf("coord task delete --json: %v\nraw: %s", err, deleteOut.String())
	}
	var deleted JSONCoordTaskDeleteOutput
	if err := json.Unmarshal(deleteOut.Bytes(), &deleted); err != nil {
		t.Fatalf("json.Unmarshal delete: %v\nraw: %s", err, deleteOut.String())
	}
	if deleted.SchemaVersion != JSONSchemaVersion || deleted.Status != "deleted" || deleted.ID != taskID {
		t.Fatalf("deleted task output = %+v, want deleted %s", deleted, taskID)
	}
}

func TestCoordTaskHumanOutputUsesCommandWriter(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var createOut bytes.Buffer
	createCmd := newCoordCmd()
	createCmd.SilenceUsage = true
	createCmd.SetOut(&createOut)
	createCmd.SetErr(io.Discard)
	createCmd.SetArgs([]string{"task", "create", "Ship contract", "--priority", "4"})
	if err := createCmd.Execute(); err != nil {
		t.Fatalf("coord task create: %v", err)
	}
	if !strings.Contains(createOut.String(), "Created task") || !strings.Contains(createOut.String(), "Ship contract") {
		t.Fatalf("create output = %q, want created task message", createOut.String())
	}

	var listOut bytes.Buffer
	listCmd := newCoordCmd()
	listCmd.SilenceUsage = true
	listCmd.SetOut(&listOut)
	listCmd.SetErr(io.Discard)
	listCmd.SetArgs([]string{"task", "list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("coord task list: %v", err)
	}
	if !strings.Contains(listOut.String(), "Ship contract") {
		t.Fatalf("list output = %q, want task title", listOut.String())
	}
}
