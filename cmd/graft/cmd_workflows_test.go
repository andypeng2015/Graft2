package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestWorkflowsCmdPrintsAllGuides(t *testing.T) {
	var out bytes.Buffer
	cmd := newWorkflowsCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	for _, want := range []string{
		"Common Graft workflows",
		"Solo Repository",
		"Git Forge Backed Repository",
		"Team Or Agent Coordination",
		"Recover Coordination Identity",
		"Recover Git Shadow Divergence",
		"Recover Interrupted Operations",
		"graft workon --recover --as <name>",
		"graft coord cleanup-stale --dry-run",
		"graft repair transaction <id>",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("workflow output missing %q:\n%s", want, raw)
		}
	}
}

func TestWorkflowsCmdPrintsTopic(t *testing.T) {
	var out bytes.Buffer
	cmd := newWorkflowsCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"recover-shadow"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	if !strings.Contains(raw, "graft repair resync-git") {
		t.Fatalf("recover-shadow output missing repair command:\n%s", raw)
	}
	if strings.Contains(raw, "Solo Repository") {
		t.Fatalf("topic output included unrelated guide:\n%s", raw)
	}
}

func TestWorkflowsCmdPrintsRecoverCoordinationTopic(t *testing.T) {
	var out bytes.Buffer
	cmd := newWorkflowsCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"recover-coordination"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	if !strings.Contains(raw, "graft workon --recover --as <name>") {
		t.Fatalf("recover-coordination output missing workon recovery command:\n%s", raw)
	}
	if !strings.Contains(raw, "graft coord cleanup-stale") {
		t.Fatalf("recover-coordination output missing stale cleanup command:\n%s", raw)
	}
	if strings.Contains(raw, "Recover Git Shadow Divergence") {
		t.Fatalf("topic output included unrelated guide:\n%s", raw)
	}
}

func TestRootHelpWorkflowsShowsGuide(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"help", "workflows"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	for _, want := range []string{
		"Common Graft workflows",
		"graft workflows <topic>",
		"Recover Coordination Identity",
		"Recover Interrupted Operations",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("help output missing %q:\n%s", want, raw)
		}
	}
}

func TestWorkflowsCmdUnknownTopicUsesUsageExitCode(t *testing.T) {
	cmd := newWorkflowsCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"unknown"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want usage error")
	}
	if got := commandExitCode(err); got != exitUsageError {
		t.Fatalf("exit code = %d, want %d", got, exitUsageError)
	}
}
