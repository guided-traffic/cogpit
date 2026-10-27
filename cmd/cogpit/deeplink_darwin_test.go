//go:build darwin

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit/prereq"
)

// deeplink_darwin.m itself, built with a native harness into a process of its own (an app's
// launch needs its process's main thread) and run: the handler receives "open URL" Apple events
// before AppKit finished launching and after (its own handlers did not replace Cogpit's), a second
// install changes nothing, and a link longer than Cogpit reads arrives cut to one byte more than
// engine.MaxDeepLink, which the queue then drops. The harness sends its events to itself only:
// nothing is registered with Launch Services and no URL is opened. Needs clang.
func TestDeepLinkAppleEvents(t *testing.T) {
	if maxReceivedLink != engine.MaxDeepLink+1 {
		t.Fatalf("COGPIT_MAX_DEEPLINK is %d, want engine.MaxDeepLink+1 (%d)", maxReceivedLink, engine.MaxDeepLink+1)
	}
	clang := prereq.LookPath(t, "clang")
	bin := filepath.Join(t.TempDir(), "deeplink_harness")
	build := exec.Command(clang, "-fno-objc-arc", "-framework", "Cocoa", "-framework", "CoreServices", "-I.", "-o", bin,
		filepath.Join("testdata", "deeplink_harness.m"), "deeplink_darwin.m")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the harness: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("the harness: %v\n%s", err, out)
	}
	want := []string{
		"link 23 cogpit://open?view=pods",
		"link 81 cogpit://open?context=kind-dev+%5Bconfig%5D&view=pods&namespace=team-a&name=web-0",
		"link 2049 cogpit://open?view=" + strings.Repeat("x", 21),
		"link 31 cogpit://open?context=%C3%BCber",
		"done",
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the harness printed\n%s\nwant\n%s", out, strings.Join(want, "\n"))
	}
}

// The queue drops what the handler hands over cut, longer than a link Cogpit reads.
func TestReceivedLinkTooLongIsDropped(t *testing.T) {
	q := engine.NewDeepLinkQueue(nil)
	q.Deliver(strings.Repeat("x", int(maxReceivedLink)))
	q.Deliver("cogpit://open?view=pods")
	if n := q.Pending(); n != 1 {
		t.Errorf("%d waiting, want the one link that fits", n)
	}
}
