package main

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/gitbridge"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newRepairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair or rebuild local graft metadata",
	}
	cmd.AddCommand(newRepairReseedCmd())
	cmd.AddCommand(newRepairResyncGitCmd())
	cmd.AddCommand(newRepairCheckGitShadowCmd())
	cmd.AddCommand(newRepairClearShadowFailuresCmd())
	cmd.AddCommand(newRepairClearStaleLocksCmd())
	cmd.AddCommand(newRepairTransactionCmd())
	cmd.AddCommand(newRepairMigrateConfigCmd())
	return cmd
}

func newRepairReseedCmd() *cobra.Command {
	var yes bool
	var gitRef string

	cmd := &cobra.Command{
		Use:   "reseed",
		Short: "Replace local .graft state with a fresh snapshot from Git",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("repair reseed replaces the local .graft directory; re-run with --yes")
			}

			rootDir, err := gitTopLevel(cmd.Context(), ".")
			if err != nil {
				return err
			}

			existingGraftDir := filepath.Join(rootDir, ".graft")
			preservedConfig, err := readReseedConfig(existingGraftDir)
			if err != nil {
				return err
			}

			tempDir, err := os.MkdirTemp("", "graft-reseed-*")
			if err != nil {
				return fmt.Errorf("create reseed temp dir: %w", err)
			}
			defer os.RemoveAll(tempDir)

			if err := extractGitArchive(cmd.Context(), rootDir, gitRef, tempDir); err != nil {
				return err
			}

			ignoreFiles, err := stashRootIgnoreFiles(tempDir)
			if err != nil {
				return err
			}

			tmpRepo, err := repo.Init(tempDir)
			if err != nil {
				return err
			}
			if preservedConfig != nil {
				if err := tmpRepo.WriteConfig(preservedConfig); err != nil {
					return err
				}
			}

			if branch := gitBranchForReseed(cmd.Context(), rootDir, gitRef); branch != "" {
				if err := tmpRepo.SetHeadSymbolic("refs/heads/" + branch); err != nil {
					return err
				}
			}

			if hasNonGraftFiles(tempDir) {
				if err := tmpRepo.Add([]string{"."}); err != nil {
					return err
				}
			}

			if err := restoreRootIgnoreFiles(ignoreFiles); err != nil {
				return err
			}
			if len(ignoreFiles) > 0 {
				paths := make([]string, 0, len(ignoreFiles))
				for _, file := range ignoreFiles {
					paths = append(paths, file.RelPath)
				}
				if err := tmpRepo.Add(paths); err != nil {
					return err
				}
			}

			if gitbridge.DetectGitRepo(rootDir) {
				if err := ensureBridgeScaffold(tmpRepo); err != nil {
					return err
				}
				if err := writeBridgeHashMap(tmpRepo); err != nil {
					return err
				}
			}

			var commitHash string
			staging, err := tmpRepo.ReadStaging()
			if err != nil {
				return err
			}
			if len(staging.Entries) > 0 {
				author := gitCommitAuthor(cmd.Context(), rootDir, gitRef)
				if strings.TrimSpace(author) == "" {
					author = tmpRepo.ResolveAuthor()
				}
				msg := reseedCommitMessage(cmd.Context(), rootDir, gitRef)
				h, err := tmpRepo.Commit(msg, author)
				if err != nil {
					return err
				}
				commitHash = string(h)
			}

			backupPath := ""
			if _, err := os.Stat(existingGraftDir); err == nil {
				backupPath = rootDir + ".graft-backup-" + time.Now().Format("20060102-150405")
				if err := os.Rename(existingGraftDir, backupPath); err != nil {
					return fmt.Errorf("backup existing .graft: %w", err)
				}
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("stat existing .graft: %w", err)
			}

			newGraftDir := filepath.Join(tempDir, ".graft")
			if err := os.Rename(newGraftDir, existingGraftDir); err != nil {
				if backupPath != "" {
					_ = os.Rename(backupPath, existingGraftDir)
				}
				return fmt.Errorf("install reseeded .graft: %w", err)
			}

			if gitbridge.DetectGitRepo(rootDir) {
				if err := ensureGraftExcludedFromGit(rootDir); err != nil {
					return err
				}
			}

			if commitHash != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "reseeded .graft from git %s at %s\n", gitRef, shortHashString(commitHash))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "reseeded empty .graft from git %s\n", gitRef)
			}
			if backupPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "backup: %s\n", backupPath)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "replace the current .graft directory without prompting")
	cmd.Flags().StringVar(&gitRef, "git-ref", "HEAD", "git ref to snapshot into the new graft store")
	return cmd
}

func newRepairResyncGitCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "resync-git",
		Short: "Force-sync git to match graft's current state",
		Long:  "Brings the colocated .git/ repository in sync with graft by staging all files and creating a git commit matching graft's HEAD.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}
			if !r.HasGitDir() {
				return fmt.Errorf("no .git/ directory found; nothing to resync")
			}
			return r.WithRepositoryLock("repair-resync-git", func() error {
				head, err := r.ResolveRef("HEAD")
				if err != nil {
					return fmt.Errorf("resolve graft HEAD: %w", err)
				}

				r.ClearShadowFailures()

				branch, _ := r.CurrentBranch()
				if branch != "" {
					r.GitShadowCheckout(branch)
				}

				author := r.ResolveAuthor()
				short := string(head)
				if len(short) > 12 {
					short = short[:12]
				}
				msg := fmt.Sprintf("graft resync: match graft HEAD %s", short)

				r.GitShadowSyncSnapshot(msg, author)
				if r.HasShadowFailures() {
					status, _ := r.GitShadowStatus()
					if jsonFlag {
						return writeJSON(cmd.OutOrStdout(), repairGitShadowStatusToJSON(status))
					}
					return fmt.Errorf("git resync failed; run graft repair check-git-shadow")
				}
				r.RecordGitShadowCommit(head, "resync-git")
				status, err := r.GitShadowStatus()
				if err != nil {
					if jsonFlag {
						return writeJSON(cmd.OutOrStdout(), repairGitShadowStatusToJSON(status))
					}
					return err
				}

				out := cmd.OutOrStdout()
				if jsonFlag {
					return writeJSON(cmd.OutOrStdout(), repairGitShadowStatusToJSON(status))
				}
				if branch != "" {
					fmt.Fprintf(out, "git synced to graft HEAD %s on branch %s\n", short, branch)
				} else {
					fmt.Fprintf(out, "git synced to graft HEAD %s\n", short)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	return cmd
}

func newRepairCheckGitShadowCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "check-git-shadow",
		Short: "Check whether git shadow matches graft state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}
			status, err := r.GitShadowStatus()
			if jsonFlag {
				if writeErr := writeJSON(cmd.OutOrStdout(), repairGitShadowStatusToJSON(status)); writeErr != nil {
					return writeErr
				}
				if err != nil {
					return err
				}
				if status.NeedsAttention() {
					return fmt.Errorf("git shadow needs attention: %s", status.State)
				}
				return nil
			}
			printGitShadowStatus(cmd.OutOrStdout(), status)
			if err != nil {
				return err
			}
			if status.NeedsAttention() {
				return fmt.Errorf("git shadow needs attention: %s", status.State)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	return cmd
}

func newRepairClearShadowFailuresCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "clear-shadow-failures",
		Short: "Clear the git shadow failure log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}
			hadFailures := r.HasShadowFailures()
			r.ClearShadowFailures()
			status, statusErr := r.GitShadowStatus()
			if jsonFlag {
				out := repairGitShadowStatusToJSON(status)
				out.OK = statusErr == nil && !status.NeedsAttention()
				if hadFailures && status.State == repo.GitShadowStateNoShadow {
					out.Message = "git shadow failure log cleared"
				}
				if err := writeJSON(cmd.OutOrStdout(), out); err != nil {
					return err
				}
				return statusErr
			}
			if hadFailures {
				fmt.Fprintln(cmd.OutOrStdout(), "git shadow failure log cleared")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "no git shadow failure log present")
			}
			return statusErr
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	return cmd
}

func newRepairClearStaleLocksCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "clear-stale-locks",
		Short: "Clear stale graft operation locks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}
			status, cleared, err := r.ClearStaleRepositoryLock()
			out := repairLockStatusToJSON(status, cleared)
			if err != nil {
				out.OK = false
				out.Message = err.Error()
				if jsonFlag {
					if writeErr := writeJSON(cmd.OutOrStdout(), out); writeErr != nil {
						return writeErr
					}
					return err
				}
				return err
			}
			if jsonFlag {
				if writeErr := writeJSON(cmd.OutOrStdout(), out); writeErr != nil {
					return writeErr
				}
				if status.Exists && !status.Stale {
					return fmt.Errorf("repository lock is active: %s", status.Path)
				}
				return nil
			}
			switch {
			case cleared:
				fmt.Fprintf(cmd.OutOrStdout(), "cleared stale repository lock: %s\n", status.Path)
			case status.Exists && !status.Stale:
				printRepositoryLockStatus(cmd.OutOrStdout(), status)
				return fmt.Errorf("repository lock is active; not clearing")
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "no stale repository lock present")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	return cmd
}

func newRepairTransactionCmd() *cobra.Command {
	var jsonFlag bool
	var markRolledBack bool
	var markCommitted bool
	var reason string
	cmd := &cobra.Command{
		Use:   "transaction <id>",
		Short: "Inspect or mark a graft transaction record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if markRolledBack && markCommitted {
				return fmt.Errorf("choose only one of --mark-rolled-back or --mark-committed")
			}
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}

			var rec *repo.TransactionRecord
			switch {
			case markRolledBack:
				rec, err = r.MarkTransactionRolledBack(args[0], reason)
			case markCommitted:
				rec, err = r.MarkTransactionCommitted(args[0], reason)
			default:
				rec, err = r.ReadTransaction(args[0])
			}
			if err != nil {
				return err
			}

			out := repairTransactionToJSON(rec)
			if markRolledBack || markCommitted {
				out.Message = "transaction status updated"
			}
			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), out)
			}
			printTransactionRecord(cmd.OutOrStdout(), rec)
			if out.Repair != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "repair: %s\n", out.Repair)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().BoolVar(&markRolledBack, "mark-rolled-back", false, "mark the transaction rolled back after manual verification")
	cmd.Flags().BoolVar(&markCommitted, "mark-committed", false, "mark the transaction committed after manual verification")
	cmd.Flags().StringVar(&reason, "reason", "", "audit note to write into the transaction record")
	return cmd
}

func newRepairMigrateConfigCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "migrate-config",
		Short: "Create missing repository format metadata for a legacy repo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}
			result, err := r.MigrateRepositoryConfig()
			if err != nil {
				return err
			}
			out := repairMigrateConfigToJSON(result)
			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), out)
			}
			if result.Migrated {
				fmt.Fprintf(cmd.OutOrStdout(), "created repository config: %s\n", result.Path)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "repository config already at format %d: %s\n", result.ToVersion, result.Path)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	return cmd
}

func repairGitShadowStatusToJSON(status repo.GitShadowStatus) JSONRepairGitShadowOutput {
	out := JSONRepairGitShadowOutput{
		OK:                !status.NeedsAttention(),
		State:             status.State,
		Message:           status.Message,
		HasGitDir:         status.HasGitDir,
		HasFailures:       status.HasFailures,
		GraftHead:         string(status.GraftHead),
		ExpectedGitCommit: status.ExpectedGitCommit,
		ExpectedGitTree:   status.ExpectedGitTree,
		ActualGitCommit:   status.ActualGitCommit,
		ActualGitTree:     status.ActualGitTree,
	}
	out.Repair = gitShadowRepairCommand(status)
	return out
}

