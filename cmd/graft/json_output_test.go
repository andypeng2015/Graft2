package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/repo"
)

// TestWriteJSON verifies writeJSON produces pretty-printed JSON with the correct structure.
func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"key": "value"}
	if err := writeJSON(&buf, data); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "\"key\": \"value\"") {
		t.Fatalf("expected pretty-printed JSON, got: %s", got)
	}
	// Verify it's valid JSON.
	var parsed map[string]string
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed["key"] != "value" {
		t.Fatalf("parsed key = %q, want %q", parsed["key"], "value")
	}
}

// TestWriteJSON_Struct verifies writeJSON works with a typed struct and camelCase tags.
func TestWriteJSON_Struct(t *testing.T) {
	var buf bytes.Buffer
	data := JSONStatusOutput{
		Branch:    "main",
		NoCommits: true,
	}
	if err := writeJSON(&buf, data); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	var parsed JSONStatusOutput
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed.Branch != "main" {
		t.Fatalf("branch = %q, want %q", parsed.Branch, "main")
	}
	if !parsed.NoCommits {
		t.Fatal("noCommits = false, want true")
	}
}

func TestWriteJSONDefaultsSchemaVersion(t *testing.T) {
	var buf bytes.Buffer
	data := JSONStatusOutput{
		Branch:    "main",
		NoCommits: true,
	}
	if err := writeJSON(&buf, data); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	var parsed JSONStatusOutput
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, buf.String())
	}
	if parsed.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", parsed.SchemaVersion, JSONSchemaVersion)
	}
}

func TestJSONTopLevelContractsHaveSchemaVersion(t *testing.T) {
	contracts := []any{
		JSONVersionOutput{},
		JSONProtocolOutput{},
		JSONRemoteOutput{},
		JSONWorkspacesOutput{},
		JSONWorkspaceMutationOutput{},
		JSONAuthDoctorOutput{},
		JSONCoorddGuardDoctorOutput{},
		JSONCoorddEventOutput{},
		JSONCoorddTailOutput{},
		JSONCoorddSnapshotOutput{},
		JSONCoorddActionDecisionOutput{},
		JSONCoorddExecOutput{},
		JSONMCPExecOutput{},
		JSONCoorddSpawnOutput{},
		JSONCoorddSpawnRecordOutput{},
		JSONCoorddSpawnViewOutput{},
		JSONCoorddSpawnTraceRawOutput{},
		JSONCoorddSpawnTraceOutput{},
		JSONCoorddSpawnsOutput{},
		JSONCoorddGuardShowOutput{},
		JSONCoorddGuardOverridesOutput{},
		JSONWorkonOutput{},
		JSONContextOutput{},
		JSONStatusOutput{},
		JSONCoordStatusOutput{},
		JSONCoordAgentsOutput{},
		JSONCoordClaimsOutput{},
		JSONCoordFeedOutput{},
		JSONCoordDecisionsOutput{},
		JSONCoordHeartbeatOutput{},
		JSONCoordSessionsOutput{},
		JSONCoordPresenceOutput{},
		JSONCoordReadingOutput{},
		JSONCoordNotesOutput{},
		JSONCoordNoteOutput{},
		JSONCoordNoteDeleteOutput{},
		JSONCoordTasksOutput{},
		JSONCoordTaskOutput{},
		JSONCoordTaskClaimOutput{},
		JSONCoordTaskDeleteOutput{},
		JSONCoordPlansOutput{},
		JSONCoordPlanOutput{},
		JSONCoordPlanDeleteOutput{},
		JSONCoordWatchOutput{},
		JSONCoordUnwatchOutput{},
		JSONCoordResolveOutput{},
		JSONCoordPublishOutput{},
		JSONCoordImpactOutput{},
		JSONCoordDiffOutput{},
		JSONCoordXrefsOutput{},
		JSONCoordGraphOutput{},
		JSONCoordCheckOutput{},
		JSONCoordCleanupStaleOutput{},
		JSONDiffOutput{},
		JSONLogOutput{},
		JSONReflogOutput{},
		JSONMergeOutput{},
		JSONShowOutput{},
		JSONBlameOutput{},
		JSONBatchBlameOutput{},
		JSONConflictsOutput{},
		JSONVerifyOutput{},
		JSONTagVerifyOutput{},
		JSONDoctorOutput{},
		JSONDoctorGlobalOutput{},
		JSONDoctorBundleOutput{},
		JSONRepairGitShadowOutput{},
		JSONRepairLockOutput{},
		JSONRepairTransactionOutput{},
		JSONRepairMigrateConfigOutput{},
		JSONVerifyPushLimitsOutput{},
		JSONReleaseManifestOutput{},
		JSONReleaseManifestVerificationOutput{},
		JSONReleaseCheckOutput{},
		JSONReleaseSignOutput{},
		JSONReleaseVerifySignatureOutput{},
		JSONLineGrepOutput{},
		JSONEntitySearchOutput{},
		JSONStructuralGrepOutput{},
		JSONHistoryGrepOutput{},
		checkIgnoreOutput{},
	}

	for _, contract := range contracts {
		t.Run(reflect.TypeOf(contract).Name(), func(t *testing.T) {
			fields := jsonFieldSignatures(reflect.TypeOf(contract))
			if got := fields["schemaVersion"]; got != "int" {
				t.Fatalf("schemaVersion signature = %q, want int", got)
			}
		})
	}
}

