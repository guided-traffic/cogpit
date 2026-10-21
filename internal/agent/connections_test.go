package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestConnectionsKeepTheKeyInTheKeyStore: a connection's key goes to the key store, never to the
// connections file (0600); nil keeps it, "" removes it, Delete removes it; one connection is the
// default; a new agent reads the file back.
func TestConnectionsKeepTheKeyInTheKeyStore(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	const key = "sk-cogpit-test-9f8e7d6c5b4a"
	second, err := h.a.SaveConnection(engine.ChatConnection{Name: "LiteLLM", Kind: engine.ChatLiteLLM, BaseURL: "https://llm.example/",
		Model: "gpt-x", Limits: engine.ChatLimits{MaxToolCalls: 10, MaxDuration: 3 * time.Minute}}, ptr(key))
	if err != nil {
		t.Fatal(err)
	}
	if !second.HasKey || second.Default || second.BaseURL != "https://llm.example" || second.ID == "" {
		t.Errorf("saved %+v", second)
	}
	if got, err := h.keys.Get(second.ID); err != nil || got != key {
		t.Fatalf("key store: %q %v", got, err)
	}
	path := filepath.Join(h.dir, ConnectionsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), key) || strings.Contains(string(data), "sk-") {
		t.Fatal("the connections file holds the key")
	}
	// Windows sets no file modes: there the user profile's permissions protect the file.
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}

	// nil keeps the key; changing the name keeps the id.
	second.Name = "LiteLLM (team)"
	if s, err := h.a.SaveConnection(second, nil); err != nil || !s.HasKey || s.ID != second.ID {
		t.Fatalf("%+v %v", s, err)
	}
	if err := h.a.SetDefaultConnection(second.ID); err != nil {
		t.Fatal(err)
	}
	conns, _ := h.a.Connections()
	if len(conns) != 2 || conns[0].ID != second.ID || !conns[0].Default || conns[1].Default {
		t.Errorf("connections %+v", conns)
	}

	// A new agent reads them back.
	b := New(Options{Session: h.s, Dir: h.dir, Keys: h.keys})
	back, _ := b.Connections()
	if len(back) != 2 || back[0].Name != "LiteLLM (team)" || !back[0].HasKey || back[0].Limits.MaxDuration != 3*time.Minute {
		t.Errorf("read back %+v", back)
	}

	// "" removes the key, Delete removes the connection and any key.
	if s, err := h.a.SaveConnection(second, ptr("")); err != nil || s.HasKey {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := h.keys.Get(second.ID); !errors.Is(err, ErrNoKey) {
		t.Errorf("key still stored: %v", err)
	}
	if _, err := h.a.SaveConnection(second, ptr(key)); err != nil {
		t.Fatal(err)
	}
	if err := h.a.DeleteConnection(second.ID); err != nil {
		t.Fatal(err)
	}
	if h.keys.Len() != 0 {
		t.Error("Delete left the key")
	}
	conns, _ = h.a.Connections()
	if len(conns) != 1 || !conns[0].Default {
		t.Errorf("after delete %+v", conns)
	}

	// Refused: missing fields, a base URL with credentials, negative limits.
	for _, bad := range []engine.ChatConnection{
		{Name: "x", Kind: engine.ChatLMStudio},
		{Kind: engine.ChatLMStudio, BaseURL: "http://h/v1"},
		{Name: "x", Kind: "ollama", BaseURL: "http://h/v1", Model: "m"},
		{Name: "x", Kind: engine.ChatOpenAI, BaseURL: "https://user:secret@h/v1", Model: "m"},
		{Name: "x", Kind: engine.ChatOpenAI, BaseURL: "https://h/v1", Model: "m", Limits: engine.ChatLimits{MaxTokens: -1}},
	} {
		if _, err := h.a.SaveConnection(bad, nil); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	// A connection without a model is kept (the settings form lists the models for it), but a turn
	// does not start on it.
	noModel, err := h.a.SaveConnection(engine.ChatConnection{Name: "later", Kind: engine.ChatLMStudio, BaseURL: h.llm.Base}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := h.a.NewConversation("test", noModel.ID)
	if err := h.a.Send(c.ID, engine.ChatUserMessage{Text: "hi"}); !errors.Is(err, ErrNoModel) {
		t.Errorf("Send without a model: %v", err)
	}
	if err := h.a.DeleteConnection(noModel.ID); err != nil {
		t.Fatal(err)
	}
	var f connectionsFile
	data, _ = os.ReadFile(path)
	if err := json.Unmarshal(data, &f); err != nil || len(f.Connections) != 1 {
		t.Errorf("file %s %v", data, err)
	}
}