func repairLockStatusToJSON(status repo.RepositoryLockStatus, cleared bool) JSONRepairLockOutput {
	out := JSONRepairLockOutput{
		OK:        true,
		State:     "no_lock",
		Path:      status.Path,
		Operation: status.Info.Operation,
		PID:       status.Info.PID,
		Hostname:  status.Info.Hostname,
		Command:   status.Info.Command,
		StartedAt: status.Info.StartedAt,
		Stale:     status.Stale,
		Cleared:   cleared,
	}
	switch {
	case cleared:
		out.State = "cleared"
		out.Message = "stale repository lock cleared"
	case status.Exists && status.Stale:
		out.State = "stale"
		out.Message = status.Reason
		out.OK = false
		out.Repair = "graft repair clear-stale-locks"
	case status.Exists:
		out.State = "active"
		out.Message = "repository lock is active"
		out.OK = false
	default:
		out.Message = "no repository lock present"
	}
	return out
}

func repairTransactionToJSON(rec *repo.TransactionRecord) JSONRepairTransactionOutput {
	if rec == nil {
		return JSONRepairTransactionOutput{OK: false, Message: "transaction not found"}
	}
	out := JSONRepairTransactionOutput{
		OK:           !transactionStatusNeedsRepair(rec.Status),
		ID:           rec.ID,
		Operation:    rec.Operation,
		Status:       rec.Status,
		StartedAt:    rec.StartedAt,
		UpdatedAt:    rec.UpdatedAt,
		Error:        rec.Error,
		TouchedFiles: append([]string(nil), rec.TouchedFiles...),
	}
	for _, ref := range rec.TouchedRefs {
		out.TouchedRefs = append(out.TouchedRefs, JSONTransactionRefMutation{
			Ref:     ref.Ref,
			OldHash: string(ref.OldHash),
			NewHash: string(ref.NewHash),
		})
	}
	if !out.OK {
		out.Repair = "graft repair transaction " + rec.ID + " --mark-rolled-back --reason <note>"
	}
	return out
}

func repairMigrateConfigToJSON(result *repo.RepositoryConfigMigrationResult) JSONRepairMigrateConfigOutput {
	if result == nil {
		return JSONRepairMigrateConfigOutput{OK: false, Message: "migration result unavailable"}
	}
	out := JSONRepairMigrateConfigOutput{
		OK:          true,
		Migrated:    result.Migrated,
		Path:        result.Path,
		FromVersion: result.FromVersion,
		ToVersion:   result.ToVersion,
	}
	if result.Migrated {
		out.Message = "repository config created"
	} else {
		out.Message = "repository config already current"
	}
	return out
}

func transactionStatusNeedsRepair(status string) bool {
	switch status {
	case repo.TransactionStatusStarted, repo.TransactionStatusPrepared, repo.TransactionStatusNeedsRepair:
		return true
	default:
		return false
	}
}

func printTransactionRecord(out io.Writer, rec *repo.TransactionRecord) {
	if rec == nil {
		fmt.Fprintln(out, "transaction: not found")
		return
	}
	fmt.Fprintf(out, "transaction: %s\n", rec.ID)
	fmt.Fprintf(out, "operation: %s\n", rec.Operation)
	fmt.Fprintf(out, "status: %s\n", rec.Status)
	fmt.Fprintf(out, "started: %s\n", rec.StartedAt)
	fmt.Fprintf(out, "updated: %s\n", rec.UpdatedAt)
	if rec.Error != "" {
		fmt.Fprintf(out, "error: %s\n", rec.Error)
	}
	if len(rec.TouchedRefs) > 0 {
		fmt.Fprintln(out, "refs:")
		for _, ref := range rec.TouchedRefs {
			fmt.Fprintf(out, "  %s %s -> %s\n", ref.Ref, ref.OldHash, ref.NewHash)
		}
	}
	if len(rec.TouchedFiles) > 0 {
		fmt.Fprintln(out, "files:")
		for _, path := range rec.TouchedFiles {
			fmt.Fprintf(out, "  %s\n", path)
		}
	}
}

