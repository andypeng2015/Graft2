package main

import (
	"bytes"
	"encoding/json"
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

func TestCoordDecisionsCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	if err := coord.SaveDecision(r.GraftDir, &coord.DecisionGraph{
		ID:        "decision-1",
		Version:   1,
		Kind:      "claim_decision",
		Source:    "graft add",
		CreatedAt: time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC),
		AgentID:   "agent-1",
		EntityKey: "decl:function_definition::Foo:func Foo():0",
		File:      "foo.go",
		Action:    "Allow",
		Rule:      "DefaultAllow",
		Outcome: coord.DecisionOutcome{
			Status:        "claim_acquired",
			ClaimAcquired: true,
		},
	}); err != nil {
		t.Fatalf("coord.SaveDecision: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newCoordCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"decisions", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("coord decisions --json: %v\nraw: %s", err, out.String())
	}

	var result JSONCoordDecisionsOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, out.String())
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(result.Decisions))
	}
	if result.Decisions[0].Outcome.Status != "claim_acquired" {
		t.Fatalf("decisions[0].Outcome.Status = %q, want claim_acquired", result.Decisions[0].Outcome.Status)
	}
	if result.Decisions[0].Rule != "DefaultAllow" {
		t.Fatalf("decisions[0].Rule = %q, want DefaultAllow", result.Decisions[0].Rule)
	}
}

func TestCoordListCommandsJSONAreVersioned(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	agentID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	req := coord.ClaimRequest{
		EntityKey: "decl:function_definition::CoordJSON:func CoordJSON():0",
		File:      "coord_json.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(agentID, req); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}
	if err := c.AppendFeed(coord.FeedEvent{
		Event:     "entity_changed",
		AgentID:   agentID,
		AgentName: "agent-a",
		Entities:  []coord.EntityChange{{Key: req.EntityKey, File: req.File, Change: "body_changed"}},
	}); err != nil {
		t.Fatalf("AppendFeed: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	t.Run("status", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordStatusOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal status: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion {
			t.Fatalf("status schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
		}
		if result.Agents != 1 || result.Claims != 1 || result.FeedCount == 0 {
			t.Fatalf("status result = %+v, want one agent/claim and feed events", result)
		}
	})

	t.Run("agents", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "agents"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord agents --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordAgentsOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal agents: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion {
			t.Fatalf("agents schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
		}
		if len(result.Agents) != 1 || result.Agents[0].ID != agentID {
			t.Fatalf("agents = %+v, want %s", result.Agents, agentID)
		}
	})

	t.Run("claims", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "claims"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord claims --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordClaimsOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal claims: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion {
			t.Fatalf("claims schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
		}
		if len(result.Claims) != 1 || result.Claims[0].EntityKey != req.EntityKey || result.Claims[0].Agent != agentID {
			t.Fatalf("claims = %+v, want claim for %s by %s", result.Claims, req.EntityKey, agentID)
		}
	})

	t.Run("feed", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "feed"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord feed --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordFeedOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal feed: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion {
			t.Fatalf("feed schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
		}
		if len(result.Events) == 0 {
			t.Fatal("feed events empty, want at least one event")
		}
		if result.Events[0].FeedHash == "" {
			t.Fatalf("feed event missing feed_hash: %+v", result.Events[0])
		}
	})
}

func TestCoordListCommandsHumanOutputUsesCommandWriter(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	agentID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	req := coord.ClaimRequest{
		EntityKey: "decl:function_definition::CoordHuman:func CoordHuman():0",
		File:      "coord_human.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(agentID, req); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}
	if err := c.AppendFeed(coord.FeedEvent{
		Event:     "entity_changed",
		AgentID:   agentID,
		AgentName: "agent-a",
		Entities:  []coord.EntityChange{{Key: req.EntityKey, File: req.File, Change: "body_changed"}},
	}); err != nil {
		t.Fatalf("AppendFeed: %v", err)
	}
	if err := coord.SaveDecision(r.GraftDir, &coord.DecisionGraph{
		ID:        "decision-human-1",
		Version:   1,
		Kind:      "claim_decision",
		Source:    "graft coord check",
		CreatedAt: time.Date(2026, 3, 18, 11, 0, 0, 0, time.UTC),
		AgentID:   agentID,
		EntityKey: req.EntityKey,
		File:      req.File,
		Action:    "Allow",
		Rule:      "DefaultAllow",
		Outcome: coord.DecisionOutcome{
			Status:        "claim_acquired",
			ClaimAcquired: true,
		},
	}); err != nil {
		t.Fatalf("coord.SaveDecision: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	t.Run("agents", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"agents"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord agents: %v", err)
		}
		if !strings.Contains(out.String(), "agent-a") || !strings.Contains(out.String(), agentID) {
			t.Fatalf("agents output = %q", out.String())
		}
	})

	t.Run("claims", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"claims"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord claims: %v", err)
		}
		if !strings.Contains(out.String(), req.EntityKey) || !strings.Contains(out.String(), "agent-a") {
			t.Fatalf("claims output = %q", out.String())
		}
	})

	t.Run("decisions", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"decisions"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord decisions: %v", err)
		}
		if !strings.Contains(out.String(), "DefaultAllow") || !strings.Contains(out.String(), "claim_acquired") {
			t.Fatalf("decisions output = %q", out.String())
		}
	})

	t.Run("feed", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"feed"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord feed: %v", err)
		}
		if !strings.Contains(out.String(), "entity_changed") || !strings.Contains(out.String(), "agent-a") {
			t.Fatalf("feed output = %q", out.String())
		}
	})
}

