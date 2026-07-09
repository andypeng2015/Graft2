package main

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestArchiveCommandUsesCommandOutputWriter(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newArchiveCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--format", "tar", "--prefix", "release", string(commitHash)})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("archive: %v", err)
	}

	tr := tar.NewReader(bytes.NewReader(out.Bytes()))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar first header: %v", err)
	}
	if hdr.Name != "release/main.go" {
		t.Fatalf("tar header name = %q, want release/main.go", hdr.Name)
	}
	data, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read tar data: %v", err)
	}
	if string(data) != "package main\n\nfunc main() {}\n" {
		t.Fatalf("tar data = %q", string(data))
	}
}
