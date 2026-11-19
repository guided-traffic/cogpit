package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// fakeOwners is the fold's lookup over a map of cached objects, with the workload kinds.
type fakeOwners map[foldObject]cachedOwner

func (f fakeOwners) kindOf(apiVersion, kind string) (string, bool) {
	key, ok := map[string]string{
		"apps/v1 ReplicaSet": "replicasets", "apps/v1 Deployment": "deployments", "apps/v1 StatefulSet": "statefulsets",
		"batch/v1 Job": "jobs", "batch/v1 CronJob": "cronjobs", "example.com/v1 App": "apps.example.com",
	}[apiVersion+" "+kind]
	return key, ok
}

func (f fakeOwners) object(kind, namespace, name string) (cachedOwner, bool) {
	o, ok := f[foldObject{kind: kind, namespace: namespace, name: name}]
	return o, ok
}

func (f fakeOwners) unread() []string { return nil }

// The fixture's workload: the Deployment shop/web, its Replica Set web-7c9, and the reference a pod
// of it carries.
var (
	webRS      = &issueController{apiVersion: "apps/v1", kind: "ReplicaSet", name: "web-7c9", uid: "rs-1"}
	webDeploy  = &issueController{apiVersion: "apps/v1", kind: "Deployment", name: "web", uid: "deploy-1"}
	webOwners  = fakeOwners{{kind: "replicasets", namespace: "shop", name: "web-7c9"}: {uid: "rs-1", controller: webDeploy}, {kind: "deployments", namespace: "shop", name: "web"}: {uid: "deploy-1"}}
	foldedTime = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
)

// statusIssue is a pod's state as podEntry makes it.
func statusIssue(namespace, pod, text string, sev model.Status, ctrl *issueController) ClusterIssue {
	return ClusterIssue{
		Ref: model.ObjectRef{Kind: "pods", Namespace: namespace, Name: pod}, Name: pod, Kind: "Pod", Source: IssueStatus,
		Severity: sev, Message: text, Since: foldedTime, reason: podStateReason(text), controller: ctrl,
	}
}

// eventIssue is a warning event about a pod as eventEntry makes it.
func eventIssue(namespace, pod, reason, message string, count int64, at time.Time) ClusterIssue {
	return ClusterIssue{
		Ref: model.ObjectRef{Kind: "pods", Namespace: namespace, Name: pod}, Name: pod, Kind: "Pod", Source: IssueEvent,
		Severity: model.StatusWarning, Message: reason + ": " + message, Since: at, reason: reason, count: count,
	}
}

