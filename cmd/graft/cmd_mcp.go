package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

const (
	mcpServerName      = "graft"
	mcpServerVersion   = "0.2.0"
	mcpProtocolVersion = "2024-11-05"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server for AI agent integration",
	}

	cmd.AddCommand(newMCPServeCmd())
	return cmd
}

func newMCPServeCmd() *cobra.Command {
	var withCodeintel bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start MCP JSON-RPC server over stdio",
		Long: `Start a JSON-RPC 2.0 MCP server over stdio with Content-Length framing.
Exposes graft coordination tools for AI agent integration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			server := newMCPServer(os.Stdin, os.Stdout, os.Stderr)
			server.withCodeintel = withCodeintel
			return server.run()
		},
	}

	cmd.Flags().BoolVar(&withCodeintel, "with-codeintel", false, "enable tree-sitter code intelligence tools (entities, symbols, references, exports, callers)")
	return cmd
}

// --- JSON-RPC types ---

type mcpRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpToolsListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpToolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type mcpToolCallResult struct {
	Content []mcpToolContent `json:"content,omitempty"`
	IsError bool             `json:"isError,omitempty"`
	Meta    map[string]any   `json:"_meta,omitempty"`
}

type mcpToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// --- Schema helpers ---

type mcpSchema struct {
	Properties map[string]mcpProperty
	Required   []string
}

type mcpProperty struct {
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

func (s mcpSchema) toMap() map[string]any {
	result := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for name, prop := range s.Properties {
			m := map[string]any{}
			if prop.Type != "" {
				m["type"] = prop.Type
			}
			if prop.Description != "" {
				m["description"] = prop.Description
			}
			props[name] = m
		}
		result["properties"] = props
	}
	if len(s.Required) > 0 {
		sorted := make([]string, len(s.Required))
		copy(sorted, s.Required)
		sort.Strings(sorted)
		result["required"] = sorted
	}
	return result
}

func mcpVersionedMap(fields map[string]any) map[string]any {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["schemaVersion"] = JSONSchemaVersion
	return fields
}

// --- MCP Server ---

type mcpServer struct {
	reader        *bufio.Reader
	writer        io.Writer
	log           *log.Logger
	outMu         sync.Mutex
	withCodeintel bool
	activity      *mcpActivityAccumulator
}

func newMCPServer(in io.Reader, out io.Writer, logOut io.Writer) *mcpServer {
	return &mcpServer{
		reader: bufio.NewReader(in),
		writer: out,
		log:    log.New(logOut, "graft-mcp: ", log.LstdFlags),
	}
}

func (s *mcpServer) run() error {
	for {
		payload, err := mcpReadFramedMessage(s.reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var request mcpRPCRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			_ = s.sendError(json.RawMessage("null"), -32700, "parse error")
			continue
		}
		if strings.TrimSpace(request.Method) == "" {
			_ = s.sendError(request.ID, -32600, "invalid request: method is required")
			continue
		}

		// Notification path (no ID) -- except exit which stops server.
		if len(bytes.TrimSpace(request.ID)) == 0 || string(bytes.TrimSpace(request.ID)) == "null" {
			if request.Method == "exit" {
				return nil
			}
			continue
		}

		if request.Method == "exit" {
			_ = s.sendResult(request.ID, map[string]any{})
			return nil
		}

		result, rpcErr := s.handleRequest(request)
		if rpcErr != nil {
			if err := s.sendError(request.ID, rpcErr.Code, rpcErr.Message); err != nil {
				return err
			}
			continue
		}
		if err := s.sendResult(request.ID, result); err != nil {
			return err
		}
	}
}

func (s *mcpServer) handleRequest(request mcpRPCRequest) (any, *mcpRPCError) {
	switch request.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    mcpServerName,
				"version": mcpServerVersion,
			},
		}, nil
	case "initialized":
		return map[string]any{}, nil
	case "shutdown":
		return map[string]any{}, nil
	case "tools/list":
		tools := mcpToolDefs()
		tools = append(tools, mcpExecToolDefs()...)
		tools = append(tools, mcpSpawnToolDefs()...)
		tools = append(tools, mcpPlanToolDefs()...)
		tools = append(tools, mcpTaskToolDefs()...)
		if s.withCodeintel {
			tools = append(tools, mcpCodeintelToolDefs()...)
			tools = append(tools, mcpGrepToolDefs()...)
		}
		sort.Slice(tools, func(i, j int) bool {
			return tools[i].Name < tools[j].Name
		})
		return mcpToolsListResult{Tools: tools}, nil
	case "tools/call":
		var params mcpToolsCallParams
		if err := mcpDecodeParams(request.Params, &params); err != nil {
			return nil, &mcpRPCError{Code: -32602, Message: err.Error()}
		}
		if strings.TrimSpace(params.Name) == "" {
			return nil, &mcpRPCError{Code: -32602, Message: "missing tool name"}
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}

		// Lazily initialize the activity accumulator when an active agent is detected.
		if s.activity == nil {
			if r, err := openRepo("."); err == nil {
				if activeID := readActiveAgentID(r); activeID != "" {
					agentName := activeID
					if c := coord.New(r, coord.DefaultConfig); c != nil {
						if agent, err := c.GetAgent(activeID); err == nil {
							agentName = agent.Name
						}
					}
					s.activity = newMCPActivityAccumulator(activeID, agentName)
				}
			}
		}

		started := time.Now()
		result, err := mcpDispatchAll(s.withCodeintel, params.Name, params.Arguments)
		durationMs := time.Since(started).Milliseconds()

		// Record tool call in accumulator.
		if s.activity != nil {
			s.activity.recordToolCall(params.Name)
		}

		meta := map[string]any{
			"tool":        params.Name,
			"duration_ms": durationMs,
		}

		// Build coord summary for _meta.
		coordSummary := mcpBuildCoordSummary(s.activity)
		if coordSummary != nil {
			meta["coord"] = coordSummary
		}

		// Publish activity digest if accumulator is ready.
		if s.activity != nil && s.activity.shouldPublish() {
			if r, err := openRepo("."); err == nil {
				c := coord.New(r, coord.DefaultConfig)
				c.AgentID = s.activity.agentID
				digest := s.activity.buildDigest()
				_ = c.PublishDigestToFeed(digest)
				_ = c.Heartbeat(s.activity.agentID)
				_, _ = coord.TouchSessionByAgentID(r.GraftDir, s.activity.agentID)
				s.activity.reset()
			}
		}

		if err != nil {
			meta["ok"] = false
			return mcpToolCallResult{
				IsError: true,
				Content: []mcpToolContent{
					{Type: "text", Text: err.Error()},
				},
				Meta: meta,
			}, nil
		}

		meta["ok"] = true
		encoded, encodeErr := json.MarshalIndent(result, "", "  ")
		if encodeErr != nil {
			encoded = []byte(`{"error":"failed to encode result"}`)
		}
		return mcpToolCallResult{
			Content: []mcpToolContent{
				{Type: "text", Text: string(encoded)},
			},
			Meta: meta,
		}, nil

	default:
		return nil, &mcpRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", request.Method)}
	}
}

func mcpDecodeParams(raw json.RawMessage, out any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (s *mcpServer) sendResult(id json.RawMessage, result any) error {
	return s.sendResponse(mcpRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *mcpServer) sendError(id json.RawMessage, code int, message string) error {
	return s.sendResponse(mcpRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &mcpRPCError{Code: code, Message: message},
	})
}

func (s *mcpServer) sendResponse(response mcpRPCResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return s.writeFramed(payload)
}

func (s *mcpServer) writeFramed(payload []byte) error {
	s.outMu.Lock()
	defer s.outMu.Unlock()

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(s.writer, header); err != nil {
		return err
	}
	_, err := s.writer.Write(payload)
	return err
}

func mcpReadFramedMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" {
				return nil, io.EOF
			}
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		if key != "content-length" {
			continue
		}
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid Content-Length %q", value)
		}
		contentLength = parsed
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// --- Tool definitions ---

func mcpToolDefs() []mcpTool {
	tools := []mcpTool{
		{
			Name:        "graft_workon",
			Description: "Join a coordination session as a named agent. Registers the agent and acquires entity claims as files are edited.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"name":          {Type: "string", Description: "agent name (required)"},
					"auto_discover": {Type: "boolean", Description: "discover workspaces from go.mod"},
					"notify":        {Type: "string", Description: "notification filter: all or breaking (default: all)"},
					"conflict_mode": {Type: "string", Description: "conflict mode: advisory, soft_block, hard_block"},
					"watch_only":    {Type: "boolean", Description: "observe only, don't claim entities"},
					"scope":         {Type: "string", Description: "limit coordination to package pattern"},
					"recover":       {Type: "boolean", Description: "replace a stale or missing local agent identity"},
				},
				Required: []string{"name"},
			}.toMap(),
		},
		{
			Name:        "graft_workon_done",
			Description: "Leave the current coordination session. Deregisters the agent and releases all claims.",
			InputSchema: mcpSchema{}.toMap(),
		},
		{
			Name:        "graft_coord_status",
			Description: "Show coordination dashboard: agent count, claims, conflicts, and feed summary.",
			InputSchema: mcpSchema{}.toMap(),
		},
		{
			Name:        "graft_coord_agents",
			Description: "List all registered coordination agents with their workspace, host, and heartbeat info.",
			InputSchema: mcpSchema{}.toMap(),
		},
		{
			Name:        "graft_coord_claims",
			Description: "List all active entity claims with agent, mode, and file info.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"workspace": {Type: "string", Description: "filter claims by workspace name"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_coord_feed",
			Description: "Read coordination feed events (claim changes, commits, impact notifications).",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"since": {Type: "string", Description: "show events after this feed hash"},
					"mine":  {Type: "boolean", Description: "show only events from the active agent"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_coord_impact",
			Description: "Run cross-workspace impact analysis for entity changes. Shows which downstream workspaces and agents are affected.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"entities": {Type: "string", Description: "comma-separated entity keys to analyze (optional; uses recent feed if omitted)"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_coord_check",
			Description: "Quick conflict check optimized for hook integration. Returns whether any other agents hold editing claims that conflict with the active agent.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"stale_after_seconds": {Type: "integer", Description: "agent heartbeat age in seconds before reporting stale"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_coord_cleanup_stale",
			Description: "Inspect or remove stale coordination agents and their claims.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"dry_run":             {Type: "boolean", Description: "show stale agents without removing them"},
					"stale_after_seconds": {Type: "integer", Description: "agent heartbeat age in seconds before removing stale agents"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_coord_diff",
			Description: "Show another agent's claimed entities and info.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"agent_id": {Type: "string", Description: "target agent ID (required)"},
				},
				Required: []string{"agent_id"},
			}.toMap(),
		},
		{
			Name:        "graft_coord_xrefs",
			Description: "Reverse call lookup for a qualified symbol name. Shows all call sites that reference the symbol.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"name": {Type: "string", Description: "qualified symbol name (required)"},
				},
				Required: []string{"name"},
			}.toMap(),
		},
		{
			Name:        "graft_coord_graph",
			Description: "Show workspace dependency graph built from go.mod dependencies.",
			InputSchema: mcpSchema{}.toMap(),
		},
		{
			Name:        "graft_coord_watch",
			Description: "Add a watch claim on an entity. Watches receive notifications when the entity is modified by other agents.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"entity_key": {Type: "string", Description: "entity key to watch (required)"},
				},
				Required: []string{"entity_key"},
			}.toMap(),
		},
		{
			Name:        "graft_coord_unwatch",
			Description: "Remove a watch claim from an entity.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"entity_key": {Type: "string", Description: "entity key to unwatch (required)"},
				},
				Required: []string{"entity_key"},
			}.toMap(),
		},
		{
			Name:        "graft_coord_resolve",
			Description: "Release or transfer a claim. Use to resolve conflicts or hand off entities to another agent.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"key_hash": {Type: "string", Description: "entity key hash (required)"},
					"transfer": {Type: "string", Description: "agent ID to transfer the claim to (optional; releases if omitted)"},
				},
				Required: []string{"key_hash"},
			}.toMap(),
		},
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return tools
}

// --- Tool dispatch ---

func mcpDispatchTool(name string, args map[string]any) (any, error) {
	switch name {
	case "graft_workon":
		return mcpToolWorkon(args)
	case "graft_workon_done":
		return mcpToolWorkonDone(args)
	case "graft_coord_status":
		return mcpToolCoordStatus(args)
	case "graft_coord_agents":
		return mcpToolCoordAgents(args)
	case "graft_coord_claims":
		return mcpToolCoordClaims(args)
	case "graft_coord_feed":
		return mcpToolCoordFeed(args)
	case "graft_coord_impact":
		return mcpToolCoordImpact(args)
	case "graft_coord_check":
		return mcpToolCoordCheck(args)
	case "graft_coord_cleanup_stale":
		return mcpToolCoordCleanupStale(args)
	case "graft_coord_diff":
		return mcpToolCoordDiff(args)
	case "graft_coord_xrefs":
		return mcpToolCoordXrefs(args)
	case "graft_coord_graph":
		return mcpToolCoordGraph(args)
	case "graft_coord_watch":
		return mcpToolCoordWatch(args)
	case "graft_coord_unwatch":
		return mcpToolCoordUnwatch(args)
	case "graft_coord_resolve":
		return mcpToolCoordResolve(args)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

// mcpDispatchAll routes a tool call to the correct dispatcher.
func mcpDispatchAll(withCodeintel bool, name string, args map[string]any) (any, error) {
	// Route by prefix to avoid fragile error-string matching.
	switch {
	case strings.HasPrefix(name, "graft_plan_"):
		return mcpDispatchPlanTool(name, args)
	case strings.HasPrefix(name, "graft_task_"):
		return mcpDispatchTaskTool(name, args)
	case strings.HasPrefix(name, "graft_exec"):
		return mcpDispatchExecTool(name, args)
	case strings.HasPrefix(name, "graft_spawn"):
		return mcpDispatchSpawnTool(name, args)
	case strings.HasPrefix(name, "graft_ci_"):
		if !withCodeintel {
			return nil, fmt.Errorf("unknown tool %q (code intelligence tools require --with-codeintel)", name)
		}
		return mcpDispatchCodeintelTool(name, args)
	case strings.HasPrefix(name, "graft_grep"), name == "graft_entity_edit":
		if !withCodeintel {
			return nil, fmt.Errorf("unknown tool %q (structural grep tools require --with-codeintel)", name)
		}
		return mcpDispatchGrepTool(name, args)
	default:
		return mcpDispatchTool(name, args)
	}
}

// --- Arg helpers ---

func mcpArgString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func mcpArgBool(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if ok {
		return b
	}
	// Handle string "true"/"false" from some clients.
	s, ok := v.(string)
	if ok {
		return strings.EqualFold(s, "true")
	}
	return false
}

func mcpArgBoolDefault(args map[string]any, key string, defaultValue bool) bool {
	_, ok := args[key]
	if !ok {
		return defaultValue
	}
	return mcpArgBool(args, key)
}

func mcpArgInt(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch value := v.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return n
	default:
		n, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", v)))
		return n
	}
}

func mcpArgOptionalInt(args map[string]any, key string) (int, bool, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false, nil
	}
	switch value := v.(type) {
	case int:
		return value, true, nil
	case int32:
		return int(value), true, nil
	case int64:
		return int(value), true, nil
	case float64:
		converted := int(value)
		if value != float64(converted) {
			return 0, true, fmt.Errorf("%s must be an integer", key)
		}
		return converted, true, nil
	case json.Number:
		n, err := strconv.Atoi(strings.TrimSpace(string(value)))
		if err != nil {
			return 0, true, fmt.Errorf("%s must be an integer", key)
		}
		return n, true, nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, true, fmt.Errorf("%s must be an integer", key)
		}
		return n, true, nil
	default:
		return 0, true, fmt.Errorf("%s must be an integer", key)
	}
}

func mcpApplyStaleAfter(c *coord.Coordinator, args map[string]any) error {
	seconds, ok, err := mcpArgOptionalInt(args, "stale_after_seconds")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if seconds <= 0 {
		return fmt.Errorf("stale_after_seconds must be positive")
	}
	c.Config.StaleThreshold = time.Duration(seconds) * time.Second
	return nil
}

// --- Coord summary for _meta ---

func mcpBuildCoordSummary(activity *mcpActivityAccumulator) map[string]any {
	r, err := openRepo(".")
	if err != nil {
		return nil
	}
	c := coord.New(r, coord.DefaultConfig)

	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()
	feedEvents, _ := c.WalkFeed("", 100)

	activeID := readActiveAgentID(r)

	// Count my claims.
	myClaims := 0
	for _, cl := range claims {
		if cl.Agent == activeID {
			myClaims++
		}
	}

	// Count conflicts.
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

	// Count unread feed (events not from us).
	unread := 0
	for _, ev := range feedEvents {
		if ev.AgentID != activeID {
			unread++
		}
	}

	result := map[string]any{
		"active_agents": len(agents),
		"your_claims":   myClaims,
		"conflicts":     conflictCount,
		"unread_feed":   unread,
	}

	if activity != nil {
		result["your_activity"] = activity.snapshot()
	}

	return result
}

// --- Tool implementations ---

func mcpToolWorkon(args map[string]any) (any, error) {
	name := mcpArgString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	r, err := openRepo(".")
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	cfg := coord.DefaultConfig
	conflictMode := mcpArgString(args, "conflict_mode")
	if conflictMode != "" {
		cfg.ConflictMode = conflictMode
	}
	c := coord.New(r, cfg)

	recover := mcpArgBool(args, "recover")
	watchOnly := mcpArgBool(args, "watch_only")
	scope := mcpArgString(args, "scope")
	mode := "editing"
	if watchOnly {
		mode = "watching"
	}
	hostname, _ := os.Hostname()

	var id string
	status := "joined"
	var previousAgentID string
	var recoveryReason string
	existingSession, err := coord.LoadSession(r.GraftDir, name)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if existingSession != nil {
		needsRecovery, reason := workonNeedsRecovery(c, existingSession, c.Config.StaleThreshold)
		if needsRecovery {
			if !recover {
				return nil, fmt.Errorf("coordination session for %q needs recovery (%s); call graft_workon with recover=true to replace it", name, reason)
			}
			previousAgentID = existingSession.AgentID
			recoveryReason = reason
			if previousAgentID != "" {
				c.AgentID = previousAgentID
				if err := c.DeregisterAgent(previousAgentID); err != nil {
					return nil, fmt.Errorf("recover stale agent: %w", err)
				}
				_ = coord.ClearAgentPresence(r.GraftDir, previousAgentID)
			}
			_ = coord.RemoveSession(r.GraftDir, name)
			c.AgentID = ""
			status = "recovered"
			existingSession = nil
		}
	}

	workspace := filepath.Base(r.RootDir)
	if existingSession != nil {
		id = existingSession.AgentID
		c.AgentID = id
		existingSession.PID = os.Getpid()
		existingSession.Host = hostname
		existingSession.Scope = scope
		existingSession.Mode = mode
		if err := coord.TouchSession(r.GraftDir, existingSession); err != nil {
			return nil, fmt.Errorf("touch session: %w", err)
		}
		_ = c.Heartbeat(id)
		status = "resumed"
		workspace = existingSession.Workspace
		if workspace == "" {
			workspace = filepath.Base(r.RootDir)
		}
	} else {
		info := coord.AgentInfo{
			Name:      name,
			Workspace: workspace,
			Host:      hostname,
		}

		id, err = c.RegisterAgent(info)
		if err != nil {
			return nil, fmt.Errorf("register agent: %w", err)
		}

		sess := &coord.Session{
			AgentID:    id,
			AgentName:  name,
			Workspace:  workspace,
			Host:       hostname,
			StartedAt:  c.AgentStartedAt(),
			LastActive: c.AgentStartedAt(),
			PID:        os.Getpid(),
			Scope:      scope,
			Mode:       mode,
		}
		if err := coord.SaveSession(r.GraftDir, sess); err != nil {
			return nil, fmt.Errorf("save session: %w", err)
		}
	}

	// Save agent ID.
	agentIDDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(agentIDDir, 0o755); err != nil {
		return nil, fmt.Errorf("create coord dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(agentIDDir, "agent-"+name), []byte(id), 0o644); err != nil {
		return nil, fmt.Errorf("save named agent ID: %w", err)
	}
	if err := os.WriteFile(filepath.Join(agentIDDir, "agent-id"), []byte(id), 0o644); err != nil {
		return nil, fmt.Errorf("save agent ID: %w", err)
	}

	// Auto-discover workspaces if requested.
	var discovered map[string]string
	if mcpArgBool(args, "auto_discover") {
		discovered, _ = coord.AutoDiscoverWorkspaces(r.RootDir)
		if len(discovered) > 0 {
			ucfg, err := userconfig.Load()
			if err == nil {
				if ucfg.Workspaces == nil {
					ucfg.Workspaces = make(map[string]string)
				}
				for wsName, wsPath := range discovered {
					if _, exists := ucfg.Workspaces[wsName]; !exists {
						ucfg.Workspaces[wsName] = wsPath
					}
				}
				_ = userconfig.Save(ucfg)
			}
		}
	}

	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()

	result := JSONWorkonOutput{
		SchemaVersion: JSONSchemaVersion,
		Status:        status,
		AgentID:       id,
		AgentName:     name,
		Workspace:     workspace,
		Mode:          mode,
		Scope:         scope,
		Agents:        len(agents),
		Claims:        len(claims),
	}
	if previousAgentID != "" {
		result.Recovered = true
		result.PreviousAgentID = previousAgentID
		result.RecoveryReason = recoveryReason
	}
	if len(discovered) > 0 {
		result.Discovered = discovered
	}
	return result, nil
}

func mcpToolWorkonDone(_ map[string]any) (any, error) {
	r, err := openRepo(".")
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	c := coord.New(r, coord.DefaultConfig)

	agentIDPath := filepath.Join(r.GraftDir, "coord", "agent-id")
	data, err := os.ReadFile(agentIDPath)
	if err != nil {
		return JSONWorkonOutput{SchemaVersion: JSONSchemaVersion, Status: "already_done"}, nil
	}
	agentID := strings.TrimSpace(string(data))

	agent, err := c.GetAgent(agentID)
	if err != nil {
		_ = os.Remove(agentIDPath)
		return JSONWorkonOutput{SchemaVersion: JSONSchemaVersion, Status: "already_done"}, nil
	}

	agentName := agent.Name
	if err := c.DeregisterAgent(agentID); err != nil {
		return nil, fmt.Errorf("deregister agent: %w", err)
	}

	_ = os.Remove(agentIDPath)

	return JSONWorkonOutput{
		SchemaVersion: JSONSchemaVersion,
		Status:        "left",
		AgentID:       agentID,
		AgentName:     agentName,
	}, nil
}

func mcpToolCoordStatus(_ map[string]any) (any, error) {
	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	result := coordStatusSummary(c, r)
	result.SchemaVersion = JSONSchemaVersion
	return result, nil
}

func mcpToolCoordAgents(_ map[string]any) (any, error) {
	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	agents, err := c.ListAgents()
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	if agents == nil {
		agents = []coord.AgentInfo{}
	}
	return JSONCoordAgentsOutput{SchemaVersion: JSONSchemaVersion, Agents: agents}, nil
}

func mcpToolCoordClaims(args map[string]any) (any, error) {
	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	claims, err := c.ListClaims()
	if err != nil {
		return nil, fmt.Errorf("list claims: %w", err)
	}

	workspace := mcpArgString(args, "workspace")
	if workspace != "" {
		var filtered []coord.ClaimInfo
		for _, cl := range claims {
			if strings.Contains(cl.AgentName, workspace) || strings.Contains(cl.File, workspace) {
				filtered = append(filtered, cl)
			}
		}
		claims = filtered
	}

	if claims == nil {
		claims = []coord.ClaimInfo{}
	}
	jsonClaims := make([]JSONCoordClaim, 0, len(claims))
	for _, claim := range claims {
		jsonClaims = append(jsonClaims, coordClaimToJSON(claim, ""))
	}
	return JSONCoordClaimsOutput{SchemaVersion: JSONSchemaVersion, Claims: jsonClaims}, nil
}

func mcpToolCoordFeed(args map[string]any) (any, error) {
	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	since := mcpArgString(args, "since")
	events, err := c.WalkFeed(since, 50)
	if err != nil {
		return nil, fmt.Errorf("walk feed: %w", err)
	}

	if mcpArgBool(args, "mine") {
		activeID := readActiveAgentID(r)
		if activeID != "" {
			var filtered []coord.FeedEvent
			for _, ev := range events {
				if ev.AgentID == activeID {
					filtered = append(filtered, ev)
				}
			}
			events = filtered
		}
	}

	if events == nil {
		events = []coord.FeedEvent{}
	}
	jsonEvents := make([]JSONCoordFeedEntry, 0, len(events))
	for _, event := range events {
		jsonEvents = append(jsonEvents, coordFeedEventToJSON(event, ""))
	}
	return JSONCoordFeedOutput{SchemaVersion: JSONSchemaVersion, Events: jsonEvents}, nil
}

func mcpToolCoordImpact(args map[string]any) (any, error) {
	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	cfg, _ := userconfig.Load()
	workspaces := make(map[string]string)
	if cfg != nil && cfg.Workspaces != nil {
		workspaces = cfg.Workspaces
	}

	var changes []coord.EntityChange

	entitiesStr := mcpArgString(args, "entities")
	if entitiesStr != "" {
		for _, key := range strings.Split(entitiesStr, ",") {
			key = strings.TrimSpace(key)
			if key != "" {
				changes = append(changes, coord.EntityChange{
					Key:    key,
					Change: "unknown",
				})
			}
		}
	} else {
		events, _ := c.WalkFeed("", 10)
		for _, ev := range events {
			changes = append(changes, ev.Entities...)
		}
	}

	if len(changes) == 0 {
		return JSONCoordImpactOutput{SchemaVersion: JSONSchemaVersion}, nil
	}

	report, err := c.AnalyzeImpact(changes, workspaces)
	if err != nil {
		return nil, fmt.Errorf("analyze impact: %w", err)
	}
	result := coordImpactReportToJSON(report)
	result.SchemaVersion = JSONSchemaVersion
	return result, nil
}

func mcpToolCoordCheck(args map[string]any) (any, error) {
	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}
	if err := mcpApplyStaleAfter(c, args); err != nil {
		return nil, err
	}

	activeID := readActiveAgentID(r)
	claims, _ := c.ListClaims()
	agents, _ := c.ListAgents()
	activeClaims, staleAgents := coordCheckClaimAndAgentSummary(c, claims, agents)
	unreadFeedEvents := coordCheckUnreadFeedEvents(c, activeID, 20)

	var conflicts []JSONCoordCheckConflict
	if activeID != "" {
		for _, cl := range claims {
			if cl.Agent != activeID && cl.Mode == coord.ClaimEditing {
				req := coord.ClaimRequest{
					EntityKey: cl.EntityKey,
					File:      cl.File,
					Mode:      coord.ClaimEditing,
				}
				ctx, decisionErr := c.InspectClaimDecisionWithExisting(activeID, req, &cl)
				if decisionErr != nil {
					return nil, fmt.Errorf("evaluate claim decision: %w", decisionErr)
				}
				recordCoordDecision(c, io.Discard, "graft mcp coord.check", activeID, req, ctx, coord.DecisionOutcome{
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

	var readers []JSONCoordCheckReader
	if presence, perr := c.ListPresence(); perr == nil {
		for _, e := range coord.OtherAgentPresence(presence, activeID) {
			readers = append(readers, JSONCoordCheckReader{AgentName: e.AgentName, File: e.File, Entity: e.Entity})
		}
	}

	return JSONCoordCheckOutput{
		SchemaVersion:    JSONSchemaVersion,
		OK:               len(conflicts) == 0,
		ActiveAgentID:    activeID,
		AgentsExamined:   len(agents),
		ClaimsExamined:   len(claims),
		ActiveClaims:     activeClaims,
		StaleAgents:      staleAgents,
		UnreadFeedEvents: unreadFeedEvents,
		Conflicts:        conflicts,
		Readers:          readers,
	}, nil
}

func mcpToolCoordCleanupStale(args map[string]any) (any, error) {
	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}
	if err := mcpApplyStaleAfter(c, args); err != nil {
		return nil, err
	}

	agents, err := c.ListAgents()
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	staleAgents := coordStaleAgentSummaries(c, agents)
	dryRun := mcpArgBool(args, "dry_run")
	removed := 0
	if !dryRun {
		removedAgents, err := c.GCStaleAgents()
		if err != nil {
			return nil, fmt.Errorf("cleanup stale agents: %w", err)
		}
		removed = len(removedAgents)
		staleAgents = coordStaleAgentSummaries(c, removedAgents)
	}
	if staleAgents == nil {
		staleAgents = []JSONCoordCheckAgent{}
	}
	return JSONCoordCleanupStaleOutput{
		SchemaVersion: JSONSchemaVersion,
		OK:            true,
		DryRun:        dryRun,
		Removed:       removed,
		StaleAgents:   staleAgents,
	}, nil
}

func mcpToolCoordDiff(args map[string]any) (any, error) {
	agentID := mcpArgString(args, "agent_id")
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	agent, err := c.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}

	claims, _ := c.ListClaims()
	var agentClaims []coord.ClaimInfo
	for _, cl := range claims {
		if cl.Agent == agentID {
			agentClaims = append(agentClaims, cl)
		}
	}
	if agentClaims == nil {
		agentClaims = []coord.ClaimInfo{}
	}

	return JSONCoordDiffOutput{
		SchemaVersion: JSONSchemaVersion,
		Agent:         agent,
		Claims:        agentClaims,
	}, nil
}

func mcpToolCoordXrefs(args map[string]any) (any, error) {
	name := mcpArgString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	idx, err := c.LoadXrefIndex()
	if err != nil {
		// Try building a fresh one.
		modulePath := ""
		gomodPath := filepath.Join(c.Repo.RootDir, "go.mod")
		if deps, parseErr := coord.ParseGoModDeps(gomodPath); parseErr == nil {
			modulePath = deps.Module
		}
		idx, err = coord.BuildXrefIndex(c.Repo.RootDir, modulePath)
		if err != nil {
			return nil, fmt.Errorf("build xref index: %w", err)
		}
	}

	sites, ok := idx.Refs[name]
	if !ok {
		return JSONCoordXrefsOutput{SchemaVersion: JSONSchemaVersion, References: []coord.XrefCallSite{}}, nil
	}
	return JSONCoordXrefsOutput{SchemaVersion: JSONSchemaVersion, References: sites}, nil
}

func mcpToolCoordGraph(_ map[string]any) (any, error) {
	cfg, err := userconfig.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if cfg.Workspaces == nil || len(cfg.Workspaces) == 0 {
		return JSONCoordGraphOutput{
			SchemaVersion: JSONSchemaVersion,
			Workspaces:    map[string]string{},
			Edges:         []JSONCoordGraphEdge{},
		}, nil
	}

	graph, err := coord.BuildWorkspaceGraph(cfg.Workspaces)
	if err != nil {
		return nil, fmt.Errorf("build workspace graph: %w", err)
	}

	var edges []JSONCoordGraphEdge
	for wsName := range cfg.Workspaces {
		deps := graph.DependentsOf(wsName)
		for _, dep := range deps {
			edges = append(edges, JSONCoordGraphEdge{From: dep, To: wsName})
		}
	}
	if edges == nil {
		edges = []JSONCoordGraphEdge{}
	}

	return JSONCoordGraphOutput{
		SchemaVersion: JSONSchemaVersion,
		Workspaces:    cfg.Workspaces,
		Edges:         edges,
	}, nil
}

func mcpToolCoordWatch(args map[string]any) (any, error) {
	entityKey := mcpArgString(args, "entity_key")
	if entityKey == "" {
		return nil, fmt.Errorf("entity_key is required")
	}

	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	activeID := readActiveAgentID(r)
	if activeID == "" {
		return nil, fmt.Errorf("no active coordination session; use graft_workon first")
	}

	err = c.AcquireClaim(activeID, coord.ClaimRequest{
		EntityKey: entityKey,
		File:      "",
		Mode:      coord.ClaimWatching,
	})
	if err != nil {
		return nil, fmt.Errorf("watch: %w", err)
	}

	return JSONCoordWatchOutput{
		SchemaVersion: JSONSchemaVersion,
		Status:        "watching",
		EntityKey:     entityKey,
	}, nil
}

func mcpToolCoordUnwatch(args map[string]any) (any, error) {
	entityKey := mcpArgString(args, "entity_key")
	if entityKey == "" {
		return nil, fmt.Errorf("entity_key is required")
	}

	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	activeID := readActiveAgentID(r)
	if activeID == "" {
		return nil, fmt.Errorf("no active coordination session; use graft_workon first")
	}

	keyHash := coord.EntityKeyHash(entityKey)
	if err := c.ReleaseWatch(keyHash, activeID); err != nil {
		return nil, fmt.Errorf("unwatch: %w", err)
	}

	return JSONCoordUnwatchOutput{
		SchemaVersion: JSONSchemaVersion,
		Status:        "unwatched",
		EntityKey:     entityKey,
	}, nil
}

func mcpToolCoordResolve(args map[string]any) (any, error) {
	keyHash := mcpArgString(args, "key_hash")
	if keyHash == "" {
		return nil, fmt.Errorf("key_hash is required")
	}

	transferTo := mcpArgString(args, "transfer")

	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	if transferTo != "" {
		activeID := readActiveAgentID(r)
		if activeID == "" {
			return nil, fmt.Errorf("no active session for transfer source")
		}
		if err := c.TransferClaim(keyHash, activeID, transferTo); err != nil {
			return nil, fmt.Errorf("transfer: %w", err)
		}
		return JSONCoordResolveOutput{
			SchemaVersion: JSONSchemaVersion,
			Status:        "transferred",
			KeyHash:       keyHash,
			ToAgent:       transferTo,
		}, nil
	}

	if err := c.ReleaseClaim(keyHash); err != nil {
		return nil, fmt.Errorf("release: %w", err)
	}

	return JSONCoordResolveOutput{
		SchemaVersion: JSONSchemaVersion,
		Status:        "released",
		KeyHash:       keyHash,
	}, nil
}
