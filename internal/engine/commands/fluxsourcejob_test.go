package commands_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Reconcile with Source waits as the flux CLI does (A91): a job asks each object of the chain in
// turn and waits for its controller, through the cache.

// fakeFluxController plays Flux's controllers: an object asked to reconcile gets
// status.lastHandledReconcileAt (the time it handles it) and its verdict, Ready True unless
// notReady names a message for it; an object it holds is handled only when released.
type fakeFluxController struct {
	h        *harness
	mu       sync.Mutex
	notReady map[string]string // name → the Ready message of a Ready False verdict
	held     map[string]bool
	pending  map[string]schema.GroupVersionResource // held objects asked to reconcile, by "namespace/name"
}

func newFakeFluxController(h *harness, w *fluxWriter) *fakeFluxController {
	c := &fakeFluxController{h: h, notReady: map[string]string{}, held: map[string]bool{}, pending: map[string]schema.GroupVersionResource{}}
	w.mu.Lock()
	w.onPatch = c.asked
	w.mu.Unlock()
	return c
}

func (c *fakeFluxController) hold(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held[name] = true
}

func (c *fakeFluxController) failWith(name, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notReady[name] = message
}

// release handles the held object of that name if it was asked, and every request for it after.
func (c *fakeFluxController) release(name string) {
	c.mu.Lock()
	delete(c.held, name)
	var asked []string
	for key := range c.pending {
		if strings.HasSuffix(key, "/"+name) {
			asked = append(asked, key)
		}
	}
	gvrs := map[string]schema.GroupVersionResource{}
	for _, key := range asked {
		gvrs[key] = c.pending[key]
		delete(c.pending, key)
	}
	c.mu.Unlock()
	for key, gvr := range gvrs {
		ns, n, _ := strings.Cut(key, "/")
		c.handle(gvr, ns, n)
	}
}

func (c *fakeFluxController) asked(gvr schema.GroupVersionResource, namespace, name string) {
	c.mu.Lock()
	if c.held[name] {
		c.pending[namespace+"/"+name] = gvr
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.handle(gvr, namespace, name)
}

// handle writes the controller's verdict into the cluster.
func (c *fakeFluxController) handle(gvr schema.GroupVersionResource, namespace, name string) {
	obj, err := c.h.s.Lookup(model.ObjectRef{Kind: gvr.Resource + "." + gvr.Group, Namespace: namespace, Name: name})
	if err != nil {
		return
	}
	c.mu.Lock()
	msg, failed := c.notReady[name]
	c.mu.Unlock()
	ready, text := "True", "Applied revision: main@sha1:new"
	if failed {
		ready, text = "False", msg
	}
	obj = obj.DeepCopy()
	obj.Object["status"] = map[string]any{
		"lastHandledReconcileAt": time.Now().UTC().Format(time.RFC3339Nano),
		"conditions":             []any{map[string]any{"type": "Ready", "status": ready, "message": text}},
	}
	c.h.src.Set(gvr, obj)
}

// waitJob waits until a job tab's job ended and returns it.
func waitJob(t *testing.T, h *harness, id string) *engine.JobTab {
	t.Helper()
	var job *engine.JobTab
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		tab, ok := h.s.UIState().DockTab(id)
		job = tab.Job
		return ok && job != nil && job.State.Ended()
	}) {
		t.Fatalf("job %s never ended: %+v", id, job)
	}
	return job
}

// jobOf returns a job tab's job as the UI state holds it now.
func jobOf(h *harness, id string) *engine.JobTab {
	tab, _ := h.s.UIState().DockTab(id)
	return tab.Job
}

// stepStates writes a job's steps as "state text: detail".
func stepStates(j *engine.JobTab) []string {
	var out []string
	for _, st := range j.Steps {
		out = append(out, string(st.State)+" "+st.Text+": "+st.Detail)
	}
	return out
}

