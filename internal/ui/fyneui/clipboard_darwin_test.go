//go:build darwin

package fyneui

import (
	hexenc "encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/testkit/prereq"
)

// A Secret value's copy on macOS (H-25): clipboard_darwin.m itself, built with a native harness
// into a process of its own that hands it a pasteboard of its own in the general pasteboard's
// place, so the user's clipboard is neither read nor changed. The value lands whole beside
// nspasteboard.org's concealed and transient types, which clipboard managers read, in contents
// prepared for this Mac only, out of Universal Clipboard: the harness records the options the copy
// asked for (AppKit reads none back), and the change count shows that nothing replaced those
// contents after. Needs clang and a pasteboard server.
func TestConcealedClipboardMarkedForThisMacOnly(t *testing.T) {
	clang := prereq.LookPath(t, "clang")
	bin := filepath.Join(t.TempDir(), "clipboard_harness")
	build := exec.Command(clang, "-fno-objc-arc", "-framework", "Cocoa", "-I.", "-o", bin,
		filepath.Join("testdata", "clipboard_harness.m"), "clipboard_darwin.m")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the harness: %v\n%s", err, out)
	}
	const value = "first line: wört\nsecond line, a key 🔑 and a trailing space \n"
	out, err := exec.Command(bin, value).Output()
	var exit *exec.ExitError
	prereq.Need(t, !errors.As(err, &exit) || exit.ExitCode() != 3, "a pasteboard server")
	if err != nil {
		t.Fatalf("the harness: %v\n%s", err, out)
	}
	facts := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, rest, _ := strings.Cut(line, " ")
		facts[name] = append(facts[name], rest)
	}
	one := func(name string) string {
		if len(facts[name]) != 1 {
			t.Fatalf("want one %q line, the harness said\n%s", name, out)
		}
		return facts[name][0]
	}

	if one("general") == "0" {
		t.Fatalf("the copy never asked for the general pasteboard:\n%s", out)
	}
	if one("set") != "1" {
		t.Fatalf("the copy reported a failure:\n%s", out)
	}
	if len(facts["prepared"]) == 0 {
		t.Fatalf("the copy never prepared the pasteboard for new contents, so not for this Mac only:\n%s", out)
	}
	var options, hostOnly uint64
	var onStandIn int
	var began string
	if _, err := fmt.Sscan(one("prepared"), &options, &hostOnly, &onStandIn, &began); err != nil {
		t.Fatalf("the prepared line %q: %v", one("prepared"), err)
	}
	if options&hostOnly == 0 {
		t.Errorf("the copy prepared the pasteboard with options %#x, without NSPasteboardContentsCurrentHostOnly (%#x): "+
			"Universal Clipboard would offer the value to the user's other devices", options, hostOnly)
	}
	if onStandIn != 1 {
		t.Errorf("the copy prepared another pasteboard than the general one")
	}
	if now := one("count"); now != began {
		t.Errorf("the pasteboard's change count is %s, the contents prepared for this Mac only began %s: something replaced them", now, began)
	}
	for _, want := range []string{"public.utf8-plain-text", "org.nspasteboard.ConcealedType", "org.nspasteboard.TransientType"} {
		if !slices.Contains(facts["type"], want) {
			t.Errorf("the pasteboard's types %q lack %s", facts["type"], want)
		}
	}
	got, err := hexenc.DecodeString(one("string"))
	if err != nil {
		t.Fatalf("the string line %q: %v", one("string"), err)
	}
	if string(got) != value {
		t.Errorf("the pasteboard holds %q, want %q", got, value)
	}
}
