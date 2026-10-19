package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The node shell (G7, G8; D-37; engine/nodeshell.go): node.shell opens a root shell on a node
// through a privileged pod in kube-system, Lens's node shell, and cluster.settings.setNodeShell
// sets its image, image pull secret and maximum duration (Cluster Settings › Node Shell). node.shell is destructive:
// every origin confirms it, the GUI too, and the confirmation names the node, the namespace, the
// image and that the pod is privileged with root access to the node (privilege-footprint.md › Node
// shell).

// nodeShellVerbDelete is the verb of the node shell pod's delete.
const nodeShellVerbDelete = "delete"

// nodeShellVerbs are the requests a node shell makes, all in kube-system whatever the node: the
// pod's create, the get and the watch on that pod while it starts, the exec into it, and its
// delete. The access reviews are asked in kube-system (Verb.Namespace).
var nodeShellVerbs = []engine.Verb{
	{Verb: nwVerbCreate, Resource: kindPods, Namespace: engine.NodeShellNamespace},
	{Verb: nwVerbGet, Resource: kindPods, Namespace: engine.NodeShellNamespace},
	{Verb: nwVerbWatch, Resource: kindPods, Namespace: engine.NodeShellNamespace},
	{Verb: nodeShellVerbDelete, Resource: kindPods, Namespace: engine.NodeShellNamespace},
	{Verb: nwVerbCreate, Resource: kindPods, Subresource: "exec", Namespace: engine.NodeShellNamespace},
}

type nodeShellSettingsParams struct {
	Context         string  `json:"context,omitempty" jsonschema:"context id as cluster_contexts lists it; empty or omitted: the connected context"`
	Image           *string `json:"image,omitempty" jsonschema:"the node shells' image, best pinned by digest (repository:tag@sha256:...); empty: Cogpit's default, the newest supported Alpine release pinned by digest (nodeShellDefaultImage in cluster_settings_get). Agents and the CLI may only reset it (an empty string)"`
	ImagePullSecret *string `json:"imagePullSecret,omitempty" jsonschema:"the name of an image pull secret in kube-system for that image; empty: none. Agents and the CLI may only clear it"`
	MaxDuration     *string `json:"maxDuration,omitempty" jsonschema:"how long a node shell's pod lives at most (its activeDeadlineSeconds and its container's sleep), in whole minutes from 5m to 24h, such as 90m, 2h or 1h30m; empty: Cogpit's default, 2h. Agents and the CLI may only reset it (an empty string)"`
}

// refuseNodeShellSettings refuses every origin but the GUI a value that is not the default: an
// image or pull secret decides what runs as root on the node at the user's next node shell, the
// maximum duration for how long.
func refuseNodeShellSettings(ctx context.Context, p nodeShellSettingsParams) error {
	if engine.OriginOf(ctx) == engine.OriginGUI {
		return nil
	}
	for _, v := range []*string{p.Image, p.ImagePullSecret, p.MaxDuration} {
		if v != nil && strings.TrimSpace(*v) != "" {
			return engine.ErrNodeShellGUIOnly
		}
	}
	return nil
}

// nodeShellEnabled is node.shell's Enabled check: a node in the cache, a cluster Cogpit can
// stream from and write to, and a connection that opens node shells.
func nodeShellEnabled(s *engine.Session, ref model.ObjectRef) error {
	if _, err := cachedOfKind(s, ref, nodeKindKey); err != nil {
		return err
	}
	b, err := streamableOn(s, ref.Context, "node shells") // the node's cluster, shown or not (D-69)
	if err != nil {
		return err
	}
	if _, ok := b.Writer().(engine.NodeShellActions); !ok {
		return errNoNodeShells
	}
	return nil
}

// errNoNodeShells disables node.shell on a connection without the node shell's writes.
var errNoNodeShells = errors.New("this cluster connection cannot open node shells")

// nodeShellEffects are node.shell's rows in the confirmation, from the node's context's settings
// (D-69: the reference's cluster, the shown one without a context).
func nodeShellEffects(s *engine.Session, p refParams) []engine.Effect {
	ctx := ""
	if b := s.BackendOf(p.Ref.Context); b != nil {
		ctx = b.Context
	}
	return engine.NodeShellEffects(p.Ref.Name, s.NodeShellSettings(ctx))
}

