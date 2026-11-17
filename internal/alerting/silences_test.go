package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Matchers match as Alertmanager's: = and != on the value, =~ and !~ anchored at both ends, a
// missing label the empty value.
func TestMatcherMatches(t *testing.T) {
	labels := map[string]string{"alertname": "KubePodCrashLooping", "namespace": "shop", "pod": "cart-7d9f"}
	for _, c := range []struct {
		name, op, value string
		want            bool
	}{
		{"alertname", "=", "KubePodCrashLooping", true},
		{"alertname", "=", "KubePod", false},
		{"alertname", "!=", "Other", true},
		{"namespace", "=~", "sh.*", true},
		{"namespace", "=~", "sh", false}, // anchored: no partial match
		{"namespace", "!~", "dev|stage", true},
		{"pod", "=~", "cart-.*|web-.*", true},
		{"missing", "=", "", true},
		{"missing", "!=", "", false},
		{"missing", "=~", ".*", true},
	} {
		m, err := NewMatcher(c.name, c.op, c.value)
		if err != nil {
			t.Fatalf("%s%s%s: %v", c.name, c.op, c.value, err)
		}
		if got := m.Matches(labels); got != c.want || m.Op() != c.op {
			t.Errorf("%s matches %v (op %s), want %v", m, got, m.Op(), c.want)
		}
	}
	if _, err := NewMatcher("pod", "=~", "cart-("); err == nil {
		t.Error("a regular expression that does not compile is accepted")
	}
	if _, err := NewMatcher("", "=", "x"); err == nil {
		t.Error("a matcher without a name is accepted")
	}
	if _, err := NewMatcher("a", "==", "x"); err == nil {
		t.Error("operator == is accepted")
	}
	if MatchesAll(nil, labels) {
		t.Error("no matchers match an alert")
	}
}

