//go:build windows

package mesa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/guided-traffic/cogpit/internal/mesa/fetch"
)

// Offers is whether this system's Cogpit offers Mesa when Fyne finds no OpenGL to draw with.
const Offers = true

// user32's functions the dialogs use beyond MessageBox, loaded from System32 only.
var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW     = user32.NewProc("FindWindowW")
	procSetDlgItemTextW = user32.NewProc("SetDlgItemTextW")
	procGetDlgItem      = user32.NewProc("GetDlgItem")
	procEnableWindow    = user32.NewProc("EnableWindow")
	procPostMessageW    = user32.NewProc("PostMessageW")
	procEnumThreadWins  = user32.NewProc("EnumThreadWindows")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	procShowWindow      = user32.NewProc("ShowWindow")
)

const (
	idOK        = 1
	wmCommand   = 0x0111
	boxTextID   = 0xFFFF // the static control that holds a MessageBox's text
	dialogClass = "#32770"
	swHide      = 0
	// downloadTitle is the download box's title, by which its goroutine finds it.
	downloadTitle = "Cogpit: downloading Mesa"
	askTitle      = "Cogpit: no OpenGL to draw with"
)

// Prefer names Mesa's llvmpipe in Cogpit's own environment when the opengl32.dll the process
// loaded lies beside cogpit.exe and the user named no driver (softGL). It runs before the window
// exists: Mesa reads GALLIUM_DRIVER with GetEnvironmentVariableA, which sees os.Setenv, when it sets
// up its driver for the first window. The local terminal's shell inherits the variable, which
// steers only programs that load Mesa.
func Prefer() {
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

// Offer answers Fyne's report that it found no OpenGL to draw with (Watch's fire) and ends the
// process through exit. It hides the window Fyne left on this thread, then: Mesa beside cogpit.exe
// that did not draw either, a build without a pin and a folder the user may not write to each get
// a box that says what to do by hand; otherwise a box asks to download the pinned Mesa, a second
// shows the download, and Cogpit starts again once Mesa is in place.
func Offer(f Failure, exit func(code int)) {
	runtime.LockOSThread() // the boxes and the window Fyne left on one thread
	defer runtime.UnlockOSThread()
	slog.Error("no OpenGL to draw with: Cogpit offers Mesa", "failure", f.String())
	hideThreadWindows()
	exe, err := os.Executable()
	if err != nil {
		box(askTitle, manualText(f, Pin{}, "the folder of cogpit.exe", "Cogpit cannot tell where cogpit.exe is: "+err.Error()), windows.MB_ICONERROR)
		exit(1)
		return
	}
	dir := filepath.Dir(exe)
	if dll := loadedModule("opengl32.dll"); dll != "" {
		if mesa, _ := softGL(dll, exe, ""); mesa {
			box(askTitle, besideText(f, dll, logPath()), windows.MB_ICONERROR)
			exit(1)
			return
		}
	}
	p, ok := pinned()
	if !ok {
		box(askTitle, manualText(f, p, dir, "This build of Cogpit carries no Mesa release to download."), windows.MB_ICONWARNING)
		exit(1)
		return
	}
	if err := fetch.Writable(dir); err != nil {
		box(askTitle, manualText(f, p, dir, fmt.Sprintf("Cogpit cannot put files into %s: %v", dir, err)), windows.MB_ICONWARNING)
		exit(1)
		return
	}
	if box(askTitle, askText(f, p, dir), windows.MB_OKCANCEL|windows.MB_ICONWARNING) != idOK {
		exit(1)
		return
	}
	if unpacking, err := download(p, dir); err != nil {
		slog.Error("Mesa not put in place", "dir", dir, "unpacking", unpacking, "err", err)
		if !errors.Is(err, context.Canceled) {
			box(askTitle, manualText(f, p, dir, failedWhy(err, unpacking)), windows.MB_ICONERROR)
		}
		exit(1)
		return
	}
	cmd := exec.Command(exe, os.Args[1:]...) // Cogpit's own executable with its own arguments
	if err := cmd.Start(); err != nil {
		box(askTitle, fmt.Sprintf("Mesa is in place beside cogpit.exe in\n%s\nStarting Cogpit again failed: %v\nStart it again yourself.", dir, err), windows.MB_ICONWARNING)
		exit(1)
		return
	}
	slog.Info("Mesa in place: Cogpit starts again", "dir", dir, "pid", cmd.Process.Pid)
	exit(0)
}

// download puts p into dir while a box shows how far it got; its Cancel stops the download. The
// box's OK button is disabled: the download's goroutine presses it when Mesa is in place, or when
// putting it in place failed. unpacking is whether the archive had come and matched its SHA-256.
func download(p Pin, dir string) (unpacking bool, err error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var unpacked atomic.Bool
	go func() {
		hwnd := waitForBox(downloadTitle, 10*time.Second)
		if hwnd != 0 {
			enableOK(hwnd, false)
		}
		err := fetch.Install(ctx, fetch.Client(), fetch.Source{URL: p.URL(), SHA256: p.SHA256}, dir, func(pr fetch.Progress) {
			if pr.Step == fetch.Unpacking {
				unpacked.Store(true)
			}
			setBoxText(hwnd, progressText(p, pr.String()))
		})
		if err == nil {
			setBoxText(hwnd, progressText(p, "Mesa is in place"))
		}
		done <- err
		if hwnd != 0 {
			pressOK(hwnd)
		}
	}()
	if box(downloadTitle, progressText(p, "Starting the download"), windows.MB_OKCANCEL|windows.MB_ICONINFORMATION) != idOK {
		cancel()
	}
	err = <-done
	return unpacked.Load(), err
}

// box shows a MessageBox in front and returns the button pressed (0 when it could not show).
func box(title, text string, flags uint32) int32 {
	t, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return 0
	}
	c, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	r, _ := windows.MessageBox(0, t, c, flags|windows.MB_SETFOREGROUND)
	return r
}

