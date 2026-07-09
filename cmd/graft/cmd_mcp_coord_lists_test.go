package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestMCPCoordListToolsReturnVersionedContracts(t *testing.T) {
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
		EntityKey: "decl:function_declaration::MCPLists:func MCPLists():0",
		File:      "mcp_lists.go",
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

	coordDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(coordDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(agentID), 0o644); err != nil {
		t.Fatalf("WriteFile agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	statusAny, err := mcpDispatchAll(false, "graft_coord_status", map[string]any{})
	if err != nil {
		t.Fatalf("mcpDispatchAll status: %v", err)
	}
	status, ok := statusAny.(JSONCoordStatusOutput)
	if !ok {
		t.Fatalf("status result type = %T, want JSONCoordStatusOutput", statusAny)
	}
	if status.SchemaVersion != JSONSchemaVersion || status.Agents != 1 || status.Claims != 1 || status.FeedCount == 0 {
		t.Fatalf("status result = %+v, want versioned status with one agent/claim and feed events", status)
	}

	agentsAny, err := mcpDispatchAll(false, "graft_coord_agents", map[string]any{})
	if err != nil {
		t.Fatalf("mcpDispatchAll agents: %v", err)
	}
	agents, ok := agentsAny.(JSONCoordAgentsOutput)
	if !ok {
		t.Fatalf("agents result type = %T, want JSONCoordAgentsOutput", agentsAny)
	}
	if agents.SchemaVersion != JSONSchemaVersion || len(agents.Agents) != 1 || agents.Agents[0].ID != agentID {
		t.Fatalf("agents result = %+v, want versioned result with %s", agents, agentID)
	}

	claimsAny, err := mcpDispatchAll(false, "graft_coord_claims", map[string]any{})
	if err != nil {
		t.Fatalf("mcpDispatchAll claims: %v", err)
	}
	claims, ok := claimsAny.(JSONCoordClaimsOutput)
	if !ok {
		t.Fatalf("claims result type = %T, want JSONCoordClaimsOutput", claimsAny)
	}
	if claims.SchemaVersion != JSONSchemaVersion || len(claims.Claims) != 1 || claims.Claims[0].EntityKey != req.EntityKey {
		t.Fatalf("claims result = %+v, want versioned claim for %s", claims, req.EntityKey)
	}

	feedAny, err := mcpDispatchAll(false, "graft_coord_feed", map[string]any{"mine": true})
	if err != nil {
		t.Fatalf("mcpDispatchAll feed: %v", err)
	}
	feed, ok := feedAny.(JSONCoordFeedOutput)
	if !ok {
		t.Fatalf("feed result type = %T, want JSONCoordFeedOutput", feedAny)
	}
	if feed.SchemaVersion != JSONSchemaVersion || len(feed.Events) == 0 {
		t.Fatalf("feed result = %+v, want versioned events", feed)
	}
	if feed.Events[0].FeedHash == "" {
		t.Fatalf("feed event missing feed hash: %+v", feed.Events[0])
	}
}
