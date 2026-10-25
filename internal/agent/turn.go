package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A turn (D-10, architecture §7): the user's message, then requests to the endpoint until the model
// answers without a tool call. Each request carries the system prompt, the conversation's history
// and the toolset; the answer streams into an assistant item; each tool call becomes a tool item
// and runs through Session.Dispatch with origin builtInAgent (D-2: confirmations, the audit log,
// the agents' read rules; ReadsLikeUser for a private connection). Limits per turn
// (engine.ChatLimits) and the user's Stop end it with a note.

// Causes that end a turn early.
var (
	errTimeLimit  = errors.New("the turn reached its time limit")
	errTokenLimit = errors.New("the turn reached its token limit")
)

// roleUser is the role of the user's messages in the history.
const roleUser = "user"

// keepTimeout bounds the scrubbing of what a turn keeps once the turn's own context ended (a stop).
const keepTimeout = 5 * time.Second

// withheldText stands in the conversations file for a text that could not be scrubbed (fail closed).
const withheldText = "<withheld: not kept, the cluster's Secrets could not be read to scrub it>"

// Send implements engine.ChatAgent.
func (a *Agent) Send(id string, msg engine.ChatUserMessage) error { return a.send(id, -1, msg) }

// Resend implements engine.ChatAgent: the user's message at item and everything after it are cut
// (conversation.cut), and msg starts a turn in its place (F9).
func (a *Agent) Resend(id string, item int, msg engine.ChatUserMessage) error {
	if item < 0 {
		return ErrNotUserMessage
	}
	return a.send(id, item, msg)
}

// send adds the user's message and starts a turn; with cut >= 0 the user's message at that item
// and everything after it go first. Every check comes before anything changes, so a refused
// message leaves the conversation as it was.
func (a *Agent) send(id string, cut int, msg engine.ChatUserMessage) error {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return ErrEmptyMessage
	}
	ts, err := a.toolset()
	if err != nil {
		return fmt.Errorf("the chat's tools: %w", err)
	}
	ui := a.s.UIState()
	a.mu.Lock()
	c := a.convs[id]
	switch {
	case c == nil:
		a.mu.Unlock()
		return ErrUnknownConv
	case c.turn != nil:
		a.mu.Unlock()
		return ErrTurnRunning
	case ui.Context == "" || !ui.Connection.State.Live(): // an interrupted connection reads on (D-58)
		a.mu.Unlock()
		return ErrNotConnected
	case ui.Context != c.info.Context:
		a.mu.Unlock()
		return ErrOtherContext
	case cut >= 0 && (cut >= len(c.items) || c.items[cut].Kind != engine.ChatItemUser):
		a.mu.Unlock()
		return ErrNotUserMessage
	}
	conn, ok := a.connectionLocked(c.info.ConnectionID)
	if !ok {
		a.mu.Unlock()
		return ErrNoConnection
	}
	if conn.Model == "" {
		a.mu.Unlock()
		return ErrNoModel
	}
	ep, err := endpointFor(conn.Kind, conn.BaseURL)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	if cut >= 0 {
		c.cut(cut)
	}
	limits := conn.Limits.WithDefaults()
	c.info.ConnectionID = conn.ID
	item := engine.ChatItem{Kind: engine.ChatItemUser, Text: text, Sent: &engine.ChatSent{Endpoint: ep.host, Private: conn.Private}}
	if msg.AttachSelection {
		item.Attached = attachedChip(ui)
	}
	c.turnItems, c.turnHist = len(c.items), len(c.history)
	userIdx := len(c.items)
	c.items = append(c.items, item)
	c.kept = append(c.kept, nil)
	c.history = append(c.history, message{Role: roleUser, Content: userContent(newUIFrame(ui, msg.AttachSelection), text)})
	c.keptHistory = append(c.keptHistory, nil)
	if c.info.Title == "" {
		c.info.Title = conversationTitle(text)
	}
	// A turn over a private connection makes the conversation memory-only for good (H-96, the rule
	// at the conversation type): the file is rewritten below without it.
	unkeep := conn.Private && !c.info.Private
	c.info.Private = c.info.Private || conn.Private
	c.info.Updated = a.opts.Now()
	c.info.Running = true
	base, cancel := context.WithCancelCause(context.Background())
	ctx, cancelTime := context.WithTimeoutCause(base, limits.MaxDuration, errTimeLimit)
	t := &turn{cancel: cancel, done: make(chan struct{})}
	c.turn = t
	c.bump()
	a.mu.Unlock()
	if unkeep || cut >= 0 {
		// A private turn: the turns kept so far go too, the file never holds part of a private
		// conversation. A cut: the file holds what came before the edited message only, at once.
		a.save()
	}
	a.notify(c, true)
	a.s.NotifyChat("", 0) // the history's list: its title and order
	go func() {
		defer cancelTime()
		defer cancel(nil)
		a.runTurn(ctx, c, t, turnSetup{conn: conn, limits: limits, tools: ts, userIdx: userIdx})
	}()
	return nil
}

