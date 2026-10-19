package commands

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A Config Map's values edited in the drawer (the owner's F3, 2026-10-15; engine/configmapdata.go):
// configMap.readData reads them whole from the cache, configMap.reviewData diffs and dry-runs a
// change, configMap.saveData writes it with the resourceVersion it is based on as precondition,
// through the same writer, dry run and conflict rule as the Edit tab's object.apply. Keys are added
// (add) and removed (remove) in the same write (the owner's audit decision of 2026-10-16,
// engine/datakeys.go). The save is mutating: an agent's waits for the user's acceptance unless the
// user allowed agent writes for the context (D-2), and the confirmation shows the diff it writes. A
// save that removes a key is destructive: it changes what workloads read and loses the value, so
// the user confirms it whoever asks, the drawer's review being that confirmation in the window.

// configMapDataVerbs are the verbs of saving values: an update of the Config Map, read from the
// cache. The review's dry run is an update too, which the server checks as one.
var configMapDataVerbs = []engine.Verb{
	{Verb: nwVerbList, Resource: kindConfigMaps}, {Verb: nwVerbWatch, Resource: kindConfigMaps}, {Verb: dataVerbUpdate, Resource: kindConfigMaps},
}

// configMapReads are the verbs of reading a Config Map's values from the cache.
var configMapReads = []engine.Verb{{Verb: nwVerbList, Resource: kindConfigMaps}, {Verb: nwVerbWatch, Resource: kindConfigMaps}}

type configMapDataParams struct {
	Ref             model.ObjectRef   `json:"ref" jsonschema:"the Config Map, e.g. {kind: configmaps, namespace: shop, name: app}"`
	ResourceVersion string            `json:"resourceVersion" jsonschema:"the version of the Config Map the values are based on, as configMap_readData returned it; a Config Map that changed since is not written"`
	Data            map[string]string `json:"data,omitempty" jsonschema:"the keys whose values change, each with its whole new value; keys not named keep their values; only keys the data holds (a new key goes in add)"`
	Add             map[string]string `json:"add,omitempty" jsonschema:"keys to add, each with its value; a key has at most 253 characters, letters, digits, '-', '_' and '.' only, and must not be one the data holds"`
	Remove          []string          `json:"remove,omitempty" jsonschema:"keys of the data to remove with their values; a save that removes a key always waits for the user's acceptance"`
}

// edit is the parameters' change of the data.
func (p configMapDataParams) edit() engine.DataEdit {
	return engine.DataEdit{Values: p.Data, Add: p.Add, Remove: p.Remove}
}

type configMapReviewParams struct {
	Ref             model.ObjectRef   `json:"ref" jsonschema:"the Config Map, e.g. {kind: configmaps, namespace: shop, name: app}"`
	ResourceVersion string            `json:"resourceVersion" jsonschema:"the version of the Config Map the values are based on, as configMap_readData returned it"`
	Data            map[string]string `json:"data,omitempty" jsonschema:"the keys whose values change, each with its whole new value; only keys the data holds (a new key goes in add)"`
	Add             map[string]string `json:"add,omitempty" jsonschema:"keys to add, each with its value; not keys the data holds"`
	Remove          []string          `json:"remove,omitempty" jsonschema:"keys of the data to remove"`
	From            map[string]string `json:"from,omitempty" jsonschema:"the changed and removed keys' values at resourceVersion, as configMap_readData returned them: when the Config Map changed since but none of these keys did and no added key appeared, the edit is rebased onto the newer version (rebasedFrom) instead of reported stale"`
	Context         *int              `json:"context,omitempty" jsonschema:"unchanged lines to keep around each change; default 3; -1 keeps every line"`
}

type configMapKeepParams struct {
	Ref             model.ObjectRef   `json:"ref" jsonschema:"the Config Map the drawer shows"`
	ResourceVersion string            `json:"resourceVersion,omitempty" jsonschema:"the version the edited values are based on"`
	Data            map[string]string `json:"data,omitempty" jsonschema:"the edited keys with their unsaved text; empty data, add and remove drop the kept edits and their draft"`
	From            map[string]string `json:"from,omitempty" jsonschema:"each edited or removed key's value at resourceVersion"`
	Add             map[string]string `json:"add,omitempty" jsonschema:"the keys added and not saved, with their text"`
	Remove          []string          `json:"remove,omitempty" jsonschema:"the keys removed and not saved"`
	Revision        uint64            `json:"revision" jsonschema:"orders the window's calls: one older than the newest for the Config Map is ignored"`
}

