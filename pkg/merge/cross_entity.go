package merge

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/entity"
)

// crossEntityOrphanDiagnostics detects the rename/delete-breaks-caller class:
// a declaration that one side renamed away or deleted, which the OTHER side
// still references, and which the merged output no longer defines. Such a merge
// is structurally clean yet leaves a dangling reference (a non-compiling "clean"
// merge), so it must be surfaced as a conflict.
//
// The check is deliberately biased against FALSE NEGATIVES: references are
// matched by bare name with no cross-file type resolution, so a symbol genuinely
// defined in another file of the same package can yield a false-positive
// conflict. Per the merge philosophy (and ASE'24's finding that silent
// clean-but-wrong merges dominate cost), a spurious conflict is far cheaper than
// a silent corruption — "conflict where others corrupt."
//
// It is also precise about what it flags: the orphan must be referenced by the
// OTHER side (so a side's own pre-existing dangling reference is not blamed on
// the merge) AND still referenced in the merged output (so a reference that the
// merge resolved away is not flagged).
func crossEntityOrphanDiagnostics(path string, merged, ours, theirs []byte, matches []MatchedEntity) []Diagnostic {
	mergedDefs := declaredNames(path, merged)
	if mergedDefs == nil {
		// Merged output could not be parsed/extracted (e.g. unsupported
		// language); the syntax gate handles unparsable output. Do not guess.
		return nil
	}
	mergedRefs := referencedNames(path, merged)
	oursRefs := referencedNames(path, ours)
	theirsRefs := referencedNames(path, theirs)

	var diags []Diagnostic
	seen := map[string]bool{}
	for _, m := range matches {
		var otherRefs map[string]bool
		switch m.Disposition {
		case RenamedOurs, DeletedOurs:
			otherRefs = theirsRefs
		case RenamedTheirs, DeletedTheirs:
			otherRefs = oursRefs
		default:
			continue
		}
		if m.Base == nil || m.Base.Kind != entity.KindDeclaration {
			continue
		}
		oldName := m.Base.Name
		if oldName == "" || seen[oldName] {
			continue
		}
		// Orphan iff: the other side references the old name, that reference
		// survived into the merged output, and the merged output no longer
		// defines it.
		if otherRefs[oldName] && mergedRefs[oldName] && !mergedDefs[oldName] {
			seen[oldName] = true
			diags = append(diags, Diagnostic{
				Severity: DiagError,
				Entity:   oldName,
				Message: fmt.Sprintf(
					"merge orphaned a reference: %q was renamed or removed on one side but is still called on the other, and the merged result does not define it — resolve manually",
					oldName),
				Rule: "cross-entity-orphan",
			})
		}
	}
	return diags
}

// declaredNames returns the set of top-level declaration names defined in src,
// or nil if src cannot be extracted.
func declaredNames(path string, src []byte) map[string]bool {
	el, err := entity.Extract(path, src)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for i := range el.Entities {
		e := &el.Entities[i]
		if e.Kind == entity.KindDeclaration && e.Name != "" {
			out[e.Name] = true
		}
	}
	return out
}

// referencedNames returns the set of bare callee names referenced in src.
func referencedNames(path string, src []byte) map[string]bool {
	refs, err := entity.ExtractReferences(path, src)
	if err != nil {
		return map[string]bool{}
	}
	out := map[string]bool{}
	for _, r := range refs {
		out[r.Callee] = true
	}
	return out
}