func printRepositoryLockStatus(out io.Writer, status repo.RepositoryLockStatus) {
	fmt.Fprintf(out, "repository lock: %s\n", status.Path)
	fmt.Fprintf(out, "operation: %s\n", status.Info.Operation)
	fmt.Fprintf(out, "pid: %d\n", status.Info.PID)
	fmt.Fprintf(out, "hostname: %s\n", status.Info.Hostname)
	fmt.Fprintf(out, "started: %s\n", status.Info.StartedAt)
	if status.Info.Command != "" {
		fmt.Fprintf(out, "command: %s\n", status.Info.Command)
	}
}

func printGitShadowStatus(out io.Writer, status repo.GitShadowStatus) {
	fmt.Fprintf(out, "git shadow: %s\n", status.State)
	if status.Message != "" {
		fmt.Fprintf(out, "%s\n", status.Message)
	}
	if status.GraftHead != "" {
		fmt.Fprintf(out, "graft HEAD: %s\n", status.GraftHead)
	}
	if status.ExpectedGitCommit != "" {
		fmt.Fprintf(out, "expected git commit: %s\n", status.ExpectedGitCommit)
	}
	if status.ActualGitCommit != "" {
		fmt.Fprintf(out, "actual git commit: %s\n", status.ActualGitCommit)
	}
	if status.ExpectedGitTree != "" {
		fmt.Fprintf(out, "expected git tree: %s\n", status.ExpectedGitTree)
	}
	if status.ActualGitTree != "" {
		fmt.Fprintf(out, "actual git tree: %s\n", status.ActualGitTree)
	}
	if repair := gitShadowRepairCommand(status); repair != "" {
		fmt.Fprintf(out, "repair: %s\n", repair)
	}
}

type stashedIgnoreFile struct {
	RelPath string
	Path    string
	Data    []byte
	Mode    os.FileMode
}

func gitTopLevel(ctx context.Context, path string) (string, error) {
	output, err := runGitCaptureWithLabel(ctx, path, "git-repair:toplevel", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve git toplevel: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func extractGitArchive(ctx context.Context, rootDir, gitRef, destDir string) error {
	tarFile, err := os.CreateTemp("", "graft-reseed-archive-*.tar")
	if err != nil {
		return fmt.Errorf("create git archive temp file: %w", err)
	}
	tarPath := tarFile.Name()
	defer os.Remove(tarPath)
	defer tarFile.Close()

	if err := runGitStreamingWithLabel(ctx, rootDir, tarFile, io.Discard, "git-repair:archive", "archive", "--format=tar", gitRef); err != nil {
		return err
	}
	if _, err := tarFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind git archive temp file: %w", err)
	}
	return extractTar(tarFile, destDir)
}

func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read git archive: %w", err)
		}
		target := filepath.Join(destDir, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(filepath.Separator)) && filepath.Clean(target) != filepath.Clean(destDir) {
			return fmt.Errorf("archive entry %q escapes destination", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, hdr.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("extract dir %q: %w", hdr.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("extract dir for %q: %w", hdr.Name, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return fmt.Errorf("extract file %q: %w", hdr.Name, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("extract file %q: %w", hdr.Name, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close extracted file %q: %w", hdr.Name, err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("extract symlink dir %q: %w", hdr.Name, err)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("extract symlink %q: %w", hdr.Name, err)
			}
		}
	}
}

func stashRootIgnoreFiles(rootDir string) ([]stashedIgnoreFile, error) {
	names := []string{".graftignore", ".gotignore", ".gitignore"}
	out := make([]stashedIgnoreFile, 0, len(names))
	for _, name := range names {
		path := filepath.Join(rootDir, name)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", name, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		out = append(out, stashedIgnoreFile{
			RelPath: name,
			Path:    path,
			Data:    data,
			Mode:    info.Mode().Perm(),
		})
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove %s during reseed: %w", name, err)
		}
	}
	return out, nil
}

