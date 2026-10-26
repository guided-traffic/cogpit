package agent

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

// The connection test's record of its requests (the owner, 2026-10-26: "WELCHER ENDPUNKT WURDE WIE
// AUFGERUFEN???", after a failed Tool use step said only "the endpoint answered HTTP 404 Not Found:
// Not Found"). A layer that sends requests keeps its last one with the answer, and a failed layer
// shows it in curl -v's form: where the connection went, the request line, the header fields as
// they went on the wire, the body, the answer's status line, its header fields and the start of its
// body, and how long it took. A server's own log and a curl compare with it line by line.
//
// The key never shows: an Authorization field is written with its scheme and •••, so that it says
// whether a key went; the key is removed from every other field and from the answer's body.
// Set-Cookie's values are withheld. A 401's or 403's body is not read (httpErrorFrom), as it may
// quote a part of the key, so it does not show either.

// maxExchangeBody bounds a request's and an answer's body in the record.
const maxExchangeBody = 2 << 10

// exchange is one request and its answer.
type exchange struct {
	mu        sync.Mutex
	method    string
	url       string
	addr      string     // the address the connection reached
	sent      [][]string // the header fields as written, name and value
	reqHeader http.Header
	body      []byte
	proto     string // the answer's: "HTTP/1.1"
	status    string // "404 Not Found"
	header    http.Header
	answer    bytes.Buffer // the answer's body as the client read it, up to maxExchangeBody
	more      bool         // the client read more than maxExchangeBody
	err       error        // why no answer came
	took      time.Duration
}

// recorder is an http.RoundTripper that records the last exchange a layer's client made.
type recorder struct {
	next http.RoundTripper
	mu   sync.Mutex
	last *exchange
}

// recording is a copy of cl whose requests rec records.
func recording(cl *client) (*client, *recorder) {
	rt := cl.hc.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	rec := &recorder{next: rt}
	c := *cl
	c.hc = &http.Client{Transport: rec, CheckRedirect: cl.hc.CheckRedirect, Jar: cl.hc.Jar, Timeout: cl.hc.Timeout}
	return &c, rec
}

// RoundTrip implements http.RoundTripper.
func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	x := &exchange{method: req.Method, url: req.URL.String(), reqHeader: req.Header.Clone()}
	if req.GetBody != nil {
		if b, err := req.GetBody(); err == nil {
			x.body, _ = io.ReadAll(io.LimitReader(b, maxExchangeBody+1))
			_ = b.Close()
		}
	}
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			x.mu.Lock()
			x.addr = info.Conn.RemoteAddr().String()
			x.mu.Unlock()
		},
		WroteHeaderField: func(key string, value []string) {
			x.mu.Lock()
			x.sent = append(x.sent, []string{key, strings.Join(value, ", ")})
			x.mu.Unlock()
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	r.mu.Lock()
	r.last = x
	r.mu.Unlock()
	start := time.Now()
	resp, err := r.next.RoundTrip(req)
	x.mu.Lock()
	defer x.mu.Unlock()
	x.took = time.Since(start)
	if err != nil {
		x.err = err
		return nil, err
	}
	x.proto, x.status, x.header = resp.Proto, resp.Status, resp.Header.Clone()
	resp.Body = &teeBody{ReadCloser: resp.Body, x: x}
	return resp, nil
}

// teeBody keeps the start of an answer's body as the client reads it.
type teeBody struct {
	io.ReadCloser
	x *exchange
}

func (t *teeBody) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if n > 0 {
		t.x.mu.Lock()
		room := maxExchangeBody - t.x.answer.Len()
		t.x.answer.Write(p[:min(n, room)])
		t.x.more = t.x.more || n > room
		t.x.mu.Unlock()
	}
	return n, err
}

// text is the last exchange in curl -v's form, the key removed; "" when the layer sent nothing.
func (r *recorder) text(key string) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	x := r.last
	r.mu.Unlock()
	if x == nil {
		return ""
	}
	return x.text(key)
}

func (x *exchange) text(key string) string {
	x.mu.Lock()
	defer x.mu.Unlock()
	var b strings.Builder
	line := func(prefix, s string) {
		b.WriteString(prefix)
		b.WriteString(s)
		b.WriteByte('\n')
	}
	if x.addr != "" {
		line("* ", "connected to "+x.addr)
	}
	line("> ", x.method+" "+x.url)
	sent := x.sent
	if len(sent) == 0 { // nothing went on the wire: the fields the request carried
		for _, k := range slices.Sorted(maps.Keys(x.reqHeader)) {
			sent = append(sent, []string{k, strings.Join(x.reqHeader[k], ", ")})
		}
	}
	keyed := false
	for _, f := range sent {
		if strings.EqualFold(f[0], "Authorization") {
			keyed = true
		}
		line("> ", f[0]+": "+fieldValue(f[0], f[1], key))
	}
	if !keyed {
		line("* ", "no Authorization field: no key went")
	}
	if len(x.body) > 0 {
		line(">", "")
		for _, l := range bodyLines(x.body, key, len(x.body) > maxExchangeBody) {
			line("> ", l)
		}
	}
	if x.err != nil {
		line("* ", "no answer after "+formatTook(x.took)+": "+cleanMessage(x.err.Error(), key))
		return strings.TrimSuffix(b.String(), "\n")
	}
	line("< ", x.proto+" "+x.status)
	for _, k := range slices.Sorted(maps.Keys(x.header)) {
		line("< ", k+": "+fieldValue(k, strings.Join(x.header[k], ", "), key))
	}
	switch {
	case x.answer.Len() > 0:
		line("<", "")
		for _, l := range bodyLines(x.answer.Bytes(), key, x.more) {
			line("< ", l)
		}
	case strings.HasPrefix(x.status, "401") || strings.HasPrefix(x.status, "403"):
		line("* ", "the body not shown: a refusal may quote a part of the key")
	}
	line("* ", formatTook(x.took)+" to the answer's header")
	return strings.TrimSuffix(b.String(), "\n")
}

// fieldValue is a header field's value as the record shows it: an Authorization field's scheme and
// •••, a Set-Cookie's withheld, the key removed from any other.
func fieldValue(name, value, key string) string {
	switch {
	case strings.EqualFold(name, "Authorization"), strings.EqualFold(name, "Proxy-Authorization"):
		if scheme, _, ok := strings.Cut(value, " "); ok {
			return scheme + " •••"
		}
		return "•••"
	case strings.EqualFold(name, "Set-Cookie"), strings.EqualFold(name, "Cookie"):
		return "(withheld)"
	}
	return oneLine(value, key)
}

// bodyLines are a body's lines, the key removed, control characters shown as �; more marks a body
// cut at maxExchangeBody.
func bodyLines(body []byte, key string, more bool) []string {
	s := string(body)
	if len(s) > maxExchangeBody {
		s = s[:maxExchangeBody]
	}
	s = strings.ToValidUTF8(truncateUTF8(s, maxExchangeBody), "�")
	if key != "" {
		s = strings.ReplaceAll(s, key, "•••")
	}
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Map(func(r rune) rune {
			if r == '\t' {
				return ' '
			}
			if unicode.IsControl(r) {
				return '�'
			}
			return r
		}, l)
	}
	if more {
		lines = append(lines, fmt.Sprintf("… (cut after %d bytes)", maxExchangeBody))
	}
	return lines
}

// oneLine is a header value on one line without the key.
func oneLine(s, key string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "•••")
	}
	return strings.Join(strings.Fields(s), " ")
}

// formatTook writes how long a request took: "38 ms", "1.2 s".
func formatTook(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%d ms", max(d.Milliseconds(), 1))
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
}
