package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

func newCoordCmd() *cobra.Command {
	var jsonFlag bool

	var allWorkspaces bool

	cmd := &cobra.Command{
		Use:   "coord",
		Short: "Multi-agent coordination dashboard and tools",
		Long:  `View and manage shared coordination state: agents, claims, plans, notes, tasks, feed, and impact analysis.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return coordDashboard(cmd, jsonFlag, allWorkspaces)
		},
	}

	cmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "JSON output")
	cmd.Flags().BoolVar(&allWorkspaces, "all", false, "aggregate dashboard across all registered workspaces")

	cmd.AddCommand(newCoordAgentsCmd(&jsonFlag))
	cmd.AddCommand(newCoordClaimsCmd(&jsonFlag))
	cmd.AddCommand(newCoordDecisionsCmd(&jsonFlag))
	cmd.AddCommand(newCoordFeedCmd(&jsonFlag))
	cmd.AddCommand(newCoordImpactCmd(&jsonFlag))
	cmd.AddCommand(newCoordCheckCmd(&jsonFlag))
	cmd.AddCommand(newCoordCleanupStaleCmd(&jsonFlag))
	cmd.AddCommand(newCoordDiffCmd(&jsonFlag))
	cmd.AddCommand(newCoordXrefsCmd(&jsonFlag))
	cmd.AddCommand(newCoordGraphCmd(&jsonFlag))
	cmd.AddCommand(newCoordWatchCmd(&jsonFlag))
	cmd.AddCommand(newCoordUnwatchCmd(&jsonFlag))
	cmd.AddCommand(newCoordResolveCmd(&jsonFlag))
	cmd.AddCommand(newCoordPlanCmd(&jsonFlag))
	cmd.AddCommand(newCoordNoteCmd(&jsonFlag))
	cmd.AddCommand(newCoordTaskCmd(&jsonFlag))
	cmd.AddCommand(newCoordPublishCmd(&jsonFlag))
	cmd.AddCommand(newCoordHeartbeatCmd(&jsonFlag))
	cmd.AddCommand(newCoordSessionsCmd(&jsonFlag))
	cmd.AddCommand(newCoordPresenceCmd(&jsonFlag))
	cmd.AddCommand(newCoordReadingCmd(&jsonFlag))

	return cmd
}

// openCoordinator opens the repo and creates a coordinator.
func openCoordinator() (*coord.Coordinator, *repo.Repo, error) {
	return openCoordinatorWithRepoOpener(openRepo)
}

func openCoordinatorForCommand(cmd *cobra.Command) (*coord.Coordinator, *repo.Repo, error) {
	return openCoordinatorWithRepoOpener(func(path string) (*repo.Repo, error) {
		return openRepoForCommand(cmd, path)
	})
}

func openCoordinatorWithRepoOpener(open func(string) (*repo.Repo, error)) (*coord.Coordinator, *repo.Repo, error) {
	r, err := open(".")
	if err != nil {
		return nil, nil, fmt.Errorf("open repo: %w", err)
	}
	c := coord.New(r, coord.DefaultConfig)
	return c, r, nil
}

// readActiveAgentID reads the current agent ID from .graft/coord/agent-id.
func readActiveAgentID(r *repo.Repo) string {
	if envID := strings.TrimSpace(os.Getenv("GRAFT_COORD_AGENT_ID")); envID != "" {
		return envID
	}
	data, err := os.ReadFile(filepath.Join(r.GraftDir, "coord", "agent-id"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// --- Dashboard ---

func coordDashboard(cmd *cobra.Command, jsonOutput bool, allWorkspaces bool) error {
	out := cmd.OutOrStdout()
	if allWorkspaces {
		return coordDashboardAll(out, jsonOutput)
	}

	c, r, err := openCoordinatorForCommand(cmd)
	if err != nil {
		return err
	}

	result := coordStatusSummary(c, r)

	if jsonOutput {
		return writeJSON(out, result)
	}

	fmt.Fprintln(out, "Coordination Dashboard")
	fmt.Fprintln(out, strings.Repeat("-", 40))
	fmt.Fprintf(out, "  Agents:     %d\n", result.Agents)
	fmt.Fprintf(out, "  Claims:     %d\n", result.Claims)
	fmt.Fprintf(out, "  Conflicts:  %d\n", result.Conflicts)
	fmt.Fprintf(out, "  Feed:       %d event(s)\n", result.FeedCount)
	fmt.Fprintf(out, "  Notes:      %d\n", result.Notes)
	fmt.Fprintf(out, "  Tasks:      %d (%d pending, %d active)\n", result.Tasks, result.TasksPending, result.TasksActive)
	if result.ActiveID != "" {
		fmt.Fprintf(out, "  Active as:  %s\n", result.ActiveID)
	} else {
		fmt.Fprintf(out, "  Active as:  (not joined)\n")
	}

	return nil
}

func coordDashboardAll(out io.Writer, jsonOutput bool) error {
	result := JSONCoordStatusOutput{}
	claimsByEntity := make(map[string][]string)

	if err := iterateWorkspaces(func(name string, c *coord.Coordinator) error {
		agents, _ := c.ListAgents()
		claims, _ := c.ListClaims()
		feedEvents, _ := c.WalkFeed("", 100)
		tasks, _ := c.ListTasks()
		notes, _ := c.ListNotes()

		result.Agents += len(agents)
		result.Claims += len(claims)
		result.FeedCount += len(feedEvents)
		result.Notes += len(notes)
		result.Tasks += len(tasks)

		for _, t := range tasks {
			switch t.Status {
			case "pending":
				result.TasksPending++
			case "in_progress":
				result.TasksActive++
			}
		}

		for _, cl := range claims {
			key := name + ":" + cl.EntityKeyHash
			claimsByEntity[key] = append(claimsByEntity[key], cl.AgentName)
		}
		return nil
	}); err != nil {
		return err
	}

	for _, holders := range claimsByEntity {
		if len(holders) > 1 {
			result.Conflicts++
		}
	}

	_, r, _ := openCoordinator()
	if r != nil {
		result.ActiveID = readActiveAgentID(r)
	}

	if jsonOutput {
		return writeJSON(out, result)
	}

	fmt.Fprintln(out, "Coordination Dashboard (all workspaces)")
	fmt.Fprintln(out, strings.Repeat("-", 40))
	fmt.Fprintf(out, "  Agents:     %d\n", result.Agents)
	fmt.Fprintf(out, "  Claims:     %d\n", result.Claims)
	fmt.Fprintf(out, "  Conflicts:  %d\n", result.Conflicts)
	fmt.Fprintf(out, "  Feed:       %d event(s)\n", result.FeedCount)
	fmt.Fprintf(out, "  Notes:      %d\n", result.Notes)
	fmt.Fprintf(out, "  Tasks:      %d (%d pending, %d active)\n", result.Tasks, result.TasksPending, result.TasksActive)
	if result.ActiveID != "" {
		fmt.Fprintf(out, "  Active as:  %s\n", result.ActiveID)
	} else {
		fmt.Fprintf(out, "  Active as:  (not joined)\n")
	}

	return nil
}

func coordStatusSummary(c *coord.Coordinator, r *repo.Repo) JSONCoordStatusOutput {
	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()
	feedEvents, _ := c.WalkFeed("", 100)
	tasks, _ := c.ListTasks()
	notes, _ := c.ListNotes()

	conflictCount := 0
	claimsByEntity := make(map[string][]string)
	for _, cl := range claims {
		claimsByEntity[cl.EntityKeyHash] = append(claimsByEntity[cl.EntityKeyHash], cl.AgentName)
	}
	for _, holders := range claimsByEntity {
		if len(holders) > 1 {
			conflictCount++
		}
	}

	taskPending, taskInProgress := 0, 0
	for _, task := range tasks {
		switch task.Status {
		case "pending":
			taskPending++
		case "in_progress":
			taskInProgress++
		}
	}

	return JSONCoordStatusOutput{
		Agents:       len(agents),
		Claims:       len(claims),
		Conflicts:    conflictCount,
		FeedCount:    len(feedEvents),
		Notes:        len(notes),
		Tasks:        len(tasks),
		TasksPending: taskPending,
		TasksActive:  taskInProgress,
		ActiveID:     readActiveAgentID(r),
	}
}

// --- Agents ---

func newCoordAgentsCmd(jsonFlag *bool) *cobra.Command {
	var allWorkspaces bool

	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List registered agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			var allAgents []coord.AgentInfo

			if allWorkspaces {
				cfg, err := userconfig.Load()
				if err != nil {
					return fmt.Errorf("load user config: %w", err)
				}
				if cfg.Workspaces != nil {
					for wsName, wsPath := range cfg.Workspaces {
						r, err := openRepo(wsPath)
						if err != nil {
							continue // skip non-graft workspaces
						}
						wc := coord.New(r, coord.DefaultConfig)
						agents, err := wc.ListAgents()
						if err != nil {
							continue
						}
						for i := range agents {
							if agents[i].Workspace == "" {
								agents[i].Workspace = wsName
							}
						}
						allAgents = append(allAgents, agents...)
					}
				}
				// Also include current repo
				c, _, err := openCoordinatorForCommand(cmd)
				if err == nil {
					agents, _ := c.ListAgents()
					// Deduplicate: skip agents already collected
					seen := make(map[string]bool)
					for _, a := range allAgents {
						seen[a.ID] = true
					}
					for _, a := range agents {
						if !seen[a.ID] {
							allAgents = append(allAgents, a)
						}
					}
				}
			} else {
				c, _, err := openCoordinatorForCommand(cmd)
				if err != nil {
					return err
				}
				agents, err := c.ListAgents()
				if err != nil {
					return fmt.Errorf("list agents: %w", err)
				}
				allAgents = agents
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordAgentsOutput{Agents: allAgents})
			}

			if len(allAgents) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No active agents.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tWORKSPACE\tHOST\tHEARTBEAT")
			for _, a := range allAgents {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					a.ID, a.Name, a.Workspace, a.Host,
					a.HeartbeatAt.Format("15:04:05"))
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&allWorkspaces, "all", false, "list agents across all registered workspaces")
	return cmd
}

// --- Claims ---

func newCoordClaimsCmd(jsonFlag *bool) *cobra.Command {
	var (
		workspace     string
		allWorkspaces bool
	)

	cmd := &cobra.Command{
		Use:   "claims",
		Short: "List active claims",
		RunE: func(cmd *cobra.Command, args []string) error {
			type claimWithSource struct {
				coord.ClaimInfo
				SourceWorkspace string `json:"source_workspace,omitempty"`
			}

			var allClaims []claimWithSource

			if allWorkspaces {
				if err := iterateWorkspaces(func(name string, c *coord.Coordinator) error {
					claims, err := c.ListClaims()
					if err != nil {
						return nil
					}
					for _, cl := range claims {
						allClaims = append(allClaims, claimWithSource{
							ClaimInfo:       cl,
							SourceWorkspace: name,
						})
					}
					return nil
				}); err != nil {
					return err
				}
			} else {
				c, _, err := openCoordinatorForCommand(cmd)
				if err != nil {
					return err
				}
				claims, err := c.ListClaims()
				if err != nil {
					return fmt.Errorf("list claims: %w", err)
				}
				for _, cl := range claims {
					allClaims = append(allClaims, claimWithSource{ClaimInfo: cl})
				}
			}

			// Filter by workspace agent name prefix if requested
			if workspace != "" {
				var filtered []claimWithSource
				for _, cl := range allClaims {
					if strings.Contains(cl.AgentName, workspace) || strings.Contains(cl.File, workspace) {
						filtered = append(filtered, cl)
					}
				}
				allClaims = filtered
			}

			if *jsonFlag {
				jsonClaims := make([]JSONCoordClaim, 0, len(allClaims))
				for _, claim := range allClaims {
					jsonClaims = append(jsonClaims, coordClaimToJSON(claim.ClaimInfo, claim.SourceWorkspace))
				}
				return writeJSON(cmd.OutOrStdout(), JSONCoordClaimsOutput{Claims: jsonClaims})
			}

			if len(allClaims) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No active claims.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			if allWorkspaces {
				fmt.Fprintln(w, "ENTITY\tFILE\tAGENT\tMODE\tSOURCE\tSINCE")
				for _, cl := range allClaims {
					entityDisplay := cl.EntityKey
					if len(entityDisplay) > 60 {
						entityDisplay = entityDisplay[:57] + "..."
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
						entityDisplay, cl.File, cl.AgentName, cl.Mode,
						cl.SourceWorkspace,
						cl.ClaimedAt.Format("15:04:05"))
				}
			} else {
				fmt.Fprintln(w, "ENTITY\tFILE\tAGENT\tMODE\tSINCE")
				for _, cl := range allClaims {
					entityDisplay := cl.EntityKey
					if len(entityDisplay) > 60 {
						entityDisplay = entityDisplay[:57] + "..."
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
						entityDisplay, cl.File, cl.AgentName, cl.Mode,
						cl.ClaimedAt.Format("15:04:05"))
				}
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "filter claims by workspace")
	cmd.Flags().BoolVar(&allWorkspaces, "all", false, "aggregate claims across all registered workspaces")
	return cmd
}

// --- Decisions ---

func newCoordDecisionsCmd(jsonFlag *bool) *cobra.Command {
	var limit int
	var mine bool
	var entity string

	cmd := &cobra.Command{
		Use:   "decisions",
		Short: "Show recent local decision traces",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			decisions, err := coord.ListDecisions(r.GraftDir, 0)
			if err != nil {
				return fmt.Errorf("list decisions: %w", err)
			}

			if mine {
				activeID := readActiveAgentID(r)
				var filtered []coord.DecisionGraph
				for _, decision := range decisions {
					if activeID != "" && decision.AgentID == activeID {
						filtered = append(filtered, decision)
					}
				}
				decisions = filtered
			}

			if entity != "" {
				var filtered []coord.DecisionGraph
				for _, decision := range decisions {
					if strings.Contains(decision.EntityKey, entity) || strings.Contains(decision.File, entity) {
						filtered = append(filtered, decision)
					}
				}
				decisions = filtered
			}

			if limit > 0 && len(decisions) > limit {
				decisions = decisions[:limit]
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordDecisionsOutput{Decisions: decisions})
			}

			if len(decisions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No decision traces.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TIME\tOUTCOME\tACTION\tENTITY\tRULE\tSOURCE")
			for _, decision := range decisions {
				entityDisplay := decision.EntityKey
				if entityDisplay == "" {
					entityDisplay = decision.File
				}
				if len(entityDisplay) > 60 {
					entityDisplay = entityDisplay[:57] + "..."
				}
				rule := decision.Rule
				if rule == "" {
					rule = "-"
				}
				source := decision.Source
				if source == "" {
					source = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					decision.CreatedAt.Format("2006-01-02 15:04:05"),
					decision.Outcome.Status,
					decision.Action,
					entityDisplay,
					rule,
					source,
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of decision traces to show")
	cmd.Flags().BoolVar(&mine, "mine", false, "show only decision traces for the active agent")
	cmd.Flags().StringVar(&entity, "entity", "", "filter decision traces by entity key or file path")
	return cmd
}

// --- Feed ---

func newCoordFeedCmd(jsonFlag *bool) *cobra.Command {
	var (
		since         string
		mine          bool
		allWorkspaces bool
	)

	cmd := &cobra.Command{
		Use:   "feed",
		Short: "Show coordination feed events",
		RunE: func(cmd *cobra.Command, args []string) error {
			type feedEventWithSource struct {
				coord.FeedEvent
				SourceWorkspace string `json:"source_workspace,omitempty"`
			}

			var allEvents []feedEventWithSource

			if allWorkspaces {
				if err := iterateWorkspaces(func(name string, c *coord.Coordinator) error {
					events, err := c.WalkFeed(since, 50)
					if err != nil {
						return nil
					}
					for _, ev := range events {
						allEvents = append(allEvents, feedEventWithSource{
							FeedEvent:       ev,
							SourceWorkspace: name,
						})
					}
					return nil
				}); err != nil {
					return err
				}
				// Sort by timestamp (FeedHash is derived from content, not time, so
				// we use the event order -- most recent first within each workspace).
			} else {
				c, _, err := openCoordinatorForCommand(cmd)
				if err != nil {
					return err
				}
				events, err := c.WalkFeed(since, 50)
				if err != nil {
					return fmt.Errorf("walk feed: %w", err)
				}
				for _, ev := range events {
					allEvents = append(allEvents, feedEventWithSource{FeedEvent: ev})
				}
			}

			if mine {
				_, r, _ := openCoordinatorForCommand(cmd)
				if r != nil {
					activeID := readActiveAgentID(r)
					if activeID != "" {
						var filtered []feedEventWithSource
						for _, ev := range allEvents {
							if ev.AgentID == activeID {
								filtered = append(filtered, ev)
							}
						}
						allEvents = filtered
					}
				}
			}

			if *jsonFlag {
				jsonEvents := make([]JSONCoordFeedEntry, 0, len(allEvents))
				for _, event := range allEvents {
					jsonEvents = append(jsonEvents, coordFeedEventToJSON(event.FeedEvent, event.SourceWorkspace))
				}
				return writeJSON(cmd.OutOrStdout(), JSONCoordFeedOutput{Events: jsonEvents})
			}

			if len(allEvents) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No feed events.")
				return nil
			}

			out := cmd.OutOrStdout()
			for _, ev := range allEvents {
				prefix := ""
				if allWorkspaces && ev.SourceWorkspace != "" {
					prefix = fmt.Sprintf("[%s] ", ev.SourceWorkspace)
				}
				hashDisplay := ev.FeedHash
				if len(hashDisplay) > 8 {
					hashDisplay = hashDisplay[:8]
				}
				fmt.Fprintf(out, "%s[%s] %s by %s", prefix, hashDisplay, ev.Event, ev.AgentName)
				if ev.CommitHash != "" {
					commitDisplay := ev.CommitHash
					if len(commitDisplay) > 8 {
						commitDisplay = commitDisplay[:8]
					}
					fmt.Fprintf(out, " (commit %s)", commitDisplay)
				}
				fmt.Fprintln(out)
				for _, ent := range ev.Entities {
					breaking := ""
					if ent.Breaking {
						breaking = " [BREAKING]"
					}
					fmt.Fprintf(out, "  %s %s in %s%s\n", ent.Change, ent.Key, ent.File, breaking)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "show events after this feed hash")
	cmd.Flags().BoolVar(&mine, "mine", false, "show only my events")
	cmd.Flags().BoolVar(&allWorkspaces, "all", false, "aggregate feed events across all registered workspaces")
	return cmd
}

func coordClaimToJSON(claim coord.ClaimInfo, sourceWorkspace string) JSONCoordClaim {
	return JSONCoordClaim{
		EntityKey:       claim.EntityKey,
		EntityKeyHash:   claim.EntityKeyHash,
		File:            claim.File,
		Agent:           claim.Agent,
		AgentName:       claim.AgentName,
		Mode:            claim.Mode,
		ClaimedAt:       claim.ClaimedAt.Format(time.RFC3339),
		SourceWorkspace: sourceWorkspace,
	}
}

func coordFeedEventToJSON(event coord.FeedEvent, sourceWorkspace string) JSONCoordFeedEntry {
	return JSONCoordFeedEntry{
		Event:           event.Event,
		AgentID:         event.AgentID,
		AgentName:       event.AgentName,
		CommitHash:      event.CommitHash,
		Entities:        event.Entities,
		Impact:          event.Impact,
		FeedHash:        event.FeedHash,
		Detail:          event.Detail,
		Digest:          event.Digest,
		Source:          event.Source,
		SourceWorkspace: sourceWorkspace,
	}
}

// --- Impact ---

func newCoordImpactCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "impact [entity-key]",
		Short: "Run impact analysis for entity changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			cfg, _ := userconfig.Load()
			workspaces := make(map[string]string)
			if cfg != nil && cfg.Workspaces != nil {
				workspaces = cfg.Workspaces
			}

			var changes []coord.EntityChange
			if len(args) > 0 {
				for _, key := range args {
					changes = append(changes, coord.EntityChange{
						Key:    key,
						Change: "unknown",
					})
				}
			} else {
				// Use recent feed events to get changes
				events, _ := c.WalkFeed("", 10)
				for _, ev := range events {
					changes = append(changes, ev.Entities...)
				}
			}

			if len(changes) == 0 {
				if *jsonFlag {
					return writeJSON(cmd.OutOrStdout(), coordImpactReportToJSON(&coord.ImpactReport{}))
				}
				fmt.Fprintln(cmd.OutOrStdout(), "No entity changes to analyze.")
				return nil
			}

			report, err := c.AnalyzeImpact(changes, workspaces)
			if err != nil {
				return fmt.Errorf("analyze impact: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), coordImpactReportToJSON(report))
			}

			if len(report.Workspaces) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No cross-workspace impact detected.")
				return nil
			}

			out := cmd.OutOrStdout()
			for ws, impact := range report.Workspaces {
				fmt.Fprintf(out, "Workspace: %s\n", ws)
				if len(impact.Callers) > 0 {
					fmt.Fprintln(out, "  Affected callers:")
					for _, caller := range impact.Callers {
						fmt.Fprintf(out, "    %s\n", caller)
					}
				}
				if len(impact.AgentsAffected) > 0 {
					fmt.Fprintln(out, "  Agents affected:")
					for _, agent := range impact.AgentsAffected {
						fmt.Fprintf(out, "    %s\n", agent)
					}
				}
			}

			return nil
		},
	}
}

// --- Check ---

func newCoordCheckCmd(jsonFlag *bool) *cobra.Command {
	var (
		quiet      bool
		staleAfter = coord.DefaultConfig.StaleThreshold
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Quick conflict check (hook-optimized)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}
			if staleAfter <= 0 {
				return fmt.Errorf("--stale-after must be positive")
			}
			c.Config.StaleThreshold = staleAfter

			activeID := readActiveAgentID(r)
			claims, _ := c.ListClaims()
			agents, _ := c.ListAgents()
			activeClaims, staleAgents := coordCheckClaimAndAgentSummary(c, claims, agents)
			unreadFeedEvents := coordCheckUnreadFeedEvents(c, activeID, 20)

			var conflicts []JSONCoordCheckConflict
			if activeID != "" {
				// Check if any of our agent's potential claims conflict
				for _, cl := range claims {
					if cl.Agent != activeID && cl.Mode == coord.ClaimEditing {
						req := coord.ClaimRequest{
							EntityKey: cl.EntityKey,
							File:      cl.File,
							Mode:      coord.ClaimEditing,
						}
						ctx, decisionErr := c.InspectClaimDecisionWithExisting(activeID, req, &cl)
						if decisionErr != nil {
							return fmt.Errorf("evaluate claim decision: %w", decisionErr)
						}
						recordCoordDecision(c, cmd.ErrOrStderr(), "graft coord check", activeID, req, ctx, coord.DecisionOutcome{
							Status:  "inspection_reported",
							Message: coordDecisionMessage(req, ctx),
						})
						conflicts = append(conflicts, JSONCoordCheckConflict{
							EntityKey:    cl.EntityKey,
							File:         cl.File,
							HeldBy:       cl.AgentName,
							Mode:         cl.Mode,
							Decision:     ctx.Decision.Action,
							Reason:       ctx.Decision.Reason,
							Rule:         ctx.Decision.Rule,
							RequireForce: ctx.Decision.RequireForce,
						})
					}
				}
			}

			// Informational: other agents currently reading the code (read
			// presence). A soft coordination signal, not a blocking conflict, so
			// it does not affect OK or the quiet/hook exit code.
			var readers []JSONCoordCheckReader
			if presence, perr := c.ListPresence(); perr == nil {
				for _, e := range coord.OtherAgentPresence(presence, activeID) {
					readers = append(readers, JSONCoordCheckReader{AgentName: e.AgentName, File: e.File, Entity: e.Entity})
				}
			}

			result := JSONCoordCheckOutput{
				OK:               len(conflicts) == 0,
				ActiveAgentID:    activeID,
				AgentsExamined:   len(agents),
				ClaimsExamined:   len(claims),
				ActiveClaims:     activeClaims,
				StaleAgents:      staleAgents,
				UnreadFeedEvents: unreadFeedEvents,
				Conflicts:        conflicts,
				Readers:          readers,
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), result)
			}

			out := cmd.OutOrStdout()
			if quiet {
				if !result.OK {
					fmt.Fprintf(out, "%d conflict(s)\n", len(conflicts))
					return fmt.Errorf("conflicts detected")
				}
				return nil
			}

			if result.OK {
				fmt.Fprintln(out, "No conflicts detected.")
			} else {
				fmt.Fprintf(out, "%d conflict(s) detected:\n", len(conflicts))
				for _, c := range conflicts {
					decision := c.Decision
					if decision == "" {
						decision = "Conflict"
					}
					fmt.Fprintf(out, "  [%s] %s in %s (held by %s)\n", decision, c.EntityKey, c.File, c.HeldBy)
					if c.Reason != "" {
						fmt.Fprintf(out, "    %s\n", c.Reason)
					}
				}
			}

			if len(result.Readers) > 0 {
				fmt.Fprintf(out, "%d other agent(s) currently reading:\n", len(result.Readers))
				for _, rd := range result.Readers {
					loc := rd.File
					if rd.Entity != "" {
						loc = rd.File + "::" + rd.Entity
					}
					fmt.Fprintf(out, "  %s reading %s\n", rd.AgentName, loc)
				}
			}
			if len(result.ActiveClaims) > 0 {
				fmt.Fprintf(out, "%d active claim(s)\n", len(result.ActiveClaims))
			}
			if len(result.StaleAgents) > 0 {
				fmt.Fprintf(out, "%d stale agent(s):\n", len(result.StaleAgents))
				for _, agent := range result.StaleAgents {
					name := agent.Name
					if name == "" {
						name = agent.ID
					}
					fmt.Fprintf(out, "  %s heartbeat %s\n", name, agent.HeartbeatAt)
				}
			}
			if len(result.UnreadFeedEvents) > 0 {
				fmt.Fprintf(out, "%d unread feed event(s)\n", len(result.UnreadFeedEvents))
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&quiet, "quiet", false, "minimal output (exit code only)")
	cmd.Flags().DurationVar(&staleAfter, "stale-after", coord.DefaultConfig.StaleThreshold, "agent heartbeat age before reporting stale")
	return cmd
}

func coordCheckClaimAndAgentSummary(c *coord.Coordinator, claims []coord.ClaimInfo, agents []coord.AgentInfo) ([]JSONCoordCheckClaim, []JSONCoordCheckAgent) {
	staleAgents := coordStaleAgentSummaries(c, agents)
	staleByAgent := make(map[string]bool, len(staleAgents))
	for _, agent := range staleAgents {
		staleByAgent[agent.ID] = true
	}

	activeClaims := make([]JSONCoordCheckClaim, 0, len(claims))
	for _, claim := range claims {
		activeClaims = append(activeClaims, JSONCoordCheckClaim{
			EntityKey: claim.EntityKey,
			File:      claim.File,
			AgentID:   claim.Agent,
			AgentName: claim.AgentName,
			Mode:      claim.Mode,
			Stale:     staleByAgent[claim.Agent],
		})
	}
	sort.Slice(activeClaims, func(i, j int) bool {
		if activeClaims[i].File != activeClaims[j].File {
			return activeClaims[i].File < activeClaims[j].File
		}
		if activeClaims[i].EntityKey != activeClaims[j].EntityKey {
			return activeClaims[i].EntityKey < activeClaims[j].EntityKey
		}
		return activeClaims[i].AgentName < activeClaims[j].AgentName
	})

	return activeClaims, staleAgents
}

func coordStaleAgentSummaries(c *coord.Coordinator, agents []coord.AgentInfo) []JSONCoordCheckAgent {
	now := time.Now().UTC()
	cutoff := now.Add(-c.Config.StaleThreshold)

	var staleAgents []JSONCoordCheckAgent
	for _, agent := range agents {
		if agent.HeartbeatAt.IsZero() || agent.HeartbeatAt.Before(cutoff) {
			staleFor := int64(0)
			if !agent.HeartbeatAt.IsZero() {
				staleFor = int64(now.Sub(agent.HeartbeatAt).Seconds())
			}
			staleAgents = append(staleAgents, JSONCoordCheckAgent{
				ID:          agent.ID,
				Name:        agent.Name,
				Workspace:   agent.Workspace,
				Host:        agent.Host,
				HeartbeatAt: agent.HeartbeatAt.Format(time.RFC3339),
				StaleForSec: staleFor,
			})
		}
	}
	sort.Slice(staleAgents, func(i, j int) bool {
		if staleAgents[i].Name != staleAgents[j].Name {
			return staleAgents[i].Name < staleAgents[j].Name
		}
		return staleAgents[i].ID < staleAgents[j].ID
	})
	return staleAgents
}

func newCoordCleanupStaleCmd(jsonFlag *bool) *cobra.Command {
	var (
		dryRun     bool
		staleAfter = coord.DefaultConfig.StaleThreshold
	)

	cmd := &cobra.Command{
		Use:   "cleanup-stale",
		Short: "Remove stale coordination agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}
			if staleAfter <= 0 {
				return fmt.Errorf("--stale-after must be positive")
			}
			c.Config.StaleThreshold = staleAfter

			agents, err := c.ListAgents()
			if err != nil {
				return fmt.Errorf("list agents: %w", err)
			}
			staleAgents := coordStaleAgentSummaries(c, agents)
			removed := 0
			if !dryRun {
				removedAgents, err := c.GCStaleAgents()
				if err != nil {
					return fmt.Errorf("cleanup stale agents: %w", err)
				}
				removed = len(removedAgents)
				staleAgents = coordStaleAgentSummaries(c, removedAgents)
			}

			result := JSONCoordCleanupStaleOutput{
				OK:          true,
				DryRun:      dryRun,
				Removed:     removed,
				StaleAgents: staleAgents,
			}
			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), result)
			}

			out := cmd.OutOrStdout()
			switch {
			case dryRun && len(staleAgents) == 0:
				fmt.Fprintln(out, "No stale agents found.")
			case dryRun:
				fmt.Fprintf(out, "%d stale agent(s) would be removed:\n", len(staleAgents))
				for _, agent := range staleAgents {
					fmt.Fprintf(out, "  %s (%s) heartbeat %s\n", coordDisplayAgentName(agent), agent.ID, agent.HeartbeatAt)
				}
			case removed == 0:
				fmt.Fprintln(out, "No stale agents removed.")
			default:
				fmt.Fprintf(out, "%d stale agent(s) removed:\n", removed)
				for _, agent := range staleAgents {
					fmt.Fprintf(out, "  %s (%s) heartbeat %s\n", coordDisplayAgentName(agent), agent.ID, agent.HeartbeatAt)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show stale agents without removing them")
	cmd.Flags().DurationVar(&staleAfter, "stale-after", coord.DefaultConfig.StaleThreshold, "agent heartbeat age before removing stale agents")
	return cmd
}

func coordDisplayAgentName(agent JSONCoordCheckAgent) string {
	if agent.Name != "" {
		return agent.Name
	}
	return agent.ID
}

func coordCheckUnreadFeedEvents(c *coord.Coordinator, activeID string, limit int) []JSONCoordCheckFeedEvent {
	if limit <= 0 {
		return nil
	}
	var events []coord.FeedEvent
	var err error
	if activeID != "" {
		events, err = c.WalkFeedSinceCursor(activeID, limit)
	} else {
		events, err = c.WalkFeed("", limit)
	}
	if err != nil {
		return nil
	}
	out := make([]JSONCoordCheckFeedEvent, 0, len(events))
	for _, event := range events {
		filesSeen := make(map[string]struct{}, len(event.Entities))
		var files []string
		for _, entity := range event.Entities {
			if entity.File == "" {
				continue
			}
			if _, seen := filesSeen[entity.File]; seen {
				continue
			}
			filesSeen[entity.File] = struct{}{}
			files = append(files, entity.File)
		}
		sort.Strings(files)
		out = append(out, JSONCoordCheckFeedEvent{
			Event:     event.Event,
			AgentID:   event.AgentID,
			AgentName: event.AgentName,
			FeedHash:  event.FeedHash,
			Files:     files,
		})
	}
	return out
}

// --- Diff ---

func newCoordDiffCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <agent-id>",
		Short: "Show another agent's claimed entities",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			targetID := args[0]
			agent, err := c.GetAgent(targetID)
			if err != nil {
				return fmt.Errorf("agent not found: %w", err)
			}

			claims, _ := c.ListClaims()
			var agentClaims []coord.ClaimInfo
			for _, cl := range claims {
				if cl.Agent == targetID {
					agentClaims = append(agentClaims, cl)
				}
			}

			result := JSONCoordDiffOutput{
				Agent:  agent,
				Claims: agentClaims,
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), result)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Agent: %s (%s)\n", agent.Name, agent.ID)
			if len(agentClaims) == 0 {
				fmt.Fprintln(out, "No active claims.")
				return nil
			}

			fmt.Fprintf(out, "Claims (%d):\n", len(agentClaims))
			for _, cl := range agentClaims {
				fmt.Fprintf(out, "  [%s] %s in %s\n", cl.Mode, cl.EntityKey, cl.File)
			}

			return nil
		},
	}
}

// --- Xrefs ---

func newCoordXrefsCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "xrefs <qualified-name>",
		Short: "Reverse call lookup for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			key := args[0]
			idx, err := c.LoadXrefIndex()
			if err != nil {
				// Try building a fresh one
				cfg, _ := userconfig.Load()
				if cfg != nil {
					modulePath := ""
					gomodPath := filepath.Join(c.Repo.RootDir, "go.mod")
					if deps, parseErr := coord.ParseGoModDeps(gomodPath); parseErr == nil {
						modulePath = deps.Module
					}
					idx, err = coord.BuildXrefIndex(c.Repo.RootDir, modulePath)
					if err != nil {
						return fmt.Errorf("build xref index: %w", err)
					}
				} else {
					return fmt.Errorf("no xref index available: %w", err)
				}
			}

			sites, ok := idx.Refs[key]
			if !ok {
				if *jsonFlag {
					return writeJSON(cmd.OutOrStdout(), JSONCoordXrefsOutput{References: []coord.XrefCallSite{}})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "No references found for %s\n", key)
				return nil
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordXrefsOutput{References: sites})
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "References to %s:\n", key)
			for _, site := range sites {
				fmt.Fprintf(out, "  %s:%d in %s\n", site.File, site.Line, site.Entity)
			}

			return nil
		},
	}
}

// --- Graph ---

func newCoordGraphCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "graph",
		Short: "Show workspace dependency graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := userconfig.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if cfg.Workspaces == nil || len(cfg.Workspaces) == 0 {
				if *jsonFlag {
					return writeJSON(cmd.OutOrStdout(), JSONCoordGraphOutput{
						Workspaces: map[string]string{},
						Edges:      []JSONCoordGraphEdge{},
					})
				}
				fmt.Fprintln(cmd.OutOrStdout(), "No workspaces configured. Use 'graft workspace add' or 'graft workon --auto-discover'.")
				return nil
			}

			graph, err := coord.BuildWorkspaceGraph(cfg.Workspaces)
			if err != nil {
				return fmt.Errorf("build workspace graph: %w", err)
			}

			var edges []JSONCoordGraphEdge
			for wsName := range cfg.Workspaces {
				deps := graph.DependentsOf(wsName)
				for _, dep := range deps {
					edges = append(edges, JSONCoordGraphEdge{From: dep, To: wsName})
				}
			}

			if *jsonFlag {
				result := JSONCoordGraphOutput{
					Workspaces: cfg.Workspaces,
					Edges:      edges,
				}
				return writeJSON(cmd.OutOrStdout(), result)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Workspace Dependency Graph")
			fmt.Fprintln(out, strings.Repeat("-", 40))
			for wsName, wsPath := range cfg.Workspaces {
				deps := graph.DependentsOf(wsName)
				if len(deps) > 0 {
					fmt.Fprintf(out, "  %s (%s)\n", wsName, wsPath)
					fmt.Fprintf(out, "    depended on by: %s\n", strings.Join(deps, ", "))
				} else {
					fmt.Fprintf(out, "  %s (%s) [leaf]\n", wsName, wsPath)
				}
			}

			return nil
		},
	}
}

// --- Watch ---

func newCoordWatchCmd(jsonFlag *bool) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "watch <entity-key>",
		Short: "Add a watch claim on an entity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			if activeID == "" {
				return fmt.Errorf("no active coordination session; run 'graft workon --as <name>' first")
			}

			entityKey := args[0]

			// Try to resolve entity key to a file if not provided
			filePath := file
			if filePath == "" {
				filePath = c.ResolveEntityFile(entityKey)
			}

			err = c.AcquireClaim(activeID, coord.ClaimRequest{
				EntityKey: entityKey,
				File:      filePath,
				Mode:      coord.ClaimWatching,
			})
			if err != nil {
				return fmt.Errorf("watch: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordWatchOutput{
					Status:    "watching",
					EntityKey: entityKey,
					File:      filePath,
				})
			}

			if filePath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Watching: %s (in %s)\n", entityKey, filePath)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Watching: %s\n", entityKey)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "file path for the entity (auto-resolved if omitted)")
	return cmd
}

// --- Unwatch ---

func newCoordUnwatchCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "unwatch <entity-key>",
		Short: "Remove a watch claim",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			if activeID == "" {
				return fmt.Errorf("no active coordination session; run 'graft workon --as <name>' first")
			}

			entityKey := args[0]
			keyHash := coord.EntityKeyHash(entityKey)
			if err := c.ReleaseWatch(keyHash, activeID); err != nil {
				return fmt.Errorf("unwatch: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordUnwatchOutput{
					Status:    "unwatched",
					EntityKey: entityKey,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Stopped watching: %s\n", entityKey)
			return nil
		},
	}
}

// --- Resolve ---

func newCoordResolveCmd(jsonFlag *bool) *cobra.Command {
	var transfer string

	cmd := &cobra.Command{
		Use:   "resolve <entity-key-hash>",
		Short: "Release or transfer a claim",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			keyHash := args[0]
			activeID := readActiveAgentID(r)

			if transfer != "" {
				if activeID == "" {
					return fmt.Errorf("no active session for transfer source")
				}
				if err := c.TransferClaim(keyHash, activeID, transfer); err != nil {
					return fmt.Errorf("transfer: %w", err)
				}
				if *jsonFlag {
					return writeJSON(cmd.OutOrStdout(), JSONCoordResolveOutput{
						Status:  "transferred",
						KeyHash: keyHash,
						ToAgent: transfer,
					})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Claim %s transferred to %s\n", keyHash, transfer)
				return nil
			}

			if err := c.ReleaseClaim(keyHash); err != nil {
				return fmt.Errorf("release: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordResolveOutput{
					Status:  "released",
					KeyHash: keyHash,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Claim %s released\n", keyHash)
			return nil
		},
	}

	cmd.Flags().StringVar(&transfer, "transfer", "", "transfer claim to another agent ID")
	return cmd
}

// --- Plan ---

func newCoordPlanCmd(jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Manage coordination plans",
	}

	cmd.AddCommand(newCoordPlanListCmd(jsonFlag))
	cmd.AddCommand(newCoordPlanGetCmd(jsonFlag))
	cmd.AddCommand(newCoordPlanCreateCmd(jsonFlag))

	return cmd
}

func newCoordPlanListCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all coordination plans",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			plans, err := c.ListPlans()
			if err != nil {
				return fmt.Errorf("list plans: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordPlansOutput{Plans: plans})
			}

			if len(plans) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No plans.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tSTEPS\tCREATED")
			for _, p := range plans {
				completed := 0
				for _, s := range p.Steps {
					if s.Status == "completed" {
						completed++
					}
				}
				stepSummary := fmt.Sprintf("%d/%d done", completed, len(p.Steps))
				if len(p.Steps) == 0 {
					stepSummary = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					p.ID, truncate(p.Title, 40), p.Status, stepSummary,
					p.CreatedAt.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

func newCoordPlanGetCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "get <plan-id>",
		Short: "Show plan details with step status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			plan, err := c.GetPlan(args[0])
			if err != nil {
				return fmt.Errorf("get plan: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordPlanOutput{Plan: plan})
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Plan: %s\n", plan.Title)
			fmt.Fprintf(out, "  ID:      %s\n", plan.ID)
			fmt.Fprintf(out, "  Status:  %s\n", plan.Status)
			if plan.Author != "" {
				fmt.Fprintf(out, "  Author:  %s\n", plan.Author)
			}
			if plan.Description != "" {
				fmt.Fprintf(out, "  Desc:    %s\n", plan.Description)
			}
			fmt.Fprintf(out, "  Created: %s\n", plan.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Fprintf(out, "  Updated: %s\n", plan.UpdatedAt.Format("2006-01-02 15:04:05"))

			if len(plan.Steps) == 0 {
				fmt.Fprintln(out, "  Steps:   (none)")
				return nil
			}

			fmt.Fprintf(out, "  Steps (%d):\n", len(plan.Steps))
			for _, step := range plan.Steps {
				marker := " "
				switch step.Status {
				case "completed":
					marker = "x"
				case "in_progress":
					marker = ">"
				case "skipped":
					marker = "-"
				}
				assignee := ""
				if step.AssignedTo != "" {
					assignee = fmt.Sprintf(" [%s]", step.AssignedTo)
				}
				fmt.Fprintf(out, "    [%s] %d. %s%s\n", marker, step.Order, step.Description, assignee)
			}

			return nil
		},
	}
}

func newCoordPlanCreateCmd(jsonFlag *bool) *cobra.Command {
	var (
		description string
		status      string
	)

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new coordination plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			author := readActiveAgentID(r)

			plan := &coord.Plan{
				Title:       args[0],
				Description: description,
				Status:      status,
				Author:      author,
			}

			if err := c.CreatePlan(plan); err != nil {
				return fmt.Errorf("create plan: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordPlanOutput{Plan: plan})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created plan %s: %s\n", plan.ID, plan.Title)
			return nil
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "plan description")
	cmd.Flags().StringVar(&status, "status", "draft", "initial status (draft, active)")
	return cmd
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// --- Publish ---

func newCoordPublishCmd(jsonFlag *bool) *cobra.Command {
	var commitRef string

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a feed event for a commit (for git/buckley commits)",
		Long:  `Manually publish a coordination feed event for the latest commit, useful when commits are made via git or buckley rather than graft.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			if activeID == "" {
				return fmt.Errorf("no active coordination session; run 'graft workon --as <name>' first")
			}
			c.AgentID = activeID

			// Resolve the commit to publish
			ref := commitRef
			if ref == "" {
				ref = "HEAD"
			}
			commitHash, err := c.Repo.ResolveRef(ref)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", ref, err)
			}

			if err := c.PostCommitHook(commitHash); err != nil {
				return fmt.Errorf("publish: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordPublishOutput{
					Status:     "published",
					CommitHash: string(commitHash),
					AgentID:    activeID,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Published feed event for commit %s\n", string(commitHash)[:12])
			return nil
		},
	}

	cmd.Flags().StringVar(&commitRef, "commit", "", "commit ref to publish (default: HEAD)")
	return cmd
}

