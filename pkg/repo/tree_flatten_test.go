package repo

import (
	"fmt"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestFlattenTreeRejectsUnsafeEntryNames(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	for _, name := range []string{".", "..", "./root.txt", "a//b", "CON", "dir\\file"} {
		t.Run(name, func(t *testing.T) {
			rootHash, err := r.Store.WriteTree(&object.TreeObj{
				Entries: []object.TreeEntry{
					{
						Name:     name,
						IsDir:    false,
						Mode:     object.TreeModeFile,
						BlobHash: testTreeHash(1),
					},
				},
			})
			if err != nil {
				t.Fatalf("write root tree: %v", err)
			}

			if _, err := r.FlattenTree(rootHash); err == nil {
				t.Fatalf("FlattenTree succeeded for unsafe entry name %q", name)
			}
		})
	}
}

func TestFlattenTree_TraversalOrder(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	nestedTreeHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "d.txt",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(3),
			},
		},
	})
	if err != nil {
		t.Fatalf("write nested tree: %v", err)
	}

	dirTreeHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "b.txt",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(2),
			},
			{
				Name:        "nested",
				IsDir:       true,
				Mode:        object.TreeModeDir,
				SubtreeHash: nestedTreeHash,
			},
			{
				Name:     "a.txt",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(4),
			},
		},
	})
	if err != nil {
		t.Fatalf("write dir tree: %v", err)
	}

	rootHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "z.txt",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(1),
			},
			{
				Name:        "dir",
				IsDir:       true,
				Mode:        object.TreeModeDir,
				SubtreeHash: dirTreeHash,
			},
			{
				Name:     "m.txt",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(5),
			},
		},
	})
	if err != nil {
		t.Fatalf("write root tree: %v", err)
	}

	entries, err := r.FlattenTree(rootHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	wantPaths := []string{
		"dir/a.txt",
		"dir/b.txt",
		"dir/nested/d.txt",
		"m.txt",
		"z.txt",
	}
	wantHashes := []object.Hash{
		testTreeHash(4),
		testTreeHash(2),
		testTreeHash(3),
		testTreeHash(5),
		testTreeHash(1),
	}

	if len(entries) != len(wantPaths) {
		t.Fatalf("FlattenTree returned %d entries, want %d", len(entries), len(wantPaths))
	}

	for i, wantPath := range wantPaths {
		if entries[i].Path != wantPath {
			t.Fatalf("entry[%d].Path = %q, want %q", i, entries[i].Path, wantPath)
		}
		if entries[i].BlobHash != wantHashes[i] {
			t.Fatalf("entry[%d].BlobHash = %q, want %q", i, entries[i].BlobHash, wantHashes[i])
		}
	}
}

func testTreeHash(seed int) object.Hash {
	return object.Hash(fmt.Sprintf("%064x", seed))
}
