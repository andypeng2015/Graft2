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

	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoorddSnapshotCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "note.txt"), []byte("hello snapshot\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"snapshot", "--json"})
		return cmd.Execute()
	})

	var snapshot coordd.Snapshot
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if snapshot.Summary.Changed == 0 {
		t.Fatal("expected snapshot to include changed files")
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("len(snapshot.Entries) = %d, want 1", len(snapshot.Entries))
	}
	if snapshot.Entries[0].Path != "note.txt" {
		t.Fatalf("snapshot path = %q, want note.txt", snapshot.Entries[0].Path)
	}
}

func TestCoorddServeOnce_PrintsAndLogsEvents(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "dirty.txt"), []byte("hello coordd\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"serve", "--once", "--print"})
		return cmd.Execute()
	})

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		t.Fatal("expected printed coordd events")
	}

	var event coordd.Event
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("json.Unmarshal event: %v\nraw: %s", err, lines[0])
	}
	if event.Type == "" {
		t.Fatal("expected event type")
	}

	events, err := coordd.ListEvents(r.GraftDir, 0)
	if err != nil {
		t.Fatalf("coordd.ListEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected event journal entries")
	}
}

func TestCoorddServeUsesCommandContext(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := newCoorddCmd()
	cmd.SilenceUsage = true
	cmd.SetContext(ctx)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"serve", "--interval", "1h"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("coordd serve did not stop after command context cancellation")
	}
}

func TestCoorddTailCmd_JSONIsVersioned(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.AppendEvent(r.GraftDir, coordd.Event{Type: "test_event"}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"tail", "--limit", "1", "--json"})
		return cmd.Execute()
	})

	var result JSONCoorddTailOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if len(result.Events) != 1 || result.Events[0].Type != "test_event" {
		t.Fatalf("events = %#v, want one test_event", result.Events)
	}
}

func TestCoorddTailFollowUsesCommandContext(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.AppendEvent(r.GraftDir, coordd.Event{Type: "test_event"}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := newCoorddCmd()
	cmd.SilenceUsage = true
	cmd.SetContext(ctx)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"tail", "--follow", "--interval", "1h"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("coordd tail --follow did not stop after command context cancellation")
	}
}

func TestCoorddPreflightCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"preflight", "--json", "--", "rm", "-rf", "./"})
		return cmd.Execute()
	})

	var result struct {
		Input    coordd.ActionPolicyInput    `json:"input"`
		Decision coordd.ActionPolicyDecision `json:"decision"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if result.Decision.Action != "HardBlock" {
		t.Fatalf("Decision.Action = %q, want HardBlock", result.Decision.Action)
	}
	if result.Decision.Profile != "blocked" {
		t.Fatalf("Decision.Profile = %q, want blocked", result.Decision.Profile)
	}

	events, err := coordd.ListEvents(r.GraftDir, 1)
	if err != nil {
		t.Fatalf("coordd.ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "action_preflight_blocked" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestCoorddExecCheckOnlyDoesNotExecute(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"exec", "--check-only", "--json", "--", "sh", "-c", "echo ran > ran.txt"})
		return cmd.Execute()
	})

	var result struct {
		Input    coordd.ActionPolicyInput    `json:"input"`
		Decision coordd.ActionPolicyDecision `json:"decision"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if result.Decision.Action == "" {
		t.Fatalf("empty decision in result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "ran.txt")); !os.IsNotExist(err) {
		t.Fatalf("check-only executed command; stat err=%v", err)
	}
}

func TestCoorddExecCheckOnlyMalformedPolicyFailsClosed(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	writeTestFile(t, filepath.Join(coordd.GuardPoliciesDir(r.GraftDir), "action.arb"), []byte(`rule BrokenAction {
    when {
        this is not valid arbiter syntax
    }
    then Allow {}
}
`))

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newCoorddCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"exec", "--check-only", "--json", "--", "sh", "-c", "echo ran > ran.txt"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want malformed policy error")
	}
	if !strings.Contains(err.Error(), "action policy") {
		t.Fatalf("error = %q, want action policy context", err.Error())
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("stdout = %q, want empty on policy compile failure", out.String())
	}
	if _, statErr := os.Stat(filepath.Join(dir, "ran.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("malformed policy path executed command; stat err=%v", statErr)
	}
}

func TestCoorddExecUsesCommandContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell cancellation fixture")
	}

	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "advisory", PreferredBackend: "host-direct"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	cmd := newCoorddCmd()
	cmd.SilenceUsage = true
	cmd.SetContext(ctx)
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"exec", "--json", "--", "sh", "-c", "sleep 5"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Execute error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("coordd exec did not stop after command context cancellation")
	}

	var result coordd.ExecResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, out.String())
	}
	assertJSONSchemaVersionInOutput(t, out.String())
	if result.ExitCode == 0 {
		t.Fatalf("ExitCode = 0, want cancellation failure result")
	}
}

