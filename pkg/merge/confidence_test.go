package merge

import "testing"

func TestMergeFilesConfidenceStructuralClean(t *testing.T) {
	base := []byte("package main\n\nfunc Base() {}\n")
	ours := []byte("package main\n\nfunc Base() {}\n\nfunc Ours() {}\n")
	theirs := []byte("package main\n\nfunc Base() {}\n\nfunc Theirs() {}\n")

	result, err := MergeFiles("main.go", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles: %v", err)
	}
	if result.Confidence != MergeConfidenceStructuralClean {
		t.Fatalf("confidence = %q, want %q", result.Confidence, MergeConfidenceStructuralClean)
	}
}

func TestMergeFilesConfidenceTextFallback(t *testing.T) {
	base := []byte("a\n")
	ours := []byte("a\nb\n")
	theirs := []byte("a\nc\n")

	result, err := MergeFiles("notes.txt", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles: %v", err)
	}
	if result.HasConflicts {
		t.Fatalf("unexpected conflict in fallback merge:\n%s", result.Merged)
	}
	if result.Confidence != MergeConfidenceTextFallback {
		t.Fatalf("confidence = %q, want %q", result.Confidence, MergeConfidenceTextFallback)
	}
}

func TestMergeFilesConfidenceConflictRequired(t *testing.T) {
	base := []byte("package main\n\nfunc F() int { return 0 }\n")
	ours := []byte("package main\n\nfunc F() int { return 1 }\n")
	theirs := []byte("package main\n\nfunc F() int { return 2 }\n")

	result, err := MergeFiles("main.go", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles: %v", err)
	}
	if !result.HasConflicts {
		t.Fatal("expected merge conflict")
	}
	if result.Confidence != MergeConfidenceConflictRequired {
		t.Fatalf("confidence = %q, want %q", result.Confidence, MergeConfidenceConflictRequired)
	}
}
