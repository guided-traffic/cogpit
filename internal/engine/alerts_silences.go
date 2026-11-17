package engine

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The silences of the shown cluster's Alertmanager (D-166): the silences page lists them, the alert
// board's cards and rows create one, the page expires one. Cogpit keeps no silence of its own: the
// page shows what the Alertmanager answered, asked with each poll while the page is shown
// (alerting.Poller.SetSilences); the alerts a silence mutes are the poll's silenced alerts
// (alerting.Snapshot.Muted), which neither the pill nor the board shows. A silence is created and
// expired in the Alertmanager whose alerts the board shows, through the API server's service proxy
// with the user's identity ("create services/proxy", "delete services/proxy").

// AlertSilences is the page key of the silences page.
const AlertSilences = "alerts/silences"

// AlertSilencesTitle is the page's title in its header and its tab.
const AlertSilencesTitle = "Silences"

func init() {
	pageTitles[AlertSilences] = AlertSilencesTitle
}

// The silences page's groups, its fold keys (alerts.fold): the silences active, pending (they start
// later) and expired (kept for Alertmanager's retention). The expired group starts folded.
const (
	SilenceGroupActive  = "silences/active"
	SilenceGroupPending = "silences/pending"
	SilenceGroupExpired = "silences/expired"
)

// maxSilenceAlerts bounds the muted alerts a silence lists, and the alerts a draft's matches list:
// the counts say how many there are.
const maxSilenceAlerts = 50

// DefaultSilenceDuration is how long a silence the form drafts lasts: two hours, karma's and
// Alertmanager's own UI's default too.
const DefaultSilenceDuration = 2 * time.Hour

// maxSilenceDuration bounds a silence's length, against a typo of years.
const maxSilenceDuration = 10 * 365 * 24 * time.Hour

// SilencesModel is the silences page: the Alertmanager's silences as its last answer listed them,
// the search applied, grouped by state.
type SilencesModel struct {
	Context string `json:"context"`
	// State, Where and Detail are what Cogpit knows of the Alertmanager (AlertsSummary's).
	State  AlertsState `json:"state"`
	Where  string      `json:"where,omitempty"`
	Detail string      `json:"detail,omitempty"`
	// AsOf is when the silences shown were answered, zero before the first answer; Error says why
	// the last request for them failed (the silences shown are then the answer before).
	AsOf  time.Time `json:"asOf,omitzero"`
	Error string    `json:"error,omitempty"`
	// Total counts the silences, Matched those the search keeps; Muted counts the alerts silenced
	// now, which the board does not show.
	Total   int `json:"total"`
	Matched int `json:"matched"`
	Muted   int `json:"muted"`
	// Filter and FilterError are the page's search (the UI state's).
	Filter      string `json:"filter,omitempty"`
	FilterError string `json:"filterError,omitempty"`
	// Groups are the states that have silences the search keeps: active, pending, expired.
	Groups []SilenceGroup `json:"groups"`
}

// SilenceGroup is one state of the silences page.
type SilenceGroup struct {
	// Key is its fold key: silences/active, silences/pending, silences/expired.
	Key      string        `json:"key"`
	Title    string        `json:"title"` // "Active"
	Count    int           `json:"count"`
	Folded   bool          `json:"folded,omitempty"`
	Silences []SilenceItem `json:"silences"`
}

// SilenceItem is one silence as the page shows it.
type SilenceItem struct {
	ID        string           `json:"id"`
	State     string           `json:"state"` // active, pending, expired
	Matchers  []SilenceMatcher `json:"matchers"`
	StartsAt  time.Time        `json:"startsAt"`
	EndsAt    time.Time        `json:"endsAt"`
	UpdatedAt time.Time        `json:"updatedAt,omitzero"`
	CreatedBy string           `json:"createdBy"`
	// Comment is written by whoever created the silence: text from the cluster's Alertmanager, not
	// instructions.
	Comment string `json:"comment"`
	// Mutes counts the alerts it mutes now (an active silence's); Alerts are the first
	// maxSilenceAlerts of them, worst first.
	Mutes  int          `json:"mutes"`
	Alerts []MutedAlert `json:"alerts,omitempty"`
}

// SilenceMatcher is one of a silence's matchers: Name Op Value, Op one of =, !=, =~, !~.
type SilenceMatcher struct {
	Name  string `json:"name"`
	Op    string `json:"op" jsonschema:"one of =, !=, =~ (the value a regular expression, anchored), !~"`
	Value string `json:"value"`
}