func TestCoorddGuardAllowThenPreflightAllows(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce", PreferredBackend: "host-direct"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	if err := func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"guard", "allow", "shell:touch *"})
		return cmd.Execute()
	}(); err != nil {
		t.Fatalf("guard allow: %v", err)
	}

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"preflight", "--json", "--", "touch", "note.txt"})
		return cmd.Execute()
	})

	var result struct {
		Decision coordd.ActionPolicyDecision `json:"decision"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if result.Decision.Action != "Allow" {
		t.Fatalf("Decision.Action = %q, want Allow", result.Decision.Action)
	}
	if result.Decision.Profile != "repo_write" {
		t.Fatalf("Decision.Profile = %q, want repo_write", result.Decision.Profile)
	}
}

func TestCoorddExecCmd_BlocksDestructiveCommand(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce", PreferredBackend: "host-direct"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newCoorddCmd()
	cmd.SilenceUsage = true
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"exec", "--", "rm", "-rf", "./"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected destructive command to be blocked")
	}

	events, err := coordd.ListEvents(r.GraftDir, 1)
	if err != nil {
		t.Fatalf("coordd.ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "action_preflight_blocked" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestCoorddExecCmd_RunsAllowedCommand(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce", PreferredBackend: "host-direct"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"exec", "--json", "--", "cat", "/dev/null"})
		return cmd.Execute()
	})

	var result coordd.ExecResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if result.Backend != "host-direct" {
		t.Fatalf("result.Backend = %q, want host-direct", result.Backend)
	}
	if result.RequestedProfile.Name != "read_only" {
		t.Fatalf("RequestedProfile.Name = %q, want read_only", result.RequestedProfile.Name)
	}
	if result.EffectiveProfile.Name != "host_direct" {
		t.Fatalf("EffectiveProfile.Name = %q, want host_direct", result.EffectiveProfile.Name)
	}

	events, err := coordd.ListEvents(r.GraftDir, 3)
	if err != nil {
		t.Fatalf("coordd.ListEvents: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected execution events, got %#v", events)
	}
	if events[len(events)-1].Type != "action_exec_finished" {
		t.Fatalf("last event = %q, want action_exec_finished", events[len(events)-1].Type)
	}
}

func TestCoorddGuardOverrideSetAndList_JSON(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	if err := func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"guard", "override", "set", "action", "AdvisoryReadOnly", "--kill-switch"})
		return cmd.Execute()
	}(); err != nil {
		t.Fatalf("guard override set: %v", err)
	}

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"guard", "override", "list", "--json"})
		return cmd.Execute()
	})

	var result JSONCoorddGuardOverridesOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	entries := result.Overrides
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Policy != "action" || entries[0].Rule != "AdvisoryReadOnly" {
		t.Fatalf("entries[0] = %#v", entries[0])
	}
	if entries[0].KillSwitch == nil || !*entries[0].KillSwitch {
		t.Fatalf("expected kill switch override, got %#v", entries[0])
	}
}

func TestCoorddGuardShow_JSONIncludesPolicyBundleInfo(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	policyPath := filepath.Join(coordd.GuardPoliciesDir(r.GraftDir), "action.arb")
	writeTestFile(t, policyPath, []byte(`rule RepoLocal priority 5 {
    when { action.selector == "shell:noop" }
    then Advisory {
        code: "repo_local",
        reason: "repo-local action policy",
        profile: "read_only",
    }
}

rule Fallback priority 999 {
    when { true }
    then Allow {
        code: "allow",
        reason: "fallback",
        profile: "read_only",
    }
}
`))

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"guard", "show", "--json"})
		return cmd.Execute()
	})

	var result struct {
		SchemaVersion int                                `json:"schemaVersion"`
		Policies      map[string]coordd.PolicyBundleInfo `json:"policies"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	action, ok := result.Policies["action"]
	if !ok {
		t.Fatalf("missing action policy bundle in %#v", result.Policies)
	}
	if action.Embedded {
		t.Fatal("action policy unexpectedly marked embedded")
	}
	if action.Root != policyPath {
		t.Fatalf("action.Root = %q, want %q", action.Root, policyPath)
	}
}

