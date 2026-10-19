package terminal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// AttachOptions select a container to attach to.
type AttachOptions struct {
	Namespace string
	Pod       string
	// Container is the container to attach to; empty leaves the choice to the API server, which
	// accepts it only for a pod with one container.
	Container string
	// Stdin and TTY are the container's own (spec.containers[].stdin and tty): attach can only use
	// what the container was started with. With Stdin, keystrokes reach the container's main
	// process; without it they are dropped. With a TTY, output arrives as one terminal stream and
	// the size is sent; without one, stdout and stderr both show.
	Stdin bool
	TTY   bool
}

// StartPodAttach attaches to the main process of a container through the API server's attach
// subresource, as kubectl attach does: the process's output from now on, and, when the container
// has stdin, the user's keystrokes as its input.
//
// It returns at once; the stream connects in the background, with the executor of pod shells
// (WebSocket first, SPDY as the fallback). The first line of output says what the session can do
// ("[cogpit] attached to …"). When the stream ends with an error, the error is written into the
// output as "[cogpit] attach ended: …"; Read then returns io.EOF. Closing the session detaches;
// the process goes on.
func StartPodAttach(ctx context.Context, config *rest.Config, client kubernetes.Interface, opts AttachOptions) (Session, error) {
	return StartPodAttachAfter(ctx, config, client, opts, AttachWait{})
}

// AttachWait is what an attach waits for before its stream connects: a container that has not
// started yet (an ephemeral container just added, D-102), whose attach the API server would
// refuse. Notice is written first, while Before runs; Before's error ends the session with
// "[cogpit] attach ended: …". The zero value waits for nothing.
type AttachWait struct {
	Notice string
	Before func(ctx context.Context) error
}

// StartPodAttachAfter is StartPodAttach after wait: its notice, then Before, then the line that
// says what the session is attached to, then the stream. Closing the session during Before cancels
// it.
func StartPodAttachAfter(ctx context.Context, config *rest.Config, client kubernetes.Interface, opts AttachOptions, wait AttachWait) (Session, error) {
	if opts.Namespace == "" || opts.Pod == "" {
		return nil, errors.New("attach: namespace and pod are required")
	}
	if config == nil || client == nil {
		return nil, errors.New("attach: no cluster connection")
	}
	exec, err := newExecutor(config, attachRequest(client, opts))
	if err != nil {
		return nil, fmt.Errorf("attach %s/%s: %w", opts.Namespace, opts.Pod, err)
	}
	mode := streamMode{tty: opts.TTY, stdin: opts.Stdin, what: "attach", notice: attachNotice(opts)}
	if wait.Before == nil {
		return startStreamMode(ctx, exec, mode), nil
	}
	after := mode.notice
	mode.notice = printable(strings.TrimRight(wait.Notice, "\r\n")) + "\r\n"
	return startStreamMode(ctx, beforeExecutor{exec: exec, before: wait.Before, after: after}, mode), nil
}

// attachRequest is the request kubectl attach sends: POST pods/<pod>/attach with the container,
// stdout, stdin only when the container has it, and a TTY only when the container has one (then
// stderr arrives on stdout).
func attachRequest(client kubernetes.Interface, opts AttachOptions) *rest.Request {
	return client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(opts.Namespace).
		Name(opts.Pod).
		SubResource("attach").
		VersionedParams(&corev1.PodAttachOptions{
			Container: opts.Container,
			Stdin:     opts.Stdin,
			Stdout:    true,
			Stderr:    !opts.TTY,
			TTY:       opts.TTY,
		}, scheme.ParameterCodec)
}

// attachNotice is the line an attach session starts with: what it is attached to and whether
// typing reaches the process. The container's name comes from the user's cluster; it is printed
// without control characters.
func attachNotice(opts AttachOptions) string {
	what := opts.Pod
	if opts.Container != "" {
		what += "/" + opts.Container
	}
	what = printable(what)
	switch {
	case !opts.Stdin:
		return "[cogpit] attached to " + what + ", output only: the container has no stdin, so typing here does nothing\r\n"
	case opts.TTY:
		return "[cogpit] attached to " + what + "; if no prompt shows, press Enter\r\n"
	default:
		return "[cogpit] attached to " + what + " without a terminal: lines typed here reach its stdin\r\n"
	}
}

// crlfWriter turns line feeds into carriage return and line feed, as a TTY's line discipline
// does, for output that comes without a TTY. It keeps no state, so the executor's stdout and
// stderr goroutines may share one.
type crlfWriter struct{ w io.Writer }

func (c crlfWriter) Write(p []byte) (int, error) {
	if bytes.IndexByte(p, '\n') < 0 {
		return c.w.Write(p)
	}
	if _, err := c.w.Write(bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))); err != nil {
		return 0, err
	}
	return len(p), nil
}
