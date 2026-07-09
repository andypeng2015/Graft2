package main

import (
	"encoding/json"
	"io"
	"reflect"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/coordd"
)

const JSONSchemaVersion = 1

// --- Version ---

type JSONVersionOutput struct {
	SchemaVersion                  int    `json:"schemaVersion,omitempty"`
	Version                        string `json:"version"`
	Commit                         string `json:"commit"`
	BuildTime                      string `json:"buildTime"`
	GoVersion                      string `json:"goVersion"`
	SupportedRepositoryFormat      int    `json:"supportedRepositoryFormat"`
	SupportedRemoteProtocolVersion string `json:"supportedRemoteProtocolVersion"`
}

// --- Remote ---

type JSONRemoteOutput struct {
	SchemaVersion int               `json:"schemaVersion,omitempty"`
	Remotes       []JSONRemoteEntry `json:"remotes"`
}

type JSONRemoteEntry struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Transport string `json:"transport"`
	Warning   string `json:"warning,omitempty"`
}

// --- Workspace ---

// JSONWorkspacesOutput is the top-level JSON output for
// "graft workspace list --json".
type JSONWorkspacesOutput struct {
	SchemaVersion int               `json:"schemaVersion,omitempty"`
	Workspaces    map[string]string `json:"workspaces"`
}

// JSONWorkspaceMutationOutput is the top-level JSON output for
// "graft workspace add/remove --json".
type JSONWorkspaceMutationOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	Name          string `json:"name"`
	Path          string `json:"path,omitempty"`
}

// --- Auth doctor ---

type JSONAuthDoctorOutput struct {
	SchemaVersion     int                        `json:"schemaVersion,omitempty"`
	OK                bool                       `json:"ok"`
	SelectedHost      string                     `json:"selectedHost"`
	ConfigPath        string                     `json:"configPath,omitempty"`
	ConfigFilePresent bool                       `json:"configFilePresent"`
	ConfigFileMode    string                     `json:"configFileMode,omitempty"`
	ConfigFileSecure  bool                       `json:"configFileSecure"`
	TokenSet          bool                       `json:"tokenSet"`
	TokenSource       string                     `json:"tokenSource,omitempty"`
	TokenExpiryKnown  bool                       `json:"tokenExpiryKnown,omitempty"`
	TokenExpiresAt    string                     `json:"tokenExpiresAt,omitempty"`
	TokenExpired      bool                       `json:"tokenExpired,omitempty"`
	Hosts             []JSONAuthDoctorHost       `json:"hosts,omitempty"`
	Diagnostics       []JSONRepositoryDiagnostic `json:"diagnostics,omitempty"`
}

type JSONAuthDoctorHost struct {
	Host               string `json:"host"`
	Selected           bool   `json:"selected,omitempty"`
	Default            bool   `json:"default,omitempty"`
	UsernameConfigured bool   `json:"usernameConfigured"`
	OwnerConfigured    bool   `json:"ownerConfigured"`
	TokenSet           bool   `json:"tokenSet"`
	TokenSource        string `json:"tokenSource,omitempty"`
	TokenExpiryKnown   bool   `json:"tokenExpiryKnown,omitempty"`
	TokenExpiresAt     string `json:"tokenExpiresAt,omitempty"`
	TokenExpired       bool   `json:"tokenExpired,omitempty"`
}

// --- Coordd guard doctor ---

type JSONCoorddGuardDoctorOutput struct {
	SchemaVersion int                               `json:"schemaVersion,omitempty"`
	OK            bool                              `json:"ok"`
	Health        coordd.SandboxBackendHealthReport `json:"health"`
	Diagnostics   []JSONRepositoryDiagnostic        `json:"diagnostics,omitempty"`
}

// JSONCoorddEventOutput is a JSON-lines event output for
// "graft coordd tail --json --follow".
type JSONCoorddEventOutput struct {
	SchemaVersion int `json:"schemaVersion,omitempty"`
	*coordd.Event `json:",omitempty"`
}

// JSONCoorddTailOutput is the top-level JSON output for
// "graft coordd tail --json" in non-follow mode.
type JSONCoorddTailOutput struct {
	SchemaVersion int            `json:"schemaVersion,omitempty"`
	Events        []coordd.Event `json:"events"`
}

// JSONCoorddSnapshotOutput is the top-level JSON output for
// "graft coordd snapshot --json".
type JSONCoorddSnapshotOutput struct {
	SchemaVersion    int    `json:"schemaVersion,omitempty"`
	Status           string `json:"status,omitempty"`
	*coordd.Snapshot `json:",omitempty"`
}

func coorddSnapshotToJSON(snapshot *coordd.Snapshot) JSONCoorddSnapshotOutput {
	if snapshot == nil {
		return JSONCoorddSnapshotOutput{Status: "clean"}
	}
	return JSONCoorddSnapshotOutput{Status: "captured", Snapshot: snapshot}
}

// JSONCoorddActionDecisionOutput is the top-level JSON output for
// "graft coordd preflight --json" and "graft coordd exec --check-only --json".
type JSONCoorddActionDecisionOutput struct {
	SchemaVersion int                          `json:"schemaVersion,omitempty"`
	Input         coordd.ActionPolicyInput     `json:"input"`
	Decision      *coordd.ActionPolicyDecision `json:"decision"`
}

// JSONCoorddExecOutput is the top-level JSON output for
// "graft coordd exec --json".
type JSONCoorddExecOutput struct {
	SchemaVersion      int `json:"schemaVersion,omitempty"`
	*coordd.ExecResult `json:",omitempty"`
}

func coorddExecResultToJSON(result *coordd.ExecResult) JSONCoorddExecOutput {
	return JSONCoorddExecOutput{ExecResult: result}
}

// JSONMCPExecOutput is the top-level MCP output for graft_exec. It includes
// preflight data plus optional execution IO captured for the MCP caller.
type JSONMCPExecOutput struct {
	SchemaVersion int                          `json:"schemaVersion,omitempty"`
	Input         coordd.ActionPolicyInput     `json:"input"`
	Decision      *coordd.ActionPolicyDecision `json:"decision"`
	Allowed       bool                         `json:"allowed"`
	Exec          *coordd.ExecResult           `json:"exec,omitempty"`
	Stdout        string                       `json:"stdout,omitempty"`
	Stderr        string                       `json:"stderr,omitempty"`
	ExitCode      int                          `json:"exit_code,omitempty"`
	Status        string                       `json:"status"`
	Error         string                       `json:"error,omitempty"`
}

