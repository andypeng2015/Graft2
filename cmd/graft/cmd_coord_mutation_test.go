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

func TestCoordMutationCommandsJSONAreVersioned(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	activeID, err := c.RegisterAgent(coord.AgentInfo{Name: "cedar", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent active: %v", err)
	}
	targetID, err := c.RegisterAgent(coord.AgentInfo{Name: "maple", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent target: %v", err)
	}
	coordDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(coordDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(activeID), 0o644); err != nil {
		t.Fatalf("WriteFile agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	watchEntity := "decl:function_declaration::WatchMe:func WatchMe():0"
	var watchOut bytes.Buffer
	watchCmd := newCoordCmd()
	watchCmd.SilenceUsage = true
	watchCmd.SetOut(&watchOut)
	watchCmd.SetErr(io.Discard)
	watchCmd.SetArgs([]string{"--json", "watch", watchEntity, "--file", "main.go"})
	if err := watchCmd.Execute(); err != nil {
		t.Fatalf("coord watch --json: %v\nraw: %s", err, watchOut.String())
	}
	var watched JSONCoordWatchOutput
	if err := json.Unmarshal(watchOut.Bytes(), &watched); err != nil {
		t.Fatalf("json.Unmarshal watch: %v\nraw: %s", err, watchOut.String())
	}
	if watched.SchemaVersion != JSONSchemaVersion || watched.Status != "watching" || watched.EntityKey != watchEntity || watched.File != "main.go" {
		t.Fatalf("watch output = %+v, want versioned watching output", watched)
	}

	var unwatchOut bytes.Buffer
	unwatchCmd := newCoordCmd()
	unwatchCmd.SilenceUsage = true
	unwatchCmd.SetOut(&unwatchOut)
	unwatchCmd.SetErr(io.Discard)
	unwatchCmd.SetArgs([]string{"--json", "unwatch", watchEntity})
	if err := unwatchCmd.Execute(); err != nil {
		t.Fatalf("coord unwatch --json: %v\nraw: %s", err, unwatchOut.String())
	}
	var unwatched JSONCoordUnwatchOutput
	if err := json.Unmarshal(unwatchOut.Bytes(), &unwatched); err != nil {
		t.Fatalf("json.Unmarshal unwatch: %v\nraw: %s", err, unwatchOut.String())
	}
	if unwatched.SchemaVersion != JSONSchemaVersion || unwatched.Status != "unwatched" || unwatched.EntityKey != watchEntity {
		t.Fatalf("unwatch output = %+v, want versioned unwatched output", unwatched)
	}

	transferEntity := "decl:function_declaration::TransferMe:func TransferMe():0"
	if err := c.AcquireClaim(activeID, coord.ClaimRequest{EntityKey: transferEntity, File: "main.go", Mode: coord.ClaimEditing}); err != nil {
		t.Fatalf("AcquireClaim transfer: %v", err)
	}
	transferHash := coord.EntityKeyHash(transferEntity)
	var transferOut bytes.Buffer
	transferCmd := newCoordCmd()
	transferCmd.SilenceUsage = true
	transferCmd.SetOut(&transferOut)
	transferCmd.SetErr(io.Discard)
	transferCmd.SetArgs([]string{"--json", "resolve", transferHash, "--transfer", targetID})
	if err := transferCmd.Execute(); err != nil {
		t.Fatalf("coord resolve --transfer --json: %v\nraw: %s", err, transferOut.String())
	}
	var transferred JSONCoordResolveOutput
	if err := json.Unmarshal(transferOut.Bytes(), &transferred); err != nil {
		t.Fatalf("json.Unmarshal transfer: %v\nraw: %s", err, transferOut.String())
	}
	if transferred.SchemaVersion != JSONSchemaVersion || transferred.Status != "transferred" || transferred.KeyHash != transferHash || transferred.ToAgent != targetID {
		t.Fatalf("transfer output = %+v, want transfer to %s", transferred, targetID)
	}

	releaseEntity := "decl:function_declaration::ReleaseMe:func ReleaseMe():0"
	if err := c.AcquireClaim(activeID, coord.ClaimRequest{EntityKey: releaseEntity, File: "main.go", Mode: coord.ClaimEditing}); err != nil {
		t.Fatalf("AcquireClaim release: %v", err)
	}
	releaseHash := coord.EntityKeyHash(releaseEntity)
	var releaseOut bytes.Buffer
	releaseCmd := newCoordCmd()
	releaseCmd.SilenceUsage = true
	releaseCmd.SetOut(&releaseOut)
	releaseCmd.SetErr(io.Discard)
	releaseCmd.SetArgs([]string{"--json", "resolve", releaseHash})
	if err := releaseCmd.Execute(); err != nil {
		t.Fatalf("coord resolve --json: %v\nraw: %s", err, releaseOut.String())
	}
	var released JSONCoordResolveOutput
	if err := json.Unmarshal(releaseOut.Bytes(), &released); err != nil {
		t.Fatalf("json.Unmarshal release: %v\nraw: %s", err, releaseOut.String())
	}
	if released.SchemaVersion != JSONSchemaVersion || released.Status != "released" || released.KeyHash != releaseHash {
		t.Fatalf("release output = %+v, want released %s", released, releaseHash)
	}

	var publishOut bytes.Buffer
	publishCmd := newCoordCmd()
	publishCmd.SilenceUsage = true
	publishCmd.SetOut(&publishOut)
	publishCmd.SetErr(io.Discard)
	publishCmd.SetArgs([]string{"--json", "publish"})
	if err := publishCmd.Execute(); err != nil {
		t.Fatalf("coord publish --json: %v\nraw: %s", err, publishOut.String())
	}
	var published JSONCoordPublishOutput
	if err := json.Unmarshal(publishOut.Bytes(), &published); err != nil {
		t.Fatalf("json.Unmarshal publish: %v\nraw: %s", err, publishOut.String())
	}
	if published.SchemaVersion != JSONSchemaVersion || published.Status != "published" || published.CommitHash != string(commitHash) || published.AgentID != activeID {
		t.Fatalf("publish output = %+v, want published commit %s", published, commitHash)
	}
}

func TestCoordMutationCommandsHumanOutputUsesCommandWriter(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	activeID, err := c.RegisterAgent(coord.AgentInfo{Name: "cedar", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent active: %v", err)
	}
	coordDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(coordDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(activeID), 0o644); err != nil {
		t.Fatalf("WriteFile agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	watchEntity := "decl:function_declaration::WatchHuman:func WatchHuman():0"
	var watchOut bytes.Buffer
	watchCmd := newCoordCmd()
	watchCmd.SilenceUsage = true
	watchCmd.SetOut(&watchOut)
	watchCmd.SetErr(io.Discard)
	watchCmd.SetArgs([]string{"watch", watchEntity, "--file", "main.go"})
	if err := watchCmd.Execute(); err != nil {
		t.Fatalf("coord watch: %v", err)
	}
	if !strings.Contains(watchOut.String(), "Watching: "+watchEntity) {
		t.Fatalf("watch output = %q", watchOut.String())
	}

	var unwatchOut bytes.Buffer
	unwatchCmd := newCoordCmd()
	unwatchCmd.SilenceUsage = true
	unwatchCmd.SetOut(&unwatchOut)
	unwatchCmd.SetErr(io.Discard)
	unwatchCmd.SetArgs([]string{"unwatch", watchEntity})
	if err := unwatchCmd.Execute(); err != nil {
		t.Fatalf("coord unwatch: %v", err)
	}
	if !strings.Contains(unwatchOut.String(), "Stopped watching: "+watchEntity) {
		t.Fatalf("unwatch output = %q", unwatchOut.String())
	}

	releaseEntity := "decl:function_declaration::ReleaseHuman:func ReleaseHuman():0"
	if err := c.AcquireClaim(activeID, coord.ClaimRequest{EntityKey: releaseEntity, File: "main.go", Mode: coord.ClaimEditing}); err != nil {
		t.Fatalf("AcquireClaim release: %v", err)
	}
	releaseHash := coord.EntityKeyHash(releaseEntity)
	var releaseOut bytes.Buffer
	releaseCmd := newCoordCmd()
	releaseCmd.SilenceUsage = true
	releaseCmd.SetOut(&releaseOut)
	releaseCmd.SetErr(io.Discard)
	releaseCmd.SetArgs([]string{"resolve", releaseHash})
	if err := releaseCmd.Execute(); err != nil {
		t.Fatalf("coord resolve: %v", err)
	}
	if !strings.Contains(releaseOut.String(), "Claim "+releaseHash+" released") {
		t.Fatalf("resolve output = %q", releaseOut.String())
	}

	var heartbeatOut bytes.Buffer
	heartbeatCmd := newCoordCmd()
	heartbeatCmd.SilenceUsage = true
	heartbeatCmd.SetOut(&heartbeatOut)
	heartbeatCmd.SetErr(io.Discard)
	heartbeatCmd.SetArgs([]string{"heartbeat"})
	if err := heartbeatCmd.Execute(); err != nil {
		t.Fatalf("coord heartbeat: %v", err)
	}
	if !strings.Contains(heartbeatOut.String(), "Heartbeat updated for agent "+activeID) {
		t.Fatalf("heartbeat output = %q", heartbeatOut.String())
	}

	var sessionsOut bytes.Buffer
	sessionsCmd := newCoordCmd()
	sessionsCmd.SilenceUsage = true
	sessionsCmd.SetOut(&sessionsOut)
	sessionsCmd.SetErr(io.Discard)
	sessionsCmd.SetArgs([]string{"sessions"})
	if err := sessionsCmd.Execute(); err != nil {
		t.Fatalf("coord sessions: %v", err)
	}
	if !strings.Contains(sessionsOut.String(), "No active sessions.") {
		t.Fatalf("sessions output = %q", sessionsOut.String())
	}

	var readingOut bytes.Buffer
	readingCmd := newCoordCmd()
	readingCmd.SilenceUsage = true
	readingCmd.SetOut(&readingOut)
	readingCmd.SetErr(io.Discard)
	readingCmd.SetArgs([]string{"reading", "main.go", "--entity", "main"})
	if err := readingCmd.Execute(); err != nil {
		t.Fatalf("coord reading: %v", err)
	}
	if !strings.Contains(readingOut.String(), "Reading: main.go (entity: main)") {
		t.Fatalf("reading output = %q", readingOut.String())
	}

	var presenceOut bytes.Buffer
	presenceCmd := newCoordCmd()
	presenceCmd.SilenceUsage = true
	presenceCmd.SetOut(&presenceOut)
	presenceCmd.SetErr(io.Discard)
	presenceCmd.SetArgs([]string{"presence"})
	if err := presenceCmd.Execute(); err != nil {
		t.Fatalf("coord presence: %v", err)
	}
	if !strings.Contains(presenceOut.String(), "main.go") || !strings.Contains(presenceOut.String(), "main") {
		t.Fatalf("presence output = %q", presenceOut.String())
	}

	var publishOut bytes.Buffer
	publishCmd := newCoordCmd()
	publishCmd.SilenceUsage = true
	publishCmd.SetOut(&publishOut)
	publishCmd.SetErr(io.Discard)
	publishCmd.SetArgs([]string{"publish"})
	if err := publishCmd.Execute(); err != nil {
		t.Fatalf("coord publish: %v", err)
	}
	if !strings.Contains(publishOut.String(), "Published feed event for commit "+string(commitHash)[:12]) {
		t.Fatalf("publish output = %q", publishOut.String())
	}
}
