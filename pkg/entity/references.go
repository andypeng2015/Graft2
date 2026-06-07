package entity

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Reference is a name-based call/use edge: the declaration FromEntity refers to
// a symbol named Callee at the given source line. It is derived syntactically
// from call expressions via tree-sitter, with no type resolution — so it
// matches by bare name and may over-connect symbols that share a name across
// packages. Unlike the go/ast cross-package xref, this captures intra-package
// calls and works across languages.
type Reference struct {
	FromEntity string // IdentityKey of the enclosing declaration
	Callee     string // bare name of the called symbol
	Line       int
}

// callNodeTypes are the tree-sitter node types that represent a call or
// invocation across the languages graft extracts. Kept explicit rather than a
// substring heuristic for precision.
var callNodeTypes = map[string]bool{
	"call_expression":          true, // Go, JS, TS, Rust, C, C++
	"call":                     true, // Python, Ruby
	"method_invocation":        true, // Java
	"function_call_expression": true, // PHP
	"invocation_expression":    true, // C#
	"method_call":              true, // Ruby (alt)
}

// identifierNodeTypes are the leaf node types that carry a usable symbol name.
var identifierNodeTypes = map[string]bool{
	"identifier":          true,
	"field_identifier":    true,
	"property_identifier": true,
	"type_identifier":     true,
	"name":                true,
}

// ExtractReferences parses a file and returns the call references made from
// within each top-level declaration, keyed by the enclosing entity's identity.
func ExtractReferences(filename string, source []byte) ([]Reference, error) {
	el, err := Extract(filename, source)
	if err != nil {
		return nil, err
	}

	bt, err := grammars.ParseFilePooled(filename, source)
	if err != nil {
		return nil, err
	}
	defer bt.Release()

	var refs []Reference
	var walk func(node *gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		if callNodeTypes[bt.NodeType(node)] {
			if callee := calleeName(bt, node); callee != "" {
				if from := enclosingEntityKey(el, node.StartByte()); from != "" {
					refs = append(refs, Reference{
						FromEntity: from,
						Callee:     callee,
						Line:       lineNumberAtByte(source, node.StartByte()),
					})
				}
			}
		}
		for i := 0; i < node.ChildCount(); i++ {
			walk(node.Child(i))
		}
	}
	walk(bt.RootNode())
	return refs, nil
}

// calleeName extracts the bare name of the symbol a call node targets. The
// call's target is its first named child; the bare name is the rightmost
// identifier within it (so "x.y.Method" yields "Method", "foo" yields "foo").
func calleeName(bt *gotreesitter.BoundTree, call *gotreesitter.Node) string {
	target := firstNamedChild(call)
	if target == nil {
		return ""
	}
	return lastIdentifier(bt, target)
}

func firstNamedChild(node *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		if c := node.Child(i); c != nil && c.IsNamed() {
			return c
		}
	}
	return nil
}

func lastIdentifier(bt *gotreesitter.BoundTree, node *gotreesitter.Node) string {
	if identifierNodeTypes[bt.NodeType(node)] {
		return bt.NodeText(node)
	}
	for i := node.ChildCount() - 1; i >= 0; i-- {
		c := node.Child(i)
		if c == nil || !c.IsNamed() {
			continue
		}
		if name := lastIdentifier(bt, c); name != "" {
			return name
		}
	}
	return ""
}

// enclosingEntityKey returns the identity key of the smallest declaration entity
// whose byte range contains pos (smallest = most specific, handling nesting).
func enclosingEntityKey(el *EntityList, pos uint32) string {
	best := ""
	var bestSize uint32 = ^uint32(0)
	for i := range el.Entities {
		e := &el.Entities[i]
		if e.Kind != KindDeclaration {
			continue
		}
		if pos >= e.StartByte && pos < e.EndByte {
			if size := e.EndByte - e.StartByte; size < bestSize {
				bestSize = size
				best = e.IdentityKey()
			}
		}
	}
	return best
}