func TestJSONCompatibilityMinimumContracts(t *testing.T) {
	contracts := map[string]struct {
		sample any
		fields map[string]string
	}{
		"version": {JSONVersionOutput{}, map[string]string{
			"schemaVersion":                  "int",
			"version":                        "string",
			"commit":                         "string",
			"buildTime":                      "string",
			"goVersion":                      "string",
			"supportedRepositoryFormat":      "int",
			"supportedRemoteProtocolVersion": "string",
		}},
		"protocol": {JSONProtocolOutput{}, map[string]string{
			"schemaVersion":       "int",
			"protocolVersion":     "string",
			"documentation":       "string",
			"baseUrlFormat":       "string",
			"defaultOrchardHost":  "string",
			"hashFunction":        "string",
			"headers":             "[]ProtocolHeader",
			"clientCapabilities":  "[]string",
			"definedCapabilities": "[]ProtocolCapability",
			"transports":          "[]ProtocolTransport",
			"serverLimits":        "[]ProtocolLimit",
			"responseLimits":      "[]ProtocolResponseLimit",
			"endpoints":           "[]ProtocolEndpoint",
			"objectTypes":         "[]string",
			"errorShape":          "ProtocolErrorShape",
		}},
		"remote": {JSONRemoteOutput{}, map[string]string{
			"schemaVersion": "int",
			"remotes":       "[]JSONRemoteEntry",
		}},
		"workspaces": {JSONWorkspacesOutput{}, map[string]string{
			"schemaVersion": "int",
			"workspaces":    "map[string]string",
		}},
		"workspaceMutation": {JSONWorkspaceMutationOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"name":          "string",
			"path":          "string",
		}},
		"authDoctor": {JSONAuthDoctorOutput{}, map[string]string{
			"schemaVersion":     "int",
			"ok":                "bool",
			"selectedHost":      "string",
			"configPath":        "string",
			"configFilePresent": "bool",
			"configFileMode":    "string",
			"configFileSecure":  "bool",
			"tokenSet":          "bool",
			"tokenSource":       "string",
			"tokenExpiryKnown":  "bool",
			"tokenExpiresAt":    "string",
			"tokenExpired":      "bool",
			"hosts":             "[]JSONAuthDoctorHost",
			"diagnostics":       "[]JSONRepositoryDiagnostic",
		}},
		"coorddGuardDoctor": {JSONCoorddGuardDoctorOutput{}, map[string]string{
			"schemaVersion": "int",
			"ok":            "bool",
			"health":        "SandboxBackendHealthReport",
			"diagnostics":   "[]JSONRepositoryDiagnostic",
		}},
		"coorddEvent": {JSONCoorddEventOutput{}, map[string]string{
			"schemaVersion": "int",
			"id":            "string",
			"type":          "string",
			"timestamp":     "Time",
		}},
		"coorddTail": {JSONCoorddTailOutput{}, map[string]string{
			"schemaVersion": "int",
			"events":        "[]Event",
		}},
		"coorddSnapshot": {JSONCoorddSnapshotOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"id":            "string",
			"created_at":    "Time",
			"summary":       "WorktreeSummary",
			"entries":       "[]SnapshotEntry",
		}},
		"coorddActionDecision": {JSONCoorddActionDecisionOutput{}, map[string]string{
			"schemaVersion": "int",
			"input":         "ActionPolicyInput",
			"decision":      "*ActionPolicyDecision",
		}},
		"coorddExec": {JSONCoorddExecOutput{}, map[string]string{
			"schemaVersion":     "int",
			"decision":          "*ActionPolicyDecision",
			"exit_code":         "int",
			"backend":           "string",
			"requested_profile": "RuntimeProfile",
			"effective_profile": "RuntimeProfile",
		}},
		"mcpExec": {JSONMCPExecOutput{}, map[string]string{
			"schemaVersion": "int",
			"input":         "ActionPolicyInput",
			"decision":      "*ActionPolicyDecision",
			"allowed":       "bool",
			"exec":          "*ExecResult",
			"stdout":        "string",
			"stderr":        "string",
			"exit_code":     "int",
			"status":        "string",
			"error":         "string",
		}},
		"coorddSpawn": {JSONCoorddSpawnOutput{}, map[string]string{
			"schemaVersion": "int",
			"action_input":  "ActionPolicyInput",
			"spawn_input":   "SpawnPolicyInput",
			"record":        "*SpawnRecord",
			"status":        "string",
			"exit_code":     "int",
			"error":         "string",
		}},
		"coorddSpawnRecord": {JSONCoorddSpawnRecordOutput{}, map[string]string{
			"schemaVersion": "int",
			"id":            "string",
			"name":          "string",
			"status":        "string",
			"started_at":    "Time",
		}},
		"coorddSpawnView": {JSONCoorddSpawnViewOutput{}, map[string]string{
			"schemaVersion": "int",
			"record":        "*SpawnRecord",
			"lease":         "*SpawnLease",
		}},
		"coorddSpawnTraceRaw": {JSONCoorddSpawnTraceRawOutput{}, map[string]string{
			"schemaVersion": "int",
			"record":        "*SpawnRecord",
			"lease":         "*SpawnLease",
			"execs":         "[]ExecTrace",
			"events":        "[]Event",
		}},
		"coorddSpawnTrace": {JSONCoorddSpawnTraceOutput{}, map[string]string{
			"schemaVersion":        "int",
			"record":               "*SpawnRecord",
			"execs":                "[]ExecTraceView",
			"phases":               "[]TracePhaseView",
			"raw_event_count":      "int",
			"rendered_event_count": "int",
		}},
		"coorddSpawns": {JSONCoorddSpawnsOutput{}, map[string]string{
			"schemaVersion": "int",
			"spawns":        "[]SpawnRecord",
		}},
		"coorddGuardShow": {JSONCoorddGuardShowOutput{}, map[string]string{
			"schemaVersion": "int",
			"config":        "*GuardConfig",
			"overrides":     "[]coorddRuleOverrideView",
			"bundle_ids":    "map[string]string",
			"policies":      "map[string]PolicyBundleInfo",
		}},
		"coorddGuardOverrides": {JSONCoorddGuardOverridesOutput{}, map[string]string{
			"schemaVersion": "int",
			"overrides":     "[]coorddRuleOverrideView",
		}},
		"workon": {JSONWorkonOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"agent_id":      "string",
			"agent_name":    "string",
			"workspace":     "string",
			"mode":          "string",
			"scope":         "string",
			"notify":        "string",
			"agents":        "int",
			"claims":        "int",
			"discovered":    "map[string]string",
			"recovered":     "bool",
		}},
		"context": {JSONContextOutput{}, map[string]string{
			"schemaVersion": "int",
			"target":        "ContextSection",
			"dependencies":  "[]ContextSection",
			"dependents":    "[]ContextSection",
			"budget_tokens": "int",
			"used_tokens":   "int",
			"truncated":     "bool",
		}},
		"status": {JSONStatusOutput{}, map[string]string{
			"schemaVersion":  "int",
			"branch":         "string",
			"noCommits":      "bool",
			"shadow_desync":  "bool",
			"shadow_state":   "string",
			"shadow_message": "string",
			"shadow_repair":  "string",
			"conflicts":      "[]JSONStatusEntry",
			"staged":         "[]JSONStatusEntry",
			"unstaged":       "[]JSONStatusEntry",
			"untracked":      "[]string",
		}},
		"coordAgents": {JSONCoordAgentsOutput{}, map[string]string{
			"schemaVersion": "int",
			"agents":        "[]AgentInfo",
		}},
		"coordStatus": {JSONCoordStatusOutput{}, map[string]string{
			"schemaVersion": "int",
			"agents":        "int",
			"claims":        "int",
			"conflicts":     "int",
			"feed_count":    "int",
			"notes":         "int",
			"tasks":         "int",
			"tasks_pending": "int",
			"tasks_active":  "int",
		}},
		"coordClaims": {JSONCoordClaimsOutput{}, map[string]string{
			"schemaVersion": "int",
			"claims":        "[]JSONCoordClaim",
		}},
		"coordFeed": {JSONCoordFeedOutput{}, map[string]string{
			"schemaVersion": "int",
			"events":        "[]JSONCoordFeedEntry",
		}},
		"coordDecisions": {JSONCoordDecisionsOutput{}, map[string]string{
			"schemaVersion": "int",
			"decisions":     "[]DecisionGraph",
		}},
		"coordHeartbeat": {JSONCoordHeartbeatOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"agent_id":      "string",
		}},
		"coordSessions": {JSONCoordSessionsOutput{}, map[string]string{
			"schemaVersion": "int",
			"sessions":      "[]Session",
		}},
		"coordPresence": {JSONCoordPresenceOutput{}, map[string]string{
			"schemaVersion": "int",
			"entries":       "[]PresenceEntry",
		}},
		"coordReading": {JSONCoordReadingOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"file":          "string",
			"agent_id":      "string",
			"entity":        "string",
		}},
		"coordNotes": {JSONCoordNotesOutput{}, map[string]string{
			"schemaVersion": "int",
			"notes":         "[]*Note",
		}},
		"coordNote": {JSONCoordNoteOutput{}, map[string]string{
			"schemaVersion": "int",
			"note":          "*Note",
		}},
		"coordNoteDelete": {JSONCoordNoteDeleteOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"id":            "string",
		}},
		"coordTasks": {JSONCoordTasksOutput{}, map[string]string{
			"schemaVersion": "int",
			"tasks":         "[]JSONCoordTaskEntry",
		}},
		"coordTask": {JSONCoordTaskOutput{}, map[string]string{
			"schemaVersion": "int",
			"task":          "*Task",
		}},
		"coordTaskClaim": {JSONCoordTaskClaimOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"task_id":       "string",
			"assigned_to":   "string",
			"task_status":   "string",
		}},
		"coordTaskDelete": {JSONCoordTaskDeleteOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"id":            "string",
		}},
		"coordPlans": {JSONCoordPlansOutput{}, map[string]string{
			"schemaVersion": "int",
			"plans":         "[]*Plan",
		}},
		"coordPlan": {JSONCoordPlanOutput{}, map[string]string{
			"schemaVersion": "int",
			"plan":          "*Plan",
		}},
		"coordPlanDelete": {JSONCoordPlanDeleteOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"id":            "string",
		}},
		"coordWatch": {JSONCoordWatchOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"entity_key":    "string",
			"file":          "string",
		}},
		"coordUnwatch": {JSONCoordUnwatchOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"entity_key":    "string",
		}},
		"coordResolve": {JSONCoordResolveOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"key_hash":      "string",
			"to_agent":      "string",
		}},
		"coordPublish": {JSONCoordPublishOutput{}, map[string]string{
			"schemaVersion": "int",
			"status":        "string",
			"commit_hash":   "string",
			"agent_id":      "string",
		}},
		"coordImpact": {JSONCoordImpactOutput{}, map[string]string{
			"schemaVersion": "int",
			"workspaces":    "map[string]WorkspaceImpact",
		}},
		"coordDiff": {JSONCoordDiffOutput{}, map[string]string{
			"schemaVersion": "int",
			"agent":         "*AgentInfo",
			"claims":        "[]ClaimInfo",
		}},
		"coordXrefs": {JSONCoordXrefsOutput{}, map[string]string{
			"schemaVersion": "int",
			"references":    "[]XrefCallSite",
		}},
		"coordGraph": {JSONCoordGraphOutput{}, map[string]string{
			"schemaVersion": "int",
			"workspaces":    "map[string]string",
			"edges":         "[]JSONCoordGraphEdge",
		}},
		"coordCheck": {JSONCoordCheckOutput{}, map[string]string{
			"schemaVersion":      "int",
			"ok":                 "bool",
			"active_agent_id":    "string",
			"agents_examined":    "int",
			"claims_examined":    "int",
			"active_claims":      "[]JSONCoordCheckClaim",
			"stale_agents":       "[]JSONCoordCheckAgent",
			"unread_feed_events": "[]JSONCoordCheckFeedEvent",
			"conflicts":          "[]JSONCoordCheckConflict",
			"readers":            "[]JSONCoordCheckReader",
		}},
		"coordCleanupStale": {JSONCoordCleanupStaleOutput{}, map[string]string{
			"schemaVersion": "int",
			"ok":            "bool",
			"dry_run":       "bool",
			"removed":       "int",
			"stale_agents":  "[]JSONCoordCheckAgent",
		}},
		"diff": {JSONDiffOutput{}, map[string]string{
			"schemaVersion": "int",
			"files":         "[]JSONDiffFile",
			"entityChanges": "[]JSONDiffEntityChange",
		}},
		"log": {JSONLogOutput{}, map[string]string{
			"schemaVersion": "int",
			"commits":       "[]JSONLogEntry",
		}},
		"reflog": {JSONReflogOutput{}, map[string]string{
			"schemaVersion": "int",
			"ref":           "string",
			"entries":       "[]JSONReflogEntry",
		}},
		"merge": {JSONMergeOutput{}, map[string]string{
			"schemaVersion":  "int",
			"action":         "string",
			"source":         "string",
			"target":         "string",
			"isFastForward":  "bool",
			"hasConflicts":   "bool",
			"totalConflicts": "int",
			"mergeCommit":    "string",
			"files":          "[]JSONMergeFile",
			"message":        "string",
		}},
		"show": {JSONShowOutput{}, map[string]string{
			"schemaVersion": "int",
			"hash":          "string",
			"author":        "string",
			"date":          "string",
			"timestamp":     "int64",
			"message":       "string",
			"parents":       "[]string",
			"changes":       "[]JSONShowChange",
		}},
		"blameEntity": {JSONBlameOutput{}, map[string]string{
			"schemaVersion": "int",
			"path":          "string",
			"entityKey":     "string",
			"author":        "string",
			"commitHash":    "string",
			"message":       "string",
		}},
		"blameBatch": {JSONBatchBlameOutput{}, map[string]string{
			"schemaVersion": "int",
			"path":          "string",
			"entities":      "[]JSONBlameOutput",
		}},
		"conflicts": {JSONConflictsOutput{}, map[string]string{
			"schemaVersion": "int",
			"files":         "[]JSONConflictFile",
		}},
		"verify": {JSONVerifyOutput{}, map[string]string{
			"schemaVersion":  "int",
			"ok":             "bool",
			"results":        "[]JSONVerifyResult",
			"checked":        "int",
			"valid":          "int",
			"unsigned":       "int",
			"invalid":        "int",
			"requireSigned":  "bool",
			"allowedSigners": "bool",
			"looseObjects":   "int",
			"packFiles":      "int",
			"packObjects":    "int",
			"diagnostics":    "[]JSONRepositoryDiagnostic",
		}},
		"tagVerify": {JSONTagVerifyOutput{}, map[string]string{
			"schemaVersion":  "int",
			"ok":             "bool",
			"tagName":        "string",
			"tagHash":        "string",
			"targetHash":     "string",
			"valid":          "bool",
			"unsigned":       "bool",
			"signerKey":      "string",
			"algorithm":      "string",
			"error":          "string",
			"requireSigned":  "bool",
			"allowedSigners": "bool",
		}},
		"doctor": {JSONDoctorOutput{}, map[string]string{
			"schemaVersion": "int",
			"ok":            "bool",
			"looseObjects":  "int",
			"packFiles":     "int",
			"packObjects":   "int",
			"diagnostics":   "[]JSONRepositoryDiagnostic",
		}},
		"doctorGlobal": {JSONDoctorGlobalOutput{}, map[string]string{
			"schemaVersion":                  "int",
			"ok":                             "bool",
			"generatedAt":                    "string",
			"version":                        "string",
			"commit":                         "string",
			"buildTime":                      "string",
			"goVersion":                      "string",
			"os":                             "string",
			"arch":                           "string",
			"supportedRepositoryFormat":      "int",
			"supportedRemoteProtocolVersion": "string",
			"git":                            "JSONDoctorGlobalTool",
			"userConfig":                     "JSONDoctorBundleUserConfig",
			"diagnostics":                    "[]JSONRepositoryDiagnostic",
			"collectionErrors":               "[]JSONDoctorBundleCollectionError",
		}},
		"doctorBundle": {JSONDoctorBundleOutput{}, map[string]string{
			"schemaVersion":    "int",
			"generatedAt":      "string",
			"repository":       "JSONDoctorBundleRepository",
			"userConfig":       "JSONDoctorBundleUserConfig",
			"hooks":            "JSONDoctorBundleHooks",
			"verify":           "JSONDoctorOutput",
			"gitShadow":        "JSONRepairGitShadowOutput",
			"recentReflog":     "[]JSONDoctorBundleReflogEntry",
			"environment":      "JSONDoctorBundleEnvironment",
			"protocol":         "JSONDoctorBundleProtocol",
			"collectionErrors": "[]JSONDoctorBundleCollectionError",
			"redaction":        "JSONDoctorBundleRedaction",
		}},
		"doctorBundleProtocol": {JSONDoctorBundleProtocol{}, map[string]string{
			"supportedRepositoryFormat":      "int",
			"supportedRemoteProtocolVersion": "string",
			"documentation":                  "string",
			"clientCapabilities":             "[]string",
			"definedCapabilities":            "[]string",
			"serverLimitKeys":                "[]string",
			"responseLimits":                 "[]JSONDoctorBundleProtocolResponseLimit",
			"diagnostics":                    "[]JSONRepositoryDiagnostic",
			"transportCount":                 "int",
			"endpointCount":                  "int",
		}},
		"repairGitShadow": {JSONRepairGitShadowOutput{}, map[string]string{
			"schemaVersion":     "int",
			"ok":                "bool",
			"state":             "string",
			"message":           "string",
			"hasGitDir":         "bool",
			"hasFailures":       "bool",
			"graftHead":         "string",
			"expectedGitCommit": "string",
			"expectedGitTree":   "string",
			"actualGitCommit":   "string",
			"actualGitTree":     "string",
			"repair":            "string",
		}},
		"repairLock": {JSONRepairLockOutput{}, map[string]string{
			"schemaVersion": "int",
			"ok":            "bool",
			"state":         "string",
			"message":       "string",
			"path":          "string",
			"operation":     "string",
			"pid":           "int",
			"hostname":      "string",
			"command":       "string",
			"startedAt":     "string",
			"stale":         "bool",
			"cleared":       "bool",
			"repair":        "string",
		}},
		"repairTransaction": {JSONRepairTransactionOutput{}, map[string]string{
			"schemaVersion": "int",
			"ok":            "bool",
			"id":            "string",
			"operation":     "string",
			"status":        "string",
			"startedAt":     "string",
			"updatedAt":     "string",
			"error":         "string",
			"touchedRefs":   "[]JSONTransactionRefMutation",
			"touchedFiles":  "[]string",
			"message":       "string",
			"repair":        "string",
		}},
		"repairMigrateConfig": {JSONRepairMigrateConfigOutput{}, map[string]string{
			"schemaVersion": "int",
			"ok":            "bool",
			"migrated":      "bool",
			"path":          "string",
			"fromVersion":   "int",
			"toVersion":     "int",
			"message":       "string",
		}},
		"verifyPushLimits": {JSONVerifyPushLimitsOutput{}, map[string]string{
			"schemaVersion":   "int",
			"ok":              "bool",
			"pushTarget":      "string",
			"remote":          "string",
			"localRef":        "string",
			"remoteRef":       "string",
			"localHash":       "string",
			"remoteHash":      "string",
			"limitBytes":      "int64",
			"objectsExamined": "int",
			"totalBytes":      "int64",
			"largest":         "*JSONVerifySizedObject",
			"blockers":        "[]JSONVerifySizedObject",
		}},
		"releaseManifest": {JSONReleaseManifestOutput{}, map[string]string{
			"schemaVersion":                  "int",
			"generatedAt":                    "string",
			"version":                        "string",
			"commit":                         "string",
			"buildTime":                      "string",
			"goVersion":                      "string",
			"supportedRepositoryFormat":      "int",
			"supportedRemoteProtocolVersion": "string",
			"files":                          "[]JSONReleaseManifestFile",
		}},
		"releaseManifestVerification": {JSONReleaseManifestVerificationOutput{}, map[string]string{
			"schemaVersion":  "int",
			"ok":             "bool",
			"manifestPath":   "string",
			"manifestFormat": "string",
			"baseDir":        "string",
			"checked":        "int",
			"matched":        "int",
			"missing":        "int",
			"mismatched":     "int",
			"errors":         "int",
			"results":        "[]JSONReleaseManifestVerificationFile",
		}},
		"releaseCheck": {JSONReleaseCheckOutput{}, map[string]string{
			"schemaVersion": "int",
			"ok":            "bool",
			"version":       "string",
			"changelogPath": "string",
			"checks":        "[]JSONReleaseCheckResult",
		}},
		"releaseSign": {JSONReleaseSignOutput{}, map[string]string{
			"schemaVersion":   "int",
			"signedAt":        "string",
			"signatureFormat": "string",
			"payloadFormat":   "string",
			"files":           "[]JSONReleaseSignatureFile",
		}},
		"releaseVerifySignature": {JSONReleaseVerifySignatureOutput{}, map[string]string{
			"schemaVersion": "int",
			"ok":            "bool",
			"signaturePath": "string",
			"baseDir":       "string",
			"checked":       "int",
			"valid":         "int",
			"missing":       "int",
			"mismatched":    "int",
			"invalid":       "int",
			"errors":        "int",
			"results":       "[]JSONReleaseSignatureVerificationResult",
		}},
		"lineGrep": {JSONLineGrepOutput{}, map[string]string{
			"schemaVersion": "int",
			"results":       "[]JSONLineGrepResult",
		}},
		"entitySearch": {JSONEntitySearchOutput{}, map[string]string{
			"schemaVersion": "int",
			"results":       "[]JSONEntitySearchResult",
		}},
		"structuralGrep": {JSONStructuralGrepOutput{}, map[string]string{
			"schemaVersion": "int",
			"results":       "[]JSONStructuralGrepResult",
			"isRewrite":     "bool",
			"rewritten":     "[]string",
		}},
		"historyGrep": {JSONHistoryGrepOutput{}, map[string]string{
			"schemaVersion": "int",
			"results":       "[]JSONHistoryGrepResult",
		}},
		"checkIgnore": {checkIgnoreOutput{}, map[string]string{
			"schemaVersion": "int",
			"results":       "[]checkIgnoreResult",
		}},
	}

	for name, contract := range contracts {
		t.Run(name, func(t *testing.T) {
			fields := jsonFieldSignatures(reflect.TypeOf(contract.sample))
			for fieldName, wantType := range contract.fields {
				gotType, ok := fields[fieldName]
				if !ok {
					t.Fatalf("field %q missing from %T", fieldName, contract.sample)
				}
				if gotType != wantType {
					t.Fatalf("field %q type = %q, want %q", fieldName, gotType, wantType)
				}
			}
		})
	}
}

