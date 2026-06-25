package main

import (
	"errors"
	"os"

	"github.com/odvcencio/graft/pkg/merge"
)

// errMergeConflict is the sentinel returned by the git merge driver when the
// merge could not be resolved cleanly. main maps any non-nil error to a
// non-zero exit, which is git's signal that conflicts remain.
var errMergeConflict = errors.New("merge conflicts remain")

// gitMergeDriver implements git's custom merge-driver contract. git invokes the
// driver with the base (%O), ours/current (%A), and theirs/other (%B) temp
// files plus the real pathname (%P); the driver writes the merged result back
// to the ours (%A) file and exits non-zero if conflicts remain.
//
// It runs graft's structural three-way merge, which is already conservative:
// it falls back to a line-level merge when any side cannot be parsed and gates
// itself against introducing syntax errors or orphaning cross-entity
// references. So even pure-git users get structural merges for clean cases and
// safe line-level behavior otherwise.
func gitMergeDriver(basePath, oursPath, theirsPath, pathname string) (bool, error) {
	base, err := os.ReadFile(basePath)
	if err != nil {
		return false, err
	}
	ours, err := os.ReadFile(oursPath)
	if err != nil {
		return false, err
	}
	theirs, err := os.ReadFile(theirsPath)
	if err != nil {
		return false, err
	}

	result, err := merge.MergeFiles(pathname, base, ours, theirs)
	if err != nil {
		return false, err
	}

	// git reads the merged content from the ours (%A) file.
	if err := os.WriteFile(oursPath, result.Merged, 0o644); err != nil {
		return false, err
	}
	return result.HasConflicts, nil
}
