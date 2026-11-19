package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The choice (D-172): over a connection the user set to choose from large results
// (engine.ChatConnection.ChooseLarge), a read command's result that is above the connection's limit
// and whose largest part is a list of entries goes to the model in a request of its own, which
// answers with the numbers of the entries that matter for the user's question. Cogpit then sends
// the conversation those entries, taken from the result itself, in the result's order, with its own
// note: how many of how many, the totals over all of them, and result_read for the rest. No text
// the model wrote in that request enters the conversation; a choice that does not come (an error,
// the time, an answer that is not a list of numbers, Stop) leaves the result's head (D-170).
//
// The request continues the conversation's own: the same system prompt, history and tools as the
// next request of the turn would carry, then this call's result as numbered entries, the other
// calls of the same answer as not run yet, Cogpit's fixed request to choose, and the beginning of
// the answer: an empty thinking block (choicePrefill). An endpoint that keeps a prompt cache so
// reads only what follows the conversation, and keeps the conversation's cache for its next request
// (D-172's measurements). An endpoint that continues a last assistant message (LM Studio, llama.cpp)
// so starts the answer after the thinking a thinking model would do first; a request field that
// switches thinking off (reasoning_effort) changes the template's rendering from the start on LM
// Studio, which made it read the whole conversation anew. An endpoint that takes the message as an
// earlier answer answers anew, and a model that thinks anyway is bounded by maxChoiceOut.
//
// It is bounded by the turn: half of the time the turn has left, its token limit, and the room the
// model's context leaves (only the entries that fit are offered, from the first). Stop and the
// turn's limit end it as they end any request; an endpoint stops reading a request whose client
// went away (LM Studio after the part of the prompt it is reading, measured).

const (
	// minChoiceEntries is the least entries a list needs for a choice; a shorter one is no list
	// worth choosing from.
	minChoiceEntries = 8
	// maxChosen is the most entries a choice keeps, in the model's order.
	maxChosen = 40
	// choiceOffered is how many times the connection's limit the entries offered may take.
	choiceOffered = 4
	// maxChoiceText bounds the answer's text, maxChoiceOut the answer with its thinking: a list of
	// 40 numbers is some 250 bytes.
	maxChoiceText = 1 << 10
	maxChoiceOut  = 4 << 10
	// minChoiceTime is the least time the turn must have left; the choice takes at most half of it.
	minChoiceTime = 2 * time.Minute
)

// foldedByCode are the commands whose large results code folds (D-171): never a choice.
var foldedByCode = map[string]bool{"cluster.overview": true}

// choiceRequest is Cogpit's message after the numbered entries: fixed, with no text of the model's
// or the cluster's in it.
const choiceRequest = "[Cogpit's request, not the user's] Cogpit numbered the entries of the last tool result (#0, #1, …). " +
	"Reply with nothing but the numbers of the entries that matter most for the user's last message, at most 40, " +
	"separated by commas, the most important first. Do not call a tool and do not explain."

// notRunYet answers, in the choice's request, the calls of the same answer after this one.
const notRunYet = "not run yet"

// choicePrefill begins the choice's answer: a thinking block that is over.
const choicePrefill = thinkOpen + "\n\n" + thinkClose + "\n\n"

// The causes that end a choice early.
var (
	errChoiceTime = errors.New("the model did not answer within the time the turn could give it")
	errChoiceLong = errors.New("the model wrote more than a list of numbers")
	errNoRoom     = errors.New("the conversation leaves too little room in the model's context window")
)

// choiceNoteStart opens Cogpit's note after a choice's entries.
const choiceNoteStart = "[Cogpit: a model chose "

// choosable returns the largest part of a result that goes to a choice, nil for one that does not:
// the connection chooses, the command reads and is not one whose results code folds, the result is
// above the limit, and its largest part is an array of at least minChoiceEntries elements. A
// string (YAML, a values file) never is.
func choosable(conn engine.ChatConnection, cmd *engine.Command, data []byte, limit int) *paged {
	if !conn.ChooseLarge || cmd == nil || cmd.Danger != engine.DangerRead || foldedByCode[cmd.ID] || len(data) <= limit {
		return nil
	}
	p := largestPart(data)
	if p == nil || !p.array || p.count() < minChoiceEntries {
		return nil
	}
	return p
}

