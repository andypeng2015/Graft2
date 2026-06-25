package coord

import (
	"errors"
	"fmt"

	"github.com/odvcencio/graft/pkg/object"
	graftrepo "github.com/odvcencio/graft/pkg/repo"
)

// maxCASRetries bounds optimistic-concurrency retries for coord ref updates.
const maxCASRetries = 20

// updateRefCASRetry performs an optimistic-concurrency read-modify-write on a
// coord ref. It reads the current ref hash, lets build produce the next blob
// hash from that current state, and compare-and-swaps the ref. On a CAS
// mismatch (a concurrent writer won the race) it retries with the fresh state,
// so concurrent updates never silently clobber each other — the lost-update
// class the plain UpdateRef path allows.
//
// build receives the current blob hash ("" if the ref does not yet exist) and
// returns the hash of the blob to install.
func (c *Coordinator) updateRefCASRetry(ref string, build func(oldHash object.Hash) (object.Hash, error)) error {
	var lastErr error
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		oldHash, _ := c.Repo.ResolveRef(ref) // zero value if the ref does not exist yet
		newHash, err := build(oldHash)
		if err != nil {
			return err
		}
		if err := c.Repo.UpdateRefCAS(ref, newHash, oldHash); err != nil {
			if errors.Is(err, graftrepo.ErrRefCASMismatch) {
				lastErr = err
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("update %s: exceeded %d CAS retries: %w", ref, maxCASRetries, lastErr)
}
