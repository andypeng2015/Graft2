package merge

import (
	"bytes"
	"testing"
)

// TestMergeStructFields_Commutative pins the set-union determinism property the
// deep dive flagged as failing: merging the same two sides in either order must
// produce byte-identical output. Today new fields are appended ours-order then
// theirs-order, so swapping the arguments swaps the layout — hazardous for
// unkeyed struct literals and cgo/alignment.
func TestMergeStructFields_Commutative(t *testing.T) {
	base := []byte("type T struct {\n\tID int\n}")
	ours := []byte("type T struct {\n\tID int\n\tAlpha string\n}")
	theirs := []byte("type T struct {\n\tID int\n\tZeta string\n}")

	ab, c1 := MergeStructFields(base, ours, theirs, "go")
	ba, c2 := MergeStructFields(base, theirs, ours, "go")
	if c1 || c2 {
		t.Fatalf("unexpected conflict: c1=%v c2=%v", c1, c2)
	}
	if !bytes.Equal(ab, ba) {
		t.Fatalf("MergeStructFields not commutative:\n merge(ours,theirs)=\n%s\n merge(theirs,ours)=\n%s", ab, ba)
	}
}

// TestMergeFiles_StructMergeCommutative is the same property at the file level:
// MergeFiles(base, A, B).Merged must equal MergeFiles(base, B, A).Merged for a
// set-union struct merge.
func TestMergeFiles_StructMergeCommutative(t *testing.T) {
	base := []byte("package main\n\ntype T struct {\n\tID int\n}\n")
	ours := []byte("package main\n\ntype T struct {\n\tID int\n\tAlpha string\n}\n")
	theirs := []byte("package main\n\ntype T struct {\n\tID int\n\tZeta string\n}\n")

	ab, err := MergeFiles("x.go", base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	ba, err := MergeFiles("x.go", base, theirs, ours)
	if err != nil {
		t.Fatal(err)
	}
	if ab.HasConflicts || ba.HasConflicts {
		t.Fatalf("unexpected conflict: ab=%v ba=%v", ab.HasConflicts, ba.HasConflicts)
	}
	if !bytes.Equal(ab.Merged, ba.Merged) {
		t.Fatalf("MergeFiles struct merge not commutative:\n A,B=\n%s\n B,A=\n%s", ab.Merged, ba.Merged)
	}
}
