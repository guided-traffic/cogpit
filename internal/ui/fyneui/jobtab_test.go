package fyneui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// reconcileJob is Reconcile with Source's job tab (A91) as the engine holds it while the source
// is done and the Kustomization is waited for; a third object's source failed.
func reconcileJob() engine.DockTab {
	git := model.ObjectRef{Context: "demo", Kind: "gitrepositories.source.toolkit.fluxcd.io", Namespace: "flux-system", Name: "flux-system"}
	ks := model.ObjectRef{Context: "demo", Kind: "kustomizations.kustomize.toolkit.fluxcd.io", Namespace: "flux-system", Name: "apps"}
	oci := model.ObjectRef{Context: "demo", Kind: "ocirepositories.source.toolkit.fluxcd.io", Namespace: "platform", Name: "platform"}
	infra := model.ObjectRef{Context: "demo", Kind: "kustomizations.kustomize.toolkit.fluxcd.io", Namespace: "flux-system", Name: "infra"}
	return engine.DockTab{
		ID: "job-1", Type: engine.DockJob, Title: "Reconcile with Source: 2 objects", Context: "demo",
		Job: &engine.JobTab{
			Command: "flux.reconcileWithSourceMany", State: engine.JobRunning, Started: fixedNow.Add(-40 * time.Second),
			Steps: []engine.JobStep{
				{Ref: &git, Text: "GitRepository flux-system/flux-system", State: engine.JobDone, Detail: "Ready: stored artifact for revision 'main@sha1:7a1e2c4'"},
				{Ref: &ks, Text: "Kustomization flux-system/apps", State: engine.JobRunning, Detail: "Waiting for its controller: Reconciling: Detecting drift for revision main@sha1:7a1e2c4"},
				{Ref: &oci, Text: "OCIRepository platform/platform", State: engine.JobFailed, Detail: "Not ready: failed to pull artifact: 401 Unauthorized"},
				{Ref: &infra, Text: "Kustomization flux-system/infra", State: engine.JobFailed, Detail: "Not asked to reconcile: its source OCIRepository platform/platform is not ready: failed to pull artifact: 401 Unauthorized"},
			},
		},
	}
}

// jobTabOf returns the dock's job tab of that id.
func jobTabOf(sh *shell, id string) *jobTab {
	if c, ok := sh.dock.contents[id]; ok {
		if j, ok := c.editor.(*jobTab); ok {
			return j
		}
	}
	return nil
}

// TestJobTabFollowsTheEngine (A91): a job tab shows the job's state and steps as the engine holds
// them, each in its state's colour, and follows them as they move, whoever moved them; the tab's
// close button closes it through dock.close, which stops the job.
func TestJobTabFollowsTheEngine(t *testing.T) {
	f := connectedFake(t, 20)
	f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{reconcileJob()}, "job-1"
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the job tab", func() bool { return jobTabOf(sh, "job-1") != nil })
	j := jobTabOf(sh, "job-1")
	if j.state.text != "Running" || j.state.color != colorAccent || len(j.rows) != 4 {
		t.Fatalf("header %q, %d rows", j.state.text, len(j.rows))
	}
	for i, want := range []struct {
		text  string
		color string
	}{
		{"GitRepository flux-system/flux-system", string(colorMuted)},
		{"Kustomization flux-system/apps", string(colorAccent)},
		{"OCIRepository platform/platform", string(colorStatusError)},
		{"Kustomization flux-system/infra", string(colorStatusError)},
	} {
		if r := j.rows[i]; r.text.text != want.text || string(r.detail.color) != want.color {
			t.Errorf("row %d: %q in %s", i, r.text.text, r.detail.color)
		}
	}

	// The engine ends the job.
	f.update(func(u *engine.UIState) {
		job := u.Dock[0].Job
		job.Steps[1].State, job.Steps[1].Detail = engine.JobDone, "Ready: Applied revision: main@sha1:7a1e2c4"
		job.State, job.Ended, job.Outcome = engine.JobFailed, fixedNow, "2 of 4 objects reconciled and ready; the steps say why the others are not."
	})
	q.until(t, "the end", func() bool { return j.state.text == "Failed" })
	if j.outcome.text != "2 of 4 objects reconciled and ready; the steps say why the others are not." || j.outcome.color != colorStatusError ||
		j.rows[1].detail.text != "Ready: Applied revision: main@sha1:7a1e2c4" {
		t.Errorf("after the end: %q, row %q", j.outcome.text, j.rows[1].detail.text)
	}

	// Closing the tab asks the engine.
	b := sh.dock.tabRow.Objects[0].(*tabButton)
	b.close.OnTapped()
	q.until(t, "dock.close", func() bool { return len(f.callsOf("dock.close")) == 1 })
}