func restoreRootIgnoreFiles(files []stashedIgnoreFile) error {
	for _, file := range files {
		if err := os.WriteFile(file.Path, file.Data, file.Mode); err != nil {
			return fmt.Errorf("restore %s: %w", file.RelPath, err)
		}
	}
	return nil
}

func hasNonGraftFiles(rootDir string) bool {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == ".graft" {
			continue
		}
		return true
	}
	return false
}

func readReseedConfig(existingGraftDir string) (*repo.Config, error) {
	cfgPath := filepath.Join(existingGraftDir, "config.json")
	if _, err := os.Stat(cfgPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat graft config: %w", err)
	}

	tmpRepo := &repo.Repo{GraftDir: existingGraftDir}
	cfg, err := tmpRepo.ReadConfig()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func gitBranchForReseed(ctx context.Context, rootDir, gitRef string) string {
	if strings.TrimSpace(gitRef) == "" || gitRef == "HEAD" {
		output, err := runGitCaptureWithLabel(ctx, rootDir, "git-repair:branch", "symbolic-ref", "--quiet", "--short", "HEAD")
		if err == nil {
			return strings.TrimSpace(string(output))
		}
		return ""
	}
	if strings.HasPrefix(gitRef, "refs/heads/") {
		return strings.TrimPrefix(gitRef, "refs/heads/")
	}
	if err := runGitStreamingWithLabel(ctx, rootDir, io.Discard, io.Discard, "git-repair:show-ref", "show-ref", "--verify", "--quiet", "refs/heads/"+gitRef); err == nil {
		return gitRef
	}
	return ""
}

func gitCommitAuthor(ctx context.Context, rootDir, gitRef string) string {
	output, err := runGitCaptureWithLabel(ctx, rootDir, "git-repair:author", "log", "-1", "--format=%an <%ae>", gitRef)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func reseedCommitMessage(ctx context.Context, rootDir, gitRef string) string {
	output, err := runGitCaptureWithLabel(ctx, rootDir, "git-repair:rev-parse", "rev-parse", "--short=12", gitRef)
	short := strings.TrimSpace(string(output))
	if err != nil || short == "" {
		return fmt.Sprintf("snapshot: reseed graft state from git %s", gitRef)
	}
	return fmt.Sprintf("snapshot: reseed graft state from git %s (%s)", gitRef, short)
}

func ensureBridgeScaffold(r *repo.Repo) error {
	for _, rel := range []string{
		filepath.Join("refs", "tags"),
		"info",
	} {
		if err := os.MkdirAll(filepath.Join(r.GraftDir, rel), 0o755); err != nil {
			return fmt.Errorf("create .graft/%s: %w", rel, err)
		}
	}
	return nil
}

func writeBridgeHashMap(r *repo.Repo) error {
	staging, err := r.ReadStaging()
	if err != nil {
		return err
	}
	hm, err := gitbridge.OpenHashMap(filepath.Join(r.GraftDir, "hashmap"))
	if err != nil {
		return err
	}
	defer hm.Close()

	for path, entry := range staging.Entries {
		content, err := os.ReadFile(filepath.Join(r.RootDir, filepath.FromSlash(path)))
		if err != nil {
			return fmt.Errorf("read %s for hash map: %w", path, err)
		}
		if err := hm.Put(entry.BlobHash, gitbridge.GitObjectHash("blob", content)); err != nil {
			return fmt.Errorf("record hash map for %s: %w", path, err)
		}
	}
	return nil
}

func ensureGraftExcludedFromGit(rootDir string) error {
	infoDir := filepath.Join(rootDir, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("create .git/info: %w", err)
	}

	excludePath := filepath.Join(infoDir, "exclude")
	existing, _ := os.ReadFile(excludePath)
	if strings.Contains(string(existing), ".graft/") {
		return nil
	}

	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", excludePath, err)
	}
	defer f.Close()
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(".graft/\n")
	return err
}

func shortHashString(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}
