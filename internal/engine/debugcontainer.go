package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The debug container (the owner, 2026-10-19 evening; D-102; docs/security/privilege-footprint.md
// › Debug container): pod.debug (commands/debugcontainer.go) adds an ephemeral container to a
// running pod, as `kubectl debug -it --target` does: a container "debugger-<5 characters>" from the
// image the user confirmed (the Preferences' DebugImage by default, nicolaka/netshoot pinned by
// digest), with stdin and a TTY, sharing the target container's processes; then the pod's Attach
// tab opens on it. Kubernetes keeps an ephemeral container as long as the pod object: it survives
// the pod's container restarts, is never restarted itself, and the API cannot remove or change it,
// so adding one cannot be undone and is destructive for every origin (the owner). The new container
// reaches Cogpit through the pod's watch, never through a read of its own (cache first).

// The debug container's names and bounds.
const (
	// DebugCommand is the id of the command that adds a debug container.
	DebugCommand = "pod.debug"

	// DefaultDebugImage is nicolaka/netshoot v0.16, the owner's choice (2026-10-19), pinned to the
	// digest of its multi-arch index (read by the lead with a HEAD of the tag on registry-1.docker.io
	// with the OCI index Accept header, 2026-10-19), as the node shell's default is pinned (D-37).
	// Renovate moves the tag and the digest (.github/renovate.json reads the comment below), and
	// CI's kind job runs every such change in a pod (TestKindDebugContainer).
	// renovate: datasource=docker depName=docker.io/nicolaka/netshoot
	DefaultDebugImage = "docker.io/nicolaka/netshoot:v0.16@sha256:b09d9b21381f47a79b3cbcb30da25266dc17186ea00ae65e99fdc51396f48e70"

	// DebugContainerPrefix starts a debug container's name, as kubectl debug names one.
	DebugContainerPrefix = "debugger-"

	// debugSeenTimeout bounds the wait for the pod's watch to bring the new container into the
	// cache (it normally takes milliseconds); containerStartTimeout the wait of an attach for a
	// container to start (an image pull, as the node shell's start allows).
	debugSeenTimeout      = 30 * time.Second
	containerStartTimeout = nodeShellStartTimeout
)

// DefaultDebugImageName is DefaultDebugImage as the texts name it: nicolaka/netshoot:v0.16.
var DefaultDebugImageName = shortImage(DefaultDebugImage)

// shortImage is an image reference without its digest and Docker Hub's registry and library path.
func shortImage(image string) string {
	if at := strings.LastIndex(image, "@"); at >= 0 {
		image = image[:at]
	}
	image = strings.TrimPrefix(image, "docker.io/")
	return strings.TrimPrefix(image, "library/")
}

// ErrDebugImageGUIOnly refuses an agent or the CLI setting the debug container's image: it decides
// what the user's next Debug runs beside a pod's processes. Resetting it to the default is allowed.
var ErrDebugImageGUIOnly = errors.New("the debug container's image is set only by the user, in Cogpit's window " +
	"(Preferences › Kubernetes): an agent or the CLI may reset it to the default, not set it")

// CheckDebugImage refuses what cannot be an image reference, as CheckNodeShellImage does; empty is
// the default.
func CheckDebugImage(image string) error {
	return checkImageReference("the debug container's image", "docker.io/nicolaka/netshoot:v0.16", image)
}

// RefuseDebugImage refuses a debug image preference set to anything but the default (empty) from
// every origin but the GUI.
func RefuseDebugImage(ctx context.Context, image *string) error {
	if image == nil || OriginOf(ctx) == OriginGUI || strings.TrimSpace(*image) == "" {
		return nil
	}
	return ErrDebugImageGUIOnly
}

// DebugImage is the image Debug offers: the Preferences' DebugImage, else DefaultDebugImage.
func (s *Session) DebugImage() string {
	p, err := s.Preferences()
	if err != nil || p.DebugImage == "" {
		return DefaultDebugImage
	}
	return p.DebugImage
}

// DebugActions are the cluster side of a debug container. *cluster.Conn implements them; tests put
// a fake in Backend.Actions.
type DebugActions interface {
	// AddEphemeralContainer adds ec to the pod with uid through its ephemeralcontainers
	// subresource; another pod under the name is refused (cluster.ErrReplaced).
	AddEphemeralContainer(ctx context.Context, namespace, pod, uid string, ec corev1.EphemeralContainer) error
}

var _ DebugActions = (*cluster.Conn)(nil)

// imageEffects are an image's rows in a confirmation: its name, and its digest or that it is a
// floating tag (the node shell's and the debug container's, D-37, D-102). The digest is a row of
// its own: the dialog breaks it where it must.
func imageEffects(image string) []Effect {
	if ImagePinned(image) {
		at := strings.LastIndex(image, "@")
		return []Effect{{Name: "Image", Value: image[:at], Tone: EffectNames}, {Name: "Digest", Value: image[at+1:]}}
	}
	return []Effect{{Name: "Image", Value: image, Tone: EffectNames},
		{Name: "Digest", Value: "none: a floating tag, what it runs can change without Cogpit knowing", Tone: EffectWarning}}
}

// DebugEffects are a debug container's rows in its confirmation, after Context › Namespace › Pod
// (the owner's order, D-85): the target, what the container sees of it, and the image with its
// digest. No sentence: "This cannot be undone." says the rest (D-102).
func DebugEffects(target, image string) []Effect {
	out := make([]Effect, 0, 4)
	if target == "" {
		out = append(out, Effect{Name: "Target", Value: "none"})
	} else {
		out = append(out, Effect{Name: "Target", Value: target, Tone: EffectNames},
			Effect{Name: "Sees", Value: target + "'s processes and files", Tone: EffectWarning})
	}
	return append(out, imageEffects(image)...)
}

// DebugDetail is DebugEffects' text, the confirmation's Detail, to which the window's Debug dialog
// binds the user's answer.
func DebugDetail(target, image string) string { return EffectsText(DebugEffects(target, image)) }

// DebugContainerName returns a new debug container's name: DebugContainerPrefix and five random
// lowercase characters, as kubectl debug names one, none of the names taken.
func DebugContainerName(taken []string) string {
	for {
		name := DebugContainerPrefix + strings.ToLower(rand.Text()[:5])
		if !slices.Contains(taken, name) {
			return name
		}
	}
}

// DebugContainer is the ephemeral container Debug adds, as kubectl debug -it builds it: the image,
// stdin and a TTY (so that an attach is a shell and it outlives a detach), the termination message
// from its file, and the target whose processes it shares ("" for none). No command: the image's
// own (netshoot's zsh).
func DebugContainer(name, image, target string) corev1.EphemeralContainer {
	return corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name: name, Image: image, Stdin: true, TTY: true,
			TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		},
		TargetContainerName: target,
	}
}

