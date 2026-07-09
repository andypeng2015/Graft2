package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/userconfig"
)

// formatAuthor returns "Name <email>" if both are set, just name if only
// name is set, or empty string if name is empty.
func formatAuthor(name, email string) string {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" {
		return ""
	}
	if email != "" {
		return name + " <" + email + ">"
	}
	return name
}

// ResolveAuthor determines the commit author by checking, in priority order:
//  1. Repo config user.name + user.email
//  2. User config (~/.graftconfig) name + email
//  3. $USER environment variable
//  4. "unknown"
func (r *Repo) ResolveAuthor() string {
	// 1. Repo-level config.
	if cfg, err := r.ReadConfig(); err == nil && cfg.User != nil {
		if author := formatAuthor(cfg.User.Name, cfg.User.Email); author != "" {
			return author
		}
	}

	// 2. User-level config (~/.graftconfig).
	if ucfg, err := userconfig.Load(); err == nil && ucfg != nil {
		if author := formatAuthor(ucfg.Name, ucfg.Email); author != "" {
			return author
		}
	}

	// 3. $USER env var.
	if u := os.Getenv("USER"); u != "" {
		return u
	}

	// 4. Fallback.
	return "unknown"
}

// CommitSigner signs canonical commit payload bytes and returns an encoded
// signature string to be persisted in CommitObj.Signature.
type CommitSigner func(payload []byte) (string, error)

// CommitOptions controls optional commit behaviors.
type CommitOptions struct {
	Signer CommitSigner
	Hooks  HookRunOptions
}

// Commit creates a new commit from the current staging area.
//
//  1. Read staging
//  2. BuildTree from staging
//  3. Resolve HEAD to get parent commit hash (if any)
//  4. Create CommitObj with tree hash, parent, author, current timestamp, message
//  5. Write commit to store
//  6. Update current branch ref to new commit hash
//  7. Return commit hash
func (r *Repo) Commit(message, author string) (object.Hash, error) {
	return r.CommitWithOptions(message, author, CommitOptions{})
}

// CommitWithSigner creates a new commit and signs it when signer is provided.
func (r *Repo) CommitWithSigner(message, author string, signer CommitSigner) (commitHash object.Hash, err error) {
	return r.CommitWithOptions(message, author, CommitOptions{Signer: signer})
}

// CommitWithOptions creates a new commit with explicit optional behaviors.
func (r *Repo) CommitWithOptions(message, author string, opts CommitOptions) (commitHash object.Hash, err error) {
	err = r.withRepositoryLock("commit", func() error {
		var commitErr error
		commitHash, commitErr = r.commitWithOptions(message, author, opts)
		return commitErr
	})
	return commitHash, err
}

