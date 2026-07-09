package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var completionShells = []string{"bash", "zsh", "fish", "powershell"}

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion scripts",
		Args:      cobra.ExactArgs(1),
		ValidArgs: completionShells,
		Long: strings.TrimSpace(`Generate a shell completion script for graft.

Write the output to the location expected by your shell, or source it directly
from your shell profile.`),
		Example: strings.TrimSpace(`graft completion bash > /etc/bash_completion.d/graft
graft completion zsh > "${fpath[1]}/_graft"
graft completion fish > ~/.config/fish/completions/graft.fish
graft completion powershell > graft.ps1`),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := root
			if target == nil {
				target = cmd.Root()
			}
			out := cmd.OutOrStdout()
			switch strings.ToLower(strings.TrimSpace(args[0])) {
			case "bash":
				return target.GenBashCompletionV2(out, true)
			case "zsh":
				return target.GenZshCompletion(out)
			case "fish":
				return target.GenFishCompletion(out, true)
			case "powershell":
				return target.GenPowerShellCompletionWithDesc(out)
			default:
				return usageError(cmd, fmt.Errorf("unsupported shell %q", args[0]))
			}
		},
	}
	return cmd
}
