package agent

import (
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit/fakellm"
)

// The API keys in the owner-only key file (keystore.go; the owner's decision of 2026-10-15, no
// keychain, D-10).

// fakeKey is an obviously fake API key. Its bytes may be in the key file and in the Authorization
// header of a request to the fake endpoint, nowhere else.
const fakeKey = "sk-cogpit-FAKE-key-file-test-0123456789"

// fileKeyAgent replaces the harness's agent with one that keeps its keys in KeysFile in h.dir, as
// the desktop binary does (Options.Keys nil), and returns the agent's log.
func (h *harness) fileKeyAgent(t *testing.T) *syncBuffer {
	t.Helper()
	logs := &syncBuffer{}
	h.a = New(Options{Session: h.s, Dir: h.dir, NotifyEvery: time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	if _, ok := h.a.keys.(*FileKeyStore); !ok {
		t.Fatalf("the key store is a %T, want the key file", h.a.keys)
	}
	return logs
}

// keyFileText reads the key file and checks that it is owner-only.
func keyFileText(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, KeysFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(path); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) || !fi.Mode().IsRegular() {
		t.Errorf("the key file: %v, mode %v", err, fi.Mode())
	}
	return string(data)
}

// writeConnections writes the connections file as an earlier Cogpit left it.
func writeConnections(t *testing.T, dir string, conns ...engine.ChatConnection) {
	t.Helper()
	data, err := json.Marshal(connectionsFile{Version: fileVersion, Connections: conns})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.WritePrivateFile(filepath.Join(dir, ConnectionsFile), data); err != nil {
		t.Fatal(err)
	}
}

// shownConnection returns the connection with id as Connections shows it.
func shownConnection(t *testing.T, a *Agent, id string) engine.ChatConnection {
	t.Helper()
	conns, err := a.Connections()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range conns {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no connection %s in %+v", id, conns)
	return engine.ChatConnection{}
}

// TestKeyFileKeepsTheKey: a saved key lands in the key file (0600) and nowhere else, the
// connection has a key, a turn sends it in the Authorization header, the next start reads it back,
// Remove Key ("") and deleting the connection take it out of the file, and the log, the audit log,
// the conversation and the files beside it never hold it.
func TestKeyFileKeepsTheKey(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	logs := h.fileKeyAgent(t)
	h.llm.RequireKey(fakeKey)

	conn, err := h.a.SaveConnection(h.conn, ptr(fakeKey))
	if err != nil || !conn.HasKey {
		t.Fatalf("saved %+v, %v", conn, err)
	}
	if got := keyFileText(t, h.dir); !strings.Contains(got, fakeKey) || !strings.Contains(got, conn.ID) {
		t.Fatalf("the key file holds no key for %s", conn.ID)
	}
	if data, _ := os.ReadFile(filepath.Join(h.dir, ConnectionsFile)); strings.Contains(string(data), fakeKey) {
		t.Fatal("the connections file holds the key")
	}

	h.llm.Script(fakellm.Reply{Text: []string{"Hello back."}})
	c := h.idle(t, h.start(t, "Hello"))
	if got := c.Items[len(c.Items)-1]; got.Text != "Hello back." {
		t.Fatalf("items %v, notes %q", itemKinds(c), notes(c))
	}
	for _, r := range h.llm.Requests() {
		if r.Auth != "Bearer "+fakeKey || strings.Contains(string(r.Body), fakeKey) {
			t.Errorf("request %s %s: Authorization %q, the key in its body: %v", r.Method, r.Path, r.Auth, strings.Contains(string(r.Body), fakeKey))
		}
	}

	// The next start reads it back.
	b := New(Options{Session: h.s, Dir: h.dir, Logger: slog.New(slog.NewTextHandler(logs, nil))})
	if got := shownConnection(t, b, conn.ID); !got.HasKey {
		t.Errorf("read back without a key: %+v", got)
	}
	if k, err := b.keys.Get(conn.ID); err != nil || k != fakeKey {
		t.Errorf("read back: %v", err)
	}

	// Remove Key.
	if s, err := h.a.SaveConnection(conn, ptr("")); err != nil || s.HasKey {
		t.Fatalf("%+v %v", s, err)
	}
	if strings.Contains(keyFileText(t, h.dir), fakeKey) {
		t.Error("Remove Key left the key in the file")
	}
	// Deleting the connection.
	if _, err := h.a.SaveConnection(conn, ptr(fakeKey)); err != nil {
		t.Fatal(err)
	}
	if err := h.a.DeleteConnection(conn.ID); err != nil {
		t.Fatal(err)
	}
	if got := keyFileText(t, h.dir); strings.Contains(got, fakeKey) || strings.Contains(got, conn.ID) {
		t.Error("deleting the connection left its key in the file")
	}

	kept, err := os.ReadFile(filepath.Join(h.dir, ConversationsFile))
	if err != nil {
		t.Fatal(err)
	}
	conv, _ := json.Marshal(c)
	for what, text := range map[string]string{"the log": logs.String(), "the audit log": h.audit.String(),
		"the conversation": string(conv), "the conversations file": string(kept), "the notes": notes(c)} {
		if strings.Contains(text, fakeKey) {
			t.Errorf("%s holds the key", what)
		}
	}
}

// TestKeyFromTheKeychainIsAskedForAgain: connections an older Cogpit saved, whose keys the macOS
// keychain kept (hasKey in the connections file, nothing in the key file), show no key; a turn asks
// for the key again without naming the keychain and sends nothing; Cogpit has no keychain code to
// read the old item with (TestNoKeychainCode). The chat's model menu (nil) keeps asking; a key
// entered again works; saving the form empty gives the key up.
func TestKeyFromTheKeychainIsAskedForAgain(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	old, other := h.conn, h.conn
	old.HasKey, old.Default = true, true
	other.ID, other.Name, other.HasKey, other.Default = "0ld0ld0ld0ld0ld0", "older", true, false
	writeConnections(t, h.dir, old, other)
	logs := h.fileKeyAgent(t)

	for _, id := range []string{old.ID, other.ID} {
		if got := shownConnection(t, h.a, id); got.HasKey {
			t.Errorf("%s shows a key it does not have", got.Name)
		}
	}
	c := h.idle(t, h.start(t, "Hello"))
	n := notes(c)
	if !strings.Contains(n, "API key is missing: enter it again") || strings.Contains(strings.ToLower(n), "keychain") {
		t.Errorf("note %q", n)
	}
	if got := len(h.llm.ChatRequests()); got != 0 {
		t.Errorf("%d requests sent without the key", got)
	}

	// The chat's model menu saves with nil: the key stays missing, across a restart too.
	shown := shownConnection(t, h.a, old.ID)
	shown.Model = "another-model"
	if s, err := h.a.SaveConnection(shown, nil); err != nil || s.HasKey {
		t.Fatalf("%+v %v", s, err)
	}
	b := New(Options{Session: h.s, Dir: h.dir, Logger: slog.New(slog.NewTextHandler(logs, nil))})
	b.mu.Lock()
	stored, _ := b.connectionLocked(old.ID)
	b.mu.Unlock()
	if _, err := b.keyFor(stored); !errors.Is(err, ErrKeyMissing) {
		t.Errorf("after a restart: %v, want ErrKeyMissing", err)
	}

	// The form saved with the field empty ("Empty: no key"): no key, none asked for.
	if s, err := h.a.SaveConnection(other, ptr("")); err != nil || s.HasKey {
		t.Fatalf("%+v %v", s, err)
	}
	conv, err := h.a.NewConversation("test", other.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.llm.Script(fakellm.Reply{Text: []string{"No key needed."}})
	if err := h.a.Send(conv.ID, engine.ChatUserMessage{Text: "Hello"}); err != nil {
		t.Fatal(err)
	}
	if got := h.idle(t, conv.ID); got.Items[len(got.Items)-1].Text != "No key needed." {
		t.Errorf("without a key: %v %q", itemKinds(got), notes(got))
	}

	// The key entered again.
	h.llm.RequireKey(fakeKey)
	if s, err := h.a.SaveConnection(shown, ptr(fakeKey)); err != nil || !s.HasKey {
		t.Fatalf("%+v %v", s, err)
	}
	h.llm.Script(fakellm.Reply{Text: []string{"Hi."}})
	if got := h.idle(t, h.start(t, "Hello")); got.Items[len(got.Items)-1].Text != "Hi." {
		t.Errorf("with the key entered again: %v %q", itemKinds(got), notes(got))
	}
	if strings.Contains(logs.String(), fakeKey) || strings.Contains(strings.ToLower(logs.String()), "keychain") {
		t.Errorf("the log: %s", logs.String())
	}
}

// TestKeyFileAccessibleByOthersIsRepaired: a key file other users may read (a copy restored
// without its mode) is made owner-only before its keys are used, and the log says so with the
// file's mode, never the key (H-99).
func TestKeyFileAccessibleByOthersIsRepaired(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not express access on Windows")
	}
	h := newHarness(t, engine.ChatLimits{})
	withKey := h.conn
	withKey.HasKey = true
	writeConnections(t, h.dir, withKey)
	path := filepath.Join(h.dir, KeysFile)
	data, _ := json.Marshal(keysFile{Version: fileVersion, Keys: map[string]string{withKey.ID: fakeKey}})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	logs := h.fileKeyAgent(t)
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the key file: %v, mode %v, want 0600", err, fi.Mode().Perm())
	}
	if !strings.Contains(keyFileText(t, h.dir), fakeKey) {
		t.Fatal("the repair lost the key")
	}
	if got := shownConnection(t, h.a, withKey.ID); !got.HasKey {
		t.Errorf("after the repair: %+v", got)
	}
	log := logs.String()
	if !strings.Contains(log, "accessible by other users") || !strings.Contains(log, "0644") || strings.Contains(log, fakeKey) {
		t.Errorf("the log: %s", log)
	}
	h.llm.RequireKey(fakeKey)
	h.llm.Script(fakellm.Reply{Text: []string{"Hi."}})
	if c := h.idle(t, h.start(t, "Hello")); c.Items[len(c.Items)-1].Text != "Hi." {
		t.Errorf("after the repair: %v %q", itemKinds(c), notes(c))
	}
}

// TestFileKeyStoreRefuses: a missing file holds no key and is not created by a read or a delete; a
// file that does not decode fails a read without quoting its bytes, and the next key saved
// replaces it; a symlink is neither read nor written through.
func TestFileKeyStoreRefuses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, KeysFile)
	logs := &syncBuffer{}
	s := NewFileKeyStore(path, slog.New(slog.NewTextHandler(logs, nil)))
	if _, err := s.Get("a"); !errors.Is(err, ErrNoKey) {
		t.Errorf("Get of a missing file: %v", err)
	}
	if err := s.Delete("a"); err != nil {
		t.Errorf("Delete of a missing file: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a read or a delete created the file: %v", err)
	}

	const digits = "4711471147114711"
	for _, junk := range []string{`{"version":1,"keys":{"a":"` + fakeKey + `"`, `{"keys":{"a":` + digits + `}}`, fakeKey} {
		if err := os.WriteFile(path, []byte(junk), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := s.Get("a")
		if !errors.Is(err, errKeysFileInvalid) || strings.Contains(err.Error(), fakeKey) || strings.Contains(err.Error(), digits) {
			t.Errorf("Get of %d bytes of junk: %v", len(junk), err)
		}
	}
	if err := s.Set("b", "sk-second-FAKE-key"); err != nil {
		t.Fatal(err)
	}
	if got := keyFileText(t, dir); strings.Contains(got, fakeKey) || !strings.Contains(got, "sk-second-FAKE-key") {
		t.Errorf("the file after Set: %d bytes, the junk gone: %v", len(got), !strings.Contains(got, fakeKey))
	}
	if k, err := s.Get("b"); err != nil || k != "sk-second-FAKE-key" {
		t.Errorf("Get after Set: %v", err)
	}
	if strings.Contains(logs.String(), fakeKey) || strings.Contains(logs.String(), digits) {
		t.Errorf("the log quotes the file: %s", logs.String())
	}

	if runtime.GOOS == "windows" {
		return // symlinks need a privilege there
	}
	target := filepath.Join(dir, "elsewhere.json")
	const targetText = `{"version":1,"keys":{"x":"sk-elsewhere-FAKE"}}`
	if err := os.WriteFile(target, []byte(targetText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if k, err := s.Get("x"); err == nil || k != "" {
		t.Errorf("Get through a symlink: %q, %v", k, err)
	}
	if err := s.Set("x", "sk-new-FAKE"); err == nil {
		t.Error("Set wrote through a symlink")
	}
	if data, _ := os.ReadFile(target); string(data) != targetText {
		t.Errorf("the symlink's target changed: %s", data)
	}
}

// TestNoKeychainCode: the package has no cgo and no keychain call, so nothing the chat does makes
// macOS ask for the login keychain, not even to migrate a key an older Cogpit kept there (the
// owner's decision, D-10).
func TestNoKeychainCode(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("%v %v", files, err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if p := strings.Trim(imp.Path.Value, `"`); p == "C" || strings.Contains(p, "keychain") || strings.Contains(p, "keyring") {
				t.Errorf("%s imports %s", name, p)
			}
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range []string{"SecItem", "find-generic-password", "add-generic-password", "/usr/bin/security"} {
			if strings.Contains(string(src), call) {
				t.Errorf("%s calls the keychain (%s)", name, call)
			}
		}
	}
}
