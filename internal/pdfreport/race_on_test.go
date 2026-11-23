//go:build race

package pdfreport

// raceEnabled: the tests run under the race detector, which the large render, on one goroutine,
// gains nothing from (it costs a minute there).
const raceEnabled = true
