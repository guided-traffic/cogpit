package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The hotbar (G13; engine/hotbar.go): the clusters the user pinned to the bar at the window's left
// edge, one list top to bottom (D-83: one hotbar, no banks). Every command but hotbar.list changes
// Cogpit's own preferences only (the hotbar's file under Cogpit's configuration directory), never a
// cluster and never a kubeconfig, so they are UI commands that agents run freely (D-2) and the
// audit log records; hotbar.openCluster connects, as cluster.connect does.

// HotbarResult is the result of the hotbar commands: the hotbar as the user sees it now.
type HotbarResult struct {
	Hotbar engine.HotbarState `json:"hotbar"`
}

type hotbarClusterParams struct {
	Context string `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it (a bare context name works when one file only defines it); empty or omitted: the connected context"`
}

type hotbarMoveParams struct {
	Context  string `json:"context" jsonschema:"context id of a tile on the hotbar, as ui_state's hotbar lists it"`
	Position int    `json:"position" jsonschema:"the tile's new place on the hotbar, from 1 (the top, F1); the tiles between move one place"`
}

type hotbarSlotParams struct {
	Slot int `json:"slot" jsonschema:"the tile's place on the hotbar, from 1 (the top); F1 to F12 open the first twelve"`
}

// hotbarContext resolves a tile command's context: the connected one when none is given, and a
// bare context name to the id the sources give it, as cluster.connect does.
func hotbarContext(s *engine.Session, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = s.UIState().Context
		if name == "" {
			return "", errors.New("no cluster connected: name a context")
		}
		return name, nil
	}
	return s.ResolveContext(name)
}

// hotbarResult is what a hotbar change returns: the hotbar as the user sees it.
func hotbarResult(s *engine.Session) (any, error) {
	v, err := s.HotbarView()
	if err != nil {
		return nil, err
	}
	return HotbarResult{Hotbar: v}, nil
}

// prefsOnly ends the description of every command that changes the hotbar.
const prefsOnly = " Changes only Cogpit's own hotbar preferences (a file in Cogpit's configuration directory), never a cluster or a kubeconfig."

