package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model/k8seol"
)

// Trimmed copies of upstream's two files as they read on 2026-10-18 (their fields as
// schedule-builder writes them, a patch list shortened).
const testSchedule = `---
schedules:
- endOfLifeDate: "2027-10-28"
  maintenanceModeStartDate: "2027-08-28"
  next:
    cherryPickDeadline: "2026-10-09"
    release: 1.37.2
    targetDate: "2026-10-13"
  release: "1.37"
  releaseDate: "2026-08-26"
- endOfLifeDate: "2027-06-28"
  maintenanceModeStartDate: "2027-04-28"
  previousPatches:
  - cherryPickDeadline: "2026-09-11"
    release: 1.36.5
    targetDate: "2026-09-15"
  release: "1.36"
  releaseDate: "2026-04-22"
upcoming_releases:
- cherryPickDeadline: "2026-10-09"
  targetDate: "2026-10-13"
`

const testEOL = `---
branches:
- endOfLifeDate: "2026-02-28"
  finalPatchRelease: 1.35.13
  release: "1.35"
- endOfLifeDate: "2025-11-11"
  finalPatchRelease: 1.34.14
  note: a note
  release: "1.34"
`

var day = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

func TestBuild(t *testing.T) {
	tab, err := build([]byte(testSchedule), []byte(testEOL), day)
	if err != nil {
		t.Fatal(err)
	}
	if tab.Fetched != "2026-10-18" || len(tab.Minors) != 4 || tab.Minors[0].Minor != "1.34" || tab.Newest().Minor != "1.37" {
		t.Fatalf("%+v", tab)
	}
	want := k8seol.Minor{Minor: "1.36", Released: "2026-04-22", MaintenanceFrom: "2027-04-28", EndOfLife: "2027-06-28"}
	if tab.Minors[2] != want {
		t.Errorf("1.36: %+v", tab.Minors[2])
	}
	if m := tab.Minors[0]; m.EndOfLife != "2025-11-11" || m.FinalPatch != "1.34.14" || m.Released != "" {
		t.Errorf("1.34: %+v", m)
	}
	// What the generator writes reads back as the same table, one minor a line.
	data := encode(tab)
	back, err := k8seol.ParseTable(data)
	if err != nil || len(back.Minors) != 4 || back.Minors[2] != want {
		t.Fatalf("encoded %s: %v", data, err)
	}
	if n := strings.Count(string(data), "\n"); n != 4+6 {
		t.Errorf("%d lines:\n%s", n, data)
	}
}

func TestBuildRefuses(t *testing.T) {
	for name, files := range map[string][2]string{
		"no schedule":                {"schedules: []\n", testEOL},
		"no ended minors":            {testSchedule, "branches: []\n"},
		"not YAML":                   {"schedules: [", testEOL},
		"a gap":                      {testSchedule, strings.Replace(testEOL, `release: "1.35"`, `release: "1.33"`, 1)},
		"a date that is none":        {strings.Replace(testSchedule, `"2027-06-28"`, `"June 2027"`, 1), testEOL},
		"a minor twice":              {testSchedule, testEOL + "- endOfLifeDate: \"2025-11-11\"\n  release: \"1.34\"\n"},
		"1.40 unquoted, read as 1.4": {strings.Replace(testSchedule, `release: "1.37"`, `release: 1.40`, 1), testEOL},
	} {
		if _, err := build([]byte(files[0]), []byte(files[1]), day); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

func TestCompare(t *testing.T) {
	old, err := build([]byte(testSchedule), []byte(testEOL), day.AddDate(0, -1, 0))
	if err != nil {
		t.Fatal(err)
	}
	same, _ := build([]byte(testSchedule), []byte(testEOL), day)
	if changes, err := compare(old, same); err != nil || len(changes) != 1 || !strings.HasPrefix(changes[0], "no minor changed") {
		t.Errorf("unchanged: %v %v", changes, err)
	}
	// Upstream moving an end of life by two weeks is a change; by half a year is refused.
	moved, _ := build([]byte(strings.Replace(testSchedule, `"2027-06-28"`, `"2027-07-13"`, 1)), []byte(testEOL), day)
	if changes, err := compare(old, moved); err != nil || len(changes) != 1 || !strings.HasPrefix(changes[0], "1.36: ") {
		t.Errorf("moved by two weeks: %v %v", changes, err)
	}
	far := strings.Replace(testEOL, `"2026-02-28"`, `"2026-09-28"`, 1)
	if tab, err := build([]byte(testSchedule), []byte(far), day); err != nil {
		t.Fatal(err)
	} else if _, err := compare(old, tab); err == nil {
		t.Error("an end of life moved by seven months accepted")
	}
	// A minor of the committed table missing from upstream's files is refused.
	lost := strings.Replace(testEOL, "- endOfLifeDate: \"2025-11-11\"\n  finalPatchRelease: 1.34.14\n  note: a note\n  release: \"1.34\"\n", "", 1)
	if tab, err := build([]byte(testSchedule), []byte(lost), day); err != nil {
		t.Fatal(err)
	} else if _, err := compare(old, tab); err == nil {
		t.Error("a lost minor accepted")
	}
}

// run downloads, checks and writes; a failed download writes nothing and the committed table
// stays one to build with (what -fallback keeps).
func TestRun(t *testing.T) {
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case fail:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case strings.HasSuffix(r.URL.Path, "/schedule.yaml"):
			_, _ = io.WriteString(w, testSchedule)
		default:
			_, _ = io.WriteString(w, testEOL)
		}
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "releases.json")
	var log strings.Builder
	if err := run(context.Background(), srv.Client(), out, srv.URL+"/schedule.yaml", srv.URL+"/eol.yaml", day, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "4 minors, 1.34 to 1.37") || !keepable(out) {
		t.Errorf("log %q, keepable %v", log.String(), keepable(out))
	}
	written, _ := os.ReadFile(out)
	fail = true
	if err := run(context.Background(), srv.Client(), out, srv.URL+"/schedule.yaml", srv.URL+"/eol.yaml", day, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "503") {
		t.Errorf("a failed download: %v", err)
	}
	if now, _ := os.ReadFile(out); string(now) != string(written) || !keepable(out) {
		t.Error("a failed download changed the committed table")
	}
	if keepable(filepath.Join(t.TempDir(), "none.json")) {
		t.Error("a missing table is keepable")
	}
}
