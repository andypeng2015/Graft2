package coord

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/entity"
)

// RefSite records a single call reference: the enclosing entity that makes the
// call, and where.
type RefSite struct {
	FromEntity string `json:"from_entity"` // identity key of the caller
	File       string `json:"file"`
	Line       int    `json:"line"`
}

// RefIndex is a name-based, multi-language reference index built from
// tree-sitter call extraction (entity.ExtractReferences). Unlike XrefIndex
// (go/ast, cross-package only), it captures intra-package calls and works
// across languages — at the cost of matching by bare symbol name with no type
// resolution, so it may over-connect symbols that share a name.
type RefIndex struct {
	// ByCallee maps a called symbol's bare name to the sites that call it.
	ByCallee map[string][]RefSite `json:"by_callee"`
	// ByEntity maps a caller's identity key to the bare names it calls.
	ByEntity map[string][]string `json:"by_entity"`
}

// BuildRefIndex walks the working tree under repoRoot, extracts call references
// from every indexable source file, and builds forward (entity->callees) and
// reverse (callee->sites) maps.
func BuildRefIndex(repoRoot string) (*RefIndex, error) {
	idx := &RefIndex{
		ByCallee: map[string][]RefSite{},
		ByEntity: map[string][]string{},
	}
	calleeSeen := map[string]map[string]bool{}
	entitySeen := map[string]map[string]bool{}

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, walkErr error) error {
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
		if !isReferenceSource(filepath.Ext(path)) {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		refs, err := entity.ExtractReferences(rel, source)
		if err != nil {
			return nil
		}
		for _, r := range refs {
			if calleeSeen[r.Callee] == nil {
				calleeSeen[r.Callee] = map[string]bool{}
			}
			siteKey := r.FromEntity + "\x00" + rel
			if !calleeSeen[r.Callee][siteKey] {
				calleeSeen[r.Callee][siteKey] = true
				idx.ByCallee[r.Callee] = append(idx.ByCallee[r.Callee], RefSite{FromEntity: r.FromEntity, File: rel, Line: r.Line})
			}
			if entitySeen[r.FromEntity] == nil {
				entitySeen[r.FromEntity] = map[string]bool{}
			}
			if !entitySeen[r.FromEntity][r.Callee] {
				entitySeen[r.FromEntity][r.Callee] = true
				idx.ByEntity[r.FromEntity] = append(idx.ByEntity[r.FromEntity], r.Callee)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return idx, nil
}

// DependentsByName returns the sites that call a symbol of the given bare name,
// sorted by file then caller for deterministic output.
func (idx *RefIndex) DependentsByName(name string) []RefSite {
	if idx == nil {
		return nil
	}
	sites := append([]RefSite(nil), idx.ByCallee[name]...)
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].File != sites[j].File {
			return sites[i].File < sites[j].File
		}
		return sites[i].FromEntity < sites[j].FromEntity
	})
	return sites
}

// CalleesByEntity returns the bare names called from the given entity key,
// sorted for deterministic output.
func (idx *RefIndex) CalleesByEntity(entityKey string) []string {
	if idx == nil {
		return nil
	}
	names := append([]string(nil), idx.ByEntity[entityKey]...)
	sort.Strings(names)
	return names
}

func isReferenceSource(ext string) bool {
	switch ext {
	case ".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".rs", ".c", ".h", ".cpp", ".hpp", ".java", ".rb", ".cs":
		return true
	}
	return false
}

// SaveRefIndex serializes the reference index to a JSON blob and stores it at
// refs/coord/meta/refindex.
func (c *Coordinator) SaveRefIndex(idx *RefIndex) error {
	h, err := c.writeJSONBlob(idx)
	if err != nil {
		return fmt.Errorf("save ref index: %w", err)
	}
	return c.Repo.UpdateRef(refPath("meta", "refindex"), h)
}

// LoadRefIndex reads the reference index from refs/coord/meta/refindex.
func (c *Coordinator) LoadRefIndex() (*RefIndex, error) {
	h, err := c.Repo.ResolveRef(refPath("meta", "refindex"))
	if err != nil {
		return nil, fmt.Errorf("ref index not found: %w", err)
	}
	var idx RefIndex
	if err := c.readJSONBlob(h, &idx); err != nil {
		return nil, fmt.Errorf("read ref index: %w", err)
	}
	return &idx, nil
}
