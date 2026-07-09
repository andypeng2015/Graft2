package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRepoWithNoticeUsesProvidedWriter(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")

	var notice bytes.Buffer
	r, err := openRepoWithNotice(dir, &notice)
	if err != nil {
		t.Fatalf("openRepoWithNotice: %v", err)
	}
	if r == nil {
		t.Fatal("openRepoWithNotice returned nil repo")
	}
	if !strings.Contains(notice.String(), ".graft not found") {
		t.Fatalf("notice = %q, want auto-init notice", notice.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".graft")); err != nil {
		t.Fatalf(".graft not initialized: %v", err)
	}

	var second bytes.Buffer
	if _, err := openRepoWithNotice(dir, &second); err != nil {
		t.Fatalf("second openRepoWithNotice: %v", err)
	}
	if second.String() != "" {
		t.Fatalf("second notice = %q, want empty when .graft already exists", second.String())
	}
}
