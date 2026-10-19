package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Deep links (D-47, the lead's decision for the owner to confirm; ui-parity › Appearance and
// application). The app bundle registers the URL scheme cogpit, and macOS hands Cogpit every
// cogpit:// link any web page, mail or app opens, whether Cogpit runs or not. A link is untrusted
// input: it only navigates, never writes. It is parsed strictly (ParseDeepLink), and what it asks
// for is done through the navigation commands of DeepLinkCommands, dispatched with origin
// OriginDeepLink, which Dispatch refuses for every other command and for any invocation that is
// not a UI or read command (checkDeepLinkCommand, checkDeepLinkDanger): a write is unreachable by
// construction, whatever a later change adds to OpenDeepLink. It connects only a context of the
// user's kubeconfig sources (or the demo cluster), never one whose credential plugin or token file
// the user has not allowed yet (that approval stays the user's, in the window), and never names
// a file or a program. Every link is audited with origin deeplink, and what it opened, or why it
// stopped, is a toast.
//
// The grammar:
//
//	cogpit://open?context=<context id>&view=<view key>&namespace=<namespace>&name=<object name>
//
// Every parameter is optional, but a link names something to open. context is a context id as
// cluster_contexts lists it ("<context> [<file>]") or a kubeconfig context name that one source
// file alone defines; without it the link applies to the connected cluster. view is a kind key
// (pods, deployments, <plural>.<group>) or a page key (cluster/overview). namespace is a namespace:
// with name, the object's; without, the namespace filter. name is an object of the view's kind,
// whose drawer opens.

// OriginDeepLink is the origin of the commands a cogpit:// link runs (deeplink.go): neither the
// user's own action in the window nor an agent.
const OriginDeepLink Origin = "deeplink"

// DeepLinkScheme is the URL scheme Cogpit's app registers (CFBundleURLTypes).
const DeepLinkScheme = "cogpit"

// deepLinkHost is the only action a link names: cogpit://open?...
const deepLinkHost = "open"

// Bounds of a link. Cogpit's defaults (D-47).
const (
	// MaxDeepLink is the longest link Cogpit reads, in bytes.
	MaxDeepLink = 2048
	// maxLinkContext bounds a context id.
	maxLinkContext = 512
	// maxLinkName bounds an object's name: Kubernetes' longest (a DNS subdomain).
	maxLinkName = 253
	// maxLinkView bounds a view key: a DNS subdomain, and a page's name after a slash.
	maxLinkView = 253 + 1 + 63
)

// The parameters of a link.
const (
	LinkContext   = "context"
	LinkView      = "view"
	LinkNamespace = "namespace"
	LinkName      = "name"
)

var (
	linkViewRE      = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(/[a-z0-9]([a-z0-9-]*[a-z0-9])?)?$`)
	linkNamespaceRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	linkNameRE      = regexp.MustCompile(`^[A-Za-z0-9._:@+-]+$`)
)

// Errors of a link. Match them with errors.Is.
var (
	// ErrDeepLinkInvalid: the text is not a link Cogpit opens.
	ErrDeepLinkInvalid = errors.New("not a link Cogpit opens")
	// ErrDeepLinkContext: the link names a context that is not one of the user's kubeconfig sources.
	ErrDeepLinkContext = errors.New("the link names no single context of Cogpit's kubeconfig sources")
	// ErrDeepLinkNeedsApproval: the context logs in with a program or token file the user has not
	// allowed yet, or skips the certificate check the user has not allowed yet (A28); a link never
	// asks for that approval.
	ErrDeepLinkNeedsApproval = errors.New("its login waits for your approval")
	// ErrDeepLinkRefused: a command that is not a link's to run (Dispatch with OriginDeepLink).
	ErrDeepLinkRefused = errors.New("a link only navigates: this command is not a link's to run")
)

// DeepLink is a parsed cogpit:// link. Every field is checked: ParseDeepLink returns nothing else.
type DeepLink struct {
	Context   string `json:"context,omitempty"`
	View      string `json:"view,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

// ParseDeepLink reads a cogpit:// link strictly: the scheme cogpit and the action open, no user,
// port or fragment, only the parameters context, view, namespace and name, each at most once, not
// empty, percent-decoded to valid UTF-8 without control or format characters, within its bounds
// and its form (a view key, a DNS label, an object name); a name needs a view that is a kind's.
// Anything else is refused with an error wrapping ErrDeepLinkInvalid that never quotes more of the
// link than a short, escaped value.
func ParseDeepLink(raw string) (DeepLink, error) {
	bad := func(format string, args ...any) (DeepLink, error) {
		return DeepLink{}, fmt.Errorf("%w: "+format, append([]any{ErrDeepLinkInvalid}, args...)...)
	}
	switch {
	case len(raw) > MaxDeepLink:
		return bad("it is %d bytes long, at most %d are read", len(raw), MaxDeepLink)
	case !utf8.ValidString(raw) || strings.IndexFunc(raw, notLinkRune) >= 0:
		return bad("it holds spaces, control characters or invalid UTF-8")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return bad("it does not parse as a URL")
	}
	switch {
	case !strings.EqualFold(u.Scheme, DeepLinkScheme):
		return bad("its scheme is not %s://", DeepLinkScheme)
	case u.Opaque != "":
		return bad("write it as %s://%s?…", DeepLinkScheme, deepLinkHost)
	case u.User != nil:
		return bad("it carries user information")
	case !strings.EqualFold(u.Host, deepLinkHost):
		return bad("its action is %q; Cogpit opens %s://%s?… only", kubeconfig.DisplayText(u.Host, 32), DeepLinkScheme, deepLinkHost)
	case u.Path != "" && u.Path != "/":
		return bad("it has a path; the parameters go after ?")
	case u.Fragment != "" || u.RawFragment != "" || strings.Contains(raw, "#"):
		return bad("it has a fragment (#…)")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return bad("its parameters do not parse")
	}
	var l DeepLink
	for key, vals := range q {
		if len(vals) != 1 {
			return bad("the parameter %q is given %d times", kubeconfig.DisplayText(key, 32), len(vals))
		}
		v := vals[0]
		if v == "" {
			return bad("the parameter %q is empty", kubeconfig.DisplayText(key, 32))
		}
		if !utf8.ValidString(v) || strings.IndexFunc(v, notValueRune) >= 0 {
			return bad("the parameter %q holds control characters or invalid UTF-8", kubeconfig.DisplayText(key, 32))
		}
		switch key {
		case LinkContext:
			if len(v) > maxLinkContext {
				return bad("the context is %d bytes long, at most %d", len(v), maxLinkContext)
			}
			l.Context = v
		case LinkView:
			if len(v) > maxLinkView || !linkViewRE.MatchString(v) || strings.Count(v, "/") > 1 {
				return bad("the view %q is not a view key such as pods or cluster/overview", kubeconfig.DisplayText(v, 64))
			}
			l.View = v
		case LinkNamespace:
			if !linkNamespaceRE.MatchString(v) {
				return bad("the namespace %q is not a namespace's name", kubeconfig.DisplayText(v, 64))
			}
			l.Namespace = v
		case LinkName:
			if len(v) > maxLinkName || !linkNameRE.MatchString(v) || v == "." || v == ".." {
				return bad("the name %q is not an object's name", kubeconfig.DisplayText(v, 64))
			}
			l.Name = v
		default:
			return bad("the parameter %q is not one of context, view, namespace and name", kubeconfig.DisplayText(key, 32))
		}
	}
	switch {
	case l == DeepLink{}:
		return bad("it names nothing to open")
	case l.Name != "" && l.View == "":
		return bad("a name needs the view of its kind")
	case l.Name != "" && strings.Contains(l.View, "/"):
		return bad("a name needs a kind's view, not the page %s", l.View)
	}
	return l, nil
}

// notLinkRune is what a link's text never holds before it is decoded: spaces and other
// non-printing characters.
func notLinkRune(r rune) bool { return r <= ' ' || r == 0x7f || !unicode.IsPrint(r) }

// notValueRune is what a decoded parameter never holds: control and format characters (the
// bidirectional overrides among them, which make one name look like another).
func notValueRune(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }

// DeepLinkCommands are the only commands a link runs, each a UI command that agents run freely
// too (D-2): close the settings page, connect a context, set the namespace filter, open a view,
// go to an object. TestDeepLinkCommandsOnlyNavigate holds them to that.
var DeepLinkCommands = []string{"settings.close", "cluster.connect", "ui.setNamespace", "ui.openView", "ui.navigate"}

// deepLinkVerbs are the API verbs a link's command may declare: the cache's reads.
var deepLinkVerbs = []string{"get", "list", "watch"}

// checkDeepLinkCommand refuses, for OriginDeepLink, every command that is not one of
// DeepLinkCommands, and any of those that is not a UI or read command, may escalate, is GUI-only
// or declares a verb that writes; before its parameters are decoded or anything is asked.
func checkDeepLinkCommand(c *Command, o Origin) error {
	if o != OriginDeepLink {
		return nil
	}
	if !slices.Contains(DeepLinkCommands, c.ID) || !c.navigates() {
		return fmt.Errorf("command %s: %w", c.ID, ErrDeepLinkRefused)
	}
	return nil
}

// checkDeepLinkDanger refuses, for OriginDeepLink, an invocation whose danger is more than UI: the
// second wall, after Escalate, should a link's command ever become a write.
func checkDeepLinkDanger(id string, o Origin, d Danger) error {
	if o == OriginDeepLink && d != DangerUI && d != DangerRead {
		return fmt.Errorf("command %s: %w", id, ErrDeepLinkRefused)
	}
	return nil
}

// navigates reports whether the command only reads or changes the UI: a UI or read command that
// never escalates, is not GUI-only and declares no verb but the cache's reads.
func (c *Command) navigates() bool {
	if (c.Danger != DangerUI && c.Danger != DangerRead) || c.escalate != nil || c.guiOnly != nil {
		return false
	}
	for _, v := range c.Verbs {
		if !slices.Contains(deepLinkVerbs, v.Verb) {
			return false
		}
	}
	return true
}

// DeepLinkResult is what a link opened.
type DeepLinkResult struct {
	Link DeepLink `json:"link"`
	// Context is the context the link opened (the connected one when it named none).
	Context string `json:"context"`
	// Connected says the link connected it (it was not connected before).
	Connected bool `json:"connected,omitempty"`
	// View is the view shown; Ref the object whose drawer opened, Found whether the cache holds it.
	View  string           `json:"view,omitempty"`
	Ref   *model.ObjectRef `json:"ref,omitempty"`
	Found bool             `json:"found,omitempty"`
	// Message is the toast that said what was opened.
	Message string `json:"message"`
}

// OpenDeepLink opens a cogpit:// link (D-47): parses it, resolves its context among the
// kubeconfig sources' contexts, and runs the navigation it asks for through DeepLinkCommands with
// OriginDeepLink. It connects the context unless it is connected, after the user's yes
// (askLinkConnect, A7), but never one whose login waits for the user's approval. What it opened, or why it stopped, is a toast and a line of the audit
// log (origin deeplink, command deeplink.open). The window calls it for each link macOS hands the
// app (DeepLinkQueue), off its main goroutine: connecting can take seconds.
func (s *Session) OpenDeepLink(ctx context.Context, raw string) (res DeepLinkResult, err error) {
	link, err := ParseDeepLink(raw)
	defer func() { s.deepLinkDone(raw, link, &res, err) }()
	if err != nil {
		return DeepLinkResult{}, err
	}
	res.Link = link
	u := s.UIState()
	connected := u.Connection.State.Live() // an interrupted connection is still the one a link navigates (D-58)
	ctxID := link.Context
	if ctxID == "" {
		if !connected || u.Context == "" {
			return res, errors.New("the link names no context and no cluster is connected")
		}
		ctxID = u.Context
	} else {
		info, err := s.deepLinkContext(link.Context)
		if err != nil {
			return res, err
		}
		ctxID = info.Name
	}
	res.Context = ctxID
	if u.Settings != nil { // the settings page would hide what the link opens
		if err := s.dispatchLink(ctx, "settings.close", struct{}{}, nil); err != nil {
			return res, err
		}
	}
	if !connected || u.Context != ctxID {
		if err := s.askLinkConnect(ctx, link, ctxID, u); err != nil { // A7: once, naming the context
			return res, err
		}
		var cr struct {
			State ConnectionState `json:"state"`
			Error string          `json:"error"`
		}
		if err := s.dispatchLink(ctx, "cluster.connect", map[string]string{"context": ctxID}, &cr); err != nil {
			return res, err
		}
		if cr.State != Connected {
			return res, fmt.Errorf("connecting to %s failed: %s", kubeconfig.DisplayText(ctxID, 200), cr.Error)
		}
		res.Connected = true
	}
	if link.Name != "" {
		ref := model.ObjectRef{Context: ctxID, Kind: link.View, Namespace: link.Namespace, Name: link.Name}
		var nr struct {
			View  string          `json:"view"`
			Ref   model.ObjectRef `json:"ref"`
			Found bool            `json:"found"`
		}
		if err := s.dispatchLink(ctx, "ui.navigate", map[string]any{"ref": ref}, &nr); err != nil {
			return res, err
		}
		res.View, res.Ref, res.Found = nr.View, &nr.Ref, nr.Found
		return res, nil
	}
	if link.Namespace != "" {
		if err := s.dispatchLink(ctx, "ui.setNamespace", map[string]any{"namespaces": []string{link.Namespace}}, nil); err != nil {
			return res, err
		}
	}
	if link.View != "" {
		if err := s.dispatchLink(ctx, "ui.openView", map[string]string{"view": link.View}, nil); err != nil {
			return res, err
		}
	}
	res.View = s.UIState().View
	return res, nil
}

// ErrDeepLinkDeclined: the user did not let a link connect another cluster.
var ErrDeepLinkDeclined = errors.New("the user declined to follow the link")

// DeepLinkConnectCommand is the confirmation's command id for a link that connects a cluster.
const DeepLinkConnectCommand = "deeplink.connect"

// askLinkConnect asks the user once before a link connects a cluster (A7, owner 2026-10-14
// evening; H-89): another cluster than the connected one, or one while none is connected (the
// lead's reading: every connect a link would start asks, so a page that opens links in a loop
// raises questions rather than connections). The question names the context and what the link
// opens; navigation within the connected cluster never asks. Without a window to ask in the link
// is refused.
func (s *Session) askLinkConnect(ctx context.Context, link DeepLink, ctxID string, u UIState) error {
	s.mu.Lock()
	confirmer := s.confirmer
	s.mu.Unlock()
	if confirmer == nil {
		return fmt.Errorf("the link would connect %s: %w", kubeconfig.DisplayText(ctxID, 200), ErrConfirmationUnavailable)
	}
	from := "none connected"
	if u.Connection.State.Live() && u.Context != "" {
		from = kubeconfig.DisplayText(u.Context, 200) + ", its tabs stay"
	}
	what := "the cluster's overview"
	switch {
	case link.Name != "":
		what = link.View + " → " + link.Name
		if link.Namespace != "" {
			what += " in namespace " + link.Namespace
		}
	case link.View != "":
		what = "the " + link.View + " view"
	}
	// The rows: the context to connect (the dialog's Context), what the link opens there, and the
	// cluster the user works in now (D-85: no sentence; "Requested by" names the link).
	effects := []Effect{
		{Name: "Opens", Value: kubeconfig.DisplayText(what, 300), Tone: EffectNames},
		{Name: "Shown now", Value: from},
	}
	req := ConfirmRequest{
		Command: DeepLinkConnectCommand, Label: "Follow a cogpit:// link and connect", Danger: DangerUI, Origin: OriginDeepLink,
		Context: ctxID, Effects: effects, Detail: EffectsText(effects),
	}
	ok, err := confirmer.Confirm(ctx, req)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: it would have connected %s", ErrDeepLinkDeclined, kubeconfig.DisplayText(ctxID, 200))
	}
	return nil
}

// deepLinkContext resolves a link's context among the connector's contexts: its id, or a
// kubeconfig context name one source file alone defines. It must come from a kubeconfig source,
// whose credential plugins run only once the user allowed them (or be the demo cluster), and its
// login must not wait for the user's approval: a link never asks for it.
func (s *Session) deepLinkContext(name string) (ContextInfo, error) {
	list, err := s.Contexts()
	if err != nil && len(list) == 0 {
		return ContextInfo{}, fmt.Errorf("the contexts cannot be listed: %w", err)
	}
	var found []ContextInfo
	for _, c := range list {
		if c.Name == name {
			found = []ContextInfo{c}
			break
		}
		if c.KubeContext == name {
			found = append(found, c)
		}
	}
	shown := kubeconfig.DisplayText(name, 200)
	switch {
	case len(found) == 0:
		return ContextInfo{}, fmt.Errorf("%w: %s", ErrDeepLinkContext, shown)
	case len(found) > 1:
		return ContextInfo{}, fmt.Errorf("%w: %d kubeconfig files define %s; name one as cluster_contexts lists it (\"%s [<file>]\")",
			ErrDeepLinkContext, len(found), shown, shown)
	}
	c := found[0]
	switch {
	case c.Source == "" && !c.Demo:
		// A connector without sources merges the kubeconfig files and runs their plugins unasked.
		return ContextInfo{}, fmt.Errorf("%w: %s is not read from a kubeconfig source", ErrDeepLinkContext, shown)
	case c.Exec == ExecRequired || c.Exec == ExecChanged:
		what := "program"
		switch c.ExecKind {
		case kubeconfig.LoginTokenFile:
			what = "token file"
		case kubeconfig.LoginInsecure: // A28: an unchecked certificate is asked once, never by a link
			what = "connection without the certificate check"
		}
		return ContextInfo{}, fmt.Errorf("%s: %w (a %s you have not allowed yet, or that changed); a link never asks for it: "+
			"connect from the navigator's clusters (⌘P) to review it", kubeconfig.DisplayText(c.Name, 200), ErrDeepLinkNeedsApproval, what)
	}
	return c, nil
}

// dispatchLink runs one of a link's commands with OriginDeepLink and decodes its result into out.
func (s *Session) dispatchLink(ctx context.Context, id string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	r, err := s.Dispatch(ctx, Invocation{ID: id, Params: raw, Origin: OriginDeepLink})
	if err != nil {
		var dis *DisabledError
		if errors.As(err, &dis) {
			return errors.New(dis.Reason)
		}
		return err
	}
	if out == nil {
		return nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// DeepLinkAuditCommand is the audit log's command of a link.
const DeepLinkAuditCommand = "deeplink.open"

// maxAuditLink bounds the link an audit line quotes when it does not parse; maxLinkToast the
// reason a toast gives (a view the cluster does not serve lists the kinds it does).
const (
	maxAuditLink = 256
	maxLinkToast = 400
)

// deepLinkDone writes the link's audit line and says in a toast what it opened or why it stopped.
func (s *Session) deepLinkDone(raw string, link DeepLink, res *DeepLinkResult, err error) {
	var params any = link
	if link == (DeepLink{}) {
		params = map[string]string{"link": kubeconfig.DisplayText(raw, maxAuditLink)}
	}
	p, _ := json.Marshal(params)
	s.audit(Invocation{ID: DeepLinkAuditCommand, Params: p, Origin: OriginDeepLink}, res.Context, auditDone, err)
	if err != nil {
		res.Message = "Cogpit did not follow a link: " + kubeconfig.DisplayText(err.Error(), maxLinkToast)
		s.emit(Event{Type: EventToast, Level: LevelWarning, Message: res.Message})
		return
	}
	res.Message = "Opened from a link: " + s.linkOpened(*res)
	s.emit(Event{Type: EventToast, Level: LevelInfo, Message: res.Message})
}

// linkOpened says what a link opened: "Pods › team-a/web-1 on kind-dev [config]".
func (s *Session) linkOpened(r DeepLinkResult) string {
	var b strings.Builder
	if r.View != "" {
		title := r.View
		if t, page := PageTitle(r.View); page {
			title = t + " (" + r.View + ")" // pages share titles: "Overview (workloads/overview)"
		} else if p, ok := s.ViewProfile(r.View); ok && p.Title != "" {
			title = p.Title
		}
		b.WriteString(title)
	}
	if r.Ref != nil {
		b.WriteString(" › " + r.Ref.Key())
		if !r.Found {
			b.WriteString(" (not in the cluster's cache yet; the drawer shows it once it appears)")
		}
	}
	if b.Len() > 0 {
		b.WriteString(" on ")
	}
	b.WriteString(kubeconfig.DisplayText(r.Context, 200))
	return b.String()
}
