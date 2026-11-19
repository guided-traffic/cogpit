package kinds

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// An AppProject's sync windows (A33, owner; H-67): a sync Cogpit writes is a manual sync, and it
// respects the windows as Argo CD's own check of a manual sync does (SyncWindows.Matches, then
// CanSync(isManual) in Argo CD 3.5.5's pkg/apis/application/v1alpha1/types.go, read; checked
// against a live Argo CD controller on a real cluster the same night): the windows that match the
// Application (its name, its destination's cluster by server or name, its destination namespace, by
// Argo CD's glob, whose * also matches "/"; with andOperator every list given must match) decide.
// An active deny window blocks unless every active deny window allows manual syncs (manualSync);
// else an active allow window lets it through; else, when allow windows match but none is active,
// it is blocked unless every one of them allows manual syncs; without matching windows it goes
// through. A window is active from each time its cron schedule (five fields, no descriptors, as
// Argo CD parses it) fires, in its time zone (UTC without one), for its duration. A window that
// cannot be read blocks (Argo CD refuses the sync then too). Argo CD's RBAC is not emulated. Pure:
// no I/O.

// The kinds of a sync window.
const (
	argoWindowAllow = "allow"
	argoWindowDeny  = "deny"
)

// argoChainBound bounds how many consecutive openings of a window the end of an open window is
// followed through: a window that opens again before it closes (every minute, for an hour) stays
// open for as long as its schedule fires.
const argoChainBound = 1440

// ArgoWindow is one sync window of an AppProject as the check reads it.
type ArgoWindow struct {
	Kind         string // "allow" or "deny"
	Schedule     string
	Duration     time.Duration
	TimeZone     *time.Location
	Applications []string
	Namespaces   []string
	Clusters     []string
	ManualSync   bool
	AndOperator  bool
	sched        cron.Schedule
}

// argoWindowParser reads a window's schedule as Argo CD does: five fields; Argo CD's parser takes
// no descriptor (@daily), so neither does Cogpit's.
var argoWindowParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ArgoWindows reads an AppProject's spec.syncWindows. A window that cannot be read (its schedule,
// duration or time zone) is returned as an error: the caller does not guess.
func ArgoWindows(project *unstructured.Unstructured) ([]ArgoWindow, error) {
	if project == nil {
		return nil, nil
	}
	raw, _, _ := unstructured.NestedSlice(project.Object, "spec", "syncWindows")
	out := make([]ArgoWindow, 0, len(raw))
	for i, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("sync window %d is not an object", i+1)
		}
		w := ArgoWindow{
			Kind: argoStr(m["kind"]), Schedule: argoStr(m["schedule"]), ManualSync: m["manualSync"] == true, AndOperator: m["andOperator"] == true,
			Applications: argoStrs(m["applications"]), Namespaces: argoStrs(m["namespaces"]), Clusters: argoStrs(m["clusters"]), TimeZone: time.UTC,
		}
		if w.Kind != argoWindowAllow && w.Kind != argoWindowDeny {
			return nil, fmt.Errorf("sync window %d: kind %q is neither allow nor deny", i+1, w.Kind)
		}
		var err error
		if w.sched, err = argoWindowParser.Parse(w.Schedule); err != nil {
			return nil, fmt.Errorf("sync window %d: schedule %q: %w", i+1, w.Schedule, err)
		}
		if w.Duration, err = time.ParseDuration(argoStr(m["duration"])); err != nil || w.Duration <= 0 {
			return nil, fmt.Errorf("sync window %d: duration %q is not a duration", i+1, argoStr(m["duration"]))
		}
		if tz := argoStr(m["timeZone"]); tz != "" {
			if w.TimeZone, err = time.LoadLocation(tz); err != nil {
				return nil, fmt.Errorf("sync window %d: time zone %q: %w", i+1, tz, err)
			}
		}
		out = append(out, w)
	}
	return out, nil
}

func argoStr(v any) string { s, _ := v.(string); return s }

