package engine

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The fold of the cluster Overview's issues: what cluster.overview returns unless every issue is
// asked for. A cluster's issues are mostly one cause repeated over the pods of one workload and
// over the events about each of them, and a model reads every byte of a result; the fold says the
// same in a bounded size:
//
//   - a row per subject: the workload that controls the affected pods where the cache holds that
//     owner under the UID the pod's owner reference names (foldOwners), else the object itself;
//     with how many of its objects have an issue, its worst severity, its state and its most
//     common event;
//   - a table of causes: a state's text or an event's reason and message, the message with what
//     differs from object to object folded (causeMessage), and how many subjects, objects and
//     issues share it;
//   - the exact number of issues, by severity, and of subjects, whatever part the tables show.
//
// The order is Cogpit's: severity, a state before an event, the number of distinct objects, an
// object Cogpit has a view for before one only an event names, then names and texts. An event's
// count and its times are never a sort key, so an event cannot buy a place by claiming to be
// frequent or recent. What a table leaves out it counts and says how to read (FoldTable.More); a
// subject's issues one by one, their messages whole, are IssueQuery.Subject.
//
// An event's writer chooses the object it is about. Such an object is counted as the event names
// it, cut to the length Kubernetes allows a name (boundedObject), and counts for a cached object
// only where the event lies in that object's namespace: an event in one namespace about an object
// of another is a subject of its own, in the event's namespace (objectOf).

// The fold's bounds.
const (
	// FoldDefaultLimit and FoldMaxLimit bound the rows of a page: subjects, or a subject's issues.
	FoldDefaultLimit = 20
	FoldMaxLimit     = 200
	// FoldMaxCauses is how many causes the fold lists.
	FoldMaxCauses = 20
	// FoldMessageBytes cuts a folded message and a state, FoldReasonBytes a reason.
	FoldMessageBytes = 160
	FoldReasonBytes  = 64
	// FoldNameBytes cuts an object's kind, namespace and name: Kubernetes allows a name 253 bytes,
	// so a longer one, which only an event can carry, names no object.
	FoldNameBytes = 253
	// FoldMemberMessageBytes cuts the message of one of a subject's issues: longer than an event
	// recorder writes one, so that such a message is whole.
	FoldMemberMessageBytes = 4 << 10
	// FoldBytes bounds the fold as JSON: a page gives up rows to stay under it. With the rest of
	// the Overview, without its charts' points, the result stays under 24 KiB.
	FoldBytes = 20 << 10
)

// reasonShortOfPods is the cause of a workload's issue; its message counts the pods.
const reasonShortOfPods = "pods not ready"

// foldUnknownKind follows, in a row's kind, an API kind an event wrote without a capital letter: a
// kind key has none either, and a row's kind must never read as the key of another object.
const foldUnknownKind = " (unknown kind)"

// foldSubjectBytes is the longest kind, namespace or name a row shows.
const foldSubjectBytes = FoldNameBytes + len("…") + len(foldUnknownKind)

// The columns the fold's tables add to a RowTable's.
const (
	foldColSource      = "source"
	foldColReason      = "reason"
	foldColMessage     = "message"
	foldColState       = "state"
	foldColObjects     = "objects"
	foldColIssues      = "issues"
	foldColSubjects    = "subjects"
	foldColOtherCauses = "otherCauses"
	foldColSince       = "since"
	foldColLast        = "last"
	foldColCount       = "count"
)

// IssueQuery says which issues a fold is of and which part of it to return.
type IssueQuery struct {
	// Severity keeps the issues of one severity: "error" or "warning"; empty: both.
	Severity string
	// Sources keeps the issues of these sources: IssueStatus, IssueEvent; empty: both.
	Sources []string
	// Reason keeps the issues of one cause, as the causes' reason column names it, whatever its case.
	Reason string
	// Subject returns one subject's issues one by one in place of the subjects and the causes.
	Subject *IssueSubject
	// Limit is the page's rows (0: FoldDefaultLimit, at most FoldMaxLimit), Offset the rows before it.
	Limit, Offset int
}

