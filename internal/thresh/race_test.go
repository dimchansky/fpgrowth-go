//go:build race

package thresh

// raceEnabled reports whether the race detector is on; the sweeps then
// use fewer cases (the package has no concurrency to check).
const raceEnabled = true
