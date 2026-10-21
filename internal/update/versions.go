package update

import (
	"strconv"
	"strings"
)

// compareVersions compares two release versions part by part ("0.10.0" > "0.9.3"): numbers as
// numbers, anything else as text. Cogpit's versions are x.y.z.
func compareVersions(a, b string) int {
	pa, pb := strings.FieldsFunc(a, isVersionSep), strings.FieldsFunc(b, isVersionSep)
	for i := range max(len(pa), len(pb)) {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil:
			if c := nx - ny; c != 0 {
				return c
			}
		case x != y:
			return strings.Compare(x, y)
		}
	}
	return 0
}

func isVersionSep(r rune) bool { return r == '.' || r == '-' || r == '+' || r == '~' }
