package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The raw results and result_read. A tool result that goes to the model as a head (head.go) is
// held whole in its conversation: in memory only, never in the conversations file, at most
// maxRawBytes per conversation with the oldest going first, and gone with the conversation or with
// the part of it a cut removes (conversation.cut). A result at or below the limit is not held, and
// neither is what result_read returns.
//
// A raw result is held under the tool call's id, which the head's note names. An endpoint may
// repeat its ids from one answer to the next: a result whose id already names an earlier one in
// the conversation is held under the id with a number ("call_0#2"), so that no note ever reads
// another call's result.
//
// result_read is the chat's own tool that reads a held result on: from an offset, the elements of
// the result's largest array or the lines of its largest string (the part the head shortened), as
// many as fit the same limit, or only those that contain a text. It reads no cluster: its note
// says when the result was read, and a result read while the connection was interrupted keeps the
// note that said so (D-58) on every page. It is a tool call like the others (the turn's limits, the
// stop when the window shows another cluster, a tool item in the window). It runs no command;
// each read on, served or refused, leaves its own line in the audit log (Session.AuditChatRead):
// what was asked, never the page.
//
// A result a turn over a private connection read holds what the window shows (H-94): it is handed
// to a turn over a private connection only. What a command reads under a switch the user can turn
// off again is not held at all (unheld): a held result would outlive the switch.

// maxRawBytes is the most a conversation holds of raw results, in bytes of their JSON.
const maxRawBytes = 8 << 20

// Bounds of what a note of Cogpit's repeats: a tool call's id a raw result is held under, the name
// made of it (the id and a number, rawRef), and the text result_read looks for.
const (
	maxCallID   = 64
	maxRef      = maxCallID + 8
	maxContains = 100
)

// unheld are the commands whose results are never held: dock.read reads a terminal tab only while
// the user's "Agents may read" switch is on (D-44) and pages a tab by itself.
var unheld = map[string]bool{"dock.read": true}

// minPage is the least room for a page's elements or lines beside a row table's columns and shared
// cells; with less, the page goes without them.
const minPage = 256

// rawResult is a tool result held whole.
type rawResult struct {
	ref     string    // the name it is held under
	data    []byte    // the result's JSON
	tool    string    // the tool that read it
	read    time.Time // when
	private bool      // read by a turn over a private connection
	stale   string    // Cogpit's note that the read was of the cache during an interruption (D-58), or ""
	hist    int       // the index of the call's tool message in the history
}

// holdable reports whether a result can be held under its call's id.
func holdable(id string, data []byte) bool {
	return id != "" && len(id) <= maxCallID && len(data) <= maxRawBytes
}

// rawRef is the name a new raw result of the tool call id is held under: the id, or, when a note
// in the conversation already names a raw result by it, the id with the first free number.
// Agent.mu is held.
func (c *conversation) rawRef(id string) string {
	ref := id
	for n := 2; c.refTaken(ref); n++ {
		ref = id + "#" + strconv.Itoa(n)
	}
	return ref
}

// refTaken reports whether a raw result is held under ref or a head's note in the history names
// one by it (headRef), held or not. Agent.mu is held.
func (c *conversation) refTaken(ref string) bool {
	if c.raws[ref] != nil {
		return true
	}
	return slices.ContainsFunc(c.history, func(m message) bool {
		named, ok := headRef(m.Content)
		return ok && m.Role == roleTool && named == ref
	})
}

// holdRaw holds r under its name; the oldest go beyond maxRawBytes, and a result larger than that
// is not held. Agent.mu is held.
func (c *conversation) holdRaw(r *rawResult) {
	c.dropRaw(r.ref)
	if len(r.data) > maxRawBytes {
		return
	}
	for len(c.rawOrder) > 0 && c.rawBytes+len(r.data) > maxRawBytes {
		c.dropRaw(c.rawOrder[0])
	}
	if c.raws == nil {
		c.raws = map[string]*rawResult{}
	}
	c.raws[r.ref] = r
	c.rawOrder = append(c.rawOrder, r.ref)
	c.rawBytes += len(r.data)
}

