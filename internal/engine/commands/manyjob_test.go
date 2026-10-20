package commands_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Actions on a selection of more than engine.DirectSelection objects run as background jobs (G4,
// manyjob.go).

// jobIn returns a job tab's job as the UI state holds it.
func jobIn(s *engine.Session, id string) *engine.JobTab {
	if t, ok := s.UIState().DockTab(id); ok {
		return t.Job
	}
	return nil
}

// waitJobEnd waits until the job of tab id ended and returns it.
func waitJobEnd(t *testing.T, s *engine.Session, id string) *engine.JobTab {
	t.Helper()
	waitUntil(t, "the job's end", func() bool { j := jobIn(s, id); return j != nil && j.State.Ended() })
	return jobIn(s, id)
}

// manyServices puts n Services s00, s01, … into namespace shop and waits until the cache holds
// them.
func manyServices(t *testing.T, h *harness, n int) []model.ObjectRef {
	t.Helper()
	refs := make([]model.ObjectRef, n)
	for i := range refs {
		name := fmt.Sprintf("s%02d", i)
		h.src.Set(testkit.ServicesGVR, testkit.Service("shop", name, fmt.Sprintf("10.96.8.%d", i+1)))
		refs[i] = model.ObjectRef{Kind: "services", Namespace: "shop", Name: name}
	}
	for _, r := range refs {
		cachedRV(t, h, r, "")
	}
	return refs
}

// TestDeleteManyAsAJob (G4): Delete of more than ten selected objects asks once, as before, then
// returns at once with a job's dock tab whose steps are the objects: the window and agents see
// each object's progress (a step under way, the counts), a failure stays that object's while the
// others go on, the outcome counts both, the deleted objects leave the selection and a toast says
// how it ended.
func TestDeleteManyAsAJob(t *testing.T) {
	conf := &recorder{answer: true}
	h, srv := editHarness(t, engine.Options{Confirmer: conf})
	toasts := recordToasts(h.s)
	refs := manyServices(t, h, 12)
	h.must(t, engine.OriginGUI, "ui.openView", map[string]any{"view": "services"}, nil)
	h.must(t, engine.OriginGUI, "ui.select", map[string]any{"refs": refs, "mode": "add", "noDrawer": true}, nil)
	conf.during = func(engine.ConfirmRequest) { // s05 created again under its name while the dialog is open
		again := testkit.Service("shop", "s05", "10.96.8.99")
		again.SetUID("uid-shop-s05-again")
		h.src.Set(testkit.ServicesGVR, again)
	}
	held, release := make(chan struct{}), make(chan struct{})
	srv.beforeDelete = func(_, name string) {
		if name == "s03" {
			close(held)
			<-release
		}
	}

	var res commands.ManyJobResult
	h.must(t, engine.OriginGUI, "object.deleteMany", map[string]any{"refs": refs}, &res)
	if reqs := conf.requests(); len(reqs) != 1 || len(reqs[0].Objects) != 12 || len(reqs[0].UIDs) != 12 {
		t.Fatalf("confirmations %+v", reqs)
	}
	if res.Action != "job started" || res.Job.Type != engine.DockJob || res.Job.Title != "Delete: 12 objects" || len(res.Targets) != 12 ||
		res.Job.Job == nil || len(res.Job.Job.Steps) != 0 || res.Job.Job.Counts.Total != 12 { // the steps are the targets
		t.Fatalf("result %+v", res)
	}
	if u := h.s.UIState(); u.ActiveDock != res.Job.ID || !u.DockOpen {
		t.Errorf("the job's tab is not shown: active %q, open %v", u.ActiveDock, u.DockOpen)
	}

	// Under way: three deleted, the fourth's request sent, the rest waiting.
	<-held
	waitUntil(t, "s03 under way", func() bool {
		j := jobIn(h.s, res.Job.ID)
		return j != nil && j.Steps[3].State == engine.JobRunning
	})
	j := jobIn(h.s, res.Job.ID)
	if j.Counts != (engine.JobCounts{Total: 12, Done: 3, Running: 1, Pending: 8}) || j.Steps[0].Text != "Service shop/s00" ||
		j.Steps[0].Detail != "Deleted" || j.Steps[0].Ref == nil || j.Command != "object.deleteMany" {
		t.Errorf("under way %+v", j)
	}
	close(release)

	j = waitJobEnd(t, h.s, res.Job.ID)
	if j.State != engine.JobFailed || j.Outcome != "Deleted: 11 of 12 objects; 1 failed: the job's steps say why." ||
		j.Counts != (engine.JobCounts{Total: 12, Done: 11, Failed: 1}) {
		t.Errorf("ended %+v", j)
	}
	if st := j.Steps[5]; st.State != engine.JobFailed || !strings.Contains(st.Detail, "uid-shop-s05") {
		t.Errorf("the replaced s05: %+v", st)
	}
	srv.mu.Lock()
	n := len(srv.deletes)
	srv.mu.Unlock()
	if n != 12 {
		t.Errorf("%d delete requests, want one each", n)
	}
	if u := h.s.UIState(); !slices.Equal(refNames(u.Selected), []string{"shop/s05"}) {
		t.Errorf("selected after the job: %v (the deleted ones leave)", refNames(u.Selected))
	}
	waitUntil(t, "the toast", func() bool {
		return slices.Contains(toasts(), engine.LevelError+" Deleted: 11 of 12 objects; 1 failed: the job's steps say why")
	})
}

// TestDeleteManyJobCancelled (G4): an agent's Delete of more than ten objects is confirmed by the
// user and runs as a job; job.cancel (any origin, here an agent) lets the request under way finish
// and attempts none after it; the outcome says who cancelled it, what was deleted and what was
// not; the audit log has the command's "requested" and "done" lines and the job's "ended" line.
// A job that ended cannot be cancelled.
func TestDeleteManyJobCancelled(t *testing.T) {
	conf := &recorder{answer: true}
	audit := &syncBuffer{}
	h, srv := editHarness(t, engine.Options{Confirmer: conf, Audit: audit})
	refs := manyServices(t, h, 12)
	cancelled := make(chan error, 1)
	srv.beforeDelete = func(_, name string) {
		if name != "s02" {
			return
		}
		// Cancelled while s02's request is under way; the call returns at once (its context ended).
		id := h.s.UIState().ActiveDock
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		raw, _ := json.Marshal(map[string]any{"id": id})
		_, err := h.s.Dispatch(ctx, engine.Invocation{ID: "job.cancel", Params: raw, Origin: engine.OriginExternalAgent})
		cancelled <- err
	}

	var res commands.ManyJobResult
	h.must(t, engine.OriginExternalAgent, "object.deleteMany", map[string]any{"refs": refs}, &res)
	if err := <-cancelled; err != nil {
		t.Fatalf("job.cancel: %v", err)
	}
	j := waitJobEnd(t, h.s, res.Job.ID)
	want := "Stopped: cancelled by an agent. Deleted: 3 of 12 objects; 9 not attempted: the job's steps say why."
	if j.State != engine.JobCancelled || j.Outcome != want || j.Counts != (engine.JobCounts{Total: 12, Done: 3, Cancelled: 9}) {
		t.Errorf("cancelled %+v", j)
	}
	if st := j.Steps[3]; st.State != engine.JobCancelled || st.Detail != "Not attempted: cancelled by an agent" {
		t.Errorf("a step after the cancel: %+v", st)
	}
	srv.mu.Lock()
	deletes := slices.Clone(srv.deletes)
	srv.mu.Unlock()
	if !slices.Equal(deletes, []string{"shop/s00 uid-shop-s00", "shop/s01 uid-shop-s01", "shop/s02 uid-shop-s02"}) {
		t.Errorf("deletes %v", deletes)
	}
	for _, r := range refs[3:] {
		if _, err := h.s.Lookup(r); err != nil {
			t.Errorf("%s not attempted but gone: %v", r.Name, err)
		}
	}

	waitUntil(t, "the ended line", func() bool {
		return slices.ContainsFunc(audit.Lines(), func(l engine.AuditEntry) bool { return l.Command == "object.deleteMany" && l.Phase == "ended" })
	})
	var phases []string
	for _, l := range audit.Lines() {
		if l.Command == "object.deleteMany" {
			phases = append(phases, fmt.Sprintf("%s %v %s", l.Phase, l.OK, l.Error))
		}
	}
	// The request comes first; the dispatch's done and the job's end in either order: the job runs
	// on and can be cancelled (at s02) before the dispatch has written its done line (CI, 2026-10-20).
	wantPhases := []string{"requested true ", "done true ", "ended false " + want}
	sorted := slices.Clone(phases)
	if len(sorted) > 1 {
		slices.Sort(sorted[1:])
	}
	if !slices.Equal(sorted, wantPhases) {
		t.Errorf("audit %q, want %q (done and ended in either order)", phases, wantPhases)
	}

	err := h.run(t, engine.OriginGUI, "job.cancel", map[string]any{"id": res.Job.ID}, nil)
	if err == nil || !strings.Contains(err.Error(), "the job has ended") {
		t.Errorf("cancelling an ended job: %v", err)
	}
}

// manyDeployments puts n Deployments d00, d01, … into namespace shop and waits for the cache.
func manyDeployments(t *testing.T, h *harness, n int) []model.ObjectRef {
	t.Helper()
	refs := make([]model.ObjectRef, n)
	for i := range refs {
		name := fmt.Sprintf("d%02d", i)
		h.src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", name, nil))
		refs[i] = model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: name}
	}
	for _, r := range refs {
		waitUntil(t, r.Name+" in the cache", func() bool { _, err := h.s.Lookup(r); return err == nil })
	}
	return refs
}

