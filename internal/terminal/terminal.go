// Package terminal runs interactive sessions for dock tabs: a local shell on Cogpit's own PTY with
// a per-terminal kubeconfig, a shell in a pod through client-go's exec stream, and a session
// attached to a container's main process through its attach stream.
//
// Layer L1. Terminal output is untrusted: it comes from the shell or the container. This package
// passes it through unchanged; the terminal widget filters escape sequences before rendering
// (internal/ui/fyneui/termview).
package terminal

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
)

// ErrNotImplemented marks a feature that is not implemented on this platform.
var ErrNotImplemented = errors.New("not implemented")

// Session is one running terminal session. Read returns the session's output, Write sends the
// user's keystrokes. Close ends the session and frees its resources (for a local shell: the PTY,
// the process and the temporary kubeconfig file).
//
// Write never blocks on a stalled program or a stream that is still connecting: input is queued
// (up to 1 MiB) and delivered in order. Read returns io.EOF once the session has ended and its
// output is drained. Close is idempotent and safe to call from any goroutine.
type Session interface {
	io.ReadWriteCloser
	// Resize tells the session the terminal's size in character cells.
	Resize(cols, rows uint16) error
	// Done is closed when the session has ended on its own (the shell exited, the stream closed),
	// and at the latest after Close.
	Done() <-chan struct{}
}

// LocalOptions configure a local shell.
type LocalOptions struct {
	// Shell is the shell to run; empty means $SHELL, else the first of /bin/zsh, /bin/bash and
	// /bin/sh. It runs as a login shell. On Windows, empty means pwsh.exe on PATH, else Windows
	// PowerShell, else %ComSpec% (localpty_windows.go).
	Shell string
	// Args are the shell's arguments after its name (the Preferences' shell arguments); none
	// by default.
	Args []string
	// Dir is the working directory; empty means the user's home directory.
	Dir string
	// KubeconfigPaths are the user's kubeconfig files in precedence order. The session's own
	// kubeconfig goes in front of them on KUBECONFIG. Empty means client-go's default loading
	// rules ($KUBECONFIG, else ~/.kube/config); tests always pass explicit paths.
	KubeconfigPaths []string
	// Context and Namespace bind kubectl in the shell to one context and namespace. An empty
	// Context means the files' current-context; an empty Namespace means the context's own.
	Context   string
	Namespace string
	// Env is added to the environment. It cannot override KUBECONFIG.
	Env []string
	// Kubectl is the kubectl the shell should find first, an absolute path: a link named kubectl
	// in the session's own directory, which leads PATH (kubectl.go); on Windows the folder of a
	// kubectl.exe leads PATH itself. Empty: PATH as it is.
	Kubectl string
	// EnforceKubectl also defines a shell function kubectl (zsh, bash, fish) that runs Kubectl,
	// so that a folder put first later or an alias does not win (A27, kubectl.go). PowerShell and
	// cmd get none.
	EnforceKubectl bool
}

// StartLocal starts a local shell on a new PTY with KUBECONFIG=<own file>:<the user's files>,
// where the own file holds only the context and namespace (docs/security/credentials.md). The
// file is owner-only and removed when the session closes or the shell exits. On Windows the
// shell runs on a pseudo console (ConPTY) and in a job that ends with the session, and the list
// separator is ";" (localpty_windows.go).
//
// Resolving the context reads the kubeconfig files but runs no credential plugin and writes
// nothing to them.
func StartLocal(opts LocalOptions) (Session, error) {
	return startLocal(opts)
}

// recoverTo logs a panic in a session goroutine instead of letting it end the process, and
// reports it to onPanic (which may be nil). Deferred directly by every goroutine this package
// starts.
func recoverTo(what string, onPanic func(error)) {
	r := recover()
	if r == nil {
		return
	}
	err := fmt.Errorf("terminal: %s: internal error: %v", what, r)
	slog.Error("terminal: goroutine panicked", "goroutine", what, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
	if onPanic != nil {
		onPanic(err)
	}
}
