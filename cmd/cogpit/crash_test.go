package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// crashHelperEnv makes the test binary act as a Cogpit that crashes (TestCrashReportSurvivesAPanic).
const crashHelperEnv = "COGPIT_CRASH_HELPER_DIR"

// TestCrashHelper is not a test: run as a subprocess with crashHelperEnv set, it captures crashes
// as main does and panics on the main goroutine with a deferred function on the way, as a panic in
// a Fyne callback would.
func TestCrashHelper(t *testing.T) {
	dir := os.Getenv(crashHelperEnv)
	if dir == "" {
		t.Skip("a helper process for TestCrashReportSurvivesAPanic")
	}
	_, done := captureCrashes(dir)
	_ = done // a crashing run never gets to run it
	defer func() { _ = os.WriteFile(filepath.Join(dir, "deferred-ran"), nil, 0o600) }()
	panic("cogpit test crash on the main goroutine")
}

// A panic on the main goroutine leaves a crash report although deferred functions ran first; the
// next start moves it to crashes/ and returns its path; a clean run leaves no report behind.
func TestCrashReportSurvivesAPanic(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
	cmd.Env = append(os.Environ(), crashHelperEnv+"="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the helper did not crash:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "deferred-ran")); err != nil {
		t.Fatal("the helper's deferred function did not run: the test does not model a main-goroutine panic")
	}
	report, err := os.ReadFile(filepath.Join(dir, crashFile))
	if err != nil || !strings.Contains(string(report), "panic: cogpit test crash on the main goroutine") ||
		!strings.Contains(string(report), "goroutine") {
		t.Fatalf("crash report %q, %v", report, err)
	}

	previous, done := captureCrashes(dir) // the next start
	if !strings.HasPrefix(previous, filepath.Join(dir, crashDir, "crash-")) {
		t.Fatalf("previous report %q", previous)
	}
	if kept, err := os.ReadFile(previous); err != nil || string(kept) != string(report) {
		t.Errorf("the kept report differs: %v", err)
	}
	// Windows sets no file modes: there the user profile's permissions protect the report.
	if fi, err := os.Stat(previous); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("the report's mode: %v, %v", fi, err)
	}
	done() // this run ends cleanly
	if _, err := os.Stat(filepath.Join(dir, crashFile)); !os.IsNotExist(err) {
		t.Errorf("a clean run left %s: %v", crashFile, err)
	}
	again, doneAgain := captureCrashes(dir)
	doneAgain() // the open crash.log would keep Windows from removing the test's folder
	if again != "" {
		t.Errorf("a clean run is reported as a crash: %q", again)
	}
}

// Only the newest maxCrashReports reports are kept.
func TestCrashReportsArePruned(t *testing.T) {
	reports := filepath.Join(t.TempDir(), crashDir)
	if err := os.MkdirAll(reports, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := range maxCrashReports + 3 {
		name := filepath.Join(reports, "crash-2026-10-12T10-00-"+string(rune('a'+i))+".log")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pruneCrashReports(reports)
	entries, _ := os.ReadDir(reports)
	if len(entries) != maxCrashReports || entries[0].Name() != "crash-2026-10-12T10-00-d.log" {
		t.Errorf("kept %d, oldest %s", len(entries), entries[0].Name())
	}
}