// JSONCoorddSpawnOutput is the top-level JSON output for
// "graft coordd spawn --json".
type JSONCoorddSpawnOutput struct {
	SchemaVersion       int `json:"schemaVersion,omitempty"`
	*coordd.SpawnResult `json:",omitempty"`
	Status              string `json:"status,omitempty"`
	ExitCode            int    `json:"exit_code,omitempty"`
	Error               string `json:"error,omitempty"`
}

func coorddSpawnResultToJSON(result *coordd.SpawnResult) JSONCoorddSpawnOutput {
	return JSONCoorddSpawnOutput{SpawnResult: result}
}

// JSONCoorddSpawnRecordOutput is the top-level JSON output for coordd commands
// that return a single spawn record.
type JSONCoorddSpawnRecordOutput struct {
	SchemaVersion       int `json:"schemaVersion,omitempty"`
	*coordd.SpawnRecord `json:",omitempty"`
}

func coorddSpawnRecordToJSON(record *coordd.SpawnRecord) JSONCoorddSpawnRecordOutput {
	return JSONCoorddSpawnRecordOutput{SpawnRecord: record}
}

// JSONCoorddSpawnViewOutput is the top-level JSON output for coordd commands
// that return a spawn record plus lease view.
type JSONCoorddSpawnViewOutput struct {
	SchemaVersion     int `json:"schemaVersion,omitempty"`
	*coordd.SpawnView `json:",omitempty"`
}

func coorddSpawnViewToJSON(view *coordd.SpawnView) JSONCoorddSpawnViewOutput {
	return JSONCoorddSpawnViewOutput{SpawnView: view}
}

// JSONCoorddSpawnTraceRawOutput is the top-level JSON output for
// "graft coordd spawn-trace --json --view raw".
type JSONCoorddSpawnTraceRawOutput struct {
	SchemaVersion      int `json:"schemaVersion,omitempty"`
	*coordd.SpawnTrace `json:",omitempty"`
}

func coorddSpawnTraceRawToJSON(trace *coordd.SpawnTrace) JSONCoorddSpawnTraceRawOutput {
	return JSONCoorddSpawnTraceRawOutput{SpawnTrace: trace}
}

// JSONCoorddSpawnTraceOutput is the top-level JSON output for
// "graft coordd spawn-trace --json".
type JSONCoorddSpawnTraceOutput struct {
	SchemaVersion          int `json:"schemaVersion,omitempty"`
	*coordd.SpawnTraceView `json:",omitempty"`
}

func coorddSpawnTraceViewToJSON(trace *coordd.SpawnTraceView) JSONCoorddSpawnTraceOutput {
	return JSONCoorddSpawnTraceOutput{SpawnTraceView: trace}
}

// JSONCoorddSpawnsOutput is the top-level JSON output for
// "graft coordd spawns --json".
type JSONCoorddSpawnsOutput struct {
	SchemaVersion int                  `json:"schemaVersion,omitempty"`
	Spawns        []coordd.SpawnRecord `json:"spawns"`
}

// JSONCoorddGuardShowOutput is the top-level JSON output for
// "graft coordd guard show --json".
type JSONCoorddGuardShowOutput struct {
	SchemaVersion int                                `json:"schemaVersion,omitempty"`
	Config        *coordd.GuardConfig                `json:"config"`
	Overrides     []coorddRuleOverrideView           `json:"overrides"`
	BundleIDs     map[string]string                  `json:"bundle_ids"`
	Policies      map[string]coordd.PolicyBundleInfo `json:"policies"`
}

// JSONCoorddGuardOverridesOutput is the top-level JSON output for
// "graft coordd guard override list --json".
type JSONCoorddGuardOverridesOutput struct {
	SchemaVersion int                      `json:"schemaVersion,omitempty"`
	Overrides     []coorddRuleOverrideView `json:"overrides"`
}

// --- Workon ---

// JSONWorkonOutput is the top-level JSON output for "graft workon --json" and
// MCP graft_workon/graft_workon_done.
type JSONWorkonOutput struct {
	SchemaVersion int               `json:"schemaVersion,omitempty"`
	Status        string            `json:"status"`
	AgentID       string            `json:"agent_id,omitempty"`
	AgentName     string            `json:"agent_name,omitempty"`
	Workspace     string            `json:"workspace,omitempty"`
	Mode          string            `json:"mode,omitempty"`
	Scope         string            `json:"scope,omitempty"`
	Notify        string            `json:"notify,omitempty"`
	Agents        int               `json:"agents,omitempty"`
	Claims        int               `json:"claims,omitempty"`
	Discovered    map[string]string `json:"discovered,omitempty"`
	Recovered     bool              `json:"recovered,omitempty"`

	PreviousAgentID string `json:"previous_agent_id,omitempty"`
	RecoveryReason  string `json:"recovery_reason,omitempty"`
}

// --- Context ---

// JSONContextOutput is the top-level JSON output for "graft context --json".
type JSONContextOutput struct {
	SchemaVersion int                    `json:"schemaVersion,omitempty"`
	Target        coord.ContextSection   `json:"target"`
	Dependencies  []coord.ContextSection `json:"dependencies"`
	Dependents    []coord.ContextSection `json:"dependents"`
	BudgetTokens  int                    `json:"budget_tokens"`
	UsedTokens    int                    `json:"used_tokens"`
	Truncated     bool                   `json:"truncated"`
}

func entityContextResultToJSON(result coord.EntityContextResult) JSONContextOutput {
	return JSONContextOutput{
		Target:       result.Target,
		Dependencies: result.Dependencies,
		Dependents:   result.Dependents,
		BudgetTokens: result.BudgetTokens,
		UsedTokens:   result.UsedTokens,
		Truncated:    result.Truncated,
	}
}

// writeJSON encodes v as indented JSON and writes it to w.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(withDefaultJSONSchemaVersion(v))
}

func writeJSONLine(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(withDefaultJSONSchemaVersion(v))
}

func withDefaultJSONSchemaVersion(v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return v
	}
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return v
		}
		setDefaultJSONSchemaVersion(rv.Elem())
		return v
	}
	if rv.Kind() != reflect.Struct {
		return v
	}

	copyValue := reflect.New(rv.Type()).Elem()
	copyValue.Set(rv)
	if setDefaultJSONSchemaVersion(copyValue) {
		return copyValue.Interface()
	}
	return v
}

