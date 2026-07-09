package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBenchArtifactsDryRunWritesContract(t *testing.T) {
	script := "./bench-artifacts.sh"
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("stat benchmark artifact script: %v", err)
	}

	if out, err := exec.Command("sh", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("shell syntax check failed: %v\n%s", err, out)
	}

	outDir := t.TempDir()
	cmd := exec.Command(script, outDir)
	cmd.Env = append(os.Environ(), "GRAFT_BENCH_DRY_RUN=1")
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

	commands, err := os.ReadFile(filepath.Join(outDir, "commands.txt"))
	if err != nil {
		t.Fatalf("read commands.txt: %v", err)
	}
	text := string(commands)
	for _, want := range []string{"./pkg/repo", "./pkg/entity ./pkg/merge ./pkg/object ./pkg/diff3", "-json >"} {
		if !strings.Contains(text, want) {
			t.Fatalf("commands.txt missing %q in:\n%s", want, text)
		}
	}
}