// choose has the model choose from the result data of the call at item idx, whose largest part is p.
// It returns the content and the shown result with the chosen entries and the raw result to hold;
// the choice says what it did, and when it failed (Failed) content is empty and the caller frames
// the result as a head.
func (a *Agent) choose(st *turnState, idx int, call toolCall, cmd *engine.Command, data []byte, p *paged) (content, shown string, raw *rawResult, ch *engine.ChatChoice) {
	c := st.c
	limit := st.limits.MaxToolResultKiB << 10
	ch = &engine.ChatChoice{Model: st.conn.Model, Entries: p.count()}
	shownCh := *ch // the item's own copy: ch changes while the request runs
	a.toolDone(c, idx, func(t *engine.ChatToolCall) { t.State, t.Choice = engine.ChatToolChoosing, &shownCh })
	defer func() {
		if r := recover(); r != nil {
			a.log.Error("chat choice panicked", "command", cmd.ID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			content, shown, raw, ch.Failed = "", "", nil, "internal error in Cogpit"
		}
	}()
	tool := cmd.ToolName()
	chosen, err := a.askChoice(st, call, tool, data, p, ch)
	if err != nil {
		ch.Failed = choiceFailure(err, st)
		return "", "", nil, ch
	}
	ref, held := "", holdable(call.ID, data) && !unheld[cmd.ID]
	if held {
		a.mu.Lock()
		ref = c.rawRef(call.ID)
		a.mu.Unlock()
	}
	shown, reads, ok := renderChoice(data, p, chosen, ch, limit, ref, held)
	if !ok {
		ch.Failed = "the chosen entries do not fit the connection's limit for a tool result"
		return "", "", nil, ch
	}
	if reads {
		raw = &rawResult{ref: ref, data: data, tool: tool, read: a.opts.Now(), private: st.conn.Private}
	}
	return resultFrame(call.Function.Name) + shown, shown, raw, ch
}

// askChoice sends the choice's request and returns the chosen entries' indexes in the model's
// order. ch gets the offered entries, the time and the count of the chosen ones.
func (a *Agent) askChoice(st *turnState, call toolCall, tool string, data []byte, p *paged, ch *engine.ChatChoice) ([]int, error) {
	c := st.c
	limits := st.limits
	deadline, ok := st.ctx.Deadline()
	left := time.Until(deadline)
	if cause := context.Cause(st.ctx); cause != nil {
		return nil, cause
	}
	if ok && left < minChoiceTime {
		return nil, fmt.Errorf("the turn has less than %d minutes left", int(minChoiceTime.Minutes()))
	}
	budget := choiceOffered * (limits.MaxToolResultKiB << 10)
	var body []byte
	var entries string
	for {
		var offered int
		offered, entries = numberedEntries(data, p, budget)
		entries = resultFrame(tool) + entries
		ch.Offered, ch.OfferedBytes = offered, len(entries)
		if offered < minChoiceEntries {
			return nil, errNoRoom
		}
		var err error
		if body, err = a.choiceBody(st, call, entries); err != nil {
			return nil, err
		}
		a.mu.Lock()
		tokens, high := c.contextTokens(len(body)), limits.ContextWindow*limits.ClearAt/100
		a.mu.Unlock()
		if tokens <= high {
			break
		}
		// Too large for the window: fewer entries, by the share that is over.
		over := float64(tokens-high) / float64(tokens)
		next := budget - int(float64(len(body))*over) - 1
		if next <= 0 {
			return nil, errNoRoom
		}
		budget = next
	}
	if st.tokens+estimateTokens(len(body)) >= limits.MaxTokens {
		return nil, errors.New("the turn's token limit leaves no room for it")
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if ok {
		ctx, cancel = context.WithTimeoutCause(st.ctx, left/2, errChoiceTime)
	} else {
		ctx, cancel = context.WithCancel(st.ctx)
	}
	defer cancel()
	start := time.Now()
	resp, err := a.sendChoice(ctx, st, body, func() ([]byte, error) { return a.choiceBody(st, call, entries) })
	ch.Took = time.Since(start).Round(time.Second)
	if err != nil {
		return nil, err
	}
	if len(resp.ToolCalls) > 0 && resp.Text == "" {
		return nil, errors.New("the model called a tool instead of choosing")
	}
	if resp.Text == "" {
		if resp.Thought {
			return nil, errors.New("the model only thought and returned no numbers")
		}
		return nil, errors.New("the model returned no answer")
	}
	chosen, err := chosenEntries(resp.Text, ch.Offered)
	if err != nil {
		return nil, err
	}
	ch.Chosen = len(chosen)
	return chosen, nil
}

// sendChoice sends a choice's body and counts it against the turn: what it sent and its tokens.
// An endpoint that refused stream_options is sent the body again, built anew by build without it.
func (a *Agent) sendChoice(ctx context.Context, st *turnState, body []byte, build func() ([]byte, error)) (response, error) {
	a.recordChoice(st, body)
	sctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	resp, err := st.cl.completeWatch(sctx, body, func(text string) {
		if len(text) > maxChoiceText {
			cancel(errChoiceLong)
		}
	}, func(n int) {
		if n > maxChoiceOut {
			cancel(errChoiceLong)
		}
	})
	if cause := context.Cause(sctx); cause != nil {
		err = cause // a bound or a stop the stream's end came before
	}
	if err != nil {
		st.tokens += estimateTokens(len(body))
		if a.refusedField(st.conn.ID, err) {
			again, berr := build()
			if berr != nil {
				return response{}, berr
			}
			return a.sendChoice(ctx, st, again, build)
		}
		return response{}, err
	}
	if n := resp.Usage.total(); n > 0 {
		st.tokens += n
	} else {
		st.tokens += estimateTokens(len(body) + resp.Bytes)
	}
	return resp, nil
}

// refusedField notes an endpoint that refused stream_options, and reports whether the request goes
// again without it.
func (a *Agent) refusedField(conn string, err error) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if errors.Is(err, errNoStreamOptions) && !a.noOptions[conn] {
		a.noOptions[conn] = true
		return true
	}
	return false
}

// choiceBody is the choice's request: the conversation's next request as the turn would send it,
// with entries as the call's tool message, the calls after it in the same answer as not run yet,
// Cogpit's request to choose and the answer's beginning.
func (a *Agent) choiceBody(st *turnState, call toolCall, entries string) ([]byte, error) {
	a.mu.Lock()
	noOpts := a.noOptions[st.conn.ID]
	msgs := append([]message{{Role: roleSystem, Content: systemPromptFor(st.conn.AllTools)}}, sentHistory(st.c.history, st.c.cleared)...)
	a.mu.Unlock()
	msgs = slices.Clip(msgs)
	msgs = append(msgs, message{Role: roleTool, ToolCallID: call.ID, Content: entries})
	for _, later := range laterCalls(msgs, call.ID) {
		msgs = append(msgs, message{Role: roleTool, ToolCallID: later.ID, Content: frameError(later.Function.Name, notRunYet)})
	}
	msgs = append(msgs, message{Role: roleUser, Content: choiceRequest}, message{Role: roleAssistant, Content: choicePrefill})
	req := chatRequest{Model: st.conn.Model, Messages: msgs, Tools: st.tools.defsFor(st.conn.AllTools), Stream: true}
	if !noOpts {
		req.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	return json.Marshal(req)
}

// laterCalls are the tool calls of the last assistant message in msgs after the call id.
func laterCalls(msgs []message, id string) []toolCall {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != roleAssistant {
			continue
		}
		calls := msgs[i].ToolCalls
		for j, tc := range calls {
			if tc.ID == id {
				return calls[j+1:]
			}
		}
		return nil
	}
	return nil
}

// numberedEntries is the result as the choice's request carries it: the document with its largest
// part empty, then Cogpit's line and the part's elements, one a line, each after its number, from
// the first for as long as they fit budget bytes. It returns how many it holds.
func numberedEntries(data []byte, p *paged, budget int) (int, string) {
	var b strings.Builder
	n := p.count()
	lines := make([]string, 0, n)
	used := 0
	for i := range n {
		line := "#" + strconv.Itoa(i) + " " + string(p.elems[i])
		if used+len(line)+1 > budget {
			break
		}
		lines = append(lines, line)
		used += len(line) + 1
	}
	b.Write(data[:p.start])
	b.WriteString("[]")
	b.Write(data[p.end:])
	b.WriteByte('\n')
	if len(lines) == n {
		fmt.Fprintf(&b, "[Cogpit: the %d %s%s follow, numbered from #0.]\n", n, p.unit(), p.of())
	} else {
		fmt.Fprintf(&b, "[Cogpit: the first %d of the %d %s%s follow, numbered from #0; the rest is not offered.]\n",
			len(lines), n, p.unit(), p.of())
	}
	b.WriteString(strings.Join(lines, "\n"))
	return len(lines), b.String()
}

// chosenEntries reads the model's answer: numbers ("12", "#12"), ranges ("3-5") and nothing else
// but commas, semicolons, blanks and a closing full stop. It returns the entries' indexes below
// offered, each once, in the answer's order, at most maxChosen; an error for an answer that is no
// such list or that names no entry offered.
func chosenEntries(text string, offered int) ([]int, error) {
	fields := strings.FieldsFunc(strings.TrimSuffix(strings.TrimSpace(text), "."), func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	})
	errList := errors.New("the model's answer is not a list of entry numbers")
	if len(fields) == 0 {
		return nil, errList
	}
	seen := map[int]bool{}
	var out []int
	add := func(i int) {
		if i >= 0 && i < offered && !seen[i] && len(out) < maxChosen {
			seen[i] = true
			out = append(out, i)
		}
	}
	for _, f := range fields {
		f = strings.TrimPrefix(f, "#")
		lo, hi, isRange := strings.Cut(f, "-")
		a, err := strconv.Atoi(lo)
		if err != nil || a < 0 {
			return nil, errList
		}
		if !isRange {
			add(a)
			continue
		}
		b, err := strconv.Atoi(strings.TrimPrefix(hi, "#"))
		if err != nil || b < a {
			return nil, errList
		}
		for i := a; i <= min(b, a+maxChosen); i++ {
			add(i)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the model named no entry that was offered")
	}
	return out, nil
}

// renderChoice builds what the conversation reads of a choice: the result's document with its
// largest part holding the chosen entries in the result's order, as many of them as fit limit in
// the model's order, a line break and Cogpit's note. With held the note names result_read for
// ref. It returns the text and whether the note names result_read; ok is false when not one
// chosen entry fits.
func renderChoice(data []byte, p *paged, chosen []int, ch *engine.ChatChoice, limit int, ref string, held bool) (string, bool, bool) {
	n := p.count()
	totals := p.totals(totalsRoom(limit))
	rest := len(data) - (p.end - p.start)
	keep := slices.Clone(chosen)
	for len(keep) > 0 {
		sorted := slices.Clone(keep)
		sort.Ints(sorted)
		reads := held && len(sorted) < n
		note := choiceNote(p, sorted, ch, totals, ref, reads)
		size := rest + 2 + len(note) + 1
		for _, i := range sorted {
			size += len(p.elems[i]) + 1
		}
		if size <= limit {
			var b strings.Builder
			b.Write(data[:p.start])
			b.WriteByte('[')
			for k, i := range sorted {
				if k > 0 {
					b.WriteByte(',')
				}
				b.Write(p.elems[i])
			}
			b.WriteByte(']')
			b.Write(data[p.end:])
			b.WriteByte('\n')
			b.WriteString(note)
			ch.Shown = len(sorted)
			return b.String(), reads, true
		}
		if totals != "" {
			totals = ""
			continue
		}
		keep = keep[:len(keep)-1] // the model's least important goes first
	}
	return "", false, false
}

// choiceNote is Cogpit's note after a choice's entries: how many of how many, from how many
// offered, their numbers, the totals over all of them and how to read on.
func choiceNote(p *paged, shown []int, ch *engine.ChatChoice, totals, ref string, reads bool) string {
	n := p.count()
	var b strings.Builder
	fmt.Fprintf(&b, choiceNoteStart+"these %d of the %d %s%s as those that matter for the user's question", len(shown), n, p.unit(), p.of())
	if ch.Offered < n {
		fmt.Fprintf(&b, ", from the first %d", ch.Offered)
	}
	b.WriteString("; Cogpit shows them in the result's order, with their text as the result has it: ")
	for k, i := range shown {
		if k > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Itoa(i))
	}
	b.WriteByte('.')
	if ch.Chosen > len(shown) {
		fmt.Fprintf(&b, " %d more it chose do not fit.", ch.Chosen-len(shown))
	}
	if totals != "" {
		fmt.Fprintf(&b, " Counted over all %d: %s.", n, totals)
	}
	if reads {
		b.WriteString(headReadOn + readOn{Call: ref, Offset: 0}.String() + headNoteEnd)
		return b.String()
	}
	if len(shown) < n {
		b.WriteString(" The rest is not held: ask the tool for less, with a namespace, a filter or a limit.")
	}
	b.WriteByte(']')
	return b.String()
}

