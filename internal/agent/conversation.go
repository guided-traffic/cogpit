package agent

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// conversation is one chat. Its fields are guarded by Agent.mu.
//
// items are what the window shows and history what the model is sent, both as the model saw them
// (on a private connection, Secret values included). kept and keptHistory are their scrubbed
// copies (H-82's filter, Session.ScrubSecretValues), index for index, made during the turn as each
// piece is final, so that what reaches the disk never carries a Secret value (D-10, as D-19). nil
// is a piece not yet scrubbed.
//
// A private conversation is never kept (H-96; the owner's decision of 2026-10-15): a private turn
// reads what the window shows (H-94), more than the scrubber knows. The rule:
//
//   - A conversation becomes private when a turn over a connection the user marked private starts
//     in it (Send: info.Private, and the user item's Sent.Private records the mark the turn ran
//     with). From then on it lives in memory only: save leaves it out, and the file is rewritten at
//     once, so that no earlier version of it stays there.
//   - It stays private for good. Switching it to a connection not marked private, or taking the
//     mark off its connection, does not make it kept again: its history still holds what the
//     private turn read, and the next turn sends that history to the model.
//   - A conversation whose connection is private but which has no turn yet is not private: it holds
//     nothing to keep (an empty conversation is never kept). Turns over a connection not marked
//     private before the first private one are kept until that private turn starts.
//   - A file written before the rule (v0.8.0 to v0.10.0) can hold private conversations: the agent
//     drops each conversation with a user item whose Sent.Private is set when it loads the file and
//     rewrites the file at once (loadConversations).
type conversation struct {
	info        engine.ChatConversationInfo
	rev         uint64
	items       []engine.ChatItem
	kept        []*engine.ChatItem
	history     []message
	keptHistory []*message
	payloads    map[int]string // the user item's index → the last request body of its turn (memory only)
	keptTitle   string         // the title scrubbed ("" until its first turn scrubbed it)
	// cleared: the history's messages before this index are sent with their larger tool results
	// replaced by a note (context.go, D-167); it only grows, but for a cut. calBytes and calTokens
	// are the last request's body size and the prompt tokens the endpoint counted for it (memory
	// only): the measure of what a request takes of the context window.
	cleared   int
	calBytes  int
	calTokens int

	turn       *turn
	turnItems  int // len(items) when the running turn began: what is kept while it runs
	turnHist   int
	lastNotify time.Time
	timer      bool // a throttled notification is pending
}

// turn is a running turn.
type turn struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
}

// cut drops the user's message at item, which is a user item, and everything after it (F9, the
// owner's decision of 2026-10-15): from the items and their kept copies, from the history the model
// is sent (the user message the item stands for, which is the history's user message of the same
// rank, and what followed it) and from the request bodies; cutting the first message takes the
// title too, so that the new one names the conversation. Nothing tells the model about the cut
// part: its tool calls, writes included, leave the model's view (H-100); the audit log keeps
// them. The full slice expressions make the next append allocate, never reuse the cut part's
// memory. Agent.mu is held.
func (c *conversation) cut(item int) {
	users := 0
	for _, it := range c.items[:item] {
		if it.Kind == engine.ChatItemUser {
			users++
		}
	}
	h := len(c.history)
	for i, m := range c.history {
		if m.Role != roleUser {
			continue
		}
		if users == 0 {
			h = i
			break
		}
		users--
	}
	c.items, c.kept = c.items[:item:item], c.kept[:item:item]
	c.history, c.keptHistory = c.history[:h:h], c.keptHistory[:h:h]
	c.cleared = min(c.cleared, h)
	for i := range c.payloads {
		if i >= item {
			delete(c.payloads, i)
		}
	}
	if item == 0 {
		c.info.Title, c.keptTitle = "", ""
	}
}

// bump records a change and returns the new revision.
func (c *conversation) bump() uint64 {
	c.rev++
	return c.rev
}

// hadPrivateTurn reports whether a turn over a private connection ran in a conversation with these
// items: a user item whose Sent records the private mark (H-96).
func hadPrivateTurn(items []engine.ChatItem) bool {
	return slices.ContainsFunc(items, func(it engine.ChatItem) bool {
		return it.Kind == engine.ChatItemUser && it.Sent != nil && it.Sent.Private
	})
}

// cloneItem copies an item with its pointers.
func cloneItem(it engine.ChatItem) engine.ChatItem {
	if it.Tool != nil {
		t := *it.Tool
		it.Tool = &t
	}
	if it.Sent != nil {
		s := *it.Sent
		it.Sent = &s
	}
	return it
}

// Conversations implements engine.ChatAgent: newest first.
func (a *Agent) Conversations(kubeContext string) []engine.ChatConversationInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []engine.ChatConversationInfo
	for _, c := range a.convs {
		if c.info.Context == kubeContext {
			out = append(out, c.info)
		}
	}
	sortInfos(out)
	return out
}

func sortInfos(infos []engine.ChatConversationInfo) {
	sort.SliceStable(infos, func(i, j int) bool {
		if !infos[i].Updated.Equal(infos[j].Updated) {
			return infos[i].Updated.After(infos[j].Updated)
		}
		return infos[i].ID < infos[j].ID
	})
}

// Conversation implements engine.ChatAgent.
func (a *Agent) Conversation(id string) (engine.ChatConversation, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.convs[id]
	if c == nil {
		return engine.ChatConversation{}, false
	}
	out := engine.ChatConversation{ChatConversationInfo: c.info, Revision: c.rev, Items: make([]engine.ChatItem, len(c.items))}
	for i, it := range c.items {
		out.Items[i] = cloneItem(it)
	}
	return out, true
}