// IssueSubject names a subject as a row of the fold's subjects does.
type IssueSubject struct {
	Kind      string `json:"kind" jsonschema:"the subject's kind as its row has it"`
	Namespace string `json:"namespace,omitempty" jsonschema:"the subject's namespace; empty for a cluster-scoped one"`
	Name      string `json:"name" jsonschema:"the subject's name"`
}

// validate refuses a query the fold cannot answer.
func (q IssueQuery) validate() error {
	switch model.Status(q.Severity) {
	case "", model.StatusError, model.StatusWarning:
	default:
		return fmt.Errorf("severity is error or warning, not %q", q.Severity)
	}
	for _, s := range q.Sources {
		if s != IssueStatus && s != IssueEvent {
			return fmt.Errorf("a source is status or event, not %q", s)
		}
	}
	if q.Limit < 0 || q.Offset < 0 {
		return errors.New("limit and offset are not negative")
	}
	if s := q.Subject; s != nil {
		if s.Kind == "" || s.Name == "" {
			return errors.New("a subject needs the kind and the name of its row")
		}
		if max(len(s.Kind), len(s.Namespace), len(s.Name)) > foldSubjectBytes {
			return errors.New("a subject's kind, namespace and name are those of its row: no row has one this long")
		}
	}
	return nil
}

func (q IssueQuery) limit() int {
	switch {
	case q.Limit <= 0:
		return FoldDefaultLimit
	case q.Limit > FoldMaxLimit:
		return FoldMaxLimit
	}
	return q.Limit
}

// keeps reports whether an issue passes the query's severity and sources.
func (q IssueQuery) keeps(is *ClusterIssue) bool {
	if q.Severity != "" && string(is.Severity) != q.Severity {
		return false
	}
	return len(q.Sources) == 0 || slices.Contains(q.Sources, is.Source)
}

// IssueFold is the Overview's issues folded. Its numbers count every issue the query selects in the
// namespaces asked for, whatever part its tables show.
type IssueFold struct {
	// Issues is the number of issues, Error and Warning the same by severity.
	Issues  int `json:"issues"`
	Error   int `json:"error"`
	Warning int `json:"warning"`
	// Subjects is the number of subjects the issues are about.
	Subjects int `json:"subjects"`
	// Causes are the distinct causes, the gravest first: level (the worst severity), source, reason
	// (a state's text or an event's reason), message (an event's, folded), and how many subjects,
	// objects and issues share it.
	Causes *FoldTable `json:"causes,omitempty"`
	// BySubject is a row per subject, the gravest first: kind, namespace and name (a reference
	// where the kind is a kind key; an API kind is an object only an event names: one without a
	// view, or one the event names from another namespace), level, how many of its objects have an
	// issue and how many issues those are, its state, its most common event (reason, message), how
	// many causes more it has, and since when its state is so.
	BySubject *FoldTable `json:"bySubject,omitempty"`
	// Subject and Members answer IssueQuery.Subject: the subject asked for and its issues one by
	// one, each with its object, level, source and whole message; the issues of one object that
	// read alike are one row with how many they are (issues), how often their events happened
	// (count) and when the first and the last of them was (since, last).
	Subject *IssueSubject `json:"subject,omitempty"`
	Members *FoldTable    `json:"members,omitempty"`
	// OwnersUnread names the kinds between a pod and its workload (replicasets, jobs, cronjobs) the
	// fold needed and could not read: their list did not arrive in time, or the user may not list
	// them. The pods such an owner controls are then subjects of their own.
	OwnersUnread []string `json:"ownersUnread,omitempty"`
}

// FoldTable is a page of one of the fold's tables.
type FoldTable struct {
	// Total is every row the table has, Offset the rows before the ones here.
	Total  int `json:"total"`
	Offset int `json:"offset,omitempty"`
	// More counts the rows after the ones here and says how to read them; empty at the end.
	More string `json:"more,omitempty"`
	RowTable
}

// issueController is an object's controller among its owner references.
type issueController struct{ apiVersion, kind, name, uid string }

// cachedOwner is what the cache holds of an object the fold asks about.
type cachedOwner struct {
	uid        string
	controller *issueController
}

