package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// Checkout switches the working directory to the state of the target.
// The target can be a branch name or a raw commit hash.
//
// Algorithm:
//  1. Check for uncommitted changes — refuse if any exist.
//  2. Resolve target: try as branch name first, then as raw hash.
//  3. Read the target commit, flatten its tree.
//  4. Remove all tracked files (files in current HEAD tree + staging).
//  5. Write all files from target tree to working directory.
//  6. Update staging to match the new tree.
//  7. Update HEAD (symbolic ref for branch, raw hash for detached).
func (r *Repo) Checkout(target string) error {
	return r.withRepositoryLock("checkout", func() error {
		return r.checkout(target)
	})
}

func (r *Repo) checkout(target string) (err error) {
	var tx *Transaction
	defer func() {
		if err != nil && tx != nil {
			_ = tx.MarkNeedsRepair(err.Error())
		}
	}()

	// 1. Check for uncommitted changes.
	if err := r.ensureClean(); err != nil {
		return fmt.Errorf("checkout: %w", err)
	}

	// 2. Resolve target.
	isBranch := false
	var targetHash object.Hash

	// Try as branch name first.
	branchHash, err := r.ResolveRef("refs/heads/" + target)
	if err == nil {
		targetHash = branchHash
		isBranch = true
	} else {
		// Try full resolution: tags, full refs, ancestor notation, raw hash.
		h, resolveErr := r.ResolveTreeish(target)
		if resolveErr != nil {
			return fmt.Errorf("checkout: cannot resolve %q: %w", target, resolveErr)
		}
		targetHash = h
	}

	// 3. Read the target commit and flatten its tree.
	commit, err := r.Store.ReadCommit(targetHash)
	if err != nil {
		return fmt.Errorf("checkout: cannot read commit %s: %w", targetHash, err)
	}

	targetFiles, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return fmt.Errorf("checkout: flatten target tree: %w", err)
	}

	// Build a map for quick lookup.
	targetMap := make(map[string]TreeFileEntry, len(targetFiles))
	for _, f := range targetFiles {
		targetMap[f.Path] = f
	}

	// 4. Determine files to remove: files in current HEAD tree + staging that
	//    are NOT in the target tree.
	currentFiles := r.trackedFiles()
	oldHeadName, _ := r.Head()
	oldHeadHash, _ := r.ResolveRef("HEAD")
	tx, err = r.BeginTransaction("checkout")
	if err != nil {
		return fmt.Errorf("checkout: begin transaction: %w", err)
	}
	if err := tx.AddRef("HEAD", oldHeadHash, targetHash); err != nil {
		return fmt.Errorf("checkout: record transaction ref: %w", err)
	}
	if err := tx.AddFiles(checkoutTransactionFiles(currentFiles, targetMap)); err != nil {
		return fmt.Errorf("checkout: record transaction files: %w", err)
	}
	if err := tx.Prepare(); err != nil {
		return fmt.Errorf("checkout: prepare transaction: %w", err)
	}

	sparseEnabled := r.IsSparseEnabled()

	for path := range currentFiles {
		// When sparse checkout is enabled, only remove files that were
		// materialized (i.e. matched sparse patterns).
		if sparseEnabled && !r.matchesSparsePatterns(path) {
			continue
		}
		absPath, err := r.safeWorktreePath(path)
		if err != nil {
			return fmt.Errorf("checkout: unsafe path %q: %w", path, err)
		}
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("checkout: remove %q: %w", path, err)
		}
		// Clean up empty parent directories.
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	// 5. Write all files from target tree (skip sidecar dirs and sparse-excluded files).
	for _, f := range targetFiles {
		if isSidecarPath(f.Path) {
			continue // sidecar files are restored separately after HEAD update
		}
		if sparseEnabled && !r.matchesSparsePatterns(f.Path) {
			continue
		}

		absPath, err := r.safeWorktreePath(f.Path)
		if err != nil {
			return fmt.Errorf("checkout: unsafe path %q: %w", f.Path, err)
		}

		// Create parent directories.
		dir := filepath.Dir(absPath)
		if err := ensureSafeParentDir(r.RootDir, absPath); err != nil {
			return fmt.Errorf("checkout: mkdir %q: %w", dir, err)
		}

		// Read blob from store and write to disk.
		blob, err := r.Store.ReadBlob(f.BlobHash)
		if err != nil {
			return fmt.Errorf("checkout: read blob for %q: %w", f.Path, err)
		}

		blobData := blob.Data
		// LFS: if blob is a pointer, restore actual content from LFS store.
		if ptr, ok := ParseLFSPointer(blobData); ok {
			lfsContent, err := r.ReadLFSObject(ptr.OID)
			if err == nil {
				blobData = lfsContent
			}
			// If LFS content not available, write pointer file as-is (lazy fetch later).
		}

		if err := os.WriteFile(absPath, blobData, filePermFromMode(f.Mode)); err != nil {
			return fmt.Errorf("checkout: write %q: %w", f.Path, err)
		}
	}

	// 6. Update staging to match the new tree (only materialized files, excluding sidecars).
	stg := &Staging{Entries: make(map[string]*StagingEntry, len(targetFiles))}
	for _, f := range targetFiles {
		if isSidecarPath(f.Path) {
			continue // sidecar files are not tracked in staging
		}
		if sparseEnabled && !r.matchesSparsePatterns(f.Path) {
			continue
		}

		absPath, err := r.safeWorktreePath(f.Path)
		if err != nil {
			return fmt.Errorf("checkout: unsafe path %q: %w", f.Path, err)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("checkout: stat %q: %w", f.Path, err)
		}

		entry := &StagingEntry{
			Path:           f.Path,
			BlobHash:       f.BlobHash,
			EntityListHash: f.EntityListHash,
		}
		setStagingEntryStat(entry, info, normalizeFileMode(f.Mode))
		stg.Entries[f.Path] = entry
	}
	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("checkout: %w", err)
	}

	// 7. Update HEAD.
	if isBranch {
		if err := r.setHeadSymbolic("refs/heads/" + target); err != nil {
			return fmt.Errorf("checkout: update HEAD: %w", err)
		}
	} else {
		if err := r.setHeadDetached(targetHash); err != nil {
			return fmt.Errorf("checkout: update HEAD: %w", err)
		}
	}

	// 8. Restore sidecar directories (.gts/) from the committed tree.
	r.restoreSidecarsFromTree(commit.TreeHash)

	// Sync modules if the new commit has module configuration.
	// Module sync failure is non-fatal during checkout — the user can retry
	// with 'graft module sync'.
	_ = r.ModuleSync()

	r.GitShadowCheckout(target)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("checkout: commit transaction: %w", err)
	}
	tx = nil

	if err := r.appendReflog("HEAD", oldHeadHash, targetHash, checkoutReflogReason(oldHeadName, targetHash, target, isBranch)); err != nil {
		return fmt.Errorf("checkout: record HEAD reflog: %w", err)
	}

	return nil
}

