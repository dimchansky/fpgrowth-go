//go:build fpgforce

package fpg

// Built with -tags fpgforce (brute-force and differential runs only, not
// a release build), the closed scan takes its rare paths on ordinary
// data: stage b runs whenever t* has a candidate and the exponential
// search whenever a transaction has at least two items and the merge
// would walk past more than one item per candidate (issue #20).
const (
	scanGallopMin   = 2
	scanGallopRatio = 1
	scanGuard       = 0
)
