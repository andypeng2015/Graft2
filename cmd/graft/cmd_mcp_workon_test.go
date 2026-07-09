package main

import (
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestMCPWorkonSchemaIncludesRecover(t *testing.T) {
	var workon *mcpTool
	tools := mcpToolDefs()
	for i := range tools {
		if tools[i].Name == "graft_workon" {
			workon = &tools[i]
			break
		}
	}
	if workon == nil {
		t.Fatal("graft_workon tool definition missing")
	}
	props, ok := workon.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %#v", workon.InputSchema["properties"])
	}
	recoverProp, ok := props["recover"].(map[string]any)
	if !ok {
		t.Fatalf("recover property missing from graft_workon schema: %#v", props)
	}
	if recoverProp["type"] != "boolean" {
		t.Fatalf("recover type = %v, want boolean", recoverProp["type"])
	}
}

func TestMCPWorkonResumesFreshSession(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	firstAny, err := mcpDispatchAll(false, "graft_workon", map[string]any{"name": "cedar"})
	if err != nil {
		t.Fatalf("mcpDispatchAll first: %v", err)
	}
	first, ok := firstAny.(JSONWorkonOutput)
	if !ok {
		t.Fatalf("first result type = %T, want JSONWorkonOutput", firstAny)
	}
	if first.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("first schemaVersion = %d, want %d", first.SchemaVersion, JSONSchemaVersion)
	}
	firstID := first.AgentID

	secondAny, err := mcpDispatchAll(false, "graft_workon", map[string]any{"name": "cedar"})
	if err != nil {
		t.Fatalf("mcpDispatchAll second: %v", err)
	}
	second, ok := secondAny.(JSONWorkonOutput)
	if !ok {
		t.Fatalf("second result type = %T, want JSONWorkonOutput", secondAny)
	}
	if second.Status != "resumed" {
		t.Fatalf("status = %v, want resumed", second.Status)
	}
	if second.AgentID != firstID {
		t.Fatalf("agent_id = %v, want original %s", second.AgentID, firstID)
	}

	c := coord.New(r, coord.DefaultConfig)
	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len(agents) = %d, want 1", len(agents))
	}
	assertWorkonAgentFile(t, r, "agent-id", firstID)
	assertWorkonAgentFile(t, r, "agent-cedar", firstID)
}

func TestMCPWorkonRecoverReplacesStaleSelfIdentity(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	c := coord.New(r, coord.DefaultConfig)
	oldID := createStaleWorkonSession(t, r, c, "cedar")
	req := coord.ClaimRequest{
		EntityKey: "decl:function_declaration::MCPRecover:func MCPRecover():0",
		File:      "mcp_recover.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(oldID, req); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	_, err = mcpDispatchAll(false, "graft_workon", map[string]any{"name": "cedar"})
	if err == nil {
		t.Fatal("mcp workon succeeded without recover, want recovery guidance")
	}
	if !strings.Contains(err.Error(), "recover=true") {
		t.Fatalf("error = %q, want recover=true guidance", err.Error())
	}
	if _, err := c.GetAgent(oldID); err != nil {
		t.Fatalf("stale agent was mutated without recover=true: %v", err)
	}

	resultAny, err := mcpDispatchAll(false, "graft_workon", map[string]any{"name": "cedar", "recover": true})
	if err != nil {
		t.Fatalf("mcpDispatchAll recover: %v", err)
	}
	result, ok := resultAny.(JSONWorkonOutput)
	if !ok {
		t.Fatalf("recover result type = %T, want JSONWorkonOutput", resultAny)
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if result.Status != "recovered" {
		t.Fatalf("status = %v, want recovered", result.Status)
	}
	if !result.Recovered {
		t.Fatalf("recovered = %v, want true", result.Recovered)
	}
	if result.PreviousAgentID != oldID {
		t.Fatalf("previous_agent_id = %v, want %s", result.PreviousAgentID, oldID)
	}
	newID := result.AgentID
	if newID == "" || newID == oldID {
		t.Fatalf("agent_id = %v, want fresh id different from %s", result.AgentID, oldID)
	}

	after := coord.New(r, coord.DefaultConfig)
	if _, err := after.GetAgent(oldID); err == nil {
		t.Fatalf("old agent %q still registered after MCP recovery", oldID)
	}
	if _, err := after.GetAgent(newID); err != nil {
		t.Fatalf("new agent %q missing after MCP recovery: %v", newID, err)
	}
	claim, err := after.LoadClaim(req.EntityKey)
	if err != nil {
		t.Fatalf("LoadClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("old claim still present after MCP recovery: %+v", claim)
	}
	assertWorkonAgentFile(t, r, "agent-id", newID)
	assertWorkonAgentFile(t, r, "agent-cedar", newID)
}

func mustMCPMap(t *testing.T, v any) map[string]any {
	t.Helper()
	result, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", v)
	}
	return result
}
