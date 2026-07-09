package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoorddSpawnCmd_JSONAndList(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn", "--name", "child-agent", "--runtime", "detached", "--json", "--", "printf", "hello"})
		return cmd.Execute()
	})

	var result coordd.SpawnResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if result.Record == nil {
		t.Fatal("Record = nil, want spawn record")
	}
	if result.Record.Name != "child-agent" {
		t.Fatalf("Record.Name = %q, want child-agent", result.Record.Name)
	}
	if result.Record.Backend != "host-direct" && result.Record.Backend != "host-bwrap" {
		t.Fatalf("Record.Backend = %q, want detached host backend", result.Record.Backend)
	}
	if result.Record.RequestedRuntime != "detached" {
		t.Fatalf("Record.RequestedRuntime = %q, want detached", result.Record.RequestedRuntime)
	}
	if got, ok := waitForCoorddSpawnFile(result.Record.StdoutPath, "hello", 2*time.Second); !ok {
		t.Fatalf("stdout log missing child output: %q", got)
	}

	listOutput := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawns", "--json"})
		return cmd.Execute()
	})

	var listResult JSONCoorddSpawnsOutput
	if err := json.Unmarshal([]byte(listOutput), &listResult); err != nil {
		t.Fatalf("json.Unmarshal spawns: %v\nraw: %s", err, listOutput)
	}
	if listResult.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("spawns schemaVersion = %d, want %d", listResult.SchemaVersion, JSONSchemaVersion)
	}
	records := listResult.Spawns
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Name != "child-agent" {
		t.Fatalf("records[0].Name = %q, want child-agent", records[0].Name)
	}
}

func TestCoorddSpawnAttachUsesCommandContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell cancellation fixture")
	}

	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "advisory",
		PreferredBackend: "host-direct",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	result, err := coordd.AuthorizeSpawn(r, "agent-parent", coordd.SpawnRequest{
		Name:    "canceled-attach",
		Command: []string{"sh", "-c", "sleep 5"},
		Runtime: "detached",
		Launch:  "lease",
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	errCh := make(chan error, 1)
	go func() {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetContext(ctx)
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn-attach", "--id", result.Record.ID, "--json"})
		errCh <- cmd.Execute()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Execute error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("coordd spawn-attach did not stop after command context cancellation")
	}

	output := out.String()
	var record coordd.SpawnRecord
	if err := json.Unmarshal([]byte(output), &record); err != nil {
		t.Fatalf("json.Unmarshal attach output: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if record.ID != result.Record.ID {
		t.Fatalf("record.ID = %q, want %q", record.ID, result.Record.ID)
	}
	if record.Status != "failed" {
		t.Fatalf("record.Status = %q, want failed", record.Status)
	}
}

func TestCoorddSpawnCmd_LeaseHeartbeatAndFinish(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn", "--name", "child-agent", "--runtime", "detached", "--launch", "lease", "--bootstrap-coord", "--json", "--", "printf", "hello"})
		return cmd.Execute()
	})

	var result coordd.SpawnResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if result.Record == nil {
		t.Fatal("Record = nil, want spawn record")
	}
	if result.Record.LaunchMode != "lease" {
		t.Fatalf("LaunchMode = %q, want lease", result.Record.LaunchMode)
	}
	if !result.Record.BootstrapCoord {
		t.Fatal("BootstrapCoord = false, want true")
	}
	if result.Record.Status != "authorized" {
		t.Fatalf("Status = %q, want authorized", result.Record.Status)
	}
	if result.Record.ChildAgentID == "" || result.Record.ChildAgentName == "" {
		t.Fatalf("missing bootstrapped child identity: %#v", result.Record)
	}

	heartbeatOutput := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn-heartbeat", "--id", result.Record.ID, "--child-agent-id", "child-subagent", "--json"})
		return cmd.Execute()
	})
	var heartbeat coordd.SpawnRecord
	if err := json.Unmarshal([]byte(heartbeatOutput), &heartbeat); err != nil {
		t.Fatalf("json.Unmarshal heartbeat: %v\nraw: %s", err, heartbeatOutput)
	}
	assertJSONSchemaVersionInOutput(t, heartbeatOutput)
	if heartbeat.Status != "active" {
		t.Fatalf("heartbeat Status = %q, want active", heartbeat.Status)
	}

	finishOutput := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn-finish", "--id", result.Record.ID, "--child-agent-id", "child-subagent", "--status", "completed", "--json"})
		return cmd.Execute()
	})
	var finished coordd.SpawnRecord
	if err := json.Unmarshal([]byte(finishOutput), &finished); err != nil {
		t.Fatalf("json.Unmarshal finish: %v\nraw: %s", err, finishOutput)
	}
	assertJSONSchemaVersionInOutput(t, finishOutput)
	if finished.Status != "completed" {
		t.Fatalf("finished Status = %q, want completed", finished.Status)
	}
}