// turnSetup is what a turn runs with.
type turnSetup struct {
	conn    engine.ChatConnection
	limits  engine.ChatLimits
	tools   *toolset
	userIdx int
}

// turnState is a running turn's counts.
type turnState struct {
	turnSetup
	ctx     context.Context
	c       *conversation
	cl      *client
	calls   int            // tool calls run
	repeats map[string]int // identical calls: tool and canonical arguments
	tokens  int            // tokens used, reported or estimated
}

// runTurn runs the turn to its end and leaves the conversation idle, kept and notified. A panic ends
// the turn with a note instead of the app (D-11).
func (a *Agent) runTurn(ctx context.Context, c *conversation, t *turn, set turnSetup) {
	st := &turnState{turnSetup: set, ctx: ctx, c: c, repeats: map[string]int{}}
	defer close(t.done)
	defer a.finishTurn(st)
	defer func() {
		if r := recover(); r != nil {
			a.log.Error("chat turn panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			a.addNote(c, engine.LevelError, "The turn ended with an internal error in Cogpit.")
		}
	}()

	// The user's message is kept scrubbed (with the ui_state it went out with).
	a.mu.Lock()
	u, h := c.items[set.userIdx], c.history[len(c.history)-1]
	hIdx := len(c.history) - 1
	title := c.info.Title
	a.mu.Unlock()
	out := a.scrub(ctx, c, u.Text, h.Content, title)
	ku, kh := cloneItem(u), h
	ku.Text, kh.Content = out[0], out[1]
	a.mu.Lock()
	c.kept[set.userIdx], c.keptHistory[hIdx] = &ku, &kh
	if c.info.Title == title {
		c.keptTitle = out[2]
	}
	a.mu.Unlock()

	key, err := a.keyFor(set.conn)
	if err == nil {
		st.cl, err = a.clientFor(set.conn, key)
	}
	if err != nil {
		a.addNote(c, engine.LevelError, err.Error())
		return
	}
	for {
		calls, ok := a.request(st)
		if !ok || len(calls) == 0 {
			return
		}
		for i, call := range calls {
			if !a.runCall(st, call) {
				a.skipCalls(st, calls[i+1:])
				return
			}
		}
	}
}

// request sends one request and streams its answer into an assistant item. It returns the tool
// calls to run, and false when the turn ends here (an error or a limit, noted).
func (a *Agent) request(st *turnState) ([]toolCall, bool) {
	c := st.c
	if st.tokens >= st.limits.MaxTokens {
		a.addNote(c, engine.LevelWarning, limitNote(errTokenLimit, st.limits))
		return nil, false
	}
	a.mu.Lock()
	msgs := append([]message{{Role: "system", Content: systemPromptFor(st.conn.AllTools)}}, c.history...)
	noOpts := a.noOptions[st.conn.ID]
	a.mu.Unlock()
	req := chatRequest{Model: st.conn.Model, Messages: msgs, Tools: st.tools.defsFor(st.conn.AllTools), Stream: true}
	if !noOpts {
		req.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	body, err := json.Marshal(req)
	if err != nil {
		a.addNote(c, engine.LevelError, "The request could not be encoded: "+err.Error())
		return nil, false
	}
	a.recordSent(st, body, msgs)

	idx := a.addItem(c, engine.ChatItem{Kind: engine.ChatItemAssistant, Streaming: true}, nil)
	sctx, cancel := context.WithCancelCause(st.ctx)
	defer cancel(nil)
	streamed := 0
	sent := time.Now()
	resp, err := st.cl.complete(sctx, body, func(text string) {
		streamed = len(text)
		if st.tokens+estimateTokens(len(body)+streamed) >= st.limits.MaxTokens {
			cancel(errTokenLimit)
		}
		a.mu.Lock()
		c.items[idx].Text = text
		c.bump()
		a.mu.Unlock()
		a.notify(c, false)
	})
	if errors.Is(err, errNoStreamOptions) && !noOpts {
		a.mu.Lock()
		a.noOptions[st.conn.ID] = true
		a.removeItemLocked(c, idx)
		a.mu.Unlock()
		return a.request(st) // once more without stream_options
	}
	if err != nil {
		if cause := context.Cause(sctx); cause != nil {
			err = cause
		}
		a.endAssistant(st, idx, "")
		st.tokens += estimateTokens(len(body) + streamed)
		a.endNote(st, err)
		return nil, false
	}
	if n := resp.Usage.total(); n > 0 {
		st.tokens += n
	} else {
		st.tokens += estimateTokens(len(body) + resp.Bytes)
	}
	if resp.Text == "" && len(resp.ToolCalls) == 0 {
		a.endAssistant(st, idx, "")
		note := "The model returned no answer."
		if resp.Thought {
			note = "The model only thought and returned no answer."
		}
		a.addNote(c, engine.LevelWarning, note)
		return nil, false
	}
	if t := timingOf(resp, sent); t != nil {
		a.mu.Lock()
		c.items[idx].Timing = t // endAssistant keeps the item with it
		a.mu.Unlock()
	}
	a.endAssistant(st, idx, resp.Text)
	// The assistant's message, thinking removed, is what the model is sent back.
	m := message{Role: "assistant", Content: resp.Text, ToolCalls: resp.ToolCalls}
	km := m
	kept := []string{resp.Text}
	for _, tc := range resp.ToolCalls { // the model chose every part of a call
		kept = append(kept, tc.Function.Arguments, tc.Function.Name, tc.ID)
	}
	out := a.scrub(st.ctx, c, kept...)
	km.Content = out[0]
	km.ToolCalls = make([]toolCall, len(m.ToolCalls))
	for i, tc := range m.ToolCalls {
		tc.Function.Arguments, tc.Function.Name, tc.ID = out[3*i+1], out[3*i+2], out[3*i+3]
		km.ToolCalls[i] = tc
	}
	a.mu.Lock()
	c.history = append(c.history, m)
	c.keptHistory = append(c.keptHistory, &km)
	a.mu.Unlock()
	return resp.ToolCalls, true
}

// endAssistant finishes the streamed item with its final text (a stopped stream keeps what was
// shown); an item without text goes.
func (a *Agent) endAssistant(st *turnState, idx int, text string) {
	c := st.c
	a.mu.Lock()
	if text == "" && c.items[idx].Text == "" {
		a.removeItemLocked(c, idx)
		c.bump()
		a.mu.Unlock()
		a.notify(c, true)
		return
	}
	if text == "" {
		text = c.items[idx].Text // stopped mid-stream: what was shown
	}
	c.items[idx].Text = text
	c.items[idx].Streaming = false
	c.bump()
	a.mu.Unlock()
	out := a.scrub(st.ctx, c, text)
	a.mu.Lock()
	k := c.items[idx]
	k.Text = out[0]
	c.kept[idx] = &k
	a.mu.Unlock()
	a.notify(c, true)
}

// removeItemLocked removes the last item when it is idx (an empty streamed answer).
func (a *Agent) removeItemLocked(c *conversation, idx int) {
	if idx == len(c.items)-1 {
		c.items = c.items[:idx]
		c.kept = c.kept[:idx]
	}
}

// runCall runs one tool call. It returns false when the turn ends here (a limit, a stop, the
// cluster switched); the call's item and its tool message say why.
func (a *Agent) runCall(st *turnState, call toolCall) bool {
	c := st.c
	r, rerr := st.tools.resolve(call.Function.Name, call.Function.Arguments)
	tc := &engine.ChatToolCall{CallID: call.ID, Command: call.Function.Name, Label: call.Function.Name,
		Args: call.Function.Arguments, State: engine.ChatToolRunning}
	switch {
	case r.cmd != nil:
		tc.Command, tc.Label, tc.Summary = r.cmd.ID, r.cmd.Label, callSummary(r.params)
	case r.search != nil:
		tc.Label, tc.Summary = "Search Cogpit's commands", strings.TrimSpace(r.search.Query)
	}

	// Limits and the kill switch come before the call runs.
	key := call.Function.Name + " " + canonicalArgs(call.Function.Arguments)
	st.repeats[key]++
	var stop error
	ui := a.s.UIState()
	switch {
	case context.Cause(st.ctx) != nil:
		stop = context.Cause(st.ctx)
	case st.repeats[key] >= st.limits.RepeatStop:
		stop = errRepeat
	case st.calls >= st.limits.MaxToolCalls:
		stop = errMaxCalls
	case ui.Context != c.info.Context || !ui.Connection.State.Live(): // reads go on while interrupted, writes are refused (D-58)
		stop = errSwitched
	}
	if stop != nil {
		tc.State = engine.ChatToolStopped
		tc.Error = limitNote(stop, st.limits)
		a.addToolItem(st, tc, call, frameError(call.Function.Name, "not run: "+tc.Error))
		a.endNote(st, stop)
		return false
	}
	st.calls++

	if rerr != nil {
		tc.State, tc.Error = engine.ChatToolFailed, rerr.Error()
		a.addToolItem(st, tc, call, frameError(call.Function.Name, rerr.Error()))
		return true
	}
	if r.search != nil {
		data, _ := json.Marshal(st.tools.search(*r.search))
		content, shown := frameResult(toolSearch, data)
		tc.State, tc.Result = engine.ChatToolDone, shown
		a.addToolItem(st, tc, call, content)
		return true
	}

	inv := engine.Invocation{ID: r.cmd.ID, Params: r.params, Origin: engine.OriginBuiltInAgent, ReadsLikeUser: st.conn.Private}
	if a.s.AsksUser(inv) {
		tc.State = engine.ChatToolWaiting
	}
	idx := a.addItem(c, engine.ChatItem{Kind: engine.ChatItemTool, Tool: tc}, nil)
	res, err := a.dispatch(st.ctx, inv)
	var content string
	if err != nil {
		state, msg := failure(r.cmd, err)
		if cause := context.Cause(st.ctx); cause != nil {
			state, msg = engine.ChatToolStopped, limitNote(cause, st.limits)
		}
		tc = a.toolDone(c, idx, func(t *engine.ChatToolCall) { t.State, t.Error = state, msg })
		content = frameError(call.Function.Name, msg)
	} else {
		data, merr := json.Marshal(res)
		if merr != nil {
			data = []byte(`"the command ran, but its result could not be encoded"`)
		}
		var shown string
		content, shown = frameResult(call.Function.Name, data)
		if note := a.s.StaleNoteFor(inv); note != "" && r.cmd.Danger == engine.DangerRead { // the cache's data as of an interruption (D-58)
			content += "\n" + note
		}
		tc = a.toolDone(c, idx, func(t *engine.ChatToolCall) { t.State, t.Result = engine.ChatToolDone, shown })
	}
	a.keepTool(st, idx, tc, call, content)
	if cause := context.Cause(st.ctx); cause != nil {
		a.endNote(st, cause)
		return false
	}
	return true
}

// dispatch runs the command, a panic outside the command's own recovery turned into an error.
func (a *Agent) dispatch(ctx context.Context, inv engine.Invocation) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			a.log.Error("chat tool call panicked", "command", inv.ID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			res, err = nil, errors.New("internal error in Cogpit")
		}
	}()
	return a.s.Dispatch(ctx, inv)
}