// choiceFailure is why a choice did not come, as the tool item and the note say it.
func choiceFailure(err error, st *turnState) string {
	switch {
	case errors.Is(err, errStopped):
		return "stopped"
	case errors.Is(err, errTimeLimit), errors.Is(err, errTokenLimit):
		return limitNote(err, st.limits)
	}
	return err.Error()
}

// choiceNoteText is Cogpit's note in the chat for a choice that ran or was refused.
func choiceNoteText(tool string, ch *engine.ChatChoice) (level, text string) {
	if ch.Failed != "" {
		return engine.LevelWarning, fmt.Sprintf("Choosing from the %d entries of %s's result failed (%s): the model reads its first "+
			"entries and Cogpit's totals instead.", ch.Entries, tool, ch.Failed)
	}
	read := fmt.Sprintf("%d", ch.Offered)
	if ch.Offered == ch.Entries {
		read = "all"
	}
	return engine.LevelInfo, fmt.Sprintf("%s chose %d of the %d entries of %s's result in a request of its own (it read %s of them, %s, in %s); "+
		"the conversation reads those, Cogpit's totals and result_read for the rest.", ch.Model, ch.Shown, ch.Entries, tool, read,
		formatKiB(ch.OfferedBytes), ch.Took)
}

// formatKiB is a size for a note: "58 KiB", "812 bytes".
func formatKiB(n int) string {
	if n < 1<<10 {
		return strconv.Itoa(n) + " bytes"
	}
	return strconv.Itoa((n+512)>>10) + " KiB"
}

// recordChoice keeps a choice's body (SentChoices) and counts it among what the turn sent.
func (a *Agent) recordChoice(st *turnState, body []byte) {
	c := st.c
	a.mu.Lock()
	if c.choices == nil {
		c.choices = map[int][]string{}
	}
	c.choices[st.userIdx] = append(c.choices[st.userIdx], string(body))
	if s := c.items[st.userIdx].Sent; s != nil {
		s.Requests++
		s.Choices++
		s.Bytes += len(body)
		s.Tokens = st.tokens
	}
	c.bump()
	a.mu.Unlock()
	a.notify(c, true)
}

// SentChoices implements engine.ChatAgent: the bodies of the choices the turn of item sent, as
// sent. Kept in memory only, as SentPayload's.
func (a *Agent) SentChoices(id string, item int) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.convs[id]
	if c == nil || item < 0 || item >= len(c.items) {
		return nil
	}
	for i := item; i >= 0; i-- {
		if c.items[i].Kind == engine.ChatItemUser {
			return slices.Clone(c.choices[i])
		}
	}
	return nil
}
