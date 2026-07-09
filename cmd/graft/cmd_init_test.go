package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/gitbridge"
)

func TestInit_FreshDirCreatesBothGraftAndGit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "myrepo")

	cmd := newInitCmd()
	cmd.SetArgs([]string{target})
	cmd.SetOut(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// .graft/ must exist
	if _, err := os.Stat(filepath.Join(target, ".graft")); err != nil {
		t.Error("expected .graft/ to exist after init")
	}

	// .git/ must exist
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Error("expected .git/ to exist after init (dual-repo mode)")
	}
}

func TestInit_PrintsNextStepsForFreshDir(t *testing.T) {
	parent := t.TempDir()
	restore := chdirForTest(t, parent)
	defer restore()

	var out bytes.Buffer
	cmd := newInitCmd()
	cmd.SetArgs([]string{"myrepo"})
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	raw := out.String()
	for _, want := range []string{
		"initialized graft repository in",
		"next steps:",
		"cd myrepo",
		"graft status",
		"graft add <files>",
		"graft commit -m \"initial commit\"",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("init output missing %q\nraw:\n%s", want, raw)
		}
	}
}

func TestInit_PrintsNextStepsWithoutCdForCurrentDir(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newInitCmd()
	cmd.SetArgs([]string{"."})
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	raw := out.String()
	if strings.Contains(raw, "\n  cd ") {
		t.Fatalf("init output should not include cd for current dir:\n%s", raw)
	}
	for _, want := range []string{"next steps:", "graft status", "graft add <files>"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("init output missing %q\nraw:\n%s", want, raw)
		}
	}
}

func TestInit_NoGitFlagSkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "myrepo")

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--no-git", target})
	cmd.SetOut(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --no-git failed: %v", err)
	}

	// .graft/ must exist
	if _, err := os.Stat(filepath.Join(target, ".graft")); err != nil {
		t.Error("expected .graft/ to exist after init --no-git")
	}

	// .git/ must NOT exist
	if _, err := os.Stat(filepath.Join(target, ".git")); err == nil {
		t.Error("expected .git/ to NOT exist after init --no-git")
	}
}

func TestInit_GtsExcludedInGitInfoExclude(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "myrepo")

	cmd := newInitCmd()
	cmd.SetArgs([]string{target})
	cmd.SetOut(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	excludePath := filepath.Join(target, ".git", "info", "exclude")
	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("could not read .git/info/exclude: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, ".gts/") {
		t.Errorf("expected .git/info/exclude to contain .gts/, got:\n%s", content)
	}
	if !strings.Contains(content, ".graft/") {
		t.Errorf("expected .git/info/exclude to contain .graft/, got:\n%s", content)
	}
}

func TestInit_ExistingGitRepoPrintsNextStepsToCommandOutput(t *testing.T) {
	dir := t.TempDir()
	if _, err := runGitCaptureWithLabel(context.Background(), dir, "test-git-init", "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}

	var out bytes.Buffer
	cmd := newInitCmd()
	cmd.SetArgs([]string{dir})
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init existing git failed: %v", err)
	}

	raw := out.String()
	for _, want := range []string{
		"initialized graft bridge alongside existing git repository",
		"next steps:",
		"graft status",
		"graft add <files>",
		"graft commit -m \"initial commit\"",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("init existing git output missing %q\nraw:\n%s", want, raw)
		}
	}
}

// TestInitGitShadow verifies the git-shadow initialization surfaces failure
// instead of swallowing it (the deep dive flagged init deferring a swallowed
// git-init error to a later confusing push).
func TestInitGitShadow(t *testing.T) {
	t.Run("success creates .git", func(t *testing.T) {
		dir := t.TempDir()
		if err := initGitShadow(dir); err != nil {
			t.Fatalf("initGitShadow on a clean dir failed: %v", err)
		}
		if !gitbridge.DetectGitRepo(dir) {
			t.Fatalf("expected .git repository to be created")
		}
	})

	t.Run("git unavailable returns error, not nil", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("PATH", "") // make the git binary unfindable
		if err := initGitShadow(dir); err == nil {
			t.Fatalf("expected an error when git is unavailable; got nil (error was swallowed)")
		}
	})
}
