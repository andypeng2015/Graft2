package entity

import (
	"strings"
	"testing"
)

func hasRef(refs []Reference, fromNameSubstr, callee string) bool {
	for _, r := range refs {
		if r.Callee == callee && strings.Contains(r.FromEntity, fromNameSubstr) {
			return true
		}
	}
	return false
}

// Intra-package: a Go function calling another function in the same file/package
// must produce a reference edge. The existing go/ast xref cannot see this (no
// import selector), so this is the capability the tree-sitter extractor adds.
func TestExtractReferences_GoIntraPackage(t *testing.T) {
	src := `package main

func helper() int { return 1 }

func Caller() int {
	return helper() + helper()
}
`
	refs, err := ExtractReferences("x.go", []byte(src))
	if err != nil {
		t.Fatalf("ExtractReferences: %v", err)
	}
	if !hasRef(refs, "Caller", "helper") {
		t.Fatalf("expected an edge Caller->helper, got %+v", refs)
	}
}

// Multi-language: the same extractor must work on Python (call node type "call",
// function node "function_definition"), proving it is not Go-specific.
func TestExtractReferences_Python(t *testing.T) {
	src := `def helper():
    return 1

def caller():
    return helper()
`
	refs, err := ExtractReferences("x.py", []byte(src))
	if err != nil {
		t.Fatalf("ExtractReferences: %v", err)
	}
	if !hasRef(refs, "caller", "helper") {
		t.Fatalf("expected an edge caller->helper, got %+v", refs)
	}
}

// A method call like x.Method() must resolve to the bare callee name "Method".
func TestExtractReferences_SelectorCallee(t *testing.T) {
	src := `package main

func Run(d *Dep) {
	d.Process()
}
`
	refs, err := ExtractReferences("x.go", []byte(src))
	if err != nil {
		t.Fatalf("ExtractReferences: %v", err)
	}
	if !hasRef(refs, "Run", "Process") {
		t.Fatalf("expected an edge Run->Process, got %+v", refs)
	}
}
