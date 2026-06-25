package coord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
)

// Coordinator manages agent coordination for a graft repository.
type Coordinator struct {
	Repo    *repo.Repo
	AgentID string
	Config  CoordinatorConfig
}

type CoordinatorConfig struct {
	HeartbeatInterval time.Duration
	StaleThreshold    time.Duration
	ConflictMode      string // "advisory", "soft_block", "hard_block"
	AutoPushCoord     bool
}

var DefaultConfig = CoordinatorConfig{
	HeartbeatInterval: 30 * time.Second,
	StaleThreshold:    120 * time.Second,
	ConflictMode:      "advisory",
}

// New creates a Coordinator for the given repo.
func New(r *repo.Repo, cfg CoordinatorConfig) *Coordinator {
	return &Coordinator{Repo: r, Config: cfg}
}

// refPath returns the full ref name for a coord sub-namespace.
func refPath(parts ...string) string {
	path := "refs/coord"
	for _, p := range parts {
		path += "/" + p
	}
	return path
}

// writeJSONBlob serializes v to JSON, writes as a blob, returns the hash.
func (c *Coordinator) writeJSONBlob(v any) (object.Hash, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return c.Repo.Store.WriteBlob(&object.Blob{Data: data})
}

// readJSONBlob reads a blob by hash and unmarshals JSON into v.
func (c *Coordinator) readJSONBlob(h object.Hash, v any) error {
	blob, err := c.Repo.Store.ReadBlob(h)
	if err != nil {
		return fmt.Errorf("read blob %s: %w", h, err)
	}
	return json.Unmarshal(blob.Data, v)
}

// ShouldAutoPush returns true when coord refs should be pushed after mutations.
func (c *Coordinator) ShouldAutoPush() bool {
	return c.Config.AutoPushCoord
}

// collectCoordPushRoots returns every object hash that must be pushed to fully
// transfer the coord state named by coordRefs. Most coord families store one
// self-contained blob per ref (the tip IS the whole object). The feed is the
// exception: it is a hash-linked chain whose Parent pointers live inside the
// blob body and are invisible to graft's object-graph traversal
// (pkg/remote referencedHashes returns nil for TypeBlob), so we walk the chain
// explicitly. Without this a peer receives the head pointing at a parent it
// never got and WalkFeed silently truncates to a single event.
func (c *Coordinator) collectCoordPushRoots(coordRefs map[string]object.Hash) []object.Hash {
	seen := make(map[object.Hash]bool, len(coordRefs))
	var roots []object.Hash
	add := func(h object.Hash) {
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		roots = append(roots, h)
	}

	for name, tip := range coordRefs {
		add(tip)
		// The feed head names a chain; walk head -> parent so all history transfers.
		if "refs/"+name == feedHeadRef {
			cur := tip
			for cur != "" {
				add(cur)
				blob, err := c.Repo.Store.ReadBlob(cur)
				if err != nil {
					break
				}
				var entry FeedEntry
				if err := json.Unmarshal(blob.Data, &entry); err != nil {
					break
				}
				cur = object.Hash(entry.Parent)
			}
		}
	}
	return roots
}

// PushCoordRefs pushes refs/coord/ to all configured remotes.
// Called after AppendFeed and OnCommit when AutoPushCoord is enabled.
// Uses the repo config to discover remotes and iterates coord refs.
func (c *Coordinator) PushCoordRefs() error {
	cfg, err := c.Repo.ReadConfig()
	if err != nil || len(cfg.Remotes) == 0 {
		return nil // no remotes configured
	}
	coordRefs, err := c.Repo.ListRefs("coord")
	if err != nil || len(coordRefs) == 0 {
		return nil // no coord refs to push
	}
	var lastErr error
	for name, remoteURL := range cfg.Remotes {
		if err := c.pushCoordRefsToRemote(coordRefs, remoteURL); err != nil {
			lastErr = fmt.Errorf("push coord refs to %q: %w", name, err)
		}
	}
	return lastErr
}