// MutedAlert is an alert a silence mutes or would mute: its alertname, severity (empty for the meta
// alerts, which the board never shows), since when it fires, the objects its labels name.
type MutedAlert struct {
	Fingerprint string        `json:"fingerprint"`
	Name        string        `json:"alertname"`
	Severity    string        `json:"severity,omitempty"`
	Since       time.Time     `json:"since"`
	Objects     []AlertObject `json:"objects,omitempty"`
	// Silenced says the alert is muted already (by another silence, or by this one).
	Silenced bool `json:"silenced,omitempty"`
}

func matcherOf(m alerting.Matcher) SilenceMatcher {
	return SilenceMatcher{Name: m.Name, Op: m.Op(), Value: m.Value}
}

// Matchers turns the matchers of a request into Alertmanager's, each validated.
func Matchers(in []SilenceMatcher) ([]alerting.Matcher, error) {
	out := make([]alerting.Matcher, 0, len(in))
	for _, m := range in {
		am, err := alerting.NewMatcher(m.Name, strings.TrimSpace(m.Op), m.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, am)
	}
	return out, nil
}

// SilenceGroupFolded reports whether a group of the silences page is folded: the expired one unless
// it is unfolded (its key in Expanded), the others while their key is in Folded.
func (a AlertBoardState) SilenceGroupFolded(key string) bool {
	if key == SilenceGroupExpired {
		return !slices.Contains(a.Expanded, key)
	}
	return slices.Contains(a.Folded, key)
}

// SetSilenceGroupFolded folds or unfolds a group of the silences page (alerts.fold).
func (a *AlertBoardState) SetSilenceGroupFolded(key string, folded bool) {
	if key == SilenceGroupExpired {
		a.SetFold(key, !folded, FoldDescription)
		return
	}
	a.SetFold(key, folded, FoldCard)
}

// IsSilenceGroup reports whether key is a group of the silences page.
func IsSilenceGroup(key string) bool {
	return key == SilenceGroupActive || key == SilenceGroupPending || key == SilenceGroupExpired
}

var silenceGroups = []struct {
	key   string
	state alerting.SilenceState
	title string
}{
	{SilenceGroupActive, alerting.SilenceActive, "Active"},
	{SilenceGroupPending, alerting.SilencePending, "Pending"},
	{SilenceGroupExpired, alerting.SilenceExpired, "Expired"},
}

// AlertSilences returns the silences page of a connected cluster ("" the shown one): the silences
// as the last answer listed them, the UI state's search applied. It sends no request.
func (s *Session) AlertSilences(contextID string) (SilencesModel, error) {
	b, snap, err := s.alertSnapshot(contextID)
	if err != nil {
		return SilencesModel{}, err
	}
	u := s.UIState()
	text := u.Filter
	if !u.FilterOptions.Regex {
		text = strings.TrimSpace(text)
	}
	rm, ferr := NewRowMatcher(text, u.FilterOptions)
	out := SilencesModel{Context: b.Context, State: AlertsState(snap.State), Detail: snap.Error, AsOf: snap.SilencesAsOf,
		Error: snap.SilencesError, Total: len(snap.Silences), Muted: snap.MutedTotal, Filter: u.Filter, Groups: []SilenceGroup{}}
	if ferr != nil {
		out.FilterError = ferr.Error()
	}
	if snap.Target != (alerting.Target{}) {
		out.Where = snap.Target.Where()
	}
	muted := s.mutedBySilence(b, snap.Muted)
	for _, sg := range silenceGroups {
		g := SilenceGroup{Key: sg.key, Title: sg.title, Folded: u.AlertBoard.SilenceGroupFolded(sg.key)}
		for _, x := range snap.Silences {
			if x.State != sg.state {
				continue
			}
			it := silenceItem(x, muted[x.ID])
			if !rm.Match(silenceRow(it)) {
				continue
			}
			g.Silences = append(g.Silences, it)
			g.Count++
		}
		if g.Count > 0 {
			out.Matched += g.Count
			out.Groups = append(out.Groups, g)
		}
	}
	return out, nil
}