func TestReadActiveAgentID_PrefersEnvOverride(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("parent-agent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	t.Setenv("GRAFT_COORD_AGENT_ID", "child-agent")
	if got := readActiveAgentID(r); got != "child-agent" {
		t.Fatalf("readActiveAgentID = %q, want child-agent", got)
	}
}

func TestCoorddSpawnCmd_ShowAndWait(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	result, err := coordd.AuthorizeSpawn(r, "agent-parent", coordd.SpawnRequest{
		Name:           "show-child",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	showOutput := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn-show", "--id", result.Record.ID, "--json"})
		return cmd.Execute()
	})
	var view coordd.SpawnView
	if err := json.Unmarshal([]byte(showOutput), &view); err != nil {
		t.Fatalf("json.Unmarshal show: %v\nraw: %s", err, showOutput)
	}
	assertJSONSchemaVersionInOutput(t, showOutput)
	if view.Lease == nil || view.Lease.Env["GRAFT_COORD_AGENT_ID"] != result.Record.ChildAgentID {
		t.Fatalf("missing expected lease env in view: %#v", view)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = coordd.FinishSpawn(r.GraftDir, result.Record.ID, "completed", "")
	}()

	waitOutput := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn-wait", "--id", result.Record.ID, "--timeout", "2s", "--poll", "20ms", "--json"})
		return cmd.Execute()
	})
	var waited coordd.SpawnRecord
	if err := json.Unmarshal([]byte(waitOutput), &waited); err != nil {
		t.Fatalf("json.Unmarshal wait: %v\nraw: %s", err, waitOutput)
	}
	assertJSONSchemaVersionInOutput(t, waitOutput)
	if waited.Status != "completed" {
		t.Fatalf("waited.Status = %q, want completed", waited.Status)
	}
}

func TestCoorddSpawnCmd_ConsumeAndAttachWithTask(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}
	c := coord.New(r, coord.DefaultConfig)
	task := &coord.Task{Title: "Attach child from CLI"}
	if err := c.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	spawnOutput := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn", "--name", "attach-child", "--runtime", "detached", "--launch", "lease", "--bootstrap-coord", "--task", task.ID, "--json", "--", "printf", "hello"})
		return cmd.Execute()
	})

	var spawned coordd.SpawnResult
	if err := json.Unmarshal([]byte(spawnOutput), &spawned); err != nil {
		t.Fatalf("json.Unmarshal spawn: %v\nraw: %s", err, spawnOutput)
	}
	assertJSONSchemaVersionInOutput(t, spawnOutput)
	if spawned.Record == nil || spawned.Record.Task == nil || spawned.Record.Task.ID != task.ID {
		t.Fatalf("spawned.Record.Task = %#v, want bound task %q", spawned.Record.Task, task.ID)
	}

	consumeOutput := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn-consume", "--id", spawned.Record.ID, "--json"})
		return cmd.Execute()
	})

	var view coordd.SpawnView
	if err := json.Unmarshal([]byte(consumeOutput), &view); err != nil {
		t.Fatalf("json.Unmarshal consume: %v\nraw: %s", err, consumeOutput)
	}
	assertJSONSchemaVersionInOutput(t, consumeOutput)
	if view.Record == nil || view.Record.Task == nil || view.Record.Task.Status != "in_progress" {
		t.Fatalf("view.Record.Task = %#v, want in_progress", view.Record.Task)
	}
	if view.Lease == nil || view.Lease.Env["GRAFT_COORDD_TASK_ID"] != task.ID {
		t.Fatalf("view.Lease = %#v, want task env", view.Lease)
	}

	attachOutput := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn-attach", "--id", spawned.Record.ID, "--heartbeat", "10ms", "--json"})
		return cmd.Execute()
	})

	var finished coordd.SpawnRecord
	if err := json.Unmarshal([]byte(attachOutput), &finished); err != nil {
		t.Fatalf("json.Unmarshal attach: %v\nraw: %s", err, attachOutput)
	}
	assertJSONSchemaVersionInOutput(t, attachOutput)
	if finished.Status != "completed" {
		t.Fatalf("finished.Status = %q, want completed", finished.Status)
	}
	if finished.Task == nil || finished.Task.Status != "completed" {
		t.Fatalf("finished.Task = %#v, want completed", finished.Task)
	}
	got, err := c.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("task.Status = %q, want completed", got.Status)
	}
}

