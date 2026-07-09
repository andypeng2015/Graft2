package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseWorkflowContainsProductionGates(t *testing.T) {
	path := filepath.Join("..", ".github", "workflows", "release.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	text := string(data)

	for _, want := range []string{
		"name: Release Artifacts",
		"quality:",
		"Release quality gates",
		"go test ./...",
		"go test -race ./...",
		"go test -run=^$ -fuzz=FuzzExtractReconstructTier1 -fuzztime=20s ./pkg/entity",
		"TestProtocolConformanceSmokeAgainstMockOrchard|TestSupportedProtocolContractMatchesClientConstants",
		"go vet ./...",
		"needs: quality",
		"goos: linux",
		"goos: darwin",
		"goos: windows",
		"GOARCH: ${{ matrix.goarch }}",
		"CGO_ENABLED: \"0\"",
		"go build -trimpath",
		"graft release manifest --json dist",
		"graft release verify-manifest --json --base-dir . release-manifest.json",
		"graft release sbom --name graft dist",
		"graft release provenance",
		"graft release check --version",
		"actions/attest-build-provenance@v2",
		"actions/upload-artifact@v4",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}
}
