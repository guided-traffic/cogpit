//go:build windows

package fyneui

import (
	"log/slog"
	"os"

	"golang.org/x/sys/windows"
)

// preferSoftGL names Mesa's llvmpipe in Cogpit's own environment when the opengl32.dll the process
// loaded lies beside cogpit.exe and the user named no driver (softgl.go). Run calls it before the
// window exists: Mesa reads GALLIUM_DRIVER with GetEnvironmentVariableA, which sees os.Setenv, when
// it sets up its driver for the first window. The local terminal's shell inherits the variable,
// which steers only programs that load Mesa.
func preferSoftGL() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dll := loadedModule("opengl32.dll")
	mesa, set := softGL(dll, exe, os.Getenv(galliumDriverEnv))
	if !mesa {
		return
	}
	if set != "" {
		if err := os.Setenv(galliumDriverEnv, set); err != nil {
			slog.Warn("software rendering: GALLIUM_DRIVER not set", "opengl32", dll, "err", err)
			return
		}
	}
	slog.Info("software rendering: Mesa's OpenGL beside cogpit.exe", "opengl32", dll,
		galliumDriverEnv, os.Getenv(galliumDriverEnv), "setByCogpit", set != "")
}

// loadedModule is the path of the module the process loaded under name, "" when it has none.
// go-gl links opengl32.dll, so Windows loads it with the process, from cogpit.exe's folder when one
// lies there, else System32's.
func loadedModule(name string) string {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var h windows.Handle
	if err := windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, p, &h); err != nil {
		return ""
	}
	var buf [windows.MAX_LONG_PATH]uint16
	n, err := windows.GetModuleFileName(h, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
