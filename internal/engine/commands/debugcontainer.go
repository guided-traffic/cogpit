package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The debug container (the owner, 2026-10-19 evening; D-102; engine/debugcontainer.go): pod.debug
// adds an ephemeral container to a running pod, as kubectl debug -it --target does, and opens the
// pod's Attach tab on it. It is destructive for every origin (the owner): an ephemeral container
// cannot be removed or changed and lives as long as the pod, and with a target it sees that
// container's processes and, through them, its files, mounted Secrets included, the node shell's
// class (D-37). The window asks in a dialog of its own that is the confirmation (debugdialog.go);
// agents and the CLI get the engine's confirmation with the same rows.

// debugParams are pod.debug's parameters.
type debugParams struct {
	Ref    model.ObjectRef `json:"ref" jsonschema:"the pod"`
	Image  string          `json:"image,omitempty" jsonschema:"the debug container's image, best pinned by digest (repository:tag@sha256:...); empty: the Preferences' debug image (Cogpit's default: nicolaka/netshoot:v0.16 pinned by digest, preferences_get's debugImage when the user set another)"`
	Target string          `json:"target,omitempty" jsonschema:"the pod's container whose processes the debug container sees (its targetContainerName, kubectl debug --target): one of the pod's containers; empty: none"`
	UID    string          `json:"uid,omitempty" jsonschema:"the pod's UID as the caller read it; when given, another pod created under the name is refused"`
}

// debugVerbs are the requests Debug makes: the patch of the pod's ephemeralcontainers
// subresource, then the attach to the new container.
var debugVerbs = []engine.Verb{
	{Verb: nwVerbPatch, Resource: kindPods, Subresource: "ephemeralcontainers"},
	{Verb: nwVerbCreate, Resource: kindPods, Subresource: subresourceAttach},
}

// subresourceAttach is a pod's attach subresource (Attach, Debug, the connection's streams).
const subresourceAttach = "attach"

// DebugResult is what pod.debug returns: the pod, the container it added and the Attach tab
// opened on it (none when the pod's watch did not bring the container in time; Attach in the
// pod's menu then reaches it).
type DebugResult struct {
	Ref       model.ObjectRef `json:"ref"`
	Container string          `json:"container"`
	Image     string          `json:"image"`
	Target    string          `json:"target,omitempty"`
	Tab       *engine.DockTab `json:"tab,omitempty"`
	Note      string          `json:"note,omitempty"`
}

// errNoDebugContainers disables pod.debug on a connection without the debug container's write.
var errNoDebugContainers = errors.New("this cluster connection cannot add debug containers")

// debugImage is the image an invocation runs: its own, else the Preferences'.
func debugImage(s *engine.Session, p debugParams) string {
	if image := strings.TrimSpace(p.Image); image != "" {
		return image
	}
	return s.DebugImage()
}

// checkDebug says why a pod cannot be debugged with p: the pod has ended or is being deleted, the
// target is none of its containers, the image is no image reference.
func checkDebug(pod *unstructured.Unstructured, p debugParams) error {
	if why := engine.PodEnded(pod); why != "" {
		return errors.New(why)
	}
	if p.Target != "" && !slices.Contains(debugTargets(pod), p.Target) {
		return fmt.Errorf("pod %s has no container %q to target; its containers: %s", pod.GetName(), p.Target,
			strings.Join(debugTargets(pod), ", "))
	}
	return engine.CheckDebugImage(strings.TrimSpace(p.Image))
}

// debugTargets are the containers a debug container may target: the pod's containers and init
// containers (the API's rule), never another ephemeral one.
func debugTargets(pod *unstructured.Unstructured) []string {
	all := engine.ContainerNames(pod, true)
	eph := engine.EphemeralContainerNames(pod)
	return slices.DeleteFunc(all, func(n string) bool { return slices.Contains(eph, n) })
}

// debugEnabled is pod.debug's Enabled check: a pod in the cache that runs, on a cluster Cogpit can
// stream from and write to.
func debugEnabled(s *engine.Session, p debugParams) error {
	pod, err := cachedOfKind(s, p.Ref, kindPods)
	if err != nil {
		return err
	}
	b, err := streamableOn(s, p.Ref.Context, "debug containers") // the pod's cluster, shown or not (D-69)
	if err != nil {
		return err
	}
	if _, ok := b.Writer().(engine.DebugActions); !ok {
		return errNoDebugContainers
	}
	return checkDebug(pod, p)
}

