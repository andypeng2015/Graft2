package repo

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepositoryLockRecordsOwnerAndRelease(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	lock, err := r.AcquireRepositoryLock("test-operation")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}

	status, err := r.RepositoryLockStatus()
	if err != nil {
		t.Fatalf("RepositoryLockStatus: %v", err)
	}
	if !status.Exists {
		t.Fatal("lock status Exists = false, want true")
	}
	if status.Stale {
		t.Fatalf("lock status Stale = true: %s", status.Reason)
	}
	if status.Info.Operation != "test-operation" {
		t.Fatalf("operation = %q, want test-operation", status.Info.Operation)
	}
	if status.Info.PID != os.Getpid() {
		t.Fatalf("pid = %d, want %d", status.Info.PID, os.Getpid())
	}
	if status.Info.Hostname == "" {
		t.Fatal("hostname is empty")
	}
	if status.Info.Command == "" {
		t.Fatal("command is empty")
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	status, err = r.RepositoryLockStatus()
	if err != nil {
		t.Fatalf("RepositoryLockStatus after release: %v", err)
	}
	if status.Exists {
		t.Fatal("lock still exists after release")
	}
}

func TestVerifyIntegrityReportsStaleRepositoryLock(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	writeStaleRepositoryLock(t, r, "commit")

	report := r.VerifyIntegrity()
	if report.OK {
		t.Fatal("VerifyIntegrity OK = true, want false")
	}
	if !reportHasDiagnostic(report, "repository_lock_stale") {
		t.Fatalf("missing repository_lock_stale diagnostic: %#v", report.Diagnostics)
	}
}

func TestAcquireRepositoryLockClearsStaleRepositoryLock(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	writeStaleRepositoryLock(t, r, "checkout")

	lock, err := r.AcquireRepositoryLock("add")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}
	defer lock.Release()

	status, err := r.RepositoryLockStatus()
	if err != nil {
		t.Fatalf("RepositoryLockStatus: %v", err)
	}
	if status.Info.Operation != "add" {
		t.Fatalf("operation after stale clear = %q, want add", status.Info.Operation)
	}
	if status.Stale {
		t.Fatalf("new lock is stale: %s", status.Reason)
	}
}

func TestAddHonorsRepositoryLock(t *testing.T) {
	t.Setenv("GRAFT_LOCK_WAIT_MS", "10")

	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	lock, err := r.AcquireRepositoryLock("held")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}
	defer lock.Release()

	err = r.Add([]string{"main.go"})
	if err == nil {
		t.Fatal("Add succeeded while repository lock was held")
	}
	if !strings.Contains(err.Error(), "repository is locked") {
		t.Fatalf("Add error = %v, want repository lock error", err)
	}
}

func TestRepositoryLockReentrantAllowsNestedMutation(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err = r.WithRepositoryLock("outer", func() error {
		status, err := r.RepositoryLockStatus()
		if err != nil {
			return err
		}
		if !status.Exists || status.Info.Operation != "outer" {
			t.Fatalf("lock status = %+v, want active outer lock", status)
		}
		return r.Add([]string{"main.go"})
	})
	if err != nil {
		t.Fatalf("WithRepositoryLock nested Add: %v", err)
	}

	status, err := r.RepositoryLockStatus()
	if err != nil {
		t.Fatalf("RepositoryLockStatus: %v", err)
	}
	if status.Exists {
		t.Fatalf("repository lock still exists after outer release: %+v", status)
	}
	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	if stg.Entries["main.go"] == nil {
		t.Fatal("nested Add did not stage main.go")
	}
}

func TestStashHonorsRepositoryLock(t *testing.T) {
	t.Setenv("GRAFT_LOCK_WAIT_MS", "10")

	r := initRepoWithFile(t, "main.go", []byte("package main\n"))
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"), []byte("package main\n\nfunc changed() {}\n"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}
	lock, err := r.AcquireRepositoryLock("held")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}
	defer lock.Release()

	_, err = r.Stash("tester")
	if err == nil {
		t.Fatal("Stash succeeded while repository lock was held")
	}
	if !strings.Contains(err.Error(), "repository is locked") {
		t.Fatalf("Stash error = %v, want repository lock error", err)
	}
}

func TestSparseCheckoutHonorsRepositoryLock(t *testing.T) {
	t.Setenv("GRAFT_LOCK_WAIT_MS", "10")

	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	lock, err := r.AcquireRepositoryLock("held")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}
	defer lock.Release()

	err = r.SparseCheckoutSet([]string{"src/"})
	if err == nil {
		t.Fatal("SparseCheckoutSet succeeded while repository lock was held")
	}
	if !strings.Contains(err.Error(), "repository is locked") {
		t.Fatalf("SparseCheckoutSet error = %v, want repository lock error", err)
	}
}

