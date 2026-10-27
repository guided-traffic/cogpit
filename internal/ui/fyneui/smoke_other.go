//go:build !linux || !cgo

package fyneui

import "fyne.io/fyne/v2"

// windowSystemFrame reads nothing off Linux: a smoke run reads the GL driver's front buffer there
// (Capture), which holds the presented frame on macOS and with Windows' WGL, Mesa's llvmpipe in CI
// included.
func windowSystemFrame(fyne.Window) (SmokeFrame, bool) { return SmokeFrame{}, false }