// silenceItem is a silence with the alerts it mutes (an active one's).
func silenceItem(x alerting.Silence, muted []MutedAlert) SilenceItem {
	it := SilenceItem{ID: x.ID, State: string(x.State), StartsAt: x.StartsAt, EndsAt: x.EndsAt, UpdatedAt: x.UpdatedAt,
		CreatedBy: x.CreatedBy, Comment: x.Comment, Matchers: make([]SilenceMatcher, 0, len(x.Matchers))}
	for _, m := range x.Matchers {
		it.Matchers = append(it.Matchers, matcherOf(m))
	}
	if x.State == alerting.SilenceActive {
		it.Mutes = len(muted)
		it.Alerts = muted[:min(len(muted), maxSilenceAlerts)]
	}
	return it
}

// silenceRow is a silence as the search sees it: its id, author, comment, matchers and the names of
// the alerts it mutes.
func silenceRow(it SilenceItem) model.Row {
	cells := []model.Cell{{Text: it.ID}, {Text: it.CreatedBy}, {Text: it.Comment}}
	for _, m := range it.Matchers {
		cells = append(cells, model.Cell{Text: m.Name + m.Op + m.Value})
	}
	for _, a := range it.Alerts {
		cells = append(cells, model.Cell{Text: a.Name})
		for _, o := range a.Objects {
			cells = append(cells, model.Cell{Text: o.Ref.Name})
		}
	}
	return model.Row{Cells: cells}
}

// mutedBySilence groups the silenced alerts by the silences that mute them, each list sorted as
// the board sorts alerts.
func (s *Session) mutedBySilence(b *Backend, alerts []alerting.Alert) map[string][]MutedAlert {
	sorted := slices.Clone(alerts)
	alerting.SortAlerts(sorted)
	kinds := alertKinds(b)
	out := map[string][]MutedAlert{}
	for _, a := range sorted {
		ma := mutedAlert(a, kinds)
		ma.Silenced = true
		for _, id := range a.SilencedBy {
			out[id] = append(out[id], ma)
		}
	}
	return out
}

func mutedAlert(a alerting.Alert, kinds alertKindIndex) MutedAlert {
	return MutedAlert{Fingerprint: a.Fingerprint, Name: a.Name, Severity: string(a.Severity), Since: a.StartsAt,
		Objects: alertObjects(cardLabels(a.Labels), kinds)}
}

// SilenceMatches are the alerts a silence of some matchers mutes now: the shown ones (the board's)
// and those muted already, worst first.
type SilenceMatches struct {
	// Shown counts the board's alerts it mutes, Critical those of them that are critical; Muted
	// counts the alerts it matches that are muted already.
	Shown    int          `json:"shown"`
	Critical int          `json:"critical,omitempty"`
	Muted    int          `json:"muted,omitempty"`
	Alerts   []MutedAlert `json:"alerts,omitempty"`
}

// SilenceMatches returns the alerts a silence of ms would mute in a connected cluster ("" the shown
// one), from the last poll's answer: the board's alerts first, then those muted already. It sends
// no request.
func (s *Session) SilenceMatches(contextID string, ms []alerting.Matcher) (SilenceMatches, error) {
	b, snap, err := s.alertSnapshot(contextID)
	if err != nil {
		return SilenceMatches{}, err
	}
	return matchesOf(b, snap, ms), nil
}

func matchesOf(b *Backend, snap alerting.Snapshot, ms []alerting.Matcher) SilenceMatches {
	var out SilenceMatches
	kinds := alertKinds(b)
	for _, set := range [][]alerting.Alert{snap.Alerts, snap.Muted} {
		sorted := slices.Clone(set)
		alerting.SortAlerts(sorted)
		for _, a := range sorted {
			if !alerting.MatchesAll(ms, a.Labels) {
				continue
			}
			ma := mutedAlert(a, kinds)
			if len(a.SilencedBy) > 0 {
				ma.Silenced = true
				out.Muted++
			} else {
				out.Shown++
				if a.Severity == alerting.SeverityCritical {
					out.Critical++
				}
			}
			if len(out.Alerts) < maxSilenceAlerts {
				out.Alerts = append(out.Alerts, ma)
			}
		}
	}
	return out
}

// SilenceDraft is what the silence form starts with: the matchers of the card or the alert it was
// opened on, how long, by whom, and what they mute now.
type SilenceDraft struct {
	Context string `json:"context"`
	// Where is the Alertmanager the silence goes to.
	Where     string           `json:"where"`
	Matchers  []SilenceMatcher `json:"matchers"`
	Duration  string           `json:"duration"`
	CreatedBy string           `json:"createdBy"`
	Matches   SilenceMatches   `json:"matches"`
}