// rowsOf are a table's rows as records, the shared cells included.
func rowsOf(t *FoldTable) []map[string]string {
	out := make([]map[string]string, len(t.Rows))
	for i := range t.Rows {
		out[i] = t.Record(i)
	}
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// crashingWeb are n pods of the fixture's Deployment in a crash loop, each with a restart count of
// its own, and events per pod about the back-off, whose message names the pod and its UID.
func crashingWeb(n, events int) []ClusterIssue {
	var issues []ClusterIssue
	for i := range n {
		pod := fmt.Sprintf("web-7c9-%05d", i)
		issues = append(issues, statusIssue("shop", pod, fmt.Sprintf("CrashLoopBackOff; %d restarts", 3+i), model.StatusError, webRS))
		for e := range events {
			msg := fmt.Sprintf("Back-off restarting failed container app in pod %s_shop(0b5a3c1e-7d2f-4c7e-9a1b-%012d)", pod, i)
			// The events carry no controller: the fold finds the pod in the cache.
			issues = append(issues, eventIssue("shop", pod, "BackOff", msg, int64(e+1), foldedTime.Add(time.Duration(e)*time.Minute)))
		}
	}
	return issues
}

// webPods adds the pods of crashingWeb to the lookup, each controlled by the Replica Set.
func webPods(n int) fakeOwners {
	owners := fakeOwners{}
	for k, v := range webOwners {
		owners[k] = v
	}
	for i := range n {
		owners[foldObject{kind: "pods", namespace: "shop", name: fmt.Sprintf("web-7c9-%05d", i)}] = cachedOwner{uid: fmt.Sprintf("pod-%d", i), controller: webRS}
	}
	return owners
}

// TestFoldOneCauseOverAWorkload: a crash loop over the pods of one Deployment, with the events about
// each pod, is one subject (the Deployment, found through the Replica Set) and two causes; the
// numbers count every issue, and what differs from pod to pod (the restart count, the pod's name and
// UID in the event's message) is folded.
func TestFoldOneCauseOverAWorkload(t *testing.T) {
	issues := crashingWeb(5, 3)
	f := foldIssues(issues, webPods(5), IssueQuery{}, FoldBytes)
	if f.Issues != 20 || f.Error != 5 || f.Warning != 15 || f.Subjects != 1 || f.Members != nil || f.Subject != nil {
		t.Fatalf("totals %s", mustJSON(t, f))
	}
	subjects := rowsOf(f.BySubject)
	want := map[string]string{
		"kind": "deployments", "namespace": "shop", "name": "web", "level": "error", "objects": "5", "issues": "20",
		"state": "CrashLoopBackOff", "reason": "BackOff", "message": "Back-off restarting failed container app in pod {name}_shop({uid})",
		"since": "2026-03-01T12:00:00Z",
	}
	if len(subjects) != 1 || fmt.Sprint(subjects[0]) != fmt.Sprint(want) || f.BySubject.Total != 1 || f.BySubject.More != "" {
		t.Fatalf("subjects %v, want %v", subjects, want)
	}
	causes := rowsOf(f.Causes)
	wantCauses := []map[string]string{
		{"level": "error", "source": "status", "reason": "CrashLoopBackOff", "subjects": "1", "objects": "5", "issues": "5"},
		{"level": "warning", "source": "event", "reason": "BackOff", "message": want["message"], "subjects": "1", "objects": "5", "issues": "15"},
	}
	// The causes share their subjects and objects, which the table says once; the state has no message.
	if fmt.Sprint(causes[0]["message"]) != "" || f.Causes.Same["subjects"] != "1" || f.Causes.Same["objects"] != "5" {
		t.Errorf("causes table %s", mustJSON(t, f.Causes))
	}
	for i, c := range causes {
		if c["message"] == "" {
			delete(c, "message")
		}
		if fmt.Sprint(c) != fmt.Sprint(wantCauses[i]) {
			t.Errorf("cause %d: %v, want %v", i, c, wantCauses[i])
		}
	}

	// A single pod's state is its own text, restart count included.
	one := foldIssues(crashingWeb(1, 0), webPods(1), IssueQuery{}, FoldBytes)
	if got := rowsOf(one.BySubject)[0]["state"]; got != "CrashLoopBackOff; 3 restarts" {
		t.Errorf("one pod's state %q", got)
	}
}

// TestFoldTwoCausesOnAWorkload: a workload whose pods fail for two reasons, and which is short of
// ready pods itself, is one subject: its state is the one most of its objects are in, its event the
// one most of them had, and the others are counted.
func TestFoldTwoCausesOnAWorkload(t *testing.T) {
	pull := func(i int) []ClusterIssue {
		pod := fmt.Sprintf("web-7c9-%05d", i)
		msg := `Failed to pull image "registry.example/web:2-rc.1": rpc error: code = NotFound desc = failed to pull and unpack image "registry.example/web:2-rc.1": registry.example/web:2-rc.1: not found`
		return []ClusterIssue{
			statusIssue("shop", pod, "ImagePullBackOff", model.StatusError, webRS),
			eventIssue("shop", pod, "Failed", msg, 4, foldedTime),
		}
	}
	issues := slices.Concat(pull(0), pull(1), pull(2))
	issues = append(issues,
		statusIssue("shop", "web-7c9-00003", "CrashLoopBackOff; 9 restarts", model.StatusError, webRS),
		eventIssue("shop", "web-7c9-00003", "OOMKilling", "Memory cgroup out of memory", 1_000_000, foldedTime.Add(24*time.Hour)),
		ClusterIssue{Ref: model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"}, Name: "web", Kind: "Deployment",
			Source: IssueStatus, Severity: model.StatusWarning, Message: "0 of 4 pods ready", Since: foldedTime.Add(-time.Hour), reason: reasonShortOfPods})
	f := foldIssues(issues, webPods(4), IssueQuery{}, FoldBytes)
	if f.Issues != 9 || f.Error != 4 || f.Warning != 5 || f.Subjects != 1 {
		t.Fatalf("totals %s", mustJSON(t, f))
	}
	got := rowsOf(f.BySubject)[0]
	wantMessage := `Failed to pull image "registry.example/web:2-rc.1": rpc error: code = NotFound desc = failed to pull and unpack image "{image}": {image}: not found`
	if got["name"] != "web" || got["kind"] != "deployments" || got["objects"] != "5" || got["issues"] != "9" || got["level"] != "error" ||
		got["state"] != "ImagePullBackOff" || got["reason"] != "Failed" || got["message"] != wantMessage ||
		got["otherCauses"] != "3" || got["since"] != "2026-03-01T11:00:00Z" {
		t.Errorf("subject %v", got)
	}
	// The causes name the image nowhere: other workloads with other images share them.
	var lines []string
	for _, c := range rowsOf(f.Causes) {
		lines = append(lines, strings.Join([]string{c["level"], c["source"], c["reason"], c["message"], c["subjects"], c["objects"], c["issues"]}, "|"))
	}
	want := []string{
		"error|status|ImagePullBackOff||1|3|3",
		"error|status|CrashLoopBackOff||1|1|1",
		"warning|status|pods not ready||1|1|1",
		`warning|event|Failed|Failed to pull image "{image}": rpc error: code = NotFound desc = failed to pull and unpack image "{image}": {image}: not found|1|3|3`,
		"warning|event|OOMKilling|Memory cgroup out of memory|1|1|1",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("causes:\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// TestFoldOwnerMustBeCached: an owner reference moves an issue to a workload only where the cache
// holds every owner on the way under the UID the reference names. A reference to an owner the cache
// does not hold, to another UID, or to a node leaves the pod its own subject; a Replica Set whose
// Deployment is not the one it names is the subject itself. The way ends at the workload: what
// controls a Stateful Set (an operator's custom resource the cache happens to hold) is not followed,
// or the subject would change with the views the user has open.
func TestFoldOwnerMustBeCached(t *testing.T) {
	forged := &issueController{apiVersion: "apps/v1", kind: "ReplicaSet", name: "web-7c9", uid: "another"}
	gone := &issueController{apiVersion: "apps/v1", kind: "ReplicaSet", name: "web-old", uid: "rs-0"}
	node := &issueController{apiVersion: "v1", kind: "Node", name: "master-1", uid: "node-1"}
	loose := &issueController{apiVersion: "apps/v1", kind: "ReplicaSet", name: "api-5d4", uid: "rs-2"}
	owners := webPods(0)
	owners[foldObject{kind: "replicasets", namespace: "shop", name: "api-5d4"}] = cachedOwner{uid: "rs-2",
		controller: &issueController{apiVersion: "apps/v1", kind: "Deployment", name: "web", uid: "not-web"}}
	owners[foldObject{kind: "pods", namespace: "shop", name: "seen-by-event"}] = cachedOwner{uid: "p", controller: webRS}
	app := &issueController{apiVersion: "example.com/v1", kind: "App", name: "shop", uid: "app-1"}
	owners[foldObject{kind: "apps.example.com", namespace: "shop", name: "shop"}] = cachedOwner{uid: "app-1"}
	owners[foldObject{kind: "statefulsets", namespace: "shop", name: "db"}] = cachedOwner{uid: "sts-1", controller: app}
	issues := []ClusterIssue{
		statusIssue("shop", "db-0", "CrashLoopBackOff", model.StatusError,
			&issueController{apiVersion: "apps/v1", kind: "StatefulSet", name: "db", uid: "sts-1"}),
		statusIssue("shop", "real", "CrashLoopBackOff", model.StatusError, webRS),
		statusIssue("shop", "forged", "CrashLoopBackOff", model.StatusError, forged),
		statusIssue("shop", "orphan", "CrashLoopBackOff", model.StatusError, gone),
		statusIssue("shop", "static", "CrashLoopBackOff", model.StatusError, node),
		statusIssue("shop", "loose", "CrashLoopBackOff", model.StatusError, loose),
		statusIssue("shop", "bare", "CrashLoopBackOff", model.StatusError, nil),
		eventIssue("shop", "seen-by-event", "BackOff", "Back-off", 1, foldedTime),
		eventIssue("shop", "deleted", "BackOff", "Back-off", 1, foldedTime),
	}
	f := foldIssues(issues, owners, IssueQuery{}, FoldBytes)
	var got []string
	for _, r := range rowsOf(f.BySubject) {
		got = append(got, r["kind"]+"/"+r["name"]+" "+r["objects"])
	}
	slices.Sort(got)
	want := []string{"deployments/web 2", "pods/bare 1", "pods/deleted 1", "pods/forged 1", "pods/orphan 1", "pods/static 1", "replicasets/api-5d4 1", "statefulsets/db 1"}
	if !slices.Equal(got, want) || f.Subjects != len(want) {
		t.Errorf("subjects %q, want %q", got, want)
	}
	// Without a lookup every object is its own subject.
	if f := foldIssues(issues, nil, IssueQuery{}, FoldBytes); f.Subjects != 9 {
		t.Errorf("%d subjects without a lookup", f.Subjects)
	}
}

// TestFoldOrderIsCogpits: the order reads severity, a state before an event, the number of distinct
// objects, a kind key before a kind an event only names, and names. Nothing a writer of events sets
// freely moves it: not an event's count or its time (an event that claims to have happened a million
// times, tomorrow), not the number of events about one object (many events about one pod do not
// outrank what two pods share), not a kind in capitals; nor does the order the issues come in.
func TestFoldOrderIsCogpits(t *testing.T) {
	future := foldedTime.Add(24 * time.Hour)
	owners := webPods(2)
	zeta := &issueController{apiVersion: "apps/v1", kind: "StatefulSet", name: "zeta", uid: "sts-9"}
	owners[foldObject{kind: "statefulsets", namespace: "shop", name: "zeta"}] = cachedOwner{uid: "sts-9"}
	for _, pod := range []string{"zeta-0", "zeta-1"} {
		owners[foldObject{kind: "pods", namespace: "shop", name: pod}] = cachedOwner{uid: pod, controller: zeta}
	}
	issues := func(count int64, at time.Time) []ClusterIssue {
		out := []ClusterIssue{
			eventIssue("shop", "quiet", "Whisper", "a real problem", 1, foldedTime),
			eventIssue("shop", "pair-a", "Twice", "two objects share this", 1, foldedTime),
			eventIssue("shop", "pair-b", "Twice", "two objects share this", 1, foldedTime),
			statusIssue("shop", "warned", "Unschedulable", model.StatusWarning, nil),
			statusIssue("shop", "broken", "CrashLoopBackOff", model.StatusError, nil),
			// One subject with two events: the one two of its objects had is its event, however many
			// events of the other one of them had and whatever those claim.
			eventIssue("shop", "web-7c9-00000", "Common", "both pods", 1, foldedTime),
			eventIssue("shop", "web-7c9-00001", "Common", "both pods", 1, foldedTime),
			// A Stateful Set's two pods: a subject of two objects, whose kind sorts after the pods'.
			eventIssue("shop", "zeta-0", "Zed", "both pods", 1, foldedTime),
			eventIssue("shop", "zeta-1", "Zed", "both pods", 1, foldedTime),
			// An object only an event names, of a kind in capitals.
			{Ref: model.ObjectRef{Kind: "events", Namespace: "shop", Name: "e9"}, Name: "decoy", Kind: "AAA", Source: IssueEvent,
				Severity: model.StatusWarning, Message: "Decoy: read this first", Since: at, reason: "Decoy", count: count},
		}
		for range 5 {
			out = append(out, eventIssue("shop", "web-7c9-00001", "Aloud", "one pod", count, at))
		}
		for range 3 {
			out = append(out, eventIssue("shop", "loud", "Shouting", "read me first", count, at))
		}
		return out
	}
	order := func(f *IssueFold) (subjects, causes []string) {
		for _, r := range rowsOf(f.BySubject) {
			subjects = append(subjects, r["name"])
		}
		for _, r := range rowsOf(f.Causes) {
			causes = append(causes, r["reason"])
		}
		return subjects, causes
	}
	plain := foldIssues(issues(1, foldedTime), owners, IssueQuery{}, FoldBytes)
	subjects, causes := order(plain)
	wantSubjects := []string{"broken", "warned", "web", "zeta", "loud", "pair-a", "pair-b", "quiet", "decoy"}
	wantCauses := []string{"CrashLoopBackOff", "Unschedulable", "Common", "Twice", "Zed", "Aloud", "Decoy", "Shouting", "Whisper"}
	if !slices.Equal(subjects, wantSubjects) || !slices.Equal(causes, wantCauses) {
		t.Fatalf("order: subjects %q, causes %q", subjects, causes)
	}
	if web := rowsOf(plain.BySubject)[2]; web["reason"] != "Common" || web["otherCauses"] != "1" || web["objects"] != "2" || web["issues"] != "7" {
		t.Errorf("the workload's event %v", web)
	}
	loud := issues(1_000_000, future)
	slices.Reverse(loud)
	if got := mustJSON(t, foldIssues(loud, owners, IssueQuery{}, FoldBytes)); got != mustJSON(t, plain) {
		t.Errorf("an event's count and time, or the issues' order, moved the fold:\n%s\nwas\n%s", got, mustJSON(t, plain))
	}

	// A subject's issues one by one: by object and message, whatever an event's count and time.
	q := IssueQuery{Subject: &IssueSubject{Kind: "deployments", Namespace: "shop", Name: "web"}}
	want := []string{"web-7c9-00000 Common: both pods", "web-7c9-00001 Aloud: one pod", "web-7c9-00001 Common: both pods"}
	for _, in := range [][]ClusterIssue{issues(1, foldedTime), loud} {
		var got []string
		for _, r := range rowsOf(foldIssues(in, owners, q, FoldBytes).Members) {
			got = append(got, r["name"]+" "+r["message"])
		}
		if !slices.Equal(got, want) {
			t.Errorf("the members' order %q, want %q", got, want)
		}
	}
}

// TestFoldAnEventsKindIsNoKindKey: what an event writes as its object's kind never makes that
// object one the cache holds. An event about an object of the kind "pods", which no view shows (the
// kind is "Pod"), named like a Deployment's pod, is a subject of its own under a kind no kind key
// reads like; the pod stays with its Deployment, in whichever order the issues come.
func TestFoldAnEventsKindIsNoKindKey(t *testing.T) {
	issues := crashingWeb(3, 0)
	issues = append(issues, ClusterIssue{Ref: model.ObjectRef{Kind: "events", Namespace: "shop", Name: "e1"}, Name: "web-7c9-00000", Kind: "pods",
		Source: IssueEvent, Severity: model.StatusWarning, Message: "Odd: an odd event", Since: foldedTime, reason: "Odd", count: 1})
	f := foldIssues(issues, webPods(3), IssueQuery{}, FoldBytes)
	var got []string
	for _, r := range rowsOf(f.BySubject) {
		got = append(got, r["kind"]+"/"+r["name"]+" "+r["objects"]+" "+r["issues"])
	}
	if want := []string{"deployments/web 3 3", "pods (unknown kind)/web-7c9-00000 1 1"}; !slices.Equal(got, want) || f.Subjects != 2 {
		t.Fatalf("subjects %q, want %q", got, want)
	}
	reversed := slices.Clone(issues)
	slices.Reverse(reversed)
	if back := mustJSON(t, foldIssues(reversed, webPods(3), IssueQuery{}, FoldBytes)); back != mustJSON(t, f) {
		t.Errorf("the issues' order moved the fold:\n%s\nwas\n%s", back, mustJSON(t, f))
	}
	// The row's kind reads the event's issue back, and the pod's own kind key does not.
	odd := foldIssues(issues, webPods(3), IssueQuery{Subject: &IssueSubject{Kind: "pods (unknown kind)", Namespace: "shop", Name: "web-7c9-00000"}}, FoldBytes)
	if odd.Issues != 1 || odd.Members.Cell(0, "message") != "Odd: an odd event" || odd.Members.Cell(0, "kind") != "pods (unknown kind)" {
		t.Errorf("the event's object by its row: %s", mustJSON(t, odd))
	}
	if pod := foldIssues(issues, webPods(3), IssueQuery{Subject: &IssueSubject{Kind: "pods", Namespace: "shop", Name: "web-7c9-00000"}}, FoldBytes); pod.Issues != 0 {
		t.Errorf("the pod counts for its Deployment, not for itself: %s", mustJSON(t, pod))
	}
	// A kind in capitals is no kind key as it is.
	if got := (foldObject{kind: "Widget", named: true}).kindCell(); got != "Widget" {
		t.Errorf("an API kind's cell %q", got)
	}
}

// TestFoldEventFromAnotherNamespace: an event that lies in one namespace and names an object of
// another counts for no object of that other namespace: its subject is the object as the event
// names it, in the event's own namespace, and the workload's row is as it was.
func TestFoldEventFromAnotherNamespace(t *testing.T) {
	issues := crashingWeb(2, 1)
	for i := range issues {
		if issues[i].Source == IssueEvent {
			issues[i].eventNamespace = "shop" // as eventEntry sets it
		}
	}
	before := mustJSON(t, rowsOf(foldIssues(issues, webPods(2), IssueQuery{}, FoldBytes).BySubject)[0])
	foreign := eventIssue("shop", "web-7c9-00000", "Shouting", "read me first", 1, foldedTime)
	foreign.eventNamespace = "sandbox"
	f := foldIssues(append(issues, foreign), webPods(2), IssueQuery{}, FoldBytes)
	rows := rowsOf(f.BySubject)
	if len(rows) != 2 || mustJSON(t, rows[0]) != before || f.Issues != 5 {
		t.Fatalf("the workload's row %s, was %s", mustJSON(t, rows), before)
	}
	if r := rows[1]; r["kind"] != "Pod" || r["namespace"] != "sandbox" || r["name"] != "web-7c9-00000" || r["reason"] != "Shouting" || r["objects"] != "1" {
		t.Errorf("the foreign event's subject %v", r)
	}
}

// manyPods are n pods without a controller, each in a state of its own reason when distinct.
func manyPods(n int, distinct bool) []ClusterIssue {
	issues := make([]ClusterIssue, n)
	for i := range n {
		text := "CrashLoopBackOff"
		if distinct {
			text = fmt.Sprintf("Reason%03d", i)
		}
		issues[i] = statusIssue("shop", fmt.Sprintf("pod-%03d", i), text, model.StatusError, nil)
	}
	return issues
}

// TestFoldPagesAndTotals: the numbers count every issue whatever the page shows; limit and offset
// page the subjects, more says how many follow and where; the causes stop at their cap and say so;
// under a byte bound a page gives up rows and says where to read on.
func TestFoldPagesAndTotals(t *testing.T) {
	issues := manyPods(50, true)
	f := foldIssues(issues, nil, IssueQuery{}, FoldBytes)
	if f.Issues != 50 || f.Error != 50 || f.Subjects != 50 || f.BySubject.Total != 50 || len(f.BySubject.Rows) != FoldDefaultLimit ||
		f.BySubject.More != "30 more: offset 20" || f.BySubject.Offset != 0 {
		t.Fatalf("the default page: %s", mustJSON(t, f))
	}
	if f.Causes.Total != 50 || len(f.Causes.Rows) != FoldMaxCauses || f.Causes.More != "30 more: narrow with severity, sources or reason" {
		t.Errorf("the causes: total %d, %d rows, more %q", f.Causes.Total, len(f.Causes.Rows), f.Causes.More)
	}
	next := foldIssues(issues, nil, IssueQuery{Offset: 40, Limit: 20}, FoldBytes)
	if next.Issues != 50 || next.BySubject.Offset != 40 || len(next.BySubject.Rows) != 10 || next.BySubject.More != "" ||
		next.BySubject.Cell(0, "name") != "pod-040" {
		t.Errorf("the last page: %s", mustJSON(t, next.BySubject))
	}
	if all := foldIssues(issues, nil, IssueQuery{Limit: 5000}, FoldBytes); len(all.BySubject.Rows) != 50 {
		t.Errorf("limit 5000 returned %d rows of 50", len(all.BySubject.Rows))
	}
	// A page has FoldMaxLimit rows at most, whatever is asked for.
	if most := foldIssues(manyPods(250, false), nil, IssueQuery{Limit: 5000}, FoldBytes); len(most.BySubject.Rows) != FoldMaxLimit ||
		most.BySubject.More != "50 more: offset 200" || most.Subjects != 250 {
		t.Errorf("limit 5000 over 250 subjects: %d rows, more %q", len(most.BySubject.Rows), most.BySubject.More)
	}
	if past := foldIssues(issues, nil, IssueQuery{Offset: 500}, FoldBytes); len(past.BySubject.Rows) != 0 || past.BySubject.Total != 50 || past.Issues != 50 {
		t.Errorf("an offset past the end: %s", mustJSON(t, past.BySubject))
	}

	// The byte bound: fewer rows than asked for, the same numbers, and where to read on.
	const budget = 600
	small := foldIssues(manyPods(50, false), nil, IssueQuery{Limit: 50}, budget)
	n := len(small.BySubject.Rows)
	if size := len(mustJSON(t, small)); size > budget || n == 0 || n >= 50 || small.Issues != 50 || small.Subjects != 50 ||
		small.BySubject.More != fmt.Sprintf("%d more: offset %d", 50-n, n) {
		t.Errorf("under %d bytes: %d bytes, %d rows, more %q", budget, len(mustJSON(t, small)), n, small.BySubject.More)
	}
	// What a cluster's writers put into events cannot push the fold over its bound: a reason and a
	// message of their own per pod, of characters JSON writes six bytes for, the page at its largest.
	// The causes give way first, then the page.
	var flood []ClusterIssue
	for i := range 400 {
		pod := fmt.Sprintf("pod-%03d", i)
		flood = append(flood, eventIssue("shop", pod, fmt.Sprintf("%03d", i)+strings.Repeat("<", 100), fmt.Sprintf("%03d", i)+strings.Repeat("&", 2000), 1, foldedTime))
	}
	full := foldIssues(flood, nil, IssueQuery{Limit: FoldMaxLimit}, FoldBytes)
	rows, causes := len(full.BySubject.Rows), len(full.Causes.Rows)
	if size := len(mustJSON(t, full)); size > FoldBytes || size < FoldBytes*3/4 || full.Issues != 400 || full.Subjects != 400 ||
		rows == 0 || rows >= FoldMaxLimit || causes == 0 || causes >= FoldMaxCauses || full.Causes.Total != 400 ||
		full.BySubject.More != fmt.Sprintf("%d more: offset %d", 400-rows, rows) ||
		full.Causes.More != fmt.Sprintf("%d more: narrow with severity, sources or reason", 400-causes) {
		t.Errorf("a flood: %d bytes, %d rows (%q), %d causes of %d (%q)", size, rows, full.BySubject.More, causes, full.Causes.Total, full.Causes.More)
	}
	for _, r := range slices.Concat(rowsOf(full.Causes), rowsOf(full.BySubject)) {
		if len(r["reason"]) > FoldReasonBytes+len("…") || len(r["message"]) > FoldMessageBytes || r["message"] == "" {
			t.Fatalf("a row's reason of %d bytes and message of %d", len(r["reason"]), len(r["message"]))
		}
	}
	// A state's text is cut as a message is, its reason as an event's.
	text := "Containers not ready: " + strings.Repeat("sidecar-with-a-long-name, ", 30)
	state := foldIssues([]ClusterIssue{statusIssue("shop", "pod-000", text, model.StatusWarning, nil)}, nil, IssueQuery{}, FoldBytes)
	if got, reason := state.BySubject.Cell(0, "state"), state.Causes.Cell(0, "reason"); len(got) > FoldMessageBytes || !strings.Contains(got, " … ") ||
		len(reason) != FoldReasonBytes+len("…") || !strings.HasPrefix(text, strings.TrimSuffix(reason, "…")) {
		t.Errorf("a long state: %d bytes %q, its reason %d bytes %q", len(got), got, len(reason), reason)
	}
}

// readOn follows a table's More from the first page to the last and returns the rows' names; every
// page must have a row and stay under the bound.
func readOn(t *testing.T, issues []ClusterIssue, q IssueQuery, budget int) []string {
	t.Helper()
	var names []string
	for range len(issues) + 1 {
		f := foldIssues(issues, nil, q, budget)
		table := f.BySubject
		if q.Subject != nil {
			table = f.Members
		}
		if size := len(mustJSON(t, f)); size > budget {
			t.Fatalf("offset %d: %d bytes over the bound of %d", q.Offset, size, budget)
		}
		for i := range table.Rows {
			names = append(names, table.Cell(i, "name"))
		}
		if table.More == "" {
			if len(names) != table.Total {
				t.Fatalf("read %d rows of %d", len(names), table.Total)
			}
			return names
		}
		next := q.Offset + len(table.Rows)
		if len(table.Rows) == 0 || table.More != fmt.Sprintf("%d more: offset %d", table.Total-next, next) {
			t.Fatalf("offset %d: %d rows, more %q: reading on is not possible", q.Offset, len(table.Rows), table.More)
		}
		q.Offset = next
	}
	t.Fatal("the pages do not end")
	return nil
}

// TestFoldLongNamesStillPage: an event's writer chooses the kind and the name of the object the
// event is about, at any length. Such an object is a row under a name cut to what Kubernetes allows
// one, so that a page always has a row, More always names a later offset, and the fold stays under
// its bound: for an object without a view and for one of a kind with a view, first on the page and
// in its middle, and for that subject's own issues.
func TestFoldLongNamesStillPage(t *testing.T) {
	long := strings.Repeat("n", 30<<10)
	named := func(kind, name string) ClusterIssue {
		return ClusterIssue{Ref: model.ObjectRef{Kind: "events", Namespace: "shop", Name: "e-" + kind}, Name: name, Kind: kind,
			Source: IssueEvent, Severity: model.StatusWarning, Message: "Broken: it broke", Since: foldedTime, reason: "Broken", count: 1}
	}
	issues := []ClusterIssue{
		statusIssue("shop", "broken", "CrashLoopBackOff", model.StatusError, nil),
		eventIssue("shop", "m"+long, "BackOff", "Back-off", 1, foldedTime), // a kind with a view: the reference carries the name
		eventIssue("shop", "zz-after", "BackOff", "Back-off", 1, foldedTime),
		named("Widget", long),
		named("Widget", "small"),
	}
	cut := long[:FoldNameBytes] + "…"
	want := []string{"broken", "m" + long[:FoldNameBytes-1] + "…", "zz-after", cut, "small"}
	if got := readOn(t, issues, IssueQuery{}, FoldBytes); !slices.Equal(got, want) {
		t.Errorf("the subjects' names: %d rows, lengths %v", len(got), lengths(got))
	}
	if got := readOn(t, issues, IssueQuery{Limit: 1}, FoldBytes); !slices.Equal(got, want) {
		t.Errorf("row by row: %d rows, lengths %v", len(got), lengths(got))
	}
	// The row's name reads the subject's issues, under the bound; the name as the event wrote it is
	// longer than any row's and refused.
	q := IssueQuery{Subject: &IssueSubject{Kind: "Widget", Namespace: "shop", Name: cut}}
	if err := q.validate(); err != nil {
		t.Fatal(err)
	}
	if got := readOn(t, issues, q, FoldBytes); !slices.Equal(got, []string{cut}) {
		t.Errorf("the subject's issues: %d rows, lengths %v", len(got), lengths(got))
	}
	if err := (IssueQuery{Subject: &IssueSubject{Kind: "Widget", Namespace: "shop", Name: long}}).validate(); err == nil {
		t.Error("a subject's name of 30 KiB was taken")
	}

	// The worst a writer of events can do to a row: kind, namespace, name, reason and message of
	// kilobytes of characters JSON writes six bytes for, a subject of its own each.
	var flood []ClusterIssue
	wide := strings.Repeat("<", 30<<10)
	for i := range 12 {
		n := fmt.Sprintf("%02d", i)
		flood = append(flood, ClusterIssue{Ref: model.ObjectRef{Kind: "events", Namespace: n + wide, Name: "e" + n}, Name: n + wide, Kind: n + wide,
			Source: IssueEvent, Severity: model.StatusWarning, Message: n + wide + ": " + n + wide, Since: foldedTime, reason: n + wide, count: 1})
	}
	if got := readOn(t, flood, IssueQuery{}, FoldBytes); len(got) != 12 {
		t.Errorf("a flood of wide rows: %d rows", len(got))
	}
	first := foldIssues(flood, nil, IssueQuery{}, FoldBytes).BySubject
	member := IssueQuery{Subject: &IssueSubject{Kind: first.Cell(0, "kind"), Namespace: first.Cell(0, "namespace"), Name: first.Cell(0, "name")}}
	if err := member.validate(); err != nil {
		t.Fatal(err)
	}
	if got := readOn(t, flood, member, FoldBytes); len(got) != 1 {
		t.Errorf("a wide subject's issues: %d rows", len(got))
	}
	// A bound smaller than a row: the page still has one, and reading on ends.
	if f := foldIssues(flood, nil, IssueQuery{}, 600); len(f.BySubject.Rows) != 1 || f.BySubject.More != "11 more: offset 1" {
		t.Errorf("under 600 bytes: %d rows, more %q", len(f.BySubject.Rows), f.BySubject.More)
	}
}

func lengths(ss []string) []int {
	out := make([]int, len(ss))
	for i, s := range ss {
		out[i] = len(s)
	}
	return out
}

// TestFoldSubjectIsItsNamespaceToo: a subject is its kind, namespace and name: the same name in two
// namespaces is two subjects, and one's issues are not the other's.
func TestFoldSubjectIsItsNamespaceToo(t *testing.T) {
	issues := []ClusterIssue{
		statusIssue("staging", "db-0", "CrashLoopBackOff", model.StatusError, nil),
		statusIssue("prod", "db-0", "Unschedulable", model.StatusWarning, nil),
	}
	if f := foldIssues(issues, nil, IssueQuery{}, FoldBytes); f.Subjects != 2 {
		t.Fatalf("%d subjects", f.Subjects)
	}
	f := foldIssues(issues, nil, IssueQuery{Subject: &IssueSubject{Kind: "pods", Namespace: "prod", Name: "db-0"}}, FoldBytes)
	if f.Issues != 1 || f.Warning != 1 || f.Error != 0 || f.Members.Total != 1 || f.Members.Cell(0, "message") != "Unschedulable" || f.Members.Cell(0, "namespace") != "prod" {
		t.Errorf("prod/db-0: %s", mustJSON(t, f))
	}
}

// TestFoldTimesAreUTC: the fold's times are in UTC whatever zone an issue's time is in (an event's
// is the computer's).
func TestFoldTimesAreUTC(t *testing.T) {
	cet := time.FixedZone("CET", 3600)
	state := statusIssue("shop", "db-0", "Unschedulable", model.StatusWarning, nil)
	state.Since = foldedTime.In(cet)
	issues := []ClusterIssue{state,
		eventIssue("shop", "db-0", "FailedScheduling", "no node fits", 1, foldedTime.In(cet)),
		eventIssue("shop", "db-0", "FailedScheduling", "no node fits", 1, foldedTime.Add(time.Minute).In(cet)),
	}
	if got := foldIssues(issues, nil, IssueQuery{}, FoldBytes).BySubject.Cell(0, "since"); got != "2026-03-01T12:00:00Z" {
		t.Errorf("a subject's since %q", got)
	}
	m := foldIssues(issues, nil, IssueQuery{Subject: &IssueSubject{Kind: "pods", Namespace: "shop", Name: "db-0"}, Sources: []string{IssueEvent}}, FoldBytes).Members
	if since, last := m.Cell(0, "since"), m.Cell(0, "last"); since != "2026-03-01T12:00:00Z" || last != "2026-03-01T12:01:00Z" {
		t.Errorf("a member's since %q and last %q", since, last)
	}
}

// TestFoldPodsOfOneWorkloadWithOtherImages: where the pods of one workload fail to pull different
// images, the workload's row names none of them (which one would depend on the order the issues come
// in) and shows the message its pods share.
func TestFoldPodsOfOneWorkloadWithOtherImages(t *testing.T) {
	var issues []ClusterIssue
	for i, image := range []string{"registry.example/web:1", "registry.example/sidecar:7"} {
		issues = append(issues, eventIssue("shop", fmt.Sprintf("web-7c9-%05d", i), "Failed", `Failed to pull image "`+image+`": not found`, 1, foldedTime))
	}
	f := foldIssues(issues, webPods(2), IssueQuery{}, FoldBytes)
	if got := f.BySubject.Cell(0, "message"); got != `Failed to pull image "{image}": not found` || f.Subjects != 1 || f.Causes.Total != 1 {
		t.Errorf("the workload's message %q of %s", got, mustJSON(t, f))
	}
	slices.Reverse(issues)
	if back := mustJSON(t, foldIssues(issues, webPods(2), IssueQuery{}, FoldBytes)); back != mustJSON(t, f) {
		t.Errorf("the issues' order moved the fold:\n%s\nwas\n%s", back, mustJSON(t, f))
	}
}

// TestFoldDrillDown: severity, sources and reason fold only the issues they select, and the numbers
// count those; a subject returns its issues one by one, which are the Overview's own entries with
// their messages whole.
func TestFoldDrillDown(t *testing.T) {
	long := strings.Repeat("a long line of a stack trace; ", 50) // 1500 bytes
	issues := crashingWeb(3, 2)
	issues = append(issues,
		statusIssue("shop", "db-0", "Unschedulable", model.StatusWarning, nil),
		eventIssue("shop", "db-0", "FailedScheduling", long, 7, foldedTime.Add(time.Minute)),
		ClusterIssue{Ref: model.ObjectRef{Kind: "events", Namespace: "kube-system", Name: "e4"}, Name: "gizmo", Kind: "Widget",
			Source: IssueEvent, Severity: model.StatusWarning, Message: "Broken: it broke", Since: foldedTime, reason: "Broken", count: 1})
	owners := webPods(3)
	fold := func(q IssueQuery) *IssueFold {
		t.Helper()
		if err := q.validate(); err != nil {
			t.Fatal(err)
		}
		return foldIssues(slices.Clone(issues), owners, q, FoldBytes)
	}
	names := func(f *IssueFold) []string {
		var out []string
		for _, r := range rowsOf(f.BySubject) {
			out = append(out, r["kind"]+"/"+r["name"])
		}
		return out
	}
	if f := fold(IssueQuery{}); f.Issues != 12 || !slices.Equal(names(f), []string{"deployments/web", "pods/db-0", "Widget/gizmo"}) {
		t.Fatalf("everything: %d issues, %q", f.Issues, names(f))
	}
	if f := fold(IssueQuery{Severity: "error"}); f.Issues != 3 || f.Error != 3 || f.Warning != 0 || !slices.Equal(names(f), []string{"deployments/web"}) ||
		f.Causes.Total != 1 || f.BySubject.Cell(0, "reason") != "" {
		t.Errorf("severity error: %s", mustJSON(t, f))
	}
	if f := fold(IssueQuery{Sources: []string{IssueEvent}}); f.Issues != 8 || f.Error != 0 || f.Subjects != 3 || f.BySubject.Cell(0, "state") != "" {
		t.Errorf("sources event: %s", mustJSON(t, f))
	}
	if f := fold(IssueQuery{Sources: []string{IssueStatus}, Severity: "warning"}); f.Issues != 1 || !slices.Equal(names(f), []string{"pods/db-0"}) {
		t.Errorf("warning states: %s", mustJSON(t, f))
	}
	if f := fold(IssueQuery{Reason: "backoff"}); f.Issues != 6 || f.Subjects != 1 || f.Causes.Total != 1 || f.Causes.Cell(0, "reason") != "BackOff" {
		t.Errorf("reason backoff: %s", mustJSON(t, f))
	}
	if f := fold(IssueQuery{Reason: "crashloopbackoff"}); f.Issues != 3 || f.Error != 3 {
		t.Errorf("a state's reason: %s", mustJSON(t, f))
	}
	if f := fold(IssueQuery{Limit: 1, Offset: 1}); !slices.Equal(names(f), []string{"pods/db-0"}) || f.BySubject.More != "1 more: offset 2" || f.Issues != 12 {
		t.Errorf("limit 1, offset 1: %s", mustJSON(t, f.BySubject))
	}

	// A subject's issues: every entry of the Overview that counts for it, its message as it is; a
	// pod's two events that read alike are one row for both.
	web := fold(IssueQuery{Subject: &IssueSubject{Kind: "deployments", Namespace: "shop", Name: "web"}})
	if web.Issues != 9 || web.Error != 3 || web.Subjects != 1 || web.BySubject != nil || web.Causes != nil ||
		web.Members.Total != 6 || len(web.Members.Rows) != 6 || web.Subject.Name != "web" {
		t.Fatalf("the Deployment's issues: %s", mustJSON(t, web))
	}
	var got []string
	for _, r := range rowsOf(web.Members) {
		got = append(got, strings.Join([]string{r["kind"], r["namespace"], r["name"], r["level"], r["source"], r["message"], r["issues"], r["count"], r["since"], r["last"]}, "|"))
	}
	var want []string
	for i := range 3 {
		pod := fmt.Sprintf("web-7c9-%05d", i)
		want = append(want, fmt.Sprintf("pods|shop|%s|error|status|CrashLoopBackOff; %d restarts|1||2026-03-01T12:00:00Z|", pod, 3+i))
	}
	for i := range 3 {
		pod := fmt.Sprintf("web-7c9-%05d", i)
		want = append(want, fmt.Sprintf("pods|shop|%s|warning|event|BackOff: Back-off restarting failed container app in pod %s_shop(0b5a3c1e-7d2f-4c7e-9a1b-%012d)|2|3|2026-03-01T12:00:00Z|2026-03-01T12:01:00Z", pod, pod, i))
	}
	if !slices.Equal(got, want) {
		t.Errorf("members:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Every entry of the unfolded list about the subject's pods is in a row, under its own message.
	rows := map[string]bool{}
	for _, r := range rowsOf(web.Members) {
		rows[r["name"]+"|"+r["level"]+"|"+r["source"]+"|"+r["message"]] = true
	}
	for _, is := range crashingWeb(3, 2) {
		if !rows[is.Name+"|"+string(is.Severity)+"|"+is.Source+"|"+is.Message] {
			t.Errorf("no row for the entry %s %s %q", is.Name, is.Source, is.Message)
		}
	}
	db := fold(IssueQuery{Subject: &IssueSubject{Kind: "pods", Namespace: "shop", Name: "db-0"}, Sources: []string{IssueEvent}})
	if db.Issues != 1 || db.Members.Cell(0, "message") != "FailedScheduling: "+long || db.Members.Cell(0, "count") != "7" {
		t.Errorf("a long message is whole: %d bytes of %d", len(db.Members.Cell(0, "message")), len("FailedScheduling: "+long))
	}
	page := fold(IssueQuery{Subject: &IssueSubject{Kind: "deployments", Namespace: "shop", Name: "web"}, Limit: 3, Offset: 1})
	if len(page.Members.Rows) != 3 || page.Members.More != "2 more: offset 4" || page.Issues != 9 || page.Members.Total != 6 {
		t.Errorf("a page of members: %s", mustJSON(t, page.Members))
	}
	// An object Cogpit has no view for is a subject by its API kind; one that is gone has no issues.
	if w := fold(IssueQuery{Subject: &IssueSubject{Kind: "Widget", Namespace: "kube-system", Name: "gizmo"}}); w.Issues != 1 || w.Members.Cell(0, "message") != "Broken: it broke" {
		t.Errorf("an object without a view: %s", mustJSON(t, w))
	}
	if none := fold(IssueQuery{Subject: &IssueSubject{Kind: "pods", Namespace: "shop", Name: "gone"}}); none.Issues != 0 || none.Subjects != 0 ||
		none.Members == nil || none.Members.Total != 0 || len(none.Members.Rows) != 0 {
		t.Errorf("a subject without issues: %s", mustJSON(t, none))
	}

	for _, bad := range []IssueQuery{{Severity: "fatal"}, {Sources: []string{"alerts"}}, {Limit: -1}, {Offset: -1}, {Subject: &IssueSubject{Name: "web"}}} {
		if bad.validate() == nil {
			t.Errorf("the query %+v was taken", bad)
		}
	}
}

// TestFoldMemberTooLargeForThePage: a subject's issue whose message alone is over the bound (four
// kibibytes of characters JSON writes six bytes for) is returned with its message cut, so that
// reading on stays possible.
func TestFoldMemberTooLargeForThePage(t *testing.T) {
	issues := []ClusterIssue{
		eventIssue("shop", "db-0", "Huge", strings.Repeat("<", 8<<10), 1, foldedTime),
		eventIssue("shop", "db-0", "Small", "fits", 1, foldedTime),
	}
	f := foldIssues(issues, nil, IssueQuery{Subject: &IssueSubject{Kind: "pods", Namespace: "shop", Name: "db-0"}}, FoldBytes)
	if size := len(mustJSON(t, f)); size > FoldBytes || len(f.Members.Rows) != 1 || f.Members.More != "1 more: offset 1" ||
		len(f.Members.Cell(0, "message")) > FoldMessageBytes || !strings.Contains(f.Members.Cell(0, "message"), "…") {
		t.Errorf("%d bytes, %d rows, more %q, a message of %d bytes", size, len(f.Members.Rows), f.Members.More, len(f.Members.Cell(0, "message")))
	}
	// A message that fits the page is whole up to FoldMemberMessageBytes and cut there.
	issues = []ClusterIssue{eventIssue("shop", "db-0", "Long", strings.Repeat("a", 6<<10), 1, foldedTime)}
	f = foldIssues(issues, nil, IssueQuery{Subject: &IssueSubject{Kind: "pods", Namespace: "shop", Name: "db-0"}}, FoldBytes)
	if got := f.Members.Cell(0, "message"); len(got) != FoldMemberMessageBytes+len("…") || !strings.HasPrefix(got, "Long: aaa") || !strings.HasSuffix(got, "a…") {
		t.Errorf("a member's message of 6 KiB: %d bytes", len(got))
	}
}

// TestCauseMessage: what an event's message keeps and what it folds, as a cause and as a subject's
// row has it.
func TestCauseMessage(t *testing.T) {
	for _, c := range []struct{ text, name, cause, row string }{
		{"Readiness probe failed: HTTP probe failed with statuscode: 503", "web-0", "", ""},
		{"Back-off restarting failed container app in pod web-0_shop(0b5a3c1e-7d2f-4c7e-9a1b-000000000001)", "web-0",
			"Back-off restarting failed container app in pod {name}_shop({uid})", ""},
		{`Failed to pull image "nginx:9": not found: nginx:9`, "web-0", `Failed to pull image "{image}": not found: {image}`,
			`Failed to pull image "nginx:9": not found: {image}`},
		{`Container image "app@sha256:` + strings.Repeat("ab", 32) + `" already present on machine`, "web-0",
			`Container image "{image}" already present on machine`, `Container image "app@{digest}" already present on machine`},
		{"web-0 and web-01 and my-web-0, web-0.", "web-0", "{name} and web-01 and my-web-0, {name}.", ""},
		{"commit 0123456789abcdef0123 on port 8080 exit code 137", "db", "commit {hash} on port 8080 exit code 137", ""},
		{"two\nlines\tand a tab", "x", "two lines and a tab", ""},
	} {
		if c.cause == "" {
			c.cause = c.text
		}
		if c.row == "" {
			c.row = c.cause
		}
		if cause, row := causeMessage(c.text, c.name); cause != c.cause || row != c.row {
			t.Errorf("%q:\ncause %q\nrow   %q", c.text, cause, row)
		}
	}
	long := "start " + strings.Repeat("ä", 200) + " the end: connection refused"
	if cause, _ := causeMessage(long, "x"); len(cause) > FoldMessageBytes || !strings.HasPrefix(cause, "start ää") ||
		!strings.HasSuffix(cause, "the end: connection refused") || !strings.Contains(cause, " … ") || strings.ContainsRune(cause, '\uFFFD') {
		t.Errorf("a long message keeps its start and its end: %d bytes %q", len(cause), cause)
	}
	for text, want := range map[string]string{
		"CrashLoopBackOff; 5 restarts": "CrashLoopBackOff", "ImagePullBackOff; 1 restart": "ImagePullBackOff",
		"Unschedulable": "Unschedulable", "Containers not ready: app; 12 restarts": "Containers not ready: app",
		"a; b": "a; b", "x; many restarts": "x; many restarts",
	} {
		if got := podStateReason(text); got != want {
			t.Errorf("podStateReason(%q) = %q, want %q", text, got, want)
		}
	}
}

// foldSession is a session over the Overview's kinds and the Replica Sets, with a Deployment of
// three crash-looping pods under its Replica Set and a pod whose owner reference names that Replica
// Set under another UID.
func foldSession(t *testing.T) (*Session, *testkit.MemorySource) {
	t.Helper()
	now := time.Now()
	src := overviewFixture(now)
	src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", "api", map[string]any{
		"spec": map[string]any{"replicas": int64(3)}, "status": map[string]any{"replicas": int64(3), "updatedReplicas": int64(3), "readyReplicas": int64(3)},
	}))
	src.Set(replicaSetsGVR, owned(testkit.Object("apps/v1", "ReplicaSet", "shop", "api-5d4", nil), "apps/v1", "Deployment", "api", "uid-shop-api"))
	crashing := func(name, rsUID string) *unstructured.Unstructured {
		p := owned(ovPod("shop", name, "worker-1", "100m", "64Mi"), "apps/v1", "ReplicaSet", "api-5d4", rsUID)
		_ = unstructured.SetNestedSlice(p.Object, []any{map[string]any{
			"name": "app", "ready": false, "restartCount": int64(len(name)),
			"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
		}}, "status", "containerStatuses")
		return p
	}
	for _, name := range []string{"api-5d4-a", "api-5d4-bb", "api-5d4-ccc"} {
		src.Set(testkit.PodsGVR, crashing(name, "uid-shop-api-5d4"))
		src.Set(testkit.EventsGVR, event("shop", name+".1", "Pod", name, "", "BackOff", "Warning", now.Add(-time.Minute)))
	}
	src.Set(testkit.PodsGVR, crashing("api-5d4-forged", "another-uid"))
	return foldKindsSession(t, src, "replicasets"), src
}

// foldProfiles are the profiles of the Overview's kinds and of more.
func foldProfiles(t *testing.T, more ...string) []*model.KindProfile {
	t.Helper()
	var profiles []*model.KindProfile
	for _, key := range append([]string{"nodes", "pods", "deployments", "statefulsets", "daemonsets", "events", "namespaces"}, more...) {
		p, ok := kinds.ByKey(key)
		if !ok {
			t.Fatalf("no profile %s", key)
		}
		profiles = append(profiles, p)
	}
	return profiles
}

// foldKindsSession is a session over src whose cluster serves the Overview's kinds and more, with
// the Pods view a connect opens listed.
func foldKindsSession(t *testing.T, src *testkit.MemorySource, more ...string) *Session {
	t.Helper()
	s := New(Options{Connector: &fakeConnector{src: src, profiles: foldProfiles(t, more...)}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced })
	return s
}

// crashLooping is a pod in a crash loop.
func crashLooping(namespace, name string) *unstructured.Unstructured {
	p := ovPod(namespace, name, "worker-1", "100m", "64Mi")
	_ = unstructured.SetNestedSlice(p.Object, []any{map[string]any{
		"name": "app", "ready": false, "restartCount": int64(3),
		"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
	}}, "status", "containerStatuses")
	return p
}

// subjectsOf reads the fold and returns its subjects' rows by kind, namespace and name.
func subjectsOf(t *testing.T, s *Session, namespaces []string, q IssueQuery) (*IssueFold, map[string]map[string]string) {
	t.Helper()
	m, err := s.ClusterOverviewFolded(context.Background(), "", namespaces, false, q)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]map[string]string{}
	for _, r := range rowsOf(m.Fold.BySubject) {
		by[r["kind"]+"/"+r["namespace"]+"/"+r["name"]] = r
	}
	return m.Fold, by
}

// TestClusterOverviewFolded: the Overview's read folds a Deployment's pods into it through the
// Replica Set, which it reads into the cache only because a pod with an issue names one; a pod that
// names the Replica Set under another UID stays its own subject; the fold counts the entries the
// unfolded read returns, whose event entries carry the event's reason and count.
func TestClusterOverviewFolded(t *testing.T) {
	s, src := foldSession(t)
	ctx := context.Background()
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced }) // the view a connect opens
	lists := src.Lists()
	every, err := s.ClusterOverview(ctx, "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if n := src.Lists() - lists; n != 5 {
		t.Errorf("the unfolded read listed %d kinds, want the Overview's 5 besides the Pods view's", n)
	}
	m, err := s.ClusterOverviewFolded(ctx, "", nil, false, IssueQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if n := src.Lists() - lists; n != 6 {
		t.Errorf("the folded read listed %d kinds more, want the Replica Sets only", n-5)
	}
	// The Replica Sets are held for the read only: their informer stays warm, as after any list.
	if handlesOn(s, "replicasets") != 0 {
		t.Errorf("%d handles on the Replica Sets after the read", handlesOn(s, "replicasets"))
	}
	for _, st := range s.Backend().Cache.Stats() {
		if st.Scope.GVR == replicaSetsGVR && st.WarmUntil.IsZero() {
			t.Errorf("the Replica Sets' informer is not warm after the read: %+v", st)
		}
	}
	f := m.Fold
	if len(f.OwnersUnread) != 0 {
		t.Errorf("owners unread: %q", f.OwnersUnread)
	}
	if m.Issues != nil || f == nil || f.Issues != len(every.Issues) || f.Error+f.Warning != f.Issues {
		t.Fatalf("the fold counts %+v of %d issues", f, len(every.Issues))
	}
	by := map[string]map[string]string{}
	for _, r := range rowsOf(f.BySubject) {
		by[r["kind"]+"/"+r["namespace"]+"/"+r["name"]] = r
	}
	api := by["deployments/shop/api"]
	if api["objects"] != "3" || api["issues"] != "6" || api["level"] != "error" || api["reason"] != "BackOff" ||
		!strings.HasPrefix(api["state"], "CrashLoopBackOff") || strings.Contains(api["state"], "restarts") {
		t.Errorf("the Deployment's row: %v of %s", api, mustJSON(t, f.BySubject))
	}
	if forged := by["pods/shop/api-5d4-forged"]; forged["objects"] != "1" || forged["issues"] != "1" {
		t.Errorf("a pod under another UID: %v", forged)
	}
	// The fixture's own issues: a Deployment short of a pod, its pod without an owner reference, a
	// node, and an event about an object without a view.
	for _, key := range []string{"deployments/shop/web", "pods/shop/web-1", "nodes//worker-2", "pods/infra/db-0", "Widget/kube-system/gizmo"} {
		if by[key] == nil {
			t.Errorf("no subject %s in %s", key, mustJSON(t, f.BySubject))
		}
	}
	if len(by) != f.Subjects || f.Subjects != 7 {
		t.Errorf("%d subjects, %d rows", f.Subjects, len(by))
	}
	// A workload's own cause is the same whatever its count of ready pods, which its row's state says.
	var short map[string]string
	for _, c := range rowsOf(f.Causes) {
		if c["reason"] == reasonShortOfPods {
			short = c
		}
	}
	if short["source"] != IssueStatus || short["subjects"] != "1" || short["issues"] != "1" || by["deployments/shop/web"]["state"] != "1 of 2 pods ready" {
		t.Errorf("the cause of a workload short of pods: %v, its row %v", short, by["deployments/shop/web"])
	}

	// One subject's issues: the Overview's entries about its pods, an event's count beside it.
	q := IssueQuery{Subject: &IssueSubject{Kind: "deployments", Namespace: "shop", Name: "api"}, Sources: []string{IssueEvent}}
	if m, err = s.ClusterOverviewFolded(ctx, "", nil, false, q); err != nil {
		t.Fatal(err)
	}
	members := rowsOf(m.Fold.Members)
	if len(members) != 3 || members[0]["name"] != "api-5d4-a" || members[0]["message"] != "BackOff: BackOff happened" || members[0]["count"] != "2" ||
		members[0]["kind"] != "pods" || !strings.HasSuffix(members[0]["since"], "Z") {
		t.Errorf("the Deployment's events: %v", members)
	}
	// The namespaces narrow the fold as they narrow the issues.
	if m, err = s.ClusterOverviewFolded(ctx, "", []string{"infra"}, false, IssueQuery{}); err != nil || m.Fold.Subjects != 2 {
		t.Errorf("namespace infra: %v, %s", err, mustJSON(t, m.Fold))
	}
	if _, err = s.ClusterOverviewFolded(ctx, "", nil, false, IssueQuery{Severity: "fatal"}); err == nil {
		t.Error("a severity that is none was taken")
	}
	// The unfolded entries are as they were: nothing of the fold travels in them.
	raw := mustJSON(t, every)
	if strings.Contains(raw, "issueFold") || strings.Contains(raw, "controller") || !strings.Contains(raw, `"issues":[{"ref":{"kind":`) {
		t.Errorf("the unfolded model: %s", raw)
	}
}

// TestClusterOverviewFoldedReadsNoMoreKinds: without a pod that names a Replica Set, a Job or a Cron
// Job, the folded read lists what the unfolded one lists.
func TestClusterOverviewFoldedReadsNoMoreKinds(t *testing.T) {
	src := overviewFixture(time.Now())
	s := foldKindsSession(t, src, "replicasets", "jobs", "cronjobs")
	lists := src.Lists()
	m, err := s.ClusterOverviewFolded(context.Background(), "", nil, false, IssueQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if n := src.Lists() - lists; n != 5 || m.Fold.Issues != 6 || m.Fold.Subjects != 5 {
		t.Errorf("listed %d kinds for %d issues of %d subjects, want the Overview's 5 kinds, 6 issues, 5 subjects", n, m.Fold.Issues, m.Fold.Subjects)
	}

	// With the page open the fold is of the page's build.
	s.UpdateUI(func(u *UIState) { u.View = ClusterOverview })
	waitTable(t, s, ClusterOverview, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 6 })
	if m, err = s.ClusterOverviewFolded(context.Background(), "", nil, false, IssueQuery{}); err != nil || m.Fold == nil || m.Fold.Issues != 6 || m.Issues != nil {
		t.Errorf("with the page open: %v, %+v", err, m)
	}
}

// TestClusterOverviewFoldedOwnerChoosesNoWatch: an owner reference is written by whoever creates the
// pod, so it never chooses what Cogpit lists and watches. A pod with an issue whose controller is
// said to be a Secret or a Config Map starts no read of those kinds and stays its own subject. An
// owner of such a kind counts only where the cache holds it for another reader (here the Config
// Maps' view), under the UID the reference names.
func TestClusterOverviewFoldedOwnerChoosesNoWatch(t *testing.T) {
	src := overviewFixture(time.Now())
	src.Set(testkit.SecretsGVR, testkit.Secret("shop", "db", nil, map[string]string{"password": "first-pass-1234"}))
	src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "settings", nil))
	src.Set(testkit.PodsGVR, owned(crashLooping("shop", "by-secret"), "v1", "Secret", "db", "uid-shop-db"))
	src.Set(testkit.PodsGVR, owned(crashLooping("shop", "by-configmap"), "v1", "ConfigMap", "settings", "uid-shop-settings"))
	s := foldKindsSession(t, src, "replicasets", "jobs", "cronjobs", "secrets", "configmaps")
	lists := src.Lists()
	f, by := subjectsOf(t, s, nil, IssueQuery{})
	if n := src.Lists() - lists; n != 5 || src.OpenWatches(testkit.SecretsGVR) != 0 || src.OpenWatches(configMapsGVR) != 0 {
		t.Errorf("the folded read listed %d kinds (want the Overview's 5), with %d watches on Secrets and %d on Config Maps",
			n, src.OpenWatches(testkit.SecretsGVR), src.OpenWatches(configMapsGVR))
	}
	if by["pods/shop/by-secret"] == nil || by["pods/shop/by-configmap"] == nil || len(f.OwnersUnread) != 0 {
		t.Errorf("the pods are their own subjects: %s", mustJSON(t, f.BySubject))
	}

	s.UpdateUI(func(u *UIState) { u.View = "configmaps" })
	waitTable(t, s, "configmaps", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 1 })
	lists = src.Lists()
	_, by = subjectsOf(t, s, nil, IssueQuery{})
	if by["configmaps/shop/settings"] == nil || by["pods/shop/by-configmap"] != nil || by["pods/shop/by-secret"] == nil || src.Lists() != lists {
		t.Errorf("with the Config Maps' view open (%d lists more): %v", src.Lists()-lists, by)
	}
}