// TestRestartManyDirectOrJob (G4): Restart of at most ten workloads returns what it restarted, as
// before; of eleven it starts a job, which restarts each bound to its confirmed UID.
func TestRestartManyDirectOrJob(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{Confirmer: &recorder{answer: true}})
	refs := manyDeployments(t, h, engine.DirectSelection+1)
	var direct commands.RestartManyResult
	h.must(t, engine.OriginGUI, "workload.restartMany", map[string]any{"refs": refs[:engine.DirectSelection]}, &direct)
	if len(direct.Restarted) != engine.DirectSelection {
		t.Errorf("ten restarted %+v", direct)
	}
	var res commands.ManyJobResult
	h.must(t, engine.OriginGUI, "workload.restartMany", map[string]any{"refs": refs}, &res)
	j := waitJobEnd(t, h.s, res.Job.ID)
	if j.State != engine.JobDone || j.Outcome != "Restarted: 11 of 11 workloads." || res.Job.Title != "Restart: 11 workloads" ||
		j.Steps[10].Text != "Deployment shop/d10" || j.Steps[10].Detail != "Restarted" {
		t.Errorf("job %+v", j)
	}
	n := 0
	for _, c := range h.writer.Calls() {
		if strings.HasPrefix(c, "restart deployments shop/d") {
			n++
		}
	}
	if n != 2*engine.DirectSelection+1 {
		t.Errorf("%d restarts: %v", n, h.writer.Calls())
	}
}