func registerConfigMapData(g *registrar) {
	add(g, engine.Spec[configMapKeepParams]{
		ID: "configMap.keepEdits", Label: "Keep Unsaved Config Map Values", Danger: engine.DangerUI,
		GUIOnly: engine.ErrConfigMapEditsGUIOnly,
		Description: "Keeps the values the user edited in a Config Map's drawer and has not saved, and the keys added and removed there: in memory for the " +
			"drawer's next visit and, a moment after the typing stops, as a draft on disk (owner-only) that a quit or a " +
			"crash leaves and editor_drafts offers when the context connects again; empty data drops them and the draft " +
			"(saved or reverted). The user's own window only: agents and the CLI are refused, whatever the parameters " +
			"(an agent changes values with configMap_saveData). Writes nothing to the cluster.",
		Run: func(_ context.Context, s *engine.Session, p configMapKeepParams) (any, error) {
			return nil, s.KeepConfigMapEdits(p.Ref, p.Revision, engine.ConfigMapEdits{BaseResourceVersion: p.ResourceVersion, Values: p.Data,
				From: p.From, Add: p.Add, Remove: p.Remove})
		},
	})

	add(g, engine.Spec[refParams]{
		ID: "configMap.readData", Label: "Read Config Map Data", Kinds: []string{kindConfigMaps}, Danger: engine.DangerRead,
		Verbs: configMapReads,
		Description: "Returns a Config Map's data from Cogpit's cache, the first 100 keys sorted, each with its whole value, and " +
			"the resourceVersion they are of: what configMap_reviewData and configMap_saveData take. readOnly says why none " +
			"can be saved (immutable, a Helm release record, whose payload is never returned). Binary data is not returned. " +
			"Makes no request." + privateReads,
		Enabled: func(s *engine.Session, p refParams) error {
			if p.Ref.Kind != kindConfigMaps {
				return fmt.Errorf("%w: this applies to %s", engine.ErrNotApplicable, kindConfigMaps)
			}
			_, err := connectedOn(s, p.Ref.Context) // the Config Map is read when the command runs, after its kind is listed
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			return s.ConfigMapData(ctx, p.Ref)
		},
	})

	add(g, engine.Spec[configMapReviewParams]{
		ID: "configMap.reviewData", Label: "Review Config Map Data", Kinds: []string{kindConfigMaps}, Danger: engine.DangerRead,
		Verbs: configMapDataVerbs,
		Description: "Shows what configMap_saveData would write: the line diff of the Config Map's YAML with the given values, " +
			"keys added and keys removed against the object as Cogpit's cache holds it (op \"-\" removed, \"+\" added, \"=\" " +
			"context), the keys whose values change (keys), the keys added (keysAdded) and removed (keysRemoved), and the API " +
			"server's verdict on a dry run (accepted, or refused with its reason). Stores nothing. Given from (the changed and " +
			"removed keys' values at resourceVersion), a Config Map that changed since but not in those keys, and where no " +
			"added key appeared, is reviewed against its newer version: rebasedFrom names resourceVersion, " +
			"baseResourceVersion the newer one, which the save then takes. stale means the Config Map changed since " +
			"resourceVersion in a key of the edit (or from was not given): the lines are the edit against the newer version " +
			"(cachedResourceVersion), and a save based on resourceVersion is refused. Needs the update permission.",
		Enabled: func(s *engine.Session, p configMapReviewParams) error { return configMapDataEnabled(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p configMapReviewParams) (any, error) {
			n := engine.DefaultDiffContext
			if p.Context != nil {
				n = *p.Context
			}
			return s.ReviewConfigMapData(ctx, p.Ref, p.ResourceVersion, engine.DataEdit{Values: p.Data, Add: p.Add, Remove: p.Remove, From: p.From}, n)
		},
	})

	add(g, engine.Spec[configMapDataParams]{
		ID: "configMap.saveData", Label: "Save Config Map Data", Kinds: []string{kindConfigMaps}, Danger: engine.DangerMutating,
		Verbs: configMapDataVerbs,
		Description: "Writes new values for keys of a Config Map's data, adds keys and removes keys, as the drawer's Save does: " +
			"the Config Map from Cogpit's cache with the edit applied, a server-side dry run, then an update with " +
			"resourceVersion as precondition. If the Config Map changed since resourceVersion nothing is written and the error " +
			"names the version it is at now; saving again with that version writes the edit over it. A key in data must be one " +
			"the data holds, a key in add one it does not hold, a key in remove one it holds; binary data and a Helm release " +
			"record are not changed, an immutable Config Map is refused. Returns the keys that changed, were added and were " +
			"removed, and the stored resourceVersion. An agent's request waits for the user's acceptance, which shows the " +
			"diff, unless the user allowed agent writes for the context; one that removes a key is destructive and always " +
			"waits for it.",
		Enabled: func(s *engine.Session, p configMapDataParams) error { return configMapDataEnabled(s, p.Ref) },
		Refuse: func(ctx context.Context, _ *engine.Session, p configMapDataParams) error {
			for _, values := range []map[string]string{p.Data, p.Add} { // a placeholder Cogpit showed in place of a Secret's value (H-82), before asking
				for _, v := range values {
					if err := engine.GuardAgentMarkers(ctx, v); err != nil {
						return err
					}
				}
			}
			return nil
		},
		// A removed key is data lost and a change of what workloads read: destructive (N152).
		Escalate: func(_ *engine.Session, p configMapDataParams) bool { return len(p.Remove) > 0 },
		Targets:  func(s *engine.Session, p configMapDataParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		// The keys removed and added, which the drawer's review binds as its confirmation.
		Effects: func(_ *engine.Session, p configMapDataParams) []engine.Effect {
			return engine.DataKeysEffects(slices.Collect(maps.Keys(p.Add)), p.Remove)
		},
		// The user reads what an agent's save writes before answering (as an agent's Helm values).
		Review: func(s *engine.Session, p configMapDataParams) string {
			return s.ConfigMapDataReviewText(p.Ref, p.ResourceVersion, p.edit())
		},
		Run: func(ctx context.Context, s *engine.Session, p configMapDataParams) (any, error) {
			return s.SaveConfigMapData(ctx, p.Ref, p.ResourceVersion, p.edit())
		},
	})
}

// configMapDataEnabled is the Enabled check of reviewing and saving values: a Config Map in the
// cache on a cluster Cogpit can change, whose data may change. It reads the cache without
// waiting; the values are checked when the command runs.
func configMapDataEnabled(s *engine.Session, ref model.ObjectRef) error {
	if ref.Kind != kindConfigMaps {
		return fmt.Errorf("%w: this applies to %s", engine.ErrNotApplicable, kindConfigMaps)
	}
	if _, err := writableOn(s, ref.Context); err != nil {
		return err
	}
	return s.ConfigMapSavable(ref)
}
