package main

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// isTest follows the go tool's rule: the prefix alone, or followed by a rune that is not lower case.
func TestIsTest(t *testing.T) {
	for name, want := range map[string]bool{
		"Test": true, "TestShell": true, "Test_x": true, "Test1": true, "Testing": false,
		"Example": true, "ExampleRun": true, "Examples": false, "FuzzParse": true, "helper": false,
	} {
		prefix := "Test"
		switch name[0] {
		case 'E':
			prefix = "Example"
		case 'F':
			prefix = "Fuzz"
		}
		if got := isTest(name, prefix); got != want {
			t.Errorf("isTest(%q, %q) = %v, want %v", name, prefix, got, want)
		}
	}
}

// Without durations every name lands in exactly one shard, the same one each time, and a name added
// later does not move the others.
func TestSplitIsStableAndComplete(t *testing.T) {
	names := []string{"TestA", "TestB", "TestC", "TestD", "TestE", "TestF", "TestG", "TestH"}
	shards := split(names, 3, nil)
	var all []string
	for _, s := range shards {
		all = append(all, s...)
	}
	slices.Sort(all)
	if !slices.Equal(all, names) {
		t.Fatalf("the shards hold %v, want each of %v once", all, names)
	}
	if again := split(names, 3, nil); !slices.EqualFunc(shards, again, slices.Equal) {
		t.Errorf("split changed between calls: %v, then %v", shards, again)
	}
	grown := split(append(slices.Clone(names), "TestNew"), 3, nil)
	for i := range shards {
		kept := slices.DeleteFunc(slices.Clone(grown[i]), func(n string) bool { return n == "TestNew" })
		if !slices.Equal(kept, shards[i]) {
			t.Errorf("shard %d was %v and is %v after adding a test", i+1, shards[i], kept)
		}
	}
	if got := pattern([]string{"TestA", "TestB"}); got != "^(TestA|TestB)$" {
		t.Errorf("pattern = %q", got)
	}
}

// -run keeps the names it matches and -skip leaves out those it matches, as go test selects
// top-level tests; without durations what they leave keeps its shard. A slash, a subtest's level for
// go test, is refused, and so is an expression that does not compile.
func TestSelection(t *testing.T) {
	names := []string{"TestRenderA", "TestRenderB", "TestRendered", "TestShell", "TestTabs", "ExampleRender"}
	for _, c := range []struct {
		run, skip string
		want      []string
	}{
		{"", "", names},
		{"^TestRender", "", []string{"TestRenderA", "TestRenderB", "TestRendered"}},
		{"", "^TestRender", []string{"TestShell", "TestTabs", "ExampleRender"}},
		{"Render", "ed$", []string{"TestRenderA", "TestRenderB", "ExampleRender"}},
	} {
		sel, err := newSelection(c.run, c.skip)
		if err != nil {
			t.Fatal(err)
		}
		got := sel.keep(names)
		if !slices.Equal(got, c.want) {
			t.Errorf("-run %q -skip %q kept %v, want %v", c.run, c.skip, got, c.want)
		}
		if sel.narrows() != (c.run != "" || c.skip != "") {
			t.Errorf("-run %q -skip %q: narrows() = %v", c.run, c.skip, sel.narrows())
		}
		all, left := split(names, 3, nil), split(got, 3, nil)
		for i := range left {
			kept := slices.DeleteFunc(slices.Clone(all[i]), func(n string) bool { return !slices.Contains(got, n) })
			if !slices.Equal(left[i], kept) {
				t.Errorf("-run %q -skip %q: shard %d holds %v, want %v as without them", c.run, c.skip, i+1, left[i], kept)
			}
		}
	}
	for _, c := range [][2]string{{"^TestRender/dark", ""}, {"", "TestA/x"}, {"(", ""}, {"", "["}} {
		if _, err := newSelection(c[0], c[1]); err == nil {
			t.Errorf("-run %q -skip %q: no error", c[0], c[1])
		}
	}
}

