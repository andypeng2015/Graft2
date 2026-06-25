package merge

import (
	"strings"
	"testing"
)

// TestMergeFiles_RenameBreaksCallerConflicts is the fatal false-negative the
// deep dive identified: ours renames Helper->HelperV2 while theirs adds a caller
// of Helper(). The structural merge takes both, leaving the merged file with a
// call to an undefined Helper — a clean, non-compiling merge. graft must surface
// this as a conflict.
func TestMergeFiles_RenameBreaksCallerConflicts(t *testing.T) {
	base := []byte("package main\n\nfunc Helper() int { return 1 }\n\nfunc Existing() {}\n")
	ours := []byte("package main\n\nfunc HelperV2() int { return 1 }\n\nfunc Existing() {}\n")
	theirs := []byte("package main\n\nfunc Helper() int { return 1 }\n\nfunc Existing() {}\n\nfunc Caller() int { return Helper() }\n")

	res, err := MergeFiles("x.go", base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasConflicts {
		t.Fatalf("expected conflict (ours renamed Helper->HelperV2 while theirs calls Helper()); got clean merge:\n%s", res.Merged)
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Rule == "cross-entity-orphan" && strings.Contains(d.Message, "Helper") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a cross-entity-orphan diagnostic naming Helper; diagnostics=%+v", res.Diagnostics)
	}
}

// TestMergeFiles_IndependentMergeHasNoOrphan guards against false positives: two
// independent function additions must merge cleanly with no orphan diagnostic.
func TestMergeFiles_IndependentMergeHasNoOrphan(t *testing.T) {
	base := []byte("package main\n\nfunc Base() {}\n")
	ours := []byte("package main\n\nfunc Base() {}\n\nfunc Ours() {}\n")
	theirs := []byte("package main\n\nfunc Base() {}\n\nfunc Theirs() {}\n")

	res, err := MergeFiles("x.go", base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasConflicts {
		t.Fatalf("independent additions must merge cleanly; got conflict:\n%s", res.Merged)
	}
	for _, d := range res.Diagnostics {
		if d.Rule == "cross-entity-orphan" {
			t.Fatalf("unexpected cross-entity-orphan on a clean independent merge: %+v", d)
		}
	}
}

// TestMergeFiles_ConsistentRenameNoOrphan guards the mergedRefs gate: when ours
// renames Helper->HelperV2 AND updates its caller, the merged output references
// HelperV2 (not Helper), so even though theirs still referenced the old name,
// nothing in the merged result is orphaned.
func TestMergeFiles_ConsistentRenameNoOrphan(t *testing.T) {
	base := []byte("package main\n\nfunc Helper() int { return 1 }\n\nfunc Caller() int { return Helper() }\n")
	ours := []byte("package main\n\nfunc HelperV2() int { return 1 }\n\nfunc Caller() int { return HelperV2() }\n")
	theirs := []byte("package main\n\nfunc Helper() int { return 1 }\n\nfunc Caller() int { return Helper() }\n\nfunc Note() {}\n")

	res, err := MergeFiles("x.go", base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Diagnostics {
		if d.Rule == "cross-entity-orphan" {
			t.Fatalf("consistent rename wrongly flagged as orphan: %+v\n%s", d, res.Merged)
		}
	}
}