// pushCoordRefsToRemote pushes the full coord object closure (the feed chain
// included, via collectCoordPushRoots) to one remote, then CAS-updates each
// coord ref there. Refs update individually so a single stale ref does not fail
// the whole set, and each retries on a remote CAS conflict.
func (c *Coordinator) pushCoordRefsToRemote(coordRefs map[string]object.Hash, remoteURL string) error {
	ctx := context.Background()
	client, err := remote.NewClient(remoteURL)
	if err != nil {
		return err
	}

	roots := c.collectCoordPushRoots(coordRefs)
	objs := make([]remote.ObjectRecord, 0, len(roots))
	for _, h := range roots {
		typ, data, err := c.Repo.Store.Read(h)
		if err != nil {
			return fmt.Errorf("read coord object %s: %w", h, err)
		}
		objs = append(objs, remote.ObjectRecord{Hash: h, Type: typ, Data: data})
	}
	if err := client.PushObjects(ctx, objs); err != nil {
		return err
	}

	for refName, newHash := range coordRefs {
		if err := casUpdateRemoteRef(ctx, client, refName, newHash); err != nil {
			return err
		}
	}
	return nil
}

// casUpdateRemoteRef sets refName to newHash on the remote via compare-and-swap
// against the remote's current value, retrying on a remote CAS conflict (e.g.
// another agent advanced coord/feed/head concurrently).
func casUpdateRemoteRef(ctx context.Context, client *remote.Client, refName string, newHash object.Hash) error {
	var lastErr error
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		remoteRefs, err := client.ListRefs(ctx)
		if err != nil {
			return err
		}
		old := remoteRefs[refName] // "" if absent
		if old == newHash {
			return nil // already current
		}
		nh := newHash
		if _, err := client.UpdateRefs(ctx, []remote.RefUpdate{{Name: refName, Old: &old, New: &nh}}); err != nil {
			var re *remote.RemoteError
			if errors.As(err, &re) && re.Code == "ref_conflict" {
				lastErr = err
				continue // remote moved; re-read and retry
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("ref %q: exceeded %d CAS retries: %w", refName, maxCASRetries, lastErr)
}

// OnCommit runs after a successful graft commit:
// 1. Builds entity change list from committed entities (by diffing HEAD vs parent)
// 2. Runs AnalyzeImpact to determine cross-repo effects
// 3. Appends a feed event with the impact report
// 4. Releases editing claims on committed entities
func (c *Coordinator) OnCommit(commitHash object.Hash, workspaces map[string]string) error {
	if c.AgentID == "" {
		return fmt.Errorf("no active agent; call RegisterAgent first")
	}

	// Read the commit to find parent and tree
	commit, err := c.Repo.Store.ReadCommit(commitHash)
	if err != nil {
		return fmt.Errorf("read commit: %w", err)
	}

	// Diff the commit tree against parent to identify changed entities
	var changes []EntityChange
	if len(commit.Parents) > 0 {
		parentCommit, err := c.Repo.Store.ReadCommit(commit.Parents[0])
		if err == nil {
			changes = c.diffTrees(parentCommit.TreeHash, commit.TreeHash)
		}
	}
	// If no parent (initial commit), treat all entities as added
	if len(commit.Parents) == 0 {
		changes = c.treeEntities(commit.TreeHash, "entity_added")
	}

	// Run impact analysis
	var impact *ImpactReport
	if len(changes) > 0 && len(workspaces) > 0 {
		impact, _ = c.AnalyzeImpact(changes, workspaces)
	}

	// Get agent info for the feed event
	agentName := c.AgentID
	if agent, err := c.GetAgent(c.AgentID); err == nil {
		agentName = agent.Name
	}

	// Append feed event
	event := FeedEvent{
		Event:      "commit",
		AgentID:    c.AgentID,
		AgentName:  agentName,
		CommitHash: string(commitHash),
		Entities:   changes,
		Impact:     impact,
		Source:     "coord",
	}
	if err := c.AppendFeed(event); err != nil {
		return fmt.Errorf("append feed: %w", err)
	}

	// Release editing claims on committed entities
	claims, _ := c.ListClaims()
	for _, cl := range claims {
		if cl.Agent != c.AgentID || cl.Mode != ClaimEditing {
			continue
		}
		for _, change := range changes {
			if cl.EntityKey == change.Key || extractNameFromKey(cl.EntityKey) == extractNameFromKey(change.Key) {
				_ = c.ReleaseClaim(cl.EntityKeyHash)
				break
			}
		}
	}

	// Auto-push coord refs if configured
	if c.ShouldAutoPush() {
		_ = c.PushCoordRefs()
	}

	return nil
}

// PostCommitHook generates a feed event for any commit (including git/buckley commits).
// Unlike OnCommit, this does not release claims -- it only publishes the feed event.
// This enables external tooling to trigger coord feed events after non-graft commits.
func (c *Coordinator) PostCommitHook(commitHash object.Hash) error {
	if c.AgentID == "" {
		return fmt.Errorf("no active agent; call RegisterAgent first")
	}

	commit, err := c.Repo.Store.ReadCommit(commitHash)
	if err != nil {
		return fmt.Errorf("read commit: %w", err)
	}

	var changes []EntityChange
	if len(commit.Parents) > 0 {
		parentCommit, err := c.Repo.Store.ReadCommit(commit.Parents[0])
		if err == nil {
			changes = c.diffTrees(parentCommit.TreeHash, commit.TreeHash)
		}
	}
	if len(commit.Parents) == 0 {
		changes = c.treeEntities(commit.TreeHash, "entity_added")
	}

	agentName := c.AgentID
	if agent, err := c.GetAgent(c.AgentID); err == nil {
		agentName = agent.Name
	}

	event := FeedEvent{
		Event:      "commit",
		AgentID:    c.AgentID,
		AgentName:  agentName,
		CommitHash: string(commitHash),
		Entities:   changes,
		Source:     "coord",
	}

	if err := c.AppendFeed(event); err != nil {
		return fmt.Errorf("append feed: %w", err)
	}

	if c.ShouldAutoPush() {
		_ = c.PushCoordRefs()
	}

	return nil
}

// PublishToFeed publishes a state-transition event to the feed chain.
func (c *Coordinator) PublishToFeed(eventType string, detail map[string]any) error {
	if c.AgentID == "" {
		return nil
	}
	agentName := c.AgentID
	if agent, err := c.GetAgent(c.AgentID); err == nil {
		agentName = agent.Name
	}
	return c.AppendFeed(FeedEvent{
		Event:     eventType,
		AgentID:   c.AgentID,
		AgentName: agentName,
		Detail:    detail,
		Source:    "coord",
	})
}

// PublishDigestToFeed publishes an activity digest from the MCP layer.
func (c *Coordinator) PublishDigestToFeed(digest *ActivityDigest) error {
	if c.AgentID == "" {
		return nil
	}
	agentName := c.AgentID
	if agent, err := c.GetAgent(c.AgentID); err == nil {
		agentName = agent.Name
	}
	return c.AppendFeed(FeedEvent{
		Event:     "activity_digest",
		AgentID:   c.AgentID,
		AgentName: agentName,
		Digest:    digest,
		Source:    "mcp",
	})
}

// diffTrees compares two tree hashes and returns entity changes.
// This uses the repo's FlattenTree to get file-level diffs and
// infers entity changes from changed Go files.
func (c *Coordinator) diffTrees(oldTree, newTree object.Hash) []EntityChange {
	oldEntries, err := c.Repo.FlattenTree(oldTree)
	if err != nil {
		return nil
	}
	newEntries, err := c.Repo.FlattenTree(newTree)
	if err != nil {
		return nil
	}

	// Build maps keyed by path
	oldMap := make(map[string]object.Hash)
	for _, e := range oldEntries {
		oldMap[e.Path] = e.BlobHash
	}
	newMap := make(map[string]object.Hash)
	for _, e := range newEntries {
		newMap[e.Path] = e.BlobHash
	}

	var changes []EntityChange

	// Check for changed and added files
	for path, newHash := range newMap {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		oldHash, existed := oldMap[path]
		if !existed {
			changes = append(changes, EntityChange{
				Key:    "file:" + path,
				File:   path,
				Change: "entity_added",
			})
		} else if oldHash != newHash {
			changes = append(changes, EntityChange{
				Key:    "file:" + path,
				File:   path,
				Change: "body_changed",
			})
		}
	}

	// Check for removed files
	for path := range oldMap {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		if _, exists := newMap[path]; !exists {
			changes = append(changes, EntityChange{
				Key:    "file:" + path,
				File:   path,
				Change: "entity_removed",
			})
		}
	}

	return changes
}

// treeEntities returns all Go files in a tree as entity changes.
func (c *Coordinator) treeEntities(treeHash object.Hash, changeType string) []EntityChange {
	entries, err := c.Repo.FlattenTree(treeHash)
	if err != nil {
		return nil
	}

	var changes []EntityChange
	for _, e := range entries {
		if !strings.HasSuffix(e.Path, ".go") || strings.HasSuffix(e.Path, "_test.go") {
			continue
		}
		changes = append(changes, EntityChange{
			Key:    "file:" + e.Path,
			File:   e.Path,
			Change: changeType,
		})
	}
	return changes
}
