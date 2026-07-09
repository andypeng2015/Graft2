package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/repo"
)

const mcpCodeintelGrepSource = `package main

import "fmt"

func Hello(name string) string {
	return fmt.Sprintf("hello %s", name)
}

func Goodbye(name string) string {
	return fmt.Sprintf("goodbye %s", name)
}
`

func TestMCPCodeintelAndGrepMapsIncludeSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/mcpgrep\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mcpCodeintelGrepSource), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}
	if err := r.Add([]string{"go.mod", "main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	entitiesAny, err := mcpDispatchAll(true, "graft_ci_entities", map[string]any{"file": "main.go"})
	if err != nil {
		t.Fatalf("mcpDispatchAll ci entities: %v", err)
	}
	entities := mustMCPVersionedMap(t, entitiesAny)
	if entities["file"] != "main.go" || intFromMCPNumber(t, entities["count"]) == 0 {
		t.Fatalf("entities result = %#v", entities)
	}

	symbolsAny, err := mcpDispatchAll(true, "graft_ci_symbols", map[string]any{"pattern": "Hello"})
	if err != nil {
		t.Fatalf("mcpDispatchAll ci symbols: %v", err)
	}
	symbols := mustMCPVersionedMap(t, symbolsAny)
	if intFromMCPNumber(t, symbols["count"]) == 0 {
		t.Fatalf("symbols result = %#v", symbols)
	}

	grepAny, err := mcpDispatchAll(true, "graft_grep", map[string]any{"pattern": "func $NAME($$$PARAMS) string"})
	if err != nil {
		t.Fatalf("mcpDispatchAll grep: %v", err)
	}
	grep := mustMCPVersionedMap(t, grepAny)
	if intFromMCPNumber(t, grep["count"]) != 2 {
		t.Fatalf("grep result = %#v, want two function matches", grep)
	}

	replaceAny, err := mcpDispatchAll(true, "graft_grep_replace", map[string]any{
		"pattern":     "func $NAME($$$PARAMS) bool",
		"replacement": "func $NAME($$$PARAMS) bool",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll grep replace: %v", err)
	}
	replace := mustMCPVersionedMap(t, replaceAny)
	if intFromMCPNumber(t, replace["total_edits"]) != 0 {
		t.Fatalf("replace result = %#v, want preview with no edits", replace)
	}

	el, err := entity.Extract("main.go", []byte(mcpCodeintelGrepSource))
	if err != nil {
		t.Fatalf("entity.Extract: %v", err)
	}
	var helloKey string
	for _, ent := range el.Entities {
		if ent.Name == "Hello" {
			helloKey = ent.IdentityKey()
			break
		}
	}
	if helloKey == "" {
		t.Fatal("Hello entity key not found")
	}

	editAny, err := mcpDispatchAll(true, "graft_entity_edit", map[string]any{
		"file":       "main.go",
		"entity_key": helloKey,
		"operation":  "replace_body",
		"content":    "func Hello(name string) string {\n\treturn name\n}\n",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll entity edit: %v", err)
	}
	edit := mustMCPVersionedMap(t, editAny)
	if edit["status"] != "ok" || edit["operation"] != "replace_body" {
		t.Fatalf("edit result = %#v", edit)
	}
}

func mustMCPVersionedMap(t *testing.T, value any) map[string]any {
	t.Helper()

	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", value)
	}
	if intFromMCPNumber(t, result["schemaVersion"]) != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %#v, want %d in %#v", result["schemaVersion"], JSONSchemaVersion, result)
	}
	return result
}

func intFromMCPNumber(t *testing.T, value any) int {
	t.Helper()

	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		t.Fatalf("value %v has type %T, want numeric", value, value)
		return 0
	}
}