func TestCoorddGuardDoctorJSONReportsHostDirectDegradation(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{PreferredBackend: "auto"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	t.Setenv("PATH", t.TempDir())

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newCoorddCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"guard", "doctor", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("guard doctor: %v", err)
	}

	var result JSONCoorddGuardDoctorOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, out.String())
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if !result.OK {
		t.Fatalf("OK = false, want true: %+v", result.Diagnostics)
	}
	if result.Health.SelectedBackend != "host-direct" {
		t.Fatalf("SelectedBackend = %q, want host-direct", result.Health.SelectedBackend)
	}
	if !jsonDiagnosticsContain(result.Diagnostics, "warning", "coordd_backend_degraded") {
		t.Fatalf("diagnostics = %+v, want coordd_backend_degraded warning", result.Diagnostics)
	}
}

func TestCoorddGuardDoctorJSONExplicitUnavailableBackendFails(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{PreferredBackend: "host-bwrap"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	t.Setenv("PATH", t.TempDir())

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newCoorddCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"guard", "doctor", "--json"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("guard doctor succeeded, want unavailable backend error")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}

	var result JSONCoorddGuardDoctorOutput
	if unmarshalErr := json.Unmarshal(out.Bytes(), &result); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", unmarshalErr, out.String())
	}
	if result.OK {
		t.Fatal("OK = true, want false")
	}
	if !jsonDiagnosticsContain(result.Diagnostics, "error", "coordd_backend_unavailable") {
		t.Fatalf("diagnostics = %+v, want coordd_backend_unavailable error", result.Diagnostics)
	}
}

func TestCoorddGuardRuntimeAndImagePersist(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	for _, args := range [][]string{
		{"guard", "runtime", "podman"},
		{"guard", "image", "docker.io/library/alpine:3.20"},
	} {
		if err := func() error {
			cmd := newCoorddCmd()
			cmd.SilenceUsage = true
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)
			return cmd.Execute()
		}(); err != nil {
			t.Fatalf("cmd.Execute(%v): %v", args, err)
		}
	}

	cfg, err := coordd.LoadGuardConfig(r.GraftDir)
	if err != nil {
		t.Fatalf("LoadGuardConfig: %v", err)
	}
	if cfg.ContainerRuntime != "podman" {
		t.Fatalf("ContainerRuntime = %q, want podman", cfg.ContainerRuntime)
	}
	if cfg.ContainerImage != "docker.io/library/alpine:3.20" {
		t.Fatalf("ContainerImage = %q", cfg.ContainerImage)
	}
}

func jsonDiagnosticsContain(diagnostics []JSONRepositoryDiagnostic, severity, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == severity && diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestCoorddExecCmd_JSONReservesStdout(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce", PreferredBackend: "host-direct"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"exec", "--json", "--", "printf", "hello"})
		return cmd.Execute()
	})

	var result coordd.ExecResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	assertJSONSchemaVersionInOutput(t, output)
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "{") {
		t.Fatalf("expected stdout to contain JSON only, got %q", output)
	}
}

func assertJSONSchemaVersionInOutput(t *testing.T, output string) {
	t.Helper()

	var envelope struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("json.Unmarshal schema envelope: %v\nraw: %s", err, output)
	}
	if envelope.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", envelope.SchemaVersion, JSONSchemaVersion)
	}
}
