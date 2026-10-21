package cluster

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// discoveryToken is the bearer token of the discovery cache's tests: neither the file's name nor its
// content may carry it.
const discoveryToken = "tok-disco-7d1e-SECRET-must-not-reach-the-disk"

// discoConnect connects to srv through kubeconfig, keeping discovery in dir.
func discoConnect(t *testing.T, kubeconfig, dir string, ttl time.Duration) *Conn {
	t.Helper()
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{kubeconfig}, DiscoveryCacheDir: dir, DiscoveryCacheTTL: ttl})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// discoveryRequests counts a connection's discovery requests, Connect's GET /version included.
func discoveryRequests(c *Conn) int64 { return c.Counter().Snapshot().ByVerb[VerbDiscovery] }

func served(t *testing.T, c *Conn) map[schema.GroupVersionResource]bool {
	t.Helper()
	got, err := c.ServedResources(context.Background())
	if err != nil {
		t.Fatalf("ServedResources: %v", err)
	}
	return got
}

// readDiscoveryFile reads a connection's cache file.
func readDiscoveryFile(t *testing.T, c *Conn) discoveryFile {
	t.Helper()
	data, err := os.ReadFile(c.discoCfg.file)
	if err != nil {
		t.Fatalf("the discovery cache file: %v", err)
	}
	var f discoveryFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// rewriteDiscoveryFile changes a connection's cache file.
func rewriteDiscoveryFile(t *testing.T, c *Conn, change func(*discoveryFile)) {
	t.Helper()
	f := readDiscoveryFile(t, c)
	change(&f)
	if err := writeOwnerOnly(c.discoCfg.file, f); err != nil {
		t.Fatal(err)
	}
}

// The next connection reads discovery from the file the first one wrote: no discovery request
// beyond Connect's GET /version, the same served resources, and the REST mapper of the object
// writes takes the same answer. The file is owner-only and carries neither the token nor the
// context's or user's name.
func TestDiscoveryCacheServesTheNextConnection(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: discoveryToken})
	defer srv.Close()
	kubeconfig := srv.WriteKubeconfig(t, "team-ctx", "")
	dir := filepath.Join(t.TempDir(), "discovery")

	first := discoConnect(t, kubeconfig, dir, 0)
	want := served(t, first)
	asked := discoveryRequests(first)
	if asked < 3 {
		t.Fatalf("the first connection asked %d discovery requests, want /version, /api, /apis and more", asked)
	}
	if info := first.DiscoveryInfo(); info.Source != "server" || !info.Persisted || info.Checked || info.Resources != len(want) {
		t.Errorf("first connection's discovery info %+v (%d served)", info, len(want))
	}

	fi, err := os.Stat(first.discoCfg.file)
	if err != nil {
		t.Fatalf("no cache file: %v", err)
	}
	// Windows sets no file modes: there the user profile's permissions protect the cache.
	if runtime.GOOS != "windows" {
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("cache file mode %v, want 0600", fi.Mode().Perm())
		}
		if di, err := os.Stat(dir); err != nil || di.Mode().Perm() != 0o700 {
			t.Errorf("cache directory mode %v (%v), want 0700", di.Mode().Perm(), err)
		}
	}
	data, _ := os.ReadFile(first.discoCfg.file)
	for _, secret := range []string{discoveryToken, "team-ctx"} {
		if strings.Contains(string(data), secret) || strings.Contains(first.discoCfg.file, secret) {
			t.Errorf("the cache file or its name carries %q", secret)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("cache directory holds %d entries, want the one file (no temporary file left)", len(entries))
	}

	second := discoConnect(t, kubeconfig, dir, 0)
	if got := served(t, second); !reflect.DeepEqual(got, want) {
		t.Errorf("from the file: %v, want %v", got, want)
	}
	if _, err := second.MapResource(context.Background(), "apps", "deployments"); err != nil {
		t.Errorf("MapResource from the cached discovery: %v", err)
	}
	if n := discoveryRequests(second); n != 1 {
		t.Errorf("the second connection asked %d discovery requests, want Connect's GET /version alone", n)
	}
	if info := second.DiscoveryInfo(); info.Source != "disk" || info.Groups == 0 {
		t.Errorf("second connection's discovery info %+v", info)
	}
}

