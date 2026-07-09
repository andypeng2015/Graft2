package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestMCPPlanAndTaskToolsReturnVersionedContracts(t *testing.T) {
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
	coordDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(coordDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(agentID), 0o644); err != nil {
		t.Fatalf("WriteFile agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	planCreateAny, err := mcpDispatchAll(false, "graft_plan_create", map[string]any{
		"title":       "Production readiness",
		"description": "stabilize MCP contracts",
		"steps":       "Ship contracts\nVerify release gates",
		"author":      "cedar",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll plan create: %v", err)
	}
	planCreate, ok := planCreateAny.(JSONCoordPlanOutput)
	if !ok {
		t.Fatalf("plan create result type = %T, want JSONCoordPlanOutput", planCreateAny)
	}
	if planCreate.SchemaVersion != JSONSchemaVersion || planCreate.Plan == nil || planCreate.Plan.ID == "" || len(planCreate.Plan.Steps) != 2 {
		t.Fatalf("plan create result = %+v, want versioned plan with two steps", planCreate)
	}
	planID := planCreate.Plan.ID
	firstStepID := planCreate.Plan.Steps[0].ID

	planListAny, err := mcpDispatchAll(false, "graft_plan_list", map[string]any{"status": "draft"})
	if err != nil {
		t.Fatalf("mcpDispatchAll plan list: %v", err)
	}
	planList, ok := planListAny.(JSONCoordPlansOutput)
	if !ok {
		t.Fatalf("plan list result type = %T, want JSONCoordPlansOutput", planListAny)
	}
	if planList.SchemaVersion != JSONSchemaVersion || len(planList.Plans) != 1 || planList.Plans[0].ID != planID {
		t.Fatalf("plan list result = %+v, want created plan %s", planList, planID)
	}

	planUpdateAny, err := mcpDispatchAll(false, "graft_plan_update", map[string]any{
		"id":          planID,
		"status":      "active",
		"step_id":     firstStepID,
		"step_status": "completed",
		"assigned_to": "cedar",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll plan update: %v", err)
	}
	planUpdate, ok := planUpdateAny.(JSONCoordPlanOutput)
	if !ok {
		t.Fatalf("plan update result type = %T, want JSONCoordPlanOutput", planUpdateAny)
	}
	if planUpdate.SchemaVersion != JSONSchemaVersion || planUpdate.Plan == nil || planUpdate.Plan.Status != "active" ||
		planUpdate.Plan.Steps[0].Status != "completed" || planUpdate.Plan.Steps[0].AssignedTo != "cedar" {
		t.Fatalf("plan update result = %+v", planUpdate)
	}

	planGetAny, err := mcpDispatchAll(false, "graft_plan_get", map[string]any{"id": planID})
	if err != nil {
		t.Fatalf("mcpDispatchAll plan get: %v", err)
	}
	planGet, ok := planGetAny.(JSONCoordPlanOutput)
	if !ok {
		t.Fatalf("plan get result type = %T, want JSONCoordPlanOutput", planGetAny)
	}
	if planGet.SchemaVersion != JSONSchemaVersion || planGet.Plan == nil || planGet.Plan.ID != planID {
		t.Fatalf("plan get result = %+v, want plan %s", planGet, planID)
	}

	taskCreateAny, err := mcpDispatchAll(false, "graft_task_create", map[string]any{
		"title":       "Ship task contracts",
		"description": "align MCP task payloads",
		"workspace":   "graft",
		"plan_id":     planID,
		"assign":      "cedar",
		"priority":    "9",
		"tags":        "mcp,json",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll task create: %v", err)
	}
	taskCreate, ok := taskCreateAny.(JSONCoordTaskOutput)
	if !ok {
		t.Fatalf("task create result type = %T, want JSONCoordTaskOutput", taskCreateAny)
	}
	if taskCreate.SchemaVersion != JSONSchemaVersion || taskCreate.Task == nil || taskCreate.Task.ID == "" || taskCreate.Task.Priority != 9 {
		t.Fatalf("task create result = %+v, want versioned task with priority", taskCreate)
	}
	taskID := taskCreate.Task.ID

	taskListAny, err := mcpDispatchAll(false, "graft_task_list", map[string]any{"plan_id": planID})
	if err != nil {
		t.Fatalf("mcpDispatchAll task list: %v", err)
	}
	taskList, ok := taskListAny.(JSONCoordTasksOutput)
	if !ok {
		t.Fatalf("task list result type = %T, want JSONCoordTasksOutput", taskListAny)
	}
	if taskList.SchemaVersion != JSONSchemaVersion || len(taskList.Tasks) != 1 || taskList.Tasks[0].ID != taskID {
		t.Fatalf("task list result = %+v, want task %s", taskList, taskID)
	}

	taskUpdateAny, err := mcpDispatchAll(false, "graft_task_update", map[string]any{
		"id":     taskID,
		"status": "in_progress",
		"tags":   "mcp,contracts",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll task update: %v", err)
	}
	taskUpdate, ok := taskUpdateAny.(JSONCoordTaskOutput)
	if !ok {
		t.Fatalf("task update result type = %T, want JSONCoordTaskOutput", taskUpdateAny)
	}
	if taskUpdate.SchemaVersion != JSONSchemaVersion || taskUpdate.Task == nil || taskUpdate.Task.Status != "in_progress" {
		t.Fatalf("task update result = %+v", taskUpdate)
	}

	taskGetAny, err := mcpDispatchAll(false, "graft_task_get", map[string]any{"id": taskID})
	if err != nil {
		t.Fatalf("mcpDispatchAll task get: %v", err)
	}
	taskGet, ok := taskGetAny.(JSONCoordTaskOutput)
	if !ok {
		t.Fatalf("task get result type = %T, want JSONCoordTaskOutput", taskGetAny)
	}
	if taskGet.SchemaVersion != JSONSchemaVersion || taskGet.Task == nil || taskGet.Task.ID != taskID {
		t.Fatalf("task get result = %+v, want task %s", taskGet, taskID)
	}

	taskClaimAny, err := mcpDispatchAll(false, "graft_task_claim", map[string]any{"id": taskID})
	if err != nil {
		t.Fatalf("mcpDispatchAll task claim: %v", err)
	}
	taskClaim, ok := taskClaimAny.(JSONCoordTaskClaimOutput)
	if !ok {
		t.Fatalf("task claim result type = %T, want JSONCoordTaskClaimOutput", taskClaimAny)
	}
	if taskClaim.SchemaVersion != JSONSchemaVersion || taskClaim.Status != "claimed" || taskClaim.TaskID != taskID || taskClaim.AssignedTo != "cedar" {
		t.Fatalf("task claim result = %+v", taskClaim)
	}

	taskDeleteAny, err := mcpDispatchAll(false, "graft_task_delete", map[string]any{"id": taskID})
	if err != nil {
		t.Fatalf("mcpDispatchAll task delete: %v", err)
	}
	taskDelete, ok := taskDeleteAny.(JSONCoordTaskDeleteOutput)
	if !ok {
		t.Fatalf("task delete result type = %T, want JSONCoordTaskDeleteOutput", taskDeleteAny)
	}
	if taskDelete.SchemaVersion != JSONSchemaVersion || taskDelete.Status != "deleted" || taskDelete.ID != taskID {
		t.Fatalf("task delete result = %+v", taskDelete)
	}

	planDeleteAny, err := mcpDispatchAll(false, "graft_plan_delete", map[string]any{"id": planID})
	if err != nil {
		t.Fatalf("mcpDispatchAll plan delete: %v", err)
	}
	planDelete, ok := planDeleteAny.(JSONCoordPlanDeleteOutput)
	if !ok {
		t.Fatalf("plan delete result type = %T, want JSONCoordPlanDeleteOutput", planDeleteAny)
	}
	if planDelete.SchemaVersion != JSONSchemaVersion || planDelete.Status != "deleted" || planDelete.ID != planID {
		t.Fatalf("plan delete result = %+v", planDelete)
	}
}