func argoStrs(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, e := range l {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Active reports whether the window is open at now, and until when: the end of its opening that
// covers now, followed through the openings that start before it ends. bounded is false when the
// window opened again each time before it closed for argoChainBound openings (it stays open as
// long as its schedule fires), or never closes. As Argo CD: open when the first firing after
// now-duration is before
// now; a schedule that never fires (February 30th) leaves its window open, as Argo CD's check
// does.
func (w ArgoWindow) Active(now time.Time) (open bool, until time.Time, bounded bool) {
	local := now.In(w.TimeZone)
	start := w.sched.Next(local.Add(-w.Duration))
	if start.IsZero() {
		return true, time.Time{}, false
	}
	if !start.Before(local) {
		return false, time.Time{}, false
	}
	end := start.Add(w.Duration)
	for range argoChainBound {
		next := w.sched.Next(start)
		if next.IsZero() || next.After(end) {
			return true, end, true
		}
		start = next
		if e := next.Add(w.Duration); e.After(end) {
			end = e
		}
	}
	return true, end, false
}

// Matches reports whether the window applies to an Application named app with the destination
// namespace ns and cluster (its server URL and its name), by Argo CD's glob.
func (w ArgoWindow) Matches(app, ns string, clusters []string) bool {
	lists := []struct {
		patterns []string
		values   []string
	}{{w.Applications, []string{app}}, {w.Namespaces, []string{ns}}, {w.Clusters, clusters}}
	given, matched := 0, 0
	for _, l := range lists {
		if len(l.patterns) == 0 {
			continue
		}
		given++
		if globAny(l.patterns, l.values) {
			matched++
		}
	}
	if w.AndOperator {
		return given > 0 && matched == given
	}
	return matched > 0
}

func globAny(patterns, values []string) bool {
	for _, p := range patterns {
		for _, v := range values {
			if v != "" && ArgoGlobMatch(p, v) {
				return true
			}
		}
	}
	return false
}

// ArgoGlobMatch matches a value against a pattern as Argo CD's glob does (gobwas/glob without
// separators): * and ** any characters, "/" included (a cluster's https://… server); ? one
// character; [abc], [a-z] and [!a-z] a class; {a,b} alternatives; \ escapes. A pattern that does not
// compile matches nothing, as in Argo CD.
func ArgoGlobMatch(pattern, value string) bool {
	re, err := argoGlobRegexp(pattern)
	return err == nil && re.MatchString(value)
}

// argoGlobRegexp translates a glob pattern into an anchored regular expression.
func argoGlobRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString(`^(?s:`)
	rs := []rune(pattern)
	depth := 0
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; r {
		case '\\':
			if i+1 >= len(rs) {
				return nil, fmt.Errorf("pattern %q ends in an escape", pattern)
			}
			i++
			b.WriteString(regexp.QuoteMeta(string(rs[i])))
		case '*':
			for i+1 < len(rs) && rs[i+1] == '*' {
				i++
			}
			b.WriteString(`.*`)
		case '?':
			b.WriteString(`.`)
		case '[':
			end := i + 1
			for end < len(rs) && rs[end] != ']' {
				end++
			}
			if end >= len(rs) || end == i+1 {
				return nil, fmt.Errorf("pattern %q has an unclosed or empty class", pattern)
			}
			b.WriteString(argoGlobClass(rs[i+1 : end]))
			i = end
		case '{':
			depth++
			b.WriteString(`(?:`)
		case '}':
			if depth == 0 {
				b.WriteString(regexp.QuoteMeta("}"))
				continue
			}
			depth--
			b.WriteString(`)`)
		case ',':
			if depth > 0 {
				b.WriteString(`|`)
				continue
			}
			b.WriteString(",")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("pattern %q has an unclosed {", pattern)
	}
	b.WriteString(`)$`)
	return regexp.Compile(b.String())
}

// argoGlobClass translates the inside of a glob class: ! first negates, a - between two characters
// is a range, every other character is itself.
func argoGlobClass(in []rune) string {
	var b strings.Builder
	b.WriteString("[")
	if in[0] == '!' && len(in) > 1 {
		b.WriteString("^")
		in = in[1:]
	}
	for j, r := range in {
		if r == '-' && j > 0 && j < len(in)-1 {
			b.WriteString("-")
			continue
		}
		fmt.Fprintf(&b, `\x{%x}`, r)
	}
	b.WriteString("]")
	return b.String()
}

// String names the window for a message.
func (w ArgoWindow) String() string {
	return fmt.Sprintf("%s window %q for %s (%s)", w.Kind, w.Schedule, w.Duration, w.TimeZone)
}

// ArgoSyncBlocked returns why the AppProject's sync windows block a manual sync of app at now, ""
// when they do not; an error when a window cannot be read.
func ArgoSyncBlocked(app, project *unstructured.Unstructured, now time.Time) (string, error) {
	windows, err := ArgoWindows(project)
	if err != nil || len(windows) == 0 {
		return "", err
	}
	name := app.GetName()
	ns, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "namespace")
	server, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "server")
	dest, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "name")
	clusters := []string{server, dest}
	var activeDeny, activeAllow, inactiveAllow []ArgoWindow
	var denyUntil time.Time
	denyBounded := true
	for _, w := range windows {
		if !w.Matches(name, ns, clusters) {
			continue
		}
		on, until, bounded := w.Active(now)
		switch {
		case w.Kind == argoWindowDeny && on:
			activeDeny = append(activeDeny, w)
			denyBounded = denyBounded && bounded
			if until.After(denyUntil) {
				denyUntil = until
			}
		case w.Kind == argoWindowAllow && on:
			activeAllow = append(activeAllow, w)
		case w.Kind == argoWindowAllow:
			inactiveAllow = append(inactiveAllow, w)
		}
	}
	if len(activeDeny) > 0 {
		if allManual(activeDeny) { // Argo CD: every active deny window must allow manual syncs
			return "", nil
		}
		w := activeDeny[0]
		for _, d := range activeDeny {
			if !d.ManualSync {
				w = d // name a window that refuses
				break
			}
		}
		if !denyBounded {
			return fmt.Sprintf("the AppProject's %s is open, and its schedule keeps it open: Argo CD refuses syncs while it is", w), nil
		}
		return fmt.Sprintf("the AppProject's %s is open: Argo CD refuses syncs until %s", w,
			denyUntil.UTC().Format(time.RFC3339)), nil
	}
	if len(activeAllow) > 0 || len(inactiveAllow) == 0 {
		return "", nil
	}
	if allManual(inactiveAllow) { // Argo CD: every inactive allow window must allow manual syncs
		return "", nil
	}
	return fmt.Sprintf("no allow window of the AppProject is open (%s): Argo CD refuses syncs outside them",
		strings.TrimSpace(inactiveAllow[0].String())), nil
}

// allManual reports whether every window allows manual syncs.
func allManual(windows []ArgoWindow) bool {
	for _, w := range windows {
		if !w.ManualSync {
			return false
		}
	}
	return len(windows) > 0
}