// With durations the names they hold go longest first, each to the shard with the least time so far
// (the first on a tie; equal times by name), so the shards' sums are as even as the times allow;
// a name they lack goes by its hash and moves none of the others; each shard keeps the names'
// order, and the split is the same each time.
func TestSplitBalancesByDurations(t *testing.T) {
	sec := func(s float64) time.Duration { return roundDuration(s) }
	names := []string{"TestA", "TestB", "TestC", "TestD", "TestE", "TestF", "TestG", "TestH"}
	times := map[string]time.Duration{"TestA": sec(9), "TestB": sec(8), "TestC": sec(7), "TestD": sec(6),
		"TestE": sec(5), "TestF": sec(4), "TestG": sec(3), "TestH": sec(2), "TestGone": sec(50)}
	want := [][]string{{"TestA", "TestF", "TestG"}, {"TestB", "TestE", "TestH"}, {"TestC", "TestD"}}
	got := split(names, 3, times)
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("split = %v, want %v", got, want)
	}
	for i, wantSum := range []float64{16, 15, 13} {
		if sum, unknown := shardTime(got[i], times); sum != sec(wantSum) || unknown != 0 {
			t.Errorf("shard %d: %v and %d without a time, want %v s", i+1, sum, unknown, wantSum)
		}
	}
	if again := split(names, 3, times); !slices.EqualFunc(again, got, slices.Equal) {
		t.Errorf("split changed between calls: %v, then %v", got, again)
	}

	grown := split(append(slices.Clone(names), "TestNew"), 3, times)
	h := hashShard("TestNew", 3)
	if !slices.Contains(grown[h], "TestNew") {
		t.Errorf("TestNew, without a time, is not in its hash's shard %d: %v", h+1, grown)
	}
	for i := range got {
		kept := slices.DeleteFunc(slices.Clone(grown[i]), func(n string) bool { return n == "TestNew" })
		if !slices.Equal(kept, got[i]) {
			t.Errorf("shard %d was %v and is %v after adding a test without a time", i+1, got[i], kept)
		}
	}

	tied := map[string]time.Duration{"TestB": sec(1), "TestA": sec(1), "TestC": sec(1)}
	if got := split([]string{"TestC", "TestB", "TestA"}, 2, tied); !slices.EqualFunc(got, [][]string{{"TestC", "TestA"}, {"TestB"}}, slices.Equal) {
		t.Errorf("equal times: %v, want TestA first to shard 1, TestB to 2, TestC to 1, in the names' order", got)
	}

	// Tests that took no measurable time count a hundredth each: they spread over the shards.
	quick := map[string]time.Duration{"TestA": sec(1), "TestB": sec(1)}
	var zeros []string
	for i := range 6 {
		name := fmt.Sprintf("TestZero%d", i)
		zeros, quick[name] = append(zeros, name), 0
	}
	got = split(append([]string{"TestA", "TestB"}, zeros...), 2, quick)
	if len(got[0]) != 4 || len(got[1]) != 4 {
		t.Errorf("six tests of no time beside two of a second: %v, want four tests a shard", got)
	}
}

