package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Accepted node conditions (D-163): the condition types a cluster's settings name never warn on that
// cluster, on top of the built-in ones (kinds.BuiltinAcceptedNodeConditions). A kind profile is pure
// and shared, so a cluster that accepts any gets a Nodes profile of its own, on its backend; a new
// profile is what restarts the views, the drawer and the Overview that read the old one.

const (
	maxAcceptedNodeConditions = 100
	maxNodeConditionType      = 256
)

// ErrAcceptedNodeConditionsGUIOnly refuses an agent or the CLI accepting a node condition.
var ErrAcceptedNodeConditionsGUIOnly = errors.New("only the user accepts a node condition, in Cogpit's window (Cluster Settings › Node Conditions): " +
	"it takes a warning out of the window; agents and the CLI may only clear the list")

// RefuseAcceptedNodeConditions refuses every origin but the window a list that names a condition.
func RefuseAcceptedNodeConditions(ctx context.Context, types []string) error {
	if len(sortedAcceptedNodeConditions(types)) > 0 && OriginOf(ctx) != OriginGUI {
		return ErrAcceptedNodeConditionsGUIOnly
	}
	return nil
}

// checkAcceptedNodeConditions says why a list cannot be kept. Ready is refused: a node that is not
// Ready must always show, and the rest of the list is the user's to judge.
func checkAcceptedNodeConditions(types []string) error {
	if len(types) > maxAcceptedNodeConditions {
		return fmt.Errorf("at most %d accepted node conditions", maxAcceptedNodeConditions)
	}
	for _, t := range types {
		if err := checkNodeConditionType(t); err != nil {
			return err
		}
	}
	return nil
}

func checkNodeConditionType(t string) error {
	switch {
	case t == "Ready":
		return errors.New("the Ready condition cannot be accepted: a node that is not Ready always shows")
	case t == "" || len(t) > maxNodeConditionType:
		return fmt.Errorf("node condition %q: 1 to %d characters", t, maxNodeConditionType)
	case strings.ContainsFunc(t, unicode.IsSpace):
		return fmt.Errorf("node condition %q: a condition type has no spaces", t)
	}
	return nil
}

// sortedAcceptedNodeConditions is the form the settings keep: trimmed, sorted, without duplicates.
func sortedAcceptedNodeConditions(types []string) []string { return sortedAccessibleNamespaces(types) }

// keptAcceptedNodeConditions is what a settings file's list becomes on load: the valid types.
func keptAcceptedNodeConditions(types []string) []string {
	kept := slices.DeleteFunc(slices.Clone(sortedAcceptedNodeConditions(types)), func(t string) bool {
		return checkNodeConditionType(t) != nil
	})
	if len(kept) > maxAcceptedNodeConditions {
		kept = kept[:maxAcceptedNodeConditions]
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// acceptedNodeConditionsOf is a context's accepted types; nil-safe.
func (c *ClusterSettingsStore) acceptedNodeConditionsOf(context string) []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.load() // an unreadable file is reported where it loads
	return c.by[context].AcceptedNodeConditions
}

// acceptedNodeProfile is a cluster's Nodes profile and the types it accepts.
type acceptedNodeProfile struct {
	profile *model.KindProfile
	types   []string
}

// setAcceptedNodeConditions gives the backend the Nodes profile for these types. The same types keep
// the same profile, so that a change of another setting restarts no view.
func (b *Backend) setAcceptedNodeConditions(types []string) {
	var cur []string
	if a := b.acceptedNodes.Load(); a != nil {
		cur = a.types
	}
	if slices.Equal(cur, types) {
		return
	}
	i := slices.IndexFunc(b.Profiles, func(p *model.KindProfile) bool { return p.Key == kindNodes })
	if i < 0 || len(types) == 0 {
		b.acceptedNodes.Store(nil)
		return
	}
	b.acceptedNodes.Store(&acceptedNodeProfile{profile: kinds.NodesAccepting(b.Profiles[i], types), types: slices.Clone(types)})
}

// withAcceptedNodeConditions is p, or the cluster's own Nodes profile for the Nodes kind.
func (b *Backend) withAcceptedNodeConditions(p *model.KindProfile) *model.KindProfile {
	if p.Key == kindNodes {
		if a := b.acceptedNodes.Load(); a != nil {
			return a.profile
		}
	}
	return p
}

// acceptedNodeConditionsKey is the accepted types for a page's plan key: a page that holds the Nodes
// profile starts again when they change.
func (b *Backend) acceptedNodeConditionsKey() string {
	if a := b.acceptedNodes.Load(); a != nil {
		return "|accepted:" + strings.Join(a.types, ",")
	}
	return ""
}

// acceptedNodeConditionsChanged applies a cluster's list to its connected backend.
func (s *Session) acceptedNodeConditionsChanged(context string, types []string) {
	for _, b := range s.Backends() {
		if b.Context == context {
			b.setAcceptedNodeConditions(types)
		}
	}
}
