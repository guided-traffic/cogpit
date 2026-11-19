package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Crash reports (credentials.md: "a local crash report follows the log rule"; no upload). The Go
// runtime writes an unhandled panic or fatal error to standard error; a GUI app's standard error
// is gone with its terminal, and a crash the owner saw left nothing to look at. captureCrashes
// makes the runtime write the same text into a file as well (runtime/debug.SetCrashOutput): <user
// config dir>/Cogpit/crash.log, 0600, which a clean exit removes again. A crash leaves it; the next
// start moves it to crashes/crash-<time>.log, keeps the newest maxCrashReports, and the window says
// where it is.

const (
	crashFile       = "crash.log"
	crashDir        = "crashes"
	maxCrashReports = 10
)

// captureCrashes starts writing crash output into dir/crash.log. It returns the report a previous
// run left ("" for none) and done, which the caller runs only when the process ends normally: on a
// panic of the main goroutine the deferred functions run before the runtime writes the crash, so
// done must not be deferred. Without a usable dir it captures nothing and says so on stderr.
func captureCrashes(dir string) (previous string, done func()) {
	done = func() {}
	if dir == "" {
		return "", done
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "cogpit: crash reports disabled:", err)
		return "", done
	}
	cur := filepath.Join(dir, crashFile)
	previous = keepPreviousCrash(dir, cur)

	f, err := os.OpenFile(cur, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cogpit: crash reports disabled:", err)
		return previous, done
	}
	defer f.Close() // SetCrashOutput keeps a duplicate of the descriptor
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		fmt.Fprintln(os.Stderr, "cogpit: crash reports disabled:", err)
		_ = os.Remove(cur)
		return previous, done
	}
	return previous, func() {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		if fi, err := os.Stat(cur); err == nil && fi.Size() == 0 {
			_ = os.Remove(cur)
		}
	}
}

// keepPreviousCrash moves a crash report the last run left at cur into dir/crashes, named by its
// time, prunes that folder to the newest maxCrashReports, and returns the report's new path; ""
// when the last run ended cleanly (no file, or an empty one, which it removes).
func keepPreviousCrash(dir, cur string) string {
	fi, err := os.Stat(cur)
	switch {
	case err != nil:
		return ""
	case fi.Size() == 0 || !fi.Mode().IsRegular():
		_ = os.Remove(cur)
		return ""
	}
	reports := filepath.Join(dir, crashDir)
	if err := os.MkdirAll(reports, 0o700); err != nil {
		return cur
	}
	kept := filepath.Join(reports, "crash-"+fi.ModTime().Format("2006-01-02T15-04-05")+".log")
	if err := os.Rename(cur, kept); err != nil {
		return cur
	}
	pruneCrashReports(reports)
	return kept
}

// pruneCrashReports removes all but the newest maxCrashReports reports (their names sort by time).
func pruneCrashReports(reports string) {
	var names []string
	_ = filepath.WalkDir(reports, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), "crash-") {
			names = append(names, p)
		}
		if d != nil && d.IsDir() && p != reports {
			return filepath.SkipDir
		}
		return nil
	})
	slices.Sort(names)
	for len(names) > maxCrashReports {
		_ = os.Remove(names[0])
		names = names[1:]
	}
}

// crashReportDir is where captureCrashes keeps the reports: Cogpit's folder in the user's
// configuration directory, as the audit log and the journals; "" without one.
func crashReportDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Cogpit")
}

// crashNotice is what the window says about a crash report the last run left, the time in the
// user's zone and format (D-31). The report's file name keeps its machine-readable time.
func crashNotice(path string, at time.Time, tf model.TimeFormat) string {
	return "Cogpit ended unexpectedly on " + tf.Format(at) + ". The crash report is in " + path +
		"; please attach it when you report the problem."
}
