package merge

import (
	"strings"
	"testing"
)

// TestMergeStructFields_PreservesComments asserts that set-union struct merge
// keeps both leading doc-comments and trailing inline comments. Today the
// line-based field parser strips inline comments and drops standalone comment
// lines entirely, silently editing documentation out of a "clean" merge.
func TestMergeStructFields_PreservesComments(t *testing.T) {
	base := []byte("type T struct {\n\t// ID identifies the row\n\tID int // primary key\n}")
	ours := []byte("type T struct {\n\t// ID identifies the row\n\tID int // primary key\n\tAlpha string\n}")
	theirs := []byte("type T struct {\n\t// ID identifies the row\n\tID int // primary key\n\tZeta string\n}")

	merged, conflict := MergeStructFields(base, ours, theirs, "go")
	if conflict {
		t.Fatal("unexpected conflict")
	}
	s := string(merged)
	if !strings.Contains(s, "// ID identifies the row") {
		t.Fatalf("leading doc-comment dropped:\n%s", s)
	}
	if !strings.Contains(s, "// primary key") {
		t.Fatalf("inline comment dropped:\n%s", s)
	}
}