// ownerLookup answers the fold's questions about who controls an object, from the cache.
type ownerLookup interface {
	// kindOf is the kind key of an owner reference's API version and kind; false for a kind the
	// cluster does not serve and for a cluster-scoped one (a node controls its static pods, and
	// nobody restarts a node to fix one).
	kindOf(apiVersion, kind string) (string, bool)
	// object is the object of a kind key as the cache holds it; false when it holds none.
	object(kind, namespace, name string) (cachedOwner, bool)
	// unread names the kinds object was asked for and could not read, sorted.
	unread() []string
}

// foldObject is an object as the fold counts it: by its kind key, or, named, as an event names an
// object the fold has no cached object for (kind is then the API kind the event wrote). The two are
// never the same object, whatever an event writes as a kind.
type foldObject struct {
	kind, namespace, name string
	named                 bool
}

// compare orders objects: one with a kind key before one an event only names (what a writer of
// events calls its object must not decide who comes first), then by kind, namespace and name.
func (o foldObject) compare(p foldObject) int {
	named := func(o foldObject) int {
		if o.named {
			return 1
		}
		return 0
	}
	return cmp.Or(cmp.Compare(named(o), named(p)),
		strings.Compare(o.kind, p.kind), strings.Compare(o.namespace, p.namespace), strings.Compare(o.name, p.name))
}

// kindCell is the object's kind as a row shows it: the kind key, or the API kind an event wrote,
// marked where it could be read as a kind key.
func (o foldObject) kindCell() string {
	if o.named && strings.ToLower(o.kind) == o.kind {
		return o.kind + foldUnknownKind
	}
	return o.kind
}

// ref is the object as a row's reference columns show it.
func (o foldObject) ref() model.ObjectRef {
	return model.ObjectRef{Kind: o.kindCell(), Namespace: o.namespace, Name: o.name}
}

// is reports whether the object is the one a row's kind, namespace and name say.
func (o foldObject) is(s *IssueSubject) bool {
	return o.namespace == s.Namespace && o.name == s.Name && o.kindCell() == s.Kind
}

// foldCause is a cause: an issue's source, its reason and its folded message.
type foldCause struct{ source, reason, message string }

// foldedIssue is an issue with what the fold made of it: its object, its subject, its cause, and
// the text a subject's row shows of it (a state's text, an event's message folded but for its
// image).
type foldedIssue struct {
	is      *ClusterIssue
	object  foldObject
	subject foldObject
	cause   foldCause
	shown   string
}

// folder folds one result; its maps keep what repeats from issue to issue.
type folder struct {
	look     ownerLookup
	subjects map[foldObject]foldObject
	messages map[[2]string][2]string // by an event's message and object name: causeMessage's two
}

// The kinds whose controller the fold follows: a pod's, and those of the objects that stand between
// a pod and what an admin acts on (a Deployment's Replica Set, a Cron Job's Job).
var (
	foldFollows      = map[string]bool{logKindPods: true, logKindReplicaSets: true, logKindJobs: true}
	foldPassesBeyond = map[string]bool{logKindReplicaSets: true, logKindJobs: true}
)

// foldIssues folds issues as q asks, under budget bytes of JSON; look answers who controls a pod
// (nil: every object is its own subject). It reads the issues in any order and returns the same
// fold.
func foldIssues(issues []ClusterIssue, look ownerLookup, q IssueQuery, budget int) *IssueFold {
	f := &folder{look: look, subjects: map[foldObject]foldObject{}, messages: map[[2]string][2]string{}}
	var kept []foldedIssue
	subjects := map[foldObject]struct{}{}
	out := &IssueFold{}
	for i := range issues {
		is := &issues[i]
		if !q.keeps(is) {
			continue
		}
		fi := foldedIssue{is: is, object: objectOf(is)}
		fi.cause, fi.shown = f.causeOf(is)
		if q.Reason != "" && !strings.EqualFold(q.Reason, fi.cause.reason) {
			continue
		}
		fi.subject = f.subjectOf(is, fi.object)
		if q.Subject != nil && !fi.subject.is(q.Subject) {
			continue
		}
		kept = append(kept, fi)
		subjects[fi.subject] = struct{}{}
		out.Issues++
		if is.Severity == model.StatusError {
			out.Error++
		} else {
			out.Warning++
		}
	}
	out.Subjects = len(subjects)
	if look != nil {
		out.OwnersUnread = look.unread()
	}
	if q.Subject != nil {
		out.Subject = q.Subject
		fitFold(out, nil, memberRows(kept), memberTitles, q, budget)
		return out
	}
	fitFold(out, causeRows(kept), subjectRows(kept), subjectTitles, q, budget)
	return out
}

