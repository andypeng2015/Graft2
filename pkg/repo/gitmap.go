package repo

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

const (
	GitShadowStateNoShadow       = "no_shadow"
	GitShadowStateClean          = "clean"
	GitShadowStateFailureLog     = "failed_operation_log_present"
	GitShadowStateUnknownMapping = "unknown_mapping"
	GitShadowStateDivergedCommit = "diverged_commit"
	GitShadowStateDivergedTree   = "diverged_tree"
	GitShadowStateUnreadable     = "unreadable"
	GitShadowStateNoCommitsYet   = "no_commits_yet"
)

type GitShadowStatus struct {
	State             string
	HasGitDir         bool
	HasFailures       bool
	GraftHead         object.Hash
	ExpectedGitCommit string
	ExpectedGitTree   string
	ActualGitCommit   string
	ActualGitTree     string
	Message           string
	Err               error
}

func (s GitShadowStatus) NeedsAttention() bool {
	switch s.State {
	case GitShadowStateFailureLog, GitShadowStateUnknownMapping, GitShadowStateDivergedCommit, GitShadowStateDivergedTree, GitShadowStateUnreadable:
		return true
	default:
		return false
	}
}

// gitMapFile is the on-disk graft-commit -> git-commit(+tree) correspondence,
// recorded as graft shadows commits to git. Each line is
// "<graftHash> <gitCommitHex> <gitTreeHex>". It lives at .graft/gitmap (a
// repo-owned file: pkg/gitbridge imports pkg/repo, so repo cannot reuse
// gitbridge.HashMap without an import cycle).
const gitMapFile = "gitmap"

// gitShadowCapture runs git capturing stdout (unlike gitShadow, which discards
// it), logging failures to the shadow-failures log.
func (r *Repo) gitShadowCapture(label string, args ...string) (string, error) {
	return r.gitShadowCaptureWithLogging(label, true, args...)
}

func (r *Repo) gitShadowCaptureReadOnly(label string, args ...string) (string, error) {
	return r.gitShadowCaptureWithLogging(label, false, args...)
}

