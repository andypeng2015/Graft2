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

// FuzzExtractReconstructTier1 runs the same reconstruction invariant across
// Graft's documented tier-1 languages. Normal `go test` executes the seed
// corpus; release fuzzing can mutate both filename and source to stress
// gotreesitter language detection and parser boundaries.
//
// Run locally with: go test -run x -fuzz FuzzExtractReconstructTier1 ./pkg/entity/
func FuzzExtractReconstructTier1(f *testing.F) {
	seeds := []struct {
		filename string
		source   []byte
	}{
		{
			filename: "main.go",
			source:   []byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hi\") }\n"),
		},
		{
			filename: "app.py",
			source:   []byte("import os\n\nclass Greeter:\n    def hello(self, name):\n        return f\"hi {name}\"\n\n"),
		},
		{
			filename: "lib.rs",
			source:   []byte("pub struct User { id: u64 }\n\nimpl User {\n    pub fn id(&self) -> u64 { self.id }\n}\n"),
		},
		{
			filename: "service.ts",
			source:   []byte("export class Service {\n  async load(id: string): Promise<string> {\n    return `id:${id}`;\n  }\n}\n"),
		},
		{
			filename: "main.c",
			source:   []byte("#include <stdio.h>\n\nstatic int add(int a, int b) { return a + b; }\nint main(void) { return add(1, 2); }\n"),
		},
	}
	for _, seed := range seeds {
		f.Add(seed.filename, seed.source)
	}

	f.Fuzz(func(t *testing.T, filename string, src []byte) {
		if len(src) > 256*1024 {
			t.Skip("keep parser fuzz cases bounded in normal release gates")
		}
		el, err := Extract(filename, src)
		if err != nil {
			return
		}
		got := Reconstruct(el)
		if !bytes.Equal(got, src) {
			t.Fatalf("tier-1 reconstruction mismatch for %q:\n src=%q (%d)\n got=%q (%d)", filename, src, len(src), got, len(got))
		}
	})
}
