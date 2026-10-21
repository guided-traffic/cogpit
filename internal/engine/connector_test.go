package engine

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestReleaseBuildsLeaveTheDemoOut: the release pipeline's make dist-app and make dist-linux build
// both binaries with the Makefile's DIST_TAGS (through BUILD_TAGS, which adds Fyne's thread check
// tag, D-103), and
// those name the tag connector_nodemo.go is built with. A tag no file knows is silently ignored,
// and a release would ship the demo cluster (D-67).
func TestReleaseBuildsLeaveTheDemoOut(t *testing.T) {
	stub, err := os.ReadFile("connector_nodemo.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^//go:build (\w+)$`).FindSubmatch(stub)
	if m == nil {
		t.Fatal("connector_nodemo.go has no single-tag build constraint")
	}
	tag := string(m[1])

	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	d := regexp.MustCompile(`(?m)^DIST_TAGS\s*:?=\s*(.*)$`).FindSubmatch(mk)
	if d == nil || !slices.Contains(strings.FieldsFunc(string(d[1]), func(r rune) bool { return r == ',' || r == ' ' }), tag) {
		t.Errorf("the Makefile's DIST_TAGS do not name %q: %q", tag, d)
	}
	if b := regexp.MustCompile(`(?m)^BUILD_TAGS\s*:?=\s*(.*)$`).FindSubmatch(mk); b == nil || !strings.Contains(string(b[1]), "$(GO_TAGS)") {
		t.Errorf("the Makefile's BUILD_TAGS do not take GO_TAGS: %q", b)
	}
	for _, want := range []string{
		"$(MAKE) app GO_LDFLAGS='$(DIST_LDFLAGS)' GO_TAGS='$(DIST_TAGS)'",   // dist-app
		"$(MAKE) build GO_LDFLAGS='$(DIST_LDFLAGS)' GO_TAGS='$(DIST_TAGS)'", // dist-linux, dist-windows
	} {
		if !strings.Contains(string(mk), want) {
			t.Errorf("the Makefile lacks %q", want)
		}
	}
	// make build: the app's own linker flags may follow GO_LDFLAGS, and $(EXE) is .exe on Windows.
	for _, bin := range []string{"cogpit", "cogpit-cli"} {
		re := `(?m)\$\(GO\) build -tags '\$\(BUILD_TAGS\)' -ldflags '[^']*\$\(GO_LDFLAGS\)[^']*' -o \$\(BIN\)/` + bin +
			`(\$\(EXE\))? \./cmd/` + bin + `$`
		if !regexp.MustCompile(re).Match(mk) {
			t.Errorf("the Makefile does not build %s with BUILD_TAGS and GO_LDFLAGS", bin)
		}
	}
}
