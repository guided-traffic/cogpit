// Package mesa is Cogpit's remedy for a Windows computer without an OpenGL Fyne can draw with
// (D-142). Fyne draws through OpenGL 2.1; Windows Server and virtual machines without a GPU driver
// offer less, and the window stays white or never opens. Mesa's software OpenGL draws instead:
// its opengl32.dll and libgallium_wgl.dll beside cogpit.exe, the first place Windows looks.
//
//   - Prefer names Mesa's llvmpipe driver for Cogpit's own process when that opengl32.dll is the one
//     it loaded: without GALLIUM_DRIVER Mesa tries d3d12 and zink first, and on such a computer the
//     first can set up and still fail.
//   - Watch notices Fyne's report that it found no OpenGL to draw with; Offer then asks to download
//     the Mesa release this build pins, puts it in place (internal/mesa/fetch) and starts Cogpit
//     again, or says what to do by hand.
//
// Each system's file has Prefer and Offer (mesa_windows.go; mesa_other.go does nothing); the rules
// and the dialogs' texts are here, tested on every system.
//
// Layer L4. It imports internal/mesa/fetch from its Windows file only.
package mesa

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
)

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

// version and archiveSHA256 are the Mesa release Offer downloads and its archive's SHA-256: the
// Makefile's MESA_VERSION and MESA_SHA256, which the linker writes into a release build of
// cogpit.exe (WIN_APP_LDFLAGS; TestMakefileBuildsThePinIntoTheApp). Empty in other builds, which
// offer only the steps by hand.
var (
	version       string
	archiveSHA256 string
)

// Pin is a release of mesa-dist-win's MSVC build and its archive's SHA-256.
type Pin struct {
	Version, SHA256 string
}

// pinned is the build's pin; ok is false without one.
func pinned() (p Pin, ok bool) {
	return Pin{Version: version, SHA256: archiveSHA256}, version != "" && archiveSHA256 != ""
}

// Archive is the release archive's name; "mesa3d-<version>-release-msvc.7z" without a version.
func (p Pin) Archive() string {
	v := p.Version
	if v == "" {
		v = "<version>"
	}
	return "mesa3d-" + v + "-release-msvc.7z"
}

// URL is where mesa-dist-win's release has the archive.
func (p Pin) URL() string {
	return "https://github.com/pal1000/mesa-dist-win/releases/download/" + p.Version + "/" + p.Archive()
}

// releasesURL is where a person finds mesa-dist-win's releases.
const releasesURL = "https://github.com/pal1000/mesa-dist-win/releases"

// Failure is Fyne's report that it found no OpenGL it can draw with: the reason it logs and the
// error behind it ("" when none came).
type Failure struct {
	Reason, Cause string
}

// String is the report as one line, Fyne's words.
func (f Failure) String() string {
	if f.Cause == "" {
		return f.Reason
	}
	return f.Reason + ": " + f.Cause
}

// glReason reports whether reason is one Fyne 2.8 logs when the window gets no OpenGL it can draw
// with: GLFW refused the context ("window creation error", glfw's window_desktop.go; the window
// never opens), go-gl found a function missing ("failed to initialise OpenGL", painter/gl's
// gl_core.go; the window stays white), a shader did not compile (painter/gl's draw.go).
func glReason(reason string) bool {
	return reason == "window creation error" || reason == "failed to initialise OpenGL" ||
		strings.HasPrefix(reason, "Failed to compile shader ")
}

// Watch passes every record on to h, then calls fire, once per process, when the first of Fyne's
// reports that it found no OpenGL to draw with has reached h. Fyne writes a report through the
// standard log package, which slog.SetDefault routes here, as lines of their own:
// "Fyne error:  <reason>", then "  Cause: <error>" when there is one, then "  At: <file:line>".
// fire runs on the goroutine that logged the cause, Fyne's main one, before Fyne goes on: after
// "window creation error" Fyne ends the process. It must not write through the standard log
// package, whose lock that goroutine holds.
func Watch(h slog.Handler, fire func(Failure)) slog.Handler {
	return watch{Handler: h, state: &watchState{fire: fire}}
}