func setDefaultJSONSchemaVersion(v reflect.Value) bool {
	if v.Kind() != reflect.Struct {
		return false
	}
	field := v.FieldByName("SchemaVersion")
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.Int {
		return false
	}
	if field.Int() == 0 {
		field.SetInt(JSONSchemaVersion)
	}
	return true
}

// --- Status ---

// JSONStatusOutput is the top-level JSON output for "graft status --json".
type JSONStatusOutput struct {
	SchemaVersion int               `json:"schemaVersion,omitempty"`
	Branch        string            `json:"branch"`
	NoCommits     bool              `json:"noCommits"`
	ShadowDesync  bool              `json:"shadow_desync,omitempty"`
	ShadowState   string            `json:"shadow_state,omitempty"`
	ShadowMessage string            `json:"shadow_message,omitempty"`
	ShadowRepair  string            `json:"shadow_repair,omitempty"`
	Conflicts     []JSONStatusEntry `json:"conflicts,omitempty"`
	Staged        []JSONStatusEntry `json:"staged,omitempty"`
	Unstaged      []JSONStatusEntry `json:"unstaged,omitempty"`
	Untracked     []string          `json:"untracked,omitempty"`
}

// JSONStatusEntry represents a single file in a status category.
type JSONStatusEntry struct {
	Path        string `json:"path"`
	Status      string `json:"status"` // "new", "modified", "deleted", "renamed", "conflict", "dirty"
	RenamedFrom string `json:"renamedFrom,omitempty"`
}

// --- Coordination ---

// JSONCoordStatusOutput is the top-level JSON output for "graft coord --json"
// and MCP graft_coord_status.
type JSONCoordStatusOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Agents        int    `json:"agents"`
	Claims        int    `json:"claims"`
	Conflicts     int    `json:"conflicts"`
	FeedCount     int    `json:"feed_count"`
	Notes         int    `json:"notes"`
	Tasks         int    `json:"tasks"`
	TasksPending  int    `json:"tasks_pending"`
	TasksActive   int    `json:"tasks_active"`
	ActiveID      string `json:"active_id,omitempty"`
}

// JSONCoordAgentsOutput is the top-level JSON output for
// "graft coord agents --json".
type JSONCoordAgentsOutput struct {
	SchemaVersion int               `json:"schemaVersion,omitempty"`
	Agents        []coord.AgentInfo `json:"agents"`
}

// JSONCoordClaimsOutput is the top-level JSON output for
// "graft coord claims --json".
type JSONCoordClaimsOutput struct {
	SchemaVersion int              `json:"schemaVersion,omitempty"`
	Claims        []JSONCoordClaim `json:"claims"`
}

type JSONCoordClaim struct {
	EntityKey       string `json:"entity_key"`
	EntityKeyHash   string `json:"entity_key_hash"`
	File            string `json:"file"`
	Agent           string `json:"agent"`
	AgentName       string `json:"agent_name"`
	Mode            string `json:"mode"`
	ClaimedAt       string `json:"claimed_at"`
	SourceWorkspace string `json:"source_workspace,omitempty"`
}

// JSONCoordFeedOutput is the top-level JSON output for
// "graft coord feed --json".
type JSONCoordFeedOutput struct {
	SchemaVersion int                  `json:"schemaVersion,omitempty"`
	Events        []JSONCoordFeedEntry `json:"events"`
}

// JSONCoordDecisionsOutput is the top-level JSON output for
// "graft coord decisions --json".
type JSONCoordDecisionsOutput struct {
	SchemaVersion int                   `json:"schemaVersion,omitempty"`
	Decisions     []coord.DecisionGraph `json:"decisions"`
}

// JSONCoordHeartbeatOutput is the top-level JSON output for
// "graft coord heartbeat --json".
type JSONCoordHeartbeatOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	AgentID       string `json:"agent_id"`
}

// JSONCoordSessionsOutput is the top-level JSON output for
// "graft coord sessions --json".
type JSONCoordSessionsOutput struct {
	SchemaVersion int             `json:"schemaVersion,omitempty"`
	Sessions      []coord.Session `json:"sessions"`
}

// JSONCoordPresenceOutput is the top-level JSON output for
// "graft coord presence --json".
type JSONCoordPresenceOutput struct {
	SchemaVersion int                   `json:"schemaVersion,omitempty"`
	Entries       []coord.PresenceEntry `json:"entries"`
}

// JSONCoordReadingOutput is the top-level JSON output for
// "graft coord reading --json".
type JSONCoordReadingOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	File          string `json:"file"`
	AgentID       string `json:"agent_id"`
	Entity        string `json:"entity,omitempty"`
}

// JSONCoordNotesOutput is the top-level JSON output for
// "graft coord note list --json".
type JSONCoordNotesOutput struct {
	SchemaVersion int           `json:"schemaVersion,omitempty"`
	Notes         []*coord.Note `json:"notes"`
}

// JSONCoordNoteOutput is the top-level JSON output for
// "graft coord note get/create/update --json".
type JSONCoordNoteOutput struct {
	SchemaVersion int         `json:"schemaVersion,omitempty"`
	Note          *coord.Note `json:"note"`
}

// JSONCoordNoteDeleteOutput is the top-level JSON output for
// "graft coord note delete --json".
type JSONCoordNoteDeleteOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	ID            string `json:"id"`
}

// JSONCoordTasksOutput is the top-level JSON output for
// "graft coord task list --json".
type JSONCoordTasksOutput struct {
	SchemaVersion int                  `json:"schemaVersion,omitempty"`
	Tasks         []JSONCoordTaskEntry `json:"tasks"`
}

// JSONCoordTaskEntry is a task list entry. SourceWorkspace is set only when
// aggregating tasks across workspaces.
type JSONCoordTaskEntry struct {
	*coord.Task
	SourceWorkspace string `json:"source_workspace,omitempty"`
}

// JSONCoordTaskOutput is the top-level JSON output for
// "graft coord task get/create/update --json".
type JSONCoordTaskOutput struct {
	SchemaVersion int         `json:"schemaVersion,omitempty"`
	Task          *coord.Task `json:"task"`
}

// JSONCoordTaskClaimOutput is the top-level JSON output for
// "graft coord task claim --json".
type JSONCoordTaskClaimOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	TaskID        string `json:"task_id"`
	AssignedTo    string `json:"assigned_to"`
	TaskStatus    string `json:"task_status"`
}

