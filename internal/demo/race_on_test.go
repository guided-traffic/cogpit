//go:build race

package demo_test

// raceEnabled: the tests run under the race detector, which slows the churn's goroutines several
// times over on a busy runner.
const raceEnabled = true