// TestStatusCmd_JSON tests the --json flag on the status command.
func TestStatusCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// Create a file and stage it.
	writeTestFile(t, filepath.Join(dir, "hello.txt"), []byte("hello\n"))
	if err := r.Add([]string{"hello.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONStatusOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.Branch != "main" {
		t.Errorf("branch = %q, want %q", result.Branch, "main")
	}
	if result.ShadowState != repo.GitShadowStateNoShadow {
		t.Errorf("shadow_state = %q, want %q", result.ShadowState, repo.GitShadowStateNoShadow)
	}
	if !result.NoCommits {
		t.Error("noCommits = false, want true (no commits yet)")
	}
	if len(result.Staged) != 1 {
		t.Fatalf("len(staged) = %d, want 1", len(result.Staged))
	}
	if result.Staged[0].Path != "hello.txt" {
		t.Errorf("staged[0].path = %q, want %q", result.Staged[0].Path, "hello.txt")
	}
	if result.Staged[0].Status != "new" {
		t.Errorf("staged[0].status = %q, want %q", result.Staged[0].Status, "new")
	}
}

// TestStatusCmd_JSON_WithUntracked tests --json shows untracked files.
func TestStatusCmd_JSON_WithUntracked(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "untracked.txt"), []byte("data\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONStatusOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Untracked) != 1 {
		t.Fatalf("len(untracked) = %d, want 1", len(result.Untracked))
	}
	if result.Untracked[0] != "untracked.txt" {
		t.Errorf("untracked[0] = %q, want %q", result.Untracked[0], "untracked.txt")
	}
}

