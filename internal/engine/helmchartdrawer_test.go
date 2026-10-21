package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A load the drawer starts stops the drawer's previous load of another chart (dropped, so that it
// starts again when asked), unless an agent waits for it; a load that fails is tried again only
// after readmeRetryAfter; a panicking load fails.
func TestReadmeStoreLoads(t *testing.T) {
	rs := &readmeStore{}
	now := time.Unix(1_000_000, 0)
	clock := func() time.Time { return now }
	a, b := readmeKey{chart: "a", version: "1"}, readmeKey{chart: "b", version: "1"}
	blocked := func(ctx context.Context) (helm.Readme, bool, error) {
		<-ctx.Done()
		return helm.Readme{}, false, ctx.Err()
	}
	ended := make(chan string, 4)
	ea := rs.start(a, true, clock, blocked, func() { ended <- "a" })
	if st, _ := rs.view(ea); st.State != ReadmeLoading {
		t.Fatalf("a: %+v", st)
	}
	eb := rs.start(b, true, clock, func(context.Context) (helm.Readme, bool, error) {
		return helm.Readme{File: "README.md", Text: "# B\n", Size: 4}, true, nil
	}, func() { ended <- "b" })
	<-ea.done
	<-eb.done
	if got := <-ended; got != "b" {
		t.Errorf("ended %s", got)
	}
	if rs.get(a, now) != nil {
		t.Error("the stopped load of a is kept")
	}
	if st, text := rs.view(eb); st.State != ReadmeLoaded || text != "# B\n" || !st.Verified {
		t.Errorf("b: %+v %q", st, text)
	}

	// An agent waits for c: the drawer's load of d does not stop it.
	c, d := readmeKey{chart: "c"}, readmeKey{chart: "d"}
	release := make(chan struct{})
	ec := rs.start(c, true, clock, func(context.Context) (helm.Readme, bool, error) {
		<-release
		return helm.Readme{}, true, nil
	}, func() {})
	waited := make(chan struct{})
	go func() { rs.wait(context.Background(), ec, time.Minute); close(waited) }()
	for {
		rs.mu.Lock()
		w := ec.waiters
		rs.mu.Unlock()
		if w == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	rs.start(d, true, clock, func(context.Context) (helm.Readme, bool, error) { return helm.Readme{}, true, nil }, func() {})
	close(release)
	<-waited
	if st, _ := rs.view(ec); st.State != ReadmeNone {
		t.Errorf("c with an agent waiting: %+v", st)
	}

	failed := readmeKey{chart: "f"}
	ef := rs.start(failed, false, clock, func(context.Context) (helm.Readme, bool, error) { panic("boom") }, func() {})
	<-ef.done
	if st, _ := rs.view(ef); st.State != ReadmeFailed || !strings.Contains(st.Error, "internal error") {
		t.Errorf("a panicking load: %+v", st)
	}
	if rs.get(failed, now.Add(readmeRetryAfter-time.Second)) == nil {
		t.Error("a failure is retried at once")
	}
	if rs.get(failed, now.Add(readmeRetryAfter)) != nil {
		t.Error("a failure is not retried after readmeRetryAfter")
	}
}

// Memory holds the last maxReadmesKept READMEs; the cache folder the last maxReadmeFiles. A kept
// file is read back checked: a tampered one (too large, not JSON, another name, a symlink) is
// ignored, and its text is cleaned again.
func TestReadmeStoreBounds(t *testing.T) {
	rs := &readmeStore{}
	now := time.Unix(1_000_000, 0)
	for i := range maxReadmesKept + 5 {
		rs.mu.Lock()
		rs.put(&readmeEntry{key: readmeKey{chart: fmt.Sprint(i)}, done: closedDone})
		rs.mu.Unlock()
	}
	if len(rs.entries) != maxReadmesKept || rs.entries[readmeKey{chart: "0"}] != nil || rs.entries[readmeKey{chart: fmt.Sprint(maxReadmesKept + 4)}] == nil {
		t.Errorf("held %d", len(rs.entries))
	}

	dir := t.TempDir()
	disk := &readmeStore{dir: dir}
	key := readmeKey{repo: "r", chart: "c", version: "1", digest: "ab"}
	disk.keep(key, helm.Readme{File: "README.md", Text: "# <b>C</b> & \"q\"\n", Size: 20})
	e := disk.get(key, now)
	if e == nil || e.readme.Text != "# <b>C</b> & \"q\"\n" || e.readme.Size != 20 || !e.verified {
		t.Fatalf("read back %+v", e)
	}
	path := filepath.Join(dir, key.file())
	for what, content := range map[string]string{
		"not JSON":         "# README",
		"another name":     `{"file":"values.yaml","text":"x","size":1}`,
		"a size below 0":   `{"file":"README.md","text":"x","size":-1}`,
		"text over bounds": `{"file":"README.md","text":"` + strings.Repeat("x", helm.MaxReadmeBytes+1) + `","size":1}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := readReadmeFile(path); ok {
			t.Errorf("%s: read", what)
		}
	}
	if err := os.WriteFile(path, []byte(`{"file":"readme","text":"a\u202eb\u0000c","size":5}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if r, ok := readReadmeFile(path); !ok || r.Text != "abc" {
		t.Errorf("cleaned again: %+v %v", r, ok)
	}
	if err := os.WriteFile(path, make([]byte, maxReadmeFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readReadmeFile(path); ok {
		t.Error("an oversized file was read")
	}
	_ = os.Remove(path)
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(target, []byte(`{"file":"README.md","text":"x","size":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	switch err := os.Symlink(target, path); {
	case err != nil && runtime.GOOS == "windows":
		t.Log("no symlink:", err) // Windows makes them only in Developer Mode or as an administrator
	case err != nil:
		t.Fatal(err)
	default:
		if _, ok := readReadmeFile(path); ok {
			t.Error("a symlink was followed")
		}
	}

	prune := t.TempDir()
	for i := range 6 {
		name := filepath.Join(prune, readmeKey{chart: fmt.Sprint(i)}.file())
		if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := now.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(name, at, at); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(prune, "notes.txt")
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pruneReadmeFiles(prune, 4)
	left, _ := filepath.Glob(filepath.Join(prune, "*.json"))
	if len(left) != 4 {
		t.Errorf("left %d", len(left))
	}
	for i := range 2 {
		if _, err := os.Stat(filepath.Join(prune, readmeKey{chart: fmt.Sprint(i)}.file())); err == nil {
			t.Errorf("the oldest %d is kept", i)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("a file not Cogpit's was removed")
	}
}

// A chart drawer's version ends when the drawer closes or shows another object, unless the change
// that moved the drawer set it.
func TestSyncDrawerChartVersion(t *testing.T) {
	chart := model.ObjectRef{Context: "kind", Kind: "helmcharts", Name: "test.docs"}
	other := model.ObjectRef{Context: "kind", Kind: "helmcharts", Name: "test.web"}
	pod := model.ObjectRef{Context: "kind", Kind: "pods", Namespace: "default", Name: "web-0"}
	for _, tc := range []struct {
		name      string
		old, now  UIState
		wantKeeps bool
	}{
		{"same drawer", UIState{Drawer: &chart, DrawerChartVersion: "1"}, UIState{Drawer: &chart, DrawerChartVersion: "1"}, true},
		{"another chart", UIState{Drawer: &chart, DrawerChartVersion: "1"}, UIState{Drawer: &other, DrawerChartVersion: "1"}, false},
		{"closed", UIState{Drawer: &chart, DrawerChartVersion: "1"}, UIState{DrawerChartVersion: "1"}, false},
		{"a pod", UIState{Drawer: &chart, DrawerChartVersion: "1"}, UIState{Drawer: &pod, DrawerChartVersion: "1"}, false},
		{"set with the move", UIState{Drawer: &chart}, UIState{Drawer: &other, DrawerChartVersion: "2"}, true},
	} {
		u := tc.now
		u.syncDrawerChartVersion(tc.old)
		if (u.DrawerChartVersion != "") != tc.wantKeeps {
			t.Errorf("%s: version %q", tc.name, u.DrawerChartVersion)
		}
	}
}