// toolDone changes a tool item and returns a copy of its call.
func (a *Agent) toolDone(c *conversation, idx int, fn func(*engine.ChatToolCall)) *engine.ChatToolCall {
	a.mu.Lock()
	t := *c.items[idx].Tool
	fn(&t)
	c.items[idx].Tool = &t
	c.bump()
	out := t
	a.mu.Unlock()
	a.notify(c, true)
	return &out
}

// addToolItem adds a finished tool item and its message.
func (a *Agent) addToolItem(st *turnState, tc *engine.ChatToolCall, call toolCall, content string) {
	idx := a.addItem(st.c, engine.ChatItem{Kind: engine.ChatItemTool, Tool: tc}, nil)
	a.keepTool(st, idx, tc, call, content)
}

// keepTool appends the tool message to the history and keeps the item and the message scrubbed.
func (a *Agent) keepTool(st *turnState, idx int, tc *engine.ChatToolCall, call toolCall, content string) {
	c := st.c
	m := message{Role: "tool", ToolCallID: call.ID, Content: content}
	out := a.scrub(st.ctx, c, content, tc.Args, tc.Result, tc.Error, tc.Summary, tc.Command, tc.Label, call.ID)
	kt := *tc
	kt.Args, kt.Result, kt.Error, kt.Summary, kt.Command, kt.Label, kt.CallID = out[1], out[2], out[3], out[4], out[5], out[6], out[7]
	km := m
	km.Content, km.ToolCallID = out[0], out[7]
	a.mu.Lock()
	ki := cloneItem(c.items[idx])
	ki.Tool = &kt
	c.kept[idx] = &ki
	c.history = append(c.history, m)
	c.keptHistory = append(c.keptHistory, &km)
	a.mu.Unlock()
}