// TestStatusCmd_JSON_CleanState tests --json after a commit (clean state).
func TestStatusCmd_JSON_CleanState(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("content\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial commit", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONStatusOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.NoCommits {
		t.Error("noCommits = true, want false after commit")
	}
	if len(result.Staged) != 0 {
		t.Errorf("len(staged) = %d, want 0", len(result.Staged))
	}
	if len(result.Unstaged) != 0 {
		t.Errorf("len(unstaged) = %d, want 0", len(result.Unstaged))
	}
	if len(result.Untracked) != 0 {
		t.Errorf("len(untracked) = %d, want 0", len(result.Untracked))
	}
}

// TestDiffCmd_JSON_Staged tests --json --staged flag on the diff command.
func TestDiffCmd_JSON_Staged(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// Create initial commit.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("line one\nline two\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Modify and stage.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("line one\nline two modified\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDiffCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--staged", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONDiffOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(result.Files))
	}
	f := result.Files[0]
	if f.Path != "file.txt" {
		t.Errorf("file.path = %q, want %q", f.Path, "file.txt")
	}
	if f.Status != "modified" {
		t.Errorf("file.status = %q, want %q", f.Status, "modified")
	}
	if len(f.Hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}
	// Check that hunks contain the expected lines.
	foundDelete := false
	foundAdd := false
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.Type == "delete" && l.Content == "line two" {
				foundDelete = true
			}
			if l.Type == "add" && l.Content == "line two modified" {
				foundAdd = true
			}
		}
	}
	if !foundDelete {
		t.Error("expected a deleted line 'line two'")
	}
	if !foundAdd {
		t.Error("expected an added line 'line two modified'")
	}
}

