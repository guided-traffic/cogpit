package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// execConfig is what -exec runs: the package's shards as the split assigns them, compiled once.
type execConfig struct {
	pkg, tags string    // the package, and the build tags its files are listed with (race with -race)
	sel       selection // the tests to split (-run, -skip)
	race      bool      // build with the race detector
	of, jobs  int       // the number of shards, and how many run at once
	timeout   time.Duration
	coverDir  string // write each shard's coverage profile here, if set
	coverName string // the profiles' name before -<n>.out
}

// shardMemory is what one shard of the UI's tests (internal/ui/fyneui, an eighth of them) takes at
// its peak, with some room: 2 GB measured without the race detector and 6.9 GB with it (an M1
// Ultra, 2026-10-21; it grows with the tests a process runs, as Fyne parses its fonts again for
// every test's app and go-text keeps every parsed face).
const (
	shardMemory     = 3 << 30
	shardMemoryRace = 8 << 30
)

// defaultJobs is how many shards run at once unless -jobs says otherwise: half the CPUs, as a
// shard of the UI's tests keeps about two busy (the test goroutine and the garbage collector), and
// no more than three quarters of the memory holds (total: the machine's bytes, 0 if unknown).
func defaultJobs(race bool, total uint64) int {
	jobs := max(1, runtime.NumCPU()/2)
	per := uint64(shardMemory)
	if race {
		per = shardMemoryRace
	}
	if total > 0 {
		jobs = min(jobs, max(1, int(total*3/4/per)))
	}
	return jobs
}

// totalMemory is the machine's memory in bytes, or the cgroup's limit where that is lower (a
// container on Linux); 0 where it cannot tell.
func totalMemory() uint64 {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0
		}
		n, _ := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
		return n
	case "linux":
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0
		}
		var total uint64
		for _, line := range strings.Split(string(data), "\n") {
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "MemTotal:" {
				kb, _ := strconv.ParseUint(f[1], 10, 64)
				total = kb << 10
			}
		}
		if limit, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
			if n, err := strconv.ParseUint(strings.TrimSpace(string(limit)), 10, 64); err == nil && n < total {
				total = n
			}
		}
		return total
	}
	return 0
}

// shardResult is one shard's run.
type shardResult struct {
	n, tests int
	took     time.Duration
	err      error
	out      []byte
	cached   bool
}

// windowsCommandLine is the length of a command line Windows' CreateProcess takes; a shard's -run
// pattern goes on the test binary's command line.
const windowsCommandLine = 32767

