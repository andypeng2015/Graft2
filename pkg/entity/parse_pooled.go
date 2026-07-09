package entity

import (
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

var pooledParseMu sync.Mutex

func parseFilePooled(filename string, source []byte) (*gotreesitter.BoundTree, error) {
	// gotreesitter v0.22.5 still has shared GLR forest fast-path state that is
	// visible to the race detector under concurrent ParseFilePooled callers.
	pooledParseMu.Lock()
	defer pooledParseMu.Unlock()
	return grammars.ParseFilePooled(filename, source)
}
