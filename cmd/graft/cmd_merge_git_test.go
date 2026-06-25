package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMergeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestGitMergeDriver_CleanStructuralMerge verifies the git merge-driver core:
// independent additions merge cleanly, the result is written back to the ours
// (%A) file, and conflict is false.
func TestGitMergeDriver_CleanStructuralMerge(t *testing.T) {
	dir := t.TempDir()
	base := writeMergeTemp(t, dir, "base", "package main\n\nfunc Base() {}\n")
	ours := writeMergeTemp(t, dir, "ours", "package main\n\nfunc Base() {}\n\nfunc Alpha() {}\n")
	theirs := writeMergeTemp(t, dir, "theirs", "package main\n\nfunc Base() {}\n\nfunc Zeta() {}\n")

	conflict, err := gitMergeDriver(base, ours, theirs, "x.go")
	if err != nil {
		t.Fatal(err)
	}
	if conflict {
		t.Fatalf("expected clean merge of independent additions")
	}
	got, err := os.ReadFile(ours)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "func Alpha()") || !strings.Contains(string(got), "func Zeta()") {
		t.Fatalf("merged result (written to %%A) missing one side's function:\n%s", got)
	}
}

// TestGitMergeDriver_Conflict verifies a genuine entity conflict is reported so
// the driver can exit non-zero.
func TestGitMergeDriver_Conflict(t *testing.T) {
	dir := t.TempDir()
	base := writeMergeTemp(t, dir, "base", "package main\n\nfunc F() int { return 0 }\n")
	ours := writeMergeTemp(t, dir, "ours", "package main\n\nfunc F() int { return 1 }\n")
	theirs := writeMergeTemp(t, dir, "theirs", "package main\n\nfunc F() int { return 2 }\n")

	conflict, err := gitMergeDriver(base, ours, theirs, "x.go")
	if err != nil {
		t.Fatal(err)
	}
	if !conflict {
		t.Fatalf("expected conflict: both sides changed F() differently")
	}
}

// TestMergeCmd_GitDriverFlag_CleanExitsZero exercises the full --git wiring:
// a clean driver merge returns nil (exit 0).
func TestMergeCmd_GitDriverFlag_CleanExitsZero(t *testing.T) {
	dir := t.TempDir()
	base := writeMergeTemp(t, dir, "base", "package main\n\nfunc Base() {}\n")
	ours := writeMergeTemp(t, dir, "ours", "package main\n\nfunc Base() {}\n\nfunc Alpha() {}\n")
	theirs := writeMergeTemp(t, dir, "theirs", "package main\n\nfunc Base() {}\n\nfunc Zeta() {}\n")

	cmd := newMergeCmd()
	cmd.SetArgs([]string{"--git", base, ours, theirs, "x.go"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("clean driver merge should exit 0, got error: %v", err)
	}
}

// TestMergeCmd_GitDriverFlag_ConflictReturnsError verifies a conflicted driver
// merge returns a non-nil error so main exits non-zero (git's conflict signal).
func TestMergeCmd_GitDriverFlag_ConflictReturnsError(t *testing.T) {
	dir := t.TempDir()
	base := writeMergeTemp(t, dir, "base", "package main\n\nfunc F() int { return 0 }\n")
	ours := writeMergeTemp(t, dir, "ours", "package main\n\nfunc F() int { return 1 }\n")
	theirs := writeMergeTemp(t, dir, "theirs", "package main\n\nfunc F() int { return 2 }\n")

	cmd := newMergeCmd()
	cmd.SetArgs([]string{"--git", base, ours, theirs, "x.go"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatalf("conflicted driver merge must return a non-nil error (non-zero exit)")
	}
}