// TestFluxManyAsAJob (G4): Reconcile of more than ten Flux objects runs as a job of the objects it
// applies to; a suspended one is left out before, as within a call.
func TestFluxManyAsAJob(t *testing.T) {
	src := fluxSource()
	refs := []model.ObjectRef{ksRef("paused")}
	for i := range engine.DirectSelection + 1 {
		name := fmt.Sprintf("k%02d", i)
		src.Set(ksGVR, fluxObject("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", name, false, "True"))
		refs = append(refs, ksRef(name))
	}
	h, w := newFluxHarnessOn(t, engine.Options{}, src)
	for _, r := range refs {
		waitUntil(t, r.Name+" in the cache", func() bool { _, err := h.s.Lookup(r); return err == nil })
	}
	var res commands.ManyJobResult
	h.must(t, engine.OriginGUI, "flux.reconcileMany", map[string]any{"refs": refs}, &res)
	j := waitJobEnd(t, h.s, res.Job.ID)
	if j.State != engine.JobDone || j.Outcome != "Reconcile requested: 11 of 11 Flux objects." || len(j.Steps) != 11 ||
		res.Job.Title != "Reconcile: 11 Flux objects" || j.Steps[0].Text != "Kustomization flux-system/k00" {
		t.Errorf("job %+v", j)
	}
	if n := len(w.Patches()); n != 11 {
		t.Errorf("%d patches", n)
	}
}

// TestArgoManyAsAJob (G4): Sync of more than ten Applications runs as a job; one with an
// operation under way fails its own step, the others are synced.
func TestArgoManyAsAJob(t *testing.T) {
	h := newArgoHarness(t)
	var refs []model.ObjectRef
	for i := range engine.DirectSelection {
		refs = append(refs, h.app(t, nil, fmt.Sprintf("a%02d", i)))
	}
	refs = append(refs, h.app(t, map[string]any{"status": map[string]any{"operationState": map[string]any{"phase": "Running"}}}, "busy"))
	res, err := h.dispatch(engine.OriginGUI, "argocd.application.syncMany", map[string]any{"refs": refs})
	if err != nil {
		t.Fatal(err)
	}
	tab := res.(commands.ManyJobResult).Job
	j := waitJobEnd(t, h.s, tab.ID)
	if j.State != engine.JobFailed || j.Outcome != "Sync started: 10 of 11 applications; 1 failed: the job's steps say why." ||
		j.Steps[10].State != engine.JobFailed || !strings.Contains(j.Steps[10].Detail, "in progress") {
		t.Errorf("job %+v", j)
	}
	syncs := 0
	for _, w := range h.writer.Writes() {
		if w.op == "sync" {
			syncs++
		}
	}
	if syncs != 10 {
		t.Errorf("%d syncs", syncs)
	}
}
