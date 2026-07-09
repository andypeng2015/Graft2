package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoordPlanCommandsJSONAreVersioned(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var createOut bytes.Buffer
	createCmd := newCoordCmd()
	createCmd.SilenceUsage = true
	createCmd.SetOut(&createOut)
	createCmd.SetErr(io.Discard)
	createCmd.SetArgs([]string{"--json", "plan", "create", "Production readiness", "--description", "stabilize public JSON contracts", "--status", "active"})
	if err := createCmd.Execute(); err != nil {
		t.Fatalf("coord plan create --json: %v\nraw: %s", err, createOut.String())
	}
	var created JSONCoordPlanOutput
	if err := json.Unmarshal(createOut.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal create: %v\nraw: %s", err, createOut.String())
	}
	if created.SchemaVersion != JSONSchemaVersion || created.Plan == nil || created.Plan.ID == "" {
		t.Fatalf("created plan output = %+v, want versioned plan", created)
	}
	planID := created.Plan.ID

	var listOut bytes.Buffer
	listCmd := newCoordCmd()
	listCmd.SilenceUsage = true
	listCmd.SetOut(&listOut)
	listCmd.SetErr(io.Discard)
	listCmd.SetArgs([]string{"--json", "plan", "list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("coord plan list --json: %v\nraw: %s", err, listOut.String())
	}
	var listed JSONCoordPlansOutput
	if err := json.Unmarshal(listOut.Bytes(), &listed); err != nil {
		t.Fatalf("json.Unmarshal list: %v\nraw: %s", err, listOut.String())
	}
	if listed.SchemaVersion != JSONSchemaVersion || len(listed.Plans) != 1 || listed.Plans[0].ID != planID {
		t.Fatalf("listed plans = %+v, want created plan %s", listed, planID)
	}

	var getOut bytes.Buffer
	getCmd := newCoordCmd()
	getCmd.SilenceUsage = true
	getCmd.SetOut(&getOut)
	getCmd.SetErr(io.Discard)
	getCmd.SetArgs([]string{"--json", "plan", "get", planID})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("coord plan get --json: %v\nraw: %s", err, getOut.String())
	}
	var got JSONCoordPlanOutput
	if err := json.Unmarshal(getOut.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal get: %v\nraw: %s", err, getOut.String())
	}
	if got.SchemaVersion != JSONSchemaVersion || got.Plan == nil || got.Plan.ID != planID || got.Plan.Status != "active" {
		t.Fatalf("got plan output = %+v, want active plan %s", got, planID)
	}
}

func TestCoordPlanHumanOutputUsesCommandWriter(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var createOut bytes.Buffer
	createCmd := newCoordCmd()
	createCmd.SilenceUsage = true
	createCmd.SetOut(&createOut)
	createCmd.SetErr(io.Discard)
	createCmd.SetArgs([]string{"plan", "create", "Production readiness", "--description", "stabilize public CLI output", "--status", "active"})
	if err := createCmd.Execute(); err != nil {
		t.Fatalf("coord plan create: %v", err)
	}
	if !strings.Contains(createOut.String(), "Created plan") || !strings.Contains(createOut.String(), "Production readiness") {
		t.Fatalf("create output = %q", createOut.String())
	}

	planID := strings.Fields(createOut.String())[2]
	planID = strings.TrimSuffix(planID, ":")

	var listOut bytes.Buffer
	listCmd := newCoordCmd()
	listCmd.SilenceUsage = true
	listCmd.SetOut(&listOut)
	listCmd.SetErr(io.Discard)
	listCmd.SetArgs([]string{"plan", "list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("coord plan list: %v", err)
	}
	if !strings.Contains(listOut.String(), "Production readiness") || !strings.Contains(listOut.String(), "active") {
		t.Fatalf("list output = %q", listOut.String())
	}

	var getOut bytes.Buffer
	getCmd := newCoordCmd()
	getCmd.SilenceUsage = true
	getCmd.SetOut(&getOut)
	getCmd.SetErr(io.Discard)
	getCmd.SetArgs([]string{"plan", "get", planID})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("coord plan get: %v", err)
	}
	if !strings.Contains(getOut.String(), "Plan: Production readiness") ||
		!strings.Contains(getOut.String(), "Status:  active") ||
		!strings.Contains(getOut.String(), "Steps:   (none)") {
		t.Fatalf("get output = %q", getOut.String())
	}
}
