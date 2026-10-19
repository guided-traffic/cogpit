package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Argo CD's Sync and Refresh (resource-parity › Paid Lens Desktop views › Argo CD, G23): on one
// Application (the row menu, the drawer, the palette) and on a selection of them (the selection's
// menu, the dashboard), through the Kubernetes API only (cluster/argocd.go). Lens's documentation
// (read 2026-10-13): Sync opens a "Sync app(s)" dialog with a "Prune resources not in Git" toggle,
// Refresh a "Refresh app(s)" dialog with Normal or Hard, both work on several Applications with one
// notification for the batch. Danger (the brief's, the lead's): Refresh and Sync are mutating,
// Sync with prune destructive (it deletes resources), so its confirmation names every Application,
// its namespace and the context and says what prune deletes, for every origin.

// argoPatchVerbs are the verbs of Sync and Refresh: a merge patch on the Application.
var argoPatchVerbs = []engine.Verb{{Verb: "patch", Group: kinds.ArgoCDGroup, Resource: "applications"}}

// argoSyncAttempts bounds how often a Sync checks the Application again after it changed between
// the check and the write (a status update of the controller, usually).
const argoSyncAttempts = 3

// argoCatchUp bounds how long a Sync waits for the cache to see the version that refused its
// write before it checks again.
const argoCatchUp = 3 * time.Second

// Danger texts of the confirmations.
const (
	argoPruneEffect = "on: the resources the Application manages that are no longer in Git are deleted from the cluster"
	argoSyncLabel   = "Sync"
	argoRefreshText = "Refresh"
)

type argoSyncParams struct {
	Ref   model.ObjectRef `json:"ref" jsonschema:"the Application, kind applications.argoproj.io"`
	Prune bool            `json:"prune,omitempty" jsonschema:"true: delete the resources the Application manages that are no longer in Git; the user always confirms a sync with prune"`
}

type argoSyncManyParams struct {
	Refs  []model.ObjectRef `json:"refs" jsonschema:"the Applications (kind applications.argoproj.io) of one connected cluster (their refs' context; the shown cluster without one), at most 500"`
	Prune bool              `json:"prune,omitempty" jsonschema:"true: delete the resources each Application manages that are no longer in Git; the user always confirms a sync with prune"`
}

type argoRefreshParams struct {
	Ref  model.ObjectRef `json:"ref" jsonschema:"the Application, kind applications.argoproj.io"`
	Hard bool            `json:"hard,omitempty" jsonschema:"true: a hard refresh, which also discards Argo CD's cached manifests; false or omitted: a normal refresh"`
}

type argoRefreshManyParams struct {
	Refs []model.ObjectRef `json:"refs" jsonschema:"the Applications (kind applications.argoproj.io) of one connected cluster (their refs' context; the shown cluster without one), at most 500"`
	Hard bool              `json:"hard,omitempty" jsonschema:"true: a hard refresh, which also discards Argo CD's cached manifests; false or omitted: a normal refresh"`
}

// ArgoManyResult is the result of Sync or Refresh on several Applications when every one was done.
type ArgoManyResult struct {
	Action string            `json:"action"` // "sync started", "refreshed"
	Done   []model.ObjectRef `json:"done"`
}

// ArgoManyError is the error of Sync or Refresh on several Applications when not every one was
// done: which were and which were not, each with its reason. Nothing is retried.
type ArgoManyError struct {
	Action string
	Done   []model.ObjectRef
	Failed []DeleteFailure
}

func (e *ArgoManyError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s for %d of %d applications", e.Action, len(e.Done), len(e.Done)+len(e.Failed))
	if len(e.Done) > 0 {
		b.WriteString(" (" + refList(e.Done) + ")")
	}
	b.WriteString("; not done: ")
	for i, f := range e.Failed {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(f.Ref.String() + ": " + f.Error)
	}
	b.WriteString(". Nothing was retried.")
	return b.String()
}