// objectOf is the object an issue is about: its reference. An event's is the object the event
// names, by its API kind and in the event's own namespace, where the fold must not take the
// reference for a cached object: an event about an object without a view (the reference is then the
// event), and an event that lies in another namespace than the object it names, which no event
// recorder writes and which would let whoever may write events in one namespace add to the rows of
// every other.
func objectOf(is *ClusterIssue) foldObject {
	o := foldObject{kind: is.Ref.Kind, namespace: is.Ref.Namespace, name: is.Ref.Name}
	if is.Source == IssueEvent {
		switch {
		case is.Ref.Kind == eventsKind:
			o = foldObject{kind: is.Kind, namespace: is.Ref.Namespace, name: is.Name, named: true}
		case is.eventNamespace != "" && is.Ref.Namespace != "" && is.Ref.Namespace != is.eventNamespace:
			o = foldObject{kind: is.Kind, namespace: is.eventNamespace, name: is.Name, named: true}
		}
	}
	return boundedObject(o)
}

// boundedObject cuts an object's kind, namespace and name to FoldNameBytes, one line each. A cached
// object's are shorter; an event's involved object is as long as its writer made it, and a row is
// read whole.
func boundedObject(o foldObject) foldObject {
	cut := func(s string) string { return cutBytes(oneLine(s), FoldNameBytes) }
	o.kind, o.namespace, o.name = cut(o.kind), cut(o.namespace), cut(o.name)
	return o
}

// subjectOf is the subject an issue counts for: the object itself, or what controls it where the
// cache holds every owner on the way under the UID the reference to it names. An owner reference is
// written by whoever creates the object, so one that names no cached object, or another UID, moves
// nothing.
func (f *folder) subjectOf(is *ClusterIssue, self foldObject) foldObject {
	if s, ok := f.subjects[self]; ok {
		return s
	}
	s := f.owner(is, self)
	f.subjects[self] = s
	return s
}

func (f *folder) owner(is *ClusterIssue, self foldObject) foldObject {
	if f.look == nil || self.namespace == "" || self.named || !foldFollows[self.kind] {
		return self
	}
	ctrl := is.controller
	if ctrl == nil {
		if o, ok := f.look.object(self.kind, self.namespace, self.name); ok {
			ctrl = o.controller
		}
	}
	cur := self
	for hops := 0; ctrl != nil && hops < 2; hops++ {
		key, ok := f.look.kindOf(ctrl.apiVersion, ctrl.kind)
		if !ok {
			break
		}
		o, held := f.look.object(key, self.namespace, ctrl.name)
		if !held || o.uid == "" || o.uid != ctrl.uid {
			break
		}
		cur = foldObject{kind: key, namespace: self.namespace, name: ctrl.name}
		if !foldPassesBeyond[key] {
			break
		}
		ctrl = o.controller
	}
	return cur
}

// causeOf is an issue's cause, a state's reason or an event's reason and its folded message, and
// the text a subject's row shows of the issue.
func (f *folder) causeOf(is *ClusterIssue) (foldCause, string) {
	reason := is.reason
	if is.Source != IssueEvent {
		if reason == "" {
			reason = podStateReason(is.Message)
		}
		return foldCause{source: is.Source, reason: cutBytes(oneLine(reason), FoldReasonBytes)}, is.Message
	}
	text := is.Message
	if reason != "" {
		text = strings.TrimPrefix(strings.TrimPrefix(text, reason), ": ")
	}
	key := [2]string{text, is.Name}
	msg, ok := f.messages[key]
	if !ok {
		msg[0], msg[1] = causeMessage(text, is.Name)
		f.messages[key] = msg
	}
	return foldCause{source: is.Source, reason: cutBytes(oneLine(reason), FoldReasonBytes), message: msg[0]}, msg[1]
}

