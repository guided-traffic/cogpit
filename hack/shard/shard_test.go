package main

import (
	"bytes"
	"fmt"
	"os"
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

// Every name lands in exactly one shard, the same one each time, and a name added later does not
// move the others.
func TestSplitIsStableAndComplete(t *testing.T) {
	names := []string{"TestA", "TestB", "TestC", "TestD", "TestE", "TestF", "TestG", "TestH"}
	shards := split(names, 3)
	var all []string
	for _, s := range shards {
		all = append(all, s...)
	}
	slices.Sort(all)
	if !slices.Equal(all, names) {
		t.Fatalf("the shards hold %v, want each of %v once", all, names)
	}
	if again := split(names, 3); !slices.EqualFunc(shards, again, slices.Equal) {
		t.Errorf("split changed between calls: %v, then %v", shards, again)
	}
	grown := split(append(slices.Clone(names), "TestNew"), 3)
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
// top-level tests; what they leave keeps its shard. A slash, a subtest's level for go test, is
// refused, and so is an expression that does not compile.
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
		all, left := split(names, 3), split(got, 3)
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

// The package's own tests are found from its files, TestMain left out.
func TestTestNamesOfThisPackage(t *testing.T) {
	names, err := testNames(".", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestDefaultJobs", "TestExecShardsRunsEveryShard", "TestIsTest", "TestRaceKept",
		"TestSelection", "TestSplitIsStableAndComplete", "TestTags", "TestTestNamesOfThisPackage"}
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
	for i, mine := range split(names, 3) {
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