// execShards runs the package's c.of shards as go test runs of their own, c.jobs at once, after one
// go test run that builds the package's tests and runs none, so that the shards find them compiled
// (each links its own binary). As go test runs, every shard's result goes into Go's test cache under
// its -run pattern (a repeated make test-ui with nothing changed is answered from it, unless GOFLAGS
// says -count=1, as on CI), and go test's own guards apply (-test.paniconexit0, its wait for the
// binary's output). It prints a line per shard as it ends, then the output of every shard that
// failed, and reports whether all passed. Interrupting it (Ctrl-C) ends the running shards.
func execShards(c execConfig, w io.Writer) (bool, error) {
	names, err := testNames(c.pkg, c.tags)
	if err != nil {
		return false, err
	}
	if names = c.sel.keep(names); len(names) == 0 {
		return false, fmt.Errorf("%s has no tests%s", c.pkg, c.sel)
	}
	flags := []string{"test", "-timeout", c.timeout.String()}
	if c.race {
		flags = append(flags, "-race")
	}
	if t := withoutTag(c.tags, "race"); t != "" {
		flags = append(flags, "-tags", t)
	}
	if c.coverDir != "" {
		// atomic only under the race detector, which would report the counters' plain writes: the
		// profiles are merged as covered or not (hack/coverage), and set costs the least.
		mode := "set"
		if c.race {
			mode = "atomic"
		}
		flags = append(flags, "-covermode="+mode)
	}
	shards := split(names, c.of)
	runs := make([][]string, len(shards))
	for i, mine := range shards {
		if len(mine) == 0 {
			continue
		}
		args := slices.Clone(flags)
		// One shard of every test is the whole package: no pattern, which Windows might find too long.
		if c.of > 1 || c.sel.narrows() {
			p := pattern(mine)
			if runtime.GOOS == "windows" && len(p) > windowsCommandLine-1024 {
				return false, fmt.Errorf("shard %d of %d: its -run pattern of %d tests is %d characters, longer than a "+
					"Windows command line takes: run the package in more shards (WINDOWS_UI_SHARDS)", i+1, c.of, len(mine), len(p))
			}
			args = append(args, "-run", p)
		}
		if c.coverDir != "" {
			profile, err := filepath.Abs(filepath.Join(c.coverDir, fmt.Sprintf("%s-%d.out", c.coverName, i+1)))
			if err != nil {
				return false, err
			}
			args = append(args, "-coverprofile", profile)
		}
		runs[i] = append(args, c.pkg)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	start := time.Now()
	build := append(slices.Clone(flags), "-run", "^$", c.pkg)
	if out, err := exec.CommandContext(ctx, "go", build...).CombinedOutput(); err != nil {
		_, _ = w.Write(out)
		return false, fmt.Errorf("go %s: %w", strings.Join(build, " "), err)
	}
	fmt.Fprintf(w, "shard: built %s in %.1f s; %d tests%s in %d shards, %d at once\n",
		c.pkg, time.Since(start).Seconds(), len(names), c.sel, c.of, min(c.jobs, c.of))

	results := make([]shardResult, len(shards))
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, c.jobs)
	for i, mine := range shards {
		if runs[i] == nil {
			results[i] = shardResult{n: i + 1}
			continue
		}
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			var out bytes.Buffer
			run := exec.CommandContext(ctx, "go", runs[i]...)
			run.Stdout, run.Stderr = &out, &out
			began := time.Now()
			err := run.Run()
			r := shardResult{n: i + 1, tests: len(mine), took: time.Since(began), err: err, out: out.Bytes(),
				cached: bytes.Contains(out.Bytes(), []byte("(cached)"))}
			results[i] = r
			mu.Lock()
			defer mu.Unlock()
			verdict := "ok"
			switch {
			case err != nil:
				verdict = "FAIL"
			case r.cached:
				verdict = "ok (cached)"
			}
			fmt.Fprintf(w, "shard %d/%d: %s, %d tests, %.1f s\n", r.n, c.of, verdict, r.tests, r.took.Seconds())
		})
	}
	wg.Wait()

	passed := true
	for _, r := range results {
		if r.tests == 0 {
			fmt.Fprintf(w, "shard %d/%d: no tests\n", r.n, c.of)
			continue
		}
		if r.err == nil {
			continue
		}
		passed = false
		fmt.Fprintf(w, "\n--- shard %d/%d failed: %v\n", r.n, c.of, r.err)
		_, _ = w.Write(r.out)
	}
	verdict := "all passed"
	if !passed {
		verdict = "FAILED"
	}
	fmt.Fprintf(w, "shard: %s, %d shards of %s in %.1f s\n", verdict, c.of, c.pkg, time.Since(start).Seconds())
	return passed, nil
}

// joinTags adds tag to the comma-separated tags unless it is there already.
func joinTags(tags, tag string) string {
	for _, t := range strings.Split(tags, ",") {
		if t == tag {
			return tags
		}
	}
	if tags == "" {
		return tag
	}
	return tags + "," + tag
}

// withoutTag is the comma-separated tags without tag: -race sets the race tag itself, and the tag
// without the detector would build the race files into a binary that has none.
func withoutTag(tags, tag string) string {
	var kept []string
	for _, t := range strings.Split(tags, ",") {
		if t != "" && t != tag {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, ",")
}
