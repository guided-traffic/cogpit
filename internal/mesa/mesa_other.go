//go:build !windows

package mesa

// Offers is whether this system's Cogpit offers Mesa when Fyne finds no OpenGL to draw with: on
// Windows only, where Mesa beside the binary is the remedy (mesa.go).
const Offers = false

// Prefer does nothing off Windows.
func Prefer() {}

// Offer does nothing off Windows; Offers keeps it from being called.
func Offer(Failure, func(code int)) {}
