package merge

import "github.com/odvcencio/graft/pkg/entity"

// mergeIntroducedSyntaxError reports whether the structurally-merged output has
// a syntax error that neither input side had. When true, the structural merge
// must not be presented as clean: the post-merge gate downgrades it to a
// line-level fallback so non-compiling output is never recorded as a clean
// structural merge.
func mergeIntroducedSyntaxError(path string, merged, ours, theirs []byte) bool {
	if !entity.HasParseErrors(path, merged) {
		return false
	}
	// Only blame the merge when both sides parsed cleanly; pre-existing breakage
	// on an input must not be attributed to the merge.
	return !entity.HasParseErrors(path, ours) && !entity.HasParseErrors(path, theirs)
}
