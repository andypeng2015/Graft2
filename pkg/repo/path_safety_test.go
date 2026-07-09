package repo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestWriteStagingRejectsReservedAndCaseFoldUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	err = r.WriteStaging(&Staging{Entries: map[string]*StagingEntry{
		"CON": {Path: "CON", BlobHash: object.Hash("a")},
	}})
	if err == nil || !strings.Contains(err.Error(), "reserved on Windows") {
		t.Fatalf("WriteStaging reserved name error = %v, want reserved Windows diagnostic", err)
	}

	err = r.WriteStaging(&Staging{Entries: map[string]*StagingEntry{
		"README.md": {Path: "README.md", BlobHash: object.Hash("a")},
		"readme.md": {Path: "readme.md", BlobHash: object.Hash("b")},
	}})
	if err == nil || !strings.Contains(err.Error(), "case-insensitive") {
		t.Fatalf("WriteStaging case-fold error = %v, want case-insensitive diagnostic", err)
	}

	err = r.WriteStaging(&Staging{Entries: map[string]*StagingEntry{
		"safe/name.txt": {Path: "safe/name.txt", BlobHash: object.Hash("a")},
	}})
	if err != nil {
		t.Fatalf("WriteStaging safe path: %v", err)
	}
}

func TestAddRejectsSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires platform-specific privileges on Windows")
	}

	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	err = r.Add([]string{"link.txt"})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Add symlink error = %v, want symlink diagnostic", err)
	}
	stg, readErr := r.ReadStaging()
	if readErr != nil {
		t.Fatalf("ReadStaging: %v", readErr)
	}
	if _, ok := stg.Entries["link.txt"]; ok {
		t.Fatalf("symlink target was staged")
	}
}

func TestResetHardRejectsSymlinkedParentDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires platform-specific privileges on Windows")
	}

	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	blobHash, err := r.Store.WriteBlob(&object.Blob{Data: []byte("safe\n")})
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	subtreeHash, err := r.Store.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{
		Name:     "file.txt",
		Mode:     object.TreeModeFile,
		BlobHash: blobHash,
	}}})
	if err != nil {
		t.Fatalf("WriteTree subtree: %v", err)
	}
	rootHash, err := r.Store.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{
		Name:        "out",
		IsDir:       true,
		Mode:        object.TreeModeDir,
		SubtreeHash: subtreeHash,
	}}})
	if err != nil {
		t.Fatalf("WriteTree root: %v", err)
	}
	commitHash, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  rootHash,
		Author:    "tester",
		Timestamp: 1,
		Message:   "unsafe parent test",
	})
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}

	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(dir, "out")); err != nil {
		t.Fatalf("symlink parent: %v", err)
	}

	err = r.ResetToCommit(commitHash, ResetHard)
	if err == nil || !strings.Contains(err.Error(), "symlinked parent") {
		t.Fatalf("ResetToCommit error = %v, want symlinked parent diagnostic", err)
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("reset wrote through symlinked parent; stat err=%v", err)
	}
}