// skipCalls answers the calls a stopped turn did not run, so that the history stays one the next
// request can carry (every tool call answered).
func (a *Agent) skipCalls(st *turnState, calls []toolCall) {
	for _, call := range calls {
		tc := &engine.ChatToolCall{CallID: call.ID, Command: call.Function.Name, Label: call.Function.Name,
			Args: call.Function.Arguments, State: engine.ChatToolStopped, Error: "not run: the turn ended before it"}
		if r, err := st.tools.resolve(call.Function.Name, call.Function.Arguments); err == nil && r.cmd != nil {
			tc.Command, tc.Label, tc.Summary = r.cmd.ID, r.cmd.Label, callSummary(r.params)
		}
		a.addToolItem(st, tc, call, frameError(call.Function.Name, tc.Error))
	}
}

// addItem appends an item (kept is its scrubbed copy, or nil until it is final) and returns its
// index.
func (a *Agent) addItem(c *conversation, it engine.ChatItem, kept *engine.ChatItem) int {
	a.mu.Lock()
	c.items = append(c.items, it)
	c.kept = append(c.kept, kept)
	c.bump()
	idx := len(c.items) - 1
	a.mu.Unlock()
	a.notify(c, true)
	return idx
}

// addNote adds Cogpit's note, kept as it is (Cogpit's own text, never a tool result).
func (a *Agent) addNote(c *conversation, level, text string) {
	it := engine.ChatItem{Kind: engine.ChatItemNote, Text: text, NoteLevel: level}
	k := it
	a.addItem(c, it, &k)
}