// A durations file is written sorted by name under its header and read back at its resolution; a
// line that is not a name and seconds, a negative time and a name given twice are errors, and the
// files checked in here read without one.
func TestDurationsFile(t *testing.T) {
	path := t.TempDir() + "/d.txt"
	times := map[string]time.Duration{"TestB": 1234 * time.Millisecond, "TestA": 0, "TestC": 61 * time.Second}
	if err := writeDurationsFile(path, "line one\nline two", times); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# line one\n# line two\nTestA 0.00\nTestB 1.23\nTestC 61.00\n"; string(data) != want {
		t.Errorf("the file is %q, want %q", data, want)
	}
	back, err := readDurationsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Duration{"TestA": 0, "TestB": 1230 * time.Millisecond, "TestC": 61 * time.Second}
	if !maps.Equal(back, want) {
		t.Errorf("read back %v, want %v", back, want)
	}
	for bad, line := range map[string]string{"TestA\n": ":3:", "TestA 1 2\n": ":3:", "TestA x\n": ":3:",
		"TestA -1\n": ":3:", "TestA NaN\n": ":3:", "TestA +Inf\n": ":3:", "TestA 1e7\n": ":3:", "TestA 1\nTestA 2\n": ":4:"} {
		if err := os.WriteFile(path, []byte("# header\n\n"+bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readDurationsFile(path); err == nil || !strings.Contains(err.Error(), line) {
			t.Errorf("%q: %v, want an error naming line %s", bad, err, line)
		}
	}
	if _, err := readDurationsFile(t.TempDir() + "/missing.txt"); err == nil {
		t.Error("a missing file: no error")
	}
	// The files make ui-durations wrote, which the make targets pass to -durations.
	checkedIn, err := filepath.Glob("durations/*.txt")
	if err != nil || len(checkedIn) == 0 {
		t.Fatalf("no durations files in durations/: %v", err)
	}
	for _, f := range checkedIn {
		if times, err := readDurationsFile(f); err != nil || len(times) == 0 {
			t.Errorf("%s: %d tests, %v", f, len(times), err)
		}
	}
}

// A shard's go test -json stream gives each top-level test's time, whether it passed, failed or
// was skipped (subtests and the package's own events left out), and the output go test would have
// printed without -json, lines that are no events kept as they are.
func TestReadTestJSON(t *testing.T) {
	stream := strings.Join([]string{
		`# github.com/x/y [build]`,
		`{"Action":"run","Test":"TestA"}`,
		`{"Action":"output","Test":"TestA","Output":"=== RUN   TestA\n"}`,
		`{"Action":"pass","Test":"TestA/sub","Elapsed":5}`,
		`{"Action":"pass","Test":"TestA","Elapsed":1.234}`,
		`{"Action":"output","Test":"TestB","Output":"--- FAIL: TestB (0.50s)\n"}`,
		`{"Action":"fail","Test":"TestB","Elapsed":0.5}`,
		`{"Action":"skip","Test":"TestC","Elapsed":0}`,
		`{"Action":"output","Output":"FAIL\n"}`,
		`{"Action":"fail","Elapsed":9}`,
	}, "\n")
	times, out, err := readTestJSON([]byte(stream))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Duration{"TestA": 1230 * time.Millisecond, "TestB": 500 * time.Millisecond, "TestC": 0}
	if !maps.Equal(times, want) {
		t.Errorf("times = %v, want %v", times, want)
	}
	if wantOut := "# github.com/x/y [build]\n=== RUN   TestA\n--- FAIL: TestB (0.50s)\nFAIL\n"; string(out) != wantOut {
		t.Errorf("output = %q, want %q", out, wantOut)
	}
}

// The package's own tests are found from its files, TestMain left out.
func TestTestNamesOfThisPackage(t *testing.T) {
	names, err := testNames(".", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestDefaultJobs", "TestDurationsFile", "TestExecShardsRecordsTimes", "TestExecShardsRunsEveryShard",
		"TestIsTest", "TestRaceKept", "TestReadTestJSON", "TestSelection", "TestSplitBalancesByDurations",
		"TestSplitIsStableAndComplete", "TestTags", "TestTestNamesOfThisPackage"}
	if !slices.Equal(names, want) {
		t.Errorf("testNames = %v, want %v", names, want)
	}
}

// The race detector keeps a test from running when only the build without it compiles the test, or
// when the test reads raceEnabled, itself or through anything of its own test package that does: a
// helper, two helpers deep, a method, a function passed as a value, a variable set from it. A test
// calling a helper that does not read it runs under the race detector, and the external test
// package's helper of the same name does not hide the package's own.
func TestRaceKept(t *testing.T) {
	kept, err := raceKept("./testdata/racekept")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestOnlyWithoutRace", "TestReadsMarker", "TestViaFuncValue", "TestViaHelper", "TestViaMethod",
		"TestViaTwoHelpers", "TestViaVar"}
	if len(kept) != 1 || !strings.HasSuffix(kept[0].importPath, "/hack/shard/testdata/racekept") ||
		!slices.Equal(kept[0].names, want) {
		t.Errorf("raceKept = %+v, want the fixture's %v", kept, want)
	}
	none, err := raceKept("./testdata/execfixture")
	if err != nil || len(none) != 0 {
		t.Errorf("a package without such tests: %+v, %v", none, err)
	}
}

// -race adds the race tag to the files the split reads, once, and the build gets the other tags
// only: the tag without the detector would build the race files into a binary that has none.
func TestTags(t *testing.T) {
	for _, c := range []struct{ tags, joined, without string }{
		{"", "race", ""},
		{"race", "race", ""},
		{"kind", "kind,race", "kind"},
		{"kind,race,nodemo", "kind,race,nodemo", "kind,nodemo"},
	} {
		if got := joinTags(c.tags, "race"); got != c.joined {
			t.Errorf("joinTags(%q) = %q, want %q", c.tags, got, c.joined)
		}
		if got := withoutTag(c.joined, "race"); got != c.without {
			t.Errorf("withoutTag(%q) = %q, want %q", c.joined, got, c.without)
		}
	}
}