// nodeShellBounds are the maximum duration's bounds and default as the descriptions name them.
var nodeShellBounds = fmt.Sprintf("default %s, from %s to %s", engine.NodeShellDeadlineText(engine.DefaultNodeShellDeadline),
	engine.NodeShellDeadlineText(engine.MinNodeShellDeadline), engine.NodeShellDeadlineText(engine.MaxNodeShellDeadline))

func registerNodeShell(g *registrar) {
	add(g, engine.Spec[refParams]{
		ID: engine.NodeShellCommand, Label: "Shell", MenuPath: "Node/Shell", Kinds: []string{nodeKindKey},
		Order: 100, Danger: engine.DangerDestructive, Verbs: nodeShellVerbs, Undoable: true, // nothing is lost (A101)
		Description: "Opens a root shell on a node, as Lens's node shell does: creates a privileged pod in kube-system on the " +
			"node that shares the node's processes (hostPID), from the image Cluster Settings › Node Shell names (default: " +
			engine.DefaultNodeShellImageName + " pinned by digest), and enters the node's namespaces with nsenter in a dock tab " +
			"\"Node shell: <node>\". The pod tolerates every taint, mounts no service account token, ends by itself after " +
			"the maximum duration Cluster Settings › Node Shell sets (activeDeadlineSeconds; " + nodeShellBounds + "), and " +
			"is deleted when the shell ends, its tab closes or Cogpit quits. Whoever asks, the user confirms it, naming the " +
			"node, the namespace, the image and the maximum duration. Returns the tab, which names the pod.",
		Effects: nodeShellEffects,
		Enabled: func(s *engine.Session, p refParams) error { return nodeShellEnabled(s, p.Ref) },
		Targets: func(s *engine.Session, p refParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			tab, err := s.StartNodeShell(ctx, p.Ref)
			if err != nil {
				return nil, err
			}
			return DockResult{Tab: tab, Created: true}, nil
		},
	})

	add(g, engine.Spec[nodeShellSettingsParams]{
		ID: "cluster.settings.setNodeShell", Label: "Set Node Shell Settings", Danger: engine.DangerUI,
		Description: "Sets a context's node shell image, image pull secret (in kube-system) and maximum duration, as Cluster " +
			"Settings › Node Shell does; a field left out stays, an empty value restores the default (Cogpit's image pinned " +
			"by digest, no pull secret, " + engine.FormatNodeShellDeadline(engine.DefaultNodeShellDeadline) + "). The maximum " +
			"duration is how long a node shell's pod lives at most, whole minutes from " +
			engine.FormatNodeShellDeadline(engine.MinNodeShellDeadline) + " to " + engine.FormatNodeShellDeadline(engine.MaxNodeShellDeadline) +
			" written as 90m, 2h or 1h30m. An image that is not pinned by digest is accepted, and the node shell's " +
			"confirmation then says it is a floating tag. Agents and the CLI may reset all three to the defaults but not " +
			"set them: they decide what runs as root on the node, and for how long." + clusterSettingsOnly + " Returns the settings.",
		Enabled: func(s *engine.Session, p nodeShellSettingsParams) error {
			_, err := settingsContext(s, p.Context)
			return err
		},
		Refuse: func(ctx context.Context, _ *engine.Session, p nodeShellSettingsParams) error {
			return refuseNodeShellSettings(ctx, p)
		},
		Run: func(ctx context.Context, s *engine.Session, p nodeShellSettingsParams) (any, error) {
			if err := refuseNodeShellSettings(ctx, p); err != nil {
				return nil, err
			}
			return changeSettings(s, p.Context, func(cs *engine.ClusterSettings) error {
				if p.Image != nil {
					cs.NodeShellImage = strings.TrimSpace(*p.Image)
				}
				if p.ImagePullSecret != nil {
					cs.NodeShellPullSecret = strings.TrimSpace(*p.ImagePullSecret)
				}
				if p.MaxDuration != nil {
					minutes, err := engine.ParseNodeShellDeadline(*p.MaxDuration)
					if err != nil {
						return err
					}
					cs.NodeShellMaxMinutes = minutes
				}
				return nil
			})
		},
	})
}