func (r *Repo) commitWithOptions(message, author string, opts CommitOptions) (commitHash object.Hash, err error) {
	var tx *Transaction
	defer func() {
		if err != nil && tx != nil {
			_ = tx.MarkNeedsRepair(err.Error())
		}
	}()

	// 0a. Run pre-commit hook. If it fails, abort.
	if err := r.RunHookWithOptions(HookPreCommit, opts.Hooks); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	// 0b. Run commit-msg hook. Write message to temp file, let hook modify it,
	// then read back the (possibly modified) message.
	msgFile := filepath.Join(r.GraftDir, "COMMIT_EDITMSG")
	if err := os.WriteFile(msgFile, []byte(message), 0o644); err != nil {
		return "", fmt.Errorf("commit: write message file: %w", err)
	}
	if err := r.RunHookWithOptions(HookCommitMsg, opts.Hooks, msgFile); err != nil {
		os.Remove(msgFile)
		return "", fmt.Errorf("commit: %w", err)
	}
	modifiedMsg, err := os.ReadFile(msgFile)
	if err != nil {
		return "", fmt.Errorf("commit: read message file: %w", err)
	}
	os.Remove(msgFile)
	message = string(modifiedMsg)

	// 1. Read staging.
	stg, err := r.ReadStaging()
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	if len(stg.Entries) == 0 {
		return "", fmt.Errorf("commit: nothing staged")
	}

	// 1b. Run pre-commit-analysis hooks before building the tree. These
	// allow analysis tools to write sidecar files (e.g. .gts/) that
	// BuildTree will inject into the committed tree.
	stagedPaths := make([]string, 0, len(stg.Entries))
	for p := range stg.Entries {
		stagedPaths = append(stagedPaths, p)
	}
	sort.Strings(stagedPaths)
	r.RunHooksForPointByNameWithOptions(string(HookPreCommitAnalysis), []byte(strings.Join(stagedPaths, "\n")), false, opts.Hooks)

	// 2. Build tree from staging.
	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	// 3. Resolve HEAD to get parent (may not exist for first commit).
	var parents []object.Hash
	parentHash, err := r.ResolveRef("HEAD")
	if err == nil && parentHash != "" {
		parents = append(parents, parentHash)
	}
	// If HEAD resolution fails (e.g., first commit, no ref file), that's fine.

	// 4. Create CommitObj.
	commitObj := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    author,
		Timestamp: time.Now().Unix(),
		Message:   message,
	}
	if opts.Signer != nil {
		payload := object.CommitSigningPayload(commitObj)
		signature, err := opts.Signer(payload)
		if err != nil {
			return "", fmt.Errorf("commit: sign commit: %w", err)
		}
		commitObj.Signature = signature
	}

	// 5. Resolve the ref that will move and start a transaction before
	// writing immutable objects or refs.
	head, err := r.Head()
	if err != nil {
		return "", fmt.Errorf("commit: read HEAD: %w", err)
	}
	tx, err = r.BeginTransaction("commit")
	if err != nil {
		return "", fmt.Errorf("commit: begin transaction: %w", err)
	}
	for _, p := range stagedPaths {
		if err := tx.AddFile(p); err != nil {
			return "", fmt.Errorf("commit: record transaction file %q: %w", p, err)
		}
	}

	// 6. Write commit to store.
	commitHash, err = r.Store.WriteCommit(commitObj)
	if err != nil {
		return "", fmt.Errorf("commit: write commit: %w", err)
	}

	// head is either a ref path ("refs/heads/main") or a detached hash.
	if strings.HasPrefix(head, "refs/") {
		if err := tx.AddRef(head, parentHash, commitHash); err != nil {
			return "", fmt.Errorf("commit: record transaction ref %q: %w", head, err)
		}
		if err := tx.Prepare(); err != nil {
			return "", fmt.Errorf("commit: prepare transaction: %w", err)
		}
		var updateErr error
		if parentHash == "" {
			updateErr = r.UpdateRefCAS(head, commitHash)
		} else {
			updateErr = r.UpdateRefCAS(head, commitHash, parentHash)
		}
		if updateErr != nil {
			return "", fmt.Errorf("commit: update ref %q: %w", head, updateErr)
		}
	} else {
		// Detached HEAD: update HEAD directly with a CAS against the old hash.
		oldHead := object.Hash(strings.TrimSpace(head))
		if err := tx.AddRef("HEAD", oldHead, commitHash); err != nil {
			return "", fmt.Errorf("commit: record transaction detached HEAD: %w", err)
		}
		if err := tx.Prepare(); err != nil {
			return "", fmt.Errorf("commit: prepare transaction: %w", err)
		}
		if err := r.UpdateRefCAS("HEAD", commitHash, oldHead); err != nil {
			return "", fmt.Errorf("commit: update detached HEAD: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: commit transaction: %w", err)
	}

	r.invalidateStatusCache()
	r.InvalidateMergeBaseCache()

	// 7. Mirror to git if a colocated .git/ directory exists.
	r.GitShadowCommit(message, author, false)
	r.recordGitShadowCommit(commitHash)

	// 8. Return commit hash.
	return commitHash, nil
}

// CommitAmend replaces the current HEAD commit with a new one built from the
// current staging area. The new commit inherits the parent(s) of the original
// HEAD commit (not HEAD itself). If message is empty, the original commit's
// message is reused.
func (r *Repo) CommitAmend(message, author string) (object.Hash, error) {
	return r.CommitAmendWithOptions(message, author, CommitOptions{})
}

// CommitAmendWithSigner is like CommitAmend but signs the new commit when
// signer is non-nil.
func (r *Repo) CommitAmendWithSigner(message, author string, signer CommitSigner) (commitHash object.Hash, err error) {
	return r.CommitAmendWithOptions(message, author, CommitOptions{Signer: signer})
}

// CommitAmendWithOptions is like CommitAmend with explicit optional behaviors.
func (r *Repo) CommitAmendWithOptions(message, author string, opts CommitOptions) (commitHash object.Hash, err error) {
	err = r.withRepositoryLock("commit-amend", func() error {
		var commitErr error
		commitHash, commitErr = r.commitAmendWithOptions(message, author, opts)
		return commitErr
	})
	return commitHash, err
}

