package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestMCPCoordCheckSchemaIncludesStaleAfter(t *testing.T) {
	tool := mustMCPToolDef(t, "graft_coord_check")
	props, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %#v", tool.InputSchema["properties"])
	}
	staleAfterProp, ok := props["stale_after_seconds"].(map[string]any)
	if !ok {
		t.Fatalf("stale_after_seconds property missing from schema: %#v", props)
	}
	if staleAfterProp["type"] != "integer" {
		t.Fatalf("stale_after_seconds type = %v, want integer", staleAfterProp["type"])
	}
}

func TestMCPApplyStaleAfterValidation(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"zero":       {"stale_after_seconds": 0},
		"negative":   {"stale_after_seconds": -1},
		"fractional": {"stale_after_seconds": 1.5},
		"nonnumeric": {"stale_after_seconds": "soon"},
	} {
		t.Run(name, func(t *testing.T) {
			c := &coord.Coordinator{Config: coord.DefaultConfig}
			if err := mcpApplyStaleAfter(c, args); err == nil {
				t.Fatalf("mcpApplyStaleAfter(%v) succeeded, want error", args)
			}
		})
	}

	c := &coord.Coordinator{Config: coord.DefaultConfig}
	if err := mcpApplyStaleAfter(c, map[string]any{"stale_after_seconds": "300"}); err != nil {
		t.Fatalf("mcpApplyStaleAfter string integer: %v", err)
	}
	if c.Config.StaleThreshold != 5*time.Minute {
		t.Fatalf("stale threshold = %s, want 5m", c.Config.StaleThreshold)
	}
}

func TestMCPCoordCheckIncludesPreMutationSummary(t *testing.T) {
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
		EntityKey: "decl:function_declaration::MCPCheck:func MCPCheck():0",
		File:      "mcp_check.go",
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
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(activeID), 0o644); err != nil {
		t.Fatalf("WriteFile agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	longThresholdAny, err := mcpDispatchAll(false, "graft_coord_check", map[string]any{"stale_after_seconds": 3600})
	if err != nil {
		t.Fatalf("mcpDispatchAll long threshold: %v", err)
	}
	longThreshold, ok := longThresholdAny.(JSONCoordCheckOutput)
	if !ok {
		t.Fatalf("long-threshold result type = %T, want JSONCoordCheckOutput", longThresholdAny)
	}
	if len(longThreshold.StaleAgents) != 0 {
		t.Fatalf("long-threshold stale agents = %+v, want none", longThreshold.StaleAgents)
	}
	if len(longThreshold.ActiveClaims) != 1 || longThreshold.ActiveClaims[0].Stale {
		t.Fatalf("long-threshold active claims = %+v, want non-stale claim", longThreshold.ActiveClaims)
	}

	resultAny, err := mcpDispatchAll(false, "graft_coord_check", map[string]any{})
	if err != nil {
		t.Fatalf("mcpDispatchAll coord check: %v", err)
	}
	result, ok := resultAny.(JSONCoordCheckOutput)
	if !ok {
		t.Fatalf("result type = %T, want JSONCoordCheckOutput", resultAny)
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if result.ActiveAgentID != activeID {
		t.Fatalf("ActiveAgentID = %q, want %q", result.ActiveAgentID, activeID)
	}
	if result.AgentsExamined != 2 {
		t.Fatalf("AgentsExamined = %d, want 2", result.AgentsExamined)
	}
	if len(result.ActiveClaims) != 1 || !result.ActiveClaims[0].Stale {
		t.Fatalf("ActiveClaims = %+v, want one stale claim", result.ActiveClaims)
	}
	if len(result.StaleAgents) != 1 || result.StaleAgents[0].ID != otherID {
		t.Fatalf("StaleAgents = %+v, want %s", result.StaleAgents, otherID)
	}
	if len(result.UnreadFeedEvents) == 0 {
		t.Fatalf("UnreadFeedEvents empty, want entity_changed event")
	}
	if result.UnreadFeedEvents[0].Event != "entity_changed" {
		t.Fatalf("unread event = %+v, want entity_changed", result.UnreadFeedEvents[0])
	}
	if len(result.UnreadFeedEvents[0].Files) != 1 || result.UnreadFeedEvents[0].Files[0] != req.File {
		t.Fatalf("unread files = %+v, want [%s]", result.UnreadFeedEvents[0].Files, req.File)
	}
}
