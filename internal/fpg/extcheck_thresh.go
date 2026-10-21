//go:build !fpgforce

package fpg

// The production thresholds of the closed scan (scanClosed, issue #20;
// extcheck_force.go lowers them for brute-force runs):
//   - scanGallopMin, scanGallopRatio: a transaction of at least 128
//     items is searched exponentially if the merge would walk more than
//     16 of its items per candidate. 16 is where the two cost the same
//     per walked item (challenge micro-benchmark); 128 keeps the search
//     off on ordinary data (transactions of up to about 100 items).
//   - scanGuard: stage b runs if t* has more than 4 candidates per
//     cover transaction, so that its pass over the cover costs at most
//     a quarter of copying t*.
const (
	scanGallopMin   = 128
	scanGallopRatio = 16
	scanGuard       = 4
)
