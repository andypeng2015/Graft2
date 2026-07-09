package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestCommitWritesCommittedTransaction(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n"))
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	records, err := r.ListTransactions()
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	rec := records[0]
	if rec.Operation != "commit" {
		t.Fatalf("operation = %q, want commit", rec.Operation)
	}
	if rec.Status != TransactionStatusCommitted {
		t.Fatalf("status = %q, want %q", rec.Status, TransactionStatusCommitted)
	}
	if len(rec.TouchedRefs) != 1 || rec.TouchedRefs[0].Ref != "refs/heads/main" {
		t.Fatalf("touched refs = %+v, want refs/heads/main", rec.TouchedRefs)
	}
	if len(rec.TouchedFiles) != 1 || rec.TouchedFiles[0] != "main.go" {
		t.Fatalf("touched files = %+v, want main.go", rec.TouchedFiles)
	}
}

func TestVerifyIntegrityReportsIncompleteTransaction(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	tx, err := r.BeginTransaction("commit")
	if err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}
	if err := tx.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	report := r.VerifyIntegrity()
	if report.OK {
		t.Fatal("VerifyIntegrity OK = true, want false")
	}
	if !hasRepositoryDiagnostic(report, "transaction_incomplete") {
		t.Fatalf("diagnostics missing transaction_incomplete: %+v", report.Diagnostics)
	}
}

func TestMarkTransactionRolledBackClearsDiagnostic(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	tx, err := r.BeginTransaction("checkout")
	if err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}
	if err := tx.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	rec, err := r.MarkTransactionRolledBack(tx.ID(), "verified refs and worktree")
	if err != nil {
		t.Fatalf("MarkTransactionRolledBack: %v", err)
	}
	if rec.Status != TransactionStatusRolledBack {
		t.Fatalf("status = %q, want %q", rec.Status, TransactionStatusRolledBack)
	}
	if rec.Error != "verified refs and worktree" {
		t.Fatalf("error = %q, want repair reason", rec.Error)
	}

	report := r.VerifyIntegrity()
	if hasRepositoryDiagnostic(report, "transaction_incomplete") {
		t.Fatalf("transaction_incomplete still present: %+v", report.Diagnostics)
	}
}

func TestCheckoutWritesCommittedTransaction(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n"))
	base, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := r.CreateBranch("feature", base); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	rec := latestTransactionForOperation(t, r, "checkout")
	if rec.Status != TransactionStatusCommitted {
		t.Fatalf("status = %q, want %q", rec.Status, TransactionStatusCommitted)
	}
	if len(rec.TouchedRefs) != 1 || rec.TouchedRefs[0].Ref != "HEAD" {
		t.Fatalf("touched refs = %+v, want HEAD", rec.TouchedRefs)
	}
	if len(rec.TouchedFiles) != 1 || rec.TouchedFiles[0] != "main.go" {
		t.Fatalf("touched files = %+v, want main.go", rec.TouchedFiles)
	}
}

func TestResetWritesCommittedTransaction(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n"))
	first, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit initial: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"), []byte("package main\n\nfunc changed() {}\n"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add changed: %v", err)
	}
	if _, err := r.Commit("changed", "tester"); err != nil {
		t.Fatalf("Commit changed: %v", err)
	}

	if err := r.ResetToCommit(first, ResetMixed); err != nil {
		t.Fatalf("ResetToCommit: %v", err)
	}

	rec := latestTransactionForOperation(t, r, "reset")
	if rec.Status != TransactionStatusCommitted {
		t.Fatalf("status = %q, want %q", rec.Status, TransactionStatusCommitted)
	}
	if len(rec.TouchedRefs) != 1 || rec.TouchedRefs[0].NewHash != first {
		t.Fatalf("touched refs = %+v, want reset to %s", rec.TouchedRefs, first)
	}
	if len(rec.TouchedFiles) != 1 || rec.TouchedFiles[0] != "main.go" {
		t.Fatalf("touched files = %+v, want main.go", rec.TouchedFiles)
	}
}