// JSONCoordTaskDeleteOutput is the top-level JSON output for
// "graft coord task delete --json".
type JSONCoordTaskDeleteOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	ID            string `json:"id"`
}

// JSONCoordPlansOutput is the top-level JSON output for
// "graft coord plan list --json".
type JSONCoordPlansOutput struct {
	SchemaVersion int           `json:"schemaVersion,omitempty"`
	Plans         []*coord.Plan `json:"plans"`
}

// JSONCoordPlanOutput is the top-level JSON output for
// "graft coord plan get/create --json".
type JSONCoordPlanOutput struct {
	SchemaVersion int         `json:"schemaVersion,omitempty"`
	Plan          *coord.Plan `json:"plan"`
}

// JSONCoordPlanDeleteOutput is the top-level JSON output for
// "graft plan delete" MCP calls.
type JSONCoordPlanDeleteOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	ID            string `json:"id"`
}

// JSONCoordWatchOutput is the top-level JSON output for
// "graft coord watch --json".
type JSONCoordWatchOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	EntityKey     string `json:"entity_key"`
	File          string `json:"file,omitempty"`
}

// JSONCoordUnwatchOutput is the top-level JSON output for
// "graft coord unwatch --json".
type JSONCoordUnwatchOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	EntityKey     string `json:"entity_key"`
}

// JSONCoordResolveOutput is the top-level JSON output for
// "graft coord resolve --json".
type JSONCoordResolveOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	KeyHash       string `json:"key_hash"`
	ToAgent       string `json:"to_agent,omitempty"`
}

// JSONCoordPublishOutput is the top-level JSON output for
// "graft coord publish --json".
type JSONCoordPublishOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Status        string `json:"status"`
	CommitHash    string `json:"commit_hash"`
	AgentID       string `json:"agent_id"`
}

// JSONCoordImpactOutput is the top-level JSON output for
// "graft coord impact --json".
type JSONCoordImpactOutput struct {
	SchemaVersion int                              `json:"schemaVersion,omitempty"`
	Workspaces    map[string]coord.WorkspaceImpact `json:"workspaces,omitempty"`
}

func coordImpactReportToJSON(report *coord.ImpactReport) JSONCoordImpactOutput {
	if report == nil {
		return JSONCoordImpactOutput{}
	}
	return JSONCoordImpactOutput{Workspaces: report.Workspaces}
}

// JSONCoordDiffOutput is the top-level JSON output for
// "graft coord diff --json".
type JSONCoordDiffOutput struct {
	SchemaVersion int               `json:"schemaVersion,omitempty"`
	Agent         *coord.AgentInfo  `json:"agent"`
	Claims        []coord.ClaimInfo `json:"claims"`
}

// JSONCoordXrefsOutput is the top-level JSON output for
// "graft coord xrefs --json".
type JSONCoordXrefsOutput struct {
	SchemaVersion int                  `json:"schemaVersion,omitempty"`
	References    []coord.XrefCallSite `json:"references"`
}

// JSONCoordGraphOutput is the top-level JSON output for
// "graft coord graph --json".
type JSONCoordGraphOutput struct {
	SchemaVersion int                  `json:"schemaVersion,omitempty"`
	Workspaces    map[string]string    `json:"workspaces"`
	Edges         []JSONCoordGraphEdge `json:"edges"`
}

type JSONCoordGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type JSONCoordFeedEntry struct {
	Event           string                `json:"event"`
	AgentID         string                `json:"agent_id,omitempty"`
	AgentName       string                `json:"agent_name,omitempty"`
	CommitHash      string                `json:"commit_hash,omitempty"`
	Entities        []coord.EntityChange  `json:"entities,omitempty"`
	Impact          *coord.ImpactReport   `json:"impact,omitempty"`
	FeedHash        string                `json:"feed_hash,omitempty"`
	Detail          map[string]any        `json:"detail,omitempty"`
	Digest          *coord.ActivityDigest `json:"digest,omitempty"`
	Source          string                `json:"source,omitempty"`
	SourceWorkspace string                `json:"source_workspace,omitempty"`
}

// JSONCoordCheckOutput is the top-level JSON output for "graft coord check --json".
type JSONCoordCheckOutput struct {
	SchemaVersion    int                       `json:"schemaVersion,omitempty"`
	OK               bool                      `json:"ok"`
	ActiveAgentID    string                    `json:"active_agent_id,omitempty"`
	AgentsExamined   int                       `json:"agents_examined"`
	ClaimsExamined   int                       `json:"claims_examined"`
	ActiveClaims     []JSONCoordCheckClaim     `json:"active_claims,omitempty"`
	StaleAgents      []JSONCoordCheckAgent     `json:"stale_agents,omitempty"`
	UnreadFeedEvents []JSONCoordCheckFeedEvent `json:"unread_feed_events,omitempty"`
	Conflicts        []JSONCoordCheckConflict  `json:"conflicts,omitempty"`
	Readers          []JSONCoordCheckReader    `json:"readers,omitempty"`
}

type JSONCoordCheckClaim struct {
	EntityKey string `json:"entity_key"`
	File      string `json:"file"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Mode      string `json:"mode"`
	Stale     bool   `json:"stale,omitempty"`
}

type JSONCoordCheckAgent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Workspace   string `json:"workspace,omitempty"`
	Host        string `json:"host,omitempty"`
	HeartbeatAt string `json:"heartbeat_at,omitempty"`
	StaleForSec int64  `json:"stale_for_sec,omitempty"`
}

// JSONCoordCleanupStaleOutput is the top-level JSON output for
// "graft coord cleanup-stale --json".
type JSONCoordCleanupStaleOutput struct {
	SchemaVersion int                   `json:"schemaVersion,omitempty"`
	OK            bool                  `json:"ok"`
	DryRun        bool                  `json:"dry_run,omitempty"`
	Removed       int                   `json:"removed"`
	StaleAgents   []JSONCoordCheckAgent `json:"stale_agents,omitempty"`
}

type JSONCoordCheckFeedEvent struct {
	Event     string   `json:"event"`
	AgentID   string   `json:"agent_id,omitempty"`
	AgentName string   `json:"agent_name,omitempty"`
	FeedHash  string   `json:"feed_hash,omitempty"`
	Files     []string `json:"files,omitempty"`
}

type JSONCoordCheckConflict struct {
	EntityKey    string `json:"entity_key"`
	File         string `json:"file"`
	HeldBy       string `json:"held_by"`
	Mode         string `json:"mode"`
	Decision     string `json:"decision,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Rule         string `json:"rule,omitempty"`
	RequireForce bool   `json:"require_force,omitempty"`
}