func (r *Repo) commitAmendWithOptions(message, author string, opts CommitOptions) (commitHash object.Hash, err error) {
	var tx *Transaction
	defer func() {
		if err != nil && tx != nil {
			_ = tx.MarkNeedsRepair(err.Error())
		}
	}()

	// 1. Read the current HEAD commit.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return "", fmt.Errorf("commit --amend: cannot resolve HEAD: %w", err)
	}
	oldCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return "", fmt.Errorf("commit --amend: read HEAD commit: %w", err)
	}

	// 2. If message is empty, reuse the original commit's message.
	if message == "" {
		message = oldCommit.Message
	}

	// 3. Run pre-commit hook.
	if err := r.RunHookWithOptions(HookPreCommit, opts.Hooks); err != nil {
		return "", fmt.Errorf("commit --amend: %w", err)
	}

	// 4. Run commit-msg hook.
	msgFile := filepath.Join(r.GraftDir, "COMMIT_EDITMSG")
	if err := os.WriteFile(msgFile, []byte(message), 0o644); err != nil {
		return "", fmt.Errorf("commit --amend: write message file: %w", err)
	}
	if err := r.RunHookWithOptions(HookCommitMsg, opts.Hooks, msgFile); err != nil {
		os.Remove(msgFile)
		return "", fmt.Errorf("commit --amend: %w", err)
	}
	modifiedMsg, err := os.ReadFile(msgFile)
	if err != nil {
		return "", fmt.Errorf("commit --amend: read message file: %w", err)
	}
	os.Remove(msgFile)
	message = string(modifiedMsg)

	// 5. Read staging and build tree.
	stg, err := r.ReadStaging()
	if err != nil {
		return "", fmt.Errorf("commit --amend: %w", err)
	}
	if len(stg.Entries) == 0 {
		return "", fmt.Errorf("commit --amend: nothing staged")
	}

	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return "", fmt.Errorf("commit --amend: %w", err)
	}

	// 6. Use HEAD's parents (not HEAD itself) as the new commit's parents.
	parents := oldCommit.Parents

	// 7. Create the new commit object.
	commitObj := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    author,
		Timestamp: time.Now().Unix(),
		Message:   message,
	}
	if opts.Signer != nil {
		payload := object.CommitSigningPayload(commitObj)
		signature, err := opts.Signer(payload)
		if err != nil {
			return "", fmt.Errorf("commit --amend: sign commit: %w", err)
		}
		commitObj.Signature = signature
	}

	// 8. Resolve the ref that will move and start the transaction.
	head, err := r.Head()
	if err != nil {
		return "", fmt.Errorf("commit --amend: read HEAD: %w", err)
	}
	tx, err = r.BeginTransaction("commit-amend")
	if err != nil {
		return "", fmt.Errorf("commit --amend: begin transaction: %w", err)
	}
	for p := range stg.Entries {
		if err := tx.AddFile(p); err != nil {
			return "", fmt.Errorf("commit --amend: record transaction file %q: %w", p, err)
		}
	}

	// 9. Write the new commit to store.
	commitHash, err = r.Store.WriteCommit(commitObj)
	if err != nil {
		return "", fmt.Errorf("commit --amend: write commit: %w", err)
	}

	if strings.HasPrefix(head, "refs/") {
		if err := tx.AddRef(head, headHash, commitHash); err != nil {
			return "", fmt.Errorf("commit --amend: record transaction ref %q: %w", head, err)
		}
		if err := tx.Prepare(); err != nil {
			return "", fmt.Errorf("commit --amend: prepare transaction: %w", err)
		}
		if err := r.UpdateRefCAS(head, commitHash, headHash); err != nil {
			return "", fmt.Errorf("commit --amend: update ref %q: %w", head, err)
		}
	} else {
		if err := tx.AddRef("HEAD", headHash, commitHash); err != nil {
			return "", fmt.Errorf("commit --amend: record transaction detached HEAD: %w", err)
		}
		if err := tx.Prepare(); err != nil {
			return "", fmt.Errorf("commit --amend: prepare transaction: %w", err)
		}
		if err := r.UpdateRefCAS("HEAD", commitHash, headHash); err != nil {
			return "", fmt.Errorf("commit --amend: update detached HEAD: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit --amend: commit transaction: %w", err)
	}

	r.invalidateStatusCache()
	r.InvalidateMergeBaseCache()

	r.GitShadowCommit(message, author, true)
	r.recordGitShadowCommit(commitHash)

	return commitHash, nil
}

