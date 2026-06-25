package merge

import (
	"strings"
	"testing"
)

// TestMergeGoImports_PreservesStdlibGrouping asserts that a set-union import
// merge keeps Go's conventional grouping: standard-library packages first, a
// blank line, then third-party packages. Today the merge flat-sorts every
// import into a single block, interleaving stdlib with third-party and
// destroying the goimports grouping.
func TestMergeGoImports_PreservesStdlibGrouping(t *testing.T) {
	base := []byte("import (\n\t\"fmt\"\n\t\"github.com/foo/bar\"\n)")
	ours := []byte("import (\n\t\"fmt\"\n\t\"os\"\n\t\"github.com/foo/bar\"\n)")
	theirs := []byte("import (\n\t\"fmt\"\n\t\"github.com/foo/bar\"\n\t\"github.com/baz/qux\"\n)")

	merged, conflict := MergeImports(base, ours, theirs, "go")
	if conflict {
		t.Fatal("unexpected conflict")
	}
	s := string(merged)

	// All stdlib imports must precede all third-party imports.
	if strings.Index(s, "\"os\"") > strings.Index(s, "github.com") {
		t.Fatalf("stdlib not grouped before third-party:\n%s", s)
	}
	// The two groups must be separated by a blank line.
	if !strings.Contains(s, "\n\n") {
		t.Fatalf("no blank-line separator between import groups:\n%s", s)
	}
}
