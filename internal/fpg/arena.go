package fpg

import (
	"fmt"
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// ErrTooLarge is returned by Miner.Mine when a variant needs more tree
// nodes than its int32-indexed node arena can address. It wraps
// tract.ErrTooLarge, so errors.Is(err, tract.ErrTooLarge) also holds.
var ErrTooLarge = fmt.Errorf("fpg: more than %d tree nodes: %w", math.MaxInt32, tract.ErrTooLarge)

// maxNodes is the capacity limit of every node arena (int32 indices). It
// is a variable so that tests can provoke an overflow with small data;
// tests that change it must not run in parallel.
var maxNodes = math.MaxInt32

// maxPresize bounds the initial capacity of a node arena. Arenas are
// presized from the extent of the transaction bag, which can be far larger
// than the number of nodes actually needed; beyond this size append grows
// them on demand.
const maxPresize = 1 << 24

// presize returns the initial arena capacity for an estimated number of
// nodes.
func presize(estimate int64) int {
	return int(max(0, min(estimate, maxPresize, int64(maxNodes))))
}

// arenaOverflow is the panic value of an arena allocation beyond maxNodes.
// The allocation sites sit deep in the recursions of all variants, so
// instead of threading an error through every add/project call the
// overflow unwinds to Miner.Mine, which converts it to ErrTooLarge
// (recoverOverflow). It never escapes the package.
type arenaOverflow struct{}

// checkArena panics with arenaOverflow if an arena of n nodes cannot grow.
func checkArena(n int) {
	if n >= maxNodes {
		panic(arenaOverflow{})
	}
}

// recoverOverflow converts an arenaOverflow panic into ErrTooLarge in
// *err; other panics continue. It must be deferred directly.
func recoverOverflow(err *error) {
	r := recover()
	if r == nil {
		return
	}
	if _, ok := r.(arenaOverflow); ok {
		*err = ErrTooLarge
		return
	}
	panic(r)
}