type argoDashboardParams struct {
	engine.OnCluster
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"namespaces to count; omitted: the namespace filter the user has set (every namespace for a cluster the window does not show)"`
	Attention  bool     `json:"attention,omitempty" jsonschema:"true: include the page's Needs attention table, the Applications not Healthy or not Synced with their message"`
}

// ArgoDashboardResult is argocd.dashboard's result.
type ArgoDashboardResult struct {
	engine.ArgoCDDashboardModel
	// Attention is the Needs attention table: Name, Namespace, Sync, Health, Message, Age (the
	// other Application columns hidden). Only with attention: true.
	Attention *engine.TableSnapshot `json:"attention,omitempty"`
}

func registerArgoCD(g *registrar) {
	add(g, engine.Spec[argoDashboardParams]{
		ID: "argocd.dashboard", Label: "Argo CD Dashboard", MenuPath: "View/Argo CD Dashboard", Danger: engine.DangerRead,
		Verbs: cacheReads,
		Description: "Reads the Argo CD Dashboard: the health strip (Applications, Synced, Healthy, Out of sync, Degraded, " +
			"Missing, Progressing), the Applications counted by health and by sync status, a card per Argo CD kind the " +
			"cluster serves (Applications, Application Sets, Projects, Repositories, Clusters) with counts per status " +
			"token, the out-of-sync Applications and all of them (what the Applications card's Sync and Refresh take), " +
			"and the recent activity (operations and syncs, newest first); with attention, the Applications not Healthy " +
			"or not Synced with their message. Read from the cache. ui_openView with view argocd/dashboard shows the page.",
		Enabled: func(s *engine.Session, p argoDashboardParams) error { _, err := connectedOn(s, p.Context); return err },
		Run: func(ctx context.Context, s *engine.Session, p argoDashboardParams) (any, error) {
			m, attention, err := s.ArgoCDDashboard(ctx, p.Context, p.Namespaces, p.Attention)
			if err != nil {
				return nil, err
			}
			return ArgoDashboardResult{ArgoCDDashboardModel: *m, Attention: attention}, nil
		},
	})
	add(g, engine.Spec[engine.OnCluster]{
		ID: "argocd.openUI", Label: "Open Argo CD UI", MenuPath: "Argo CD/Open Argo CD UI", Danger: engine.DangerMutating,
		Verbs: serviceForwardVerbs,
		Description: "Opens Argo CD's own web UI: finds the argocd-server Service by its label " +
			"app.kubernetes.io/name=argocd-server in Cogpit's cache, and forwards its https port (443, else its http " +
			"port 80) to 127.0.0.1, or reuses that forward while it runs, as service.portForward does. Returns the " +
			"forward with its URL; the GUI opens it in the browser, an agent gets the URL. The user must accept an " +
			"agent's request.",
		Enabled: func(s *engine.Session, p engine.OnCluster) error {
			b, err := connectedOn(s, p.Context)
			if err != nil {
				return err
			}
			if _, ok := b.Profile(kinds.ArgoApplicationsKey); !ok {
				return errors.New("the cluster serves no Argo CD Applications")
			}
			return s.CanForward(p.Context)
		},
		Targets: func(s *engine.Session, p engine.OnCluster) ([]model.ObjectRef, error) {
			ref, _, _, err := argoServer(s, p.Context)
			if err != nil {
				return nil, err
			}
			return targetOf(s, ref)
		},
		Run: runArgoOpenUI,
	})
	appKinds := []string{kinds.ArgoApplicationsKey}
	add(g, engine.Spec[argoSyncParams]{
		ID: "argocd.application.sync", Label: argoSyncLabel, MenuPath: "Argo CD/Sync", Kinds: appKinds, Order: 100,
		Danger: engine.DangerMutating, Verbs: argoPatchVerbs,
		Description: "Syncs an Argo CD Application through the Kubernetes API, as argocd app sync does: it writes the " +
			"Application's operation (the revisions its sources target, its own sync options and retry, initiated by " +
			"cogpit), which Argo CD's controller then runs. Refused while an operation is requested or running. With " +
			"prune, resources the Application manages that are no longer in Git are deleted: the user always confirms " +
			"that. The write is bound to the Application's UID and the version Cogpit checked. Returns at once; the " +
			"Application's operation state shows the progress.",
		Enabled: func(s *engine.Session, p argoSyncParams) error {
			obj, err := argoEnabled(s, p.Ref)
			if err != nil {
				return err
			}
			if busy := kinds.ArgoOperationBusy(obj); busy != "" {
				return errors.New(busy)
			}
			if why := argoWindowBlock(context.Background(), s, p.Ref, obj, false); why != "" { // A33
				return errors.New(why)
			}
			return nil
		},
		Escalate: func(_ *engine.Session, p argoSyncParams) bool { return p.Prune },
		Targets:  func(s *engine.Session, p argoSyncParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Effects: func(s *engine.Session, p argoSyncParams) []engine.Effect {
			return argoSyncEffects(s, []model.ObjectRef{p.Ref}, p.Prune)
		},
		Run: func(ctx context.Context, s *engine.Session, p argoSyncParams) (any, error) {
			ref, err := argoOne(ctx, s, p.Ref, func(w engine.ArgoCDActions, version string, ref model.ObjectRef) error {
				return argoSync(ctx, s, w, version, ref, p.Prune)
			})
			if err != nil {
				return nil, err
			}
			userToast(s, engine.LevelInfo, "Application "+ref.Key()+": "+argoSyncDone(p.Prune))
			return ActionResult{Ref: ref, Action: argoSyncDone(p.Prune)}, nil
		},
	})
	add(g, engine.Spec[argoRefreshParams]{
		ID: "argocd.application.refresh", Label: argoRefreshText, MenuPath: "Argo CD/Refresh", Kinds: appKinds, Order: 110,
		Danger: engine.DangerMutating, Verbs: argoPatchVerbs,
		Description: "Refreshes an Argo CD Application through the Kubernetes API: it sets the annotation " +
			"argocd.argoproj.io/refresh to normal (compare the live state with Git again) or, with hard, to hard (also " +
			"discard Argo CD's cached manifests); Argo CD's controller refreshes and removes the annotation. Changes " +
			"nothing in the cluster but the annotation.",
		Enabled: func(s *engine.Session, p argoRefreshParams) error { _, err := argoEnabled(s, p.Ref); return err },
		Targets: func(s *engine.Session, p argoRefreshParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Effects: func(_ *engine.Session, p argoRefreshParams) []engine.Effect { return argoRefreshEffects(p.Hard) },
		Run: func(ctx context.Context, s *engine.Session, p argoRefreshParams) (any, error) {
			ref, err := argoOne(ctx, s, p.Ref, func(w engine.ArgoCDActions, version string, ref model.ObjectRef) error {
				return argoRefresh(ctx, s, w, version, ref, p.Hard)
			})
			if err != nil {
				return nil, err
			}
			userToast(s, engine.LevelInfo, "Application "+ref.Key()+": "+argoRefreshDone(p.Hard))
			return ActionResult{Ref: ref, Action: argoRefreshDone(p.Hard)}, nil
		},
	})
	add(g, engine.Spec[argoSyncManyParams]{
		ID: "argocd.application.syncMany", Label: argoSyncLabel, MenuPath: "Argo CD/Sync Selected", Kinds: appKinds, Order: 100,
		Danger: engine.DangerMutating, Verbs: argoPatchVerbs,
		Description: "Syncs several Argo CD Applications at once, as argocd.application.sync does for one: one " +
			"acceptance names them all (with prune, a confirmation for every origin), then each is synced in turn, bound " +
			"to its UID; one that has an operation requested or running is skipped and reported. At most 500. " +
			jobManyDescription,
		Enabled:  func(s *engine.Session, p argoSyncManyParams) error { return argoManyEnabled(s, p.Refs) },
		Escalate: func(_ *engine.Session, p argoSyncManyParams) bool { return p.Prune },
		Targets: func(s *engine.Session, p argoSyncManyParams) ([]model.ObjectRef, error) {
			return selectionTargets(s, p.Refs)
		},
		Effects: func(s *engine.Session, p argoSyncManyParams) []engine.Effect {
			return argoSyncEffects(s, p.Refs, p.Prune)
		},
		Run: func(ctx context.Context, s *engine.Session, p argoSyncManyParams) (any, error) {
			return argoMany(ctx, s, "argocd.application.syncMany", argoSyncLabel, p.Refs, argoSyncDone(p.Prune),
				func(ctx context.Context, w engine.ArgoCDActions, version string, ref model.ObjectRef) error {
					return argoSync(ctx, s, w, version, ref, p.Prune)
				})
		},
	})
	add(g, engine.Spec[argoRefreshManyParams]{
		ID: "argocd.application.refreshMany", Label: argoRefreshText, MenuPath: "Argo CD/Refresh Selected", Kinds: appKinds, Order: 110,
		Danger: engine.DangerMutating, Verbs: argoPatchVerbs,
		Description: "Refreshes several Argo CD Applications at once, normal or hard, as argocd.application.refresh does " +
			"for one: one acceptance names them all, then each is refreshed in turn, bound to its UID. At most 500. " +
			jobManyDescription,
		Enabled: func(s *engine.Session, p argoRefreshManyParams) error { return argoManyEnabled(s, p.Refs) },
		Targets: func(s *engine.Session, p argoRefreshManyParams) ([]model.ObjectRef, error) {
			return selectionTargets(s, p.Refs)
		},
		Effects: func(_ *engine.Session, p argoRefreshManyParams) []engine.Effect { return argoRefreshEffects(p.Hard) },
		Run: func(ctx context.Context, s *engine.Session, p argoRefreshManyParams) (any, error) {
			return argoMany(ctx, s, "argocd.application.refreshMany", argoRefreshText, p.Refs, argoRefreshDone(p.Hard),
				func(ctx context.Context, w engine.ArgoCDActions, version string, ref model.ObjectRef) error {
					return argoRefresh(ctx, s, w, version, ref, p.Hard)
				})
		},
	})
}

// argoServerLabel finds Argo CD's API server and UI among the cached Services, as Lens's
// documentation says Lens finds it ("the argocd-server Service by label").
const argoServerLabel = "app.kubernetes.io/name=argocd-server"

// argoServer finds the argocd-server Service in the cache and the port to forward: 443 (https),
// else 80 (http). It reads the Services through the cache, waiting for their first list (bounded).
func argoServer(s *engine.Session, contextID string) (model.ObjectRef, uint16, bool, error) {
	b, err := connectedOn(s, contextID) // the context's cluster, "" the shown one (D-69)
	if err != nil {
		return model.ObjectRef{}, 0, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	svcs, err := s.ObjectsIn(ctx, b.Context, kindServices, nil)
	if err != nil {
		return model.ObjectRef{}, 0, false, err
	}
	name, value, _ := strings.Cut(argoServerLabel, "=")
	for _, svc := range svcs {
		if svc.GetLabels()[name] != value {
			continue
		}
		ref := model.ObjectRef{Context: b.Context, Kind: kindServices, Namespace: svc.GetNamespace(), Name: svc.GetName()}
		var http uint16
		for _, p := range model.NestedSlice(svc.Object, "spec", "ports") {
			n, _ := model.NestedInt64(p, "port")
			switch n {
			case 443:
				return ref, 443, true, nil
			case 80:
				http = 80
			}
		}
		if http != 0 {
			return ref, http, false, nil
		}
		return model.ObjectRef{}, 0, false, fmt.Errorf("service %s/%s has neither port 443 nor port 80", ref.Namespace, ref.Name)
	}
	return model.ObjectRef{}, 0, false, errors.New("no Service labelled " + argoServerLabel + " in Cogpit's cache: Argo CD's UI is not installed, or you may not list its Service")
}

// runArgoOpenUI reuses the forward of argocd-server while it listens, else starts it.
func runArgoOpenUI(ctx context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
	ref, port, https, err := argoServer(s, p.Context)
	if err != nil {
		return nil, err
	}
	{
		fref := model.ObjectRef{Context: ref.Context, Kind: engine.PortForwardKind, Namespace: ref.Namespace, Name: engine.ForwardName(ref, port)}
		if f, err := s.LookupForward(fref); err == nil && f.Listening() {
			return f, nil
		}
	}
	return startForward(ctx, s, engine.ForwardSpec{Target: ref, Port: port, HTTPS: https, OpenInBrowser: true})
}

func argoSyncDone(prune bool) string {
	if prune {
		return "sync with prune started"
	}
	return "sync started"
}

func argoRefreshDone(hard bool) string {
	if hard {
		return "hard refresh requested"
	}
	return "refresh requested"
}

// argoRefreshEffects are a Refresh's confirmation row: normal or hard, and what that does.
func argoRefreshEffects(hard bool) []engine.Effect {
	if hard {
		return []engine.Effect{{Name: "Refresh", Value: "hard: Argo CD discards its cached manifests and generates them again"}}
	}
	return []engine.Effect{{Name: "Refresh", Value: "normal: Argo CD compares the live state with Git again"}}
}

// argoPrunedNamed is how many resources a Sync's confirmation names that prune deletes.
const argoPrunedNamed = 10

// argoSyncEffects are a Sync's confirmation rows: what it syncs to and, with prune, what prune
// deletes: the resources the Applications' status marks requiresPruning, as the controller last
// compared them, one a line.
func argoSyncEffects(s *engine.Session, refs []model.ObjectRef, prune bool) []engine.Effect {
	var out []engine.Effect
	var pruned []string
	for _, ref := range refs {
		obj, err := s.Lookup(ref)
		if err != nil {
			continue
		}
		if len(refs) == 1 {
			if revs := argoTargets(obj); revs != "" {
				out = append(out, engine.Effect{Name: "Syncs to", Value: revs, Tone: engine.EffectNames})
			}
		}
		pruned = append(pruned, kinds.ArgoPrunable(obj)...)
	}
	if !prune {
		return out
	}
	out = append(out, engine.Effect{Name: "Prune", Value: argoPruneEffect, Tone: engine.EffectLost})
	marked := engine.Effect{Name: "To prune", Value: "none, as Argo CD last compared", Tone: engine.EffectLost}
	switch n := len(pruned); {
	case n == 0:
		marked.Tone = engine.EffectPlain
	case n <= argoPrunedNamed:
		marked.Value = strings.Join(pruned, "\n")
	default:
		marked.Value = strings.Join(pruned[:argoPrunedNamed], "\n") + fmt.Sprintf("\nand %d more", n-argoPrunedNamed)
	}
	return append(out, marked)
}

// argoTargets names the revisions a Sync of obj targets: "main", "65.1.0 and main".
func argoTargets(obj *unstructured.Unstructured) string {
	op := kinds.ArgoSyncOperation(obj, false)
	sync, _ := op["sync"].(map[string]any)
	if r, _ := sync["revision"].(string); r != "" {
		return r
	}
	var revs []string
	if list, ok := sync["revisions"].([]any); ok {
		for _, r := range list {
			if s, _ := r.(string); s != "" {
				revs = append(revs, s)
			}
		}
	}
	return strings.Join(revs, " and ")
}

// argoEnabled is the Enabled check of an action on one Application: of Argo CD's Application kind
// (else not applicable), in the cache, on a connection that can write Argo CD's Applications.
func argoEnabled(s *engine.Session, ref model.ObjectRef) (*unstructured.Unstructured, error) {
	obj, err := cachedOfKind(s, ref, kinds.ArgoApplicationsKey)
	if err != nil {
		return nil, err
	}
	if _, _, err := argoActionsOf(s, ref.Context); err != nil {
		return nil, err
	}
	return obj, nil
}

// argoManyEnabled is the Enabled check of an action on several Applications.
func argoManyEnabled(s *engine.Session, refs []model.ObjectRef) error {
	if len(refs) == 0 {
		return errors.New("no applications are selected")
	}
	if len(refs) > engine.MaxSelection {
		return fmt.Errorf("at most %d applications at once, %d are selected", engine.MaxSelection, len(refs))
	}
	for _, r := range refs {
		if r.Kind != kinds.ArgoApplicationsKey {
			return fmt.Errorf("%w: %s is no Argo CD Application", engine.ErrNotApplicable, r.String())
		}
	}
	for _, r := range refs {
		if _, err := argoEnabled(s, r); err != nil {
			return fmt.Errorf("%s: %w", r.String(), err)
		}
	}
	return nil
}

var errConnectionCannotArgo = errors.New("this connection cannot change Argo CD applications")

// argoActionsOf is the writable check of Argo CD's actions.
func argoActionsOf(s *engine.Session, contextID string) (*engine.Backend, engine.ArgoCDActions, error) {
	b, err := writableOn(s, contextID)
	if err != nil {
		return nil, nil, err
	}
	w, ok := b.Writer().(engine.ArgoCDActions)
	if !ok {
		return nil, nil, errConnectionCannotArgo
	}
	return b, w, nil
}

// argoOne runs an action on one Application of its connected cluster after checking that the user
// confirmed it for this cluster, with the version the cluster serves Applications at.
func argoOne(ctx context.Context, s *engine.Session, ref model.ObjectRef, do func(engine.ArgoCDActions, string, model.ObjectRef) error) (model.ObjectRef, error) {
	b, w, err := argoActionsOf(s, ref.Context) // the Application's cluster, shown or not (D-69)
	if err != nil {
		return ref, err
	}
	ref, _, err = s.ResolveRef(ref)
	if err != nil {
		return ref, err
	}
	if ref.Context != b.Context {
		return ref, fmt.Errorf("%w: the shown cluster changed", engine.ErrWrongCluster)
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return ref, err
	}
	p, ok := b.Profile(kinds.ArgoApplicationsKey)
	if !ok {
		return ref, errors.New("the cluster no longer serves Argo CD's Applications")
	}
	if err := do(w, p.Version, ref); err != nil {
		return ref, fmt.Errorf("%s: %w", ref.String(), err)
	}
	return ref, nil
}

// argoMany runs an action on the Applications the user confirmed, one after the other, and
// reports them in one notification (as Lens does) and the result; more than
// engine.DirectSelection as a job (manyjob.go). A crash in the middle leaves some Applications
// with their operation or refresh annotation written and the others not, each wholly one or the
// other: Argo CD's controller runs what was written whether Cogpit runs or not, and the action on
// those left finishes what the user asked for (D-11).
func argoMany(ctx context.Context, s *engine.Session, id, label string, refs []model.ObjectRef, action string,
	do func(context.Context, engine.ArgoCDActions, string, model.ObjectRef) error) (any, error) {
	b, w, err := argoActionsOf(s, refsContext(refs))
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	targets, err := selectionTargets(s, refs)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedTargets(ctx, targets); err != nil {
		return nil, err
	}
	p, ok := b.Profile(kinds.ArgoApplicationsKey)
	if !ok {
		return nil, errors.New("the cluster no longer serves Argo CD's Applications")
	}
	m := manyAction{
		id: id, label: label, done: action, noun: "applications", b: b, targets: targets,
		do: func(ctx context.Context, ref model.ObjectRef) (model.ObjectRef, error) {
			return ref, do(ctx, w, p.Version, ref)
		},
	}
	return runMany(ctx, s, m, func(out manyOutcome) (any, error) {
		if len(out.failed) > 0 {
			return nil, &ArgoManyError{Action: action, Done: out.done, Failed: out.failed}
		}
		userToast(s, engine.LevelInfo, fmt.Sprintf("%d applications: %s", len(out.done), action))
		return ArgoManyResult{Action: action, Done: out.done}, nil
	})
}

// argoRefresh refreshes one Application, bound to the UID the user confirmed.
func argoRefresh(ctx context.Context, s *engine.Session, w engine.ArgoCDActions, version string, ref model.ObjectRef, hard bool) error {
	uid, err := sameAsConfirmed(ctx, s, ref)
	if err != nil {
		return err
	}
	return replaced(ref, w.RefreshArgoApplication(ctx, version, ref.Namespace, ref.Name, uid, hard))
}

// argoSync syncs one Application: the operation built from the cached object, written bound to
// its UID and resourceVersion. When the Application changed in between (cluster.ErrArgoChanged),
// it waits for the cache to see the newer version, checks again that no operation runs, and tries
// once more, at most argoSyncAttempts times in all.
func argoSync(ctx context.Context, s *engine.Session, w engine.ArgoCDActions, version string, ref model.ObjectRef, prune bool) error {
	uid, err := sameAsConfirmed(ctx, s, ref)
	if err != nil {
		return err
	}
	for attempt := 1; ; attempt++ {
		obj, err := s.Lookup(ref)
		if err != nil {
			return err
		}
		if string(obj.GetUID()) != uid {
			return fmt.Errorf("%w: %s is no longer the object that was confirmed; nothing was done", engine.ErrTargetsChanged, ref.String())
		}
		if busy := kinds.ArgoOperationBusy(obj); busy != "" {
			return errors.New("not synced: " + busy)
		}
		if why := argoWindowBlock(ctx, s, ref, obj, true); why != "" { // A33: refused, nothing written
			return errors.New("not synced: " + why)
		}
		rv := obj.GetResourceVersion()
		err = w.SyncArgoApplication(ctx, version, ref.Namespace, ref.Name, uid, rv, kinds.ArgoSyncOperation(obj, prune))
		if !errors.Is(err, cluster.ErrArgoChanged) || attempt == argoSyncAttempts {
			return replaced(ref, err)
		}
		argoCatchUpWith(ctx, s, ref, rv)
	}
}

// argoCatchUpWith waits until the cache holds a version of the object other than rv, or
// argoCatchUp passed: the cache is in memory, so reading it again costs no request.
func argoCatchUpWith(ctx context.Context, s *engine.Session, ref model.ObjectRef, rv string) {
	wctx, cancel := context.WithTimeout(ctx, argoCatchUp)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if obj, err := s.Lookup(ref); err != nil || obj.GetResourceVersion() != rv {
			return
		}
		select {
		case <-wctx.Done():
			return
		case <-tick.C:
		}
	}
}
