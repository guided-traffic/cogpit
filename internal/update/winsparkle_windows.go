//go:build windows

package update

import (
	"crypto/ed25519"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
)

// On Windows Cogpit updates through WinSparkle (D-111; docs/security/updates.md#windows), Sparkle's
// Windows port, which the setup puts beside cogpit.exe. The binary does not link it: load reads
// WinSparkle.dll from the app's folder at run time (no cgo), so every other build, the tests and a
// copy outside the setup's folder simply have no updater, and Unavailable says why.
//
//   - load: a release build, installed by the setup (cogpit.exe's folder is the InstallLocation of
//     Cogpit's uninstall entry, the user's or the machine's), WinSparkle.dll beside it, and the feed
//     and the update key built in (make dist-windows sets windowsFeedURL and edPublicKey).
//   - start: WinSparkle is told the feed, the key, Cogpit's name and running version, where to keep
//     its state (HKCU\Software\guided-traffic\Cogpit\WinSparkle) and English, its own scheduled
//     checks off, and started. With the preference Check for updates automatically on, Cogpit
//     reads the signed feed itself at once and every hour (appcast.go): a newer release than the
//     running one, not the one the user skipped in WinSparkle's window, shows the Update button.
//   - check (the button, Help › Check for Updates…): WinSparkle's window checks the feed and shows
//     the release with its notes. Install downloads the setup, checks its EdDSA signature with the
//     key and runs it as the feed says ("/S /relaunch"), then asks Cogpit to quit: the functions
//     OnShutdownRequest registered close the window. The setup waits for cogpit.exe to be free,
//     installs, and starts the new release.
//
// Cogpit's own reading goes through Go's HTTP client, which follows HTTPS_PROXY but not Windows'
// proxy settings; WinSparkle's own requests go through WinINet, which follows them. Behind a proxy
// only Windows knows, the Update button stays away and Help › Check for Updates… still finds the
// release.

var (
	// windowsFeedURL and edPublicKey are the update feed and the public half of the update key (base64,
	// as Sparkle's generate_keys prints it), set by the linker in a release (make dist-windows: -X);
	// empty in every other build, which has no updater.
	windowsFeedURL string
	edPublicKey    string

	// checkInterval is how often the scheduled checks read the feed: Sparkle's, an hour (D-4).
	checkInterval = time.Hour
	// feedTimeout bounds a read of the feed.
	feedTimeout = 30 * time.Second

	executable     = os.Executable
	runningVersion = func() (string, bool) { i := buildinfo.Read(); return i.Version, i.Release }
	// installLocations reads the folders Cogpit's uninstall entries name, the user's and the
	// machine's.
	installLocations = readInstallLocations
	// skippedVersion reads the release the user skipped in WinSparkle's window.
	skippedVersion = func() string { return readRegString(syscall.HKEY_CURRENT_USER, winSparkleKey, "SkipThisVersion") }
	fetchFeed      = httpFeed
)

