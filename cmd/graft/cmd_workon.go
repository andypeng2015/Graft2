package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

func newWorkonCmd() *cobra.Command {
	var (
		name         string
		done         bool
		recover      bool
		autoDiscover bool
		notifyMode   string
		conflictMode string
		watchOnly    bool
		scope        string
		jsonFlag     bool
	)

	cmd := &cobra.Command{
		Use:   "workon",
		Short: "Join or leave a coordination session",
		Long: `Start coordinating entity-level changes with other agents in this repository.

Use --as to register as a named agent. Use --done to deregister and release all claims.
Use --recover --as <name> to replace a stale local identity after an interrupted session.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if recover && done {
				return fmt.Errorf("--recover cannot be combined with --done")
			}
			if !done && name == "" {
				return fmt.Errorf("either --as <name> or --done is required")
			}

			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return fmt.Errorf("open repo: %w", err)
			}

			cfg := coord.DefaultConfig
			if conflictMode != "" {
				cfg.ConflictMode = conflictMode
			}
			c := coord.New(r, cfg)

			if done {
				return workonDone(cmd.OutOrStdout(), c, r, name, jsonFlag)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return workonStartWithContext(ctx, cmd.OutOrStdout(), c, r, name, autoDiscover, notifyMode, watchOnly, scope, recover, jsonFlag)
		},
	}

	cmd.Flags().StringVar(&name, "as", "", "agent name")
	cmd.Flags().BoolVar(&done, "done", false, "leave coordination session")
	cmd.Flags().BoolVar(&recover, "recover", false, "replace a stale or missing local agent identity")
	cmd.Flags().BoolVar(&autoDiscover, "auto-discover", false, "discover workspaces from go.mod")
	cmd.Flags().StringVar(&notifyMode, "notify", "all", "notification filter: all, breaking")
	cmd.Flags().StringVar(&conflictMode, "conflict-mode", "", "override conflict mode")
	cmd.Flags().BoolVar(&watchOnly, "watch-only", false, "observe only, don't claim")
	cmd.Flags().StringVar(&scope, "scope", "", "limit coordination to package pattern")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")

	return cmd
}

func workonStart(out io.Writer, c *coord.Coordinator, r *repo.Repo, name string, autoDiscover bool, notifyMode string, watchOnly bool, scope string, recover bool, jsonOutput bool) error {
	return workonStartWithContext(context.Background(), out, c, r, name, autoDiscover, notifyMode, watchOnly, scope, recover, jsonOutput)
}

func workonStartWithContext(ctx context.Context, out io.Writer, c *coord.Coordinator, r *repo.Repo, name string, autoDiscover bool, notifyMode string, watchOnly bool, scope string, recover bool, jsonOutput bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cleanup := workonStartupCleanup{c: c, r: r, name: name}
	checkCanceled := func() error {
		if err := ctx.Err(); err != nil {
			cleanup.run()
			return fmt.Errorf("workon start canceled: %w", err)
		}
		return nil
	}
	startupError := func(format string, args ...any) error {
		cleanup.run()
		return fmt.Errorf(format, args...)
	}
	if err := checkCanceled(); err != nil {
		return err
	}

	hostname, _ := os.Hostname()

	mode := "editing"
	if watchOnly {
		mode = "watching"
	}

	// Check for existing persistent session
	var id string
	var resumed bool
	var recovered bool
	var recoveryReason string
	var previousAgentID string
	existingSession, err := coord.LoadSession(r.GraftDir, name)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if existingSession != nil {
		needsRecovery, reason := workonNeedsRecovery(c, existingSession, c.Config.StaleThreshold)
		if needsRecovery {
			if !recover {
				return fmt.Errorf("coordination session for %q needs recovery (%s); run 'graft workon --recover --as %s' to replace it", name, reason, name)
			}
			previousAgentID = existingSession.AgentID
			recovered = true
			recoveryReason = reason
			if previousAgentID != "" {
				c.AgentID = previousAgentID
				if err := c.DeregisterAgent(previousAgentID); err != nil {
					return fmt.Errorf("recover stale agent: %w", err)
				}
				_ = coord.ClearAgentPresence(r.GraftDir, previousAgentID)
			}
			_ = coord.RemoveSession(r.GraftDir, name)
			c.AgentID = ""
			existingSession = nil
		}
	}
	if existingSession != nil {
		// Resume: reuse the existing agent ID, update PID/host
		id = existingSession.AgentID
		c.AgentID = id
		existingSession.PID = os.Getpid()
		existingSession.Host = hostname
		existingSession.Scope = scope
		existingSession.Mode = mode
		if err := coord.TouchSession(r.GraftDir, existingSession); err != nil {
			return fmt.Errorf("touch session: %w", err)
		}
		// Update the agent heartbeat in the ref store too
		_ = c.Heartbeat(id)
		resumed = true
	} else {
		// New session (or replacing a stale one)
		info := coord.AgentInfo{
			Name:      name,
			Workspace: filepath.Base(r.RootDir),
			Host:      hostname,
		}

		var err error
		id, err = c.RegisterAgent(info)
		if err != nil {
			return fmt.Errorf("register agent: %w", err)
		}
		cleanup.agentID = id
		cleanup.cleanupNewSession = true
		if err := checkCanceled(); err != nil {
			return err
		}

		// Write persistent session
		sess := &coord.Session{
			AgentID:    id,
			AgentName:  name,
			Workspace:  filepath.Base(r.RootDir),
			Host:       hostname,
			StartedAt:  c.AgentStartedAt(),
			LastActive: c.AgentStartedAt(),
			PID:        os.Getpid(),
			Scope:      scope,
			Mode:       mode,
		}
		if err := coord.SaveSession(r.GraftDir, sess); err != nil {
			return startupError("save session: %w", err)
		}
		if err := checkCanceled(); err != nil {
			return err
		}
	}

	// Save the agent ID to .graft/coord/agent-{name} for later use by --done.
	// Per-agent files prevent concurrent agents from clobbering each other's session.
	agentIDDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(agentIDDir, 0o755); err != nil {
		return startupError("create coord dir: %w", err)
	}
	agentFileName := "agent-" + name
	if err := os.WriteFile(filepath.Join(agentIDDir, agentFileName), []byte(id), 0o644); err != nil {
		return startupError("save agent ID: %w", err)
	}
	// Also write a legacy agent-id for backwards compat with single-agent workflows
	_ = os.WriteFile(filepath.Join(agentIDDir, "agent-id"), []byte(id), 0o644)
	if err := checkCanceled(); err != nil {
		return err
	}

	// Auto-discover workspaces
	var discovered map[string]string
	if autoDiscover {
		discovered, _ = coord.AutoDiscoverWorkspaces(r.RootDir)
		if len(discovered) > 0 {
			cfg, err := userconfig.Load()
			if err == nil {
				if cfg.Workspaces == nil {
					cfg.Workspaces = make(map[string]string)
				}
				for wsName, wsPath := range discovered {
					if _, exists := cfg.Workspaces[wsName]; !exists {
						cfg.Workspaces[wsName] = wsPath
					}
				}
				_ = userconfig.Save(cfg)
			}
		}
	}
	if err := checkCanceled(); err != nil {
		return err
	}

	// Get current agent and peer state
	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()
	if err := checkCanceled(); err != nil {
		return err
	}

	status := "joined"
	if resumed {
		status = "resumed"
	} else if recovered {
		status = "recovered"
	}
	result := workonResult{
		Status:          status,
		AgentID:         id,
		AgentName:       name,
		Workspace:       filepath.Base(r.RootDir),
		Agents:          len(agents),
		Claims:          len(claims),
		Mode:            mode,
		Scope:           scope,
		Notify:          notifyMode,
		Recovered:       recovered,
		PreviousAgentID: previousAgentID,
		RecoveryReason:  recoveryReason,
	}
	if len(discovered) > 0 {
		result.Discovered = discovered
	}

	if jsonOutput {
		return writeJSON(out, result)
	}

	if resumed {
		fmt.Fprintln(out, "Coordination session resumed")
	} else if recovered {
		fmt.Fprintln(out, "Coordination session recovered")
	} else {
		fmt.Fprintln(out, "Coordination session started")
	}
	fmt.Fprintf(out, "  Agent:     %s (%s)\n", name, id)
	if previousAgentID != "" {
		fmt.Fprintf(out, "  Replaced:  %s (%s)\n", previousAgentID, recoveryReason)
	}
	fmt.Fprintf(out, "  Workspace: %s\n", filepath.Base(r.RootDir))
	fmt.Fprintf(out, "  Mode:      %s\n", result.Mode)
	if scope != "" {
		fmt.Fprintf(out, "  Scope:     %s\n", scope)
	}
	fmt.Fprintf(out, "  Peers:     %d agent(s) active\n", len(agents)-1)
	fmt.Fprintf(out, "  Claims:    %d active\n", len(claims))
	if len(discovered) > 0 {
		fmt.Fprintln(out, "  Discovered workspaces:")
		for wsName, wsPath := range discovered {
			fmt.Fprintf(out, "    %s -> %s\n", wsName, wsPath)
		}
	}

	return nil
}

type workonStartupCleanup struct {
	c                 *coord.Coordinator
	r                 *repo.Repo
	name              string
	agentID           string
	cleanupNewSession bool
}

func (cleanup workonStartupCleanup) run() {
	if !cleanup.cleanupNewSession || cleanup.c == nil || cleanup.r == nil || cleanup.agentID == "" {
		return
	}
	_ = cleanup.c.DeregisterAgent(cleanup.agentID)
	_ = coord.ClearAgentPresence(cleanup.r.GraftDir, cleanup.agentID)
	if cleanup.name != "" {
		_ = coord.RemoveSession(cleanup.r.GraftDir, cleanup.name)
		_ = os.Remove(filepath.Join(cleanup.r.GraftDir, "coord", "agent-"+cleanup.name))
	}

	legacyPath := filepath.Join(cleanup.r.GraftDir, "coord", "agent-id")
	if data, err := os.ReadFile(legacyPath); err == nil && string(data) == cleanup.agentID {
		_ = os.Remove(legacyPath)
	}
}

func workonNeedsRecovery(c *coord.Coordinator, sess *coord.Session, staleThreshold time.Duration) (bool, string) {
	agent, err := c.GetAgent(sess.AgentID)
	if err != nil {
		if coord.IsSessionStale(sess, coord.SessionStaleThreshold) {
			return true, "stale_session_missing_agent_ref"
		}
		return true, "missing_agent_ref"
	}
	if coord.IsSessionStale(sess, coord.SessionStaleThreshold) && agent.HeartbeatAt.Before(time.Now().UTC().Add(-staleThreshold)) {
		return true, "stale_session_and_heartbeat"
	}
	return false, ""
}

func workonDone(out io.Writer, c *coord.Coordinator, r *repo.Repo, name string, jsonOutput bool) error {
	coordDir := filepath.Join(r.GraftDir, "coord")

	var agentID string
	var agentIDPath string

	// If name is provided, look up the per-agent file directly
	if name != "" {
		agentFileName := "agent-" + name
		p := filepath.Join(coordDir, agentFileName)
		if data, err := os.ReadFile(p); err == nil {
			agentID = string(data)
			agentIDPath = p
		}
	}

	// Otherwise scan for any per-agent file
	if agentID == "" {
		entries, _ := os.ReadDir(coordDir)
		for _, e := range entries {
			if e.IsDir() || e.Name() == "agent-id" {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(coordDir, e.Name())); err == nil {
				agentID = string(data)
				agentIDPath = filepath.Join(coordDir, e.Name())
				break
			}
		}
	}

	// Fall back to legacy single agent-id file
	if agentID == "" {
		legacyPath := filepath.Join(coordDir, "agent-id")
		data, err := os.ReadFile(legacyPath)
		if err != nil {
			return fmt.Errorf("no active coordination session found")
		}
		agentID = string(data)
		agentIDPath = legacyPath
	}

	agent, err := c.GetAgent(agentID)
	if err != nil {
		// Agent already gone; clean up local state
		_ = os.Remove(agentIDPath)
		if agentIDPath != filepath.Join(coordDir, "agent-id") {
			_ = os.Remove(filepath.Join(coordDir, "agent-id"))
		}
		if jsonOutput {
			return writeJSON(out, workonResult{Status: "already_done"})
		}
		fmt.Fprintln(out, "No active coordination session found.")
		return nil
	}

	agentName := agent.Name

	// Ownership check: if the owning process is still alive, block release
	// from a different process. This prevents one agent from accidentally
	// deregistering another active agent. Dead processes are allowed to be
	// cleaned up by any caller.
	if name != "" {
		if sess, _ := coord.LoadSession(r.GraftDir, name); sess != nil && sess.PID != 0 {
			callerPID := os.Getpid()
			if sess.PID != callerPID && isProcessAlive(sess.PID) {
				return fmt.Errorf("cannot release agent %q: owned by active PID %d (caller is PID %d)", name, sess.PID, callerPID)
			}
		}
	}

	if err := c.DeregisterAgent(agentID); err != nil {
		return fmt.Errorf("deregister agent: %w", err)
	}

	// Remove persistent session file
	if agentName != "" {
		_ = coord.RemoveSession(r.GraftDir, agentName)
	}

	// Clear any presence entries for this agent
	_ = coord.ClearAgentPresence(r.GraftDir, agentID)

	_ = os.Remove(agentIDPath)
	// Also clean legacy file if this was the last agent
	_ = os.Remove(filepath.Join(coordDir, "agent-id"))

	if jsonOutput {
		return writeJSON(out, workonResult{
			Status:    "left",
			AgentID:   agentID,
			AgentName: agentName,
		})
	}

	fmt.Fprintf(out, "Coordination session ended for %s (%s)\n", agentName, agentID)
	fmt.Fprintln(out, "All claims released.")
	return nil
}

// isProcessAlive checks whether a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 tests process existence without sending a real signal.
	return proc.Signal(syscall.Signal(0)) == nil
}

type workonResult = JSONWorkonOutput
