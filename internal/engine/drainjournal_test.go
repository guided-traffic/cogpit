package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func drainEntry(context, node string) PendingDrain {
	yes := true
	return PendingDrain{
		Context: context, Node: node, Started: time.Date(2026, 10, 11, 9, 30, 0, 0, time.UTC), StartedBy: OriginGUI,
		State: DrainRunning, Settings: DrainSettings{IgnoreDaemonSets: &yes, TimeoutSeconds: 300},
	}
}

func TestFileDrainJournalRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config", "Cogpit")
	j := NewFileDrainJournal(dir)
	if got, err := j.List(); err != nil || len(got) != 0 {
		t.Fatalf("a missing journal: %v, %v", got, err)
	}
	if err := j.Remove("ctx", "nope"); err != nil {
		t.Fatalf("remove from a missing journal: %v", err)
	}
	if _, err := os.Stat(j.Path()); !os.IsNotExist(err) {
		t.Error("removing nothing created the file")
	}

	a, b := drainEntry("kind-cogpit", "worker-1"), drainEntry("kind-cogpit", "worker-2")
	for _, e := range []PendingDrain{a, b} {
		if err := j.Put(e); err != nil {
			t.Fatal(err)
		}
	}
	failed := a
	failed.State, failed.Error = DrainFailed, "timed out"
	if err := j.Put(failed); err != nil { // replaces worker-1's entry
		t.Fatal(err)
	}
	// A second journal on the same directory (the next start of Cogpit) reads the same.
	got, err := NewFileDrainJournal(dir).List()
	if err != nil || len(got) != 2 || got[0].Node != "worker-2" || got[1].Node != "worker-1" || got[1].State != DrainFailed ||
		got[1].Error != "timed out" || !got[1].Started.Equal(a.Started) || got[1].Settings.IgnoreDaemonSets == nil {
		t.Fatalf("read back: %+v, %v", got, err)
	}
	if err := j.Remove("kind-cogpit", "worker-2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := j.List(); len(got) != 1 || got[0].Node != "worker-1" {
		t.Errorf("after remove: %+v", got)
	}

	checkMode(t, j.Path(), 0o600)
	checkMode(t, dir, 0o700)
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "drains.json journals.lock owners" { // the lock and owners: H-22
		t.Errorf("temporary files left behind: %v", names)
	}
	checkMode(t, filepath.Join(dir, journalLockFile), 0o600)
	raw, _ := os.ReadFile(j.Path())
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil || doc["version"] != float64(1) {
		t.Errorf("file format: %s", raw)
	}
}

func TestFileDrainJournalTightensAWiderMode(t *testing.T) {
	dir := t.TempDir()
	j := NewFileDrainJournal(dir)
	if err := os.WriteFile(j.Path(), []byte(`{"version":1,"drains":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := j.Put(drainEntry("c", "n")); err != nil {
		t.Fatal(err)
	}
	checkMode(t, j.Path(), 0o600)
}

// TestFileDrainJournalReportsCorruption: a journal that cannot be read is an error naming the
// file, never a panic and never an empty list, and a write does not replace it.
func TestFileDrainJournalReportsCorruption(t *testing.T) {
	for name, content := range map[string]string{
		"truncated":     `{"version":1,"drains":[{"context":"c","node":`,
		"empty":         ``,
		"not json":      "\x00\x01garbage",
		"wrong version": `{"version":2,"drains":[]}`,
		"no node":       `{"version":1,"drains":[{"context":"c"}]}`,
		"wrong type":    `{"version":1,"drains":{"context":"c"}}`,
		"too large":     `{"version":1,"drains":[],"pad":"` + strings.Repeat("x", maxJournalSize) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			j := NewFileDrainJournal(t.TempDir())
			if err := os.WriteFile(j.Path(), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			var jerr *JournalError
			if got, err := j.List(); !errors.As(err, &jerr) || jerr.Path != j.Path() || got != nil {
				t.Errorf("List: %v, %v", got, err)
			}
			if err := j.Put(drainEntry("c", "n")); !errors.As(err, &jerr) {
				t.Errorf("Put: %v", err)
			}
			if err := j.Remove("c", "n"); !errors.As(err, &jerr) {
				t.Errorf("Remove: %v", err)
			}
			if got, _ := os.ReadFile(j.Path()); string(got) != content {
				t.Error("the corrupt journal was replaced")
			}
			if !strings.Contains(jerr.Error(), "move it aside") {
				t.Errorf("message %q", jerr.Error())
			}
		})
	}
}

func TestFileDrainJournalConcurrentWrites(t *testing.T) {
	j := NewFileDrainJournal(t.TempDir())
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			node := "worker-" + string(rune('a'+i))
			if err := j.Put(drainEntry("c", node)); err != nil {
				t.Error(err)
			}
			if i%2 == 0 {
				if err := j.Remove("c", node); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if got, err := j.List(); err != nil || len(got) != 10 {
		t.Errorf("after concurrent writes: %d entries, %v", len(got), err)
	}
}

func TestDrainSettingsDefaults(t *testing.T) {
	opts := DrainSettings{}.ClusterOptions()
	if !opts.IgnoreDaemonSets || opts.DeleteEmptyDirData || opts.Force || opts.GracePeriodSeconds != -1 || opts.Timeout != DefaultDrainTimeout {
		t.Errorf("defaults %+v", opts)
	}
	no := false
	opts = DrainSettings{IgnoreDaemonSets: &no, GracePeriodSeconds: 10, TimeoutSeconds: 30}.ClusterOptions()
	if opts.IgnoreDaemonSets || opts.GracePeriodSeconds != 10 || opts.Timeout != 30*time.Second {
		t.Errorf("explicit %+v", opts)
	}
}