const (
	// uninstallKey is the setup's uninstall entry (packaging/windows/cogpit.nsi), in HKCU for a
	// per-user installation and in HKLM's 64-bit view for one for all users.
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\io.guided-traffic.cogpit`
	// winSparkleKey is where WinSparkle keeps its state; the uninstaller removes it.
	winSparkleKey = `Software\guided-traffic\Cogpit\WinSparkle`
	// keyWOW6464Key reads HKLM's 64-bit view, where the setup writes the machine's entry.
	keyWOW6464Key = 0x0100
)

// winSparkle is the Windows updater's state: one per process, as the notice is.
var winSparkle winSparkleState

type winSparkleState struct {
	mu      sync.Mutex
	dll     *syscall.DLL
	running string            // the version this process runs
	key     ed25519.PublicKey // the update key, for the feed Cogpit reads itself
	stop    chan struct{}     // closed to end the scheduled checks; nil while they are off
	quits   []func()          // OnShutdownRequest's
	keep    []uintptr         // the callbacks handed to WinSparkle, kept for the process's life
	strs    []unsafe.Pointer  // the strings handed to WinSparkle (it may keep the pointers)
}

// load checks that this process is the setup's cogpit.exe with WinSparkle beside it and the feed
// and key built in, and loads WinSparkle.dll.
func load() error {
	if windowsFeedURL == "" {
		return ErrNoFeed
	}
	key, err := decodeKey(edPublicKey)
	if err != nil {
		return ErrNoKey
	}
	exe, err := executable()
	if err != nil {
		return ErrNotFromSetup
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	version, release := runningVersion()
	if !release || !installedBySetup(filepath.Dir(exe)) {
		return ErrNotFromSetup
	}
	// The full path: never the DLL search order, which would take a WinSparkle.dll from elsewhere.
	dll, err := syscall.LoadDLL(filepath.Join(filepath.Dir(exe), "WinSparkle.dll"))
	if err != nil {
		return ErrNoWinSparkle
	}
	for _, name := range winSparkleProcs {
		if _, err := dll.FindProc(name); err != nil {
			_ = dll.Release()
			return ErrNoWinSparkle
		}
	}
	winSparkle.mu.Lock()
	winSparkle.dll, winSparkle.running, winSparkle.key = dll, version, key
	winSparkle.mu.Unlock()
	return nil
}

// winSparkleProcs are the functions of WinSparkle's C API (winsparkle.h, 0.9) Cogpit calls.
var winSparkleProcs = []string{
	"win_sparkle_set_appcast_url", "win_sparkle_set_eddsa_public_key", "win_sparkle_set_app_details",
	"win_sparkle_set_registry_path", "win_sparkle_set_lang", "win_sparkle_set_automatic_check_for_updates",
	"win_sparkle_set_can_shutdown_callback", "win_sparkle_set_shutdown_request_callback",
	"win_sparkle_set_did_not_find_update_callback", "win_sparkle_set_update_skipped_callback",
	"win_sparkle_set_update_postponed_callback", "win_sparkle_set_update_dismissed_callback",
	"win_sparkle_init", "win_sparkle_check_update_with_ui",
}

// installedBySetup reports whether dir is the folder one of Cogpit's uninstall entries names.
func installedBySetup(dir string) bool {
	for _, loc := range installLocations() {
		if loc != "" && strings.EqualFold(filepath.Clean(loc), filepath.Clean(dir)) {
			return true
		}
	}
	return false
}

func readInstallLocations() []string {
	return []string{
		readRegString(syscall.HKEY_CURRENT_USER, uninstallKey, "InstallLocation"),
		readRegString(syscall.HKEY_LOCAL_MACHINE, uninstallKey, "InstallLocation"),
	}
}

// readRegString reads a string value, "" when it or its key is missing.
func readRegString(root syscall.Handle, path, name string) string {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var h syscall.Handle
	if syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ|keyWOW6464Key, &h) != nil {
		return ""
	}
	defer func() { _ = syscall.RegCloseKey(h) }()
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var typ, size uint32
	if syscall.RegQueryValueEx(h, n, nil, &typ, nil, &size) != nil || size == 0 ||
		(typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ) {
		return ""
	}
	buf := make([]uint16, size/2+1)
	if syscall.RegQueryValueEx(h, n, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size) != nil {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func startFailure() error { return nil }

// start configures WinSparkle and starts it, then the scheduled checks when they are on.
func start(automaticChecks bool) {
	w := &winSparkle
	w.mu.Lock()
	w.cstr("win_sparkle_set_appcast_url", windowsFeedURL)
	w.cstr("win_sparkle_set_eddsa_public_key", edPublicKey)
	w.call("win_sparkle_set_app_details", w.wstr("guided-traffic"), w.wstr("Cogpit"), w.wstr(w.running))
	w.cstr("win_sparkle_set_registry_path", winSparkleKey)
	w.cstr("win_sparkle_set_lang", "en")
	// WinSparkle's own scheduled checks would show its window unasked; Cogpit's show the button.
	// Setting them also keeps WinSparkle from asking whether to check at all.
	w.call("win_sparkle_set_automatic_check_for_updates", 0)
	w.callback("win_sparkle_set_can_shutdown_callback", func() uintptr {
		winSparkle.mu.Lock()
		defer winSparkle.mu.Unlock()
		if len(winSparkle.quits) > 0 {
			return 1
		}
		return 0
	})
	w.callback("win_sparkle_set_shutdown_request_callback", func() uintptr {
		winSparkle.mu.Lock()
		fns := append([]func(){}, winSparkle.quits...)
		winSparkle.mu.Unlock()
		for _, fn := range fns {
			go fn()
		}
		return 0
	})
	// The update session ended without an install: the button goes, as Sparkle's gentle reminders
	// end on macOS, and the next scheduled check brings it back unless the release was skipped.
	for _, name := range []string{"win_sparkle_set_did_not_find_update_callback", "win_sparkle_set_update_skipped_callback",
		"win_sparkle_set_update_postponed_callback", "win_sparkle_set_update_dismissed_callback"} {
		w.callback(name, func() uintptr { notice.set(false, ""); return 0 })
	}
	w.call("win_sparkle_init")
	w.mu.Unlock()
	setAutomaticChecks(automaticChecks)
}

// check shows WinSparkle's window, which checks the feed and offers the release it finds; it
// returns at once. No lock: the DLL is set once by load, and WinSparkle's callbacks take it.
func check() { winSparkle.call("win_sparkle_check_update_with_ui") }

func setAutomaticChecks(on bool) {
	w := &winSparkle
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case on && w.stop == nil:
		w.stop = make(chan struct{})
		go scheduledChecks(w.stop)
	case !on && w.stop != nil:
		close(w.stop)
		w.stop = nil
	}
}

// scheduledChecks reads the feed at once and every checkInterval until stop closes.
func scheduledChecks(stop <-chan struct{}) {
	defer func() { _ = recover() }() // D-11: a check's panic never ends the app
	t := time.NewTicker(checkInterval)
	defer t.Stop()
	for {
		_ = refreshNotice()
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// refreshNotice reads the feed into the notice. A feed that cannot be read or verified changes
// nothing: the button keeps what the last good read said.
func refreshNotice() error {
	winSparkle.mu.Lock()
	running, key := winSparkle.running, winSparkle.key
	winSparkle.mu.Unlock()
	data, err := fetchFeed(windowsFeedURL)
	if err != nil {
		return err
	}
	version, ok, err := newestRelease(data, key, feedSystem(), running, skippedVersion())
	if err != nil {
		return err
	}
	notice.set(ok, version)
	return nil
}

// feedSystem is this build's sparkle:os in the feed: windows-x64 or windows-arm64.
func feedSystem() string {
	if runtime.GOARCH == "amd64" {
		return "windows-x64"
	}
	return "windows-" + runtime.GOARCH
}

// httpFeed fetches the feed past Pages' cache, bounded in time and size.
func httpFeed(url string) ([]byte, error) {
	c := &http.Client{Timeout: feedTimeout}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the update feed: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxFeedSize+1))
}

// WinSparkle asks the app to quit once it started the setup (Updater.OnShutdownRequest).
func init() {
	shutdownHook = func(fn func()) {
		winSparkle.mu.Lock()
		defer winSparkle.mu.Unlock()
		winSparkle.quits = append(winSparkle.quits, fn)
	}
}

// call calls one of WinSparkle's functions.
func (w *winSparkleState) call(name string, args ...uintptr) uintptr {
	p, err := w.dll.FindProc(name)
	if err != nil {
		return 0 // load found every one
	}
	r, _, _ := p.Call(args...)
	return r
}

// cstr calls name with s as a C string, kept for the process's life; w.mu is held.
func (w *winSparkleState) cstr(name, s string) {
	b, err := syscall.BytePtrFromString(s)
	if err != nil {
		return
	}
	w.strs = append(w.strs, unsafe.Pointer(b))
	w.call(name, uintptr(unsafe.Pointer(b)))
}

// wstr is s as a wide C string, kept for the process's life; w.mu is held.
func (w *winSparkleState) wstr(s string) uintptr {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("")
	}
	w.strs = append(w.strs, unsafe.Pointer(p))
	return uintptr(unsafe.Pointer(p))
}

// callback hands fn to WinSparkle through set (one of its callback setters); WinSparkle calls it
// on a thread of its own. w.mu is held.
func (w *winSparkleState) callback(set string, fn func() uintptr) {
	cb := syscall.NewCallbackCDecl(func() uintptr {
		defer func() { _ = recover() }() // D-11: a callback's panic never ends the app
		return fn()
	})
	w.keep = append(w.keep, cb)
	w.call(set, cb)
}
