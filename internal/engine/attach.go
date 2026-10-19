package engine

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/terminal"
)

// openAttach starts the session of an attach dock tab (pod.attach, and pod.debug's tab on the
// debug container it added): the stream to the tab's container's main process, with stdin and a
// TTY as the container has them in the cached pod. A container that has not started yet (a debug
// container pulling its image) is waited for through the cache first, the tab saying so; the API
// server would refuse its attach (D-102).
func (s *Session) openAttach(ctx context.Context, tab DockTab, b *Backend) (terminal.Session, error) {
	pod, err := s.Object(ctx, *tab.Ref)
	if err != nil {
		return nil, err
	}
	var wait terminal.AttachWait
	if containerStarting(pod, tab.Container) {
		ref, uid, container := *tab.Ref, string(pod.GetUID()), tab.Container
		wait = terminal.AttachWait{
			Notice: "[cogpit] Waiting for " + container + " to start…",
			Before: func(ctx context.Context) error { return s.waitContainerStarted(ctx, ref, uid, container) },
		}
	}
	return terminal.StartPodAttachAfter(ctx, b.Conn.RESTConfig(), b.Conn.Kube(), AttachOptions(pod, tab.Container), wait)
}

// AttachOptions are the attach options of a pod's container: its stdin and TTY flags from the
// pod's spec (containers, then init containers, then ephemeral containers). An unknown container
// gets neither: its output is shown and nothing typed is sent.
func AttachOptions(pod *unstructured.Unstructured, container string) terminal.AttachOptions {
	opts := terminal.AttachOptions{Namespace: pod.GetNamespace(), Pod: pod.GetName(), Container: container}
	for _, field := range []string{"containers", "initContainers", specEphemeralContainers} {
		for _, c := range model.NestedSlice(pod, "spec", field) {
			if model.NestedString(c, "name") != container {
				continue
			}
			opts.Stdin, _ = model.NestedBool(c, "stdin")
			opts.TTY, _ = model.NestedBool(c, "tty")
			return opts
		}
	}
	return opts
}
