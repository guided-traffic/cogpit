package fyneui

import (
	"path/filepath"
	"strings"
)

// Mesa's OpenGL beside cogpit.exe (D-142): a Windows computer without an OpenGL 2.1 driver, Fyne's
// minimum (Windows Server, a virtual machine without a GPU), draws with Mesa's opengl32.dll and
// libgallium_wgl.dll put beside cogpit.exe, the first place Windows looks for them. Without
// GALLIUM_DRIVER, Mesa tries its d3d12 and zink drivers before its software one, llvmpipe, and on
// such a computer the first can set up and still fail, so Cogpit names llvmpipe for its own process
// unless the user named a driver. Windows' file has the lookup (softgl_windows.go, softgl_other.go
// elsewhere); the rule is here, tested on every system.

// galliumDriverEnv is the variable through which Mesa takes its driver; softGLDriver is its
// software one.
const (
	galliumDriverEnv = "GALLIUM_DRIVER"
	softGLDriver     = "llvmpipe"
)

// softGL decides for the opengl32.dll the process loaded (glDLL, "" when unknown), Cogpit's
// executable (exe) and GALLIUM_DRIVER's value (driver; empty is unset, as for Mesa): mesa is whether
// the DLL lies in the executable's folder, set the driver Cogpit names, "" when it leaves the
// variable alone. Windows' paths compare without case.
func softGL(glDLL, exe, driver string) (mesa bool, set string) {
	if glDLL == "" || exe == "" || !strings.EqualFold(filepath.Dir(glDLL), filepath.Dir(exe)) {
		return false, ""
	}
	if driver != "" {
		return true, ""
	}
	return true, softGLDriver
}
