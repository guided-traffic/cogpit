package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// Asking for credential plugins once per file and plugin (D-28, owner's decision): a kubeconfig
// with ten contexts that run the same `aws eks get-token` shows one dialog that lists the ten, and
// one click allows them all. Only the asking is bundled. The approvals stay one per context, each
// with its own fingerprint (the server and how it is trusted are bound per context,
// trust-boundaries H-1), and the engine verifies every fingerprint the dialog showed against the
// file as it is now before it records any of them.

// ExecApprovalContext is one context of an ExecApprovalGroup: what is specific to it. The server
// and how it is reached and trusted are what decide who receives the credential, so each context
// shows its own.
type ExecApprovalContext struct {
	// Context is the context id, KubeContext its name in the file.
	Context     string `json:"context"`
	KubeContext string `json:"kubeContext"`
	// State is ExecRequired or ExecChanged.
	State string `json:"state"`
	// Server is the API server this context's credential goes to, without user information.
	Server string `json:"server,omitempty"`
	// Fingerprint binds this context's approval (kubeconfig.approveExec takes it back).
	Fingerprint string `json:"fingerprint"`
	// TLS is how the server is reached and trusted (R-11); Files holds the facts about the CA file.
	TLS   kubeconfig.TLSView   `json:"tls"`
	Files kubeconfig.FileFacts `json:"files"`
}

// ExecApprovalGroup is the contexts of one kubeconfig file whose logins are the same plugin
// (kubeconfig.ExecPlugin.Identity): the same command, arguments, environment, API version,
// interactive mode, provideClusterInfo, program binding and token file. The dialog asks about them
// together.
type ExecApprovalGroup struct {
	// Source is the label of the file, File its path.
	Source string `json:"source"`
	File   string `json:"file"`
	// Kind is what needs the approval: kubeconfig.LoginProgram, LoginTokenFile or LoginBoth.
	Kind string `json:"kind"`
	// Plugin is the plugin the contexts share. What is specific to a context (its name, user,
	// server, TLS and CA file facts) is left empty here and listed in Contexts.
	Plugin *kubeconfig.ExecPlugin `json:"plugin"`
	// Found is where the command is found now; RiskyEnv as ExecPluginInfo.RiskyEnv.
	Found    string   `json:"found,omitempty"`
	RiskyEnv []string `json:"riskyEnv,omitempty"`
	// Contexts are the contexts of the file that use the plugin and are not allowed yet (required
	// or changed), by name. A context that is allowed already is not listed.
	Contexts []ExecApprovalContext `json:"contexts"`
}

// ExecApprovalItem is one approval the user gives: the context and the fingerprint the dialog
// showed for it.
type ExecApprovalItem struct {
	Context     string `json:"context" jsonschema:"context id of a kubeconfig source as cluster_contexts lists it"`
	Fingerprint string `json:"fingerprint" jsonschema:"the context's fingerprint as the pending approvals listing returned it"`
}

// ErrExecChangedSinceShown refuses an approval whose fingerprints are not the plugins' current
// ones: the kubeconfig changed while the user looked at the dialog. Match it with errors.Is.
var ErrExecChangedSinceShown = errors.New("the credential plugin changed since it was shown; review it again")

// ExecChangedError is ErrExecChangedSinceShown naming the contexts whose plugin, server or trust
// changed (or that left the file) since the dialog listed them. Nothing was allowed.
type ExecChangedError struct{ Contexts []string }

func (e *ExecChangedError) Error() string {
	names := make([]string, len(e.Contexts))
	for i, c := range e.Contexts {
		names[i] = kubeconfig.DisplayText(c, 200)
	}
	return "the credential plugin of " + strings.Join(names, ", ") + " changed since it was shown, or the context is gone; " +
		"nothing was allowed; review it again"
}

// Is makes errors.Is(err, ErrExecChangedSinceShown) true.
func (e *ExecChangedError) Is(target error) bool { return target == ErrExecChangedSinceShown }

