package entity

import (
	"bytes"
	"testing"
)

// FuzzExtractReconstruct fuzzes the foundational data-integrity invariant: for
// any input that extracts successfully, concatenating the entity bodies must
// reproduce the source byte-for-byte. The defensive overlap clamp makes this
// hold unconditionally by construction; this fuzz exercises it across arbitrary
// inputs to catch any path that violates byte coverage.
//
// Run locally with: go test -run x -fuzz FuzzExtractReconstruct ./pkg/entity/
func FuzzExtractReconstruct(f *testing.F) {
	seeds := []string{
		"package main\n\nfunc F() {}\n",
		"package main\n\ntype T struct {\n\tID int\n\tName string\n}\n",
		"package x\n// doc\nfunc A() int { return 1 } // inline\n",
		"package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc M() { fmt.Println(os.Args) }\n",
		"package main\nfunc a(){}\nfunc b(){}\nfunc c(){}\n",
		"",
		"\n\n\n",
		"package main\n\nfunc Generic[T any](x T) T { return x }\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		el, err := Extract("fuzz.go", src)
		if err != nil {
			return // unsupported/unparseable input is not a round-trip case
		}
		got := Reconstruct(el)
		if !bytes.Equal(got, src) {
			t.Fatalf("reconstruction not byte-for-byte:\n src=%q (%d)\n got=%q (%d)", src, len(src), got, len(got))
		}
	})
}
