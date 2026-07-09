package main

import (
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestMCPCoordCleanupStaleSchemaIncludesDryRunAndStaleAfter(t *testing.T) {
	tool := mustMCPToolDef(t, "graft_coord_cleanup_stale")
	props, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %#v", tool.InputSchema["properties"])
	}
	dryRunProp, ok := props["dry_run"].(map[string]any)
	if !ok {
		t.Fatalf("dry_run property missing from schema: %#v", props)
	}
	if dryRunProp["type"] != "boolean" {
		t.Fatalf("dry_run type = %v, want boolean", dryRunProp["type"])
	}
	staleAfterProp, ok := props["stale_after_seconds"].(map[string]any)
	if !ok {
		t.Fatalf("stale_after_seconds property missing from schema: %#v", props)
	}
	if staleAfterProp["type"] != "integer" {
		t.Fatalf("stale_after_seconds type = %v, want integer", staleAfterProp["type"])
	}
}

func TestMCPCoordCleanupStaleDryRunAndRemove(t *testing.T) {
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
		EntityKey: "decl:function_declaration::MCPCleanup:func MCPCleanup():0",
		File:      "mcp_cleanup.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(staleID, req); err != nil {
		t.Fatalf("AcquireClaim stale: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	longThresholdAny, err := mcpDispatchAll(false, "graft_coord_cleanup_stale", map[string]any{"dry_run": true, "stale_after_seconds": 3600})
	if err != nil {
		t.Fatalf("mcpDispatchAll long threshold: %v", err)
	}
	longThreshold, ok := longThresholdAny.(JSONCoordCleanupStaleOutput)
	if !ok {
		t.Fatalf("long-threshold result type = %T, want JSONCoordCleanupStaleOutput", longThresholdAny)
	}
	if longThreshold.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("long-threshold schemaVersion = %d, want %d", longThreshold.SchemaVersion, JSONSchemaVersion)
	}
	if !longThreshold.OK || !longThreshold.DryRun || longThreshold.Removed != 0 {
		t.Fatalf("long-threshold result = %+v, want ok dry run without removals", longThreshold)
	}
	if len(longThreshold.StaleAgents) != 0 {
		t.Fatalf("long-threshold stale agents = %+v, want none", longThreshold.StaleAgents)
	}

	dryRunAny, err := mcpDispatchAll(false, "graft_coord_cleanup_stale", map[string]any{"dry_run": true})
	if err != nil {
		t.Fatalf("mcpDispatchAll dry-run: %v", err)
	}
	dryRun, ok := dryRunAny.(JSONCoordCleanupStaleOutput)
	if !ok {
		t.Fatalf("dry-run result type = %T, want JSONCoordCleanupStaleOutput", dryRunAny)
	}
	if dryRun.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("dry-run schemaVersion = %d, want %d", dryRun.SchemaVersion, JSONSchemaVersion)
	}
	if !dryRun.OK || !dryRun.DryRun || dryRun.Removed != 0 {
		t.Fatalf("dry-run result = %+v, want ok dry run without removals", dryRun)
	}
	if len(dryRun.StaleAgents) != 1 || dryRun.StaleAgents[0].ID != staleID {
		t.Fatalf("dry-run stale agents = %+v, want %s", dryRun.StaleAgents, staleID)
	}
	if _, err := c.GetAgent(staleID); err != nil {
		t.Fatalf("stale agent removed during MCP dry-run: %v", err)
	}

	removeAny, err := mcpDispatchAll(false, "graft_coord_cleanup_stale", map[string]any{})
	if err != nil {
		t.Fatalf("mcpDispatchAll remove: %v", err)
	}
	removed, ok := removeAny.(JSONCoordCleanupStaleOutput)
	if !ok {
		t.Fatalf("remove result type = %T, want JSONCoordCleanupStaleOutput", removeAny)
	}
	if removed.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("remove schemaVersion = %d, want %d", removed.SchemaVersion, JSONSchemaVersion)
	}
	if !removed.OK || removed.DryRun || removed.Removed != 1 {
		t.Fatalf("remove result = %+v, want one stale removal", removed)
	}
	if len(removed.StaleAgents) != 1 || removed.StaleAgents[0].ID != staleID {
		t.Fatalf("removed stale agents = %+v, want %s", removed.StaleAgents, staleID)
	}
	if _, err := c.GetAgent(activeID); err != nil {
		t.Fatalf("active agent missing after MCP cleanup: %v", err)
	}
	if _, err := c.GetAgent(staleID); err == nil {
		t.Fatalf("stale agent %q still registered after MCP cleanup", staleID)
	}
	claim, err := c.LoadClaim(req.EntityKey)
	if err != nil {
		t.Fatalf("LoadClaim after MCP cleanup: %v", err)
	}
	if claim != nil {
		t.Fatalf("stale claim still present after MCP cleanup: %+v", claim)
	}
}

func mustMCPToolDef(t *testing.T, name string) mcpTool {
	t.Helper()
	for _, tool := range mcpToolDefs() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q missing", name)
	return mcpTool{}
}
