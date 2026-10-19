package commands

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A Secret's values edited in the drawer, the window's only (the owner's audit answer of
// 2026-10-15, D-59; engine/secretdataedit.go): secret.reviewData shows the masked diff (keys and
// sizes, never a value) and the server's dry run, secret.saveData writes the values with the
// version they are based on as precondition, as configMap.reviewData and configMap.saveData do
// for a Config Map. Both are MCP tools like every command, marked GUI-only: Dispatch refuses an
// agent or the CLI before anything is read, asked or sent, Refuse and the engine refuse again, and
// the audit line of a refused call names the Secret and its keys, never a value. Keys are added
// (add) and removed (remove) in the same write (the owner's audit decision of 2026-10-16); a save
// that removes a key is destructive, which the drawer's review confirms.

// dataVerbUpdate is the verb of the drawers' saves: an object written back whole.
const dataVerbUpdate = "update"

// secretDataVerbs are the verbs of saving a Secret's values: an update of the Secret, read from
// the cache's full watch. The review's dry run is an update too.
var secretDataVerbs = []engine.Verb{
	{Verb: nwVerbList, Resource: kindSecrets}, {Verb: nwVerbWatch, Resource: kindSecrets}, {Verb: dataVerbUpdate, Resource: kindSecrets},
}

type secretDataParams struct {
	Ref             model.ObjectRef   `json:"ref" jsonschema:"the Secret the drawer shows: kind secrets, its namespace and name"`
	ResourceVersion string            `json:"resourceVersion" jsonschema:"the version of the Secret the values are based on"`
	Data            map[string]string `json:"data,omitempty" jsonschema:"the keys whose values change, each with its whole new value as text; only text values the data holds"`
	Add             map[string]string `json:"add,omitempty" jsonschema:"keys to add, each with its value as text; not keys the data holds"`
	Remove          []string          `json:"remove,omitempty" jsonschema:"keys of the data to remove with their values"`
	From            map[string]string `json:"from,omitempty" jsonschema:"for the review: each changed or removed key's value at resourceVersion; a Secret that changed since, but not in these keys and with no added key appeared, is reviewed against its newer version (rebasedFrom)"`
}

// edit is the parameters' change of the data.
func (p secretDataParams) edit() engine.DataEdit {
	return engine.DataEdit{Values: p.Data, Add: p.Add, Remove: p.Remove, From: p.From}
}

// refuseSecretData refuses every origin but the GUI (D-59).
func refuseSecretData(ctx context.Context) error {
	if engine.OriginOf(ctx) != engine.OriginGUI {
		return engine.ErrSecretDataGUIOnly
	}
	return nil
}

func registerSecretData(g *registrar) {
	add(g, engine.Spec[secretDataParams]{
		ID: engine.SecretReviewDataCommand, Label: "Review Secret Data", Kinds: []string{kindSecrets}, Danger: engine.DangerRead,
		GUIOnly: engine.ErrSecretDataGUIOnly,
		Verbs:   secretDataVerbs,
		Description: "Shows the user what Save in a Secret's drawer would write: the keys whose values change with their sizes " +
			"before and after, never a value, and the API server's verdict on a dry run. For the user in Cogpit's window " +
			"only (D-59): agents and the CLI are refused before anything is read or sent, whatever the parameters. To " +
			"change a Secret's values, ask the user to edit them in its drawer.",
		Enabled: func(s *engine.Session, p secretDataParams) error { return secretDataEnabled(s, p.Ref) },
		Refuse:  func(ctx context.Context, _ *engine.Session, _ secretDataParams) error { return refuseSecretData(ctx) },
		Run: func(ctx context.Context, s *engine.Session, p secretDataParams) (any, error) {
			if err := refuseSecretData(ctx); err != nil {
				return nil, err
			}
			return s.ReviewSecretData(ctx, p.Ref, p.ResourceVersion, p.edit())
		},
	})

	add(g, engine.Spec[secretDataParams]{
		ID: engine.SecretSaveDataCommand, Label: "Save Secret Data", Kinds: []string{kindSecrets}, Danger: engine.DangerMutating,
		GUIOnly: engine.ErrSecretDataGUIOnly,
		Verbs:   secretDataVerbs,
		Description: "Writes new values for keys of a Secret's data, adds keys and removes keys, as Save in its drawer does: the " +
			"Secret from Cogpit's cache with the values set base64-encoded, a server-side dry run, then an update with " +
			"resourceVersion as precondition; a Secret that changed since is not written. A save that removes a key is " +
			"destructive. For the user in Cogpit's window only (D-59): agents and the CLI are refused before anything is " +
			"asked or sent, whatever the parameters, and no value is ever logged. To change a Secret's values or keys, ask the " +
			"user to edit them in its drawer.",
		Enabled: func(s *engine.Session, p secretDataParams) error { return secretDataEnabled(s, p.Ref) },
		Refuse:  func(ctx context.Context, _ *engine.Session, _ secretDataParams) error { return refuseSecretData(ctx) },
		// A removed key is data lost and a change of what workloads read: destructive (N152).
		Escalate: func(_ *engine.Session, p secretDataParams) bool { return len(p.Remove) > 0 },
		Targets:  func(s *engine.Session, p secretDataParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		// The keys only, which the drawer's review binds as its confirmation of a removal.
		Effects: func(_ *engine.Session, p secretDataParams) []engine.Effect {
			return engine.SecretSaveEffects(slices.Collect(maps.Keys(p.Data)), slices.Collect(maps.Keys(p.Add)), p.Remove)
		},
		Run: func(ctx context.Context, s *engine.Session, p secretDataParams) (any, error) {
			if err := refuseSecretData(ctx); err != nil {
				return nil, err
			}
			p.From = nil // the save never rebases
			return s.SaveSecretData(ctx, p.Ref, p.ResourceVersion, p.edit())
		},
	})
}

// secretDataEnabled is the Enabled check of reviewing and saving a Secret's values: a Secret in
// the cache on a cluster Cogpit can change, whose data may change. Only the window asks it (the
// GUI-only mark refuses every other origin before).
func secretDataEnabled(s *engine.Session, ref model.ObjectRef) error {
	if ref.Kind != kindSecrets {
		return fmt.Errorf("%w: this applies to %s", engine.ErrNotApplicable, kindSecrets)
	}
	if _, err := writableOn(s, ref.Context); err != nil {
		return err
	}
	return s.SecretSavable(ref)
}
