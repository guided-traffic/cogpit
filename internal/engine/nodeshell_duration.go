package engine

import (
	"fmt"
	"strings"
	"time"
)

// The node shell's maximum duration (owner; D-37): how long a node shell's pod lives at most, its
// activeDeadlineSeconds and its container's sleep, so that it ends by then whatever Cogpit does.
// Cluster Settings › Node Shell › Maximum duration sets it per context, in whole minutes from
// MinNodeShellDeadline to MaxNodeShellDeadline (Cogpit's bounds); unset, it is
// DefaultNodeShellDeadline. Only the user sets it, as the image (D-38): agents and the CLI may
// reset it to the default.

// The node shell's deadline and its bounds.
const (
	// DefaultNodeShellDeadline is a node shell's maximum duration when its cluster's settings name
	// none (owner; Lens's is not known).
	DefaultNodeShellDeadline = 2 * time.Hour
	// MinNodeShellDeadline is the shortest maximum duration the setting takes: time to pull the
	// image and look at something (Cogpit's bound).
	MinNodeShellDeadline = 5 * time.Minute
	// MaxNodeShellDeadline is the longest: a working day and more, never an idle root pod for days
	// (Cogpit's bound).
	MaxNodeShellDeadline = 24 * time.Hour
)

// errNodeShellDeadlineBounds is the refusal of a maximum duration out of the bounds.
var errNodeShellDeadlineBounds = fmt.Errorf("the node shell's maximum duration is from %s to %s, such as 90m, 2h or 1h30m",
	FormatNodeShellDeadline(MinNodeShellDeadline), FormatNodeShellDeadline(MaxNodeShellDeadline))

// ParseNodeShellDeadline reads a maximum duration as typed and returns it in minutes: empty is the
// default (0); else a duration such as 90m, 2h or 1h30m, in whole minutes, within the bounds.
func ParseNodeShellDeadline(text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(text)
	switch {
	case err != nil:
		return 0, fmt.Errorf("the node shell's maximum duration %q: a duration such as 90m, 2h or 1h30m", text)
	case d < MinNodeShellDeadline || d > MaxNodeShellDeadline:
		return 0, errNodeShellDeadlineBounds
	case d%time.Minute != 0:
		return 0, fmt.Errorf("the node shell's maximum duration %q: whole minutes, such as 90m or 1h30m", text)
	}
	return int(d / time.Minute), nil
}

// CheckNodeShellMaxMinutes refuses a kept maximum duration out of the bounds; 0 is the default.
func CheckNodeShellMaxMinutes(minutes int) error {
	if minutes == 0 {
		return nil
	}
	if minutes < int(MinNodeShellDeadline/time.Minute) || minutes > int(MaxNodeShellDeadline/time.Minute) {
		return errNodeShellDeadlineBounds
	}
	return nil
}

// nodeShellDeadline is the deadline a kept maximum duration in minutes means.
func nodeShellDeadline(minutes int) time.Duration {
	if minutes == 0 || CheckNodeShellMaxMinutes(minutes) != nil {
		return DefaultNodeShellDeadline
	}
	return time.Duration(minutes) * time.Minute
}

// FormatNodeShellDeadline writes a whole number of minutes as the setting's field shows it: 2h,
// 1h30m, 45m.
func FormatNodeShellDeadline(d time.Duration) string {
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// NodeShellDeadlineText writes a whole number of minutes as a sentence does: 2 hours, 1 hour 30
// minutes, 45 minutes.
func NodeShellDeadlineText(d time.Duration) string {
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case h == 0:
		return plural(m, "minute")
	case m == 0:
		return plural(h, "hour")
	}
	return plural(h, "hour") + " " + plural(m, "minute")
}