// TestClusterOverviewFoldedThroughAJob: a Cron Job's pod counts for the Cron Job through its Job,
// and so does a warning event about the Job; the Jobs and the Cron Jobs are read for it. A warning
// event about a Replica Set alone has the Replica Sets read too: the fold asks whether a Deployment
// controls it.
func TestClusterOverviewFoldedThroughAJob(t *testing.T) {
	now := time.Now()
	src := overviewFixture(now)
	src.Set(cronJobsGVR, testkit.Object("batch/v1", "CronJob", "shop", "nightly", nil))
	src.Set(jobsGVR, owned(testkit.Object("batch/v1", "Job", "shop", "nightly-123", nil), "batch/v1", "CronJob", "nightly", "uid-shop-nightly"))
	src.Set(testkit.PodsGVR, owned(crashLooping("shop", "nightly-123-abcde"), "batch/v1", "Job", "nightly-123", "uid-shop-nightly-123"))
	src.Set(testkit.EventsGVR, event("shop", "ej", "Job", "nightly-123", "", "BackoffLimitExceeded", "Warning", now.Add(-time.Minute)))
	s := foldKindsSession(t, src, "replicasets", "jobs", "cronjobs")
	lists := src.Lists()
	f, by := subjectsOf(t, s, nil, IssueQuery{})
	nightly := by["cronjobs/shop/nightly"]
	if nightly["objects"] != "2" || nightly["issues"] != "2" || nightly["level"] != "error" || nightly["reason"] != "BackoffLimitExceeded" ||
		by["pods/shop/nightly-123-abcde"] != nil || by["jobs/shop/nightly-123"] != nil || len(f.OwnersUnread) != 0 {
		t.Errorf("the Cron Job's row %v of %s", nightly, mustJSON(t, f.BySubject))
	}
	if n := src.Lists() - lists; n != 7 || src.OpenWatches(replicaSetsGVR) != 0 {
		t.Errorf("the folded read listed %d kinds, want the Overview's 5, the Jobs and the Cron Jobs", n)
	}
	if handlesOn(s, "jobs") != 0 || handlesOn(s, "cronjobs") != 0 {
		t.Errorf("handles after the read: %d on Jobs, %d on Cron Jobs", handlesOn(s, "jobs"), handlesOn(s, "cronjobs"))
	}

	src.Set(testkit.EventsGVR, event("shop", "ers", "ReplicaSet", "gone-5d4", "", "FailedCreate", "Warning", now.Add(-time.Minute)))
	waitFor(t, "the event about the Replica Set", func() bool {
		_, by = subjectsOf(t, s, nil, IssueQuery{})
		return by["replicasets/shop/gone-5d4"] != nil
	})
	if n := src.Lists() - lists; n != 8 {
		t.Errorf("with an event about a Replica Set %d kinds are listed, want the Replica Sets too", n)
	}
}