// PodEnded reports why a pod cannot be debugged: it has ended (Succeeded or Failed) or is being
// deleted; "" when it can.
func PodEnded(pod *unstructured.Unstructured) string {
	switch phase := model.NestedString(pod.Object, "status", "phase"); {
	case pod.GetDeletionTimestamp() != nil:
		return "the pod is being deleted"
	case phase == string(corev1.PodSucceeded) || phase == string(corev1.PodFailed):
		return "the pod has ended (" + strings.ToLower(phase) + ")"
	}
	return ""
}

// AddDebugContainer adds a debug container to the pod ref names, the one with uid as the cache
// held it, from image with target ("" for none), and returns its name. The caller (pod.debug) was
// confirmed for that pod, image and target; pod is the cached pod, whose container names the new
// name avoids.
func (s *Session) AddDebugContainer(ctx context.Context, ref model.ObjectRef, pod *unstructured.Unstructured, uid, image, target string) (string, error) {
	b, err := s.TargetBackend(ref.Context) // the pod's cluster, shown or not (D-69)
	if err != nil {
		return "", err
	}
	w, ok := b.Writer().(DebugActions)
	if !ok {
		return "", errors.New("this cluster connection cannot add debug containers")
	}
	name := DebugContainerName(ContainerNames(pod, true))
	if err := w.AddEphemeralContainer(ctx, ref.Namespace, ref.Name, uid, DebugContainer(name, image, target)); err != nil {
		if errors.Is(err, cluster.ErrReplaced) {
			return "", fmt.Errorf("%w: %w", ErrTargetsChanged, err)
		}
		return "", err
	}
	return name, nil
}

// EphemeralContainerNames returns the names of a pod's ephemeral (debug) containers.
func EphemeralContainerNames(pod *unstructured.Unstructured) []string {
	return containerNames(pod, specEphemeralContainers)
}

// errPodGone ends a wait whose pod was deleted, or replaced under its name, meanwhile.
var errPodGone = errors.New("the pod is gone")