func (r *Repo) gitShadowCaptureWithLogging(label string, logFailure bool, args ...string) (string, error) {
	var out bytes.Buffer
	spec := ExternalProcessSpec{
		Dir:    r.RootDir,
		Path:   gitPath(),
		Args:   args,
		Stdout: &out,
		Stderr: io.Discard,
		Label:  label,
	}
	if err := RunExternalProcess(spec); err != nil {
		if logFailure {
			r.logShadowFailure(label, args, err)
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// recordGitShadowCommit captures the git commit + tree SHA produced by the most
// recent shadow snapshot and records the graft->git correspondence in
// .graft/gitmap. It is a no-op when no .git shadow is present and best-effort
// otherwise: a failure is logged but never blocks the graft commit (git is the
// mirror; graft is authoritative).
func (r *Repo) RecordGitShadowCommit(graftCommit object.Hash, operation string) {
	r.recordGitShadowCommit(graftCommit, operation)
}

func (r *Repo) recordGitShadowCommit(graftCommit object.Hash, operation ...string) {
	if !r.HasGitDir() {
		return
	}
	gitCommit, err := r.gitShadowCapture("git-shadow:map-commit", "rev-parse", "HEAD")
	if err != nil || gitCommit == "" {
		return
	}
	gitTree, err := r.gitShadowCapture("git-shadow:map-tree", "rev-parse", "HEAD^{tree}")
	if err != nil || gitTree == "" {
		return
	}
	op := "commit"
	if len(operation) > 0 && strings.TrimSpace(operation[0]) != "" {
		op = strings.ReplaceAll(strings.TrimSpace(operation[0]), " ", "_")
	}
	path := filepath.Join(r.GraftDir, gitMapFile)
	line := fmt.Sprintf("%s %s %s %d %s\n", string(graftCommit), gitCommit, gitTree, time.Now().Unix(), op)
	if err := appendFileAtomic(path, []byte(line), 0o644); err != nil {
		r.logShadowFailure("git-shadow:map-write", nil, err)
	}
}

// GitCommitFor returns the git commit + tree hashes graft shadowed for the given
// graft commit, reading the most recent mapping from .graft/gitmap. ok is false
// if no mapping exists (e.g. the commit predates dual-track or has no shadow).
func (r *Repo) GitCommitFor(graftCommit object.Hash) (gitCommit, gitTree string, ok bool) {
	data, err := os.ReadFile(filepath.Join(r.GraftDir, gitMapFile))
	if err != nil {
		return "", "", false
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 3 && len(parts) != 5 {
			continue
		}
		if object.Hash(parts[0]) == graftCommit {
			// Keep scanning so the most recent (last) mapping wins.
			gitCommit, gitTree, ok = parts[1], parts[2], true
		}
	}
	return gitCommit, gitTree, ok
}

// GitShadowDiverged reports whether the git shadow has drifted from what graft
// recorded for its current HEAD: it compares git's actual HEAD commit + tree
// against the mapping in .graft/gitmap. This is the succeed-but-diverge tripwire
// the exit-code-only shadow-failure check cannot see (git can succeed yet commit
// a different tree, or be moved out of band).
//
// It returns false ("not diverged") when there is no git shadow, graft has no
// HEAD commit yet, or there is no mapping for graft's HEAD commit (e.g. commits
// predating the map): "unknown" is deliberately not treated as "diverged".
func (r *Repo) GitShadowDiverged() (bool, error) {
	status, err := r.GitShadowStatus()
	if err != nil {
		return false, err
	}
	return status.State == GitShadowStateDivergedCommit || status.State == GitShadowStateDivergedTree, nil
}

func (r *Repo) GitShadowStatus() (GitShadowStatus, error) {
	if !r.HasGitDir() {
		return GitShadowStatus{
			State:     GitShadowStateNoShadow,
			HasGitDir: false,
			Message:   "no .git shadow repository is present",
		}, nil
	}
	status := GitShadowStatus{
		State:       GitShadowStateClean,
		HasGitDir:   true,
		HasFailures: r.HasShadowFailures(),
	}
	if status.HasFailures {
		status.State = GitShadowStateFailureLog
		status.Message = "git shadow failure log is present"
		return status, nil
	}

	graftHead, err := r.ResolveRef("HEAD")
	if err != nil || graftHead == "" {
		status.State = GitShadowStateNoCommitsYet
		status.Message = "graft has no HEAD commit yet"
		return status, nil
	}
	status.GraftHead = graftHead

	expectedCommit, expectedTree, ok := r.GitCommitFor(graftHead)
	if !ok {
		status.State = GitShadowStateUnknownMapping
		status.Message = "no git shadow mapping is recorded for graft HEAD"
		return status, nil
	}
	status.ExpectedGitCommit = expectedCommit
	status.ExpectedGitTree = expectedTree

	actualCommit, err := r.gitShadowCaptureReadOnly("git-shadow:status-commit", "rev-parse", "HEAD")
	if err != nil {
		status.State = GitShadowStateUnreadable
		status.Message = "could not read git shadow HEAD"
		status.Err = err
		return status, err
	}
	actualTree, err := r.gitShadowCaptureReadOnly("git-shadow:status-tree", "rev-parse", "HEAD^{tree}")
	if err != nil {
		status.State = GitShadowStateUnreadable
		status.ActualGitCommit = actualCommit
		status.Message = "could not read git shadow tree"
		status.Err = err
		return status, err
	}
	status.ActualGitCommit = actualCommit
	status.ActualGitTree = actualTree

	switch {
	case actualCommit != expectedCommit:
		status.State = GitShadowStateDivergedCommit
		status.Message = "git shadow HEAD differs from the recorded mapping for graft HEAD"
	case actualTree != expectedTree:
		status.State = GitShadowStateDivergedTree
		status.Message = "git shadow tree differs from the recorded mapping for graft HEAD"
	default:
		status.State = GitShadowStateClean
		status.Message = "git shadow matches graft HEAD"
	}
	return status, nil
}
