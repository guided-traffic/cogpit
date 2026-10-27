//go:build darwin && cgo

package update

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sparkleFramework is the Sparkle.framework make sparkle keeps in .cache (or
// $COGPIT_SPARKLE_FRAMEWORK); the test is skipped without one.
func sparkleFramework(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("COGPIT_SPARKLE_FRAMEWORK"); p != "" {
		return p
	}
	found, _ := filepath.Glob(filepath.Join("..", "..", ".cache", "sparkle", "*", "Sparkle.framework"))
	if len(found) == 0 {
		t.Skip("no Sparkle.framework in .cache/sparkle: make sparkle")
	}
	abs, err := filepath.Abs(found[len(found)-1])
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// buildHarness builds testdata/sparkle_harness.m with sparkle_darwin.m.
func buildHarness(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("no clang")
	}
	bin := filepath.Join(t.TempDir(), "harness")
	build := exec.Command(clang, "-fno-objc-arc", "-framework", "Cocoa", "-I.", "-o", bin,
		filepath.Join("testdata", "sparkle_harness.m"), "sparkle_darwin.m")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the harness: %v\n%s", err, out)
	}
	return bin
}

// harnessID is the test bundles' identifier: what Sparkle writes into user defaults is under it.
const harnessID = "io.guided-traffic.cogpit.test-sparkle-harness"

// makeApp lays out <dir>/Cogpit.app around the harness: its Info.plist with the given SU keys,
// and Sparkle.framework in Contents/Frameworks when framework is set. It returns the executable.
func makeApp(t *testing.T, dir, harness, framework string, keys map[string]string) string {
	t.Helper()
	return makeAppWith(t, dir, harness, framework, keys, harnessID, "")
}