// Log walks the commit history starting from the given hash, following
// first-parent links, returning up to limit commits in reverse-chronological
// order (newest first). In a shallow repository, walking stops at shallow
// boundaries instead of erroring on missing parent commits.
func (r *Repo) Log(start object.Hash, limit int) ([]*object.CommitObj, error) {
	shallow, _ := r.ShallowState()

	var commits []*object.CommitObj
	current := start

	for len(commits) < limit {
		c, err := r.Store.ReadCommit(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// In a shallow repo, a missing commit at a boundary is expected.
				if shallow != nil && shallow.IsShallow(current) {
					break
				}
				break
			}
			return nil, fmt.Errorf("log: read commit %s: %w", current, err)
		}
		commits = append(commits, c)

		// Follow first parent.
		if len(c.Parents) == 0 {
			break
		}
		next := c.Parents[0]
		// Stop at shallow boundaries.
		if shallow != nil && shallow.IsShallow(next) {
			break
		}
		current = next
	}

	return commits, nil
}

// commitStagingParams holds parameters for commitFromStaging.
type commitStagingParams struct {
	Message  string
	Author   string
	Parents  []object.Hash
	HeadName string      // ref to update; empty = resolve from current HEAD
	HeadHash object.Hash // expected hash for CAS update
}

// commitFromStaging reads the staging area, builds a tree, creates a commit,
// and advances the current branch ref. Used by cherry-pick, revert, and
// their --continue paths to avoid duplicating the stage→commit→ref-update flow.
func (r *Repo) commitFromStaging(p commitStagingParams) (commitHash object.Hash, err error) {
	err = r.withRepositoryLock("commit-from-staging", func() error {
		var commitErr error
		commitHash, commitErr = r.commitFromStagingLocked(p)
		return commitErr
	})
	return commitHash, err
}

func (r *Repo) commitFromStagingLocked(p commitStagingParams) (commitHash object.Hash, err error) {
	var tx *Transaction
	defer func() {
		if err != nil && tx != nil {
			_ = tx.MarkNeedsRepair(err.Error())
		}
	}()

	stg, err := r.ReadStaging()
	if err != nil {
		return "", fmt.Errorf("read staging: %w", err)
	}
	if len(stg.Entries) == 0 {
		return "", fmt.Errorf("nothing staged")
	}

	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return "", fmt.Errorf("build tree: %w", err)
	}

	commitObj := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   p.Parents,
		Author:    p.Author,
		Timestamp: time.Now().Unix(),
		Message:   p.Message,
	}

	headName := p.HeadName
	if headName == "" {
		head, err := r.Head()
		if err != nil {
			return "", fmt.Errorf("read HEAD: %w", err)
		}
		headName = head
	}

	tx, err = r.BeginTransaction("commit-from-staging")
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	for path := range stg.Entries {
		if err := tx.AddFile(path); err != nil {
			return "", fmt.Errorf("record transaction file %q: %w", path, err)
		}
	}

	commitHash, err = r.Store.WriteCommit(commitObj)
	if err != nil {
		return "", fmt.Errorf("write commit: %w", err)
	}

	if strings.HasPrefix(headName, "refs/") {
		if err := tx.AddRef(headName, p.HeadHash, commitHash); err != nil {
			return "", fmt.Errorf("record transaction ref %q: %w", headName, err)
		}
		if err := tx.Prepare(); err != nil {
			return "", fmt.Errorf("prepare transaction: %w", err)
		}
		if err := r.UpdateRefCAS(headName, commitHash, p.HeadHash); err != nil {
			return "", fmt.Errorf("update ref %q: %w", headName, err)
		}
	} else {
		if err := tx.AddRef("HEAD", p.HeadHash, commitHash); err != nil {
			return "", fmt.Errorf("record transaction detached HEAD: %w", err)
		}
		if err := tx.Prepare(); err != nil {
			return "", fmt.Errorf("prepare transaction: %w", err)
		}
		if err := r.UpdateRefCAS("HEAD", commitHash, p.HeadHash); err != nil {
			return "", fmt.Errorf("update detached HEAD: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	r.invalidateStatusCache()
	r.GitShadowSyncSnapshot(p.Message, p.Author)
	r.recordGitShadowCommit(commitHash)
	return commitHash, nil
}
