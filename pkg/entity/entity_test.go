package entity

import (
	"testing"
)

func TestEntityIdentity(t *testing.T) {
	e := Entity{
		Kind:     KindDeclaration,
		Name:     "HandleRequest",
		DeclKind: "function_definition",
		Receiver: "",
		Body:     []byte("func HandleRequest(w http.ResponseWriter, r *http.Request) {\n\treturn\n}"),
	}
	e.ComputeHash()

	if e.BodyHash == "" {
		t.Fatal("expected non-empty body hash")
	}
	if e.IdentityKey() == "" {
		t.Fatal("expected non-empty identity key")
	}

	// Same content, different name = different identity key but same hash
	e2 := Entity{
		Kind:     KindDeclaration,
		Name:     "ServeRequest",
		DeclKind: "function_definition",
		Body:     []byte("func HandleRequest(w http.ResponseWriter, r *http.Request) {\n\treturn\n}"),
	}
	e2.ComputeHash()

	if e.IdentityKey() == e2.IdentityKey() {
		t.Fatal("different names should produce different identity keys")
	}
	if e.BodyHash != e2.BodyHash {
		t.Fatal("same body should produce same hash")
	}
}

// A declaration's identity must survive a signature change (e.g. adding a
// parameter). Today the normalized signature is baked into the identity key,
// so changing it makes merge/blame/diff read the entity as delete+add instead
// of a modification. The identity key should depend on kind/receiver/name/
// ordinal — not the signature.
func TestIdentityKeyStableAcrossSignatureChange(t *testing.T) {
	before := Entity{
		Kind:      KindDeclaration,
		Name:      "Process",
		DeclKind:  "function_definition",
		Signature: "func Process(a int) int",
		Body:      []byte("func Process(a int) int { return a }"),
	}
	after := Entity{
		Kind:      KindDeclaration,
		Name:      "Process",
		DeclKind:  "function_definition",
		Signature: "func Process(a int, b int) int",
		Body:      []byte("func Process(a int, b int) int { return a + b }"),
	}

	if before.IdentityKey() != after.IdentityKey() {
		t.Fatalf("signature change must preserve identity key:\n  before %q\n  after  %q",
			before.IdentityKey(), after.IdentityKey())
	}
}

// Overloads (same name/kind/receiver, different signatures) must still get
// distinct identity keys. With the signature removed from the key, the
// disambiguator is the ordinal assigned by assignIdentityOrdinals — so the
// ordinal must be computed from a base key that ALSO ignores the signature.
// This guards against an incomplete fix that updates IdentityKey but not the
// ordinal assignment.
func TestIdentityKeyDistinguishesOverloads(t *testing.T) {
	el := &EntityList{
		Language: "java",
		Path:     "Overloads.java",
		Entities: []Entity{
			{
				Kind:      KindDeclaration,
				Name:      "add",
				DeclKind:  "method_declaration",
				Signature: "int add(int a)",
				Body:      []byte("int add(int a) { return a; }"),
			},
			{
				Kind:      KindDeclaration,
				Name:      "add",
				DeclKind:  "method_declaration",
				Signature: "int add(String a)",
				Body:      []byte("int add(String a) { return a.length(); }"),
			},
		},
	}

	assignIdentityOrdinals(el)

	if el.Entities[0].IdentityKey() == el.Entities[1].IdentityKey() {
		t.Fatalf("overloads must have distinct identity keys, both were %q",
			el.Entities[0].IdentityKey())
	}
}

func TestEntityKinds(t *testing.T) {
	tests := []struct {
		kind EntityKind
		str  string
	}{
		{KindPreamble, "preamble"},
		{KindImportBlock, "import_block"},
		{KindDeclaration, "declaration"},
		{KindInterstitial, "interstitial"},
	}
	for _, tt := range tests {
		if tt.kind.String() != tt.str {
			t.Errorf("expected %q, got %q", tt.str, tt.kind.String())
		}
	}
}
