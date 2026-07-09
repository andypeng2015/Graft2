package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestWorkonRequiresRecoverForStaleSelfIdentity(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	c := coord.New(r, coord.DefaultConfig)
	oldID := createStaleWorkonSession(t, r, c, "cedar")

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newWorkonCmd()
	cmd.SilenceUsage = true
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--as", "cedar", "--json"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("workon succeeded without --recover, want recovery guidance")
	}
	if !strings.Contains(err.Error(), "graft workon --recover --as cedar") {
		t.Fatalf("error = %q, want recovery command", err.Error())
	}
	if _, err := c.GetAgent(oldID); err != nil {
		t.Fatalf("stale agent was mutated without --recover: %v", err)
	}
}

func TestWorkonRecoverReplacesStaleSelfIdentity(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	c := coord.New(r, coord.DefaultConfig)
	oldID := createStaleWorkonSession(t, r, c, "cedar")
	req := coord.ClaimRequest{
		EntityKey: "decl:function_declaration::RecoverMe:func RecoverMe():0",
		File:      "recover.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(oldID, req); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	raw := captureCommandStdout(t, func() error {
		cmd := newWorkonCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--recover", "--as", "cedar", "--json"})
		return cmd.Execute()
	})

	var result workonResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, raw)
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if result.Status != "recovered" {
		t.Fatalf("Status = %q, want recovered", result.Status)
	}
	if !result.Recovered {
		t.Fatal("Recovered = false, want true")
	}
	if result.PreviousAgentID != oldID {
		t.Fatalf("PreviousAgentID = %q, want %q", result.PreviousAgentID, oldID)
	}
	if result.AgentID == "" || result.AgentID == oldID {
		t.Fatalf("AgentID = %q, want fresh id different from %q", result.AgentID, oldID)
	}
	if result.RecoveryReason != "stale_session_and_heartbeat" {
		t.Fatalf("RecoveryReason = %q, want stale_session_and_heartbeat", result.RecoveryReason)
	}

	after := coord.New(r, coord.DefaultConfig)
	if _, err := after.GetAgent(oldID); err == nil {
		t.Fatalf("old agent %q still registered after recovery", oldID)
	}
	if _, err := after.GetAgent(result.AgentID); err != nil {
		t.Fatalf("new agent %q missing after recovery: %v", result.AgentID, err)
	}
	claim, err := after.LoadClaim(req.EntityKey)
	if err != nil {
		t.Fatalf("LoadClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("old self claim still present after recovery: %+v", claim)
	}
	session, err := coord.LoadSession(r.GraftDir, "cedar")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if session == nil || session.AgentID != result.AgentID {
		t.Fatalf("session = %+v, want recovered agent %q", session, result.AgentID)
	}
	assertWorkonAgentFile(t, r, "agent-id", result.AgentID)
	assertWorkonAgentFile(t, r, "agent-cedar", result.AgentID)
}

func TestWorkonJSONUsesCommandOutputWriter(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newWorkonCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--as", "cedar", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workon --json: %v", err)
	}

	var result workonResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, out.String())
	}
	if result.SchemaVersion != JSONSchemaVersion || result.Status != "joined" || result.AgentName != "cedar" {
		t.Fatalf("result = %+v, want versioned joined result for cedar", result)
	}
}

func TestWorkonStartWithCanceledContextDoesNotRegisterAgent(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	c := coord.New(r, coord.DefaultConfig)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = workonStartWithContext(ctx, io.Discard, c, r, "cedar", false, "all", false, "", false, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("workonStartWithContext error = %v, want context.Canceled", err)
	}
	assertNoWorkonSession(t, r, "cedar")
	assertNoWorkonAgentFile(t, r, "agent-cedar")
	assertNoWorkonAgentFile(t, r, "agent-id")

	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("agents = %+v, want none after canceled startup", agents)
	}
}