// Reasons a turn ends before the model's answer.
var (
	errRepeat   = errors.New("the model repeated a tool call")
	errMaxCalls = errors.New("the turn reached its tool-call limit")
	errSwitched = errors.New("the window switched to another cluster")
)

// limitNote says why a turn ended early, with the limit's number.
func limitNote(cause error, l engine.ChatLimits) string {
	switch cause {
	case errStopped:
		return "Stopped."
	case errTimeLimit:
		return fmt.Sprintf("The turn reached its time limit of %s and was stopped.", l.MaxDuration)
	case errTokenLimit:
		return fmt.Sprintf("The turn reached its limit of %d tokens and was stopped.", l.MaxTokens)
	case errRepeat:
		return fmt.Sprintf("The model asked for the same tool call a %s time, so the turn was stopped (a model looping on one request).", ordinal(l.RepeatStop))
	case errMaxCalls:
		return fmt.Sprintf("The turn reached its limit of %d tool calls and was stopped.", l.MaxToolCalls)
	case errSwitched:
		return "The window is no longer connected to this conversation's cluster, so the turn was stopped."
	}
	return cause.Error()
}

func ordinal(n int) string {
	switch n {
	case 1:
		return "first"
	case 2:
		return "second"
	case 3:
		return "third"
	}
	return fmt.Sprintf("%dth", n)
}