func TestFetchWritesCommittedTransaction(t *testing.T) {
	local, _, commitHash := setupRemotePair(t)

	if _, err := local.Fetch("origin"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	rec := latestTransactionForOperation(t, local, "fetch")
	if rec.Status != TransactionStatusCommitted {
		t.Fatalf("status = %q, want %q", rec.Status, TransactionStatusCommitted)
	}
	if !transactionHasRefUpdate(rec, "refs/remotes/origin/heads/main", commitHash) {
		t.Fatalf("touched refs = %+v, want origin/main -> %s", rec.TouchedRefs, commitHash)
	}
	if !transactionHasTouchedFile(rec, ".graft/refs/remotes/origin") {
		t.Fatalf("touched files = %+v, want remote refs path", rec.TouchedFiles)
	}
}

func TestModuleMutationsWriteCommittedTransactions(t *testing.T) {
	r := createTestRepo(t)
	entry := ModuleEntry{
		Name:  "lib",
		URL:   "https://example.com/lib.git",
		Path:  "vendor/lib",
		Track: "main",
	}

	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}
	addRec := latestTransactionForOperation(t, r, "module-add")
	if addRec.Status != TransactionStatusCommitted {
		t.Fatalf("module-add status = %q, want %q", addRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedFile(addRec, ".graftmodules") ||
		!transactionHasTouchedFile(addRec, ".graft/modules/lib") {
		t.Fatalf("module-add touched files = %+v", addRec.TouchedFiles)
	}

	lockHash := object.Hash("1111111111111111111111111111111111111111")
	if err := r.UpdateModuleLock("lib", lockHash, "https://example.com/lib.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}
	lockRec := latestTransactionForOperation(t, r, "module-lock")
	if lockRec.Status != TransactionStatusCommitted {
		t.Fatalf("module-lock status = %q, want %q", lockRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedFile(lockRec, ".graftmodules.lock") {
		t.Fatalf("module-lock touched files = %+v", lockRec.TouchedFiles)
	}

	if err := r.RemoveModuleEntry("lib"); err != nil {
		t.Fatalf("RemoveModuleEntry: %v", err)
	}
	removeRec := latestTransactionForOperation(t, r, "module-remove")
	if removeRec.Status != TransactionStatusCommitted {
		t.Fatalf("module-remove status = %q, want %q", removeRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedFile(removeRec, ".graftmodules") ||
		!transactionHasTouchedFile(removeRec, ".graftmodules.lock") ||
		!transactionHasTouchedFile(removeRec, ".graft/modules/lib") {
		t.Fatalf("module-remove touched files = %+v", removeRec.TouchedFiles)
	}
}

func TestStashMutationsWriteCommittedTransactions(t *testing.T) {
	r, _ := initRepoWithCommit(t, "main.go", []byte("package main\n"), "initial")
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"), []byte("package main\n\nfunc changed() {}\n"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}

	if _, err := r.Stash("tester"); err != nil {
		t.Fatalf("Stash: %v", err)
	}
	stashRec := latestTransactionForOperation(t, r, "stash")
	if stashRec.Status != TransactionStatusCommitted {
		t.Fatalf("stash status = %q, want %q", stashRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedFile(stashRec, ".graft/stash") ||
		!transactionHasTouchedFile(stashRec, "main.go") {
		t.Fatalf("stash touched files = %+v", stashRec.TouchedFiles)
	}

	if _, err := r.StashApplyMerge(0); err != nil {
		t.Fatalf("StashApplyMerge: %v", err)
	}
	applyRec := latestTransactionForOperation(t, r, "stash-apply")
	if applyRec.Status != TransactionStatusCommitted {
		t.Fatalf("stash-apply status = %q, want %q", applyRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedFile(applyRec, "main.go") {
		t.Fatalf("stash-apply touched files = %+v", applyRec.TouchedFiles)
	}

	if err := r.StashDrop(0); err != nil {
		t.Fatalf("StashDrop: %v", err)
	}
	dropRec := latestTransactionForOperation(t, r, "stash-drop")
	if dropRec.Status != TransactionStatusCommitted {
		t.Fatalf("stash-drop status = %q, want %q", dropRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedFile(dropRec, ".graft/stash") {
		t.Fatalf("stash-drop touched files = %+v", dropRec.TouchedFiles)
	}
}

func TestWorktreeMutationsWriteCommittedTransactions(t *testing.T) {
	r, dir := setupRepoWithCommit(t)
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	wtPath := filepath.Join(dir, "wt-tx")
	if _, err := r.WorktreeAdd(wtPath, "feature"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}
	addRec := latestTransactionForOperation(t, r, "worktree-add")
	if addRec.Status != TransactionStatusCommitted {
		t.Fatalf("worktree-add status = %q, want %q", addRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedFile(addRec, ".graft/worktrees/wt-tx") ||
		!transactionHasTouchedFile(addRec, "worktree:"+filepath.ToSlash(wtPath)) {
		t.Fatalf("worktree-add touched files = %+v", addRec.TouchedFiles)
	}

	if err := r.WorktreeRemove("wt-tx"); err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}
	removeRec := latestTransactionForOperation(t, r, "worktree-remove")
	if removeRec.Status != TransactionStatusCommitted {
		t.Fatalf("worktree-remove status = %q, want %q", removeRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedFile(removeRec, ".graft/worktrees/wt-tx") {
		t.Fatalf("worktree-remove touched files = %+v", removeRec.TouchedFiles)
	}

	prunePath := filepath.Join(dir, "wt-prune")
	if _, err := r.WorktreeAdd(prunePath, "feature"); err != nil {
		t.Fatalf("WorktreeAdd prune target: %v", err)
	}
	if err := os.RemoveAll(prunePath); err != nil {
		t.Fatalf("RemoveAll prune target: %v", err)
	}
	if err := r.WorktreePrune(); err != nil {
		t.Fatalf("WorktreePrune: %v", err)
	}
	pruneRec := latestTransactionForOperation(t, r, "worktree-prune")
	if pruneRec.Status != TransactionStatusCommitted {
		t.Fatalf("worktree-prune status = %q, want %q", pruneRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedFile(pruneRec, ".graft/worktrees/wt-prune") {
		t.Fatalf("worktree-prune touched files = %+v", pruneRec.TouchedFiles)
	}
}

func TestRebaseStateTransitionsWriteCommittedTransactions(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	rebaseCommitFile(t, r, "base.txt", []byte("base\n"), "initial", "alice")
	baseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch feature: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout feature: %v", err)
	}
	rebaseCommitFile(t, r, "feature.txt", []byte("feature\n"), "feature", "bob")
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout main: %v", err)
	}
	rebaseCommitFile(t, r, "main.txt", []byte("main\n"), "main", "alice")
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout feature again: %v", err)
	}
	if err := r.Rebase("main"); err != nil {
		t.Fatalf("Rebase main: %v", err)
	}

	finishRec := latestTransactionForOperation(t, r, "rebase-finish")
	if finishRec.Status != TransactionStatusCommitted {
		t.Fatalf("rebase-finish status = %q, want %q", finishRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedRef(finishRec, "refs/heads/feature") ||
		!transactionHasTouchedFile(finishRec, ".graft/rebase-merge") {
		t.Fatalf("rebase-finish transaction = %+v", finishRec)
	}

	abortDir := t.TempDir()
	abortRepo, err := Init(abortDir)
	if err != nil {
		t.Fatalf("Init abort repo: %v", err)
	}
	rebaseCommitFile(t, abortRepo, "conflict.txt", []byte("base\n"), "initial", "alice")
	abortBaseHash, err := abortRepo.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef abort HEAD: %v", err)
	}
	if err := abortRepo.CreateBranch("abort-feature", abortBaseHash); err != nil {
		t.Fatalf("CreateBranch abort-feature: %v", err)
	}
	if err := abortRepo.Checkout("abort-feature"); err != nil {
		t.Fatalf("Checkout abort-feature: %v", err)
	}
	rebaseCommitFile(t, abortRepo, "conflict.txt", []byte("feature\n"), "feature conflict", "bob")
	if err := abortRepo.Checkout("main"); err != nil {
		t.Fatalf("Checkout abort main: %v", err)
	}
	rebaseCommitFile(t, abortRepo, "conflict.txt", []byte("main\n"), "main conflict", "alice")
	if err := abortRepo.Checkout("abort-feature"); err != nil {
		t.Fatalf("Checkout abort-feature again: %v", err)
	}
	if err := abortRepo.Rebase("main"); err == nil {
		t.Fatal("expected rebase conflict")
	}
	if err := abortRepo.RebaseAbort(); err != nil {
		t.Fatalf("RebaseAbort: %v", err)
	}

	abortRec := latestTransactionForOperation(t, abortRepo, "rebase-abort")
	if abortRec.Status != TransactionStatusCommitted {
		t.Fatalf("rebase-abort status = %q, want %q", abortRec.Status, TransactionStatusCommitted)
	}
	if !transactionHasTouchedRef(abortRec, "refs/heads/abort-feature") ||
		!transactionHasTouchedFile(abortRec, ".graft/rebase-merge") {
		t.Fatalf("rebase-abort transaction = %+v", abortRec)
	}
}

func latestTransactionForOperation(t *testing.T, r *Repo, operation string) TransactionRecord {
	t.Helper()
	records, err := r.ListTransactions()
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Operation == operation {
			return records[i]
		}
	}
	t.Fatalf("missing transaction operation %q in %+v", operation, records)
	return TransactionRecord{}
}

func transactionHasTouchedFile(rec TransactionRecord, path string) bool {
	for _, touched := range rec.TouchedFiles {
		if touched == path {
			return true
		}
	}
	return false
}

func transactionHasTouchedRef(rec TransactionRecord, ref string) bool {
	for _, touched := range rec.TouchedRefs {
		if touched.Ref == ref {
			return true
		}
	}
	return false
}

func transactionHasRefUpdate(rec TransactionRecord, ref string, newHash object.Hash) bool {
	for _, touched := range rec.TouchedRefs {
		if touched.Ref == ref && touched.NewHash == newHash {
			return true
		}
	}
	return false
}
