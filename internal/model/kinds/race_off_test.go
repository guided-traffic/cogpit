//go:build !race

package kinds_test

// raceEnabled: the tests run under the race detector, which the type-confusion tests, on one
// goroutine, gain nothing from.
const raceEnabled = false
