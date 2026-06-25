package merge

import (
	"context"
)

// AIResolver is an optional, NOT-YET-WIRED extension point for resolving
// entity-level merge conflicts via an external AI service. It currently has no
// callers in graft: MergeFiles does not invoke any AI resolver and there is no
// LLM in the merge path. A future implementation would live outside graft (e.g.
// in Orchard) and must run behind the post-merge validation gate so it can
// never emit non-compiling output as a clean merge.
type AIResolver interface {
	Resolve(ctx context.Context, req AIResolveRequest) (*AIResolveResult, error)
}

// AIResolveRequest contains the entity-level context for an AI merge resolution.
type AIResolveRequest struct {
	FilePath   string
	EntityKey  string
	EntityKind string
	Language   string
	BaseBody   []byte
	OursBody   []byte
	TheirsBody []byte
}

// AIResolveResult holds the AI-generated resolution for a merge conflict.
type AIResolveResult struct {
	ResolvedBody []byte
	Explanation  string
	Confidence   float64
}
