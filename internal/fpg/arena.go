package fpg

import (
	"fmt"
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// ErrTooLarge is returned by Miner.Mine when a variant needs more tree
// nodes than its int32-indexed node arena can address, or when the item
// set tree (istree.ErrTooLarge) or the reporter's closed/maximal
// repository (report.ErrTooLarge) outgrows its int32 indices. It wraps
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

// treePresize returns the initial arena capacity for the FP-tree of bag
// and, if projections is set, the conditional trees built below it. The
// initial tree has about as many nodes as the bag's prefix tree (fewer
// after removing infrequent items); the projections alive at any time
// add less than half of that on the benchmark data (dense closed: peak
// 1.43 times the initial tree), while twice the extent (the number of
// item instances) reserves 2 to 3.3 times the peak. Whatever a run needs
// beyond the estimate, append still provides. The top-down
// variant builds its tree from the last item of every transaction, which
// gives it a different shape; it keeps the extent-based estimate.
func treePresize(bag *tract.Bag, projections bool) int {
	n := int64(bag.NodeCount()) + 16
	if projections {
		n += n / 2
	}
	return presize(min(n, 2*bag.Extent()+16))
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
