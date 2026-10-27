package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A durations file holds a package's measured tests, one top-level test a line as "<name>
// <seconds>", sorted by name, after a header of # lines (make ui-durations writes it with -exec
// -record). -durations reads it so that the split balances the shards by the tests' time.

// durationStep is the resolution of a durations file: go test reports a test's time in hundredths
// of a second. The split adds the times as integers, so every machine assigns a name to the same
// shard.
const durationStep = 10 * time.Millisecond

// maxSeconds bounds a test's time in a durations file, so that the shards' sums stay far from
// time.Duration's limit.
const maxSeconds = 1e6

// readDurationsFile reads a durations file; a line that is not a name and a number of seconds from
// 0 to maxSeconds, or a name given twice, is an error.
func readDurationsFile(path string) (map[string]time.Duration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	times := map[string]time.Duration{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("%s:%d: %q is not \"<test> <seconds>\"", path, i+1, line)
		}
		s, err := strconv.ParseFloat(f[1], 64)
		if err != nil || !(s >= 0 && s <= maxSeconds) {
			return nil, fmt.Errorf("%s:%d: %q is not a number of seconds", path, i+1, f[1])
		}
		if _, dup := times[f[0]]; dup {
			return nil, fmt.Errorf("%s:%d: %s is named twice", path, i+1, f[0])
		}
		times[f[0]] = roundDuration(s)
	}
	return times, nil
}

// roundDuration is s seconds at the file's resolution.
func roundDuration(s float64) time.Duration {
	return time.Duration(math.Round(s*float64(time.Second/durationStep))) * durationStep
}

// writeDurationsFile writes the times as a durations file under the header's lines, through a
// file beside it that replaces it once complete, so that an interrupted write leaves the old one.
func writeDurationsFile(path, header string, times map[string]time.Duration) error {
	var b bytes.Buffer
	for _, line := range strings.Split(header, "\n") {
		b.WriteString(strings.TrimRight("# "+line, " ") + "\n")
	}
	for _, name := range slices.Sorted(maps.Keys(times)) {
		fmt.Fprintf(&b, "%s %.2f\n", name, times[name].Seconds())
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// split assigns every name to one of n shards; each shard keeps the names' order. Without times a
// name's shard is the FNV-1a hash of the name, so a name keeps its shard while n stays the same.
// With times the names they hold go longest first, each to the shard with the least time so far
// (the first of them on a tie), which balances the shards' time; a test counts at least the file's
// resolution, so the tests that took no measurable time spread over the shards instead of all
// going to the one that is a hundredth behind. The names the times lack, tests added since they
// were measured, go by their hash.
func split(names []string, n int, times map[string]time.Duration) [][]string {
	shards := make([][]string, n)
	var known []string
	for _, name := range names {
		if _, ok := times[name]; ok {
			known = append(known, name)
			continue
		}
		i := hashShard(name, n)
		shards[i] = append(shards[i], name)
	}
	weight := func(name string) time.Duration { return max(times[name], durationStep) }
	slices.SortFunc(known, func(a, b string) int {
		if c := cmp.Compare(weight(b), weight(a)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	load := make([]time.Duration, n)
	for _, name := range known {
		i := 0
		for j := 1; j < n; j++ {
			if load[j] < load[i] {
				i = j
			}
		}
		shards[i] = append(shards[i], name)
		load[i] += weight(name)
	}
	if len(known) > 0 {
		order := make(map[string]int, len(names))
		for i, name := range names {
			order[name] = i
		}
		for _, s := range shards {
			slices.SortFunc(s, func(a, b string) int { return order[a] - order[b] })
		}
	}
	return shards
}

// hashShard is the shard of a name the times do not hold: its FNV-1a hash modulo n.
func hashShard(name string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return int(h.Sum32() % uint32(n))
}

// shardTime is the time the shard's tests took when measured, and how many of them the times lack.
func shardTime(names []string, times map[string]time.Duration) (sum time.Duration, unknown int) {
	for _, name := range names {
		if d, ok := times[name]; ok {
			sum += d
		} else {
			unknown++
		}
	}
	return sum, unknown
}

// testEvent is the part of a go test -json event the shards read.
type testEvent struct {
	Action  string
	Test    string
	Elapsed float64
	Output  string
}

// readTestJSON reads a shard's go test -json stream: the time of every top-level test that passed,
// failed or was skipped, and the output as go test without -json would have printed it (lines that
// are not events, such as the build's on its standard error, as they are).
func readTestJSON(stream []byte) (map[string]time.Duration, []byte, error) {
	times := map[string]time.Duration{}
	var text bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(stream))
	sc.Buffer(make([]byte, 1<<20), len(stream)+1)
	for sc.Scan() {
		var ev testEvent
		if line := sc.Bytes(); len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &ev) != nil {
			text.Write(line)
			text.WriteByte('\n')
			continue
		}
		text.WriteString(ev.Output)
		if ev.Test == "" || strings.Contains(ev.Test, "/") {
			continue
		}
		if ev.Action == "pass" || ev.Action == "fail" || ev.Action == "skip" {
			times[ev.Test] = roundDuration(ev.Elapsed)
		}
	}
	return times, text.Bytes(), sc.Err()
}