// NewConversation implements engine.ChatAgent.
func (a *Agent) NewConversation(kubeContext, connectionID string) (engine.ChatConversation, error) {
	if strings.TrimSpace(kubeContext) == "" {
		return engine.ChatConversation{}, ErrNotConnected
	}
	a.mu.Lock()
	if connectionID != "" && !slices.ContainsFunc(a.conns, func(x engine.ChatConnection) bool { return x.ID == connectionID }) {
		a.mu.Unlock()
		return engine.ChatConversation{}, ErrUnknownConn
	}
	if connectionID == "" {
		if conn, ok := a.connectionLocked(""); ok {
			connectionID = conn.ID
		}
	}
	c := &conversation{info: engine.ChatConversationInfo{ID: newID(), Context: kubeContext, ConnectionID: connectionID,
		Updated: a.opts.Now()}, payloads: map[int]string{}}
	c.bump()
	a.convs[c.info.ID] = c
	a.pruneLocked(kubeContext)
	out := engine.ChatConversation{ChatConversationInfo: c.info, Revision: c.rev}
	a.mu.Unlock()
	a.save()
	a.s.NotifyChat("", 0)
	return out, nil
}

// pruneLocked drops a context's oldest conversations beyond MaxConversations; running ones stay.
func (a *Agent) pruneLocked(kubeContext string) {
	var infos []engine.ChatConversationInfo
	for _, c := range a.convs {
		if c.info.Context == kubeContext {
			infos = append(infos, c.info)
		}
	}
	if len(infos) <= MaxConversations {
		return
	}
	sortInfos(infos)
	for _, info := range infos[MaxConversations:] {
		if c := a.convs[info.ID]; c != nil && c.turn == nil {
			delete(a.convs, info.ID)
		}
	}
}

// SetConversationConnection implements engine.ChatAgent. A private conversation stays private on
// any connection (H-96: its history holds what its private turn read).
func (a *Agent) SetConversationConnection(id, connectionID string) error {
	a.mu.Lock()
	c := a.convs[id]
	switch {
	case c == nil:
		a.mu.Unlock()
		return ErrUnknownConv
	case c.turn != nil:
		a.mu.Unlock()
		return ErrTurnRunning
	case !slices.ContainsFunc(a.conns, func(x engine.ChatConnection) bool { return x.ID == connectionID }):
		a.mu.Unlock()
		return ErrUnknownConn
	}
	if c.info.ConnectionID != connectionID {
		c.calBytes, c.calTokens = 0, 0 // another model counts tokens its own way
	}
	c.info.ConnectionID = connectionID
	rev := c.bump()
	a.mu.Unlock()
	a.save()
	a.s.NotifyChat(id, rev)
	return nil
}

// DeleteConversation implements engine.ChatAgent: a running turn is stopped first.
func (a *Agent) DeleteConversation(id string) error {
	a.Stop(id)
	a.mu.Lock()
	if a.convs[id] == nil {
		a.mu.Unlock()
		return ErrUnknownConv
	}
	delete(a.convs, id)
	a.mu.Unlock()
	a.save()
	a.s.NotifyChat("", 0)
	return nil
}

// ClearConversations implements engine.ChatAgent: every conversation of the context, running
// turns stopped first.
func (a *Agent) ClearConversations(kubeContext string) error {
	for _, info := range a.Conversations(kubeContext) {
		a.Stop(info.ID)
	}
	a.mu.Lock()
	for id, c := range a.convs {
		if c.info.Context == kubeContext {
			if c.turn != nil { // a turn started meanwhile
				a.mu.Unlock()
				return ErrTurnRunning
			}
			delete(a.convs, id)
		}
	}
	a.mu.Unlock()
	a.save()
	a.s.NotifyChat("", 0)
	return nil
}

// Stop implements engine.ChatAgent: it cancels the turn's context (the stream and a running tool
// call, a pending confirmation included) and returns once the turn's goroutine has ended.
func (a *Agent) Stop(id string) {
	a.mu.Lock()
	var t *turn
	if c := a.convs[id]; c != nil {
		t = c.turn
	}
	a.mu.Unlock()
	if t == nil {
		return
	}
	t.cancel(errStopped)
	<-t.done
}

// SentPayload implements engine.ChatAgent: the last request body of the turn that item belongs to,
// as sent. Kept in memory only; a restored conversation's earlier turns have none.
func (a *Agent) SentPayload(id string, item int) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.convs[id]
	if c == nil || item < 0 || item >= len(c.items) {
		return "", false
	}
	for i := item; i >= 0; i-- {
		if c.items[i].Kind == engine.ChatItemUser {
			p, ok := c.payloads[i]
			return p, ok
		}
	}
	return "", false
}

// errStopped is the cause of a turn the user stopped.
var errStopped = errors.New("stopped by the user")

// notify emits EventChat for c: at once when now, else at most every NotifyEvery (a timer sends
// the latest revision when it is due). Called without a.mu.
func (a *Agent) notify(c *conversation, now bool) {
	a.mu.Lock()
	id, rev := c.info.ID, c.rev
	t := a.opts.Now()
	wait := a.opts.NotifyEvery - t.Sub(c.lastNotify)
	switch {
	case now || wait <= 0:
		c.lastNotify = t
	case c.timer:
		a.mu.Unlock()
		return
	default:
		c.timer = true
		a.mu.Unlock()
		time.AfterFunc(wait, func() {
			a.mu.Lock()
			if !c.timer {
				a.mu.Unlock()
				return
			}
			c.timer = false
			c.lastNotify = a.opts.Now()
			id, rev := c.info.ID, c.rev
			a.mu.Unlock()
			a.s.NotifyChat(id, rev)
		})
		return
	}
	c.timer = false
	a.mu.Unlock()
	a.s.NotifyChat(id, rev)
}
