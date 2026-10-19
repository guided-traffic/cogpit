package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Editing (G9): Edit opens an object's YAML from the cache in a dock tab; editor_diff shows the
// edited text against the object as the cache holds it now; object_dryRun asks the server to
// validate it; object_apply writes it with the resourceVersion the edit is based on as
// precondition, so a concurrent change is never overwritten. Create resource tabs and
// object_create make new objects from YAML. Every step is a command, so agents can drive the
// whole flow; apply and create are mutating and follow D-2.

// EditResult is what object.edit returns: the Edit tab and its text.
type EditResult struct {
	Tab                 engine.DockTab `json:"tab"`
	Created             bool           `json:"created"` // false: the tab already open on the object was activated
	Text                string         `json:"yaml"`
	Revision            uint64         `json:"revision"`
	BaseResourceVersion string         `json:"baseResourceVersion"`
}

// TextResult is what editor.setText returns.
type TextResult struct {
	Revision uint64 `json:"revision"`
}

type editorParams struct {
	ID string `json:"id" jsonschema:"editor tab id as object_edit or resource_createTab returned it, e.g. edit-1"`
}

type editorTextParams struct {
	ID   string `json:"id" jsonschema:"editor tab id, e.g. edit-1 or create-1"`
	YAML string `json:"yaml" jsonschema:"the tab's new text"`
}

type editParams struct {
	ID   string `json:"id" jsonschema:"Edit tab id as object_edit returned it, e.g. edit-1"`
	YAML string `json:"yaml,omitempty" jsonschema:"the edited YAML of the object; empty uses the tab's text"`
}

// applyParams are object.apply's: an apply from the tab's text names the text revision it takes,
// so that a text changed after the user reviewed it (an agent's editor.setText while the
// confirmation is open) is refused (R-10 of the fourth run's review).
type applyParams struct {
	ID           string `json:"id" jsonschema:"Edit tab id as object_edit returned it, e.g. edit-1"`
	YAML         string `json:"yaml,omitempty" jsonschema:"the edited YAML of the object; empty uses the tab's text at textRevision"`
	TextRevision uint64 `json:"textRevision,omitempty" jsonschema:"without yaml: the tab's text revision to apply (editor_get and editor_setText return it); the apply is refused when the text changed since"`
}

type diffParams struct {
	ID      string `json:"id" jsonschema:"Edit tab id as object_edit returned it, e.g. edit-1"`
	YAML    string `json:"yaml,omitempty" jsonschema:"the edited YAML of the object; empty uses the tab's text"`
	Context *int   `json:"context,omitempty" jsonschema:"unchanged lines to keep around each change; default 3; -1 keeps every line"`
}

type createParams struct {
	engine.OnCluster
	YAML      string `json:"yaml" jsonschema:"one or more manifests separated by ---, or a v1 List"`
	Namespace string `json:"namespace,omitempty" jsonschema:"namespace for objects whose manifest names none; empty uses the namespace filter if it names exactly one namespace, else the context's default namespace"`
}

type createTabParams struct {
	engine.OnCluster
	YAML     string `json:"yaml,omitempty" jsonschema:"text to start the tab with, e.g. a manifest for the user to review and create"`
	Template string `json:"template,omitempty" jsonschema:"start from a template instead: its kind, e.g. Deployment (resource_templates lists them); default Pod"`
}

// edits are the verbs of apply and its dry run: an update of whatever kind the tab edits.
var edits = []engine.Verb{{Verb: "update", Resource: "*"}}

// lookupTimeout bounds the discovery lookups of Targets, which has no context of its own.
const lookupTimeout = 15 * time.Second

