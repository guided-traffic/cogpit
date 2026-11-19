package agent

import (
	"encoding/json"
	"path/filepath"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The agent's two files in Cogpit's configuration directory, owner-only through the engine's
// writer (0600 in 0700, atomic replace, symlinks refused): the connections without any key, and
// the conversations, scrubbed (D-10), but the private ones, which stay in memory (H-96). The
// request bodies (SentPayload) stay in memory.

const (
	fileVersion          = 1
	maxConnectionsFile   = 1 << 20
	maxConversationsFile = 256 << 20
)

type connectionsFile struct {
	Version     int                     `json:"version"`
	Connections []engine.ChatConnection `json:"connections"`
}

type conversationsFile struct {
	Version       int                  `json:"version"`
	Conversations []storedConversation `json:"conversations"`
}

// storedConversation is a conversation as kept: its items and the history the model is sent when it
// goes on, both scrubbed, and how far the history's tool results are cleared from what is sent
// (conversation.cleared).
type storedConversation struct {
	engine.ChatConversationInfo
	Items   []engine.ChatItem `json:"items"`
	History []message         `json:"history"`
	Cleared int               `json:"cleared,omitempty"`
}

func (a *Agent) path(name string) string {
	if a.opts.Dir == "" {
		return ""
	}
	return filepath.Join(a.opts.Dir, name)
}

// loadConnections reads the connections file; one connection is the default.
func (a *Agent) loadConnections() {
	p := a.path(ConnectionsFile)
	if p == "" {
		return
	}
	var f connectionsFile
	if err := engine.ReadPrivateJSON(p, maxConnectionsFile, &f); err != nil {
		a.log.Warn("the chat's connections could not be read", "file", p, "err", err)
		return
	}
	hasDefault := false
	for _, c := range f.Connections {
		c, err := normalize(c)
		if err != nil || c.ID == "" {
			continue
		}
		if c.Default && hasDefault {
			c.Default = false
		}
		hasDefault = hasDefault || c.Default
		a.conns = append(a.conns, c)
	}
	if !hasDefault && len(a.conns) > 0 {
		a.conns[0].Default = true
	}
}

// saveConnections writes the connections file.
func (a *Agent) saveConnections() {
	p := a.path(ConnectionsFile)
	if p == "" {
		return
	}
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	f := connectionsFile{Version: fileVersion, Connections: a.connectionsLocked()}
	a.mu.Unlock()
	data, err := json.MarshalIndent(f, "", "  ")
	if err == nil {
		err = engine.WritePrivateFile(p, data)
	}
	if err != nil {
		a.log.Warn("the chat's connections could not be kept", "file", p, "err", err)
	}
}

// loadConversations reads the conversations file. What it holds was scrubbed when it was kept. A
// private conversation in it, which only a file of v0.8.0 to v0.10.0 holds, is dropped, and the
// file is rewritten at once without it (H-96, the rule at the conversation type).
func (a *Agent) loadConversations() {
	p := a.path(ConversationsFile)
	if p == "" {
		return
	}
	var f conversationsFile
	if err := engine.ReadPrivateJSON(p, maxConversationsFile, &f); err != nil {
		a.log.Warn("the chat's conversations could not be read", "file", p, "err", err)
		return
	}
	contexts := map[string]bool{}
	dropped := 0
	for _, sc := range f.Conversations {
		if storedPrivate(sc) {
			dropped++
			continue
		}
		if sc.ID == "" || sc.Context == "" || a.convs[sc.ID] != nil {
			continue
		}
		c := &conversation{info: sc.ChatConversationInfo, payloads: map[int]string{}, keptTitle: sc.Title}
		c.info.Running = false
		for _, it := range sc.Items {
			it.Streaming = false
			k := cloneItem(it)
			c.items = append(c.items, it)
			c.kept = append(c.kept, &k)
		}
		for _, m := range sc.History {
			k := m
			c.history = append(c.history, m)
			c.keptHistory = append(c.keptHistory, &k)
		}
		c.cleared = max(0, min(sc.Cleared, len(c.history)))
		c.bump()
		a.convs[c.info.ID] = c
		contexts[c.info.Context] = true
	}
	for ctx := range contexts {
		a.pruneLocked(ctx)
	}
	if dropped > 0 {
		a.log.Info("the chat's private conversations were removed from its file", "file", p, "conversations", dropped)
		a.save()
	}
}

// storedPrivate reports whether a kept conversation is a private one: a turn of it ran over a
// connection marked private. Every user item a file holds carries the mark its turn ran with
// (ChatSent.Private, written since v0.8.0, the first with the chat).
func storedPrivate(sc storedConversation) bool {
	return sc.Private || hadPrivateTurn(sc.Items)
}

// save writes the conversations file: the kept (scrubbed) copies of every conversation with
// something in it but the private ones (H-96); of a running one only what came before its turn.
func (a *Agent) save() {
	p := a.path(ConversationsFile)
	if p == "" {
		return
	}
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	f := conversationsFile{Version: fileVersion}
	for _, c := range a.convs {
		items, hist := len(c.items), len(c.history)
		if c.turn != nil {
			items, hist = c.turnItems, c.turnHist
		}
		if items == 0 || c.info.Private { // a private conversation lives in memory only
			continue
		}
		sc := storedConversation{ChatConversationInfo: c.info, Cleared: min(c.cleared, hist)}
		sc.Running = false
		sc.Title = c.keptTitle // the first message, scrubbed
		if sc.Title == "" && c.info.Title != "" {
			sc.Title = withheldText
		}
		for i := range items {
			sc.Items = append(sc.Items, keptItem(c, i))
		}
		for i := range hist {
			sc.History = append(sc.History, keptMessage(c, i))
		}
		f.Conversations = append(f.Conversations, sc)
	}
	a.mu.Unlock()
	data, err := json.Marshal(f)
	if err == nil {
		err = engine.WritePrivateFile(p, data)
	}
	if err != nil {
		a.log.Warn("the chat's conversations could not be kept", "file", p, "err", err)
	}
}

// keptItem is item i as kept: its scrubbed copy (with the user item's current counts of what was
// sent), or, for one never scrubbed, withheld. a.mu is held.
func keptItem(c *conversation, i int) engine.ChatItem {
	it := c.items[i]
	k := c.kept[i]
	if k == nil {
		w := engine.ChatItem{Kind: it.Kind, NoteLevel: it.NoteLevel, Attached: it.Attached}
		if it.Text != "" {
			w.Text = withheldText
		}
		if it.Tool != nil {
			t := *it.Tool
			t.Args, t.Result, t.Error = "", "", withheldText
			w.Tool = &t
		}
		k = &w
	}
	out := cloneItem(*k)
	out.Streaming = false
	if it.Sent != nil {
		s := *it.Sent
		out.Sent = &s
	}
	return out
}

// keptMessage is history message i as kept, or, never scrubbed, withheld (its tool calls keep their
// ids and names, so the history stays one a request can carry). a.mu is held.
func keptMessage(c *conversation, i int) message {
	if k := c.keptHistory[i]; k != nil {
		return *k
	}
	m := c.history[i]
	if m.Content != "" {
		m.Content = withheldText
	}
	calls := make([]toolCall, len(m.ToolCalls))
	for j, tc := range m.ToolCalls {
		tc.Function.Arguments = "{}"
		calls[j] = tc
	}
	if len(calls) > 0 {
		m.ToolCalls = calls
	}
	return m
}