// dropRaw lets the raw result held under ref go. Agent.mu is held.
func (c *conversation) dropRaw(ref string) {
	r := c.raws[ref]
	if r == nil {
		return
	}
	delete(c.raws, ref)
	c.rawBytes -= len(r.data)
	c.rawOrder = slices.DeleteFunc(c.rawOrder, func(x string) bool { return x == ref })
}

// cutRaws lets the raw results go whose tool messages a cut removed: those from history index h
// on. Agent.mu is held.
func (c *conversation) cutRaws(h int) {
	for _, ref := range slices.Clone(c.rawOrder) {
		if c.raws[ref].hist >= h {
			c.dropRaw(ref)
		}
	}
}

// heldCalls names the newest raw results a turn may read, for the answer to a name that holds
// none: `"call_7" (resources_list)`. Agent.mu is held.
func (c *conversation) heldCalls(private bool) []string {
	const most = 3
	var out []string
	for i := len(c.rawOrder) - 1; i >= 0 && len(out) < most; i-- {
		if r := c.raws[c.rawOrder[i]]; !r.private || private {
			out = append(out, quoted(c.rawOrder[i])+" ("+r.tool+")")
		}
	}
	return out
}

// readArgs are result_read's arguments. A number may come as a string and the id as a number: a
// local model writes them either way.
type readArgs struct {
	Call     looseString `json:"call"`
	Offset   looseInt    `json:"offset"`
	Limit    looseInt    `json:"limit"`
	Contains string      `json:"contains"`
}

// looseInt is an integer that may be written as a number or as a string of one.
type looseInt int

