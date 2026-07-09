package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManCmdGeneratesPages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "manual")

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"man", "--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "generated man pages in") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}

	for _, name := range []string{"graft.1", "graft-workflows.1", "graft-completion.1", "graft-man.1"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated man page %s: %v", name, err)
		}
	}

	workflowPage, err := os.ReadFile(filepath.Join(dir, "graft-workflows.1"))
	if err != nil {
		t.Fatalf("ReadFile workflows page: %v", err)
	}
	if !strings.Contains(string(workflowPage), "Common Graft workflows") {
		t.Fatalf("workflow man page missing guide text")
	}
}

func TestManCmdCreatesOutputDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "man")

	cmd := newRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"man", "--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "graft.1")); err != nil {
		t.Fatalf("expected root man page in created directory: %v", err)
	}
}
