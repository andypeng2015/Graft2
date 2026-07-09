package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestRootFlagErrorUsesUsageExitCode(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"status", "--not-a-real-flag"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected flag error")
	}
	if got := commandExitCode(err); got != exitUsageError {
		t.Fatalf("exit code = %d, want %d", got, exitUsageError)
	}

	var out bytes.Buffer
	printCommandError(&out, err)
	if !strings.Contains(out.String(), "error [usage]:") {
		t.Fatalf("formatted error missing usage code: %q", out.String())
	}
	if !strings.Contains(out.String(), "suggestion:") {
		t.Fatalf("formatted error missing suggestion: %q", out.String())
	}
}

func TestVerifyIntegrityFailureUsesVerificationExitCode(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	refPath := filepath.Join(dir, ".graft", "refs", "heads", "bad")
	if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(refPath, []byte("not-a-hash\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newVerifyCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected verify failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}
}

func TestDoctorIntegrityFailureUsesRepairExitCode(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	refPath := filepath.Join(dir, ".graft", "refs", "heads", "bad")
	if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(refPath, []byte("not-a-hash\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newDoctorCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected doctor failure")
	}
	if got := commandExitCode(err); got != exitRepositoryNeedsRepair {
		t.Fatalf("exit code = %d, want %d", got, exitRepositoryNeedsRepair)
	}
}

func TestConflictAndRemoteErrorsClassifyExitCodes(t *testing.T) {
	if got := commandExitCode(errMergeConflict); got != exitConflict {
		t.Fatalf("merge conflict exit code = %d, want %d", got, exitConflict)
	}

	authErr := &remote.RemoteError{Code: "unauthorized", Message: "bad token"}
	if got := commandExitCode(authErr); got != exitAuthenticationFailure {
		t.Fatalf("auth exit code = %d, want %d", got, exitAuthenticationFailure)
	}

	remoteErr := &remote.RemoteError{Code: "unavailable", Message: "try later"}
	if got := commandExitCode(remoteErr); got != exitNetworkFailure {
		t.Fatalf("remote exit code = %d, want %d", got, exitNetworkFailure)
	}
}

func TestPrintCommandErrorRedactsSensitiveRemoteDetails(t *testing.T) {
	err := &remote.RemoteError{
		Code:    "unauthorized",
		Message: "bad token: bearer-secret",
		Detail:  `Authorization: Bearer header-secret GRAFT_TOKEN=env-secret https://alice:remote-secret@example.com/graft/alice/repo?token=remote-token&ok=yes`,
	}

	var out bytes.Buffer
	printCommandError(&out, err)
	raw := out.String()
	for _, forbidden := range []string{"bearer-secret", "header-secret", "env-secret", "remote-secret", "remote-token"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("formatted error leaked %q:\n%s", forbidden, raw)
		}
	}
	for _, want := range []string{"error [auth_failed]:", "token: redacted", "Authorization: redacted", "GRAFT_TOKEN=redacted", "redacted@example.com"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("formatted error missing %q:\n%s", want, raw)
		}
	}
}

func TestExternalProcessExitCodeIsPreserved(t *testing.T) {
	err := &coordd.ExitCodeError{Code: 126, Err: errors.New("blocked")}
	if got := commandExitCode(err); got != 126 {
		t.Fatalf("exit code = %d, want 126", got)
	}
}
