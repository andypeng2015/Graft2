package entity

import "testing"

// TestHasParseErrors verifies the syntax-error detection primitive the
// post-merge validation gate relies on: broken source is flagged, valid source
// is not, and unknown/empty input is treated as "no detected error" so the gate
// never invents conflicts on languages graft cannot parse.
func TestHasParseErrors(t *testing.T) {
	cases := []struct {
		name string
		file string
		src  string
		want bool
	}{
		{"valid go", "x.go", "package main\n\nfunc F() int { return 1 }\n", false},
		{"broken go unbalanced braces", "x.go", "package main\n\nfunc F() int { return 1\n", true},
		{"broken go garbage tokens", "x.go", "package main\n\nfunc )( { ]]\n", true},
		{"empty source", "x.go", "", false},
		{"unsupported language is not an error", "notes.unknownext", "@@@ not code @@@", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HasParseErrors(tc.file, []byte(tc.src))
			if got != tc.want {
				t.Fatalf("HasParseErrors(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