func registerEdit(g *registrar) {
	add(g, engine.Spec[refParams]{
		ID: "object.edit", Label: "Edit", MenuPath: "Object/Edit", Order: engine.OrderEdit, Danger: engine.DangerUI,
		Verbs: append(append([]engine.Verb(nil), cacheReads...), edits...), // offered when the user may update it (access reviews)
		Description: "Opens a dock tab with an object's YAML from Cogpit's cache (managed fields dropped) for editing, or " +
			"activates the tab already open on it. Returns the tab id, the text and the resourceVersion the edit is based " +
			"on. Continue with editor_diff, object_dryRun and object_apply. Secrets are edited by the user in Cogpit's window " +
			"only: an agent or the CLI is refused (D-19).",
		Enabled: func(s *engine.Session, p refParams) error { return editable(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			st, created, err := s.OpenEditTab(ctx, p.Ref)
			if err != nil {
				return nil, err
			}
			return EditResult{Tab: st.Tab, Created: created, Text: st.Text, Revision: st.Revision, BaseResourceVersion: st.BaseResourceVersion}, nil
		},
	})

	add(g, engine.Spec[editorParams]{
		ID: "editor.get", Label: "Read Editor", Danger: engine.DangerRead, Verbs: cacheReads,
		Description: "Returns an editor tab's text (what the user has typed so far) and its revision; for an Edit tab also " +
			"the resourceVersion the edit is based on, the one Cogpit's cache holds now, and whether they differ (stale: an " +
			"apply would be refused with a conflict) or the object is gone (missing); for a Helm Upgrade tab the release " +
			"revision its values are based on. For agents and the CLI, the text of a tab that holds a Secret is withheld " +
			"whole (<withheld>, D-19), and for agents so are a Helm Upgrade tab's values." + privateReads,
		Run: func(ctx context.Context, s *engine.Session, p editorParams) (any, error) {
			st, err := s.EditorState(ctx, p.ID)
			switch {
			case err != nil || engine.ReadsLikeUser(ctx): // the window, a private turn (H-94): the tab the user opened
			case engine.HelmValuesTab(st.Tab.Type):
				if engine.ReadsAsAgent(ctx) {
					st.Text = model.Withheld // Helm values can hold credentials (helmupgrade.go, helminstall.go)
				}
			case s.EditorHoldsSecret(p.ID):
				// A Secret in an Edit or Create tab is the user's to see, not an agent's or the CLI's (D-19).
				st.Text = model.Withheld
			}
			return st, err
		},
	})

	add(g, engine.Spec[editorTextParams]{
		ID: "editor.setText", Label: "Set Editor Text", Danger: engine.DangerUI,
		Description: "Replaces an editor tab's text, as typing does; the tab shows it unless the user has unsaved typing " +
			"of their own. Writes nothing to the cluster. Returns the text's new revision.",
		Run: func(ctx context.Context, s *engine.Session, p editorTextParams) (any, error) {
			if err := s.GuardSecretEditor(ctx, p.ID, p.YAML); err != nil { // a Secret's text: the GUI only (D-19)
				return nil, err
			}
			rev, err := s.SetEditorText(p.ID, p.YAML)
			if err != nil {
				return nil, err
			}
			return TextResult{Revision: rev}, nil
		},
	})

	add(g, engine.Spec[diffParams]{
		ID: "editor.diff", Label: "Show Changes", Danger: engine.DangerRead, Verbs: cacheReads,
		Description: "Returns the line diff of an Edit tab's text (or of yaml) against the object as Cogpit's cache holds it " +
			"now: lines with op \"-\" (removed), \"+\" (added) or \"=\" (context), with line numbers. Also checks that the text " +
			"still names the same object. stale means the object changed since the edit's base version: its changes show " +
			"as reverted, and an apply would be refused with a conflict.",
		Enabled: func(s *engine.Session, p diffParams) error { _, err := writableOn(s, tabContext(s, p.ID)); return err },
		Run: func(ctx context.Context, s *engine.Session, p diffParams) (any, error) {
			n := engine.DefaultDiffContext
			if p.Context != nil {
				n = *p.Context
			}
			return s.EditDiff(ctx, p.ID, p.YAML, n)
		},
	})

	add(g, engine.Spec[editParams]{
		ID: "object.dryRun", Label: "Check with Server", Danger: engine.DangerRead, Verbs: edits,
		Description: "Sends an Edit tab's text (or yaml) to the API server as an update with dryRun=All: the server " +
			"validates it and runs admission, and stores nothing. Carries the edit's base resourceVersion, so a concurrent " +
			"change is reported as a conflict (then the tab compares with the newer version). Needs the update permission.",
		Enabled: func(s *engine.Session, p editParams) error { _, err := writableOn(s, tabContext(s, p.ID)); return err },
		Run: func(ctx context.Context, s *engine.Session, p editParams) (any, error) {
			return s.DryRunEdit(ctx, p.ID, p.YAML)
		},
	})

	add(g, engine.Spec[applyParams]{
		ID: "object.apply", Label: "Apply", MenuPath: "Object/Apply", Danger: engine.DangerMutating, Verbs: edits,
		Description: "Writes an Edit tab's text at textRevision (or yaml) to the cluster: a server-side dry run, then an update with the " +
			"resourceVersion the edit is based on as precondition. If the object changed meanwhile nothing is written and " +
			"the result is a conflict; the tab then compares with the newer version, and applying again overwrites it. On " +
			"success returns the stored version, which becomes the tab's text. An agent's request waits for the user's acceptance; " +
			"for a RoleBinding or ClusterRoleBinding it always does, also where the user allowed agent writes. " +
			"An edit that scales the object to zero replicas is destructive and is always confirmed, as Scale to zero is; " +
			"so is an edit that removes a key of a Config Map's data or binaryData or of a Secret's data (stringData " +
			"counting as data) against the cached object, as removing a key in the drawer is, and the confirmation names " +
			"the removed keys, never a value.",
		// A removed data key is data lost and a change of what workloads read: destructive for every
		// origin, as the drawer's removal is (the owner, 2026-10-16 evening; D-55).
		Escalate: func(s *engine.Session, p applyParams) bool {
			return s.EditScalesToZero(p.ID, p.YAML) || len(s.EditRemovesDataKeys(p.ID, p.YAML)) > 0
		},
		// The removed keys, names only (editdatakeys.go), a row "Removes" as the drawer's review
		// shows it (WF33); none for an apply that removes none.
		Effects: func(s *engine.Session, p applyParams) []engine.Effect {
			return engine.DataKeysEffects(nil, s.EditRemovesDataKeys(p.ID, p.YAML))
		},
		AgentsAsk: func(s *engine.Session, p applyParams) bool { return s.EditorHoldsRBAC(p.ID, p.YAML) }, // H-27, A1
		Refuse: func(ctx context.Context, s *engine.Session, p applyParams) error {
			return s.GuardSecretEditor(ctx, p.ID, p.YAML) // a Secret: the GUI only (D-19), before asking
		},
		Enabled: func(s *engine.Session, p applyParams) error { _, err := writableOn(s, tabContext(s, p.ID)); return err },
		Targets: func(s *engine.Session, p applyParams) ([]model.ObjectRef, error) {
			st, err := editTabRef(s, p.ID)
			if err != nil {
				return nil, err
			}
			return []model.ObjectRef{st}, nil
		},
		Run: func(ctx context.Context, s *engine.Session, p applyParams) (any, error) {
			return s.ApplyEdit(ctx, p.ID, p.YAML, p.TextRevision)
		},
	})

	add(g, engine.Spec[createParams]{
		ID: "object.create", Label: "Create", MenuPath: "Object/Create", Danger: engine.DangerMutating,
		Verbs: []engine.Verb{{Verb: "create", Resource: "*"}},
		Description: "Creates the objects of a YAML text (several documents separated by ---) in order, each after a " +
			"server-side dry run; an object that fails is reported and the next one is tried. Objects without a namespace " +
			"land in namespace, else in the namespace filter's single namespace, else in the context's default. Secrets are " +
			"created by the user in Cogpit's window only: for an agent or the CLI each Secret in the text is refused (D-19). Returns one item per object. An agent's request waits for the user's acceptance; " +
			"when the text holds a RoleBinding or ClusterRoleBinding it always does, also where the user allowed agent writes.",
		Enabled:   func(s *engine.Session, p createParams) error { _, err := writableOn(s, p.Context); return err },
		AgentsAsk: func(_ *engine.Session, p createParams) bool { return engine.TextHoldsRBAC(p.YAML) }, // H-27, A1
		Refuse: func(ctx context.Context, _ *engine.Session, p createParams) error {
			return engine.GuardSecretText(ctx, p.YAML) // a Secret: the GUI only (D-19), before asking
		},
		Targets: func(s *engine.Session, p createParams) ([]model.ObjectRef, error) {
			ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
			defer cancel()
			return s.CreateTargets(ctx, p.Context, p.YAML, p.Namespace)
		},
		Run: func(ctx context.Context, s *engine.Session, p createParams) (any, error) {
			res, err := s.CreateObjects(ctx, p.Context, p.YAML, p.Namespace)
			if err == nil && res.Failed == 0 && res.Created > 0 {
				s.CreatedText(p.Context, p.YAML) // a Create tab holding this text has nothing unsaved: its draft goes (drafts.go)
				revealCreated(ctx, s, res)       // A102
			}
			return res, err
		},
	})

	add(g, engine.Spec[createTabParams]{
		ID: "resource.createTab", Label: "Create Resource", MenuPath: "View/Create Resource", Danger: engine.DangerUI,
		Description: "Opens a Create resource dock tab with yaml, with a template (resource_templates lists them), or with " +
			"an example Pod. Writes nothing: the user reviews the text and creates it, or object_create does. Returns the tab.",
		Enabled: func(s *engine.Session, p createTabParams) error { _, err := writableOn(s, p.Context); return err },
		Run: func(ctx context.Context, s *engine.Session, p createTabParams) (any, error) {
			if err := engine.GuardSecretText(ctx, p.YAML); err != nil { // a Secret's text: the GUI only (D-19)
				return nil, err
			}
			text := p.YAML
			if text == "" {
				name := p.Template
				if name == "" {
					name = engine.DefaultTemplate
				}
				t, err := engine.TemplateByName(name)
				if err != nil {
					return nil, err
				}
				text = t.Text
			} else if p.Template != "" {
				return nil, errors.New("give yaml or template, not both")
			}
			st, err := s.OpenCreateTab(p.Context, text)
			if err != nil {
				return nil, err
			}
			return EditResult{Tab: st.Tab, Created: true, Text: st.Text, Revision: st.Revision}, nil
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "resource.templates", Label: "List Templates", Danger: engine.DangerRead,
		Description: "Lists Cogpit's templates for new objects, one per kind in alphabetical order, each with its " +
			"apiVersion, whether the kind is namespaced, and its YAML (without a namespace).",
		Run: func(context.Context, *engine.Session, struct{}) (any, error) { return engine.Templates() },
	})
}

// editable is object.edit's Enabled check. It must not block: a kind with a profile is checked
// against the cache without waiting; a kind without one is resolved when the command runs.
func editable(s *engine.Session, ref model.ObjectRef) error {
	b, err := writableOn(s, ref.Context) // the object's cluster, shown or not (D-69)
	if err != nil {
		return err
	}
	if _, err := b.ObjectWriter(); err != nil {
		return err
	}
	if p, ok := b.Profile(ref.Kind); ok {
		if !p.APIObject() {
			return fmt.Errorf("%s are not objects of the cluster's API: %w", p.Title, engine.ErrNotApplicable)
		}
		// A Secret is editable: object.edit's Run opens its tab for the GUI only (D-19).
		_, err := s.Lookup(ref)
		return err
	}
	return nil
}

// editTabRef is the object an Edit tab edits, for the confirmation of object.apply.
func editTabRef(s *engine.Session, id string) (model.ObjectRef, error) {
	for _, t := range s.UIState().Dock {
		if t.ID != id {
			continue
		}
		if t.Type != engine.DockEdit || t.Ref == nil {
			return model.ObjectRef{}, fmt.Errorf("dock tab %q is not an Edit tab", id)
		}
		return *t.Ref, nil
	}
	return model.ObjectRef{}, fmt.Errorf("%w: %q", engine.ErrNotEditorTab, id)
}

// objectTarget is the Targets function of the generic commands on one object (Delete): the
// resolved reference of any kind, so that the confirmation names its namespace and context.
func objectTarget(s *engine.Session, ref model.ObjectRef) ([]model.ObjectRef, error) {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	t, _, err := s.ResolveTarget(ctx, ref)
	if err != nil {
		return nil, err
	}
	return []model.ObjectRef{t.Ref}, nil
}

// cachedAny is the Enabled check of the generic commands on one object: the object must be in the
// cache when its kind has a profile (checked without waiting); other kinds are resolved when the
// command runs.
func cachedAny(s *engine.Session, ref model.ObjectRef) error {
	b, err := writableOn(s, ref.Context) // the object's cluster, shown or not (D-69)
	if err != nil {
		return err
	}
	if _, err := b.ObjectWriter(); err != nil {
		return err
	}
	if p, ok := b.Profile(ref.Kind); ok {
		if !p.APIObject() {
			return fmt.Errorf("%s are not objects of the cluster's API: %w", p.Title, engine.ErrNotApplicable)
		}
		_, err := s.Lookup(ref)
		return err
	}
	if ref.Name == "" {
		return errors.New("the reference has no name")
	}
	return nil
}