func registerDebugContainer(g *registrar) {
	add(g, engine.Spec[debugParams]{
		ID: engine.DebugCommand, Label: "Debug", MenuPath: "Pod/Debug", Kinds: []string{kindPods},
		// Lens's pod menu has no Debug (not verified in its documentation); Cogpit's place: after
		// Shell, before Evict (D-102).
		Order: 250, Danger: engine.DangerDestructive, Verbs: debugVerbs,
		Description: "Adds a debug container to a running pod, as kubectl debug -it --target does: an ephemeral container " +
			"named debugger-<5 characters> from image (default: the Preferences' debug image, nicolaka/netshoot:v0.16 " +
			"pinned by digest) with stdin and a TTY, sharing the processes of the target container when one is named, " +
			"then opens the pod's Attach tab on it. Kubernetes keeps an ephemeral container as long as the pod exists: " +
			"it survives the pod's container restarts, is never restarted itself, and cannot be removed or changed, so " +
			"this cannot be undone; with a target it sees that container's processes and files. Whoever asks, the user " +
			"confirms it, naming the context, the namespace, the pod, the target and the image. Logs, Shell and Attach " +
			"reach the container afterwards. Returns the container's name and the tab.",
		Enabled: debugEnabled,
		Targets: func(s *engine.Session, p debugParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Effects: func(s *engine.Session, p debugParams) []engine.Effect {
			return engine.DebugEffects(p.Target, debugImage(s, p))
		},
		// The dialog's starting values: the Preferences' image, the pod's first container as the
		// target, the pod's UID as the cache holds it (the window's dialog binds its answer to it).
		Prefill: func(s *engine.Session, p debugParams) (debugParams, error) {
			pod, err := cachedOfKind(s, p.Ref, kindPods)
			if err != nil {
				return p, err
			}
			if ref, _, err := s.ResolveRef(p.Ref); err == nil {
				p.Ref = ref // its context named, as the confirmation names it
			}
			if p.Image == "" {
				p.Image = s.DebugImage()
			}
			if targets := engine.ContainerNames(pod, false); p.Target == "" && len(targets) > 0 {
				p.Target = targets[0]
			}
			if p.UID == "" {
				p.UID = string(pod.GetUID())
			}
			return p, nil
		},
		Run: runDebug,
	})
}

// runDebug adds the debug container the user confirmed and opens the Attach tab on it once the
// pod's watch brought it into the cache.
func runDebug(ctx context.Context, s *engine.Session, p debugParams) (any, error) {
	ref, _, err := s.ResolveRef(p.Ref)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedContext(ctx, ref.Context); err != nil {
		return nil, err
	}
	pod, err := s.Object(ctx, ref)
	if err != nil {
		return nil, err
	}
	if err := checkDebug(pod, p); err != nil {
		return nil, err
	}
	image := debugImage(s, p)
	if req, ok := engine.Confirmed(ctx); ok && req.Detail != engine.DebugDetail(p.Target, image) {
		return nil, fmt.Errorf("%w: the debug container's image changed since you confirmed; nothing was added", engine.ErrTargetsChanged)
	}
	uid, err := engine.ActUID(ctx, ref, func() (string, bool) { return string(pod.GetUID()), true })
	if err != nil {
		return nil, err
	}
	if p.UID != "" && p.UID != uid {
		return nil, fmt.Errorf("%w: pod %s is not the one read (another pod has its name now); nothing was added", engine.ErrTargetsChanged, ref.Name)
	}
	name, err := s.AddDebugContainer(ctx, ref, pod, uid, image, p.Target)
	if err != nil {
		return nil, fmt.Errorf("debug pod %s: %w", ref.Name, err)
	}
	res := DebugResult{Ref: ref, Container: name, Image: image, Target: p.Target}
	if err := s.WaitDebugContainerSeen(ctx, ref, uid, name); err != nil {
		res.Note = fmt.Sprintf("added; the pod's watch did not show %s yet (%v): Attach in the pod's menu reaches it once it does", name, err)
		return res, nil
	}
	tab := engine.DockTab{
		ID: s.NewDockID(engine.DockAttach), Type: engine.DockAttach, Title: "Attach: " + ref.Name + "/" + name,
		Context: ref.Context, Ref: &ref, Container: name,
	}
	s.UpdateUI(func(u *engine.UIState) {
		u.Dock = append(u.Dock, tab)
		u.ActiveDock = tab.ID
	})
	res.Tab = &tab
	return res, nil
}
