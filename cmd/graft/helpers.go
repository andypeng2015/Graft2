package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/graft/pkg/gitbridge"
	"m31labs.dev/graft/pkg/object"
	"m31labs.dev/graft/pkg/repo"
)

// shortHash delegates to repo.ShortHash for display purposes.
func shortHash(h object.Hash) string {
	return repo.ShortHash(h)
}

// branchName returns the current branch name (without refs/heads/ prefix),
// or "HEAD" if the repo is in detached HEAD state.
func branchName(r *repo.Repo) string {
	head, err := r.Head()
	if err == nil && strings.HasPrefix(head, "refs/heads/") {
		return strings.TrimPrefix(head, "refs/heads/")
	}
	return "HEAD"
}

// openRepo opens the graft repo at path. If no .graft/ is found walking up from
// path but a .git/ exists somewhere up the tree, it auto-imports from git and
// opens the resulting bridge. Agents running graft commands in plain git repos
// get a one-line notice on stderr instead of a hard error.
func openRepo(path string) (*repo.Repo, error) {
	r, err := repo.Open(path)
	if err == nil {
		return r, nil
	}
	if !isNotAGraftRepoErr(err) {
		return nil, err
	}
	gitRoot := findGitRoot(path)
	if gitRoot == "" {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, ".graft not found — initializing from git at %s\n", gitRoot)
	bridge, initErr := gitbridge.InitBridge(gitRoot)
	if initErr != nil {
		return nil, fmt.Errorf("auto-init from git: %w", initErr)
	}
	_ = bridge.Close()
	return repo.Open(path)
}

// isNotAGraftRepoErr reports whether err indicates repo.Open failed because no
// .graft/ directory was found. Matches the error text in repo.Open.
func isNotAGraftRepoErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not a graft repository")
}

// findGitRoot walks up from path looking for a directory containing .git/.
// Returns the absolute path of the git root, or "" if none is found.
func findGitRoot(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	cur := abs
	for {
		if gitbridge.DetectGitRepo(cur) {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}
