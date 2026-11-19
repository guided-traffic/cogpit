package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// job.cancel (G4): stops a background job (engine/jobs.go) and keeps its tab with the outcome, as
// the job tab's Cancel button does; dock.close stops it too and closes the tab.
//
// Who may cancel (the lead's decision for the owner to confirm): every origin, any job, a UI
// command (D-2: it runs without asking). A stop writes nothing to the cluster and undoes nothing;
// agents could already stop any job by closing its tab (dock.close, a UI command), so limiting
// job.cancel to the jobs an agent started would guard nothing. The tab's outcome says when an agent
// cancelled it ("Stopped: cancelled by an agent."), and an agent's job.cancel is in the audit log
// as every agent command is.

type jobParams struct {
	ID string `json:"id" jsonschema:"the job's dock tab id, as ui_state's dock lists it (type job), e.g. job-1"`
}

func registerJobs(g *registrar) {
	add(g, engine.Spec[jobParams]{
		ID: "job.cancel", Label: "Cancel Job", Danger: engine.DangerUI,
		Description: "Cancels a background job, a dock tab of type job (an action on more than 10 selected objects, " +
			"Reconcile with Source): no further step starts (a request already sent finishes; a wait ends), and the tab " +
			"stays open with the job's outcome, which says that it was cancelled, what was done and what was not. " +
			"Nothing done is undone. dock_close on the tab stops it too and closes the tab. Refused for a job that has " +
			"ended. Returns the job's tab once it shows how the job ended (at most 10 seconds later).",
		Enabled: func(s *engine.Session, p jobParams) error { return s.JobCancellable(p.ID) },
		Run: func(ctx context.Context, s *engine.Session, p jobParams) (any, error) {
			return s.CancelJob(ctx, p.ID, engine.OriginOf(ctx))
		},
	})
}
