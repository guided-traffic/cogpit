package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// A node shell (engine/nodeshell.go): the pod shell's exec stream into the container of a
// privileged pod on the node that shares the host's PID namespace, running nsenter into PID 1's
// namespaces, so that the shell is the node's own root shell. The pod is the engine's to create
// and delete; this file only waits for it (through the caller's Before) and streams.

// NodeShellCommand is what a node shell runs in its pod: nsenter into the mount, UTS, IPC, network
// and PID namespaces of the host's PID 1, then the node's bash as a login shell, else its sh, with
// a terminal type and a home directory the pod's environment does not set.
var NodeShellCommand = []string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--",
	"env", "TERM=xterm-256color", "HOME=/root",
	"sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash -l; else exec sh -l; fi"}

// NodeShellOptions select a node shell's pod and say what it waits for.
type NodeShellOptions struct {
	ExecOptions
	// Notice is written to the terminal before anything else: which pod on which node.
	Notice string
	// Before runs before the stream connects (the engine waits for the pod to run); its error ends
	// the session with "[cogpit] node shell ended: …". Nil waits for nothing.
	Before func(ctx context.Context) error
}

// StartNodeShell opens a node shell: it writes the notice, runs Before, then streams
// NodeShellCommand (or opts.Command) into the pod's container as StartPodExec does. It returns at
// once; Close during Before cancels it.
func StartNodeShell(ctx context.Context, config *rest.Config, client kubernetes.Interface, opts NodeShellOptions) (Session, error) {
	if opts.Namespace == "" || opts.Pod == "" {
		return nil, errors.New("node shell: namespace and pod are required")
	}
	if config == nil || client == nil {
		return nil, errors.New("node shell: no cluster connection")
	}
	if len(opts.Command) == 0 {
		opts.Command = NodeShellCommand
	}
	exec, err := newExecutor(config, execRequest(client, opts.ExecOptions, opts.Command))
	if err != nil {
		return nil, fmt.Errorf("node shell %s/%s: %w", opts.Namespace, opts.Pod, err)
	}
	mode := streamMode{tty: true, stdin: true, what: "node shell", notice: opts.Notice}
	if opts.Before == nil {
		return startStreamMode(ctx, exec, mode), nil
	}
	return startStreamMode(ctx, beforeExecutor{exec: exec, before: opts.Before}, mode), nil
}

// beforeExecutor runs before, then writes after (if any) to the output, then the executor's stream.
type beforeExecutor struct {
	exec   remotecommand.Executor
	before func(ctx context.Context) error
	after  string // a line once before succeeded: what an attach that waited is attached to
}

// Stream is StreamWithContext without a deadline (remotecommand.Executor).
func (b beforeExecutor) Stream(opts remotecommand.StreamOptions) error {
	return b.StreamWithContext(context.Background(), opts)
}

// StreamWithContext runs before, then the stream; before's error is the stream's.
func (b beforeExecutor) StreamWithContext(ctx context.Context, opts remotecommand.StreamOptions) error {
	if err := b.before(ctx); err != nil {
		return err
	}
	if b.after != "" && opts.Stdout != nil {
		if _, err := io.WriteString(opts.Stdout, b.after); err != nil {
			return err
		}
	}
	return b.exec.StreamWithContext(ctx, opts)
}