// -exec runs every shard as a go test run of its own (in the package's directory, as go test runs
// a test binary), a line and a coverage profile per shard (mode set without the race detector); a
// failing test fails its shard, whose output follows, and the run. With -run or -skip a single shard
// runs what they select, not the whole package.
func TestExecShardsRunsEveryShard(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a test binary")
	}
	t.Setenv("GOFLAGS", "-count=1") // every shard runs: a cached result would print "ok (cached)"
	run := func(fail string, of int, sel selection) (bool, string, string) {
		t.Setenv("SHARD_FIXTURE_FAIL", fail)
		var out bytes.Buffer
		dir := t.TempDir()
		ok, err := execShards(execConfig{pkg: "./testdata/execfixture", sel: sel, of: of, jobs: 2, timeout: time.Minute,
			coverDir: dir, coverName: "fixture"}, &out)
		if err != nil {
			t.Fatalf("execShards: %v\n%s", err, &out)
		}
		return ok, out.String(), dir
	}
	ok, out, dir := run("", 3, selection{})
	if !ok || !strings.Contains(out, "all passed, 3 shards") {
		t.Fatalf("all six tests pass, yet:\n%s", out)
	}
	names, err := testNames("./testdata/execfixture", "")
	if err != nil {
		t.Fatal(err)
	}
	for i, mine := range split(names, 3, nil) {
		line := regexp.MustCompile(fmt.Sprintf(`(?m)^shard %d/3: ok, %d tests`, i+1, len(mine)))
		if len(mine) == 0 {
			line = regexp.MustCompile(fmt.Sprintf(`(?m)^shard %d/3: no tests$`, i+1))
		}
		if !line.MatchString(out) {
			t.Errorf("no line %q:\n%s", line, out)
		}
		profile, err := os.ReadFile(fmt.Sprintf("%s/fixture-%d.out", dir, i+1))
		if (err == nil) != (len(mine) > 0) || (err == nil && !strings.HasPrefix(string(profile), "mode: set\n")) {
			t.Errorf("shard %d of %d tests: its coverage profile: %v, %.20q", i+1, len(mine), err, profile)
		}
	}
	ok, out, _ = run("TestD", 3, selection{})
	if ok || !strings.Contains(out, "FAILED") || !strings.Contains(out, "failed on purpose") {
		t.Errorf("TestD fails, yet:\n%s", out)
	}
	sel, err := newSelection("^Test[A-D]$", "^TestD$")
	if err != nil {
		t.Fatal(err)
	}
	ok, out, _ = run("TestD", 1, sel) // TestD would fail: the whole package ran
	if !ok || !regexp.MustCompile(`(?m)^shard 1/1: ok, 3 tests`).MatchString(out) {
		t.Errorf("TestA to TestC selected, yet:\n%s", out)
	}
}

// -exec -record runs the shards with go test -json and writes every test's time to the durations
// file, which then balances the next run's split; a failing shard leaves the file as it was.
func TestExecShardsRecordsTimes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a test binary")
	}
	path := t.TempDir() + "/durations.txt"
	run := func(fail string, times map[string]time.Duration) (bool, string) {
		t.Setenv("SHARD_FIXTURE_FAIL", fail)
		var out bytes.Buffer
		ok, err := execShards(execConfig{pkg: "./testdata/execfixture", of: 2, jobs: 2, timeout: time.Minute,
			times: times, durations: path, record: path}, &out)
		if err != nil {
			t.Fatalf("execShards: %v\n%s", err, &out)
		}
		return ok, out.String()
	}
	ok, out := run("", nil)
	if !ok || !strings.Contains(out, "the times of 6 tests written to "+path) {
		t.Fatalf("all six tests pass, yet:\n%s", out)
	}
	times, err := readDurationsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(times)); !slices.Equal(got, []string{"TestA", "TestB", "TestC", "TestD", "TestE", "TestF"}) {
		t.Errorf("recorded %v, want the fixture's six tests", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# Seconds per top-level test of ./testdata/execfixture, without the race detector, on ") {
		t.Errorf("the file's header: %.120q", data)
	}

	ok, out = run("TestD", times)
	if ok || !strings.Contains(out, "balanced by "+path) || !strings.Contains(out, "failed on purpose") ||
		!strings.Contains(out, "not written, as a shard failed") {
		t.Errorf("TestD fails, yet:\n%s", out)
	}
	if again, err := os.ReadFile(path); err != nil || !bytes.Equal(again, data) {
		t.Errorf("a failed run changed the durations file: %v", err)
	}
}

// The shards that run at once by default: half the CPUs, and no more than three quarters of the
// memory holds; one at least, and half the CPUs where the memory is unknown.
func TestDefaultJobs(t *testing.T) {
	half := max(1, runtime.NumCPU()/2)
	for _, c := range []struct {
		race  bool
		total uint64
		want  int
	}{
		{false, 0, half},
		{true, 0, half},
		{false, 1 << 40, half},
		{true, 16 << 30, 1},
		{false, 16 << 30, min(half, 4)},
		{true, 1 << 30, 1},
	} {
		if got := defaultJobs(c.race, c.total); got != c.want {
			t.Errorf("defaultJobs(race %v, %d GiB) = %d, want %d", c.race, c.total>>30, got, c.want)
		}
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if totalMemory() == 0 {
			t.Error("totalMemory found nothing on " + runtime.GOOS)
		}
	}
}
