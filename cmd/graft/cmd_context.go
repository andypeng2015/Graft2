package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/entity"
	"github.com/spf13/cobra"
)

// declDef is a declaration found while indexing the working tree, carrying
// enough to render it into a context window and to resolve xref names back to
// a definition body.
type declDef struct {
	File      string
	PkgDir    string
	Name      string
	Receiver  string
	DeclKind  string
	Signature string
	Body      string
	Key       string // identity key; populated only for a resolved target
}

func (d declDef) displayName() string {
	if d.Receiver != "" {
		return d.Receiver + "." + d.Name
	}
	return d.Name
}

func (d declDef) section() coord.ContextSection {
	return coord.ContextSection{
		Name:      d.File + "::" + d.displayName(),
		Signature: d.Signature,
		Body:      d.Body,
	}
}

func newContextCmd() *cobra.Command {
	var budget int
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "context <path::entity>",
		Short: "Token-budgeted context for an entity: itself, its dependencies, and its dependents",
		Long: `Assemble a token-budgeted context window for a single entity — the entity
body, the symbols it depends on, and the entities that depend on it — for
feeding to an LLM.

The selector is <path::entity>, where entity is either the entity's name or
its full identity key:

  graft context pkg/coord/xref.go::BuildXrefIndex
  graft context pkg/coord/xref.go::BuildXrefIndex --budget 1500 --json

Dependency and dependent edges come from graft's cross-reference index, which
today is Go-only and captures cross-package calls (calls to imported-package
symbols). Same-package and non-Go edges are not yet represented.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := buildContext(args[0], budget)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOutput {
				return writeJSON(out, entityContextResultToJSON(*res))
			}
			renderContext(out, res)
			return nil
		},
	}

	cmd.Flags().IntVar(&budget, "budget", 2000, "approximate token budget for the assembled context")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON instead of text")
	return cmd
}

// buildContext resolves the target entity, derives its dependencies and
// dependents from the xref index, and packs them into the token budget.
func buildContext(selector string, budget int) (*coord.EntityContextResult, error) {
	file, sel, ok := strings.Cut(selector, "::")
	if !ok || file == "" || sel == "" {
		return nil, fmt.Errorf("selector must be <path::entity>, got %q", selector)
	}
	file = filepath.ToSlash(file)

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}
	root := c.Repo.RootDir

	modulePath := ""
	if deps, e := coord.ParseGoModDeps(filepath.Join(root, "go.mod")); e == nil {
		modulePath = deps.Module
	}

	target, ok := resolveContextTarget(root, file, sel)
	if !ok {
		return nil, fmt.Errorf("entity %q not found in %s", sel, file)
	}

	defs, err := buildDeclIndex(root)
	if err != nil {
		return nil, fmt.Errorf("index declarations: %w", err)
	}

	idx, err := loadOrBuildXrefIndex(c)
	if err != nil {
		return nil, err
	}

	// Dependencies (cross-package): the symbols whose call sites are enclosed by
	// the target, from the go/ast xref.
	enclosing := target.displayName()
	var depSections []coord.ContextSection
	seenDep := map[string]bool{}
	addDep := func(s coord.ContextSection) {
		if seenDep[s.Name] {
			return
		}
		seenDep[s.Name] = true
		depSections = append(depSections, s)
	}
	for _, qual := range idx.CalleesOf(enclosing) {
		if d, ok := lookupQualified(defs, qual, modulePath); ok {
			addDep(d.section())
		} else {
			addDep(coord.ContextSection{Name: qual, Signature: qual, SignatureOnly: true})
		}
	}

	// Dependencies (intra-package / any language): call references made from
	// within the target's body, via the tree-sitter reference extractor. This
	// covers same-package calls the go/ast xref cannot see.
	if src, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(file))); e == nil {
		if refs, e2 := entity.ExtractReferences(file, src); e2 == nil {
			for _, rf := range refs {
				if rf.FromEntity != target.Key || rf.Callee == target.Name {
					continue
				}
				if d, ok := lookupEnclosing(defs, rf.Callee); ok {
					addDep(d.section())
				}
			}
		}
	}

	// Dependents (cross-package): go/ast callers of the target's qualified name.
	qualTarget := coord.QualifiedSymbolName(modulePath, filepath.Dir(file), target.Name)
	var depnSections []coord.ContextSection
	seenDepn := map[string]bool{}
	addDependent := func(s coord.ContextSection) {
		if seenDepn[s.Name] {
			return
		}
		seenDepn[s.Name] = true
		depnSections = append(depnSections, s)
	}
	for _, encl := range idx.CallersOf(qualTarget) {
		if d, ok := lookupEnclosing(defs, encl); ok {
			addDependent(d.section())
		} else {
			addDependent(coord.ContextSection{Name: encl, Signature: encl, SignatureOnly: true})
		}
	}

	// Dependents (intra-package / any language): callers from the tree-sitter
	// reference index, cached at refs/coord/meta/refindex.
	if refIdx, e := loadOrBuildRefIndex(c); e == nil {
		for _, site := range refIdx.DependentsByName(target.Name) {
			callerName := nameFromEntityKey(site.FromEntity)
			if callerName == "" || callerName == target.Name {
				continue
			}
			if d, ok := lookupByNameAndFile(defs, callerName, site.File); ok {
				addDependent(d.section())
			}
		}
	}

	res := coord.AssembleEntityContext(target.section(), depSections, depnSections, budget)
	return &res, nil
}

// resolveContextTarget reads a single file and returns the declaration matching
// the selector, which may be an entity name or a full identity key.
func resolveContextTarget(root, file, sel string) (declDef, bool) {
	abs := filepath.Join(root, filepath.FromSlash(file))
	source, err := os.ReadFile(abs)
	if err != nil {
		return declDef{}, false
	}
	el, err := entity.Extract(file, source)
	if err != nil {
		return declDef{}, false
	}
	for i := range el.Entities {
		e := &el.Entities[i]
		if e.Kind != entity.KindDeclaration {
			continue
		}
		if e.IdentityKey() == sel || e.Name == sel {
			return declDef{
				File:      file,
				PkgDir:    pkgDirOf(file),
				Name:      e.Name,
				Receiver:  e.Receiver,
				DeclKind:  e.DeclKind,
				Signature: e.Signature,
				Body:      string(e.Body),
				Key:       e.IdentityKey(),
			}, true
		}
	}
	return declDef{}, false
}

// buildDeclIndex walks the working tree once and indexes every top-level
// declaration by name, so xref names can be resolved back to a definition body.
func buildDeclIndex(root string) (map[string][]declDef, error) {
	defs := make(map[string][]declDef)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		base := filepath.Base(path)
		if info.IsDir() {
			if strings.HasPrefix(base, ".") || base == "vendor" || base == "node_modules" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isIndexableSource(filepath.Ext(path)) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		el, err := entity.Extract(rel, source)
		if err != nil {
			return nil
		}
		for i := range el.Entities {
			e := &el.Entities[i]
			if e.Kind != entity.KindDeclaration || e.Name == "" {
				continue
			}
			defs[e.Name] = append(defs[e.Name], declDef{
				File:      rel,
				PkgDir:    pkgDirOf(rel),
				Name:      e.Name,
				Receiver:  e.Receiver,
				DeclKind:  e.DeclKind,
				Signature: e.Signature,
				Body:      string(e.Body),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return defs, nil
}

// lookupQualified resolves a fully-qualified xref symbol (module/pkg.Name) to a
// definition, preferring the candidate whose own qualified name matches.
func lookupQualified(defs map[string][]declDef, qual, modulePath string) (declDef, bool) {
	dot := strings.LastIndex(qual, ".")
	if dot < 0 {
		return declDef{}, false
	}
	name := qual[dot+1:]
	cands := defs[name]
	for _, d := range cands {
		if coord.QualifiedSymbolName(modulePath, d.PkgDir, d.Name) == qual {
			return d, true
		}
	}
	if len(cands) > 0 {
		return cands[0], true
	}
	return declDef{}, false
}

// lookupEnclosing resolves an xref enclosing-entity name ("Name" or
// "Receiver.Name") to a definition, preferring a receiver match.
func lookupEnclosing(defs map[string][]declDef, encl string) (declDef, bool) {
	name, recv := encl, ""
	if i := strings.LastIndex(encl, "."); i >= 0 {
		recv, name = encl[:i], encl[i+1:]
	}
	cands := defs[name]
	for _, d := range cands {
		if recv == "" || d.Receiver == recv {
			return d, true
		}
	}
	if len(cands) > 0 {
		return cands[0], true
	}
	return declDef{}, false
}

// loadOrBuildRefIndex returns the cached tree-sitter reference index, building
// and persisting it on first use (mirrors loadOrBuildXrefIndex).
func loadOrBuildRefIndex(c *coord.Coordinator) (*coord.RefIndex, error) {
	idx, err := c.LoadRefIndex()
	if err != nil {
		idx, err = coord.BuildRefIndex(c.Repo.RootDir)
		if err != nil {
			return nil, fmt.Errorf("build ref index: %w", err)
		}
		_ = c.SaveRefIndex(idx)
	}
	return idx, nil
}

// nameFromEntityKey extracts the declaration name from an identity key of the
// form "decl:DeclKind:Receiver:Name:Ordinal".
func nameFromEntityKey(key string) string {
	if !strings.HasPrefix(key, "decl:") {
		return ""
	}
	parts := strings.SplitN(key, ":", 5)
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

// lookupByNameAndFile resolves a declaration by name, preferring the candidate
// in the given file.
func lookupByNameAndFile(defs map[string][]declDef, name, file string) (declDef, bool) {
	cands := defs[name]
	for _, d := range cands {
		if d.File == file {
			return d, true
		}
	}
	if len(cands) > 0 {
		return cands[0], true
	}
	return declDef{}, false
}

func pkgDirOf(relFile string) string {
	dir := filepath.ToSlash(filepath.Dir(relFile))
	if dir == "." {
		return ""
	}
	return dir
}

func isIndexableSource(ext string) bool {
	switch ext {
	case ".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".rs", ".c", ".h", ".cpp", ".hpp", ".java", ".rb", ".cs":
		return true
	}
	return false
}

func renderContext(out interface{ Write([]byte) (int, error) }, res *coord.EntityContextResult) {
	w := func(format string, a ...any) { fmt.Fprintf(out, format, a...) }

	trunc := ""
	if res.Truncated {
		trunc = " (truncated to fit budget)"
	}
	w("context: %s\n", res.Target.Name)
	w("budget: %d tokens, used ~%d%s\n\n", res.BudgetTokens, res.UsedTokens, trunc)

	w("# target\n%s\n\n", bodyOrSig(res.Target))

	w("# dependencies (%d)\n", len(res.Dependencies))
	renderSections(w, res.Dependencies)
	w("\n# dependents (%d)\n", len(res.Dependents))
	renderSections(w, res.Dependents)
}

func renderSections(w func(string, ...any), secs []coord.ContextSection) {
	if len(secs) == 0 {
		w("  (none)\n")
		return
	}
	// Stable order for readable output.
	sort.SliceStable(secs, func(i, j int) bool { return secs[i].Name < secs[j].Name })
	for _, s := range secs {
		if s.SignatureOnly {
			w("- %s  [signature-only]\n", s.Name)
			if s.Signature != "" {
				w("    %s\n", s.Signature)
			}
			continue
		}
		w("- %s\n", s.Name)
		for _, line := range strings.Split(strings.TrimRight(s.Body, "\n"), "\n") {
			w("    %s\n", line)
		}
	}
}

func bodyOrSig(s coord.ContextSection) string {
	if s.SignatureOnly {
		return s.Signature
	}
	return s.Body
}
