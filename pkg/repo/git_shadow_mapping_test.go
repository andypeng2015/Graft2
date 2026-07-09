package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGitShadowCommitMapping verifies that committing in a dual-track repo
// records the graft-commit -> git-commit(+tree) correspondence, the precondition
// for structural history fidelity and never-desync divergence detection. A
// graft commit (SHA-256) can never equal a git commit (SHA-1), so the map is
// mandatory.
func TestGitShadowCommitMapping(t *testing.T) {
	r := initGitGraftRepo(t)

	shadowWriteFile(t, r.RootDir, "main.go", "package main\n\nfunc main() {}\n")
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	graftCommit, err := r.Commit("first", "test <test@test.com>")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	gitCommit, gitTree, ok := r.GitCommitFor(graftCommit)
	if !ok {
		t.Fatalf("no git mapping recorded for graft commit %s", graftCommit)
	}

	wantCommit := gitOutput(t, r.RootDir, "rev-parse", "HEAD")
	wantTree := gitOutput(t, r.RootDir, "rev-parse", "HEAD^{tree}")
	if gitCommit != wantCommit {
		t.Fatalf("mapped git commit = %s, want %s", gitCommit, wantCommit)
	}
	if gitTree != wantTree {
		t.Fatalf("mapped git tree = %s, want %s", gitTree, wantTree)
	}

	// The graft commit hash must differ from the git commit hash (SHA-256 vs SHA-1).
	if string(graftCommit) == gitCommit {
		t.Fatalf("graft and git commit hashes unexpectedly equal: %s", gitCommit)
	}
}

// TestGitShadowDiverged verifies the succeed-but-diverge tripwire: after a clean
// dual-track commit the shadow matches the recorded mapping, but if git's HEAD
// is moved out of band (a divergence the exit-code-only failure check cannot
// see) GitShadowDiverged reports it.
func TestGitShadowDiverged(t *testing.T) {
	r := initGitGraftRepo(t)
	shadowWriteFile(t, r.RootDir, "main.go", "package main\n")
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("first", "test <test@test.com>"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	diverged, err := r.GitShadowDiverged()
	if err != nil {
		t.Fatalf("GitShadowDiverged: %v", err)
	}
	if diverged {
		t.Fatalf("expected NOT diverged immediately after a clean dual-track commit")
	}

	// Move git's HEAD out of band; graft's HEAD does not move.
	shadowWriteFile(t, r.RootDir, "drift.txt", "out of band\n")
	gitOutput(t, r.RootDir, "add", "-A")
	gitOutput(t, r.RootDir, "commit", "-m", "out of band")

	diverged, err = r.GitShadowDiverged()
	if err != nil {
		t.Fatalf("GitShadowDiverged: %v", err)
	}
	if !diverged {
		t.Fatalf("expected diverged after an out-of-band git commit moved git HEAD")
	}
}

func TestGitShadowStatusStates(t *testing.T) {
	r := initGitGraftRepo(t)
	shadowWriteFile(t, r.RootDir, "main.go", "package main\n")
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	graftCommit, err := r.Commit("first", "test <test@test.com>")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	status, err := r.GitShadowStatus()
	if err != nil {
		t.Fatalf("GitShadowStatus(clean): %v", err)
	}
	if status.State != GitShadowStateClean {
		t.Fatalf("state = %q, want %q", status.State, GitShadowStateClean)
	}
	if status.GraftHead != graftCommit {
		t.Fatalf("graft head = %s, want %s", status.GraftHead, graftCommit)
	}

	if err := os.Remove(filepath.Join(r.GraftDir, gitMapFile)); err != nil {
		t.Fatalf("remove gitmap: %v", err)
	}
	status, err = r.GitShadowStatus()
	if err != nil {
		t.Fatalf("GitShadowStatus(unknown): %v", err)
	}
	if status.State != GitShadowStateUnknownMapping {
		t.Fatalf("state = %q, want %q", status.State, GitShadowStateUnknownMapping)
	}

	r.RecordGitShadowCommit(graftCommit, "test")
	shadowWriteFile(t, r.RootDir, "drift.txt", "out of band\n")
	gitOutput(t, r.RootDir, "add", "-A")
	gitOutput(t, r.RootDir, "commit", "-m", "out of band")
	status, err = r.GitShadowStatus()
	if err != nil {
		t.Fatalf("GitShadowStatus(diverged): %v", err)
	}
	if status.State != GitShadowStateDivergedCommit {
		t.Fatalf("state = %q, want %q", status.State, GitShadowStateDivergedCommit)
	}
	if !status.NeedsAttention() {
		t.Fatal("diverged status should need attention")
	}
}
