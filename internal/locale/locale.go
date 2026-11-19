// Package locale finds how this computer writes dates and times (D-31, owner: every timestamp in
// the local time zone and in the format that fits the location). On macOS the format comes from the
// region settings through CoreFoundation, which also honours a user's own formats and the 24-hour
// switch (system_darwin.go); elsewhere, and in the pure-Go cogpit-cli, from the POSIX locale the
// environment names, through a small table of common locales' CLDR formats. The result is a
// model.TimeLayout of Go layouts; the binaries look it up once at start and hand it to the engine,
// which writes every time the user reads with it, in the Preferences' time zone.
//
// Layer L1: it reads the environment and, on macOS, the system's settings; it imports nothing of
// Cogpit's but model and never talks to a cluster.
package locale

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Format is how this computer writes dates and times, and where Cogpit found it.
type Format struct {
	Layout model.TimeLayout
	// Source says where the layout came from, for the log: "macOS region en_DE (dd.MM.yy,
	// HH:mm:ss)", "LC_TIME=de_DE.UTF-8", "the default (ISO 8601)".
	Source string
}

// System returns the computer's format: on macOS built with cgo the region's (systemFormat), else
// Environment's.
func System() Format {
	if f, ok := systemFormat(); ok {
		return f
	}
	return Environment(os.Getenv)
}

// defaultSource names the ISO 8601 fallback.
const defaultSource = "the default (ISO 8601)"

// Environment returns the format of the POSIX locale the environment names, the first set of
// LC_ALL, LC_TIME and LANG, as POSIX resolves the time category: the CLDR formats of that locale
// (a short date with a four-digit year, a medium time), else of its language. C, POSIX, a locale
// the table does not hold, or none: ISO 8601. getenv is os.Getenv; tests pass their own.
func Environment(getenv func(string) string) Format {
	for _, name := range []string{"LC_ALL", "LC_TIME", "LANG"} {
		v := getenv(name)
		if v == "" {
			continue
		}
		p, ok := patternsFor(v)
		if !ok { // C, POSIX, or a locale the table does not hold
			return Format{Source: name + "=" + v + ": " + defaultSource}
		}
		l, err := FromPatterns(p.date, p.time)
		src := name + "=" + v
		if err != nil {
			src += " (" + err.Error() + ")"
		}
		return Format{Layout: l, Source: src}
	}
	return Format{Source: defaultSource}
}

// FromPatterns converts a date pattern and a time pattern of Unicode's (UTS #35, what macOS, ICU
// and CLDR write: "dd.MM.yy", "HH:mm:ss") to a layout. A pattern that does not convert, or a date
// without day, month and year or a time without hours and minutes, gives ISO 8601's in its place,
// and the error says which.
func FromPatterns(date, timeOfDay string) (model.TimeLayout, error) {
	var l model.TimeLayout
	var errs []error
	if d, err := GoLayout(date); err != nil {
		errs = append(errs, fmt.Errorf("date pattern %q: %w", date, err))
	} else if !hasElements(d, "year", "month", "day") {
		errs = append(errs, fmt.Errorf("date pattern %q has no day, month and year", date))
	} else {
		l.Date = d
	}
	if t, err := GoLayout(timeOfDay); err != nil {
		errs = append(errs, fmt.Errorf("time pattern %q: %w", timeOfDay, err))
	} else if !hasElements(t, "hour", "minute") {
		errs = append(errs, fmt.Errorf("time pattern %q has no hours and minutes", timeOfDay))
	} else {
		l.Time = t
	}
	return l, errors.Join(errs...)
}

// hasElements reports whether a Go layout has every named element.
func hasElements(layout string, want ...string) bool {
	have := model.LayoutElements(layout)
	for _, w := range want {
		found := false
		for _, h := range have {
			found = found || h == w
		}
		if !found {
			return false
		}
	}
	return true
}

// patternsFor returns the CLDR patterns of a POSIX locale name, "de_DE.UTF-8", "de_DE@euro",
// "de-DE" or "de": the territory's, else the language's.
func patternsFor(name string) (patterns, bool) {
	if i := strings.IndexAny(name, ".@"); i >= 0 {
		name = name[:i]
	}
	name = strings.ReplaceAll(name, "-", "_")
	if name == "" || name == "C" || name == "POSIX" {
		return patterns{}, false
	}
	if p, ok := cldr[name]; ok {
		return p, true
	}
	lang, _, _ := strings.Cut(name, "_")
	p, ok := cldr[strings.ToLower(lang)]
	return p, ok
}