func (n *looseInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(strings.Trim(string(b), `"`))
	if s == "" || s == jsonNull {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f != float64(int(f)) {
		return fmt.Errorf("%s is not a whole number", b)
	}
	*n = looseInt(f)
	return nil
}

// looseString is a string that may be written as a number.
type looseString string

func (s *looseString) UnmarshalJSON(b []byte) error {
	var text string
	if json.Unmarshal(b, &text) != nil {
		text = string(b) // a number, as it is written
		if text == jsonNull {
			text = ""
		}
	}
	*s = looseString(strings.TrimSpace(text))
	return nil
}

// check refuses arguments result_read cannot work with; the error is the text the model is sent.
func (a readArgs) check() error {
	switch {
	case a.Call == "":
		return errors.New(toolRead + ` needs "call": the id that Cogpit's note after a shortened result names`)
	case len(a.Call) > maxRef:
		return errors.New(`"call" is no id of a tool call: use the one that Cogpit's note after a shortened result names`)
	case len(a.Contains) > maxContains:
		return fmt.Errorf(`"contains" is at most %d bytes: look for a shorter text`, maxContains)
	}
	return nil
}

// readLabel is the tool item's label of a result_read call.
const readLabel = "Read on in a large result"

// runRead answers a result_read call from the conversation's raw results, as a tool item and a
// tool message of its own. What it returns is not held.
func (a *Agent) runRead(st *turnState, tc *engine.ChatToolCall, call toolCall, args readArgs) {
	id := string(args.Call)
	a.mu.Lock()
	raw := st.c.raws[id]
	held := st.c.heldCalls(st.conn.Private)
	a.mu.Unlock()
	var shown string
	var err error
	switch {
	case raw == nil:
		err = errors.New(notHeldText(id, held))
	case raw.private && !st.conn.Private:
		err = fmt.Errorf("the result of call %s was read over a private connection, and this turn's connection is not private, "+
			"so Cogpit does not hand it out: call the tool again", quoted(id))
	default:
		shown, err = readPage(raw, args, st.limits.MaxToolResultKiB<<10, a.s.TimeFormat().Format(raw.read))
	}
	// One line of the audit log for each read on, served or refused: what was asked, never the page.
	a.s.AuditChatRead(toolRead, readAudit{Call: id, Offset: int(args.Offset), Limit: int(args.Limit), Contains: args.Contains},
		st.c.info.Context, st.conn.Private, err)
	if err != nil {
		tc.State, tc.Error = engine.ChatToolFailed, err.Error()
		a.addToolItem(st, tc, call, frameError(toolRead, err.Error()), nil)
		return
	}
	tc.State, tc.Result = engine.ChatToolDone, shown
	content := resultFrame(toolRead) + shown
	if raw.stale != "" {
		content += "\n" + raw.stale
	}
	a.addToolItem(st, tc, call, content, nil)
}

// readAudit is result_read's parameters as the audit log keeps them.
type readAudit struct {
	Call     string `json:"call"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Contains string `json:"contains,omitempty"`
}

// notHeldText answers a name no raw result is held under: one that never had a result, a result
// the conversation's bound let go, or one from before Cogpit started.
func notHeldText(id string, held []string) string {
	text := "Cogpit holds no result of call " + quoted(id) + " (it holds only results too large to show whole, in memory, " +
		"the newest): call the tool again, with a filter or a limit."
	if len(held) > 0 {
		text += " Held now: " + strings.Join(held, ", ") + "."
	}
	return text
}

// readPage builds result_read's answer from a raw result: a page of its largest part as a JSON
// document of its own, a line break and Cogpit's note, together at most limit bytes. stamp is when
// the result was read, as the window writes times (D-31).
func readPage(r *rawResult, args readArgs, limit int, stamp string) (string, error) {
	p := largestPart(r.data)
	if p == nil {
		return "", errors.New("this result cannot be read in parts: call the tool again and ask for less")
	}
	n := p.count()
	offset := max(int(args.Offset), 0)
	// The note's room is measured with the widest numbers it can carry.
	widest := span{shown: n, next: n, left: n, cut: true, cutIndex: n, cutAt: len(r.data), cutSize: len(r.data)}
	room := limit - 1 - len(pageNote(p, widest, n, args, r, stamp))
	header := true
	budget := room - len(p.wrap(nil, header))
	if budget < minPage {
		header = false
		budget = room - len(p.wrap(nil, header))
	}
	if budget < minCut+2 {
		return "", errors.New("the connection's limit for a tool result leaves no room for a page of this result: call the tool again and ask for less")
	}
	sp := p.span(offset, int(args.Limit), args.Contains, budget, true)
	return string(p.wrap(sp.json, header)) + "\n" + pageNote(p, sp, offset, args, r, stamp), nil
}

// pageNote is Cogpit's note after a page: which elements or lines of which result it shows, when
// the result was read, and how to read on.
func pageNote(p *paged, sp span, offset int, args readArgs, r *rawResult, stamp string) string {
	all := fmt.Sprintf("the %d %s%s in the result of %s, as read at %s", p.count(), p.unit(), p.of(), r.tool, stamp)
	var b strings.Builder
	b.WriteString("[Cogpit: ")
	switch {
	case args.Contains == "" && sp.shown == 0:
		fmt.Fprintf(&b, "nothing from %d on in %s.", offset, all)
	case args.Contains == "":
		fmt.Fprintf(&b, "%s %d to %d of %s.", p.unit(), offset, sp.next-1, all)
	case sp.shown == 0:
		fmt.Fprintf(&b, "none that contain %s from %d on in %s.", quoted(args.Contains), offset, all)
	default:
		fmt.Fprintf(&b, "the %d that contain %s among %s %d to %d of %s.", sp.shown, quoted(args.Contains), p.unit(), offset, sp.next-1, all)
	}
	b.WriteString(cutClause(p, sp))
	next := readOn{Call: string(args.Call), Offset: sp.next, Limit: max(int(args.Limit), 0), Contains: args.Contains}
	switch {
	case sp.left == 0:
		b.WriteString(" Nothing follows.")
	case args.Contains == "":
		fmt.Fprintf(&b, " Read on with %s.", next)
	default:
		fmt.Fprintf(&b, " %d more follow: %s.", sp.left, next)
	}
	b.WriteByte(']')
	return b.String()
}
