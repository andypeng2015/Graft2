package merge

import (
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
)

// makeEntitySig builds a declaration entity with an explicit signature so the
// tests can exercise signature-change behavior independently of the body.
func makeEntitySig(name, sig, body string) entity.Entity {
	e := entity.Entity{
		Kind:      entity.KindDeclaration,
		Name:      name,
		DeclKind:  "function_definition",
		Signature: sig,
		Body:      []byte(body),
	}
	e.ComputeHash()
	return e
}

// Changing a function's signature on one side — even when the body is also
// substantially rewritten so body-similarity rename detection does not fire —
// must register as a single modification (OursOnly), not a delete+add pair.
func TestSignatureChangeIsModifyNotAddDelete(t *testing.T) {
	base := makeEntityList([]entity.Entity{
		makeEntitySig("Process", "func Process(a int) int",
			"func Process(a int) int { return a }"),
	})
	ours := makeEntityList([]entity.Entity{
		makeEntitySig("Process", "func Process(a int, b int) int",
			"func Process(a int, b int) int {\n\tsum := a + b\n\tlog.Println(sum)\n\treturn sum\n}"),
	})
	theirs := makeEntityList([]entity.Entity{
		makeEntitySig("Process", "func Process(a int) int",
			"func Process(a int) int { return a }"),
	})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match (modification), got %d: %+v", len(matches), dispositions(matches))
	}
	if matches[0].Disposition != OursOnly {
		t.Fatalf("expected OursOnly for a signature change, got %v", matches[0].Disposition)
	}
}

// When both sides change the same function's signature in different ways, that
// is a genuine conflict on one entity — not two independent additions.
func TestDuelingSignatureChangesAreConflict(t *testing.T) {
	base := makeEntityList([]entity.Entity{
		makeEntitySig("Process", "func Process(a int) int",
			"func Process(a int) int { return a }"),
	})
	ours := makeEntityList([]entity.Entity{
		makeEntitySig("Process", "func Process(a int, b int) int",
			"func Process(a int, b int) int { return a + b }"),
	})
	theirs := makeEntityList([]entity.Entity{
		makeEntitySig("Process", "func Process(a string) int",
			"func Process(a string) int { return len(a) }"),
	})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match (conflict), got %d: %+v", len(matches), dispositions(matches))
	}
	if matches[0].Disposition != Conflict {
		t.Fatalf("expected Conflict for dueling signature changes, got %v", matches[0].Disposition)
	}
}

func dispositions(matches []MatchedEntity) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Key + "=" + m.Disposition.String()
	}
	return out
}
