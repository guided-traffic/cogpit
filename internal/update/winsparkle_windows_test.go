//go:build windows

package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSetup puts the Windows updater's surroundings in place for one test: a release build of
// version running, installed by the setup into a folder of its own with WinSparkle.dll beside
// cogpit.exe when dll names one (the real DLL of make winsparkle, or none), the feed and the key
// built in. It returns the folder and the key's private half.
func fakeSetup(t *testing.T, running, dll string) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Cogpit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if dll != "" {
		b, err := os.ReadFile(dll)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "WinSparkle.dll"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saved := []any{windowsFeedURL, edPublicKey, executable, runningVersion, installLocations, skippedVersion, fetchFeed, winSparkle.dll}
	t.Cleanup(func() {
		if winSparkle.dll != nil && winSparkle.dll != saved[7] {
			_ = winSparkle.dll.Release()
		}
		windowsFeedURL, edPublicKey = saved[0].(string), saved[1].(string)
		executable = saved[2].(func() (string, error))
		runningVersion = saved[3].(func() (string, bool))
		installLocations = saved[4].(func() []string)
		skippedVersion = saved[5].(func() string)
		fetchFeed = saved[6].(func(string) ([]byte, error))
		winSparkle.dll, winSparkle.running, winSparkle.key = nil, "", nil
		notice.set(false, "")
	})
	windowsFeedURL = "https://example.invalid/appcast-windows.xml"
	edPublicKey = base64.StdEncoding.EncodeToString(pub)
	executable = func() (string, error) { return filepath.Join(dir, "cogpit.exe"), nil }
	runningVersion = func() (string, bool) { return running, true }
	// The entry names the folder as Windows keeps it, in another case than the process's path.
	installLocations = func() []string { return []string{"", strings.ToUpper(dir)} }
	skippedVersion = func() string { return "" }
	fetchFeed = func(string) ([]byte, error) { return nil, errors.New("no feed in this test") }
	return dir, priv
}

// winSparkleDLL is the x64 WinSparkle.dll make winsparkle fetched, or "" (the test then skips the
// parts that need it).
func winSparkleDLL(t *testing.T) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join("..", "..", ".cache", "winsparkle", "*", "x64", "WinSparkle.dll"))
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

// Every way a Windows Cogpit is not the setup's says why: a build without the feed or the key, a
// development build, a copy outside the setup's folder, the setup's folder without WinSparkle.
func TestWindowsUpdaterUnavailable(t *testing.T) {
	for name, c := range map[string]struct {
		change func(dir string)
		want   error
	}{
		"no feed":           {func(string) { windowsFeedURL = "" }, ErrNoFeed},
		"no key":            {func(string) { edPublicKey = "" }, ErrNoKey},
		"a key that is not": {func(string) { edPublicKey = "bm90IGEga2V5" }, ErrNoKey},
		"development build": {func(string) { runningVersion = func() (string, bool) { return "dev", false } }, ErrNotFromSetup},
		"another folder":    {func(string) { installLocations = func() []string { return []string{`C:\Elsewhere\Cogpit`} } }, ErrNotFromSetup},
		"never installed":   {func(string) { installLocations = func() []string { return []string{"", ""} } }, ErrNotFromSetup},
		"no WinSparkle":     {func(string) {}, ErrNoWinSparkle},
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := fakeSetup(t, "0.35.0", "")
			c.change(dir)
			if err := New().Unavailable(); !errors.Is(err, c.want) {
				t.Errorf("Unavailable() = %v, want %v", err, c.want)
			}
		})
	}
}

// In the setup's folder with WinSparkle 0.9 beside it, the updater loads, and the DLL has every
// function Cogpit calls.
func TestWindowsUpdaterLoadsWinSparkle(t *testing.T) {
	dll := winSparkleDLL(t)
	if dll == "" {
		t.Skip("no WinSparkle.dll in .cache/winsparkle: run make winsparkle")
	}
	fakeSetup(t, "0.35.0", dll)
	if err := New().Unavailable(); err != nil {
		t.Fatalf("Unavailable() = %v, want nil", err)
	}
	for _, name := range winSparkleProcs {
		if _, err := winSparkle.dll.FindProc(name); err != nil {
			t.Errorf("WinSparkle.dll has no %s: %v", name, err)
		}
	}
}

// A scheduled check reads the signed feed into the Update button: a newer release shows it, the
// release the user skipped in WinSparkle's window does not, and a feed that does not verify or
// cannot be read changes nothing.
func TestWindowsScheduledCheckShowsTheButton(t *testing.T) {
	_, priv := fakeSetup(t, "0.35.0", "")
	winSparkle.running = "0.35.0"
	winSparkle.key = priv.Public().(ed25519.PublicKey)
	feed := signFeed(testFeed(testItem("0.36.0", "windows-x64", ""), testItem("0.36.1", "windows-arm64", "")), priv)
	fetchFeed = func(string) ([]byte, error) { return feed, nil }

	if err := refreshNotice(); err != nil {
		t.Fatal(err)
	}
	if v, ok := notice.get(); !ok || v != "0.36.0" {
		t.Errorf("after a check: %q, %v; want 0.36.0", v, ok)
	}

	_, other, _ := ed25519.GenerateKey(rand.Reader)
	fetchFeed = func(string) ([]byte, error) {
		return signFeed(testFeed(testItem("0.99.0", "windows-x64", "")), other), nil
	}
	if err := refreshNotice(); err == nil {
		t.Error("a feed signed with another key: no error")
	}
	fetchFeed = func(string) ([]byte, error) { return nil, errors.New("offline") }
	if err := refreshNotice(); err == nil {
		t.Error("an unreadable feed: no error")
	}
	if v, ok := notice.get(); !ok || v != "0.36.0" {
		t.Errorf("after failed reads: %q, %v; want 0.36.0 kept", v, ok)
	}

	fetchFeed = func(string) ([]byte, error) { return feed, nil }
	skippedVersion = func() string { return "0.36.0" }
	if err := refreshNotice(); err != nil {
		t.Fatal(err)
	}
	if v, ok := notice.get(); ok {
		t.Errorf("the skipped release: %q shown", v)
	}
}

func TestFeedSystem(t *testing.T) {
	if got := feedSystem(); got != "windows-x64" && got != "windows-arm64" {
		t.Errorf("feedSystem() = %q", got)
	}
}
