package engine

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/guided-traffic/cogpit/internal/terminal"
)

// ErrDemoCluster is returned for streams the synthetic demo cluster cannot serve.
var ErrDemoCluster = errors.New("not available on the demo cluster")

// logTailLines is how much history a logs tab starts with.
const logTailLines = 1000

// streamTab returns a dock tab of the wanted types with the connected backend, checking that the
// tab belongs to the connected cluster and that the cluster has a connection for streams.
func (s *Session) streamTab(tabID string, types ...DockType) (DockTab, *Backend, error) {
	tab, ok := s.UIState().DockTab(tabID)
	if !ok {
		return DockTab{}, nil, fmt.Errorf("no dock tab %q", tabID)
	}
	typeOK := false
	for _, t := range types {
		typeOK = typeOK || tab.Type == t
	}
	if !typeOK {
		return DockTab{}, nil, fmt.Errorf("dock tab %q is a %s tab", tabID, tab.Type)
	}
	b, err := s.TargetBackend(tab.Context) // the tab's cluster, shown or not (D-69)
	if err != nil {
		return DockTab{}, nil, err
	}
	if b.Conn == nil {
		return DockTab{}, nil, fmt.Errorf("%s: %w", tab.Type, ErrDemoCluster)
	}
	if tab.Type != DockTerminal && tab.Ref == nil {
		return DockTab{}, nil, fmt.Errorf("dock tab %q names no pod", tabID)
	}
	return tab, b, nil
}

// OpenLogs opens the log stream of a logs dock tab, with the tab's options (logs.go): for a pod,
// the container's last 1,000 lines, then follow; for a controlling resource, the merged logs of
// its pods. The caller closes it.
func (s *Session) OpenLogs(ctx context.Context, tabID string) (io.ReadCloser, error) {
	tab, b, err := s.streamTab(tabID, DockLogs)
	if err != nil {
		return nil, err
	}
	return s.openLogs(ctx, tab, b, b.Conn)
}

// OpenTerminal starts the session of a terminal, shell, attach or node shell dock tab. A terminal
// tab is a local shell bound to the connected context and to the namespace filter if it names
// exactly one namespace (else the context's namespace); a shell tab is a shell in the tab's
// container; an attach tab is attached to the container's main process; a node shell tab waits for
// its pod, then enters the node (nodeshell.go). The caller closes it.
func (s *Session) OpenTerminal(ctx context.Context, tabID string) (terminal.Session, error) {
	tab, b, err := s.streamTab(tabID, DockTerminal, DockShell, DockAttach, DockNodeShell)
	if err != nil {
		return nil, err
	}
	if tab.Type != DockTerminal {
		// A session in the cluster ends when the connection is interrupted (D-58, S7).
		if err := s.writeBlocked(DangerMutating, b.Context); err != nil {
			return nil, err
		}
		cctx, cancel := context.WithCancel(ctx)
		sess, err := s.openClusterSession(cctx, tab, b)
		if err != nil {
			cancel()
			return nil, err
		}
		return b.watch.trackSession(tab.ID, sess, cancel), nil
	}
	ns := b.Namespace
	if u := s.UIState(); len(u.Namespaces) == 1 && u.Context == b.Context { // the window's filter, of the tab's cluster only
		ns = u.Namespaces[0]
	}
	opts, err := s.LocalTerminalOptions(b.Context, terminal.LocalOptions{
		KubeconfigPaths: b.Conn.KubeconfigPaths(), Context: b.Conn.Context(), Namespace: ns, // the name in the file, not a source's id
	})
	if err != nil {
		return nil, err
	}
	check, first := s.checkKubectl(ctx, b.ServerVersion()) // D-3 (kubectl.go); the version the heartbeats follow (connwatch_version.go)
	s.setKubectlCheck(tab.ID, check)
	opts.Kubectl = first
	if p, err := s.Preferences(); err == nil {
		opts.EnforceKubectl = p.KubectlEnforce // A27
	}
	return terminal.StartLocal(opts)
}

// openClusterSession starts the session of a shell, attach or node shell tab: a stream through the
// API server.
func (s *Session) openClusterSession(ctx context.Context, tab DockTab, b *Backend) (terminal.Session, error) {
	switch tab.Type {
	case DockAttach:
		return s.openAttach(ctx, tab, b)
	case DockNodeShell:
		return s.openNodeShell(ctx, tab, b) // nodeshell.go
	}
	return terminal.StartPodExec(ctx, b.Conn.RESTConfig(), b.Conn.Kube(), terminal.ExecOptions{
		Namespace: tab.Ref.Namespace, Pod: tab.Ref.Name, Container: tab.Container,
	})
}