// podStateReason is a pod's state without its restart count, which differs from pod to pod of one
// workload: "CrashLoopBackOff; 5 restarts" is "CrashLoopBackOff".
func podStateReason(text string) string {
	i := strings.LastIndex(text, "; ")
	if i < 0 {
		return text
	}
	n, word, ok := strings.Cut(text[i+2:], " ")
	if _, err := strconv.Atoi(n); err != nil || !ok || (word != "restart" && word != "restarts") {
		return text
	}
	return text[:i]
}

// What differs from one object's message to another's of the same cause.
var (
	foldUID    = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	foldDigest = regexp.MustCompile(`sha256:[0-9a-f]{64}`)
	foldHash   = regexp.MustCompile(`\b[0-9a-f]{12,}\b`)
)

// causeMessage folds an event's message to what the objects of one cause share, cut to
// FoldMessageBytes: the object's own name is {name}, a UID {uid}, an image digest {digest}, a
// longer run of hexadecimal digits {hash}, the image the message names {image}. Numbers stay: an
// exit code or a port is the cause. It returns the message as a cause has it, and as a subject's
// row has it: there the image stays where the message first names it (which image fails is what
// the row is read for) and is {image} only where the message repeats it, as a kubelet's does twice.
func causeMessage(text, name string) (cause, row string) {
	cause, row = text, text
	if start, end, ok := imageIn(text); ok {
		image := text[start:end]
		cause = strings.ReplaceAll(text, image, "{image}")
		row = text[:end] + strings.ReplaceAll(text[end:], image, "{image}")
	}
	fold := func(text string) string {
		if len(name) >= 3 {
			text = replaceName(text, name, "{name}")
		}
		text = foldUID.ReplaceAllString(text, "{uid}")
		text = foldDigest.ReplaceAllString(text, "{digest}")
		text = foldHash.ReplaceAllString(text, "{hash}")
		return cutMiddle(oneLine(text), FoldMessageBytes)
	}
	return fold(cause), fold(row)
}

// imageIn finds the image a message names: the quoted text after the word image.
func imageIn(text string) (start, end int, ok bool) {
	const lead = `image "`
	i := strings.Index(text, lead)
	if i < 0 {
		return 0, 0, false
	}
	start = i + len(lead)
	n := strings.IndexByte(text[start:], '"')
	if n < 3 {
		return 0, 0, false
	}
	return start, start + n, true
}

// replaceName replaces name in text where it stands as a name of its own: not inside a longer run
// of letters, digits and hyphens.
func replaceName(text, name, with string) string {
	inName := func(c byte) bool {
		return c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	var b strings.Builder
	for {
		i := strings.Index(text, name)
		if i < 0 {
			break
		}
		end := i + len(name)
		if (i > 0 && inName(text[i-1])) || (end < len(text) && inName(text[end])) {
			b.WriteString(text[:end])
		} else {
			b.WriteString(text[:i])
			b.WriteString(with)
		}
		text = text[end:]
	}
	b.WriteString(text)
	return b.String()
}

// cutBytes cuts text to limit bytes at a character's start, with an ellipsis.
func cutBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit] + "…"
}

// cutMiddle cuts text to limit bytes by leaving out its middle: an error wrapped in others says
// what was tried at its start and why it failed at its end.
func cutMiddle(text string, limit int) string {
	const gap = " … "
	if len(text) <= limit || limit <= len(gap) {
		return text
	}
	head := (limit - len(gap)) * 2 / 3
	tail := len(text) - (limit - len(gap) - head)
	for head > 0 && !utf8.RuneStart(text[head]) {
		head--
	}
	for tail < len(text) && !utf8.RuneStart(text[tail]) {
		tail++
	}
	return strings.TrimRight(text[:head], " ") + gap + strings.TrimLeft(text[tail:], " ")
}

func severityRank(s model.Status) int {
	switch s {
	case model.StatusError:
		return 2
	case model.StatusWarning:
		return 1
	}
	return 0
}

// worse returns the graver of two severities.
func worse(a, b model.Status) model.Status {
	if severityRank(b) > severityRank(a) {
		return b
	}
	return a
}

// sourceRank puts a state before an event.
func sourceRank(source string) int {
	if source == IssueStatus {
		return 0
	}
	return 1
}

// causeCount is what a cause adds up to, over every issue or over one subject's.
type causeCount struct {
	cause    foldCause
	level    model.Status
	issues   int
	objects  map[foldObject]struct{}
	subjects map[foldObject]struct{}
	// text is what a subject's row shows of the issues while every one of them shows the same;
	// differs once they do not.
	text    string
	differs bool
}

