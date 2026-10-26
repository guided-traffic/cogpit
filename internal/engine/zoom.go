package engine

import (
	"errors"
	"fmt"
	"math"
)

// The window's zoom (D-134, the owner's request of 2026-10-26: "wie bei VSCode … ⌘- und ⌘+"): VS
// Code's Zoom In, Zoom Out and Reset Zoom, its keys on each platform, and its step: a zoom level is
// a factor of 1.2 to its power (VS Code's zoomLevelToZoomFactor, Electron's scale := 1.2 ^ level).
// The level scales the whole window, text, icons, room and panels alike, as Chromium's page zoom
// does for VS Code: Fyne's canvas scale (fyneui/zoom.go). Preferences.ZoomLevel keeps it, one value
// for the one window, kept across starts; Reset Zoom goes back to 0, 100% (the owner's answer of
// 2026-10-26: one value that stays, as VS Code before 1.86, whose later split into a default and a
// zoom per window answers several windows). The commands are ui.zoom.in, ui.zoom.out and
// ui.zoom.reset (commands/zoom.go); agents may zoom too: it changes only how large the window
// draws.

// The zoom levels Cogpit offers (the lead's choice, 2026-10-26): VS Code allows −8 to 8, 23% to
// 430%; Fyne's window grows to fit its content's minimum size at the new scale, about 593 × 249
// points of Cogpit's, so 210% (1245 points wide) still fits a 1280-point laptop screen and 250%
// would not fit a 13-inch MacBook Air's 1470. Down to 50%, where Cogpit's text is 6.5 points.
const (
	MinZoomLevel = -4
	MaxZoomLevel = 4
)

// zoomStep is VS Code's factor per level.
const zoomStep = 1.2

// ZoomFactor is the factor a zoom level scales the window by, to a tenth as Fyne rounds a canvas
// scale (glfw's calculateScale): 1.2, 1.4, 1.7, 2.1 up, 0.8, 0.7, 0.6, 0.5 down.
func ZoomFactor(level int) float64 {
	return math.Round(math.Pow(zoomStep, float64(level))*10) / 10
}

// ZoomPercent is a zoom level as the status bar writes it: "120%".
func ZoomPercent(level int) string {
	return fmt.Sprintf("%d%%", int(math.Round(ZoomFactor(level)*100)))
}

// CheckZoomLevel refuses a zoom level out of Cogpit's range.
func CheckZoomLevel(level int) error {
	if level < MinZoomLevel || level > MaxZoomLevel {
		return fmt.Errorf("a zoom level is %d to %d (%s to %s; 0: 100%%), not %d", MinZoomLevel, MaxZoomLevel,
			ZoomPercent(MinZoomLevel), ZoomPercent(MaxZoomLevel), level)
	}
	return nil
}

// ErrZoomMax and ErrZoomMin disable Zoom In at the largest level and Zoom Out at the smallest;
// ErrZoomReset disables Reset Zoom at 100%; ErrZoomFit, wrapped with the level, Zoom In where the
// window's content would not fit the window (ZoomInError).
var (
	ErrZoomMax   = fmt.Errorf("the window is at its largest zoom, %s", ZoomPercent(MaxZoomLevel))
	ErrZoomMin   = fmt.Errorf("the window is at its smallest zoom, %s", ZoomPercent(MinZoomLevel))
	ErrZoomReset = errors.New("the window is at 100%")
	ErrZoomFit   = errors.New("the window is too small")
)

// The window's fit (the lead's decision of 2026-10-26, from the first zoom in a real window): Fyne
// widens a window to its content's minimum size, Cogpit's about 825 × 444 Fyne units with a cluster
// shown, so 210% grew a window of 1440 points to 1733. VS Code's window never grows when it zooms.
// So Zoom In stops at the largest level whose content still fits the window as it is: the window
// tells the session that level (SetZoomCeiling) when its size or its zoom changes, and a larger
// window, or full screen, zooms further.

// SetZoomCeiling keeps the largest zoom level the window's content fits the window at.
func (s *Session) SetZoomCeiling(level int) {
	s.mu.Lock()
	s.zoomCeiling = &level
	s.mu.Unlock()
}

// ZoomInError says why Zoom In cannot go beyond level: the largest level, or a window too small for
// the next one; nil when it can.
func (s *Session) ZoomInError(level int) error {
	if level >= MaxZoomLevel {
		return ErrZoomMax
	}
	s.mu.Lock()
	ceiling := s.zoomCeiling
	s.mu.Unlock()
	if ceiling != nil && level >= *ceiling {
		return ZoomFitError(level + 1)
	}
	return nil
}

// ZoomFitError is ErrZoomFit for a level: "the window is too small for 210%: make it larger first".
func ZoomFitError(level int) error {
	return fmt.Errorf("%w for %s: make it larger first", ErrZoomFit, ZoomPercent(level))
}

// SetZoomLevel keeps a zoom level in the preferences, clamped to Cogpit's range, and returns them.
func (s *Session) SetZoomLevel(level int) (Preferences, error) {
	level = max(MinZoomLevel, min(MaxZoomLevel, level))
	return s.ChangePreferences(func(p *Preferences) error {
		p.ZoomLevel = level
		return nil
	})
}
