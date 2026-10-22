package racekept

import "testing"

func TestPlain(t *testing.T) {}

func TestReadsMarker(t *testing.T) {
	if raceEnabled {
		t.Skip("not under the race detector")
	}
}

func TestViaHelper(t *testing.T) { skipUnderRace(t) }

func TestViaTwoHelpers(t *testing.T) { outer(t) }

func TestCallsAnUnrelatedHelper(t *testing.T) { unrelated(t) }

func skipUnderRace(t *testing.T) {
	if raceEnabled {
		t.Skip("not under the race detector")
	}
}

func outer(t *testing.T) { skipUnderRace(t) }

func unrelated(t *testing.T) { t.Helper() }

func TestViaMethod(t *testing.T) { fixture{}.skip(t) }

func TestViaFuncValue(t *testing.T) { run(t, skipUnderRace) }

func TestViaVar(t *testing.T) {
	if noTerminals {
		t.Skip("not under the race detector")
	}
}

type fixture struct{}

func (fixture) skip(t *testing.T) { skipUnderRace(t) }

func run(t *testing.T, step func(*testing.T)) { step(t) }

var noTerminals = raceEnabled