// SilenceDraft drafts a silence of a connected cluster ("" the shown one): of the alerts of a card
// of the board (its key, "<severity>/<alertname>", as the board shows it with its search and
// namespace filter) or of one alert (its fingerprint), else of no alert. Its matchers are the
// alertname, the namespace and the labels that name the object an alert is about (its subject, as
// the board links it), each where all the alerts share it: alertname first, namespace next, then by
// name. The labels of the scraped target (an exporter's instance, pod, job, endpoint) are left out:
// they change when the exporter restarts, and a silence of them would stop muting. It sends no
// request.
func (s *Session) SilenceDraft(contextID, card, fingerprint string) (SilenceDraft, error) {
	b, snap, err := s.alertSnapshot(contextID)
	if err != nil {
		return SilenceDraft{}, err
	}
	out := SilenceDraft{Context: b.Context, Duration: FormatSilenceDuration(DefaultSilenceDuration), CreatedBy: s.silenceAuthor(),
		Matchers: []SilenceMatcher{}}
	if snap.Target != (alerting.Target{}) {
		out.Where = snap.Target.Where()
	}
	byFP := map[string]alerting.Alert{}
	for _, set := range [][]alerting.Alert{snap.Alerts, snap.Muted} {
		for _, a := range set {
			byFP[a.Fingerprint] = a
		}
	}
	var labels []map[string]string
	switch {
	case fingerprint != "":
		a, ok := byFP[fingerprint]
		if !ok {
			return SilenceDraft{}, fmt.Errorf("no alert %q: it may have resolved", fingerprint)
		}
		labels = append(labels, a.Labels)
	case card != "":
		board, err := s.AlertBoard(b.Context)
		if err != nil {
			return SilenceDraft{}, err
		}
		for _, g := range board.Groups {
			for _, c := range g.Cards {
				if c.Key != card {
					continue
				}
				for _, it := range c.Alerts {
					if a, ok := byFP[it.Fingerprint]; ok {
						labels = append(labels, a.Labels)
					}
				}
			}
		}
		if len(labels) == 0 {
			return SilenceDraft{}, fmt.Errorf("no card %q on the board: its alerts may have resolved, or the search hides them", card)
		}
	}
	keep := silenceDraftKeys(labels)
	for _, name := range sharedLabels(labels) {
		if keep[name] {
			out.Matchers = append(out.Matchers, SilenceMatcher{Name: name, Op: alerting.OpEqual, Value: labels[0][name]})
		}
	}
	ms, _ := Matchers(out.Matchers)
	out.Matches = matchesOf(b, snap, ms)
	return out, nil
}

// silenceDraftKeys are the labels a draft's matchers may take: alertname, and of every alert the namespace
// and the labels that name its subject (alertSubject), each as the alert carries it (exported_<l>
// where kube-state-metrics' scrape renamed it, as cardLabels reads it).
func silenceDraftKeys(sets []map[string]string) map[string]bool {
	keep := map[string]bool{"alertname": true}
	for _, raw := range sets {
		cl := cardLabels(raw)
		for _, k := range append(subjectKeys(cl), namespaceKey) {
			if _, ok := raw["exported_"+k]; ok {
				k = "exported_" + k
			}
			keep[k] = true
		}
	}
	return keep
}

// subjectKeys are the labels alertSubject reads the subject of an alert from, in its order of
// preference: nil when it finds none.
func subjectKeys(labels map[string]string) []string {
	ns := labels[namespaceKey]
	if labels["customresource_kind"] != "" && labels[labelCRName] != "" {
		return []string{"customresource_group", "customresource_kind", labelCRName}
	}
	for _, sub := range alertSubjects {
		if labels[sub.label] != "" && (ns != "" || sub.cluster) {
			return []string{sub.label}
		}
	}
	if labels["secret_name"] != "" && labels["secret_namespace"] != "" {
		return []string{"secret_name", "secret_namespace"}
	}
	if pod := labels[labelPod]; pod != "" && ns != "" && !scrapedPod(pod, labels) {
		return []string{labelPod}
	}
	return nil
}