// makeAppWith is makeApp with the bundle's identifier and more of its Info.plist (raw XML).
func makeAppWith(t *testing.T, dir, harness, framework string, keys map[string]string, id, raw string) string {
	t.Helper()
	app := filepath.Join(dir, "Cogpit.app")
	exe := filepath.Join(app, "Contents", "MacOS", "harness")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(harness)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, data, 0o755); err != nil { //nolint:gosec // a test bundle's executable
		t.Fatal(err)
	}
	var extra strings.Builder
	for k, v := range keys {
		fmt.Fprintf(&extra, "\t<key>%s</key>\n\t<string>%s</string>\n", k, v)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>harness</string>
	<key>CFBundleIdentifier</key>
	<string>` + id + `</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleVersion</key>
	<string>0.0.1</string>
	<key>SUEnableAutomaticChecks</key>
	<false/>
` + extra.String() + raw + `</dict>
</plist>
`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o600); err != nil {
		t.Fatal(err)
	}
	if framework != "" {
		dst := filepath.Join(app, "Contents", "Frameworks", "Sparkle.framework")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("ditto", framework, dst).CombinedOutput(); err != nil {
			t.Fatalf("ditto: %v\n%s", err, out)
		}
	}
	return exe
}

// The app finds and loads Sparkle only from an installed app bundle that carries the framework,
// the appcast's URL and the update key, and says which of them is missing otherwise; loaded, the
// controller is made from another thread while the main thread pumps AppKit's events, as in the
// app (sparkle_darwin.m, in a harness process of its own, without starting the updater: no
// network, no window). Skipped without clang or make sparkle's framework.
func TestSparkleLoadsFromTheBundle(t *testing.T) {
	framework := sparkleFramework(t)
	harness := buildHarness(t)
	const key = "pfIShU4dEXqPd5ObYNfDBiQWcXozk7estwzTnF9BamQ=" // Sparkle's documentation's example key
	both := map[string]string{"SUFeedURL": "https://example.invalid/appcast.xml", "SUPublicEDKey": key}
	for _, tc := range []struct {
		name string
		exe  func(dir string) string
		args []string
		want []string
	}{
		{"bare executable", func(dir string) string {
			exe := filepath.Join(dir, "harness")
			data, _ := os.ReadFile(harness)
			if err := os.WriteFile(exe, data, 0o755); err != nil { //nolint:gosec // a test executable
				t.Fatal(err)
			}
			return exe
		}, nil, []string{"load 1"}},
		{"translocated", func(dir string) string {
			return makeApp(t, filepath.Join(dir, "AppTranslocation", "X", "d"), harness, framework, both)
		}, nil, []string{"load 2"}},
		{"no framework", func(dir string) string { return makeApp(t, dir, harness, "", both) }, nil, []string{"load 3"}},
		{"no feed", func(dir string) string {
			return makeApp(t, dir, harness, framework, map[string]string{"SUPublicEDKey": key})
		}, nil, []string{"load 4"}},
		{"no key", func(dir string) string {
			return makeApp(t, dir, harness, framework, map[string]string{"SUFeedURL": both["SUFeedURL"]})
		}, nil, []string{"load 5"}},
		{"loaded and made", func(dir string) string { return makeApp(t, dir, harness, framework, both) },
			[]string{"create"}, []string{"load 0", "state 4", "automatic 0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(tc.exe(dir), tc.args...).Output()
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				t.Fatalf("the harness: %v\n%s\n%s", err, out, exit.Stderr)
			}
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) != len(tc.want) {
				t.Fatalf("output %q, want lines starting %q", out, tc.want)
			}
			for i, want := range tc.want {
				if got := strings.TrimSpace(lines[i]); got != want && !strings.HasPrefix(got, want+" ") {
					t.Errorf("line %d: %q, want %q", i, got, want)
				}
			}
		})
	}
}

// A test binary is no app bundle: New finds no updater and says so, and Start, CheckForUpdates
// and SetAutomaticChecks do nothing.
func TestNewOutsideABundle(t *testing.T) {
	u := New()
	if err := u.Unavailable(); !errors.Is(err, ErrNoBundle) {
		t.Fatalf("Unavailable() = %v, want ErrNoBundle", err)
	}
	u.Start(true)
	u.SetAutomaticChecks(false)
	if err := u.CheckForUpdates(); !errors.Is(err, ErrNoBundle) {
		t.Errorf("CheckForUpdates() = %v, want ErrNoBundle", err)
	}
}

// probeID is the bundle identifier of the probe's app, whose user defaults and caches the test
// removes again: unlike "create", the probe starts the updater, and Sparkle keeps its state there.
const probeID = "io.guided-traffic.cogpit.test-sparkle-probe"

// The Update button's signal from the real Sparkle (F7), and every scheduled check reading the feed
// again: an installed app with the framework, the key and a feed on a local server runs Cogpit's
// scheduled check twice; the first finds 99.1.0 and the delegate reports it with its version,
// which the app's Go side keeps (cogpitSparkleUpdateChanged; the harness prints it), the second
// finds 99.2.0, released meanwhile, and no update session stays open after either. Before
// 2026-10-26 Sparkle's own scheduled check held the release it found in an update session and no
// check ran while it lasted: a copy running for days offered 0.48.2 while 0.49.1 was out (the
// owner's report). Sparkle shows no window for these checks. Skipped without clang or make
// sparkle's framework. Not checked here: a check the user starts (Sparkle's window).
func TestSparkleScheduledChecksFindTheNewestRelease(t *testing.T) {
	framework := sparkleFramework(t)
	harness := buildHarness(t)
	srv, feedHits := feedServer(t, "99.1.0", "99.2.0")
	t.Cleanup(func() { forgetBundle(t, probeID) })
	forgetBundle(t, probeID) // a run before that did not get to its cleanup

	exe := makeFeedApp(t, harness, framework, srv.URL, probeID, "")
	out, err := exec.Command(exe, "probe", "2").Output()
	if err != nil {
		t.Fatalf("the harness: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	want := []string{"load 0", "state 2", "update 1 99.1.0", "update 1 99.2.0", "session 0"}
	if len(lines) != len(want) {
		t.Fatalf("output %q (the feed asked %d times), want %q", out, feedHits.Load(), want)
	}
	for i, w := range want {
		if got := strings.TrimSpace(lines[i]); got != w && !strings.HasPrefix(got, w+" ") {
			t.Errorf("line %d: %q, want %q", i, got, w)
		}
	}
	if got := feedHits.Load(); got != 2 {
		t.Errorf("the feed was asked %d times, want 2", got)
	}
}

// launchID begins the bundle identifiers of the launch test's apps, one per case: their user
// defaults and caches are their own, and the test removes them again, as the probe's.
const launchID = "io.guided-traffic.cogpit.test-sparkle-launch"

// Cogpit checks for updates at every start while "Check for updates automatically" is on (the
// owner, 2026-10-16): once, whenever Sparkle last checked, at the first launch too; its find goes
// to the Update button, and a check that finds nothing says so. Sparkle never checks by itself:
// its own scheduled checks are off, also where an older Cogpit left them on in the app's user
// defaults and the last check is older than the interval that older release set (an hour), so
// that Sparkle would check at once. With the preference off, no check at all. The harness starts
// the updater as the app does and pumps events a few seconds past the first report or the
// session's end; the test counts the feed's requests. The cases run in parallel, each an app of its
// own with its own feed and user defaults, as each mostly waits. Skipped without clang or make
// sparkle's framework.
func TestSparkleChecksAtEveryStart(t *testing.T) {
	framework := sparkleFramework(t)
	harness := buildHarness(t)
	now := time.Now().UTC()
	// The interval releases before 2026-10-26 set (hack/macos/sparkle.sh), an hour: Sparkle's own
	// check would be due two hours after the last one.
	interval := "\t<key>SUScheduledCheckInterval</key>\n\t<integer>3600</integer>\n"
	for i, tc := range []struct {
		name      string
		automatic bool
		lastCheck time.Time // zero: Sparkle never checked (the first launch)
		olderOn   bool      // an older Cogpit left Sparkle's own scheduled checks on
		feed      string    // the feed's newest version; the bundle is 0.0.1
		hits      int32
		report    string
	}{
		{"last check a minute ago", true, now.Add(-time.Minute), false, "99.1.0", 1, "update 1 99.1.0"},
		{"last check two hours ago", true, now.Add(-2 * time.Hour), false, "99.1.0", 1, "update 1 99.1.0"},
		{"first launch", true, time.Time{}, false, "99.1.0", 1, "update 1 99.1.0"},
		{"nothing newer", true, now.Add(-2 * time.Hour), false, "0.0.1", 1, "update 0"},
		{"an older Cogpit's settings", true, now.Add(-2 * time.Hour), true, "99.1.0", 1, "update 1 99.1.0"},
		{"checks off", false, now.Add(-2 * time.Hour), false, "99.1.0", 0, ""},
		{"checks off, an older Cogpit's settings", false, now.Add(-2 * time.Hour), true, "99.1.0", 0, ""},
		{"checks off, first launch", false, time.Time{}, false, "99.1.0", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id := fmt.Sprintf("%s-%d", launchID, i+1)
			srv, feedHits := feedServer(t, tc.feed)
			t.Cleanup(func() { forgetBundle(t, id) })
			forgetBundle(t, id)
			var writes [][]string
			if !tc.lastCheck.IsZero() {
				writes = append(writes,
					[]string{"write", id, "SUHasLaunchedBefore", "-bool", "YES"},
					[]string{"write", id, "SULastCheckTime", "-date", tc.lastCheck.Format("2006-01-02 15:04:05 +0000")})
			}
			if tc.olderOn {
				writes = append(writes, []string{"write", id, "SUEnableAutomaticChecks", "-bool", "YES"})
			}
			for _, args := range writes {
				if out, err := exec.Command("defaults", args...).CombinedOutput(); err != nil {
					t.Fatalf("defaults %v: %v\n%s", args, err, out)
				}
			}
			exe := makeFeedApp(t, harness, framework, srv.URL, id, interval)
			arg := "0"
			if tc.automatic {
				arg = "1"
			}
			out, err := exec.Command(exe, "launch", arg).Output()
			if err != nil {
				t.Fatalf("the harness: %v\n%s", err, out)
			}
			if !strings.HasSuffix(strings.TrimSpace(string(out)), "automatic 0\ndone") {
				t.Fatalf("output %q, want it to end with Sparkle's own checks off and done", out)
			}
			if got := feedHits.Load(); got != tc.hits {
				t.Errorf("the feed was asked %d times, want %d (output %q)", got, tc.hits, out)
			}
			if tc.report != "" && !strings.Contains(string(out), tc.report+"\n") && !strings.Contains(string(out), tc.report+" \n") {
				t.Errorf("output %q, want the report %q", out, tc.report)
			}
			if tc.report == "" && strings.Contains(string(out), "update ") {
				t.Errorf("output %q reports a check", out)
			}
		})
	}
}

// Cogpit's scheduled checks run Sparkle's probe at once and every interval, start once, and stop
// when turned off.
func TestSparkleScheduledChecks(t *testing.T) {
	interval, p := checkInterval, probe
	t.Cleanup(func() {
		setAutomaticChecks(false)
		checkInterval, probe = interval, p
	})
	var probes atomic.Int32
	probe = func() { probes.Add(1) }
	waitProbes := func(n int32) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for probes.Load() < n {
			if time.Now().After(deadline) {
				t.Fatalf("%d checks, want %d", probes.Load(), n)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	checkInterval = time.Hour
	setAutomaticChecks(true)
	setAutomaticChecks(true) // a second start starts nothing more
	waitProbes(1)
	time.Sleep(50 * time.Millisecond)
	if got := probes.Load(); got != 1 {
		t.Errorf("%d checks at the start, want 1", got)
	}
	setAutomaticChecks(false)

	checkInterval = 10 * time.Millisecond
	setAutomaticChecks(true)
	waitProbes(4) // the start's and the interval's
	setAutomaticChecks(false)
	time.Sleep(30 * time.Millisecond)
	n := probes.Load()
	time.Sleep(100 * time.Millisecond)
	if got := probes.Load(); got != n {
		t.Errorf("%d checks after they were turned off", got-n)
	}
}

// feedServer serves an appcast whose one item has the first of versions, then, at each further
// request, the next, the last for the rest; it counts its requests.
func feedServer(t *testing.T, versions ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/appcast.xml" {
			http.NotFound(w, r)
			return
		}
		version := versions[min(int(hits.Add(1)), len(versions))-1]
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
  <channel>
    <title>Cogpit</title>
    <item>
      <title>Cogpit %[1]s</title>
      <sparkle:version>%[1]s</sparkle:version>
      <sparkle:shortVersionString>%[1]s</sparkle:shortVersionString>
      <sparkle:minimumSystemVersion>13.0</sparkle:minimumSystemVersion>
      <pubDate>Thu, 15 Oct 2026 08:00:00 +0000</pubDate>
      <enclosure url="http://%[2]s/Cogpit-%[1]s-macos-arm64.dmg" length="1024" type="application/octet-stream" sparkle:edSignature="%[3]s"/>
    </item>
  </channel>
</rss>
`, version, r.Host, base64.StdEncoding.EncodeToString(make([]byte, 64)))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