// waitPod waits through the cache, bounded by ctx, until done says the pod ref names (the one with
// uid) is as it wants, or fails: done's error, the pod deleted or replaced, ctx's end. It reads
// the pods of ref's namespace as any view of them does, and decides at each event.
func (s *Session) waitPod(ctx context.Context, ref model.ObjectRef, uid string, done func(*unstructured.Unstructured) (bool, error)) error {
	b, ref, p, err := s.resolve(ref)
	if err != nil {
		return err
	}
	h, err := b.readFollowing(ctx, p, p.Key, b.inNamespace(p, ref.Namespace))
	if err != nil {
		return err
	}
	defer h.Release()
	wake := make(chan struct{}, 1)
	unsub := h.Subscribe(func(cache.Event) { signal(wake) })
	defer unsub()
	for {
		pod, ok := h.Get(ref.Namespace, ref.Name)
		switch {
		case !ok:
			return fmt.Errorf("%s: %w: deleted", ref.String(), errPodGone)
		case uid != "" && string(pod.GetUID()) != uid:
			return fmt.Errorf("%s: %w: replaced by another pod under its name", ref.String(), errPodGone)
		}
		if ok, err := done(pod); ok || err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		}
	}
}

// WaitDebugContainerSeen waits, through the pod's watch and at most debugSeenTimeout, until the
// cache holds the pod with uid with the ephemeral container name in its spec: the Attach tab reads
// its stdin and TTY there.
func (s *Session) WaitDebugContainerSeen(ctx context.Context, ref model.ObjectRef, uid, name string) error {
	wctx, cancel := context.WithTimeout(ctx, debugSeenTimeout)
	defer cancel()
	return s.waitPod(wctx, ref, uid, func(pod *unstructured.Unstructured) (bool, error) {
		return slices.Contains(containerNames(pod, specEphemeralContainers), name), nil
	})
}

// statusOfContainer returns a container's status from a pod's three status lists, nil without one.
func statusOfContainer(pod *unstructured.Unstructured, container string) any {
	for _, field := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
		for _, st := range model.NestedSlice(pod.Object, "status", field) {
			if model.NestedString(st, "name") == container {
				return st
			}
		}
	}
	return nil
}

// containerStarting reports whether a container of the pod has not started yet: in its spec,
// without a status or waiting. Its attach would be refused, so the attach waits for it.
func containerStarting(pod *unstructured.Unstructured, container string) bool {
	if !slices.Contains(ContainerNames(pod, true), container) {
		return false
	}
	st := statusOfContainer(pod, container)
	return st == nil || model.NestedMap(st, "state", "waiting") != nil
}

// startFailures are the waiting reasons with which a container does not start as it is: its image
// cannot be pulled or named, or the container cannot be made (cluster.WaitPodRunning's).
var startFailures = []string{"ErrImagePull", "ImagePullBackOff", "InvalidImageName", "ErrImageNeverPull",
	"CreateContainerConfigError", "CreateContainerError", "RunContainerError"}

// waitContainerStarted waits through the cache, at most containerStartTimeout, until a container
// of the pod with uid runs; it fails at once when the container ended, or waits for a reason that
// does not pass (an image that cannot be pulled), naming the reason.
func (s *Session) waitContainerStarted(ctx context.Context, ref model.ObjectRef, uid, container string) error {
	wctx, cancel := context.WithTimeout(ctx, containerStartTimeout)
	defer cancel()
	err := s.waitPod(wctx, ref, uid, func(pod *unstructured.Unstructured) (bool, error) {
		st := statusOfContainer(pod, container)
		switch {
		case st == nil:
			return false, nil
		case model.NestedMap(st, "state", "running") != nil:
			return true, nil
		case model.NestedMap(st, "state", "terminated") != nil:
			return false, fmt.Errorf("%s ended before it could be attached to", container)
		}
		if reason := model.NestedString(st, "state", "waiting", "reason"); slices.Contains(startFailures, reason) {
			if msg := model.NestedString(st, "state", "waiting", "message"); msg != "" {
				reason += ": " + msg
			}
			return false, fmt.Errorf("%s does not start: %s", container, reason)
		}
		return false, nil
	})
	if err != nil && ctx.Err() == nil && errors.Is(wctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s did not start within %s", container, NodeShellDeadlineText(containerStartTimeout))
	}
	return err
}