type watch struct {
	slog.Handler
	state *watchState
}

// watchState is shared by a watch and the handlers WithAttrs and WithGroup make of it.
type watchState struct {
	fire    func(Failure)
	mu      sync.Mutex
	pending string // a GL reason whose next line has not come yet
	fired   bool
}

func (w watch) Handle(ctx context.Context, r slog.Record) error {
	err := w.Handler.Handle(ctx, r)
	if f, ok := w.state.note(r.Message); ok {
		w.state.fire(f)
	}
	return err
}

func (w watch) WithAttrs(attrs []slog.Attr) slog.Handler {
	return watch{Handler: w.Handler.WithAttrs(attrs), state: w.state}
}

func (w watch) WithGroup(name string) slog.Handler {
	return watch{Handler: w.Handler.WithGroup(name), state: w.state}
}

// note reads one logged line and returns the failure to report, once: a GL reason waits for the
// line after it, its cause, or reports without one when another line comes.
func (s *watchState) note(msg string) (Failure, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fired {
		return Failure{}, false
	}
	if reason, ok := strings.CutPrefix(msg, "Fyne error:"); ok {
		s.pending = ""
		if reason = strings.TrimSpace(reason); glReason(reason) {
			s.pending = reason
		}
		return Failure{}, false
	}
	if s.pending == "" {
		return Failure{}, false
	}
	f := Failure{Reason: s.pending}
	if cause, ok := strings.CutPrefix(strings.TrimSpace(msg), "Cause:"); ok {
		f.Cause = strings.TrimSpace(cause)
	}
	s.pending, s.fired = "", true
	return f, true
}

// The dialogs' texts (mesa_windows.go). MessageBox wraps long lines; Ctrl+C copies a box's text.

// askText asks to download p into dir.
func askText(f Failure, p Pin, dir string) string {
	return fmt.Sprintf("Cogpit draws its window through OpenGL 2.1, and this computer offers none it can draw with:\n%s\n\n"+
		"Mesa's software OpenGL draws instead. Cogpit can download Mesa %s (%s) from github.com/pal1000/mesa-dist-win, "+
		"check it against the SHA-256 this Cogpit carries, put its opengl32.dll and libgallium_wgl.dll beside cogpit.exe in\n%s\n"+
		"and start again. Drawing in software takes CPU time.\n\n"+
		"OK downloads Mesa, Cancel quits Cogpit.", f, p.Version, p.Archive(), dir)
}

// manualText says why Cogpit does not put Mesa in place itself and what to do by hand.
func manualText(f Failure, p Pin, dir, why string) string {
	return fmt.Sprintf("Cogpit draws its window through OpenGL 2.1, and this computer offers none it can draw with:\n%s\n\n%s\n\n"+
		"Mesa's software OpenGL draws instead: put opengl32.dll and libgallium_wgl.dll from the x64 folder of %s (%s) "+
		"beside cogpit.exe in\n%s\nand start Cogpit again.", f, why, p.Archive(), releasesURL, dir)
}

// besideText says that Mesa beside cogpit.exe did not draw either.
func besideText(f Failure, dll, log string) string {
	return fmt.Sprintf("Cogpit found no OpenGL it can draw with:\n%s\n\n"+
		"It loaded Mesa's opengl32.dll beside cogpit.exe,\n%s,\nwhich did not draw either. "+
		"Removing opengl32.dll and libgallium_wgl.dll from that folder brings back the computer's own OpenGL. Cogpit's log:\n%s",
		f, dll, log)
}

// progressText is the download's box: its first line is the longest, so that the box, sized for
// the text it opens with, holds every progress line after it.
func progressText(p Pin, line string) string {
	return fmt.Sprintf("Downloading Mesa %s (%s) and putting it beside cogpit.exe.\n%s\n\n"+
		"Cogpit starts again by itself when Mesa is in place. Cancel stops it.", p.Version, p.Archive(), line)
}