// waitForBox returns the box titled title once it is open, 0 after within.
func waitForBox(title string, within time.Duration) uintptr {
	class, _ := windows.UTF16PtrFromString(dialogClass)
	t, _ := windows.UTF16PtrFromString(title)
	for end := time.Now().Add(within); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		// #nosec G103 -- the strings' addresses for the duration of the call.
		if hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(t))); hwnd != 0 {
			return hwnd
		}
	}
	return 0
}

// setBoxText replaces an open box's text; nothing without one.
func setBoxText(hwnd uintptr, text string) {
	if hwnd == 0 {
		return
	}
	if t, err := windows.UTF16PtrFromString(text); err == nil {
		// #nosec G103 -- the string's address for the duration of the call.
		_, _, _ = procSetDlgItemTextW.Call(hwnd, boxTextID, uintptr(unsafe.Pointer(t)))
	}
}

// enableOK enables or disables a box's OK button.
func enableOK(hwnd uintptr, on bool) {
	if b, _, _ := procGetDlgItem.Call(hwnd, idOK); b != 0 {
		var v uintptr
		if on {
			v = 1
		}
		_, _, _ = procEnableWindow.Call(b, v)
	}
}

// pressOK ends a box as its OK button would, disabled or not.
func pressOK(hwnd uintptr) { _, _, _ = procPostMessageW.Call(hwnd, wmCommand, idOK, 0) }

// hideThreadWindows hides this thread's visible windows: the one Fyne opened and could not draw
// in, which would otherwise stay white behind the boxes.
func hideThreadWindows() {
	cb := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		if v, _, _ := procIsWindowVisible.Call(hwnd); v != 0 {
			_, _, _ = procShowWindow.Call(hwnd, swHide)
		}
		return 1 // go on
	})
	_, _, _ = procEnumThreadWins.Call(uintptr(windows.GetCurrentThreadId()), cb, 0)
}

// logPath is Cogpit's log (cmd/cogpit's newLogger).
func logPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return `%LOCALAPPDATA%\Cogpit\cogpit.log`
	}
	return filepath.Join(dir, "Cogpit", "cogpit.log")
}