type JSONCoordCheckReader struct {
	AgentName string `json:"agent_name"`
	File      string `json:"file"`
	Entity    string `json:"entity,omitempty"`
}

// --- Diff ---

// JSONDiffEntityChange represents a single entity-level change in a diff.
type JSONDiffEntityChange struct {
	Path       string `json:"path"`
	EntityKey  string `json:"entityKey"`
	ChangeType string `json:"changeType"`
}

// JSONDiffOutput is the top-level JSON output for "graft diff --json".
type JSONDiffOutput struct {
	SchemaVersion int                    `json:"schemaVersion,omitempty"`
	Files         []JSONDiffFile         `json:"files"`
	EntityChanges []JSONDiffEntityChange `json:"entityChanges,omitempty"`
}

// JSONDiffFile represents a single file's diff.
type JSONDiffFile struct {
	Path        string         `json:"path"`
	Status      string         `json:"status"` // "modified", "added", "deleted", "renamed"
	RenamedFrom string         `json:"renamedFrom,omitempty"`
	RenamedTo   string         `json:"renamedTo,omitempty"`
	Hunks       []JSONDiffHunk `json:"hunks,omitempty"`
}

// JSONDiffHunk represents a single hunk in a unified diff.
type JSONDiffHunk struct {
	OldStart int            `json:"oldStart"`
	OldCount int            `json:"oldCount"`
	NewStart int            `json:"newStart"`
	NewCount int            `json:"newCount"`
	Lines    []JSONDiffLine `json:"lines"`
}

// JSONDiffLine represents a single line in a diff hunk.
type JSONDiffLine struct {
	Type    string `json:"type"` // "context", "add", "delete"
	Content string `json:"content"`
}

// --- Log ---

// JSONLogOutput is the top-level JSON output for "graft log --json".
type JSONLogOutput struct {
	SchemaVersion int            `json:"schemaVersion,omitempty"`
	Commits       []JSONLogEntry `json:"commits"`
}

// JSONLogEntry represents a single commit in the log.
type JSONLogEntry struct {
	Hash       string   `json:"hash"`
	ShortHash  string   `json:"shortHash"`
	Author     string   `json:"author"`
	Date       string   `json:"date"`
	Timestamp  int64    `json:"timestamp"`
	Message    string   `json:"message"`
	Parents    []string `json:"parents,omitempty"`
	Decoration string   `json:"decoration,omitempty"`
}

// --- Reflog ---

// JSONReflogOutput is the top-level JSON output for "graft reflog --json".
type JSONReflogOutput struct {
	SchemaVersion int               `json:"schemaVersion,omitempty"`
	Ref           string            `json:"ref,omitempty"`
	Entries       []JSONReflogEntry `json:"entries"`
}

type JSONReflogEntry struct {
	Ref       string                   `json:"ref"`
	OldHash   string                   `json:"oldHash"`
	NewHash   string                   `json:"newHash"`
	ShortHash string                   `json:"shortHash,omitempty"`
	Timestamp int64                    `json:"timestamp"`
	Date      string                   `json:"date"`
	Reason    string                   `json:"reason"`
	Entities  []JSONReflogEntityChange `json:"entities,omitempty"`
}

type JSONReflogEntityChange struct {
	Path       string `json:"path"`
	EntityKey  string `json:"entityKey"`
	ChangeType string `json:"changeType"`
}

// --- Merge ---

// JSONMergeOutput is the top-level JSON output for "graft merge --json".
type JSONMergeOutput struct {
	SchemaVersion  int             `json:"schemaVersion,omitempty"`
	Action         string          `json:"action"` // "merge", "abort", "preview"
	Source         string          `json:"source,omitempty"`
	Target         string          `json:"target,omitempty"`
	IsFastForward  bool            `json:"isFastForward"`
	HasConflicts   bool            `json:"hasConflicts"`
	TotalConflicts int             `json:"totalConflicts"`
	MergeCommit    string          `json:"mergeCommit,omitempty"`
	Files          []JSONMergeFile `json:"files,omitempty"`
	Message        string          `json:"message,omitempty"`
}

// JSONMergeFile represents the merge status of a single file.
type JSONMergeFile struct {
	Path            string               `json:"path"`
	Status          string               `json:"status"` // "clean", "conflict", "added", "deleted"
	Confidence      string               `json:"confidence,omitempty"`
	EntityCount     int                  `json:"entityCount,omitempty"`
	ConflictCount   int                  `json:"conflictCount,omitempty"`
	EntityConflicts []JSONEntityConflict `json:"entityConflicts,omitempty"`
	Diagnostics     []JSONDiagnostic     `json:"diagnostics,omitempty"`
}

// JSONEntityConflict represents a single entity-level conflict within a file.
type JSONEntityConflict struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// JSONDiagnostic represents a merge rule diagnostic in JSON output.
type JSONDiagnostic struct {
	Severity string `json:"severity"`
	Entity   string `json:"entity,omitempty"`
	Message  string `json:"message"`
	Rule     string `json:"rule"`
}

// --- Show ---

// JSONShowOutput is the top-level JSON output for "graft show --json".
type JSONShowOutput struct {
	SchemaVersion int              `json:"schemaVersion,omitempty"`
	Hash          string           `json:"hash"`
	Author        string           `json:"author"`
	Date          string           `json:"date"`
	Timestamp     int64            `json:"timestamp"`
	Message       string           `json:"message"`
	Parents       []string         `json:"parents,omitempty"`
	Changes       []JSONShowChange `json:"changes,omitempty"`
}

// JSONShowChange represents a file changed in a commit.
type JSONShowChange struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "A" (added), "D" (deleted), "M" (modified)
}

// --- Blame ---

// JSONBlameOutput is the JSON output for "graft blame --entity --json".
type JSONBlameOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	Path          string `json:"path"`
	EntityKey     string `json:"entityKey"`
	Author        string `json:"author"`
	CommitHash    string `json:"commitHash"`
	Message       string `json:"message"`
}

// JSONBatchBlameOutput is the JSON output for "graft blame <path> --json".
type JSONBatchBlameOutput struct {
	SchemaVersion int               `json:"schemaVersion,omitempty"`
	Path          string            `json:"path"`
	Entities      []JSONBlameOutput `json:"entities"`
}

