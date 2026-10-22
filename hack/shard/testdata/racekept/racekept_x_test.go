package racekept_test

import "testing"

// skipUnderRace of the external test package reads no marker: it must not hide the package's own.
func skipUnderRace(t *testing.T) { t.Helper() }

func TestExternalPlain(t *testing.T) { skipUnderRace(t) }
