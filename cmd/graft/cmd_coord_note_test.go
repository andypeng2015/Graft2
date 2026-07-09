package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoordNoteCreateAndList_JSON(t *testing.T) {
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
	createCmd.SetArgs([]string{"--json", "note", "create", "Working thread", "--kind", "scratch", "--body", "tracking in-flight edge cases"})
	if err := createCmd.Execute(); err != nil {
		t.Fatalf("coord note create --json: %v\nraw: %s", err, createOut.String())
	}

	var createdOutput JSONCoordNoteOutput
	if err := json.Unmarshal(createOut.Bytes(), &createdOutput); err != nil {
		t.Fatalf("json.Unmarshal(create): %v\nraw: %s", err, createOut.String())
	}
	if createdOutput.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("create schemaVersion = %d, want %d", createdOutput.SchemaVersion, JSONSchemaVersion)
	}
	if createdOutput.Note == nil {
		t.Fatal("created note is nil")
	}
	created := createdOutput.Note
	if created.ID == "" {
		t.Fatal("expected created note ID")
	}
	if created.Kind != "scratch" || created.Status != "active" {
		t.Fatalf("created note = %#v", created)
	}

	var listOut bytes.Buffer
	listCmd := newCoordCmd()
	listCmd.SilenceUsage = true
	listCmd.SetOut(&listOut)
	listCmd.SetErr(io.Discard)
	listCmd.SetArgs([]string{"--json", "note", "list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("coord note list --json: %v\nraw: %s", err, listOut.String())
	}

	var notesOutput JSONCoordNotesOutput
	if err := json.Unmarshal(listOut.Bytes(), &notesOutput); err != nil {
		t.Fatalf("json.Unmarshal(list): %v\nraw: %s", err, listOut.String())
	}
	if notesOutput.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("list schemaVersion = %d, want %d", notesOutput.SchemaVersion, JSONSchemaVersion)
	}
	notes := notesOutput.Notes
	if len(notes) != 1 || notes[0].ID != created.ID {
		t.Fatalf("listed notes = %#v, want created note", notes)
	}
}

func TestCoordNoteHumanOutputUsesCommandWriter(t *testing.T) {
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
	createCmd.SetArgs([]string{"note", "create", "Working thread", "--body", "plain output"})
	if err := createCmd.Execute(); err != nil {
		t.Fatalf("coord note create: %v", err)
	}
	if !strings.Contains(createOut.String(), "Created note") || !strings.Contains(createOut.String(), "Working thread") {
		t.Fatalf("create output = %q, want created note message", createOut.String())
	}

	var listOut bytes.Buffer
	listCmd := newCoordCmd()
	listCmd.SilenceUsage = true
	listCmd.SetOut(&listOut)
	listCmd.SetErr(io.Discard)
	listCmd.SetArgs([]string{"note", "list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("coord note list: %v", err)
	}
	if !strings.Contains(listOut.String(), "Working thread") {
		t.Fatalf("list output = %q, want note title", listOut.String())
	}
}

func TestCoordNoteUpdateAndGet_JSON(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	c, _, err := openCoordinator()
	if err != nil {
		t.Fatalf("openCoordinator: %v", err)
	}
	note := &coord.Note{Title: "Scratch", Kind: "scratch", Status: "active"}
	if err := c.CreateNote(note); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	var updateOut bytes.Buffer
	updateCmd := newCoordCmd()
	updateCmd.SilenceUsage = true
	updateCmd.SetOut(&updateOut)
	updateCmd.SetErr(io.Discard)
	updateCmd.SetArgs([]string{"--json", "note", "update", note.ID, "--kind", "handoff", "--status", "paused", "--body", "handing this off"})
	if err := updateCmd.Execute(); err != nil {
		t.Fatalf("coord note update --json: %v\nraw: %s", err, updateOut.String())
	}
	var updatedOutput JSONCoordNoteOutput
	if err := json.Unmarshal(updateOut.Bytes(), &updatedOutput); err != nil {
		t.Fatalf("json.Unmarshal(update): %v\nraw: %s", err, updateOut.String())
	}
	if updatedOutput.SchemaVersion != JSONSchemaVersion || updatedOutput.Note == nil {
		t.Fatalf("updated output = %+v, want versioned note", updatedOutput)
	}

	var getOut bytes.Buffer
	getCmd := newCoordCmd()
	getCmd.SilenceUsage = true
	getCmd.SetOut(&getOut)
	getCmd.SetErr(io.Discard)
	getCmd.SetArgs([]string{"--json", "note", "get", note.ID})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("coord note get --json: %v\nraw: %s", err, getOut.String())
	}

	var gotOutput JSONCoordNoteOutput
	if err := json.Unmarshal(getOut.Bytes(), &gotOutput); err != nil {
		t.Fatalf("json.Unmarshal(get): %v\nraw: %s", err, getOut.String())
	}
	if gotOutput.SchemaVersion != JSONSchemaVersion || gotOutput.Note == nil {
		t.Fatalf("get output = %+v, want versioned note", gotOutput)
	}
	got := gotOutput.Note
	if got.Kind != "handoff" || got.Status != "paused" || got.Body != "handing this off" {
		t.Fatalf("updated note = %#v", got)
	}

	var deleteOut bytes.Buffer
	deleteCmd := newCoordCmd()
	deleteCmd.SilenceUsage = true
	deleteCmd.SetOut(&deleteOut)
	deleteCmd.SetErr(io.Discard)
	deleteCmd.SetArgs([]string{"--json", "note", "delete", note.ID})
	if err := deleteCmd.Execute(); err != nil {
		t.Fatalf("coord note delete --json: %v\nraw: %s", err, deleteOut.String())
	}
	var deleted JSONCoordNoteDeleteOutput
	if err := json.Unmarshal(deleteOut.Bytes(), &deleted); err != nil {
		t.Fatalf("json.Unmarshal(delete): %v\nraw: %s", err, deleteOut.String())
	}
	if deleted.SchemaVersion != JSONSchemaVersion || deleted.Status != "deleted" || deleted.ID != note.ID {
		t.Fatalf("delete output = %+v, want deleted %s", deleted, note.ID)
	}
}