// slowListConnector connects to a cache over a source of the test's own.
type slowListConnector struct {
	*fakeConnector
	src cache.Source
}

func (c *slowListConnector) Connect(_ context.Context, name string) (*Backend, error) {
	ca := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	return &Backend{Context: name, Namespace: "default", Cache: ca, Profiles: c.profiles, Close: ca.Close}, nil
}

// TestClusterOverviewFoldedNamesOwnersUnread: where the Replica Sets' list does not arrive in the
// time the read has, the fold says so, and the pods a Replica Set controls are subjects of their
// own; once the list is there they count for their Deployment.
func TestClusterOverviewFoldedNamesOwnersUnread(t *testing.T) {
	_, src := foldSession(t)
	held, release := testkit.HoldList(src, replicaSetsGVR)
	t.Cleanup(release)
	s := New(Options{Connector: &slowListConnector{fakeConnector: &fakeConnector{profiles: foldProfiles(t, "replicasets")}, src: held}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read := make(chan *ClusterOverviewModel, 1)
	go func() {
		m, err := s.ClusterOverviewFolded(ctx, "", nil, false, IssueQuery{})
		if err != nil {
			t.Error(err)
		}
		read <- m
	}()
	waitFor(t, "the fold to wait for the Replica Sets", func() bool { return handlesOn(s, "replicasets") == 1 })
	cancel() // the read's time is over
	m := <-read
	if m == nil {
		t.FailNow()
	}
	if f := m.Fold; !slices.Equal(f.OwnersUnread, []string{"replicasets"}) || f.BySubject.Find("shop", "api-5d4-a") < 0 || f.Subjects != 9 {
		t.Fatalf("without the Replica Sets: unread %q, %d subjects: %s", f.OwnersUnread, f.Subjects, mustJSON(t, f.BySubject))
	}
	release()
	f, by := subjectsOf(t, s, nil, IssueQuery{})
	if len(f.OwnersUnread) != 0 || by["deployments/shop/api"]["objects"] != "3" || f.Subjects != 7 {
		t.Errorf("with the Replica Sets: unread %q, %s", f.OwnersUnread, mustJSON(t, f.BySubject))
	}
}

// TestClusterOverviewFoldedEventsNameNoCachedObject: what a warning event says its object is does
// not move a workload's row. Events about objects of the kind "pods" (no kind: it is "Pod") named
// like a Deployment's pods, and an event in another namespace about one of those pods, are subjects
// of their own; an event about an object with a name of 30 KiB is a row under a cut name, and
// following more reads every subject.
func TestClusterOverviewFoldedEventsNameNoCachedObject(t *testing.T) {
	s, src := foldSession(t)
	_, by := subjectsOf(t, s, nil, IssueQuery{})
	before := mustJSON(t, by["deployments/shop/api"])
	if by["deployments/shop/api"]["objects"] != "3" {
		t.Fatalf("the Deployment's row %s", before)
	}

	now := time.Now()
	for _, pod := range []string{"api-5d4-a", "api-5d4-bb", "api-5d4-ccc"} {
		src.Set(testkit.EventsGVR, event("shop", pod+".odd", "pods", pod, "", "Odd", "Warning", now))
	}
	foreign := event("sandbox", "x1", "Pod", "api-5d4-a", "", "Shouting", "Warning", now)
	_ = unstructured.SetNestedField(foreign.Object, "shop", "involvedObject", "namespace")
	_ = unstructured.SetNestedField(foreign.Object, "v1", "involvedObject", "apiVersion")
	src.Set(testkit.EventsGVR, foreign)
	long := strings.Repeat("n", 30<<10)
	src.Set(testkit.EventsGVR, event("shop", "long.1", "Gadget", long, "", "Broken", "Warning", now))
	src.Set(testkit.EventsGVR, event("shop", "long.2", "Pod", long, "", "Broken", "Warning", now))

	var f *IssueFold
	waitFor(t, "the events' subjects", func() bool {
		f, by = subjectsOf(t, s, []string{"shop"}, IssueQuery{Limit: FoldMaxLimit})
		return f.Subjects == 11
	})
	cut := long[:FoldNameBytes] + "…"
	for _, key := range []string{"pods (unknown kind)/shop/api-5d4-a", "pods (unknown kind)/shop/api-5d4-bb", "pods (unknown kind)/shop/api-5d4-ccc",
		"Pod/sandbox/api-5d4-a", "Gadget/shop/" + cut, "pods/shop/" + cut} {
		if by[key]["objects"] != "1" || by[key]["issues"] != "1" {
			t.Errorf("no subject of its own for %.60s in %d rows", key, len(by))
		}
	}
	if after := mustJSON(t, by["deployments/shop/api"]); after != before {
		t.Errorf("the Deployment's row %s, was %s", after, before)
	}

	// Page by page, as More says: every subject is read, each page under the bound.
	q := IssueQuery{Sources: []string{IssueEvent}, Limit: 2}
	var names []string
	for {
		m, err := s.ClusterOverviewFolded(context.Background(), "", []string{"shop"}, false, q)
		if err != nil {
			t.Fatal(err)
		}
		table := m.Fold.BySubject
		if size := len(mustJSON(t, m.Fold)); size > FoldBytes || len(table.Rows) == 0 {
			t.Fatalf("offset %d: %d bytes, %d rows, more %q", q.Offset, size, len(table.Rows), table.More)
		}
		for i := range table.Rows {
			names = append(names, table.Cell(i, "name"))
		}
		q.Offset += len(table.Rows)
		if table.More == "" {
			if len(names) != table.Total || !slices.Contains(names, cut) {
				t.Errorf("read %d subjects of %d", len(names), table.Total)
			}
			break
		}
		if table.More != fmt.Sprintf("%d more: offset %d", table.Total-q.Offset, q.Offset) {
			t.Fatalf("more %q at offset %d of %d", table.More, q.Offset, table.Total)
		}
	}
}
