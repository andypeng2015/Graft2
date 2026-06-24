package merge

import (
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
)

// TestMergeIntroducedSyntaxError covers the post-merge gate's decision: a merge
// is only blamed for invalid syntax when the merged output fails to parse AND
// both input sides parsed cleanly. Pre-existing breakage on an input side must
// not be attributed to the merge.
func TestMergeIntroducedSyntaxError(t *testing.T) {
	validA := []byte("package main\n\nfunc A() int { return 1 }\n")
	validB := []byte("package main\n\nfunc B() int { return 2 }\n")
	broken := []byte("package main\n\nfunc C() int { return 3\n") // unbalanced brace

	cases := []struct {
		name                 string
		merged, ours, theirs []byte
		want                 bool
	}{
		{"valid merged is not flagged", validA, validA, validB, false},
		{"broken merged with valid sides is flagged", broken, validA, validB, true},
		{"broken merged not blamed when ours already broken", broken, broken, validB, false},
		{"broken merged not blamed when theirs already broken", broken, validA, broken, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeIntroducedSyntaxError("x.go", tc.merged, tc.ours, tc.theirs)
			if got != tc.want {
				t.Fatalf("mergeIntroducedSyntaxError(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestMergeFiles_DoesNotFalselyDowngradeCleanMerge guards the main risk of the
// gate: a legitimate clean structural merge (two independent functions added)
// must not be downgraded to a conflict, and the merged output must parse.
func TestMergeFiles_DoesNotFalselyDowngradeCleanMerge(t *testing.T) {
	base := []byte("package main\n\nfunc Base() {}\n")
	ours := []byte("package main\n\nfunc Base() {}\n\nfunc Ours() {}\n")
	theirs := []byte("package main\n\nfunc Base() {}\n\nfunc Theirs() {}\n")

	res, err := MergeFiles("x.go", base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasConflicts {
		t.Fatalf("clean structural merge wrongly downgraded to conflict:\n%s", res.Merged)
	}
	if entity.HasParseErrors("x.go", res.Merged) {
		t.Fatalf("merged output unexpectedly has parse errors:\n%s", res.Merged)
	}
	for _, d := range res.Diagnostics {
		if d.Rule == "post-merge-syntax-gate" {
			t.Fatalf("syntax gate falsely tripped on a clean merge")
		}
	}
}