// --- Conflicts ---

// JSONConflictsOutput is the top-level JSON output for "graft conflicts --json".
type JSONConflictsOutput struct {
	SchemaVersion int                `json:"schemaVersion,omitempty"`
	Files         []JSONConflictFile `json:"files"`
}

// JSONConflictFile represents a file with conflicts.
type JSONConflictFile struct {
	Path     string               `json:"path"`
	Entities []JSONConflictEntity `json:"entities"`
}

// JSONConflictEntity represents a single entity conflict within a file.
type JSONConflictEntity struct {
	EntityName   string `json:"entityName,omitempty"`
	EntityKey    string `json:"entityKey,omitempty"`
	EntityKind   string `json:"entityKind,omitempty"`
	ConflictType string `json:"conflictType"`
}

// --- Verify ---

// JSONVerifyOutput is the top-level JSON output for "graft verify --json".
type JSONVerifyOutput struct {
	SchemaVersion  int                        `json:"schemaVersion,omitempty"`
	OK             bool                       `json:"ok"`
	Results        []JSONVerifyResult         `json:"results,omitempty"`
	Checked        int                        `json:"checked,omitempty"`
	Valid          int                        `json:"valid,omitempty"`
	Unsigned       int                        `json:"unsigned,omitempty"`
	Invalid        int                        `json:"invalid,omitempty"`
	RequireSigned  bool                       `json:"requireSigned,omitempty"`
	AllowedSigners bool                       `json:"allowedSigners,omitempty"`
	LooseObjects   int                        `json:"looseObjects,omitempty"`
	PackFiles      int                        `json:"packFiles,omitempty"`
	PackObjects    int                        `json:"packObjects,omitempty"`
	Diagnostics    []JSONRepositoryDiagnostic `json:"diagnostics,omitempty"`
}

// JSONVerifyResult represents the signature verification result for a single commit.
type JSONVerifyResult struct {
	CommitHash string `json:"commitHash"`
	Valid      bool   `json:"valid"`
	Unsigned   bool   `json:"unsigned,omitempty"`
	SignerKey  string `json:"signerKey,omitempty"`
	Algorithm  string `json:"algorithm,omitempty"`
	Error      string `json:"error,omitempty"`
}

// JSONTagVerifyOutput is the JSON output for "graft tag --verify --json".
type JSONTagVerifyOutput struct {
	SchemaVersion  int    `json:"schemaVersion,omitempty"`
	OK             bool   `json:"ok"`
	TagName        string `json:"tagName"`
	TagHash        string `json:"tagHash"`
	TargetHash     string `json:"targetHash"`
	Valid          bool   `json:"valid"`
	Unsigned       bool   `json:"unsigned,omitempty"`
	SignerKey      string `json:"signerKey,omitempty"`
	Algorithm      string `json:"algorithm,omitempty"`
	Error          string `json:"error,omitempty"`
	RequireSigned  bool   `json:"requireSigned,omitempty"`
	AllowedSigners bool   `json:"allowedSigners,omitempty"`
}

type JSONRepositoryDiagnostic struct {
	Severity  string `json:"severity"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Path      string `json:"path,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Object    string `json:"object,omitempty"`
	Repair    string `json:"repair,omitempty"`
	Operation string `json:"operation,omitempty"`
}

type JSONDoctorOutput struct {
	SchemaVersion int                        `json:"schemaVersion,omitempty"`
	OK            bool                       `json:"ok"`
	LooseObjects  int                        `json:"looseObjects,omitempty"`
	PackFiles     int                        `json:"packFiles,omitempty"`
	PackObjects   int                        `json:"packObjects,omitempty"`
	Diagnostics   []JSONRepositoryDiagnostic `json:"diagnostics,omitempty"`
}

type JSONDoctorGlobalOutput struct {
	SchemaVersion                  int                               `json:"schemaVersion,omitempty"`
	OK                             bool                              `json:"ok"`
	GeneratedAt                    string                            `json:"generatedAt"`
	Version                        string                            `json:"version"`
	Commit                         string                            `json:"commit"`
	BuildTime                      string                            `json:"buildTime"`
	GoVersion                      string                            `json:"goVersion"`
	OS                             string                            `json:"os"`
	Arch                           string                            `json:"arch"`
	SupportedRepositoryFormat      int                               `json:"supportedRepositoryFormat"`
	SupportedRemoteProtocolVersion string                            `json:"supportedRemoteProtocolVersion"`
	Git                            JSONDoctorGlobalTool              `json:"git"`
	UserConfig                     JSONDoctorBundleUserConfig        `json:"userConfig"`
	Diagnostics                    []JSONRepositoryDiagnostic        `json:"diagnostics,omitempty"`
	CollectionErrors               []JSONDoctorBundleCollectionError `json:"collectionErrors,omitempty"`
}

