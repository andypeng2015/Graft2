package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicReplacesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meta", "index")

	if err := writeFileAtomic(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic(old): %v", err)
	}
	if err := writeFileAtomic(path, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic(new): %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new\n" {
		t.Fatalf("content = %q, want %q", got, "new\n")
	}
}

func TestAppendFileAtomicPreservesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "refs", "heads", "main")

	if err := appendFileAtomic(path, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("appendFileAtomic(first): %v", err)
	}
	if err := appendFileAtomic(path, []byte("second\n"), 0o644); err != nil {
		t.Fatalf("appendFileAtomic(second): %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "first\nsecond\n" {
		t.Fatalf("content = %q, want two appended lines", got)
	}
}