// makeFeedApp lays out an installed app with the framework, the update key and the feed on url,
// local networking allowed by App Transport Security, under the bundle identifier id, with more of
// its Info.plist (raw XML).
func makeFeedApp(t *testing.T, harness, framework, url, id, raw string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const key = "pfIShU4dEXqPd5ObYNfDBiQWcXozk7estwzTnF9BamQ=" // Sparkle's documentation's example key
	ats := "\t<key>NSAppTransportSecurity</key>\n\t<dict>\n\t\t<key>NSAllowsLocalNetworking</key>\n\t\t<true/>\n\t</dict>\n"
	return makeAppWith(t, dir, harness, framework,
		map[string]string{"SUFeedURL": url + "/appcast.xml", "SUPublicEDKey": key}, id, ats+raw)
}

// forgetBundle removes what Sparkle and Foundation kept for a test bundle: its user defaults and
// its caches.
func forgetBundle(t *testing.T, id string) {
	t.Helper()
	_ = exec.Command("defaults", "delete", id).Run() // absent the first time
	if home, err := os.UserHomeDir(); err == nil {
		_ = os.RemoveAll(filepath.Join(home, "Library", "Caches", id))
		_ = os.RemoveAll(filepath.Join(home, "Library", "HTTPStorages", id))
	}
}