func TestCoordCheck_RecordsDecisionTrace(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	activeID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent active: %v", err)
	}
	otherID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-b", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent other: %v", err)
	}

	req := coord.ClaimRequest{
		EntityKey: "decl:function_definition::Foo:func Foo():0",
		File:      "foo.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(otherID, req); err != nil {
		t.Fatalf("AcquireClaim other: %v", err)
	}

	coordDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(coordDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coord dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(activeID), 0o644); err != nil {
		t.Fatalf("WriteFile agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var longThresholdOut bytes.Buffer
	longThresholdCmd := newCoordCmd()
	longThresholdCmd.SilenceUsage = true
	longThresholdCmd.SetOut(&longThresholdOut)
	longThresholdCmd.SetErr(io.Discard)
	longThresholdCmd.SetArgs([]string{"check", "--json", "--stale-after", "1h"})
	if err := longThresholdCmd.Execute(); err != nil {
		t.Fatalf("coord check --stale-after: %v\nraw: %s", err, longThresholdOut.String())
	}
	var longThreshold JSONCoordCheckOutput
	if err := json.Unmarshal(longThresholdOut.Bytes(), &longThreshold); err != nil {
		t.Fatalf("json.Unmarshal long threshold: %v\nraw: %s", err, longThresholdOut.String())
	}
	if len(longThreshold.StaleAgents) != 0 {
		t.Fatalf("long-threshold stale_agents = %+v, want none", longThreshold.StaleAgents)
	}
	if len(longThreshold.ActiveClaims) != 1 || longThreshold.ActiveClaims[0].Stale {
		t.Fatalf("long-threshold active_claims = %+v, want non-stale claim", longThreshold.ActiveClaims)
	}

	var out bytes.Buffer
	cmd := newCoordCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("coord check --json: %v\nraw: %s", err, out.String())
	}

	var result JSONCoordCheckOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal check output: %v\nraw: %s", err, out.String())
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if result.ActiveAgentID != activeID {
		t.Fatalf("active_agent_id = %q, want %q", result.ActiveAgentID, activeID)
	}
	if result.ClaimsExamined != 1 {
		t.Fatalf("claims_examined = %d, want 1", result.ClaimsExamined)
	}
	if result.OK {
		t.Fatal("expected conflict result")
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("len(conflicts) = %d, want 1", len(result.Conflicts))
	}
	if result.Conflicts[0].EntityKey != req.EntityKey {
		t.Fatalf("conflict entity_key = %q, want %q", result.Conflicts[0].EntityKey, req.EntityKey)
	}
	if result.Conflicts[0].Decision == "" {
		t.Fatalf("conflict decision missing: %+v", result.Conflicts[0])
	}

	decisions, err := coord.ListDecisions(r.GraftDir, 10)
	if err != nil {
		t.Fatalf("coord.ListDecisions: %v", err)
	}
	if len(decisions) == 0 {
		t.Fatal("expected recorded decision trace")
	}
	if decisions[0].Source != "graft coord check" {
		t.Fatalf("decisions[0].Source = %q, want graft coord check", decisions[0].Source)
	}
	if decisions[0].Outcome.Status != "inspection_reported" {
		t.Fatalf("decisions[0].Outcome.Status = %q, want inspection_reported", decisions[0].Outcome.Status)
	}
}