// TestFluxReconcileWithSourceWaitsForTheSource (A91): the command returns at once with the job's
// tab, active in the dock; the object is not asked until the source's controller has handled the
// request and reports it ready; then it is asked and waited for, and the job ends done with a toast.
func TestFluxReconcileWithSourceWaitsForTheSource(t *testing.T) {
	h, w := newFluxHarnessOn(t, engine.Options{}, fluxSourcesSource())
	ctl := newFakeFluxController(h, w)
	ctl.hold("flux-system")
	toasts := recordToasts(h.s)
	var res commands.FluxSourceJobResult
	h.must(t, engine.OriginGUI, "flux.reconcileWithSource", map[string]any{"ref": ksRef("apps")}, &res)
	if res.Job.Type != engine.DockJob || h.s.UIState().ActiveDock != res.Job.ID || res.Job.Title != "Reconcile with Source: Kustomization flux-system/apps" {
		t.Errorf("job tab %+v", res.Job)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		j := jobOf(h, res.Job.ID)
		return j != nil && j.Steps[0].State == engine.JobRunning && strings.HasPrefix(j.Steps[0].Detail, "Waiting for its controller: Ready")
	}) {
		t.Fatalf("the source's step %v", stepStates(jobOf(h, res.Job.ID)))
	}
	time.Sleep(30 * time.Millisecond) // the object must not be asked meanwhile
	if got := w.Patches(); len(got) != 1 || !strings.HasPrefix(got[0], "gitrepositories/") {
		t.Fatalf("patches while the source is reconciled: %v", got)
	}
	if j := jobOf(h, res.Job.ID); j.Steps[1].State != engine.JobPending || j.State != engine.JobRunning {
		t.Errorf("the object's step %v", stepStates(j))
	}

	ctl.release("flux-system")
	j := waitJob(t, h, res.Job.ID)
	if j.State != engine.JobDone || j.Outcome != "Reconciled with its source: 2 objects ready." || j.Command != "flux.reconcileWithSource" {
		t.Errorf("job %+v", j)
	}
	if got := stepStates(j); got[0] != "done GitRepository flux-system/flux-system: Ready: Applied revision: main@sha1:new" ||
		got[1] != "done Kustomization flux-system/apps: Ready: Applied revision: main@sha1:new" {
		t.Errorf("steps %q", got)
	}
	if got := w.Patches(); len(got) != 2 || !strings.HasPrefix(got[1], "kustomizations/v1 flux-system/apps") {
		t.Errorf("patches %v", got)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if !testkit.WaitFor(ctx2, func() bool {
		return slices.ContainsFunc(toasts(), func(m string) bool {
			return m == "info Reconcile with Source: Kustomization flux-system/apps: Reconciled with its source: 2 objects ready."
		})
	}) {
		t.Errorf("toasts %q", toasts())
	}
}