func TestWorkonStartupCleanupRemovesNewSessionState(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	c := coord.New(r, coord.DefaultConfig)

	var out bytes.Buffer
	if err := workonStart(&out, c, r, "cedar", false, "all", false, "", false, true); err != nil {
		t.Fatalf("workonStart: %v", err)
	}
	var result workonResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, out.String())
	}
	if result.AgentID == "" {
		t.Fatalf("AgentID is empty in result: %+v", result)
	}

	cleanup := workonStartupCleanup{
		c:                 c,
		r:                 r,
		name:              "cedar",
		agentID:           result.AgentID,
		cleanupNewSession: true,
	}
	cleanup.run()

	if _, err := c.GetAgent(result.AgentID); err == nil {
		t.Fatalf("agent %q still registered after startup cleanup", result.AgentID)
	}
	assertNoWorkonSession(t, r, "cedar")
	assertNoWorkonAgentFile(t, r, "agent-cedar")
	assertNoWorkonAgentFile(t, r, "agent-id")
}

func TestWorkonStartupCleanupLeavesResumedSessionState(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	c := coord.New(r, coord.DefaultConfig)

	var out bytes.Buffer
	if err := workonStart(&out, c, r, "cedar", false, "all", false, "", false, true); err != nil {
		t.Fatalf("workonStart: %v", err)
	}
	var result workonResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, out.String())
	}

	cleanup := workonStartupCleanup{
		c:                 c,
		r:                 r,
		name:              "cedar",
		agentID:           result.AgentID,
		cleanupNewSession: false,
	}
	cleanup.run()

	if _, err := c.GetAgent(result.AgentID); err != nil {
		t.Fatalf("agent %q was removed by resumed-session cleanup: %v", result.AgentID, err)
	}
	session, err := coord.LoadSession(r.GraftDir, "cedar")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if session == nil || session.AgentID != result.AgentID {
		t.Fatalf("session = %+v, want preserved agent %q", session, result.AgentID)
	}
	assertWorkonAgentFile(t, r, "agent-cedar", result.AgentID)
	assertWorkonAgentFile(t, r, "agent-id", result.AgentID)
}

func createStaleWorkonSession(t *testing.T, r *repo.Repo, c *coord.Coordinator, name string) string {
	t.Helper()

	id, err := c.RegisterAgent(coord.AgentInfo{Name: name, Workspace: filepath.Base(r.RootDir), Host: "test-host"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	agent, err := c.GetAgent(id)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	agent.HeartbeatAt = time.Now().UTC().Add(-5 * time.Minute)
	writeWorkonAgentRef(t, r, *agent)

	staleAt := time.Now().UTC().Add(-5 * time.Minute)
	if err := coord.SaveSession(r.GraftDir, &coord.Session{
		AgentID:    id,
		AgentName:  name,
		Workspace:  filepath.Base(r.RootDir),
		Host:       "test-host",
		StartedAt:  staleAt,
		LastActive: staleAt,
		PID:        1,
		Mode:       "editing",
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	assertWriteWorkonAgentFile(t, r, "agent-id", id)
	assertWriteWorkonAgentFile(t, r, "agent-"+name, id)
	return id
}

func writeWorkonAgentRef(t *testing.T, r *repo.Repo, agent coord.AgentInfo) {
	t.Helper()

	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal agent: %v", err)
	}
	h, err := r.Store.WriteBlob(&object.Blob{Data: data})
	if err != nil {
		t.Fatalf("WriteBlob agent: %v", err)
	}
	if err := r.UpdateRef("refs/coord/agents/"+agent.ID, h); err != nil {
		t.Fatalf("UpdateRef agent: %v", err)
	}
}

func assertWriteWorkonAgentFile(t *testing.T, r *repo.Repo, name, value string) {
	t.Helper()

	dir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
}

func assertNoWorkonSession(t *testing.T, r *repo.Repo, name string) {
	t.Helper()

	session, err := coord.LoadSession(r.GraftDir, name)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if session != nil {
		t.Fatalf("session = %+v, want none", session)
	}
}

func assertWorkonAgentFile(t *testing.T, r *repo.Repo, name, want string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(r.GraftDir, "coord", name))
	if err != nil {
		t.Fatalf("ReadFile %s: %v", name, err)
	}
	if got := strings.TrimSpace(string(data)); got != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}

func assertNoWorkonAgentFile(t *testing.T, r *repo.Repo, name string) {
	t.Helper()

	_, err := os.Stat(filepath.Join(r.GraftDir, "coord", name))
	if err == nil {
		t.Fatalf("%s exists, want absent", name)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat %s: %v", name, err)
	}
}
