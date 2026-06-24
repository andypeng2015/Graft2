package entity

import (
	"bytes"
	"testing"
)

// TestBuildCoveringEntities_ByteForByteUnderOverlap asserts the foundational
// data-integrity invariant unconditionally: concatenating the produced entity
// bodies reproduces the source byte-for-byte, even when the upstream parser
// hands us overlapping, fully-contained, or out-of-order sibling ranges.
// Without a defensive clamp these cases duplicate or drop source bytes.
func TestBuildCoveringEntities_ByteForByteUnderOverlap(t *testing.T) {
	cases := []struct {
		name   string
		source string
		nodes  []classifiedNode
	}{
		{
			name:   "non-overlapping adjacent (clamp must be a no-op)",
			source: "AAAABBBBCCCC",
			nodes: []classifiedNode{
				{kind: KindDeclaration, start: 0, end: 4, declKind: "x", name: "a"},
				{kind: KindDeclaration, start: 4, end: 8, declKind: "x", name: "b"},
				{kind: KindDeclaration, start: 8, end: 12, declKind: "x", name: "c"},
			},
		},
		{
			name:   "overlapping siblings",
			source: "AAAABBBBCCCC",
			nodes: []classifiedNode{
				{kind: KindDeclaration, start: 0, end: 8, declKind: "x", name: "a"},
				{kind: KindDeclaration, start: 4, end: 12, declKind: "x", name: "b"},
			},
		},
		{
			name:   "fully contained sibling",
			source: "AAAABBBBCCCC",
			nodes: []classifiedNode{
				{kind: KindDeclaration, start: 0, end: 12, declKind: "x", name: "a"},
				{kind: KindDeclaration, start: 4, end: 8, declKind: "x", name: "b"},
			},
		},
		{
			name:   "out-of-order overlapping input",
			source: "AAAABBBBCCCC",
			nodes: []classifiedNode{
				{kind: KindDeclaration, start: 4, end: 12, declKind: "x", name: "b"},
				{kind: KindDeclaration, start: 0, end: 8, declKind: "x", name: "a"},
			},
		},
		{
			name:   "leading and trailing gaps preserved",
			source: "  AAAA  ",
			nodes: []classifiedNode{
				{kind: KindDeclaration, start: 2, end: 6, declKind: "x", name: "a"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.source)
			ents := buildCoveringEntities(src, tc.nodes, nil)
			el := &EntityList{Entities: ents}
			got := Reconstruct(el)
			if !bytes.Equal(got, src) {
				t.Fatalf("reconstruction not byte-for-byte:\n got: %q (%d bytes)\nwant: %q (%d bytes)",
					got, len(got), src, len(src))
			}
		})
	}
}
