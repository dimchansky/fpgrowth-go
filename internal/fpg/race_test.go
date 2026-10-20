//go:build race

package fpg

// raceEnabled reports whether the race detector is on; slow tests use
// fewer cases then.
const raceEnabled = true
