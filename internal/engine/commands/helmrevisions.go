package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A release's revisions (D-131; engine/helmrevisions.go): each
// revision's values and rendered manifest from the release records in Cogpit's cache, compared
// with another revision, and the Revisions dock tab that shows them. Reads only; nothing asks the
// API server. Values and rendered Secrets' values can hold credentials (H-8): agents read them
// withheld, the user in Cogpit's own window.

// RevisionsCommand opens a release's Revisions dock tab.
const RevisionsCommand = "ui.helm.revisions"

// orderRevisions places Revisions between Rollback (200) and Upgrade (300) in the drawer's toolbar
// and the row menu.
const orderRevisions = 250

type revisionParams struct {
	Ref      model.ObjectRef `json:"ref" jsonschema:"the release: kind helmreleases, its namespace and name"`
	Revision int             `json:"revision,omitempty" jsonschema:"the revision, one of helm_release_history's; 0 or empty: the newest"`
	All      bool            `json:"all,omitempty" jsonschema:"every value, the chart's defaults included (helm get values --all); default: the user-supplied values only"`
}

type revisionDiffParams struct {
	Ref    model.ObjectRef `json:"ref" jsonschema:"the release: kind helmreleases, its namespace and name"`
	From   int             `json:"from,omitempty" jsonschema:"the revision compared: the diff says what changed from it to the revision to; 0 or empty: the newest"`
	To     int             `json:"to,omitempty" jsonschema:"the revision shown; 0 or empty: the newest"`
	What   string          `json:"what,omitempty" jsonschema:"values (the user-supplied ones; the default), allValues (with the chart's defaults) or manifest (the rendered objects)"`
	Object string          `json:"object,omitempty" jsonschema:"with what manifest: an object's key as the result's objects carry it (<Kind>.<group>/<namespace>/<name>), whose line diff lines holds"`
	Masked bool            `json:"masked,omitempty" jsonschema:"with an object that is a Secret: its values masked with their size on both sides, as the window shows them before Show; agents read them withheld either way"`
}

type revisionsTabParams struct {
	Ref      model.ObjectRef `json:"ref" jsonschema:"the release: kind helmreleases, its namespace and name"`
	Revision int             `json:"revision,omitempty" jsonschema:"the revision to show; 0 or empty: the newest"`
	// Compare is a pointer so that 0 (no comparison) differs from none given (the default).
	Compare *int   `json:"compare,omitempty" jsonschema:"the revision to compare with; 0: none; empty: the newest for an older revision, none for the newest"`
	Mode    string `json:"mode,omitempty" jsonschema:"values or manifest; empty: values for a new tab, the tab's for an open one"`
}

type revisionsSetParams struct {
	ID       string  `json:"id" jsonschema:"the Revisions dock tab, as ui_helm_revisions returned it or ui_state lists it, e.g. helmRevisions-1"`
	Revision *int    `json:"revision,omitempty" jsonschema:"the revision to show"`
	Compare  *int    `json:"compare,omitempty" jsonschema:"the revision to compare with; 0: none"`
	Mode     *string `json:"mode,omitempty" jsonschema:"values or manifest"`
	All      *bool   `json:"all,omitempty" jsonschema:"every value, the chart's defaults included; false: the user-supplied ones"`
	Raw      *bool   `json:"raw,omitempty" jsonschema:"the manifest as Helm stored it (manifest.yaml); false: its objects"`
	Object   *string `json:"object,omitempty" jsonschema:"the object of the manifest selected, its key as helm_release_revision lists it"`
	LineDiff *bool   `json:"lineDiff,omitempty" jsonschema:"the values' line diff unfolded under the changed keys"`
}

// RevisionResult is what helm.release.revision returns.
type RevisionResult struct {
	Ref model.ObjectRef `json:"ref"`
	helm.Revision
}

// ForAgent replaces the cached Secrets' values in the manifest and its objects.
func (r RevisionResult) ForAgent(v *engine.AgentView) any {
	r.Manifest = v.YAML(r.Manifest)
	for i, o := range r.Objects {
		r.Objects[i].YAML = v.YAML(o.YAML)
	}
	return r
}

// RevisionDiffResult is what helm.release.diff returns.
type RevisionDiffResult struct{ engine.RevisionDiff }

// ForAgent replaces the cached Secrets' values in the object's diff.
func (r RevisionDiffResult) ForAgent(v *engine.AgentView) any {
	r.Lines = v.DiffLines(r.Lines)
	return r
}

