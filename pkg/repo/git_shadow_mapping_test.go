package repo

import "testing"

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