func (c *causeCount) add(fi *foldedIssue) {
	if c.issues == 0 {
		c.text = fi.shown
	} else if c.text != fi.shown {
		c.differs = true
	}
	c.issues++
	c.level = worse(c.level, fi.is.Severity)
	c.objects[fi.object] = struct{}{}
	c.subjects[fi.subject] = struct{}{}
}

// countCause returns the count of a cause in by, a new one at its first issue.
func countCause(by map[foldCause]*causeCount, c foldCause) *causeCount {
	n := by[c]
	if n == nil {
		n = &causeCount{cause: c, objects: map[foldObject]struct{}{}, subjects: map[foldObject]struct{}{}}
		by[c] = n
	}
	return n
}

// compareCauses is the causes' order: the graver, a state before an event, the one more objects
// have, then by reason and message.
func compareCauses(a, b *causeCount) int {
	return cmp.Or(
		cmp.Compare(severityRank(b.level), severityRank(a.level)),
		cmp.Compare(sourceRank(a.cause.source), sourceRank(b.cause.source)),
		cmp.Compare(len(b.objects), len(a.objects)),
		strings.Compare(a.cause.reason, b.cause.reason),
		strings.Compare(a.cause.message, b.cause.message),
	)
}

var causeTitles = []string{RowLevel, foldColSource, foldColReason, foldColMessage, foldColSubjects, foldColObjects, foldColIssues}

// causeRows are the causes of the issues, in their order.
func causeRows(kept []foldedIssue) [][]string {
	by := map[foldCause]*causeCount{}
	for i := range kept {
		countCause(by, kept[i].cause).add(&kept[i])
	}
	causes := make([]*causeCount, 0, len(by))
	for _, c := range by {
		causes = append(causes, c)
	}
	slices.SortFunc(causes, compareCauses)
	rows := make([][]string, len(causes))
	for i, c := range causes {
		rows[i] = []string{string(c.level), c.cause.source, c.cause.reason, c.cause.message,
			strconv.Itoa(len(c.subjects)), strconv.Itoa(len(c.objects)), strconv.Itoa(c.issues)}
	}
	return rows
}

// subjectCount is what a subject's issues add up to.
type subjectCount struct {
	subject foldObject
	level   model.Status
	issues  int
	objects map[foldObject]struct{}
	causes  map[foldCause]*causeCount
	state   bool      // one of its issues is a state
	since   time.Time // since when the oldest of its states is so
}

var subjectTitles = []string{foldColObjects, foldColIssues, foldColState, foldColReason, foldColMessage, foldColOtherCauses, foldColSince}

// subjectRows are a row per subject: the graver first, one with a state before one with events
// only, the one with more objects, one with a kind key before one an event only names, then by
// kind, namespace and name.
func subjectRows(kept []foldedIssue) []TableRow {
	by := map[foldObject]*subjectCount{}
	for i := range kept {
		fi := &kept[i]
		s := by[fi.subject]
		if s == nil {
			s = &subjectCount{subject: fi.subject, objects: map[foldObject]struct{}{}, causes: map[foldCause]*causeCount{}}
			by[fi.subject] = s
		}
		s.issues++
		s.level = worse(s.level, fi.is.Severity)
		s.objects[fi.object] = struct{}{}
		countCause(s.causes, fi.cause).add(fi)
		if fi.is.Source == IssueStatus {
			s.state = true
			if t := fi.is.Since; !t.IsZero() && (s.since.IsZero() || t.Before(s.since)) {
				s.since = t
			}
		}
	}
	subjects := make([]*subjectCount, 0, len(by))
	for _, s := range by {
		subjects = append(subjects, s)
	}
	slices.SortFunc(subjects, func(a, b *subjectCount) int {
		return cmp.Or(
			cmp.Compare(severityRank(b.level), severityRank(a.level)),
			cmp.Compare(sourceRank(sourceOf(a.state)), sourceRank(sourceOf(b.state))),
			cmp.Compare(len(b.objects), len(a.objects)),
			a.subject.compare(b.subject),
		)
	})
	rows := make([]TableRow, len(subjects))
	for i, s := range subjects {
		rows[i] = s.row()
	}
	return rows
}

