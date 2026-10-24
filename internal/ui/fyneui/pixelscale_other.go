//go:build !darwin

package fyneui

import "fyne.io/fyne/v2"

// watchBackingScale has nothing to watch off macOS (D-121): Fyne keeps the framebuffer density at
// 1 on Windows and on Linux (X11), and a change of its scale goes through its settings listener's
// reloadScale, which draws the texts again.
func watchBackingScale(fyne.Window, func()) {}
