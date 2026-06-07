package coord

import (
	"reflect"
	"strings"
	"testing"
)

// CalleesOf inverts the reverse xref index: the dependencies of an entity are
// the symbols whose call sites are enclosed by that entity.
func TestXrefCalleesOf(t *testing.T) {
	idx := &XrefIndex{Refs: map[string][]XrefCallSite{
		"mod/pkg.Foo": {{Entity: "Bar", File: "a.go", Line: 10}},
		"mod/pkg.Baz": {{Entity: "Bar", File: "a.go", Line: 12}, {Entity: "Qux", File: "b.go", Line: 3}},
	}}

	if got, want := idx.CalleesOf("Bar"), []string{"mod/pkg.Baz", "mod/pkg.Foo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CalleesOf(Bar) = %v, want %v", got, want)
	}
	if got, want := idx.CalleesOf("Qux"), []string{"mod/pkg.Baz"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CalleesOf(Qux) = %v, want %v", got, want)
	}
	if got := idx.CalleesOf("Nobody"); len(got) != 0 {
		t.Fatalf("CalleesOf(Nobody) = %v, want empty", got)
	}
}

// CallersOf reads the reverse index directly: dependents are the distinct
// entities that reference the given qualified symbol name.
func TestXrefCallersOf(t *testing.T) {
	idx := &XrefIndex{Refs: map[string][]XrefCallSite{
		"mod/pkg.Foo": {{Entity: "Bar"}, {Entity: "Baz"}, {Entity: "Bar"}},
	}}

	if got, want := idx.CallersOf("mod/pkg.Foo"), []string{"Bar", "Baz"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CallersOf = %v, want %v", got, want)
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := map[string]int{
		"":      0,
		"abcd":  1, // 4/4
		"abcde": 2, // ceil(5/4)
	}
	for in, want := range cases {
		if got := EstimateTokens(in); got != want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", in, got, want)
		}
	}
}

// QualifiedSymbolName must reproduce the xref key format exactly, including
// the empty-component branches and treating a "." package dir as the module
// root.
func TestQualifiedSymbolName(t *testing.T) {
	cases := []struct{ mod, pkg, name, want string }{
		{"m31labs.dev/graft", "pkg/coord", "BuildXrefIndex", "m31labs.dev/graft/pkg/coord.BuildXrefIndex"},
		{"m31labs.dev/graft", "", "Main", "m31labs.dev/graft.Main"},
		{"m31labs.dev/graft", ".", "Main", "m31labs.dev/graft.Main"},
		{"", "pkg/x", "F", "pkg/x.F"},
		{"", "", "F", "F"},
	}
	for _, c := range cases {
		if got := QualifiedSymbolName(c.mod, c.pkg, c.name); got != c.want {
			t.Errorf("QualifiedSymbolName(%q,%q,%q) = %q, want %q", c.mod, c.pkg, c.name, got, c.want)
		}
	}
}

// Under a generous budget every section is included in full.
func TestAssembleEntityContext_FitsWithinBudget(t *testing.T) {
	target := ContextSection{Name: "Target", Signature: "func Target()", Body: "func Target() { Dep() }"}
	deps := []ContextSection{{Name: "Dep", Signature: "func Dep()", Body: "func Dep() { return }"}}

	res := AssembleEntityContext(target, deps, nil, 1000)

	if res.Truncated {
		t.Fatal("should not truncate with a generous budget")
	}
	if len(res.Dependencies) != 1 || res.Dependencies[0].SignatureOnly {
		t.Fatalf("dependency should be included in full, got %+v", res.Dependencies)
	}
}

// Under a tight budget the target stays full but dependency bodies degrade to
// signature-only, and the result is marked truncated.
func TestAssembleEntityContext_DegradesToSignatureUnderTightBudget(t *testing.T) {
	target := ContextSection{Name: "Target", Signature: "func Target()", Body: strings.Repeat("x", 400)} // ~100 tokens
	bigDep := ContextSection{Name: "Dep", Signature: "func Dep()", Body: strings.Repeat("y", 4000)}       // ~1000 tokens

	res := AssembleEntityContext(target, []ContextSection{bigDep}, nil, 120)

	if res.Target.SignatureOnly {
		t.Fatal("target must always be included in full")
	}
	if len(res.Dependencies) != 1 {
		t.Fatalf("expected the dependency to be present as a signature, got %d", len(res.Dependencies))
	}
	if !res.Dependencies[0].SignatureOnly {
		t.Fatal("dependency body should degrade to signature-only under a tight budget")
	}
	if !res.Truncated {
		t.Fatal("Truncated should be set when bodies are dropped")
	}
}
