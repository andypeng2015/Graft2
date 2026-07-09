package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestMCPRemainingCoordToolsReturnVersionedContracts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/mcpcontracts\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	activeID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent active: %v", err)
	}
	targetID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-b", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent target: %v", err)
	}
	diffReq := coord.ClaimRequest{
		EntityKey: "decl:function_declaration::MCPDiff:func MCPDiff():0",
		File:      "main.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(activeID, diffReq); err != nil {
		t.Fatalf("AcquireClaim diff: %v", err)
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

	impactAny, err := mcpDispatchAll(false, "graft_coord_impact", map[string]any{})
	if err != nil {
		t.Fatalf("mcpDispatchAll impact: %v", err)
	}
	impact, ok := impactAny.(JSONCoordImpactOutput)
	if !ok {
		t.Fatalf("impact result type = %T, want JSONCoordImpactOutput", impactAny)
	}
	if impact.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("impact schemaVersion = %d, want %d", impact.SchemaVersion, JSONSchemaVersion)
	}

	diffAny, err := mcpDispatchAll(false, "graft_coord_diff", map[string]any{"agent_id": activeID})
	if err != nil {
		t.Fatalf("mcpDispatchAll diff: %v", err)
	}
	diff, ok := diffAny.(JSONCoordDiffOutput)
	if !ok {
		t.Fatalf("diff result type = %T, want JSONCoordDiffOutput", diffAny)
	}
	if diff.SchemaVersion != JSONSchemaVersion || diff.Agent == nil || diff.Agent.ID != activeID || len(diff.Claims) != 1 {
		t.Fatalf("diff result = %+v, want versioned diff for %s", diff, activeID)
	}

	xrefsAny, err := mcpDispatchAll(false, "graft_coord_xrefs", map[string]any{"name": "example.com/mcpcontracts.DoesNotExist"})
	if err != nil {
		t.Fatalf("mcpDispatchAll xrefs: %v", err)
	}
	xrefs, ok := xrefsAny.(JSONCoordXrefsOutput)
	if !ok {
		t.Fatalf("xrefs result type = %T, want JSONCoordXrefsOutput", xrefsAny)
	}
	if xrefs.SchemaVersion != JSONSchemaVersion || len(xrefs.References) != 0 {
		t.Fatalf("xrefs result = %+v, want versioned empty references", xrefs)
	}

	graphAny, err := mcpDispatchAll(false, "graft_coord_graph", map[string]any{})
	if err != nil {
		t.Fatalf("mcpDispatchAll graph: %v", err)
	}
	graph, ok := graphAny.(JSONCoordGraphOutput)
	if !ok {
		t.Fatalf("graph result type = %T, want JSONCoordGraphOutput", graphAny)
	}
	if graph.SchemaVersion != JSONSchemaVersion || len(graph.Workspaces) != 0 || len(graph.Edges) != 0 {
		t.Fatalf("graph result = %+v, want versioned empty graph", graph)
	}

	watchEntity := "decl:function_declaration::MCPWatch:func MCPWatch():0"
	watchAny, err := mcpDispatchAll(false, "graft_coord_watch", map[string]any{"entity_key": watchEntity})
	if err != nil {
		t.Fatalf("mcpDispatchAll watch: %v", err)
	}
	watch, ok := watchAny.(JSONCoordWatchOutput)
	if !ok {
		t.Fatalf("watch result type = %T, want JSONCoordWatchOutput", watchAny)
	}
	if watch.SchemaVersion != JSONSchemaVersion || watch.Status != "watching" || watch.EntityKey != watchEntity {
		t.Fatalf("watch result = %+v", watch)
	}
	watches, err := c.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches after watch: %v", err)
	}
	if len(watches) != 1 || watches[0].Agent != activeID {
		t.Fatalf("watches after watch = %+v, want one watch by %s", watches, activeID)
	}

	unwatchAny, err := mcpDispatchAll(false, "graft_coord_unwatch", map[string]any{"entity_key": watchEntity})
	if err != nil {
		t.Fatalf("mcpDispatchAll unwatch: %v", err)
	}
	unwatch, ok := unwatchAny.(JSONCoordUnwatchOutput)
	if !ok {
		t.Fatalf("unwatch result type = %T, want JSONCoordUnwatchOutput", unwatchAny)
	}
	if unwatch.SchemaVersion != JSONSchemaVersion || unwatch.Status != "unwatched" || unwatch.EntityKey != watchEntity {
		t.Fatalf("unwatch result = %+v", unwatch)
	}
	watches, err = c.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches after unwatch: %v", err)
	}
	if len(watches) != 0 {
		t.Fatalf("watches after unwatch = %+v, want none", watches)
	}

	transferEntity := "decl:function_declaration::MCPTransfer:func MCPTransfer():0"
	if err := c.AcquireClaim(activeID, coord.ClaimRequest{EntityKey: transferEntity, File: "main.go", Mode: coord.ClaimEditing}); err != nil {
		t.Fatalf("AcquireClaim transfer: %v", err)
	}
	transferHash := coord.EntityKeyHash(transferEntity)
	transferAny, err := mcpDispatchAll(false, "graft_coord_resolve", map[string]any{"key_hash": transferHash, "transfer": targetID})
	if err != nil {
		t.Fatalf("mcpDispatchAll resolve transfer: %v", err)
	}
	transfer, ok := transferAny.(JSONCoordResolveOutput)
	if !ok {
		t.Fatalf("transfer result type = %T, want JSONCoordResolveOutput", transferAny)
	}
	if transfer.SchemaVersion != JSONSchemaVersion || transfer.Status != "transferred" || transfer.KeyHash != transferHash || transfer.ToAgent != targetID {
		t.Fatalf("transfer result = %+v", transfer)
	}
}