func TestCoordCheckJSONIncludesPreMutationSummary(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	activeID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent active: %v", err)
	}
	otherID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-b", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent other: %v", err)
	}

	markCoordAgentHeartbeat(t, r, c, otherID, time.Now().UTC().Add(-10*time.Minute))

	req := coord.ClaimRequest{
		EntityKey: "decl:function_definition::Foo:func Foo():0",
		File:      "foo.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(otherID, req); err != nil {
		t.Fatalf("AcquireClaim other: %v", err)
	}

	currentFeed, err := c.WalkFeed("", 10)
	if err != nil {
		t.Fatalf("WalkFeed: %v", err)
	}
	if len(currentFeed) == 0 {
		t.Fatal("expected feed events before saving cursor")
	}
	if err := c.SaveCursor(activeID, currentFeed[0].FeedHash); err != nil {
		t.Fatalf("SaveCursor: %v", err)
	}
	if err := c.AppendFeed(coord.FeedEvent{
		Event:     "entity_changed",
		AgentID:   otherID,
		AgentName: "agent-b",
		Entities:  []coord.EntityChange{{Key: req.EntityKey, File: req.File, Change: "body_changed"}},
	}); err != nil {
		t.Fatalf("AppendFeed: %v", err)
	}

	coordDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(coordDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coord dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(activeID), 0o644); err != nil {
		t.Fatalf("WriteFile agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newCoordCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("coord check --json: %v\nraw: %s", err, out.String())
	}

	var result JSONCoordCheckOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal check output: %v\nraw: %s", err, out.String())
	}
	if result.AgentsExamined != 2 {
		t.Fatalf("agents_examined = %d, want 2", result.AgentsExamined)
	}
	if len(result.ActiveClaims) != 1 {
		t.Fatalf("active_claims len = %d, want 1: %+v", len(result.ActiveClaims), result.ActiveClaims)
	}
	if !result.ActiveClaims[0].Stale {
		t.Fatalf("active claim stale = false, want true: %+v", result.ActiveClaims[0])
	}
	if len(result.StaleAgents) != 1 || result.StaleAgents[0].ID != otherID {
		t.Fatalf("stale_agents = %+v, want %s", result.StaleAgents, otherID)
	}
	if len(result.UnreadFeedEvents) != 1 {
		t.Fatalf("unread_feed_events len = %d, want 1: %+v", len(result.UnreadFeedEvents), result.UnreadFeedEvents)
	}
	if result.UnreadFeedEvents[0].Event != "entity_changed" {
		t.Fatalf("unread event = %+v, want entity_changed", result.UnreadFeedEvents[0])
	}
	if len(result.UnreadFeedEvents[0].Files) != 1 || result.UnreadFeedEvents[0].Files[0] != req.File {
		t.Fatalf("unread event files = %+v, want [%s]", result.UnreadFeedEvents[0].Files, req.File)
	}
}

