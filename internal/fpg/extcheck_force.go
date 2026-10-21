//go:build fpgforce

package fpg

// Built with -tags fpgforce (brute-force and differential runs only, not
// a release build), the closed scan takes its rare paths on ordinary
// data: stage b runs whenever t* has a candidate and the exponential
// search whenever a transaction has at least two items (issue #20).
func init() {
	scanDefaults = scanParams{gallopMin: 2, gallopRatio: 1, guard: 0}
}