// sharedLabels are the labels all of sets have with one value: alertname first, namespace next,
// then by name.
func sharedLabels(sets []map[string]string) []string {
	if len(sets) == 0 {
		return nil
	}
	var names []string
	for k, v := range sets[0] {
		if v == "" {
			continue
		}
		shared := true
		for _, l := range sets[1:] {
			if w, ok := l[k]; !ok || w != v {
				shared = false
				break
			}
		}
		if shared {
			names = append(names, k)
		}
	}
	rank := func(n string) int {
		switch n {
		case "alertname":
			return 0
		case namespaceKey:
			return 1
		}
		return 2
	}
	slices.SortFunc(names, func(a, b string) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	return names
}

// silenceAuthor is the author the form fills in: the one the user last created a silence with in
// the window (Preferences.SilenceAuthor), else the computer's user's full name, else their login.
func (s *Session) silenceAuthor() string {
	if p, err := s.Preferences(); err == nil && p.SilenceAuthor != "" {
		return p.SilenceAuthor
	}
	if u, err := user.Current(); err == nil {
		if name := strings.TrimSpace(u.Name); name != "" {
			return name
		}
		return u.Username
	}
	return ""
}

// SilenceRequest is a silence to create: its matchers, how long from now, who and why.
type SilenceRequest struct {
	Matchers  []alerting.Matcher
	Duration  time.Duration
	CreatedBy string
	Comment   string
}

// SilenceCreated is a silence Cogpit created.
type SilenceCreated struct {
	ID       string    `json:"id"`
	Where    string    `json:"where"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	// Mutes counts the board's alerts it matched when it was created.
	Mutes int `json:"mutes"`
}

// CreateSilence creates a silence in a connected cluster's Alertmanager ("" the shown one), the one
// whose alerts the board shows, from now for r.Duration. remember keeps its author for the next
// form (the user's own silence in the window). The alerts it mutes leave the board at the poll that
// follows at once.
func (s *Session) CreateSilence(ctx context.Context, contextID string, r SilenceRequest, remember bool) (SilenceCreated, error) {
	b, snap, err := s.alertSnapshot(contextID)
	if err != nil {
		return SilenceCreated{}, err
	}
	p := b.alerts.current()
	if p == nil {
		return SilenceCreated{}, alerting.ErrNoAlertmanager
	}
	if r.Duration <= 0 || r.Duration > maxSilenceDuration {
		return SilenceCreated{}, fmt.Errorf("a silence lasts more than nothing and at most %s", FormatSilenceDuration(maxSilenceDuration))
	}
	now := time.Now()
	n := alerting.NewSilence{Matchers: r.Matchers, StartsAt: now, EndsAt: now.Add(r.Duration),
		CreatedBy: strings.TrimSpace(r.CreatedBy), Comment: strings.TrimSpace(r.Comment)}
	mutes := matchesOf(b, snap, r.Matchers).Shown
	id, t, err := p.CreateSilence(ctx, n)
	if err != nil {
		return SilenceCreated{}, err
	}
	if remember {
		if _, err := s.ChangePreferences(func(p *Preferences) error { p.SilenceAuthor = n.CreatedBy; return nil }); err != nil &&
			!errors.Is(err, ErrNoPreferences) {
			s.log.Warn("silences: the author is not kept", "err", err)
		}
	}
	return SilenceCreated{ID: id, Where: t.Where(), StartsAt: n.StartsAt, EndsAt: n.EndsAt, Mutes: mutes}, nil
}

// SilenceExpired is a silence Cogpit expired.
type SilenceExpired struct {
	ID    string `json:"id"`
	Where string `json:"where"`
}

// ExpireSilence expires a silence in a connected cluster's Alertmanager ("" the shown one): it ends
// now, and the alerts it muted come back on the board at the poll that follows at once.
func (s *Session) ExpireSilence(ctx context.Context, contextID, id string) (SilenceExpired, error) {
	b, _, err := s.alertSnapshot(contextID)
	if err != nil {
		return SilenceExpired{}, err
	}
	p := b.alerts.current()
	if p == nil {
		return SilenceExpired{}, alerting.ErrNoAlertmanager
	}
	t, err := p.ExpireSilence(ctx, id)
	if err != nil {
		return SilenceExpired{}, err
	}
	return SilenceExpired{ID: id, Where: t.Where()}, nil
}

// KnownSilence returns a silence of a connected cluster's Alertmanager ("" the shown one) as the
// last answer listed it, for the confirmation that names it; false when Cogpit has not seen it.
func (s *Session) KnownSilence(contextID, id string) (alerting.Silence, bool) {
	_, snap, err := s.alertSnapshot(contextID)
	if err != nil {
		return alerting.Silence{}, false
	}
	for _, x := range snap.Silences {
		if x.ID == id {
			return x, true
		}
	}
	return alerting.Silence{}, false
}

// ReadSilences makes the silences of a connected cluster's Alertmanager ("" the shown one) current
// for an agent: while the silences page shows them they are (each poll asks), else one GET asks for
// them now when the last answer is older than the poll interval.
func (s *Session) ReadSilences(ctx context.Context, contextID string) error {
	b, snap, err := s.alertSnapshot(contextID)
	if err != nil {
		return err
	}
	p := b.alerts.current()
	if p == nil || !snap.Answered() {
		return nil // nothing answers: the model says why
	}
	interval := s.opts.Alerts.Interval
	if interval <= 0 {
		interval = alerting.DefaultInterval
	}
	if u := s.UIState(); u.View == AlertSilences && u.Context == b.Context || time.Since(snap.SilencesAsOf) < interval {
		return nil
	}
	return p.ReadSilences(ctx)
}

// AlertmanagerRef is the Service of a connected cluster's Alertmanager ("" the shown one), the
// object a silence's confirmation names.
func (s *Session) AlertmanagerRef(contextID string) ([]model.ObjectRef, error) {
	b, snap, err := s.alertSnapshot(contextID)
	if err != nil {
		return nil, err
	}
	if snap.Target == (alerting.Target{}) {
		return nil, alerting.ErrNoAlertmanager
	}
	return []model.ObjectRef{{Context: b.Context, Kind: kindServices, Namespace: snap.Target.Namespace, Name: snap.Target.Service}}, nil
}

// AlertmanagerWritable says why the user may not create (verb "create") or expire ("delete") a
// silence in a connected cluster's Alertmanager ("" the shown one): the access reviews deny that
// verb on services/proxy in its namespace. Nil while they allow it or do not know yet: the API
// server decides.
func (s *Session) AlertmanagerWritable(contextID, verb string) error {
	b, snap, err := s.alertSnapshot(contextID)
	if err != nil {
		return err
	}
	if snap.Target == (alerting.Target{}) || b.Access == nil {
		return nil
	}
	v := Verb{Verb: verb, Resource: "services", Subresource: "proxy"}
	if allowed, known := b.Access.Allowed(snap.Target.Namespace, v); known && !allowed {
		return fmt.Errorf("%w: the current user may not %s services/proxy in namespace %s", ErrNotPermitted, verb, snap.Target.Namespace)
	}
	return nil
}

// ParseSilenceDuration reads a silence's length: Go's durations ("90m", "2h30m") and days and
// weeks ("1d", "2d12h", "1w").
func ParseSilenceDuration(text string) (time.Duration, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return 0, errors.New("no duration")
	}
	var total time.Duration
	rest := text
	for rest != "" {
		i := strings.IndexFunc(rest, func(r rune) bool { return !unicode.IsDigit(r) && r != '.' })
		if i <= 0 {
			return 0, fmt.Errorf("duration %q: a number and a unit (w, d, h, m), such as 2h or 1d", text)
		}
		j := strings.IndexFunc(rest[i:], func(r rune) bool { return unicode.IsDigit(r) })
		if j < 0 {
			j = len(rest) - i
		}
		num, unit := rest[:i], rest[i:i+j]
		rest = rest[i+j:]
		var d time.Duration
		switch unit {
		case "w":
			d = 7 * 24 * time.Hour
		case "d":
			d = 24 * time.Hour
		default:
			parsed, err := time.ParseDuration(num + unit)
			if err != nil {
				return 0, fmt.Errorf("duration %q: a number and a unit (w, d, h, m), such as 2h or 1d", text)
			}
			total += parsed
			continue
		}
		n, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, fmt.Errorf("duration %q: %q is no number", text, num)
		}
		total += time.Duration(n * float64(d))
	}
	if total <= 0 {
		return 0, fmt.Errorf("duration %q: longer than nothing", text)
	}
	return total, nil
}

// FormatSilenceDuration writes a silence's length in the form's units: "2h", "1d", "1w2d", "1h30m".
func FormatSilenceDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	var b strings.Builder
	for _, u := range []struct {
		d    time.Duration
		unit string
	}{{7 * 24 * time.Hour, "w"}, {24 * time.Hour, "d"}, {time.Hour, "h"}, {time.Minute, "m"}} {
		if n := d / u.d; n > 0 {
			b.WriteString(strconv.FormatInt(int64(n), 10) + u.unit)
			d -= n * u.d
		}
	}
	if b.Len() == 0 {
		return "1m"
	}
	return b.String()
}