// --- Heartbeat ---

func newCoordHeartbeatCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "heartbeat",
		Short: "Update the active agent's heartbeat timestamp",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			if activeID == "" {
				return fmt.Errorf("no active coordination session; run 'graft workon --as <name>' first")
			}

			if err := c.Heartbeat(activeID); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
			if _, err := coord.TouchSessionByAgentID(r.GraftDir, activeID); err != nil {
				return fmt.Errorf("touch session: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordHeartbeatOutput{
					Status:  "ok",
					AgentID: activeID,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Heartbeat updated for agent %s\n", activeID)
			return nil
		},
	}
}

// --- Sessions ---

func newCoordSessionsCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List active persistent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			sessions, err := coord.ListSessions(r.GraftDir)
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordSessionsOutput{Sessions: sessions})
			}

			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No active sessions.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "AGENT\tID\tMODE\tHOST\tPID\tLAST ACTIVE\tSTALE")
			for _, s := range sessions {
				stale := ""
				if coord.IsSessionStale(&s, coord.SessionStaleThreshold) {
					stale = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
					s.AgentName, s.AgentID, s.Mode, s.Host, s.PID,
					s.LastActive.Format("15:04:05"), stale)
			}
			return w.Flush()
		},
	}
}

// --- Presence ---

func newCoordPresenceCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "presence",
		Short: "Show who is reading what files",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			entries, err := c.ListPresence()
			if err != nil {
				return fmt.Errorf("list presence: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordPresenceOutput{Entries: entries})
			}

			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No active readers.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "AGENT\tFILE\tENTITY\tSINCE")
			for _, e := range entries {
				entity := e.Entity
				if entity == "" {
					entity = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					e.AgentName, e.File, entity,
					e.Timestamp.Format("15:04:05"))
			}
			return w.Flush()
		},
	}
}

// --- Reading ---

func newCoordReadingCmd(jsonFlag *bool) *cobra.Command {
	var entity string

	cmd := &cobra.Command{
		Use:   "reading <file>",
		Short: "Register that you are reading a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinatorForCommand(cmd)
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			if activeID == "" {
				return fmt.Errorf("no active coordination session; run 'graft workon --as <name>' first")
			}
			c.AgentID = activeID

			file := args[0]
			if err := c.RegisterPresence(file, entity); err != nil {
				return fmt.Errorf("register presence: %w", err)
			}

			if *jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONCoordReadingOutput{
					Status:  "reading",
					File:    file,
					AgentID: activeID,
					Entity:  entity,
				})
			}

			if entity != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Reading: %s (entity: %s)\n", file, entity)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Reading: %s\n", file)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&entity, "entity", "", "specific entity being read")
	return cmd
}