// TestLogCmd_JSON tests --json flag on the log command.
func TestLogCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("a\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("first commit", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "b.txt"), []byte("b\n"))
	if err := r.Add([]string{"b.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("second commit", "bob"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newLogCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONLogOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Commits) != 2 {
		t.Fatalf("len(commits) = %d, want 2", len(result.Commits))
	}

	// Most recent first.
	if result.Commits[0].Message != "second commit" {
		t.Errorf("commits[0].message = %q, want %q", result.Commits[0].Message, "second commit")
	}
	if result.Commits[0].Author != "bob" {
		t.Errorf("commits[0].author = %q, want %q", result.Commits[0].Author, "bob")
	}
	if result.Commits[1].Message != "first commit" {
		t.Errorf("commits[1].message = %q, want %q", result.Commits[1].Message, "first commit")
	}
	// Verify hash fields are populated.
	if result.Commits[0].Hash == "" {
		t.Error("commits[0].hash is empty")
	}
	if result.Commits[0].ShortHash == "" {
		t.Error("commits[0].shortHash is empty")
	}
	// HEAD commit should have a decoration.
	if result.Commits[0].Decoration == "" {
		t.Error("commits[0].decoration is empty, expected HEAD decoration")
	}
}

// TestShowCmd_JSON tests --json flag on the show command.
func TestShowCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("content\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("test commit", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newShowCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONShowOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.Hash != string(commitHash) {
		t.Errorf("hash = %q, want %q", result.Hash, commitHash)
	}
	if result.Author != "alice" {
		t.Errorf("author = %q, want %q", result.Author, "alice")
	}
	if result.Message != "test commit" {
		t.Errorf("message = %q, want %q", result.Message, "test commit")
	}
	if len(result.Changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(result.Changes))
	}
	if result.Changes[0].Path != "file.txt" {
		t.Errorf("changes[0].path = %q, want %q", result.Changes[0].Path, "file.txt")
	}
	if result.Changes[0].Status != "A" {
		t.Errorf("changes[0].status = %q, want %q", result.Changes[0].Status, "A")
	}
}