// Without a cache directory nothing is written and each connection asks.
func TestDiscoveryWithoutCacheDirStaysInMemory(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	kubeconfig := srv.WriteKubeconfig(t, "fake", "")
	c := discoConnect(t, kubeconfig, "", 0)
	served(t, c)
	before := discoveryRequests(c)
	served(t, c)
	if discoveryRequests(c) != before {
		t.Error("the answer in memory was asked again")
	}
	if c.discoCfg.file != "" || c.DiscoveryInfo().Persisted {
		t.Errorf("a file without a directory: %q", c.discoCfg.file)
	}
	again := discoConnect(t, kubeconfig, "", 0)
	served(t, again)
	if discoveryRequests(again) < 3 {
		t.Error("a connection without a cache directory did not ask the server")
	}
}

// The file is not used for another server version, another identity, after its TTL (ten minutes
// while no definitions checked it, the configured TTL once they did), with a fetch time in the
// future, or when it does not parse; the connection then asks and writes it again.
func TestDiscoveryCacheFileIsCheckedBeforeUse(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	kubeconfig := srv.WriteKubeconfig(t, "fake", "")
	dir := t.TempDir()
	seed := discoConnect(t, kubeconfig, dir, time.Hour)
	served(t, seed)

	asks := func(name string, change func(*discoveryFile), wantAsked bool) {
		t.Helper()
		rewriteDiscoveryFile(t, seed, change)
		c := discoConnect(t, kubeconfig, dir, time.Hour)
		served(t, c)
		if asked := discoveryRequests(c) > 1; asked != wantAsked {
			t.Errorf("%s: asked the server %v, want %v", name, asked, wantAsked)
		}
	}
	asks("as written", func(*discoveryFile) {}, false)
	asks("another server version", func(f *discoveryFile) { f.Version = "v1.36.0" }, true)
	asks("another identity", func(f *discoveryFile) { f.Key = strings.Repeat("0", 64) }, true)
	asks("another format", func(f *discoveryFile) { f.Format = 2 }, true)
	asks("unchecked, 9 minutes old", func(f *discoveryFile) { f.Definitions, f.FetchedAt = "", time.Now().Add(-9*time.Minute) }, false)
	asks("unchecked, 11 minutes old", func(f *discoveryFile) { f.Definitions, f.FetchedAt = "", time.Now().Add(-11*time.Minute) }, true)
	asks("checked, 50 minutes old", func(f *discoveryFile) { f.Definitions, f.FetchedAt = "d1", time.Now().Add(-50*time.Minute) }, false)
	asks("checked, 61 minutes old", func(f *discoveryFile) { f.Definitions, f.FetchedAt = "d1", time.Now().Add(-61*time.Minute) }, true)
	asks("fetched in the future", func(f *discoveryFile) { f.FetchedAt = time.Now().Add(time.Hour) }, true)

	if err := os.WriteFile(seed.discoCfg.file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := discoConnect(t, kubeconfig, dir, time.Hour)
	served(t, c)
	if discoveryRequests(c) == 1 {
		t.Error("a file that does not parse was used")
	}
	if f := readDiscoveryFile(t, c); f.Format != discoveryFileFormat || len(f.Resources) == 0 {
		t.Errorf("the file was not written again: %+v", f)
	}
}

// Another user entry for the same server, in the same file, keeps a file of its own.
func TestDiscoveryCacheIsPerIdentity(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	dir := t.TempDir()
	path := srv.WriteKubeconfig(t, "fake", "")
	one := discoConnect(t, path, dir, 0)
	served(t, one)
	other := strings.ReplaceAll(string(srv.Kubeconfig("fake", "")), "- name: fake\n  user:", "- name: other-user\n  user:")
	other = strings.ReplaceAll(other, "user: fake", "user: other-user")
	if !strings.Contains(other, "- name: other-user") {
		t.Fatal("the kubeconfig's user entry was not renamed")
	}
	if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	two := discoConnect(t, path, dir, 0)
	served(t, two)
	if one.discoCfg.file == two.discoCfg.file {
		t.Fatal("two identities share a cache file")
	}
	if discoveryRequests(two) < 3 {
		t.Error("another identity read the first one's answer")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("%d cache files, want one per identity", len(entries))
	}
}

// The custom resource definitions confirm the answer: one asked in this connection takes their
// digest (and is reused for the long TTL from then on); the same digest later changes nothing;
// another digest drops the answer from memory and disk, and the next read asks the server and
// writes the answer with the new digest. A file nobody checked is dropped at the first digest.
func TestDefinitionsConfirmOrDropTheDiscoveryCache(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	kubeconfig := srv.WriteKubeconfig(t, "fake", "")
	dir := t.TempDir()

	c := discoConnect(t, kubeconfig, dir, 0)
	served(t, c)
	if c.DefinitionsSeen("d1") {
		t.Fatal("an answer asked in this connection was dropped at the first digest")
	}
	if f := readDiscoveryFile(t, c); f.Definitions != "d1" {
		t.Fatalf("the file's digest %q, want d1", f.Definitions)
	}
	if !c.DiscoveryInfo().Checked {
		t.Error("the answer does not say it was checked")
	}

	next := discoConnect(t, kubeconfig, dir, 0)
	served(t, next)
	if next.DefinitionsSeen("d1") || discoveryRequests(next) != 1 {
		t.Fatalf("a confirmed answer: dropped or asked (%d discovery requests)", discoveryRequests(next))
	}
	if !next.DefinitionsSeen("d2") {
		t.Fatal("another digest kept the answer")
	}
	if _, err := os.Stat(next.discoCfg.file); !os.IsNotExist(err) {
		t.Errorf("the dropped answer's file is still there: %v", err)
	}
	before := discoveryRequests(next)
	served(t, next)
	if discoveryRequests(next) == before {
		t.Error("the next read after the drop did not ask the server")
	}
	if f := readDiscoveryFile(t, next); f.Definitions != "d2" {
		t.Errorf("the answer asked after the drop carries the digest %q, want d2", f.Definitions)
	}

	// A file written before any digest (the window quit before the definitions were listed).
	rewriteDiscoveryFile(t, next, func(f *discoveryFile) { f.Definitions = "" })
	unchecked := discoConnect(t, kubeconfig, dir, 0)
	served(t, unchecked)
	if discoveryRequests(unchecked) != 1 {
		t.Fatal("the unchecked file was not read")
	}
	if !unchecked.DefinitionsSeen("d2") {
		t.Error("a file nobody checked was kept at the first digest")
	}
}

// A partial answer (an aggregated API that does not answer) is used for a minute and never
// written, so the next connection asks again.
func TestPartialDiscoveryIsNotWritten(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{BrokenGroups: []string{"metrics.k8s.io"}})
	defer srv.Close()
	kubeconfig := srv.WriteKubeconfig(t, "fake", "")
	dir := t.TempDir()
	c := discoConnect(t, kubeconfig, dir, 0)
	if got := served(t, c); !got[testkit.PodsGVR] {
		t.Fatalf("partial answer without pods: %v", got)
	}
	if !c.DiscoveryInfo().Partial {
		t.Error("the partial answer does not say so")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a partial answer was written: %d files", len(entries))
	}
	c.DefinitionsSeen("d1") // checked or not, partial stays unwritten
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a partial answer was written after the digest: %d files", len(entries))
	}
}

// A kind the cached answer does not know resets the mapper, which asks the server again: a
// definition created after the answer was cached is found.
func TestMapKindAsksAgainForAKindTheCacheDoesNotKnow(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	kubeconfig := srv.WriteKubeconfig(t, "fake", "")
	dir := t.TempDir()
	served(t, discoConnect(t, kubeconfig, dir, 0))
	c := discoConnect(t, kubeconfig, dir, 0)
	if _, err := c.MapKind(context.Background(), "v1", "Pod"); err != nil || discoveryRequests(c) != 1 {
		t.Fatalf("MapKind from the file: %v, %d discovery requests", err, discoveryRequests(c))
	}
	if _, err := c.MapKind(context.Background(), "example.com/v1", "Widget"); err == nil {
		t.Fatal("an unknown kind mapped")
	}
	if discoveryRequests(c) == 1 {
		t.Error("an unknown kind did not ask the server again")
	}
}
