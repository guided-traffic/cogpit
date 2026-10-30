//go:build !windows

package fyneui

// preferSoftGL does nothing off Windows: Mesa's OpenGL beside the binary is Windows' remedy for a
// computer without an OpenGL 2.1 driver (softgl.go).
func preferSoftGL() {}
