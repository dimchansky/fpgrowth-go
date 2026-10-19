//go:build race

package ruleval

// raceEnabled reports whether the race detector is on; slow tests use
// smaller inputs then.
const raceEnabled = true
