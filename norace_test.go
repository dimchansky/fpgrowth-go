//go:build !race

package fpgrowth

// raceEnabled reports whether the race detector is on; slow tests use
// smaller inputs then.
const raceEnabled = false
