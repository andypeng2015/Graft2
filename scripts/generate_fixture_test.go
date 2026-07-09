package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateBenchFixtureDryRunWritesContract(t *testing.T) {
	script := "./generate-bench-fixture.sh"
	if out, err := exec.Command("sh", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("shell syntax check failed: %v\n%s", err, out)
	}

	outDir := t.TempDir()
	cmd := exec.Command(script, "medium", outDir)
	cmd.Env = append(os.Environ(), "GRAFT_FIXTURE_DRY_RUN=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, out)
	}

	for _, name := range []string{"metadata.json", "commands.txt"} {
		path := filepath.Join(outDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestGenerateBenchFixtureMonorepoSmoke(t *testing.T) {
	script := "./generate-bench-fixture.sh"
	outDir := filepath.Join(t.TempDir(), "fixture")
	cmd := exec.Command(script, "monorepo", outDir)
	cmd.Env = append(os.Environ(),
		"GRAFT_FIXTURE_FILES=3",
		"GRAFT_FIXTURE_VENDOR_FILES=2",
		"GRAFT_FIXTURE_GENERATED_FILES=2",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate fixture failed: %v\n%s", err, out)
	}

	for _, path := range []string{
		"metadata.json",
		".graftignore",
		"README.fixture.md",
		"src/pkg0000/file000001.go",
		"vendor/example.com/dependency/pkg0000/file000001.txt",
		"generated/proto/pkg0000/file000001.pb.go",
	} {
		if _, err := os.Stat(filepath.Join(outDir, path)); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}

	ignore, err := os.ReadFile(filepath.Join(outDir, ".graftignore"))
	if err != nil {
		t.Fatalf("read .graftignore: %v", err)
	}
	for _, want := range []string{"vendor/", "generated/"} {
		if !strings.Contains(string(ignore), want) {
			t.Fatalf(".graftignore missing %q:\n%s", want, ignore)
		}
	}
}
