package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckFindsDatesInCommentsOnly: a date in a line comment, a trailing comment and a block
// comment is found; one in a string literal, in quotes in a comment, as a timestamp or as Go's
// layout is data; testdata is not read.
func TestCheckFindsDatesInCommentsOnly(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", `package a

// Built on 2026-10-12. "2026-10-13" is an example; 2026-10-11 14:30 UTC and 2026-10-11T13:00:00Z
// are timestamps; 2006-01-02 is the layout.
var s = "2026-10-14" // the owner, 2026-10-15

/* 2026-10-16 */
`)
	write("testdata/b.go", "// 2026-10-17\npackage b\n")
	write("notes.txt", "2026-10-18\n")

	found, err := check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if found != 3 {
		t.Errorf("found %d dates, want 3 (2026-10-12, 2026-10-15, 2026-10-16)", found)
	}
}

func TestIsData(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{`// decided 2026-10-12`, false},
		{`// ("done (2026-10-11, …)")`, true},
		{`// at 2026-10-11 14:30 UTC`, true},
		{`// the layout 2006-01-02`, true},
		{`// "Option A" of 2026-10-26`, false},
	} {
		m := date.FindStringIndex(tc.line)
		if got := isData(tc.line, m[0], m[1]); got != tc.want {
			t.Errorf("isData(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