func TestCoordCleanupStaleDryRunAndRemove(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	activeID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent active: %v", err)
	}
	staleID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-b", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent stale: %v", err)
	}
	markCoordAgentHeartbeat(t, r, c, staleID, time.Now().UTC().Add(-10*time.Minute))

	req := coord.ClaimRequest{
		EntityKey: "decl:function_definition::CleanupMe:func CleanupMe():0",
		File:      "cleanup.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(staleID, req); err != nil {
		t.Fatalf("AcquireClaim stale: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	longThresholdRaw := captureCommandStdout(t, func() error {
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "cleanup-stale", "--dry-run", "--stale-after", "1h"})
		return cmd.Execute()
	})
	var longThreshold JSONCoordCleanupStaleOutput
	if err := json.Unmarshal([]byte(longThresholdRaw), &longThreshold); err != nil {
		t.Fatalf("json.Unmarshal long-threshold dry-run: %v\nraw: %s", err, longThresholdRaw)
	}
	if len(longThreshold.StaleAgents) != 0 || longThreshold.Removed != 0 {
		t.Fatalf("long-threshold cleanup result = %+v, want no stale candidates", longThreshold)
	}

	dryRunRaw := captureCommandStdout(t, func() error {
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "cleanup-stale", "--dry-run"})
		return cmd.Execute()
	})
	var dryRun JSONCoordCleanupStaleOutput
	if err := json.Unmarshal([]byte(dryRunRaw), &dryRun); err != nil {
		t.Fatalf("json.Unmarshal dry-run: %v\nraw: %s", err, dryRunRaw)
	}
	if dryRun.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("dry-run schemaVersion = %d, want %d", dryRun.SchemaVersion, JSONSchemaVersion)
	}
	if !dryRun.DryRun || dryRun.Removed != 0 || len(dryRun.StaleAgents) != 1 || dryRun.StaleAgents[0].ID != staleID {
		t.Fatalf("dry-run result = %+v, want one stale candidate and no removal", dryRun)
	}
	if _, err := c.GetAgent(staleID); err != nil {
		t.Fatalf("stale agent was removed during dry-run: %v", err)
	}
	if claim, err := c.LoadClaim(req.EntityKey); err != nil || claim == nil {
		t.Fatalf("stale claim after dry-run = %+v, err=%v", claim, err)
	}

	removeRaw := captureCommandStdout(t, func() error {
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "cleanup-stale"})
		return cmd.Execute()
	})
	var removed JSONCoordCleanupStaleOutput
	if err := json.Unmarshal([]byte(removeRaw), &removed); err != nil {
		t.Fatalf("json.Unmarshal remove: %v\nraw: %s", err, removeRaw)
	}
	if removed.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("remove schemaVersion = %d, want %d", removed.SchemaVersion, JSONSchemaVersion)
	}
	if removed.DryRun || removed.Removed != 1 || len(removed.StaleAgents) != 1 || removed.StaleAgents[0].ID != staleID {
		t.Fatalf("remove result = %+v, want one removed stale agent", removed)
	}
	if _, err := c.GetAgent(activeID); err != nil {
		t.Fatalf("active agent missing after cleanup: %v", err)
	}
	if _, err := c.GetAgent(staleID); err == nil {
		t.Fatalf("stale agent %q still registered after cleanup", staleID)
	}
	claim, err := c.LoadClaim(req.EntityKey)
	if err != nil {
		t.Fatalf("LoadClaim after cleanup: %v", err)
	}
	if claim != nil {
		t.Fatalf("stale claim still present after cleanup: %+v", claim)
	}
}

