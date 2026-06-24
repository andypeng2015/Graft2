package entity

import "github.com/odvcencio/gotreesitter/grammars"

// HasParseErrors reports whether source, parsed as the language implied by
// filename, contains syntax errors (tree-sitter ERROR/MISSING nodes).
//
// It returns false for empty input or unsupported languages: callers (notably
// the post-merge validation gate) treat "unknown" as "no detected error" so a
// merge is never downgraded to a conflict merely because graft cannot parse the
// language.
func HasParseErrors(filename string, source []byte) bool {
	if len(source) == 0 {
		return false
	}
	if grammars.DetectLanguage(filename) == nil {
		return false
	}
	bt, err := grammars.ParseFilePooled(filename, source)
	if err != nil {
		return true
	}
	defer bt.Release()
	root := bt.RootNode()
	if root == nil {
		return false
	}
	return root.HasError()
}