// endNote notes why the turn ended (once; a stop's note is a plain "Stopped.").
func (a *Agent) endNote(st *turnState, cause error) {
	level := engine.LevelWarning
	switch cause {
	case errStopped:
		level = engine.LevelInfo
	case errTimeLimit, errTokenLimit, errRepeat, errMaxCalls, errSwitched:
	default:
		level = engine.LevelError
	}
	a.addNote(st.c, level, limitNote(cause, st.limits))
}

// recordSent keeps the request body (SentPayload) and updates what the user's item says was sent.
func (a *Agent) recordSent(st *turnState, body []byte, msgs []message) {
	c := st.c
	tools := 0
	for _, m := range msgs {
		if m.Role == "tool" {
			tools++
		}
	}
	a.mu.Lock()
	c.payloads[st.userIdx] = string(body)
	if s := c.items[st.userIdx].Sent; s != nil {
		s.Requests++
		s.Bytes += len(body)
		s.Messages = len(msgs)
		s.ToolResults = tools
		s.Tokens = st.tokens
	}
	c.bump()
	a.mu.Unlock()
	a.notify(c, true)
}

// timingOf is an answer's TTFT and OTPS (the owner's request of 2026-10-25): the time from the
// request sent to its first part, and its output tokens over the time from its first part to its
// last; the endpoint's completion tokens, else an estimate from the parts' bytes. Nil when no part
// came.
func timingOf(resp response, sent time.Time) *engine.ChatTiming {
	if resp.First.IsZero() {
		return nil
	}
	t := &engine.ChatTiming{TTFT: resp.First.Sub(sent), Generating: resp.Last.Sub(resp.First)}
	if resp.Usage != nil && resp.Usage.CompletionTokens > 0 {
		t.OutputTokens = resp.Usage.CompletionTokens
	} else {
		t.OutputTokens, t.Estimated = estimateTokens(resp.OutBytes), true
	}
	return t
}

// estimateTokens is about four bytes a token, for endpoints that report no usage.
func estimateTokens(bytes int) int { return (bytes + 3) / 4 }

// scrub returns texts as they may be kept: through Session.ScrubSecretValues for the conversation's
// context. When the turn's context ended (a stop), a short detached one is used. On failure every
// text is withheldText (fail closed).
func (a *Agent) scrub(ctx context.Context, c *conversation, texts ...string) []string {
	if context.Cause(ctx) != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), keepTimeout)
		defer cancel()
	}
	a.mu.Lock()
	kubeContext := c.info.Context
	a.mu.Unlock()
	res, err := a.s.ScrubSecretValues(ctx, kubeContext, texts...)
	if err != nil {
		out := make([]string, len(texts))
		for i, t := range texts {
			if t != "" {
				out[i] = withheldText
			}
		}
		return out
	}
	return res
}

// finishTurn leaves the conversation idle, keeps it and notifies.
func (a *Agent) finishTurn(st *turnState) {
	c := st.c
	a.mu.Lock()
	for i := range c.items { // nothing streams or runs any more
		c.items[i].Streaming = false
		if t := c.items[i].Tool; t != nil && (t.State == engine.ChatToolRunning || t.State == engine.ChatToolWaiting) {
			nt := *t
			nt.State = engine.ChatToolStopped
			c.items[i].Tool = &nt
		}
	}
	if s := c.items[st.userIdx].Sent; s != nil {
		s.Tokens = st.tokens
	}
	c.turn = nil // save keeps the whole conversation now
	c.info.Updated = a.opts.Now()
	a.mu.Unlock()
	a.save()
	// Idle only once kept: whoever waits for the turn's end finds the file written.
	a.mu.Lock()
	if c.turn == nil {
		c.info.Running = false
	}
	c.bump()
	a.mu.Unlock()
	a.notify(c, true)
	a.s.NotifyChat("", 0)
}
