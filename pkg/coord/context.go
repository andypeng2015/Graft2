package coord

import "sort"

// EstimateTokens returns a rough token count using the standard ~4-bytes-per-
// token heuristic (ceiling division). It is deterministic and dependency-free,
// and intentionally rounds up so budgets are not silently exceeded.
func EstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + 3) / 4
}

// QualifiedSymbolName builds the fully-qualified symbol name used as an xref
// index key, e.g. "module/pkg/dir.Name". A package dir of "" or "." denotes the
// module root. This mirrors the format produced by buildQualifiedNames.
func QualifiedSymbolName(modulePath, pkgDir, name string) string {
	if pkgDir == "." {
		pkgDir = ""
	}
	switch {
	case modulePath != "" && pkgDir != "":
		return modulePath + "/" + pkgDir + "." + name
	case modulePath != "":
		return modulePath + "." + name
	case pkgDir != "":
		return pkgDir + "." + name
	default:
		return name
	}
}

// CalleesOf returns the dependencies of an entity: the distinct symbols whose
// call sites are enclosed by the named entity. It inverts the reverse xref
// index (symbol -> call sites) on each call site's enclosing Entity. Results
// are sorted for deterministic output.
func (idx *XrefIndex) CalleesOf(entity string) []string {
	if idx == nil || entity == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for symbol, sites := range idx.Refs {
		for _, s := range sites {
			if s.Entity == entity {
				if !seen[symbol] {
					seen[symbol] = true
					out = append(out, symbol)
				}
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// CallersOf returns the dependents of a symbol: the distinct entities that
// reference the given qualified symbol name. Results are sorted.
func (idx *XrefIndex) CallersOf(qualifiedName string) []string {
	if idx == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range idx.Refs[qualifiedName] {
		if s.Entity == "" || seen[s.Entity] {
			continue
		}
		seen[s.Entity] = true
		out = append(out, s.Entity)
	}
	sort.Strings(out)
	return out
}

// ContextSection is one entity rendered into an LLM context window: either its
// full body or, when the budget is tight, its signature only.
type ContextSection struct {
	Name          string `json:"name"`
	Signature     string `json:"signature,omitempty"`
	Body          string `json:"body,omitempty"`
	SignatureOnly bool   `json:"signature_only"`
}

// EntityContextResult is a token-budgeted context window for a target entity:
// the entity itself, what it depends on, and what depends on it.
type EntityContextResult struct {
	Target       ContextSection   `json:"target"`
	Dependencies []ContextSection `json:"dependencies"`
	Dependents   []ContextSection `json:"dependents"`
	BudgetTokens int              `json:"budget_tokens"`
	UsedTokens   int              `json:"used_tokens"`
	Truncated    bool             `json:"truncated"`
}

// AssembleEntityContext packs the target entity plus its dependencies and
// dependents into a token budget. The target is always included in full — it is
// the point of the request. Dependencies, then dependents, are added in order:
// each is included in full if its body fits the remaining budget, otherwise
// degraded to signature-only if that fits, otherwise dropped. Any degrade or
// drop marks the result truncated.
func AssembleEntityContext(target ContextSection, deps, dependents []ContextSection, budgetTokens int) EntityContextResult {
	res := EntityContextResult{
		Target:       target,
		BudgetTokens: budgetTokens,
	}
	res.Target.SignatureOnly = false
	used := EstimateTokens(target.Body)

	pack := func(sections []ContextSection) []ContextSection {
		var out []ContextSection
		for _, sec := range sections {
			remaining := budgetTokens - used
			switch {
			case EstimateTokens(sec.Body) <= remaining:
				sec.SignatureOnly = false
				used += EstimateTokens(sec.Body)
				out = append(out, sec)
			case EstimateTokens(sec.Signature) <= remaining:
				sec.Body = ""
				sec.SignatureOnly = true
				used += EstimateTokens(sec.Signature)
				res.Truncated = true
				out = append(out, sec)
			default:
				res.Truncated = true
			}
		}
		return out
	}

	res.Dependencies = pack(deps)
	res.Dependents = pack(dependents)
	res.UsedTokens = used
	return res
}
