package repo

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// gitMapFile is the on-disk graft-commit -> git-commit(+tree) correspondence,
// recorded as graft shadows commits to git. Each line is
// "<graftHash> <gitCommitHex> <gitTreeHex>". It lives at .graft/gitmap (a
// repo-owned file: pkg/gitbridge imports pkg/repo, so repo cannot reuse
// gitbridge.HashMap without an import cycle).
const gitMapFile = "gitmap"

// gitShadowCapture runs git capturing stdout (unlike gitShadow, which discards
// it), logging failures to the shadow-failures log.
func (r *Repo) gitShadowCapture(label string, args ...string) (string, error) {
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
		r.logShadowFailure(label, args, err)
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// recordGitShadowCommit captures the git commit + tree SHA produced by the most
// recent shadow snapshot and records the graft->git correspondence in
// .graft/gitmap. It is a no-op when no .git shadow is present and best-effort
// otherwise: a failure is logged but never blocks the graft commit (git is the
// mirror; graft is authoritative).
func (r *Repo) recordGitShadowCommit(graftCommit object.Hash) {
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
	path := filepath.Join(r.GraftDir, gitMapFile)
	f, ferr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr != nil {
		r.logShadowFailure("git-shadow:map-write", nil, ferr)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s %s\n", string(graftCommit), gitCommit, gitTree)
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
		if len(parts) != 3 {
			continue
		}
		if object.Hash(parts[0]) == graftCommit {
			// Keep scanning so the most recent (last) mapping wins.
			gitCommit, gitTree, ok = parts[1], parts[2], true
		}
	}
	return gitCommit, gitTree, ok
}