// TestBlameCmd_JSON tests --json flag on the blame command.
func TestBlameCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	source := []byte("package main\n\nfunc target() int { return 1 }\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), source)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial target", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	key := jsonTestDeclarationKey(t, "main.go", source, "target")

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newBlameCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--entity", "main.go::" + key, "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONBlameOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.EntityKey != key {
		t.Errorf("entityKey = %q, want %q", result.EntityKey, key)
	}
	if result.Author != "alice" {
		t.Errorf("author = %q, want %q", result.Author, "alice")
	}
	if result.CommitHash != string(commitHash) {
		t.Errorf("commitHash = %q, want %q", result.CommitHash, commitHash)
	}
	if result.Message != "initial target" {
		t.Errorf("message = %q, want %q", result.Message, "initial target")
	}
	if result.Path != "main.go" {
		t.Errorf("path = %q, want %q", result.Path, "main.go")
	}
}

// TestConflictsCmd_JSON_NoConflicts tests --json on conflicts when there are no conflicts.
func TestConflictsCmd_JSON_NoConflicts(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("content\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newConflictsCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONConflictsOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Files) != 0 {
		t.Errorf("len(files) = %d, want 0", len(result.Files))
	}
}

// TestDiffCmd_JSON_NewFile tests --json for a newly added file (no HEAD yet).
func TestDiffCmd_JSON_NewFile(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "new.txt"), []byte("new content\n"))
	if err := r.Add([]string{"new.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDiffCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--staged", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONDiffOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(result.Files))
	}
	if result.Files[0].Status != "added" {
		t.Errorf("files[0].status = %q, want %q", result.Files[0].Status, "added")
	}
}

