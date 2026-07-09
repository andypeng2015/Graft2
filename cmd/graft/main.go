// Package main implements the graft CLI, a structural version control system.
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

var version = "0.10.0"
var commit = "unknown"
var buildTime = "unknown"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		printCommandError(os.Stderr, err)
		os.Exit(commandExitCode(err))
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "graft",
		Short:         "Structural version control powered by tree-sitter",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError(cmd, err)
	})

	root.AddCommand(newVersionCmd())
	root.AddCommand(newProtocolCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newAddCmd())
	root.AddCommand(newResetCmd())
	root.AddCommand(newRmCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newCheckIgnoreCmd())
	root.AddCommand(newCommitCmd())
	root.AddCommand(newLogCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newBlameCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newBranchCmd())
	root.AddCommand(newTagCmd())
	root.AddCommand(newCheckoutCmd())
	root.AddCommand(newSwitchCmd())
	root.AddCommand(newMergeCmd())
	root.AddCommand(newConflictsCmd())
	root.AddCommand(newCherryPickCmd())
	root.AddCommand(newRevertCmd())
	root.AddCommand(newRemoteCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newAuthCmd())
	root.AddCommand(newPublishCmd())
	root.AddCommand(newCloneCmd())
	root.AddCommand(newFetchCmd())
	root.AddCommand(newPullCmd())
	root.AddCommand(newPushCmd())
	root.AddCommand(newReflogCmd())
	root.AddCommand(newGcCmd())
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newStashCmd())
	root.AddCommand(newRebaseCmd())
	root.AddCommand(newSparseCheckoutCmd())
	root.AddCommand(newLFSCmd())
	root.AddCommand(newBisectCmd())
	root.AddCommand(newWorktreeCmd())
	root.AddCommand(newCleanCmd())
	root.AddCommand(newGrepCmd())
	root.AddCommand(newContextCmd())
	root.AddCommand(newShortlogCmd())
	root.AddCommand(newWorkflowsCmd())
	root.AddCommand(newArchiveCmd())
	root.AddCommand(newReleaseCmd())
	root.AddCommand(newModuleCmd())
	root.AddCommand(newRepairCmd())
	root.AddCommand(newWorkonCmd())
	root.AddCommand(newCoordCmd())
	root.AddCommand(newCoorddCmd())
	root.AddCommand(newWorkspaceCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newManCmd(root))
	root.AddCommand(newCompletionCmd(root))

	return root
}

func newVersionCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			if jsonFlag {
				_ = writeJSON(cmd.OutOrStdout(), JSONVersionOutput{
					Version:                        version,
					Commit:                         commit,
					BuildTime:                      buildTime,
					GoVersion:                      runtime.Version(),
					SupportedRepositoryFormat:      repo.RepositoryFormatVersion,
					SupportedRemoteProtocolVersion: remote.ProtocolVersion,
				})
				return
			}
			fmt.Fprintln(cmd.OutOrStdout(), "graft "+version)
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	return cmd
}