// PendingExecApprovals lists the logins that wait for the user, grouped by file and plugin
// identity. With contextID it returns only the group that context belongs to (with its siblings),
// and none when the context needs nothing (it is allowed, or runs no program and reads no foreign
// token file); an id no source offers is an error. Without it, it returns every group of every
// readable source file.
func (s *Session) PendingExecApprovals(contextID string) ([]ExecApprovalGroup, error) {
	k := s.KubeconfigSources()
	if k == nil {
		return nil, errors.New("this Cogpit has no kubeconfig sources")
	}
	files, _, err := k.Files()
	if err != nil {
		return nil, err
	}
	approvals, err := k.approvals()
	if err != nil {
		return nil, err
	}
	groups := []ExecApprovalGroup{}
	known := contextID == ""
	for _, f := range files {
		if f.Err != nil {
			continue
		}
		byIdentity := map[string]int{} // identity -> index in groups
		for _, c := range f.Contexts {
			id := SourceContextID(c.Name, f.Label)
			known = known || id == contextID
			p := f.Exec[c.Name]
			if !p.NeedsApproval() {
				continue
			}
			state := approvalState(approvals, p)
			if state == ExecApproved {
				continue
			}
			ident := p.Identity()
			i, ok := byIdentity[ident]
			if !ok {
				i = len(groups)
				byIdentity[ident] = i
				groups = append(groups, ExecApprovalGroup{
					Source: f.Label, File: f.Path, Kind: p.Kind(), Plugin: sharedPlugin(p),
					Found: cluster.LookPathNow(p.Command), RiskyEnv: p.RiskyEnv(),
				})
			}
			groups[i].Contexts = append(groups[i].Contexts, ExecApprovalContext{
				Context: id, KubeContext: c.Name, State: state, Server: p.Server, Fingerprint: p.Fingerprint(), TLS: p.TLS,
				Files: kubeconfig.FileFacts{CAFileResolved: p.Files.CAFileResolved, CAFileSHA256: p.Files.CAFileSHA256, CAFileNote: p.Files.CAFileNote},
			})
		}
	}
	if !known {
		return nil, fmt.Errorf("%q is not a context of a kubeconfig source (cluster_contexts lists them)", contextID)
	}
	for _, g := range groups {
		slices.SortStableFunc(g.Contexts, func(a, b ExecApprovalContext) int { return strings.Compare(a.KubeContext, b.KubeContext) })
	}
	if contextID == "" {
		return groups, nil
	}
	for _, g := range groups {
		if slices.ContainsFunc(g.Contexts, func(c ExecApprovalContext) bool { return c.Context == contextID }) {
			return []ExecApprovalGroup{g}, nil
		}
	}
	return []ExecApprovalGroup{}, nil
}

// sharedPlugin is p without what is specific to its context.
func sharedPlugin(p *kubeconfig.ExecPlugin) *kubeconfig.ExecPlugin {
	c := *p
	c.Context, c.User, c.Server, c.TLS = "", "", "", kubeconfig.TLSView{}
	c.Files.CAFileResolved, c.Files.CAFileSHA256, c.Files.CAFileNote = "", "", ""
	return &c
}

// ApproveExecAll records the user's allowance of the credential plugins of the listed source
// contexts, each provided it is still exactly the one the user was shown (its fingerprint). All or
// none: if the fingerprint of any context is not its current one, or the context left the file,
// nothing is recorded and the error names every such context (*ExecChangedError). The approvals
// are written one per context, each with its own fingerprint. Only the GUI's command calls it.
func (s *Session) ApproveExecAll(items []ExecApprovalItem) ([]ExecPluginInfo, error) {
	k := s.KubeconfigSources()
	if k == nil {
		return nil, errors.New("this Cogpit has no kubeconfig sources")
	}
	if len(items) == 0 {
		return nil, errors.New("no credential plugin to allow")
	}
	files, _, err := k.Files()
	if err != nil {
		return nil, err
	}
	type login struct {
		source string
		p      *kubeconfig.ExecPlugin
	}
	logins := map[string]login{}
	for _, f := range files {
		for _, c := range f.Contexts {
			if p := f.Exec[c.Name]; p != nil {
				logins[SourceContextID(c.Name, f.Label)] = login{f.Label, p}
			}
		}
	}
	var changed []string
	var plugins []*kubeconfig.ExecPlugin
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.Context] {
			return nil, fmt.Errorf("the context %q is listed twice", kubeconfig.DisplayText(it.Context, 200))
		}
		seen[it.Context] = true
		l, ok := logins[it.Context]
		if !ok || l.p.Fingerprint() != it.Fingerprint {
			changed = append(changed, it.Context)
			continue
		}
		plugins = append(plugins, l.p)
	}
	if len(changed) > 0 {
		return nil, &ExecChangedError{Contexts: changed}
	}
	if err := k.ApproveAll(plugins...); err != nil {
		return nil, err
	}
	out := make([]ExecPluginInfo, 0, len(items))
	for _, it := range items {
		l := logins[it.Context]
		out = append(out, ExecPluginInfo{
			Context: it.Context, Source: l.source, State: ExecApproved, Kind: l.p.Kind(), Fingerprint: it.Fingerprint, Plugin: l.p,
			Found: cluster.LookPathNow(l.p.Command), RiskyEnv: l.p.RiskyEnv(),
		})
	}
	s.emit(Event{Type: EventContexts})
	return out, nil
}