// TestStatusCmd_JSON_NoHumanOutput verifies --json suppresses human-readable output.
func TestStatusCmd_JSON_NoHumanOutput(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	// JSON output should not contain human-readable markers.
	if strings.Contains(raw, "on main") && !strings.Contains(raw, "\"branch\"") {
		t.Errorf("output contains human-readable text: %s", raw)
	}
	// Should start with { (JSON object).
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		t.Errorf("output does not start with '{': %s", raw)
	}
}

// --- helpers ---

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func jsonTestDeclarationKey(t *testing.T, path string, source []byte, name string) string {
	t.Helper()
	el, err := entity.Extract(path, source)
	if err != nil {
		t.Fatalf("entity.Extract(%s): %v", path, err)
	}
	for i := range el.Entities {
		if el.Entities[i].Name == name {
			return el.Entities[i].IdentityKey()
		}
	}
	t.Fatalf("declaration %q not found in %s", name, path)
	return ""
}

func jsonFieldSignatures(t reflect.Type) map[string]string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	fields := make(map[string]string)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if field.Anonymous && name == "" {
			embedded := field.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				for embeddedName, embeddedType := range jsonFieldSignatures(embedded) {
					fields[embeddedName] = embeddedType
				}
				continue
			}
		}
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = jsonTypeSignature(field.Type)
	}
	return fields
}

func jsonTypeSignature(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.String:
		return t.Kind().String()
	case reflect.Slice, reflect.Array:
		return "[]" + jsonTypeSignature(t.Elem())
	case reflect.Pointer:
		return "*" + jsonTypeSignature(t.Elem())
	case reflect.Map:
		return "map[" + jsonTypeSignature(t.Key()) + "]" + jsonTypeSignature(t.Elem())
	case reflect.Struct:
		if t.Name() != "" {
			return t.Name()
		}
		return "struct"
	default:
		if t.Name() != "" {
			return t.Name()
		}
		return t.String()
	}
}