// recordToasts returns what lists the toasts emitted since, as "level message".
func recordToasts(s *engine.Session) func() []string {
	var mu sync.Mutex
	var got []string
	s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventToast {
			mu.Lock()
			got = append(got, ev.Level+" "+ev.Message)
			mu.Unlock()
		}
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

// TestFluxReconcileWithSourceStopsAtASourceNotReady (A91): a source its controller reports not
// ready stops the job before the object, as the flux CLI stops: the object is never asked, and
// the steps say why.
func TestFluxReconcileWithSourceStopsAtASourceNotReady(t *testing.T) {
	h, w := newFluxHarnessOn(t, engine.Options{}, fluxSourcesSource())
	ctl := newFakeFluxController(h, w)
	ctl.failWith("podinfo", "failed to fetch index: 401 Unauthorized")
	var res commands.FluxSourceJobResult
	h.must(t, engine.OriginGUI, "flux.reconcileWithSource", map[string]any{"ref": hrRef()}, &res)
	j := waitJob(t, h, res.Job.ID)
	got := stepStates(j)
	want := []string{
		"failed HelmRepository flux-system/podinfo: Not ready: failed to fetch index: 401 Unauthorized",
		"failed HelmChart flux-system/shop-podinfo: Not asked to reconcile: its source HelmRepository flux-system/podinfo is not ready: failed to fetch index: 401 Unauthorized",
		"failed HelmRelease shop/podinfo: Not asked to reconcile: its source HelmChart flux-system/shop-podinfo was not asked to reconcile either",
	}
	if !slices.Equal(got, want) {
		t.Errorf("steps\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if j.State != engine.JobFailed || j.Outcome != "0 of 3 objects reconciled and ready; the steps say why the others are not." {
		t.Errorf("job %+v", j)
	}
	if p := w.Patches(); len(p) != 1 || !strings.HasPrefix(p[0], "helmrepositories/") {
		t.Errorf("patches %v", p)
	}
}

// TestFluxReconcileWithSourceTimesOut (A91): a controller that does not finish within the wait
// fails the step with what it last showed, and the object is not asked.
func TestFluxReconcileWithSourceTimesOut(t *testing.T) {
	t.Cleanup(commands.SetFluxJobWait(150 * time.Millisecond))
	h, w := newFluxHarnessOn(t, engine.Options{}, fluxSourcesSource())
	ctl := newFakeFluxController(h, w)
	ctl.hold("platform")
	var res commands.FluxSourceJobResult
	h.must(t, engine.OriginGUI, "flux.reconcileWithSource", map[string]any{"ref": ksRef("infra")}, &res)
	j := waitJob(t, h, res.Job.ID)
	if got := stepStates(j); got[0] != "failed OCIRepository platform/platform: Its controller did not finish within 150ms; last seen: Ready: m" ||
		got[1] != "failed Kustomization flux-system/infra: Not asked to reconcile: its source OCIRepository platform/platform was not ready within 150ms" {
		t.Errorf("steps %q", got)
	}
	if p := w.Patches(); len(p) != 1 {
		t.Errorf("patches %v", p)
	}
}

// TestFluxReconcileWithSourceStopsWithItsTab (A91): closing the job's tab stops the wait; the
// object is never asked, even when the source's controller answers afterwards. What was asked
// stays asked.
func TestFluxReconcileWithSourceStopsWithItsTab(t *testing.T) {
	h, w := newFluxHarnessOn(t, engine.Options{}, fluxSourcesSource())
	ctl := newFakeFluxController(h, w)
	ctl.hold("flux-system")
	var res commands.FluxSourceJobResult
	h.must(t, engine.OriginGUI, "flux.reconcileWithSource", map[string]any{"ref": ksRef("apps")}, &res)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return len(w.Patches()) == 1 }) {
		t.Fatalf("the source was not asked: %v", w.Patches())
	}
	h.must(t, engine.OriginGUI, "dock.close", map[string]any{"id": res.Job.ID}, nil)
	ctl.release("flux-system")
	// The source's controller answered and the cache shows it: a job still waiting would ask the
	// object now.
	if !testkit.WaitFor(ctx, func() bool {
		obj, err := h.s.Lookup(model.ObjectRef{Kind: "gitrepositories.source.toolkit.fluxcd.io", Namespace: "flux-system", Name: "flux-system"})
		return err == nil && model.NestedString(obj.Object, "status", "lastHandledReconcileAt") != ""
	}) {
		t.Fatal("the source's answer never reached the cache")
	}
	time.Sleep(100 * time.Millisecond)
	if p := w.Patches(); len(p) != 1 {
		t.Errorf("patched after the tab closed: %v", p)
	}
	if _, ok := h.s.UIState().DockTab(res.Job.ID); ok {
		t.Error("the tab is still open")
	}
}

