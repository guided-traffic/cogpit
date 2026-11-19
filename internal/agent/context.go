package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Context editing (D-167). Every request carries the whole conversation, and a conversation only
// grows: tool results are most of it. Before a request goes out its size is held against the
// connection's context window (engine.ChatLimits.ContextWindow). A request that would reach ClearAt
// percent of the window goes out with the conversation's oldest tool results replaced by a note,
// as many as bring it down to ClearTo percent.
//
//   - Only what is sent changes: the history, the window's items and the conversations file keep
//     every result. The request body the window shows (SentPayload) is the one that went.
//   - Only tool results go, and only those above minClearable: the user's messages, the model's
//     answers and its tool calls with their arguments stay, so the model still knows what it asked
//     for and what it changed.
//   - A result the model has not been sent yet stays: the results after its last message.
//   - Oldest first, and once removed a result stays removed (conversation.cleared only grows; a cut
//     takes it back with the history). Between two clearings every request so begins with the same
//     bytes as the one before it, which an endpoint's prompt cache needs: a local server reads only
//     what is new. A clearing costs that once, from the first removed result on, which is why it
//     removes down to ClearTo at once and not a result with each request.
//
// The size is the endpoint's own count: the prompt tokens it reported for the last request, scaled
// by the bodies' sizes; without one, about four bytes a token.

// minClearable is the size above which a tool result is worth replacing by its note.
const minClearable = 1 << 10

// Bounds of a believable calibration, in bytes of request body per prompt token; a count outside
// them (an endpoint that reports only the tokens it read anew, say) is not used.
const (
	minBytesPerToken = 1
	maxBytesPerToken = 16
)

// clearedNote stands in a request for a tool result that was taken out.
func clearedNote(tool string, bytes int) string {
	if tool == "" {
		tool = "a tool"
	}
	return "Result of " + tool + ": removed by Cogpit to keep the conversation within the model's context (it had " +
		strconv.Itoa(bytes) + " bytes). Call the tool again if you need it."
}

// clearable reports whether a history message is a tool result that clearing replaces.
func clearable(m message) bool {
	return m.Role == roleTool && len(m.Content) > minClearable
}

// sentHistory is the history as a request carries it: of the messages before cleared, the
// clearable tool results replaced by their notes. The history itself is not changed.
func sentHistory(history []message, cleared int) []message {
	cleared = min(cleared, len(history))
	if cleared <= 0 {
		return history
	}
	out := make([]message, len(history))
	copy(out, history)
	tools := map[string]string{} // tool call id → tool name
	for i := range cleared {
		m := history[i]
		for _, tc := range m.ToolCalls {
			tools[tc.ID] = tc.Function.Name
		}
		if clearable(m) {
			out[i].Content = clearedNote(tools[m.ToolCallID], len(m.Content))
		}
	}
	return out
}

// calibrate keeps what the endpoint counted for a request body of bodyBytes. Agent.mu is held.
func (c *conversation) calibrate(bodyBytes, promptTokens int) {
	if bodyBytes <= 0 || promptTokens <= 0 || bodyBytes < promptTokens*minBytesPerToken || bodyBytes > promptTokens*maxBytesPerToken {
		return
	}
	c.calBytes, c.calTokens = bodyBytes, promptTokens
}

// contextTokens is what a request body of bodyBytes takes of the context window: scaled from the
// endpoint's count of the last request, else about four bytes a token. Agent.mu is held.
func (c *conversation) contextTokens(bodyBytes int) int {
	if c.calBytes > 0 && c.calTokens > 0 {
		return int(int64(bodyBytes) * int64(c.calTokens) / int64(c.calBytes))
	}
	return estimateTokens(bodyBytes)
}

// jsonSize is the size of s as a JSON string, as a request body holds it.
func jsonSize(s string) int {
	b, err := json.Marshal(s)
	if err != nil {
		return len(s)
	}
	return len(b)
}

// clearing is what one check of the context did.
type clearing struct {
	removed       int // tool results taken out now
	before, after int // the request's tokens before and after
	full          bool
}

// clearContext holds a request body of bodyBytes against the context window and, at ClearAt
// percent of it, takes the oldest tool results out of what is sent until the request is down to
// ClearTo percent or none is left. full: the conversation would stay at ClearAt without any tool
// result, so no clearing can shorten it. Agent.mu is held.
func (c *conversation) clearContext(l engine.ChatLimits, bodyBytes int) clearing {
	high, low := l.ContextWindow*l.ClearAt/100, l.ContextWindow*l.ClearTo/100
	r := clearing{before: c.contextTokens(bodyBytes)}
	r.after = r.before
	if r.before < high {
		return r
	}
	// The results after the model's last message have not been sent yet: they stay.
	end := 0
	for i := len(c.history) - 1; i >= 0; i-- {
		if c.history[i].Role == roleAssistant {
			end = i
			break
		}
	}
	tools := map[string]string{}
	for _, m := range c.history[:min(c.cleared, end)] {
		for _, tc := range m.ToolCalls {
			tools[tc.ID] = tc.Function.Name
		}
	}
	i := c.cleared
	for ; i < end && r.after > low; i++ {
		m := c.history[i]
		for _, tc := range m.ToolCalls {
			tools[tc.ID] = tc.Function.Name
		}
		if !clearable(m) {
			continue
		}
		bodyBytes -= jsonSize(m.Content) - jsonSize(clearedNote(tools[m.ToolCallID], len(m.Content)))
		r.after = c.contextTokens(bodyBytes)
		r.removed++
	}
	c.cleared = max(c.cleared, i)
	if r.after < high {
		return r
	}
	// Still at ClearAt: full when the results not sent yet, which the next request may clear, are
	// not what keeps it there.
	for _, m := range c.history[min(i, len(c.history)):] {
		for _, tc := range m.ToolCalls {
			tools[tc.ID] = tc.Function.Name
		}
		if clearable(m) {
			bodyBytes -= jsonSize(m.Content) - jsonSize(clearedNote(tools[m.ToolCallID], len(m.Content)))
		}
	}
	r.full = c.contextTokens(bodyBytes) >= high
	return r
}

// clearedText is the chat's note of a clearing.
func clearedText(r clearing, l engine.ChatLimits) string {
	results := "old tool results"
	if r.removed == 1 {
		results = "old tool result"
	}
	return fmt.Sprintf("Cogpit removed %d %s from what the model is sent: the conversation reached %d%% of the context window "+
		"(%s tokens) and is now at about %d%%. The model can read them again with its tools. This answer may start later, as the "+
		"endpoint reads the shortened conversation anew.",
		r.removed, results, percent(r.before, l.ContextWindow), count(l.ContextWindow), percent(r.after, l.ContextWindow))
}

// fullText is the chat's note of a conversation that clearing cannot shorten any more.
func fullText(r clearing, l engine.ChatLimits) string {
	return fmt.Sprintf("The conversation fills about %d%% of the context window (%s tokens), and its messages alone keep it "+
		"there: no tool result is left to remove. Start a new conversation before the model's context overflows.",
		percent(r.after, l.ContextWindow), count(l.ContextWindow))
}

func percent(n, of int) int {
	if of <= 0 {
		return 0
	}
	return int(int64(n) * 100 / int64(of))
}

// count writes a number with thousands separators: 3,000,000.
func count(n int) string {
	s := strconv.Itoa(n)
	if n < 0 || len(s) <= 3 {
		return s
	}
	var b strings.Builder
	head := len(s) % 3
	b.WriteString(s[:head])
	for i := head; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
