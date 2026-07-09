package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/gitbridge"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestRepairReseedPreservesTrackedIgnoredFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")

	writeVerifyCmdFile(t, filepath.Join(dir, ".graftignore"), []byte("orchard\n"))
	writeVerifyCmdFile(t, filepath.Join(dir, "README.md"), []byte("hello\n"))
	writeVerifyCmdFile(t, filepath.Join(dir, "cmd", "orchard", "main.go"), []byte("package main\n"))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	existing, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := existing.WriteConfig(&repo.Config{Remotes: map[string]string{"origin": "https://example.com/graft/repo"}}); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRepairCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"reseed", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}

	backups, err := filepath.Glob(dir + ".graft-backup-*")
	if err != nil {
		t.Fatalf("Glob backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("len(backups) = %d, want 1", len(backups))
	}

	reseeded, err := repo.Open(dir)
	if err != nil {
		t.Fatalf("repo.Open: %v", err)
	}
	headHash, err := reseeded.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	commitObj, err := reseeded.Store.ReadCommit(headHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	files, err := reseeded.FlattenTree(commitObj.TreeHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	got := make(map[string]struct{}, len(files))
	for _, file := range files {
		got[file.Path] = struct{}{}
	}
	for _, want := range []string{".graftignore", "README.md", "cmd/orchard/main.go"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing %s from reseeded commit", want)
		}
	}

	cfg, err := reseeded.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Remotes["origin"] != "https://example.com/graft/repo" {
		t.Fatalf("origin = %q, want preserved remote", cfg.Remotes["origin"])
	}

	statusEntries, err := reseeded.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statusEntries) != 3 {
		t.Fatalf("len(statusEntries) = %d, want 3 tracked files", len(statusEntries))
	}
	for _, entry := range statusEntries {
		if entry.IndexStatus != repo.StatusClean || entry.WorkStatus != repo.StatusClean {
			t.Fatalf("status entry %#v, want clean/clean", entry)
		}
	}

	bridge, err := gitbridge.OpenBridge(dir)
	if err != nil {
		t.Fatalf("OpenBridge: %v", err)
	}
	defer bridge.Close()

	if _, err := os.Stat(filepath.Join(dir, ".graft", "hashmap")); err != nil {
		t.Fatalf("hashmap missing: %v", err)
	}
}

func TestRepairCheckGitShadowJSONNoShadow(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRepairCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"check-git-shadow", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	var result JSONRepairGitShadowOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if !result.OK {
		t.Fatalf("ok = false, want true: %+v", result)
	}
	if result.State != repo.GitShadowStateNoShadow {
		t.Fatalf("state = %q, want %q", result.State, repo.GitShadowStateNoShadow)
	}
}

func TestRepairClearShadowFailures(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "shadow-failures.log"), []byte("failure\n"), 0o644); err != nil {
		t.Fatalf("write failure log: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRepairCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"clear-shadow-failures"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	if r.HasShadowFailures() {
		t.Fatal("failure log still present after clear-shadow-failures")
	}
	if !strings.Contains(out.String(), "cleared") {
		t.Fatalf("output = %q, want cleared message", out.String())
	}
}

func TestRepairClearStaleLocksJSON(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	lockPath := filepath.Join(dir, ".graft", "locks", "repository.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}
	hostname, _ := os.Hostname()
	data := []byte(fmt.Sprintf(
		"{\n  \"schema_version\": 1,\n  \"operation\": \"commit\",\n  \"pid\": 0,\n  \"hostname\": %q,\n  \"command\": \"graft commit\",\n  \"started_at\": %q\n}\n",
		hostname,
		time.Now().Add(-20*time.Minute).UTC().Format(time.RFC3339Nano),
	))
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRepairCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"clear-stale-locks", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	var result JSONRepairLockOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if !result.OK {
		t.Fatalf("ok = false, want true: %+v", result)
	}
	if result.State != "cleared" || !result.Cleared {
		t.Fatalf("result = %+v, want cleared state", result)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock stat after clear = %v, want not exist", err)
	}
}

func TestRepairTransactionMarkRolledBackJSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	tx, err := r.BeginTransaction("checkout")
	if err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}
	if err := tx.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRepairCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"transaction", tx.ID(), "--mark-rolled-back", "--reason", "verified manually", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	var result JSONRepairTransactionOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if !result.OK {
		t.Fatalf("ok = false, want true: %+v", result)
	}
	if result.Status != repo.TransactionStatusRolledBack {
		t.Fatalf("status = %q, want %q", result.Status, repo.TransactionStatusRolledBack)
	}
	if result.Error != "verified manually" {
		t.Fatalf("error = %q, want repair reason", result.Error)
	}
	report := r.VerifyIntegrity()
	for _, d := range report.Diagnostics {
		if d.Code == "transaction_incomplete" {
			t.Fatalf("transaction_incomplete still present: %+v", report.Diagnostics)
		}
	}
}

func TestRepairMigrateConfigJSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.Remove(filepath.Join(r.GraftDir, "config")); err != nil {
		t.Fatalf("remove config: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRepairCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"migrate-config", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	var result JSONRepairMigrateConfigOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if !result.OK || !result.Migrated {
		t.Fatalf("result = %+v, want ok migrated", result)
	}
	if result.FromVersion != 0 || result.ToVersion != repo.RepositoryFormatVersion {
		t.Fatalf("versions = %d -> %d, want 0 -> %d", result.FromVersion, result.ToVersion, repo.RepositoryFormatVersion)
	}
	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		t.Fatalf("ReadRepositoryConfig: %v", err)
	}
	if cfg.RepositoryFormatVersion != repo.RepositoryFormatVersion {
		t.Fatalf("repository_format_version = %d, want %d", cfg.RepositoryFormatVersion, repo.RepositoryFormatVersion)
	}
}

func TestRepairResyncGitRecordsCleanMapping(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")

	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	writeVerifyCmdFile(t, filepath.Join(dir, "main.go"), []byte("package main\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "Test User <test@example.com>"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	writeVerifyCmdFile(t, filepath.Join(dir, "drift.txt"), []byte("out of band\n"))
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "out of band")

	status, err := r.GitShadowStatus()
	if err != nil {
		t.Fatalf("GitShadowStatus: %v", err)
	}
	if status.State != repo.GitShadowStateDivergedCommit {
		t.Fatalf("state before resync = %q, want %q", status.State, repo.GitShadowStateDivergedCommit)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRepairCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"resync-git", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	var result JSONRepairGitShadowOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if !result.OK {
		t.Fatalf("ok = false after resync: %+v", result)
	}
	if result.State != repo.GitShadowStateClean {
		t.Fatalf("state after resync = %q, want %q", result.State, repo.GitShadowStateClean)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