// TestFluxReconcileWithSourceJobForAgents (A91, D-2): an agent's Reconcile with Source still asks
// the user first; its job's progress is in ui_state, as the window shows it.
func TestFluxReconcileWithSourceJobForAgents(t *testing.T) {
	asked := 0
	h, w := newFluxHarnessOn(t, engine.Options{Audit: &syncBuffer{}, Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) {
		asked++
		return true, nil
	})}, fluxSourcesSource())
	newFakeFluxController(h, w)
	var res commands.FluxSourceJobResult
	h.must(t, engine.OriginExternalAgent, "flux.reconcileWithSource", map[string]any{"ref": ksRef("apps")}, &res)
	if asked != 1 {
		t.Errorf("asked %d times", asked)
	}
	waitJob(t, h, res.Job.ID)
	var state json.RawMessage
	h.must(t, engine.OriginExternalAgent, "ui.state", map[string]any{}, &state)
	var u struct {
		Dock []struct {
			ID  string `json:"id"`
			Job *struct {
				Command string `json:"command"`
				State   string `json:"state"`
				Outcome string `json:"outcome"`
				Steps   []struct {
					State, Text, Detail string
				} `json:"steps"`
			} `json:"job"`
		} `json:"dock"`
	}
	if err := json.Unmarshal(state, &u); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(u.Dock, func(d struct {
		ID  string `json:"id"`
		Job *struct {
			Command string `json:"command"`
			State   string `json:"state"`
			Outcome string `json:"outcome"`
			Steps   []struct {
				State, Text, Detail string
			} `json:"steps"`
		} `json:"job"`
	}) bool {
		return d.ID == res.Job.ID
	})
	if i < 0 || u.Dock[i].Job == nil || u.Dock[i].Job.State != "done" || u.Dock[i].Job.Command != "flux.reconcileWithSource" ||
		len(u.Dock[i].Job.Steps) != 2 || u.Dock[i].Job.Steps[1].Text != "Kustomization flux-system/apps" {
		t.Errorf("ui_state's dock %s", state)
	}
}

// TestFluxReconcileWithSourceJobOnASelection (A91): one job for a selection: the chains side by
// side, a source two objects share asked once and waited for by both, the objects that cannot be
// reconciled skipped with why; one object's failure leaves the others.
func TestFluxReconcileWithSourceJobOnASelection(t *testing.T) {
	h, w := newFluxHarnessOn(t, engine.Options{}, fluxSourcesSource())
	ctl := newFakeFluxController(h, w)
	ctl.failWith("infra", "kustomize build failed")
	ctl.hold("flux-system")
	var res commands.FluxSourceJobResult
	h.must(t, engine.OriginGUI, "flux.reconcileWithSourceMany", map[string]any{"refs": []model.ObjectRef{ksRef("apps"), ksRef("flux-system"), ksRef("held"), ksRef("infra")}}, &res)
	if len(res.Skipped) != 1 || res.Skipped[0].Ref.Name != "held" || len(res.Targets) != 5 || res.Job.Title != "Reconcile with Source: 4 objects" {
		t.Errorf("result %+v", res)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The OCI chain goes on while the Git repository is held: one chain never waits for another.
	if !testkit.WaitFor(ctx, func() bool {
		j := jobOf(h, res.Job.ID)
		return j != nil && j.Steps[len(j.Steps)-1].State == engine.JobFailed
	}) {
		t.Fatalf("the OCI chain waited for the Git repository: %q", stepStates(jobOf(h, res.Job.ID)))
	}
	if j := jobOf(h, res.Job.ID); j.Steps[0].State != engine.JobRunning || j.Steps[1].State != engine.JobPending {
		t.Errorf("the Git chain while held: %q", stepStates(j))
	}
	ctl.release("flux-system")
	j := waitJob(t, h, res.Job.ID)
	got := stepStates(j)
	want := []string{
		"done GitRepository flux-system/flux-system: Ready: Applied revision: main@sha1:new",
		"done Kustomization flux-system/apps: Ready: Applied revision: main@sha1:new",
		"done Kustomization flux-system/flux-system: Ready: Applied revision: main@sha1:new",
		"failed Kustomization flux-system/held: Not reconciled: its source " + gitKey + "/flux-system/held is suspended: resume it to reconcile with it",
		"done OCIRepository platform/platform: Ready: Applied revision: main@sha1:new",
		"failed Kustomization flux-system/infra: Not ready: kustomize build failed",
	}
	if !slices.Equal(got, want) {
		t.Errorf("steps\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if j.State != engine.JobFailed || j.Outcome != "4 of 6 objects reconciled and ready; the steps say why the others are not." {
		t.Errorf("job %+v", j)
	}
	git := 0
	for _, p := range w.Patches() {
		if strings.HasPrefix(p, "gitrepositories/") {
			git++
		}
	}
	if git != 1 || len(w.Patches()) != 5 {
		t.Errorf("patches %v", w.Patches())
	}
}
