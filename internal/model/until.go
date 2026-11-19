package model

import (
	"strconv"
	"time"
)

// FormatUntil formats the time left until a moment in the future the way Lens's Cron Jobs view
// shows Next execution. Captured from Lens: "In about 1 hour 57 min". The other ranges follow the
// same pattern and are inferred: "In about 12 min", "In about 2 days 3 hours", "In less than a
// minute"; a moment already past reads "now".
func FormatUntil(d time.Duration) string {
	switch {
	case d <= 0:
		return "now"
	case d < time.Minute:
		return "In less than a minute"
	case d < time.Hour:
		return "In about " + strconv.Itoa(int(d/time.Minute)) + " min"
	case d < 24*time.Hour:
		h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
		out := "In about " + plural(h, "hour")
		if m > 0 {
			out += " " + strconv.Itoa(m) + " min"
		}
		return out
	default:
		days, h := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour)
		out := "In about " + plural(days, "day")
		if h > 0 {
			out += " " + plural(h, "hour")
		}
		return out
	}
}

// plural renders "1 hour", "2 hours".
func plural(n int, unit string) string {
	s := strconv.Itoa(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	return s
}