func sourceOf(state bool) string {
	if state {
		return IssueStatus
	}
	return IssueEvent
}

// row is the subject's row: its state is the state most of its objects are in (the text itself
// where they all say the same, restart count included), its event the one most of its objects had
// (with its image where they all name the same).
func (s *subjectCount) row() TableRow {
	var state, event *causeCount
	for _, c := range s.causes {
		top := &event
		if c.cause.source == IssueStatus {
			top = &state
		}
		if *top == nil || compareCauses(c, *top) < 0 {
			*top = c
		}
	}
	cells := make([]string, len(subjectTitles))
	cells[0], cells[1] = strconv.Itoa(len(s.objects)), strconv.Itoa(s.issues)
	shown := 0
	if state != nil {
		shown++
		cells[2] = state.cause.reason
		if !state.differs {
			cells[2] = cutMiddle(oneLine(state.text), FoldMessageBytes)
		}
	}
	if event != nil {
		shown++
		cells[3], cells[4] = event.cause.reason, event.cause.message
		if !event.differs {
			cells[4] = event.text
		}
	}
	if other := len(s.causes) - shown; other > 0 {
		cells[5] = strconv.Itoa(other)
	}
	if !s.since.IsZero() {
		cells[6] = s.since.UTC().Format(time.RFC3339)
	}
	return TableRow{Ref: s.subject.ref(), Status: s.level, Cells: cells}
}

var memberTitles = []string{foldColSource, foldColMessage, foldColIssues, foldColCount, foldColSince, foldColLast}

// memberRows are a subject's issues one by one, each with its whole message: the graver first, a
// state before an event, then by object and message. The issues of one object that read alike (the
// same event again and again) are one row that says how many they are, how often their events
// happened in all, and when the first and the last of them was.
func memberRows(kept []foldedIssue) []TableRow {
	type key struct {
		object  foldObject
		level   model.Status
		source  string
		message string
	}
	type member struct {
		key
		issues      int
		count       int64
		first, last time.Time
	}
	by := map[key]*member{}
	for _, fi := range kept {
		k := key{fi.object, fi.is.Severity, fi.is.Source, fi.is.Message}
		m := by[k]
		if m == nil {
			m = &member{key: k}
			by[k] = m
		}
		m.issues++
		m.count += fi.is.count
		if t := fi.is.Since; !t.IsZero() {
			if m.first.IsZero() || t.Before(m.first) {
				m.first = t
			}
			if t.After(m.last) {
				m.last = t
			}
		}
	}
	members := make([]*member, 0, len(by))
	for _, m := range by {
		members = append(members, m)
	}
	slices.SortFunc(members, func(a, b *member) int {
		return cmp.Or(
			cmp.Compare(severityRank(b.level), severityRank(a.level)),
			cmp.Compare(sourceRank(a.source), sourceRank(b.source)),
			a.object.compare(b.object),
			strings.Compare(a.message, b.message),
		)
	})
	rows := make([]TableRow, len(members))
	for i, m := range members {
		cells := []string{m.source, cutBytes(m.message, FoldMemberMessageBytes), strconv.Itoa(m.issues), "", "", ""}
		if m.count > 0 {
			cells[3] = strconv.FormatInt(m.count, 10)
		}
		if !m.first.IsZero() {
			cells[4] = m.first.UTC().Format(time.RFC3339)
		}
		if m.last.After(m.first) {
			cells[5] = m.last.UTC().Format(time.RFC3339)
		}
		rows[i] = TableRow{Ref: m.object.ref(), Status: m.level, Cells: cells}
	}
	return rows
}

