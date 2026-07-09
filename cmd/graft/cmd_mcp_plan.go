package main

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/coord"
)

// mcpPlanToolDefs returns tool definitions for plan management.
func mcpPlanToolDefs() []mcpTool {
	return []mcpTool{
		{
			Name:        "graft_plan_create",
			Description: "Create a coordination plan stored in refs/coord/plans/. Plans track multi-step work with assignable steps for agent collaboration.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"title":       {Type: "string", Description: "plan title (required)"},
					"description": {Type: "string", Description: "plan description"},
					"steps":       {Type: "string", Description: "newline-separated list of step descriptions"},
					"author":      {Type: "string", Description: "author name or agent ID"},
				},
				Required: []string{"title"},
			}.toMap(),
		},
		{
			Name:        "graft_plan_list",
			Description: "List all coordination plans. Shows ID, title, status, step progress, and timestamps.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"status": {Type: "string", Description: "filter by status: draft, active, completed, archived"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_plan_get",
			Description: "Get full details of a plan by ID, including all steps and their statuses.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"id": {Type: "string", Description: "plan ID (required)"},
				},
				Required: []string{"id"},
			}.toMap(),
		},
		{
			Name:        "graft_plan_update",
			Description: "Update a plan's status, title, description, or step statuses.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"id":          {Type: "string", Description: "plan ID (required)"},
					"status":      {Type: "string", Description: "new plan status: draft, active, completed, archived"},
					"title":       {Type: "string", Description: "new title"},
					"description": {Type: "string", Description: "new description"},
					"step_id":     {Type: "string", Description: "step ID to update"},
					"step_status": {Type: "string", Description: "new step status: pending, in_progress, completed, skipped"},
					"assigned_to": {Type: "string", Description: "assign step to agent name or ID"},
				},
				Required: []string{"id"},
			}.toMap(),
		},
		{
			Name:        "graft_plan_delete",
			Description: "Delete a plan by ID.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"id": {Type: "string", Description: "plan ID (required)"},
				},
				Required: []string{"id"},
			}.toMap(),
		},
	}
}

// mcpDispatchPlanTool routes a plan tool call to its handler.
func mcpDispatchPlanTool(name string, args map[string]any) (any, error) {
	switch name {
	case "graft_plan_create":
		return mcpToolPlanCreate(args)
	case "graft_plan_list":
		return mcpToolPlanList(args)
	case "graft_plan_get":
		return mcpToolPlanGet(args)
	case "graft_plan_update":
		return mcpToolPlanUpdate(args)
	case "graft_plan_delete":
		return mcpToolPlanDelete(args)
	default:
		return nil, fmt.Errorf("unknown plan tool %q", name)
	}
}

// --- Tool implementations ---

func mcpToolPlanCreate(args map[string]any) (any, error) {
	title := mcpArgString(args, "title")
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	plan := &coord.Plan{
		Title:       title,
		Description: mcpArgString(args, "description"),
		Author:      mcpArgString(args, "author"),
	}

	// Parse steps from newline-separated text.
	stepsStr := mcpArgString(args, "steps")
	if stepsStr != "" {
		for _, line := range strings.Split(stepsStr, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			plan.Steps = append(plan.Steps, coord.PlanStep{
				Description: line,
			})
		}
	}

	if err := c.CreatePlan(plan); err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}

	return JSONCoordPlanOutput{SchemaVersion: JSONSchemaVersion, Plan: plan}, nil
}

func mcpToolPlanList(args map[string]any) (any, error) {
	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	plans, err := c.ListPlans()
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}

	statusFilter := mcpArgString(args, "status")

	var filtered []*coord.Plan
	for _, p := range plans {
		if statusFilter != "" && p.Status != statusFilter {
			continue
		}
		filtered = append(filtered, p)
	}
	if filtered == nil {
		filtered = []*coord.Plan{}
	}

	return JSONCoordPlansOutput{SchemaVersion: JSONSchemaVersion, Plans: filtered}, nil
}

func mcpToolPlanGet(args map[string]any) (any, error) {
	id := mcpArgString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	plan, err := c.GetPlan(id)
	if err != nil {
		return nil, err
	}
	return JSONCoordPlanOutput{SchemaVersion: JSONSchemaVersion, Plan: plan}, nil
}

func mcpToolPlanUpdate(args map[string]any) (any, error) {
	id := mcpArgString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	plan, err := c.GetPlan(id)
	if err != nil {
		return nil, err
	}

	// Update plan-level fields.
	if s := mcpArgString(args, "status"); s != "" {
		plan.Status = s
	}
	if t := mcpArgString(args, "title"); t != "" {
		plan.Title = t
	}
	if d := mcpArgString(args, "description"); d != "" {
		plan.Description = d
	}

	// Update a specific step.
	stepID := mcpArgString(args, "step_id")
	if stepID != "" {
		found := false
		for i := range plan.Steps {
			if plan.Steps[i].ID == stepID {
				if ss := mcpArgString(args, "step_status"); ss != "" {
					plan.Steps[i].Status = ss
				}
				if at := mcpArgString(args, "assigned_to"); at != "" {
					plan.Steps[i].AssignedTo = at
				}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("step %q not found in plan %q", stepID, id)
		}
	}

	if err := c.UpdatePlan(plan); err != nil {
		return nil, fmt.Errorf("update plan: %w", err)
	}

	return JSONCoordPlanOutput{SchemaVersion: JSONSchemaVersion, Plan: plan}, nil
}

func mcpToolPlanDelete(args map[string]any) (any, error) {
	id := mcpArgString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	if err := c.DeletePlan(id); err != nil {
		return nil, err
	}

	return JSONCoordPlanDeleteOutput{SchemaVersion: JSONSchemaVersion, Status: "deleted", ID: id}, nil
}
