package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIWorkflowContainsFuzzSmokeGate(t *testing.T) {
	path := filepath.Join("..", ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ci workflow: %v", err)
	}
	text := string(data)

	for _, want := range []string{
		"fuzz-smoke:",
		"Fuzz smoke",
		"go test -run=^$ -fuzz=FuzzExtractReconstructTier1 -fuzztime=20s ./pkg/entity",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ci workflow missing %q", want)
		}
	}
}