func TestCoordHeartbeatTouchesPersistentSession(t *testing.T) {
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
	oldActive := time.Now().UTC().Add(-10 * time.Minute)
	if err := coord.SaveSession(r.GraftDir, &coord.Session{
		AgentID:    agentID,
		AgentName:  "cedar",
		Workspace:  "graft",
		Host:       "host-a",
		StartedAt:  oldActive,
		LastActive: oldActive,
		PID:        1234,
		Scope:      "./pkg/...",
		Mode:       "watching",
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
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

	var out bytes.Buffer
	cmd := newCoordCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "heartbeat"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("coord heartbeat --json: %v\nraw: %s", err, out.String())
	}
	var heartbeat JSONCoordHeartbeatOutput
	if err := json.Unmarshal(out.Bytes(), &heartbeat); err != nil {
		t.Fatalf("json.Unmarshal heartbeat: %v\nraw: %s", err, out.String())
	}
	if heartbeat.SchemaVersion != JSONSchemaVersion || heartbeat.Status != "ok" || heartbeat.AgentID != agentID {
		t.Fatalf("heartbeat = %+v, want versioned ok for %s", heartbeat, agentID)
	}

	session, err := coord.LoadSession(r.GraftDir, "cedar")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if session == nil {
		t.Fatal("session missing after heartbeat")
	}
	if !session.LastActive.After(oldActive) {
		t.Fatalf("LastActive = %s, want after %s", session.LastActive, oldActive)
	}
	if session.Workspace != "graft" || session.Host != "host-a" || session.Scope != "./pkg/..." || session.Mode != "watching" {
		t.Fatalf("session metadata changed after heartbeat: %+v", session)
	}
}

func TestCoordAgentActivityCommandsJSONAreVersioned(t *testing.T) {
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
	if err := coord.SaveSession(r.GraftDir, &coord.Session{
		AgentID:    agentID,
		AgentName:  "cedar",
		Workspace:  "graft",
		Host:       "host-a",
		StartedAt:  time.Now().UTC(),
		LastActive: time.Now().UTC(),
		Mode:       "editing",
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
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

	t.Run("sessions", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "sessions"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord sessions --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordSessionsOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal sessions: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion || len(result.Sessions) != 1 || result.Sessions[0].AgentID != agentID {
			t.Fatalf("sessions = %+v, want versioned session for %s", result, agentID)
		}
	})

	t.Run("reading", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "reading", "pkg/coord.go", "--entity", "decl:function_declaration::Run:func Run():0"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord reading --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordReadingOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal reading: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion || result.Status != "reading" || result.File != "pkg/coord.go" || result.AgentID != agentID {
			t.Fatalf("reading = %+v, want versioned reading output for %s", result, agentID)
		}
		if result.Entity != "decl:function_declaration::Run:func Run():0" {
			t.Fatalf("reading entity = %q", result.Entity)
		}
	})

	t.Run("presence", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "presence"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("coord presence --json: %v\nraw: %s", err, out.String())
		}
		var result JSONCoordPresenceOutput
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("json.Unmarshal presence: %v\nraw: %s", err, out.String())
		}
		if result.SchemaVersion != JSONSchemaVersion || len(result.Entries) != 1 || result.Entries[0].AgentID != agentID {
			t.Fatalf("presence = %+v, want versioned presence entry for %s", result, agentID)
		}
	})
}

func TestCoordCheckTextUsesCommandOutput(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newCoordCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("coord check: %v", err)
	}

	if got := out.String(); !strings.Contains(got, "No conflicts detected.") {
		t.Fatalf("coord check output = %q, want no-conflicts message", got)
	}
}

func captureCommandStdout(t *testing.T, fn func() error) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	if runErr != nil {
		t.Fatalf("command execute: %v", runErr)
	}
	return string(data)
}

func markCoordAgentHeartbeat(t *testing.T, r *repo.Repo, c *coord.Coordinator, agentID string, heartbeat time.Time) {
	t.Helper()

	agent, err := c.GetAgent(agentID)
	if err != nil {
		t.Fatalf("GetAgent %s: %v", agentID, err)
	}
	agent.HeartbeatAt = heartbeat
	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal agent: %v", err)
	}
	h, err := r.Store.WriteBlob(&object.Blob{Data: data})
	if err != nil {
		t.Fatalf("WriteBlob agent: %v", err)
	}
	if err := r.UpdateRef("refs/coord/agents/"+agentID, h); err != nil {
		t.Fatalf("UpdateRef agent: %v", err)
	}
}