// fitFold writes the fold's tables: the causes up to FoldMaxCauses, and the page of rows the query
// asks for. Over budget bytes of JSON the causes give way first, down to a third of the budget, then
// the page keeps as many rows as fit. A page that has rows always returns one, so that following
// More reaches the last row: where the first row alone is too large (a member's message of
// kilobytes of characters JSON escapes) it is returned with its texts cut as a folded message is
// (cutRow), which under FoldBytes fits whatever the row holds.
func fitFold(out *IssueFold, causes [][]string, rows []TableRow, titles []string, q IssueQuery, budget int) {
	total := len(rows)
	offset := min(q.Offset, total)
	page := rows[offset:min(total, offset+q.limit())]
	causeTotal := len(causes)
	causes = causes[:min(causeTotal, FoldMaxCauses)]
	causeTable := func(c int) *FoldTable {
		t := &FoldTable{Total: causeTotal, RowTable: plainTable(causeTitles, causes[:c])}
		if left := causeTotal - c; left > 0 {
			t.More = fmt.Sprintf("%d more: narrow with severity, sources or reason", left)
		}
		return t
	}
	size := func(v any) int {
		raw, _ := json.Marshal(v)
		return len(raw)
	}
	fits := func(n, c int) bool {
		t := &FoldTable{Total: total, Offset: offset, RowTable: rowsTable(titles, page[:n])}
		if left := total - offset - n; left > 0 {
			t.More = fmt.Sprintf("%d more: offset %d", left, offset+n)
		}
		if out.Subject != nil {
			out.Members = t
		} else {
			out.BySubject, out.Causes = t, causeTable(c)
		}
		return size(out) <= budget
	}
	n, c := len(page), len(causes)
	if fits(n, c) {
		return
	}
	c = largest(c, func(c int) bool { return size(causeTable(c)) <= budget/3 })
	n = largest(n, func(n int) bool { return fits(n, c) })
	if n == 0 && len(page) > 0 {
		page = append([]TableRow{cutRow(page[0])}, page[1:]...)
		n = 1
	}
	for c > 0 && !fits(n, c) {
		c--
	}
	fits(n, c)
}

// cutRow is a row with each of its texts cut as a folded message is.
func cutRow(r TableRow) TableRow {
	cells := make([]string, len(r.Cells))
	for i, c := range r.Cells {
		cells[i] = cutMiddle(oneLine(c), FoldMessageBytes)
	}
	r.Cells = cells
	return r
}

// largest returns the largest n up to limit that ok holds for, 0 when it holds for none. ok holds
// for small n and stops holding as n grows, give or take a step (fewer rows are fewer bytes, but for
// the cells that move out of a table's Same), so the search's answer is checked and stepped down.
func largest(limit int, ok func(int) bool) int {
	lo, hi := 0, limit
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if ok(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	for lo > 0 && !ok(lo) {
		lo--
	}
	return lo
}

// rowsTable is the RowTable of rows about objects of several kinds, without the columns no row has
// a cell in.
func rowsTable(titles []string, rows []TableRow) RowTable {
	keep := make([]int, 0, len(titles))
	for i := range titles {
		if slices.ContainsFunc(rows, func(r TableRow) bool { return r.Cells[i] != "" }) {
			keep = append(keep, i)
		}
	}
	if len(keep) < len(titles) {
		kept := make([]string, len(keep))
		for j, i := range keep {
			kept[j] = titles[i]
		}
		titles = kept
		narrowed := make([]TableRow, len(rows))
		for r, row := range rows {
			cells := make([]string, len(keep))
			for j, i := range keep {
				cells[j] = row.Cells[i]
			}
			row.Cells = cells
			narrowed[r] = row
		}
		rows = narrowed
	}
	return NewRowTable("", "", titles, rows)
}

// plainTable is a table of rows that are no objects (the causes) in a RowTable's form: the cells
// every row shares once in Same, a column no row has a cell in left out.
func plainTable(columns []string, rows [][]string) RowTable {
	t := RowTable{Columns: []string{}, Rows: make([][]string, len(rows))}
	var keep []int
	for i, col := range columns {
		if !slices.ContainsFunc(rows, func(r []string) bool { return r[i] != "" }) {
			continue
		}
		if len(rows) > 1 && !slices.ContainsFunc(rows, func(r []string) bool { return r[i] != rows[0][i] }) {
			if t.Same == nil {
				t.Same = map[string]string{}
			}
			t.Same[col] = rows[0][i]
			continue
		}
		keep = append(keep, i)
		t.Columns = append(t.Columns, col)
	}
	for r, row := range rows {
		line := make([]string, len(keep))
		for j, i := range keep {
			line[j] = row[i]
		}
		t.Rows[r] = line
	}
	return t
}
