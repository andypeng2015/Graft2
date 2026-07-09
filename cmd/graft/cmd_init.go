package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/odvcencio/graft/pkg/gitbridge"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var noGit bool
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Create an empty graft repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			abs, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			// Ensure the target directory exists.
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return fmt.Errorf("create directory: %w", err)
			}

			// Existing .git/ → bridge init as today (no behavior change)
			if gitbridge.DetectGitRepo(abs) {
				bridge, err := gitbridge.InitBridge(abs)
				if err != nil {
					return fmt.Errorf("init git bridge: %w", err)
				}
				bridge.Close()
				excludeFromGitInfoExclude(abs, ".gts/")
				fmt.Fprintln(cmd.OutOrStdout(), "initialized graft bridge alongside existing git repository")
				printInitNextSteps(cmd.OutOrStdout(), abs)
				return nil
			}

			// Fresh directory: create .graft/ first
			r, err := repo.Init(abs)
			if err != nil {
				return err
			}

			// Then optionally create .git/ and bridge. Git is the mirror;
			// graft is authoritative — so a shadow failure warns but does not
			// abort. It must NOT be swallowed: a deferred failure would surface
			// confusingly at a later push.
			if !noGit {
				if err := initGitShadow(abs); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"warning: git shadow not initialized (graft is authoritative; git is the mirror): %v\n", err)
				} else if err := r.SetRepositoryFeature("git_shadow", true); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"warning: git shadow feature flag not recorded in .graft/config: %v\n", err)
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "initialized graft repository in %s\n",
				filepath.Join(r.RootDir, ".graft")+string(filepath.Separator))
			printInitNextSteps(cmd.OutOrStdout(), abs)
			return nil
		},
	}
	cmd.Flags().BoolVar(&noGit, "no-git", false, "skip creating .git/ directory")
	return cmd
}

// initGitShadow creates the .git mirror for a fresh graft repo and registers
// the bridge. It returns an error instead of swallowing one, so init can warn
// at the point of failure rather than deferring a confusing failure to a later
// git operation. Bridge registration is best-effort (matching prior behavior);
// the hard requirement is that "git init" produced a usable .git repository.
func initGitShadow(abs string) error {
	if err := repo.RunExternalProcess(repo.ExternalProcessSpec{
		Dir: abs, Path: "git", Args: []string{"init", "-b", "main"},
		Stdout: io.Discard, Stderr: io.Discard, Label: "init-git",
	}); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	if !gitbridge.DetectGitRepo(abs) {
		return fmt.Errorf("git init did not create a .git repository in %s", abs)
	}
	if bridge, _ := gitbridge.InitBridge(abs); bridge != nil {
		bridge.Close()
	}
	excludeFromGitInfoExclude(abs, ".gts/")
	return nil
}

func excludeFromGitInfoExclude(repoRoot, pattern string) {
	excludePath := filepath.Join(repoRoot, ".git", "info", "exclude")
	data, _ := os.ReadFile(excludePath)
	if strings.Contains(string(data), pattern) {
		return
	}
	os.MkdirAll(filepath.Dir(excludePath), 0o755)
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(pattern + "\n")
}

func printInitNextSteps(out io.Writer, repoPath string) {
	fmt.Fprintln(out, "next steps:")
	if displayPath := initDisplayPath(repoPath); displayPath != "" {
		fmt.Fprintf(out, "  cd %s\n", shellQuoteInitArg(displayPath))
	}
	fmt.Fprintln(out, "  graft status")
	fmt.Fprintln(out, "  graft add <files>")
	fmt.Fprintln(out, "  graft commit -m \"initial commit\"")
}

func initDisplayPath(repoPath string) string {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return repoPath
	}
	cwd, err := os.Getwd()
	if err != nil {
		return abs
	}
	if samePath(abs, cwd) {
		return ""
	}
	if rel, err := filepath.Rel(cwd, abs); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return filepath.ToSlash(rel)
	}
	return abs
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA == nil {
		a = aa
	}
	if errB == nil {
		b = bb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func shellQuoteInitArg(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\n\"'\\$`") {
		return strconv.Quote(s)
	}
	return s
}
