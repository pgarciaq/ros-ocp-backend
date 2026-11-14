//go:build race

package kafka

// raceEnabled is true only under the race detector — the #804 race
// regression is meaningful only there (make test does not default to
// -race; the cycle's verification and mutation runs do).
const raceEnabled = true
