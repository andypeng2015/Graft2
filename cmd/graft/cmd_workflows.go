package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type workflowGuide struct {
	Topic string
	Title string
	Steps []string
}

var workflowGuides = []workflowGuide{
	{
		Topic: "solo",
		Title: "Solo Repository",
		Steps: []string{
			"graft init myrepo",
			"graft status",
			"graft add .",
			"graft commit -m \"describe the change\"",
			"graft diff --entity",
			"graft log",
			"Use graft doctor when status or verify reports repository damage.",
		},
	},
	{
		Topic: "github",
		Title: "Git Forge Backed Repository",
		Steps: []string{
			"graft clone https://github.com/OWNER/REPO.git",
			"graft status",
			"graft add <paths>",
			"graft commit -m \"describe the change\"",
			"graft push",
			"Use normal git status, git log, git diff, gh, and CI against the Git shadow repository.",
			"If Graft reports shadow divergence, run graft repair check-git-shadow before repairing.",
		},
	},
	{
		Topic: "coordination",
		Title: "Team Or Agent Coordination",
		Steps: []string{
			"graft workon --as <name>",
			"graft coord check",
			"graft coord task list",
			"graft coord plan list",
			"graft coord",
			"graft coord cleanup-stale --dry-run",
			"Run graft coord check before mutating files that other people or agents may be editing.",
			"If your local identity is stale or missing, run graft workon --recover --as <name>.",
			"Use graft coord cleanup-stale --stale-after <duration> to remove stale shared agent entries after reviewing the dry run.",
			"Finish the session with graft workon --done --as <name>.",
		},
	},
	{
		Topic: "recover-coordination",
		Title: "Recover Coordination Identity",
		Steps: []string{
			"graft coord sessions",
			"graft coord check",
			"graft workon --recover --as <name>",
			"graft coord cleanup-stale --dry-run",
			"graft coord cleanup-stale",
			"graft coord check",
			"Use this when a previous local session is stale or its agent ref is missing.",
			"Use graft workon --done --as <name> only when the live session should be intentionally closed.",
		},
	},
	{
		Topic: "recover-shadow",
		Title: "Recover Git Shadow Divergence",
		Steps: []string{
			"graft status",
			"graft doctor --json",
			"graft repair check-git-shadow",
			"graft repair resync-git",
			"graft repair clear-shadow-failures",
			"Graft history is authoritative; these commands inspect or rebuild the Git mirror.",
		},
	},
	{
		Topic: "recover-interrupt",
		Title: "Recover Interrupted Operations",
		Steps: []string{
			"graft doctor",
			"graft verify --json",
			"graft repair transaction <id>",
			"graft repair clear-stale-locks",
			"graft doctor --bundle",
			"Do not delete .graft/txn or .graft/locks by hand unless a repair command says to.",
		},
	},
}

func newWorkflowsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:       "workflows [topic]",
		Short:     "Show common Graft workflow guides",
		Long:      strings.TrimSpace(allWorkflowGuideText()),
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: workflowTopicNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Fprint(cmd.OutOrStdout(), allWorkflowGuideText())
				return nil
			}
			text, ok := workflowGuideText(args[0])
			if !ok {
				return usageError(cmd, fmt.Errorf("unknown workflow topic %q", args[0]))
			}
			fmt.Fprint(cmd.OutOrStdout(), text)
			return nil
		},
	}
	return cmd
}

func workflowTopicNames() []string {
	topics := make([]string, 0, len(workflowGuides))
	for _, guide := range workflowGuides {
		topics = append(topics, guide.Topic)
	}
	return topics
}

func workflowGuideText(topic string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(topic))
	for _, guide := range workflowGuides {
		if guide.Topic == normalized {
			return renderWorkflowGuide(guide), true
		}
	}
	return "", false
}

func allWorkflowGuideText() string {
	var b strings.Builder
	b.WriteString("Common Graft workflows\n\n")
	b.WriteString("Topics:\n")
	for _, guide := range workflowGuides {
		fmt.Fprintf(&b, "  %-17s %s\n", guide.Topic, guide.Title)
	}
	b.WriteString("\nRun graft workflows <topic> for one workflow.\n")
	b.WriteString("Run graft help workflows to show this guide through the standard help system.\n\n")
	for i, guide := range workflowGuides {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(renderWorkflowGuide(guide))
	}
	return b.String()
}

func renderWorkflowGuide(guide workflowGuide) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", guide.Title)
	b.WriteString(strings.Repeat("-", len(guide.Title)))
	b.WriteString("\n")
	for _, step := range guide.Steps {
		fmt.Fprintf(&b, "- %s\n", step)
	}
	return b.String()
}