// TestRenderJobTab: Reconcile with Source's job tab in the dock while it waits for a
// Kustomization's controller, dark and light (A91).
func TestRenderJobTab(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		f := connectedFake(t, 200)
		f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{reconcileJob()}, "job-1"
		sh, w, q := newTestShell(t, f, Options{Theme: mode})
		q.until(t, "the job tab", func() bool { return jobTabOf(sh, "job-1") != nil && sh.dock.open() })
		q.drain()
		savePNG(t, w, "job_tab_"+string(mode)+".png")
	}
}

// deleteJob is Delete of 24 selected pods as a job (G4) while its eleventh step is under way: ten
// steps ended, one of them failed, the rest waiting.
func deleteJob() engine.DockTab {
	steps := make([]engine.JobStep, 24)
	for i := range steps {
		ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: "shop", Name: fmt.Sprintf("web-7d9f-%02d", i)}
		steps[i] = engine.JobStep{Ref: &ref, Text: "Pod shop/" + ref.Name, State: engine.JobPending}
		switch {
		case i == 4:
			steps[i].State, steps[i].Detail = engine.JobFailed,
				`Pods "web-7d9f-04" is forbidden: User "dev" cannot delete resource "pods" in the namespace "shop"`
		case i < 10:
			steps[i].State, steps[i].Detail = engine.JobDone, "Deleted"
		case i == 10:
			steps[i].State = engine.JobRunning
		}
	}
	return engine.DockTab{
		ID: "job-2", Type: engine.DockJob, Title: "Delete: 24 objects", Context: "demo",
		Job: &engine.JobTab{Command: "object.deleteMany", State: engine.JobRunning, Started: fixedNow.Add(-2 * time.Second),
			Steps: steps, Counts: engine.CountJobSteps(steps)},
	}
}

// TestJobTabCancel (G4): while a job runs its header shows the progress from the steps' counts and
// a Cancel button, which asks the engine (job.cancel) and waits for its answer; once the job ended
// the button goes and the outcome takes the progress's place.
func TestJobTabCancel(t *testing.T) {
	f := connectedFake(t, 20)
	f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{deleteJob()}, "job-2"
	f.handlers["job.cancel"] = func(context.Context, engine.Invocation) (any, error) { return engine.DockTab{}, nil }
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the job tab", func() bool { return jobTabOf(sh, "job-2") != nil })
	j := jobTabOf(sh, "job-2")
	if j.outcome.text != "9 of 24 done, 1 failed" || !j.cancel.Visible() || j.cancel.Disabled() {
		t.Fatalf("running: %q, cancel visible %v", j.outcome.text, j.cancel.Visible())
	}
	j.cancel.OnTapped()
	q.until(t, "job.cancel", func() bool { return len(f.callsOf("job.cancel")) == 1 })
	if p := decodeParams[jobParams](t, f.callsOf("job.cancel")[0]); p.ID != "job-2" {
		t.Errorf("job.cancel %+v", p)
	}
	q.drain()
	if !j.cancel.Disabled() {
		t.Error("Cancel stays enabled while the engine stops the job")
	}

	f.update(func(u *engine.UIState) {
		job := u.Dock[0].Job
		job.Steps[10].State, job.Steps[10].Detail = engine.JobDone, "Deleted"
		for i := 11; i < len(job.Steps); i++ {
			job.Steps[i].State, job.Steps[i].Detail = engine.JobCancelled, "Not attempted: cancelled"
		}
		job.Counts = engine.CountJobSteps(job.Steps)
		job.State, job.Ended = engine.JobCancelled, fixedNow
		job.Outcome = "Stopped: cancelled. Deleted: 10 of 24 objects; 1 failed, 13 not attempted: the job's steps say why."
	})
	q.until(t, "the end", func() bool { return j.state.text == "Stopped" })
	if j.cancel.Visible() || !strings.HasPrefix(j.outcome.text, "Stopped: cancelled. Deleted: 10 of 24") || j.rows[23].detail.text != "Not attempted: cancelled" {
		t.Errorf("after the end: cancel %v, %q, last row %q", j.cancel.Visible(), j.outcome.text, j.rows[23].detail.text)
	}
}

// TestRenderJobTabDelete: Delete of 24 pods as a job in the dock while it runs, a failed step and
// Cancel, dark and light (G4).
func TestRenderJobTabDelete(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		f := connectedFake(t, 200)
		f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{deleteJob()}, "job-2"
		sh, w, q := newTestShell(t, f, Options{Theme: mode})
		q.until(t, "the job tab", func() bool { return jobTabOf(sh, "job-2") != nil && sh.dock.open() })
		q.drain()
		savePNG(t, w, "job_tab_delete_"+string(mode)+".png")
	}
}