func TestCoorddSpawnCmd_Trace(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "host-direct",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	result, err := coordd.AuthorizeSpawn(r, "agent-parent", coordd.SpawnRequest{
		Name:           "trace-child",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	t.Setenv("GRAFT_COORDD_SPAWN_ID", result.Record.ID)
	input, err := coordd.BuildShellActionInput(r, result.Record.ChildAgentID, []string{"true"})
	if err != nil {
		t.Fatalf("BuildShellActionInput: %v", err)
	}
	decision, err := coordd.EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	if _, err := coordd.ExecuteGuardedWithIO(r, input, decision, coordd.ExecIO{
		Stdout: io.Discard,
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("ExecuteGuardedWithIO: %v", err)
	}

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn-trace", "--id", result.Record.ID, "--json"})
		return cmd.Execute()
	})

	var trace coordd.SpawnTraceView
	if err := json.Unmarshal([]byte(output), &trace); err != nil {
		t.Fatalf("json.Unmarshal trace: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if trace.Record == nil || trace.Record.ID != result.Record.ID {
		t.Fatalf("trace.Record = %#v, want spawn %q", trace.Record, result.Record.ID)
	}
	if len(trace.Execs) == 0 {
		t.Fatal("expected trace.Execs to include persisted exec")
	}
	if len(trace.Phases) == 0 {
		t.Fatal("expected trace.Phases to include grouped events")
	}
}

func TestCoorddSpawnCmd_TraceRedactedJSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "host-direct",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	result, err := coordd.AuthorizeSpawn(r, "agent-parent", coordd.SpawnRequest{
		Name:           "trace-redacted-child",
		Command:        []string{"printf", "secret-token"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn-trace", "--id", result.Record.ID, "--json", "--redact"})
		return cmd.Execute()
	})
	assertJSONSchemaVersionInOutput(t, output)
	for _, forbidden := range []string{"secret-token", dir} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("redacted trace leaked %q:\n%s", forbidden, output)
		}
	}

	var trace coordd.SpawnTraceView
	if err := json.Unmarshal([]byte(output), &trace); err != nil {
		t.Fatalf("json.Unmarshal trace: %v\nraw: %s", err, output)
	}
	if trace.Redaction == nil || trace.Redaction.SecretsIncluded || trace.Redaction.CommandsIncluded || trace.Redaction.LocalPathsIncluded {
		t.Fatalf("unexpected redaction metadata: %#v", trace.Redaction)
	}
	if trace.Record == nil || len(trace.Record.Command) != 0 || trace.Record.RepoRoot != "" {
		t.Fatalf("record was not redacted: %#v", trace.Record)
	}
	if trace.Lease == nil || len(trace.Lease.Command) != 0 || trace.Lease.RepoRoot != "" || trace.Lease.Env["GRAFT_COORD_AGENT_ID"] != "redacted" {
		t.Fatalf("lease was not redacted: %#v", trace.Lease)
	}
}

func TestCoorddSpawnCmd_TraceExecutionOnlyNoFallbacks(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "host-direct",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	result, err := coordd.AuthorizeSpawn(r, "agent-parent", coordd.SpawnRequest{
		Name:           "trace-child-phase",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	t.Setenv("GRAFT_COORDD_SPAWN_ID", result.Record.ID)
	input, err := coordd.BuildShellActionInput(r, result.Record.ChildAgentID, []string{"true"})
	if err != nil {
		t.Fatalf("BuildShellActionInput: %v", err)
	}
	decision, err := coordd.EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	if _, err := coordd.ExecuteGuardedWithIO(r, input, decision, coordd.ExecIO{
		Stdout: io.Discard,
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("ExecuteGuardedWithIO: %v", err)
	}

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{
			"spawn-trace",
			"--id", result.Record.ID,
			"--phase", "execution",
			"--no-default-fallbacks",
			"--json",
		})
		return cmd.Execute()
	})

	var trace coordd.SpawnTraceView
	if err := json.Unmarshal([]byte(output), &trace); err != nil {
		t.Fatalf("json.Unmarshal trace: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if trace.SpawnAction != nil || trace.SpawnPolicy != nil {
		t.Fatalf("expected authorization decisions to be filtered out: %#v %#v", trace.SpawnAction, trace.SpawnPolicy)
	}
	if len(trace.Phases) != 1 || trace.Phases[0].Name != "execution" {
		t.Fatalf("trace.Phases = %#v, want only execution", trace.Phases)
	}
	if len(trace.Execs) != 1 || trace.Execs[0].Decision == nil {
		t.Fatalf("trace.Execs = %#v, want one execution trace with decision", trace.Execs)
	}
	if len(trace.Execs[0].Decision.Rules) != 1 || trace.Execs[0].Decision.Rules[0].Rule != "AllowReadOnly" {
		t.Fatalf("exec decision rules = %#v, want only AllowReadOnly", trace.Execs[0].Decision.Rules)
	}
}

func waitForCoorddSpawnFile(path, needle string, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Clean(path))
		if err == nil {
			content := string(data)
			if strings.Contains(content, needle) {
				return content, true
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	data, _ := os.ReadFile(filepath.Clean(path))
	return string(data), false
}