// PreviousCheckoutBranch returns the branch that was active before the most
// recent checkout into the current branch or detached HEAD.
func (r *Repo) PreviousCheckoutBranch() (string, error) {
	entries, err := r.ReadHEADReflog(50)
	if err != nil {
		return "", err
	}
	currentBranch, _ := r.CurrentBranch()

	for _, entry := range entries {
		from, to, ok := parseCheckoutReflogReason(entry.Reason)
		if !ok {
			continue
		}
		if currentBranch != "" && to != currentBranch {
			continue
		}
		if from == "" || from == currentBranch {
			continue
		}
		if _, err := r.ResolveRef("refs/heads/" + from); err == nil {
			return from, nil
		}
		if currentBranch != "" && to == currentBranch {
			return "", fmt.Errorf("previous branch %q no longer exists", from)
		}
	}

	return "", fmt.Errorf("no previous branch recorded")
}

func checkoutReflogReason(oldHeadName string, targetHash object.Hash, target string, isBranch bool) string {
	return fmt.Sprintf("checkout: moving from %s to %s", checkoutHeadLabel(oldHeadName), checkoutTargetLabel(targetHash, target, isBranch))
}

func checkoutTargetLabel(targetHash object.Hash, target string, isBranch bool) string {
	if isBranch {
		return target
	}
	return checkoutHeadLabel(string(targetHash))
}

func checkoutHeadLabel(head string) string {
	head = strings.TrimSpace(head)
	if branch, ok := checkoutBranchName(head); ok {
		return branch
	}
	if len(head) > 12 {
		return head[:12]
	}
	if head == "" {
		return "unknown"
	}
	return head
}

func checkoutBranchName(head string) (string, bool) {
	const prefix = "refs/heads/"
	if strings.HasPrefix(head, prefix) {
		return strings.TrimPrefix(head, prefix), true
	}
	return "", false
}

func parseCheckoutReflogReason(reason string) (from string, to string, ok bool) {
	const prefix = "checkout: moving from "
	if !strings.HasPrefix(reason, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(reason, prefix)
	parts := strings.SplitN(rest, " to ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	from = strings.TrimSpace(parts[0])
	to = strings.TrimSpace(parts[1])
	return from, to, from != "" && to != ""
}

func checkoutTransactionFiles(current map[string]bool, target map[string]TreeFileEntry) []string {
	seen := make(map[string]struct{}, len(current)+len(target))
	for path := range current {
		seen[path] = struct{}{}
	}
	for path := range target {
		seen[path] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// ensureClean checks that the working tree has no uncommitted changes.
// It returns an error if there are any staged changes or dirty files.
func (r *Repo) ensureClean() error {
	entries, err := r.Status()
	if err != nil {
		return fmt.Errorf("check status: %w", err)
	}

	for _, e := range entries {
		if e.IndexStatus != StatusClean || e.WorkStatus != StatusClean {
			return fmt.Errorf("working tree is not clean (file %q has uncommitted changes)", e.Path)
		}
	}
	return nil
}

// trackedFiles returns a set of all currently tracked file paths. It merges
// paths from the HEAD tree and the staging index.
func (r *Repo) trackedFiles() map[string]bool {
	files := make(map[string]bool)

	// From HEAD tree.
	headEntries := r.headTreeEntries()
	for path := range headEntries {
		files[path] = true
	}

	// From staging.
	stg, err := r.ReadStaging()
	if err == nil {
		for path := range stg.Entries {
			files[path] = true
		}
	}

	return files
}

// removeEmptyParents removes empty directories up to (but not including)
// the repository root.
func (r *Repo) removeEmptyParents(dir string) {
	for {
		// Never remove the repo root itself.
		if dir == r.RootDir || !strings.HasPrefix(dir, r.RootDir) {
			return
		}

		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}

		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}