type JSONDoctorGlobalTool struct {
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

type JSONDoctorBundleOutput struct {
	SchemaVersion    int                               `json:"schemaVersion,omitempty"`
	GeneratedAt      string                            `json:"generatedAt"`
	Repository       JSONDoctorBundleRepository        `json:"repository"`
	UserConfig       JSONDoctorBundleUserConfig        `json:"userConfig"`
	Hooks            JSONDoctorBundleHooks             `json:"hooks"`
	Verify           JSONDoctorOutput                  `json:"verify"`
	GitShadow        JSONRepairGitShadowOutput         `json:"gitShadow"`
	RecentReflog     []JSONDoctorBundleReflogEntry     `json:"recentReflog,omitempty"`
	Environment      JSONDoctorBundleEnvironment       `json:"environment"`
	Protocol         JSONDoctorBundleProtocol          `json:"protocol"`
	CollectionErrors []JSONDoctorBundleCollectionError `json:"collectionErrors,omitempty"`
	Redaction        JSONDoctorBundleRedaction         `json:"redaction"`
}

type JSONDoctorBundleRepository struct {
	RepositoryFormatVersion int                      `json:"repositoryFormatVersion"`
	ObjectHash              string                   `json:"objectHash,omitempty"`
	Features                map[string]bool          `json:"features,omitempty"`
	CurrentBranch           string                   `json:"currentBranch,omitempty"`
	Head                    string                   `json:"head,omitempty"`
	Remotes                 []JSONDoctorBundleRemote `json:"remotes,omitempty"`
	User                    JSONDoctorBundleRepoUser `json:"user"`
}

type JSONDoctorBundleRepoUser struct {
	NameConfigured  bool `json:"nameConfigured"`
	EmailConfigured bool `json:"emailConfigured"`
}

type JSONDoctorBundleRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type JSONDoctorBundleUserConfig struct {
	Loaded               bool                             `json:"loaded"`
	ConfigFilePresent    bool                             `json:"configFilePresent"`
	ConfigFileMode       string                           `json:"configFileMode,omitempty"`
	ConfigFileSecure     bool                             `json:"configFileSecure"`
	ConfigFileWarning    string                           `json:"configFileWarning,omitempty"`
	ConfigFileRepair     string                           `json:"configFileRepair,omitempty"`
	NameConfigured       bool                             `json:"nameConfigured"`
	EmailConfigured      bool                             `json:"emailConfigured"`
	DefaultOrchardURL    string                           `json:"defaultOrchardUrl,omitempty"`
	TokenSet             bool                             `json:"tokenSet"`
	UsernameConfigured   bool                             `json:"usernameConfigured"`
	OwnerConfigured      bool                             `json:"ownerConfigured"`
	SigningKeyConfigured bool                             `json:"signingKeyConfigured"`
	AutoSign             bool                             `json:"autoSign"`
	Workspaces           int                              `json:"workspaces,omitempty"`
	Profiles             []JSONDoctorBundleOrchardProfile `json:"profiles,omitempty"`
	LoadError            string                           `json:"loadError,omitempty"`
}

type JSONDoctorBundleOrchardProfile struct {
	Host               string `json:"host"`
	TokenSet           bool   `json:"tokenSet"`
	UsernameConfigured bool   `json:"usernameConfigured"`
	OwnerConfigured    bool   `json:"ownerConfigured"`
}

type JSONDoctorBundleHooks struct {
	Configured       bool     `json:"configured"`
	Trusted          bool     `json:"trusted"`
	TrustedAt        string   `json:"trustedAt,omitempty"`
	HooksTomlPresent bool     `json:"hooksTomlPresent"`
	ExecutableHooks  []string `json:"executableHooks,omitempty"`
	Warning          string   `json:"warning,omitempty"`
	Repair           string   `json:"repair,omitempty"`
}

type JSONDoctorBundleReflogEntry struct {
	Ref       string `json:"ref"`
	OldHash   string `json:"oldHash,omitempty"`
	NewHash   string `json:"newHash,omitempty"`
	Timestamp int64  `json:"timestamp"`
	Reason    string `json:"reason,omitempty"`
}

type JSONDoctorBundleEnvironment struct {
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

type JSONDoctorBundleProtocol struct {
	SupportedRepositoryFormat      int                                     `json:"supportedRepositoryFormat"`
	SupportedRemoteProtocolVersion string                                  `json:"supportedRemoteProtocolVersion"`
	Documentation                  string                                  `json:"documentation"`
	ClientCapabilities             []string                                `json:"clientCapabilities,omitempty"`
	DefinedCapabilities            []string                                `json:"definedCapabilities,omitempty"`
	ServerLimitKeys                []string                                `json:"serverLimitKeys,omitempty"`
	ResponseLimits                 []JSONDoctorBundleProtocolResponseLimit `json:"responseLimits,omitempty"`
	Diagnostics                    []JSONRepositoryDiagnostic              `json:"diagnostics,omitempty"`
	TransportCount                 int                                     `json:"transportCount"`
	EndpointCount                  int                                     `json:"endpointCount"`
}

type JSONDoctorBundleProtocolResponseLimit struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

type JSONDoctorBundleCollectionError struct {
	Section string `json:"section"`
	Error   string `json:"error"`
}

type JSONDoctorBundleRedaction struct {
	SecretsIncluded bool     `json:"secretsIncluded"`
	SourceIncluded  bool     `json:"sourceIncluded"`
	Notes           []string `json:"notes,omitempty"`
}

type JSONRepairGitShadowOutput struct {
	SchemaVersion     int    `json:"schemaVersion,omitempty"`
	OK                bool   `json:"ok"`
	State             string `json:"state"`
	Message           string `json:"message,omitempty"`
	HasGitDir         bool   `json:"hasGitDir"`
	HasFailures       bool   `json:"hasFailures,omitempty"`
	GraftHead         string `json:"graftHead,omitempty"`
	ExpectedGitCommit string `json:"expectedGitCommit,omitempty"`
	ExpectedGitTree   string `json:"expectedGitTree,omitempty"`
	ActualGitCommit   string `json:"actualGitCommit,omitempty"`
	ActualGitTree     string `json:"actualGitTree,omitempty"`
	Repair            string `json:"repair,omitempty"`
}

type JSONRepairLockOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	OK            bool   `json:"ok"`
	State         string `json:"state"`
	Message       string `json:"message,omitempty"`
	Path          string `json:"path,omitempty"`
	Operation     string `json:"operation,omitempty"`
	PID           int    `json:"pid,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	Command       string `json:"command,omitempty"`
	StartedAt     string `json:"startedAt,omitempty"`
	Stale         bool   `json:"stale,omitempty"`
	Cleared       bool   `json:"cleared,omitempty"`
	Repair        string `json:"repair,omitempty"`
}

type JSONRepairTransactionOutput struct {
	SchemaVersion int                          `json:"schemaVersion,omitempty"`
	OK            bool                         `json:"ok"`
	ID            string                       `json:"id,omitempty"`
	Operation     string                       `json:"operation,omitempty"`
	Status        string                       `json:"status,omitempty"`
	StartedAt     string                       `json:"startedAt,omitempty"`
	UpdatedAt     string                       `json:"updatedAt,omitempty"`
	Error         string                       `json:"error,omitempty"`
	TouchedRefs   []JSONTransactionRefMutation `json:"touchedRefs,omitempty"`
	TouchedFiles  []string                     `json:"touchedFiles,omitempty"`
	Message       string                       `json:"message,omitempty"`
	Repair        string                       `json:"repair,omitempty"`
}

type JSONRepairMigrateConfigOutput struct {
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	OK            bool   `json:"ok"`
	Migrated      bool   `json:"migrated"`
	Path          string `json:"path,omitempty"`
	FromVersion   int    `json:"fromVersion"`
	ToVersion     int    `json:"toVersion"`
	Message       string `json:"message,omitempty"`
}

type JSONTransactionRefMutation struct {
	Ref     string `json:"ref"`
	OldHash string `json:"oldHash,omitempty"`
	NewHash string `json:"newHash,omitempty"`
}

// JSONVerifyPushLimitsOutput is the JSON output for "graft verify push-limits --json".
type JSONVerifyPushLimitsOutput struct {
	SchemaVersion   int                     `json:"schemaVersion,omitempty"`
	OK              bool                    `json:"ok"`
	PushTarget      string                  `json:"pushTarget,omitempty"`
	Remote          string                  `json:"remote,omitempty"`
	LocalRef        string                  `json:"localRef,omitempty"`
	RemoteRef       string                  `json:"remoteRef,omitempty"`
	LocalHash       string                  `json:"localHash,omitempty"`
	RemoteHash      string                  `json:"remoteHash,omitempty"`
	LimitBytes      int64                   `json:"limitBytes"`
	ObjectsExamined int                     `json:"objectsExamined"`
	TotalBytes      int64                   `json:"totalBytes,omitempty"`
	Largest         *JSONVerifySizedObject  `json:"largest,omitempty"`
	Blockers        []JSONVerifySizedObject `json:"blockers,omitempty"`
}

// JSONVerifySizedObject describes one object in push-limit output.
type JSONVerifySizedObject struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"shortHash,omitempty"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"sizeBytes"`
}

// --- Release ---

// JSONReleaseManifestOutput is the JSON output for "graft release manifest --json".
type JSONReleaseManifestOutput struct {
	SchemaVersion                  int                       `json:"schemaVersion,omitempty"`
	GeneratedAt                    string                    `json:"generatedAt"`
	Version                        string                    `json:"version"`
	Commit                         string                    `json:"commit"`
	BuildTime                      string                    `json:"buildTime"`
	GoVersion                      string                    `json:"goVersion"`
	SupportedRepositoryFormat      int                       `json:"supportedRepositoryFormat"`
	SupportedRemoteProtocolVersion string                    `json:"supportedRemoteProtocolVersion"`
	Files                          []JSONReleaseManifestFile `json:"files"`
}

// JSONReleaseManifestFile describes one checksummed artifact.
type JSONReleaseManifestFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

// JSONReleaseManifestVerificationOutput is the JSON output for "graft release verify-manifest --json".
type JSONReleaseManifestVerificationOutput struct {
	SchemaVersion  int                                   `json:"schemaVersion,omitempty"`
	OK             bool                                  `json:"ok"`
	ManifestPath   string                                `json:"manifestPath"`
	ManifestFormat string                                `json:"manifestFormat"`
	BaseDir        string                                `json:"baseDir"`
	Checked        int                                   `json:"checked"`
	Matched        int                                   `json:"matched"`
	Missing        int                                   `json:"missing"`
	Mismatched     int                                   `json:"mismatched"`
	Errors         int                                   `json:"errors"`
	Results        []JSONReleaseManifestVerificationFile `json:"results"`
}

// JSONReleaseManifestVerificationFile describes one artifact verification result.
type JSONReleaseManifestVerificationFile struct {
	Path              string `json:"path"`
	OK                bool   `json:"ok"`
	Status            string `json:"status"`
	ExpectedSizeBytes int64  `json:"expectedSizeBytes"`
	ActualSizeBytes   *int64 `json:"actualSizeBytes,omitempty"`
	ExpectedSHA256    string `json:"expectedSha256"`
	ActualSHA256      string `json:"actualSha256,omitempty"`
	Error             string `json:"error,omitempty"`
}

// JSONReleaseCheckOutput is the JSON output for "graft release check --json".
type JSONReleaseCheckOutput struct {
	SchemaVersion int                      `json:"schemaVersion,omitempty"`
	OK            bool                     `json:"ok"`
	Version       string                   `json:"version"`
	ChangelogPath string                   `json:"changelogPath"`
	Checks        []JSONReleaseCheckResult `json:"checks"`
}

// JSONReleaseCheckResult describes one release preflight check.
type JSONReleaseCheckResult struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// JSONReleaseSignOutput is the JSON output for "graft release sign".
type JSONReleaseSignOutput struct {
	SchemaVersion   int                        `json:"schemaVersion,omitempty"`
	SignedAt        string                     `json:"signedAt"`
	SignatureFormat string                     `json:"signatureFormat"`
	PayloadFormat   string                     `json:"payloadFormat"`
	Files           []JSONReleaseSignatureFile `json:"files"`
}

// JSONReleaseSignatureFile describes one signed release artifact.
type JSONReleaseSignatureFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

// JSONReleaseVerifySignatureOutput is the JSON output for "graft release verify-signature --json".
type JSONReleaseVerifySignatureOutput struct {
	SchemaVersion int                                      `json:"schemaVersion,omitempty"`
	OK            bool                                     `json:"ok"`
	SignaturePath string                                   `json:"signaturePath"`
	BaseDir       string                                   `json:"baseDir"`
	Checked       int                                      `json:"checked"`
	Valid         int                                      `json:"valid"`
	Missing       int                                      `json:"missing"`
	Mismatched    int                                      `json:"mismatched"`
	Invalid       int                                      `json:"invalid"`
	Errors        int                                      `json:"errors"`
	Results       []JSONReleaseSignatureVerificationResult `json:"results"`
}

// JSONReleaseSignatureVerificationResult describes one artifact signature verification result.
type JSONReleaseSignatureVerificationResult struct {
	Path      string `json:"path"`
	OK        bool   `json:"ok"`
	Status    string `json:"status"`
	SignerKey string `json:"signerKey,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
	Error     string `json:"error,omitempty"`
}

// --- Grep ---

// JSONLineGrepOutput is the top-level JSON output for "graft grep --line --json".
type JSONLineGrepOutput struct {
	SchemaVersion int                  `json:"schemaVersion,omitempty"`
	Results       []JSONLineGrepResult `json:"results"`
}

// JSONLineGrepResult represents a single line grep result.
type JSONLineGrepResult struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// JSONEntitySearchOutput is the top-level JSON output for "graft grep --entity --json".
type JSONEntitySearchOutput struct {
	SchemaVersion int                      `json:"schemaVersion,omitempty"`
	Results       []JSONEntitySearchResult `json:"results"`
}

// JSONEntitySearchResult represents a single entity match.
type JSONEntitySearchResult struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	DeclKind string `json:"declKind"`
	Key      string `json:"key"`
}