func TestRebaseHonorsRepositoryLock(t *testing.T) {
	t.Setenv("GRAFT_LOCK_WAIT_MS", "10")

	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	lock, err := r.AcquireRepositoryLock("held")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}
	defer lock.Release()

	err = r.Rebase("main")
	if err == nil {
		t.Fatal("Rebase succeeded while repository lock was held")
	}
	if !strings.Contains(err.Error(), "repository is locked") {
		t.Fatalf("Rebase error = %v, want repository lock error", err)
	}
}

func TestFetchHonorsRepositoryLock(t *testing.T) {
	t.Setenv("GRAFT_LOCK_WAIT_MS", "10")

	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	lock, err := r.AcquireRepositoryLock("held")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}
	defer lock.Release()

	_, err = r.Fetch("origin")
	if err == nil {
		t.Fatal("Fetch succeeded while repository lock was held")
	}
	if !strings.Contains(err.Error(), "repository is locked") {
		t.Fatalf("Fetch error = %v, want repository lock error", err)
	}
}

func TestModuleMutationHonorsRepositoryLock(t *testing.T) {
	t.Setenv("GRAFT_LOCK_WAIT_MS", "10")

	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	lock, err := r.AcquireRepositoryLock("held")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}
	defer lock.Release()

	err = r.AddModuleEntry(ModuleEntry{Name: "lib", URL: "https://example.com/lib", Path: "lib", Track: "main"})
	if err == nil {
		t.Fatal("AddModuleEntry succeeded while repository lock was held")
	}
	if !strings.Contains(err.Error(), "repository is locked") {
		t.Fatalf("AddModuleEntry error = %v, want repository lock error", err)
	}
}

func TestWorktreeMutationHonorsRepositoryLock(t *testing.T) {
	t.Setenv("GRAFT_LOCK_WAIT_MS", "10")

	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	lock, err := r.AcquireRepositoryLock("held")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}
	defer lock.Release()

	_, err = r.WorktreeAdd(filepath.Join(t.TempDir(), "wt"), "main")
	if err == nil {
		t.Fatal("WorktreeAdd succeeded while repository lock was held")
	}
	if !strings.Contains(err.Error(), "repository is locked") {
		t.Fatalf("WorktreeAdd error = %v, want repository lock error", err)
	}
}

func TestLinkedWorktreeUsesCommonRepositoryLock(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	linked := &Repo{
		RootDir:   t.TempDir(),
		GraftDir:  filepath.Join(r.GraftDir, "worktrees", "linked"),
		CommonDir: r.GraftDir,
		Store:     r.Store,
	}

	lock, err := r.AcquireRepositoryLock("main")
	if err != nil {
		t.Fatalf("AcquireRepositoryLock: %v", err)
	}
	defer lock.Release()

	status, err := linked.RepositoryLockStatus()
	if err != nil {
		t.Fatalf("linked RepositoryLockStatus: %v", err)
	}
	if !status.Exists {
		t.Fatal("linked worktree did not see common repository lock")
	}
	if status.Path != r.repositoryLockPath() {
		t.Fatalf("linked lock path = %q, want common path %q", status.Path, r.repositoryLockPath())
	}
}

func writeStaleRepositoryLock(t *testing.T, r *Repo, operation string) {
	t.Helper()
	hostname, _ := os.Hostname()
	info := RepositoryLockInfo{
		SchemaVersion: RepositoryLockSchemaVersion,
		Operation:     operation,
		PID:           0,
		Hostname:      hostname,
		Command:       "graft " + operation,
		StartedAt:     time.Now().Add(-repositoryLockTTL - time.Minute).UTC().Format(time.RFC3339Nano),
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(r.repositoryLockPath()), 0o755); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}
	if err := os.WriteFile(r.repositoryLockPath(), data, 0o644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}
	if err := os.Chtimes(r.repositoryLockPath(), time.Now().Add(-repositoryLockTTL-time.Minute), time.Now().Add(-repositoryLockTTL-time.Minute)); err != nil && !errors.Is(err, os.ErrPermission) {
		t.Fatalf("chtimes stale lock: %v", err)
	}
}

func reportHasDiagnostic(report *RepositoryIntegrityReport, code string) bool {
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}