func registerHotbar(g *registrar) {
	add(g, engine.Spec[struct{}]{
		ID: "hotbar.list", Label: "Show Hotbar", MenuPath: "Hotbar/Show Hotbar", Danger: engine.DangerRead,
		Description: "Returns the hotbar, the bar of pinned clusters at the left edge of Cogpit's window: one list of tiles " +
			"(clusters), top to bottom, numbered from 1 as F1 opens the first; F1–F12 open the first twelve, the others a click " +
			"or hotbar_openCluster. Each tile has its context id, its label (the context's name without its file), its " +
			"displayName (the name the window shows: the display name set, else the part of the label after its last @), the " +
			"state of a connection to it (several clusters stay connected at once, one shown), missing when no kubeconfig " +
			"source offers the context any more, and exec when its credential plugin needs the user's approval. ui_state " +
			"carries the same.",
		Enabled: func(s *engine.Session, _ struct{}) error { _, err := s.HotbarState(); return err },
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return hotbarResult(s)
		},
	})

	add(g, engine.Spec[hotbarClusterParams]{
		ID: "hotbar.addCluster", Label: "Add to Hotbar", MenuPath: "Hotbar/Add to Hotbar", Danger: engine.DangerUI,
		Description: "Pins a cluster to the hotbar: a tile for the context at its end. The hotbar holds as many clusters as " +
			"are pinned, a context once, and scrolls when they do not fit; F1–F12 open the first twelve. The context must be " +
			"one cluster_contexts lists." + prefsOnly,
		Enabled: func(s *engine.Session, p hotbarClusterParams) error {
			st, err := s.HotbarState()
			if err != nil {
				return err
			}
			id, err := hotbarContext(s, p.Context)
			if err != nil {
				return err
			}
			return st.CheckAdd(id)
		},
		Run: func(_ context.Context, s *engine.Session, p hotbarClusterParams) (any, error) {
			id, err := hotbarContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if err := offered(s, id); err != nil {
				return nil, err
			}
			if _, err := s.ChangeHotbar(func(h *engine.HotbarState) error { return h.AddCluster(id) }); err != nil {
				return nil, err
			}
			return hotbarResult(s)
		},
	})

	add(g, engine.Spec[hotbarClusterParams]{
		ID: "hotbar.removeCluster", Label: "Remove from Hotbar", MenuPath: "Hotbar/Remove from Hotbar", Danger: engine.DangerUI,
		Description: "Unpins a cluster: removes the context's tile from the hotbar; the tiles below it move up one place. The " +
			"context and its kubeconfig stay as they are, and a connection to it stays open." + prefsOnly,
		Enabled: func(s *engine.Session, p hotbarClusterParams) error {
			st, err := s.HotbarState()
			if err != nil {
				return err
			}
			id, err := hotbarContext(s, p.Context)
			if err != nil {
				return err
			}
			return st.CheckPinned(id)
		},
		Run: func(_ context.Context, s *engine.Session, p hotbarClusterParams) (any, error) {
			id, err := hotbarContext(s, p.Context)
			if err != nil {
				return nil, err
			}
			if _, err := s.ChangeHotbar(func(h *engine.HotbarState) error { return h.RemoveCluster(id) }); err != nil {
				return nil, err
			}
			return hotbarResult(s)
		},
	})

	add(g, engine.Spec[hotbarMoveParams]{
		ID: "hotbar.moveCluster", Label: "Move on Hotbar", MenuPath: "Hotbar/Move", Danger: engine.DangerUI,
		Description: "Moves a tile to another place on the hotbar (from 1, the top), which changes the F-key that opens it; " +
			"the tiles between move one place. Dragging a tile in the window sends the same." + prefsOnly,
		Enabled: func(s *engine.Session, p hotbarMoveParams) error {
			st, err := s.HotbarState()
			if err != nil {
				return err
			}
			id, err := s.ResolveContext(p.Context)
			if err != nil {
				return err
			}
			return st.CheckMove(id, p.Position)
		},
		Run: func(_ context.Context, s *engine.Session, p hotbarMoveParams) (any, error) {
			id, err := s.ResolveContext(p.Context)
			if err != nil {
				return nil, err
			}
			if _, err := s.ChangeHotbar(func(h *engine.HotbarState) error { return h.MoveCluster(id, p.Position) }); err != nil {
				return nil, err
			}
			return hotbarResult(s)
		},
	})

	add(g, engine.Spec[hotbarSlotParams]{
		ID: "hotbar.openCluster", Label: "Open Hotbar Cluster", MenuPath: "Hotbar/Open Cluster", Shortcut: "F1–F12", Danger: engine.DangerUI,
		Description: "Opens the cluster of a hotbar tile, as clicking it or pressing its F-key does (F1 the top tile, F12 the " +
			"twelfth; a tile further down has no key): shows its last used tab or its Pods view, exactly as cluster_connect " +
			"does: a cluster connected already is only shown, from Cogpit's cache and without a request, another one is " +
			"connected first, and the cluster shown before stays connected; the tile of the cluster shown changes nothing. " +
			"Connecting only reads from the cluster. A tile whose context no kubeconfig source offers any more is disabled. " +
			"Returns {context, state, error} as cluster_connect does: state connecting when another cluster was shown while " +
			"it waited (the connect goes on in the background), a cancelled error when cluster_disconnect cancelled it.",
		Enabled: func(s *engine.Session, p hotbarSlotParams) error {
			_, err := openableTile(s, p)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p hotbarSlotParams) (any, error) {
			t, err := openableTile(s, p)
			if err != nil {
				return nil, err
			}
			if u := s.UIState(); u.Context == t.Context && (u.Connection.State.Live() || u.Connection.State == engine.Connecting) {
				return ConnectResult{Context: u.Context, State: u.Connection.State, TLSUnchecked: u.Connection.TLSUnchecked}, nil
			}
			return connectAnswer(s, t.Context, s.Connect(ctx, t.Context))
		},
	})
}

// offered refuses a context that no kubeconfig source offers (and that is not the connected one):
// a tile is pinned for a context the user can connect to.
func offered(s *engine.Session, id string) error {
	if id == s.UIState().Context {
		return nil
	}
	ctxs, err := s.Contexts()
	for _, c := range ctxs {
		if c.Name == id {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("the contexts cannot be listed: %w", err)
	}
	return fmt.Errorf("no context %s: cluster_contexts lists the ones Cogpit offers", id)
}

// openableTile is the tile hotbar.openCluster opens: it exists, and its context is offered.
func openableTile(s *engine.Session, p hotbarSlotParams) (engine.HotbarTile, error) {
	v, err := s.HotbarView()
	if err != nil {
		return engine.HotbarTile{}, err
	}
	t, err := v.Tile(p.Slot)
	if err != nil {
		return engine.HotbarTile{}, err
	}
	if t.Missing {
		return engine.HotbarTile{}, fmt.Errorf("%s is no longer offered by any kubeconfig source; its tile stays until it is removed", t.Context)
	}
	return t, nil
}
