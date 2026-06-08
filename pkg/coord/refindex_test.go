package coord

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefIndexLookups(t *testing.T) {
	idx := &RefIndex{
		ByCallee: map[string][]RefSite{
			"helper": {
				{FromEntity: "decl:function_declaration::B:0", File: "b.go", Line: 3},
				{FromEntity: "decl:function_declaration::A:0", File: "a.go", Line: 5},
			},
		},
		ByEntity: map[string][]string{
			"decl:function_declaration::A:0": {"helper", "other"},
		},
	}

	dep := idx.DependentsByName("helper")
	if len(dep) != 2 {
		t.Fatalf("expected 2 dependents, got %d", len(dep))
	}
	// Sorted by File: a.go before b.go.
	if dep[0].File != "a.go" || dep[1].File != "b.go" {
		t.Fatalf("expected sort by file, got %s then %s", dep[0].File, dep[1].File)
	}

	callees := idx.CalleesByEntity("decl:function_declaration::A:0")
	if len(callees) != 2 || callees[0] != "helper" || callees[1] != "other" {
		t.Fatalf("unexpected callees: %v", callees)
	}

	if got := idx.DependentsByName("nobody"); len(got) != 0 {
		t.Fatalf("expected no dependents for unknown name, got %v", got)
	}
}

// BuildRefIndex must capture an intra-package caller, which the go/ast xref
// cannot represent.
func TestBuildRefIndex(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "util.go"), "package u\n\nfunc Helper() int { return 1 }\n")
	mustWriteFile(t, filepath.Join(dir, "main.go"), "package u\n\nfunc Run() int {\n\treturn Helper()\n}\n")

	idx, err := BuildRefIndex(dir)
	if err != nil {
		t.Fatalf("BuildRefIndex: %v", err)
	}

	found := false
	for _, s := range idx.DependentsByName("Helper") {
		if strings.Contains(s.FromEntity, "Run") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Run among Helper's dependents, got %+v", idx.DependentsByName("Helper"))
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