func registerHelmRevisions(g *registrar) {
	add(g, engine.Spec[revisionParams]{
		ID: "helm.release.revision", Label: "Release Revision", Danger: engine.DangerRead, Verbs: releaseCacheReads,
		Description: "Returns one revision of a release from Cogpit's cache (as helm get values|manifest --revision): its chart, " +
			"status and description, its values (the user-supplied ones, or all of them with the chart's defaults), and its " +
			"rendered manifest as Helm stored it and split into objects (kind, name, namespace, the template that rendered " +
			"it, its YAML), in the order Helm installs them, at most 500. Values and rendered Secrets' data can hold " +
			"credentials: agents read them withheld; the user sees them in Cogpit's own window." + privateReads,
		Enabled: func(s *engine.Session, p revisionParams) error {
			_, err := cachedOfKind(s, p.Ref, helm.ReleaseKind)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p revisionParams) (any, error) {
			ref, _, err := s.ResolveRef(p.Ref)
			if err != nil {
				return nil, err
			}
			r, err := s.ReleaseRevision(ctx, ref, p.Revision, p.All)
			if err != nil {
				return nil, err
			}
			if engine.ReadsAsAgent(ctx) {
				r = r.Withhold(model.Withheld)
			}
			return RevisionResult{Ref: ref, Revision: r}, nil
		},
	})

	add(g, engine.Spec[revisionDiffParams]{
		ID: "helm.release.diff", Label: "Release Revision Diff", Danger: engine.DangerRead, Verbs: releaseCacheReads,
		Description: "Compares two revisions of a release from Cogpit's cache: what changed from revision from to revision to. " +
			"values and allValues: the values that differ by key path, each one's value in both, and the line diff of the " +
			"values' YAML; manifest: every object of both revisions marked + (only in to), - (only in from), ~ (changed) or " +
			"= (the same), their counts, and with object one object's line diff. Agents read the values and rendered " +
			"Secrets' values withheld." + privateReads,
		Enabled: func(s *engine.Session, p revisionDiffParams) error {
			_, err := cachedOfKind(s, p.Ref, helm.ReleaseKind)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p revisionDiffParams) (any, error) {
			ref, _, err := s.ResolveRef(p.Ref)
			if err != nil {
				return nil, err
			}
			agent := engine.ReadsAsAgent(ctx)
			var mask func(int) string
			switch {
			case agent:
				mask = func(int) string { return model.Withheld }
			case p.Masked:
				mask = helm.MaskSize
			}
			d, err := s.ReleaseRevisionDiff(ctx, ref, p.From, p.To, p.What, p.Object, mask)
			if err != nil {
				return nil, err
			}
			if agent && d.What != engine.DiffManifest {
				for i := range d.Changes {
					d.Changes[i].From, d.Changes[i].To = withheldIf(d.Changes[i].From), withheldIf(d.Changes[i].To)
				}
				d.Lines = nil
			}
			return RevisionDiffResult{d}, nil
		},
	})

	add(g, engine.Spec[revisionsTabParams]{
		ID: RevisionsCommand, Label: "Revisions", MenuPath: "Helm/Revisions", Kinds: []string{helm.ReleaseKind},
		Order: orderRevisions, Danger: engine.DangerUI, Verbs: releaseCacheReads,
		Description: "Opens a release's Revisions dock tab, or activates the one open on it: the release's revisions, one " +
			"revision's values (user-supplied or all) or rendered manifest (its objects, or manifest.yaml), compared with " +
			"another revision (by default the newest for an older revision). Reads Cogpit's cache only. Returns the tab; " +
			"helm_release_revision and helm_release_diff read what it shows, ui_helm_revisions_set changes it.",
		Enabled: func(s *engine.Session, p revisionsTabParams) error {
			_, err := cachedOfKind(s, p.Ref, helm.ReleaseKind)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p revisionsTabParams) (any, error) {
			tab, created, err := s.OpenHelmRevisionsTab(ctx, p.Ref, p.Revision, p.Compare, p.Mode)
			if err != nil {
				return nil, err
			}
			return DockResult{Tab: tab, Created: created}, nil
		},
	})

	add(g, engine.Spec[revisionsSetParams]{
		ID: "ui.helm.revisions.set", Label: "Set Revisions Tab", Danger: engine.DangerUI,
		Description: "Changes what a Revisions dock tab shows: the revision, the revision it is compared with (0: none), " +
			"values or manifest, all values or the user-supplied ones, the manifest's objects or manifest.yaml, the object " +
			"selected, the values' line diff unfolded. A revision must be one the release's records hold. Returns the tab.",
		Run: func(_ context.Context, s *engine.Session, p revisionsSetParams) (any, error) {
			tab, err := s.SetHelmRevisionsTab(p.ID, engine.HelmRevisionsPatch{
				Revision: p.Revision, Compare: p.Compare, Mode: p.Mode, All: p.All, Raw: p.Raw, Object: p.Object, LineDiff: p.LineDiff,
			})
			if err != nil {
				return nil, err
			}
			return DockResult{Tab: tab}, nil
		},
	})
}

// withheldIf is Withheld for a value that is set, "" for one that is not.
func withheldIf(s string) string {
	if s == "" {
		return ""
	}
	return model.Withheld
}