// A silence Alertmanager refuses is refused before it is sent, and so is one that would mute every
// alert.
func TestNewSilenceValidate(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	eq := func(n, v string) Matcher { m, _ := NewMatcher(n, "=", v); return m }
	re := func(n, v string) Matcher { m, _ := NewMatcher(n, "=~", v); return m }
	ok := NewSilence{Matchers: []Matcher{eq("alertname", "A")}, StartsAt: now, EndsAt: now.Add(time.Hour), CreatedBy: "hans", Comment: "maintenance"}
	if err := ok.Validate(now); err != nil {
		t.Fatalf("a valid silence: %v", err)
	}
	for name, mod := range map[string]func(n *NewSilence){
		"no matcher":   func(n *NewSilence) { n.Matchers = nil },
		"only empty":   func(n *NewSilence) { n.Matchers = []Matcher{re("alertname", ".*")} },
		"ends before":  func(n *NewSilence) { n.EndsAt = n.StartsAt.Add(-time.Minute) },
		"ended":        func(n *NewSilence) { n.StartsAt, n.EndsAt = now.Add(-2*time.Hour), now.Add(-time.Hour) },
		"no author":    func(n *NewSilence) { n.CreatedBy = "  " },
		"no comment":   func(n *NewSilence) { n.Comment = "" },
		"long comment": func(n *NewSilence) { n.Comment = strings.Repeat("x", maxComment+1) },
		"bad matcher": func(n *NewSilence) {
			n.Matchers = append(n.Matchers, Matcher{Name: "a", Value: "(", IsRegex: true, IsEqual: true})
		},
		"label name sep": func(n *NewSilence) { n.Matchers = []Matcher{{Name: "a=b", Value: "x", IsEqual: true}} },
	} {
		n := ok
		n.Matchers = append([]Matcher(nil), ok.Matchers...)
		mod(&n)
		if err := n.Validate(now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// GET /api/v2/silences is read, sorted (active ending soonest, pending, expired ending last first),
// bounded; an old Alertmanager's matchers without isEqual are equal ones; another shape is not an
// Alertmanager.
func TestDecodeSilences(t *testing.T) {
	at := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	s := func(id, state string, ends time.Time) map[string]any {
		return map[string]any{"id": id, "status": map[string]any{"state": state}, "createdBy": "hans", "comment": "c",
			"startsAt": at.Add(-time.Hour), "endsAt": ends, "updatedAt": at,
			"matchers": []any{map[string]any{"name": "alertname", "value": "A", "isRegex": false}}}
	}
	body, _ := json.Marshal([]any{
		s("e1", "expired", at.Add(-2*time.Hour)), s("a2", "active", at.Add(2*time.Hour)), s("p1", "pending", at.Add(5*time.Hour)),
		s("a1", "active", at.Add(time.Hour)), s("e2", "expired", at.Add(-time.Hour)),
	})
	got, err := decodeSilences(body)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, x := range got {
		ids = append(ids, x.ID)
	}
	if strings.Join(ids, ",") != "a1,a2,p1,e2,e1" {
		t.Errorf("order %v", ids)
	}
	if m := got[0].Matchers[0]; !m.IsEqual || m.IsRegex || m.Op() != "=" {
		t.Errorf("a matcher without isEqual: %+v", m)
	}
	for _, other := range []string{`{"status":"success"}`, `[{"name":"x"}]`, `<html>`} {
		if _, err := decodeSilences([]byte(other)); err != errNotAlertmanager {
			t.Errorf("%s: %v", other, err)
		}
	}
}

// While the silences page is shown, a poll asks for the silences after the alerts; turned on, a poll
// runs at once; the silenced alerts are apart; turned off, nothing more is asked for them.
func TestPollerSilences(t *testing.T) {
	px := &fakeProxy{}
	at := time.Now()
	muted := wireAlertOf("B", "warning", at, nil)
	muted["status"] = map[string]any{"state": "suppressed", "silencedBy": []string{"s-1"}}
	px.set("monitoring/alertmanager-operated:9093", answer(wire(t, wireAlertOf("A", "critical", at, nil), muted)))
	silences, _ := json.Marshal([]any{map[string]any{"id": "s-1", "status": map[string]any{"state": "active"}, "createdBy": "hans",
		"comment": "c", "startsAt": at.Add(-time.Hour), "endsAt": at.Add(time.Hour), "updatedAt": at,
		"matchers": []any{map[string]any{"name": "alertname", "value": "B", "isRegex": false, "isEqual": true}}}})
	px.silences = answer(silences)
	p := newPoller(px, services(Service{Namespace: "monitoring", Name: "alertmanager-operated", Ports: []ServicePort{{Name: "web", Port: 9093}}}),
		Options{Interval: time.Hour})
	defer p.Close()
	p.SetActive(true)
	waitFor(t, "the poll", func() bool { return p.Snapshot().Answered() })
	snap := p.Snapshot()
	if snap.Total != 1 || snap.MutedTotal != 1 || snap.Muted[0].Name != "B" || len(snap.Silences) != 0 || !snap.SilencesAsOf.IsZero() {
		t.Fatalf("before the silences page: %+v", snap)
	}
	p.SetSilences(true) // a poll at once, though the interval is an hour
	waitFor(t, "the poll", func() bool { return !p.Snapshot().SilencesAsOf.IsZero() })
	snap = p.Snapshot()
	if len(snap.Silences) != 1 || snap.Silences[0].ID != "s-1" || snap.SilencesError != "" {
		t.Errorf("silences: %+v, %q", snap.Silences, snap.SilencesError)
	}
	// A failing silences request keeps the last answer and says why.
	px.silences = func() ([]byte, error) {
		return nil, apierrors.NewServiceUnavailable("no endpoints available for service")
	}
	p.Refresh()
	waitFor(t, "the poll", func() bool { return p.Snapshot().SilencesError != "" })
	if snap := p.Snapshot(); len(snap.Silences) != 1 || !strings.Contains(snap.SilencesError, "silences of monitoring/alertmanager-operated:9093") {
		t.Errorf("after a failure: %+v, %q", snap.Silences, snap.SilencesError)
	}
	p.SetSilences(false)
	n := px.count()
	p.Refresh()
	waitFor(t, "the poll", func() bool { return px.count() > n })
	for _, q := range px.asked[n:] {
		if strings.Contains(q, silencesPath) {
			t.Errorf("silences asked with the page closed: %s", q)
		}
	}
}

// A silence is posted to the Alertmanager that answered, validated first; its id comes back and a
// poll runs at once. An expire is a DELETE of its id. A refusal names the request.
func TestPollerSilenceWrites(t *testing.T) {
	px := &fakeProxy{}
	px.set("monitoring/alertmanager-operated:9093", answer(wire(t)))
	p := newPoller(px, services(Service{Namespace: "monitoring", Name: "alertmanager-operated", Ports: []ServicePort{{Name: "web", Port: 9093}}}),
		Options{Interval: time.Hour})
	defer p.Close()
	eq, _ := NewMatcher("alertname", "=", "A")
	n := NewSilence{Matchers: []Matcher{eq}, StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), CreatedBy: "hans", Comment: "deploy"}
	if _, _, err := p.CreateSilence(context.Background(), n); !errors.Is(err, ErrNoAlertmanager) {
		t.Errorf("before an answer: %v", err)
	}
	p.SetActive(true)
	waitFor(t, "the poll", func() bool { return p.Snapshot().Answered() })

	var method, path string
	px.writes = func(m, pth string, body []byte) ([]byte, error) {
		method, path = m, pth
		return []byte(`{"silenceID":"8f2c-11"}`), nil
	}
	before := px.count()
	id, tg, err := p.CreateSilence(context.Background(), n)
	if err != nil || id != "8f2c-11" || tg.Where() != "monitoring/alertmanager-operated:9093" || method != http.MethodPost || path != silencesPath {
		t.Fatalf("create: %q %s %v (%s %s)", id, tg.Where(), err, method, path)
	}
	var sent NewSilence
	if err := json.Unmarshal([]byte(px.sent[0]), &sent); err != nil || sent.Comment != "deploy" || sent.Matchers[0].Name != "alertname" {
		t.Errorf("sent %s: %v", px.sent[0], err)
	}
	waitFor(t, "the poll", func() bool { return px.count() > before+1 }) // the poll after it

	bad := n
	bad.Comment = ""
	if _, _, err := p.CreateSilence(context.Background(), bad); err == nil || strings.Contains(strings.Join(px.asked, " "), "POST") && len(px.sent) > 1 {
		t.Errorf("an invalid silence: %v, sent %d", err, len(px.sent))
	}

	if _, err := p.ExpireSilence(context.Background(), "8f2c-11"); err != nil || method != http.MethodDelete || path != silencePath+"8f2c-11" {
		t.Errorf("expire: %v (%s %s)", err, method, path)
	}
	if _, err := p.ExpireSilence(context.Background(), "../alerts"); err == nil {
		t.Error("an id with a path is accepted")
	}

	// A refusal of the API server names the request and quotes the server's own message.
	px.writes = func(m, pth string, _ []byte) ([]byte, error) {
		return nil, newRequestError(m, "https://api.example:6443/api/v1/namespaces/monitoring/services/alertmanager-operated:9093/proxy"+pth,
			Target{Namespace: "monitoring", Service: "alertmanager-operated", Port: "9093"},
			apierrors.NewForbidden(schema.GroupResource{Resource: "services/proxy"}, "alertmanager-operated", errors.New("RBAC: create not allowed")), nil)
	}
	_, err = p.ExpireSilence(context.Background(), "8f2c-11")
	var re *RequestError
	if !errors.As(err, &re) || !strings.Contains(err.Error(), "DELETE https://api.example:6443/api/v1/namespaces/monitoring/services/alertmanager-operated:9093/proxy/api/v2/silence/8f2c-11") ||
		!strings.Contains(err.Error(), "forbidden (HTTP 403)") || !strings.Contains(err.Error(), "RBAC: create not allowed") {
		t.Errorf("refused: %v", err)
	}
}

// Alertmanager's own answer to a refused silence is quoted, one line, bounded; a JSON string's
// quotes go.
func TestRequestErrorQuotesAlertmanager(t *testing.T) {
	answer := []byte(`"silence invalid: invalid label matcher 0: invalid regular expression"` + "\n")
	err := apierrors.NewGenericServerResponse(400, "POST", schema.GroupResource{Resource: "services"}, "am", string(answer), 0, true)
	e := newRequestError("POST", "https://api/x", Target{Namespace: "m", Service: "am", Port: "9093"}, err, answer)
	if e.Answer != "silence invalid: invalid label matcher 0: invalid regular expression" || !strings.Contains(e.Error(), "POST https://api/x (Alertmanager m/am:9093)") {
		t.Errorf("%q / %v", e.Answer, e)
	}
	long := []byte(strings.Repeat("x ", 400))
	e = newRequestError("POST", "u", Target{}, errors.New("x"), long)
	if len(e.Answer) > maxAnswer+len("…") {
		t.Errorf("%d bytes quoted", len(e.Answer))
	}
}
