package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The port-forward commands (gap G10; resource-parity › Network › Port Forwarding). Starting a
// forward opens a stream into a pod with the user's identity, so pod.portForward,
// service.portForward and portforward.edit are mutating: an agent's request waits for the user's
// acceptance (D-2). Opening, stopping and deleting a forward touch nothing in the cluster: they
// are UI commands.

const kindServices = "services"

// forwardVerbs are what a forward uses: the port-forward subresource, and the pods it connects to,
// which the forward's cache handle lists and watches (for a service, its pods by selector).
var forwardVerbs = []engine.Verb{
	{Verb: nwVerbCreate, Resource: kindPods, Subresource: "portforward"},
	{Verb: nwVerbList, Resource: kindPods},
	{Verb: nwVerbWatch, Resource: kindPods},
}

// serviceForwardVerbs add the Service a service forward reads from the cache.
var serviceForwardVerbs = append(slices.Clone(forwardVerbs),
	engine.Verb{Verb: nwVerbList, Resource: kindServices},
	engine.Verb{Verb: nwVerbWatch, Resource: kindServices},
)

// forwardOptions are the settings every way of starting a forward shares.
type forwardOptions struct {
	LocalPort     uint16 `json:"localPort,omitempty" jsonschema:"the local port to listen on, on 127.0.0.1 only; 0 or left out picks a free port"`
	HTTPS         bool   `json:"https,omitempty" jsonschema:"the forward's URL is https://127.0.0.1:<port> instead of http://"`
	OpenInBrowser bool   `json:"openInBrowser,omitempty" jsonschema:"the GUI opens the URL in the browser once the forward runs; an agent gets the URL in the result and nothing opens"`
}

type podForwardParams struct {
	Ref       model.ObjectRef `json:"ref" jsonschema:"the pod"`
	Container string          `json:"container,omitempty" jsonschema:"the container whose port it is, one of the pod's containers or init containers; it labels the forward"`
	Port      uint16          `json:"port" jsonschema:"the port in the pod to forward, 1 to 65535"`
	forwardOptions
}

type serviceForwardParams struct {
	Ref  model.ObjectRef `json:"ref" jsonschema:"the service"`
	Port uint16          `json:"port" jsonschema:"the service's port (spec.ports[].port), forwarded to its target port in a ready pod of the service"`
	forwardOptions
}

type forwardRefParams struct {
	Ref model.ObjectRef `json:"ref" jsonschema:"the port forward (kind portforwards)"`
}

// editForwardParams leave out what does not change: a field left out keeps the forward's setting.
type editForwardParams struct {
	Ref           model.ObjectRef `json:"ref" jsonschema:"the port forward (kind portforwards)"`
	LocalPort     *uint16         `json:"localPort,omitempty" jsonschema:"the local port to listen on, on 127.0.0.1 only; 0 picks a free port; left out keeps the forward's"`
	HTTPS         *bool           `json:"https,omitempty" jsonschema:"the URL's scheme is https; left out keeps the forward's"`
	OpenInBrowser *bool           `json:"openInBrowser,omitempty" jsonschema:"the GUI opens the URL once the forward runs again; left out keeps the forward's"`
}

func registerPortForward(g *registrar) {
	add(g, engine.Spec[podForwardParams]{
		ID: "pod.portForward", Label: "Forward", MenuPath: "Pod/Forward Port", Kinds: []string{kindPods},
		Danger: engine.DangerMutating, Verbs: forwardVerbs,
		Description: "Forwards a port of a pod to a local port on 127.0.0.1 (only loopback; every local process can " +
			"connect while it runs), as kubectl port-forward does, and keeps it up: it connects again when the " +
			"connection is lost while the pod exists, and ends when the user stops it or Cogpit leaves the cluster. " +
			"Starting the same pod and port again restarts that forward with the new settings. Returns the forward " +
			"with its URL (http(s)://127.0.0.1:<port>). The user must accept an agent's request.",
		Enabled: func(s *engine.Session, p podForwardParams) error {
			pod, err := cachedOfKind(s, p.Ref, kindPods)
			if err != nil {
				return err
			}
			if err := s.CanForward(p.Ref.Context); err != nil {
				return err
			}
			if p.Port == 0 {
				return errors.New("say which port to forward: port is required")
			}
			if p.Container != "" && !slices.Contains(engine.ContainerNames(pod, true), p.Container) {
				return fmt.Errorf("%s has no container %q", p.Ref.Name, p.Container)
			}
			switch phase := model.NestedString(pod.Object, "status", "phase"); phase {
			case "Succeeded", "Failed":
				return fmt.Errorf("pod %s has ended (%s): nothing listens on its ports", p.Ref.Name, strings.ToLower(phase))
			}
			if udpOnly(model.NestedSlice(pod.Object, "spec", "containers"), "containerPort", p.Port) {
				return fmt.Errorf("port %d of pod %s is UDP: only TCP ports can be forwarded", p.Port, p.Ref.Name)
			}
			return nil
		},
		Targets: func(s *engine.Session, p podForwardParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Effects: func(_ *engine.Session, p podForwardParams) []engine.Effect {
			return forwardEffects(p.LocalPort, fmt.Sprintf("port %d", p.Port))
		},
		Run: func(ctx context.Context, s *engine.Session, p podForwardParams) (any, error) {
			return startForward(ctx, s, engine.ForwardSpec{
				Target: p.Ref, Container: p.Container, Port: p.Port,
				LocalPort: p.LocalPort, HTTPS: p.HTTPS, OpenInBrowser: p.OpenInBrowser,
			})
		},
	})

	add(g, engine.Spec[serviceForwardParams]{
		ID: "service.portForward", Label: "Forward", MenuPath: "Service/Forward Port", Kinds: []string{kindServices},
		Danger: engine.DangerMutating, Verbs: serviceForwardVerbs,
		Description: "Forwards a service's port to a local port on 127.0.0.1 (only loopback; every local process can " +
			"connect while it runs) through a ready pod of the service's selector, at the port the service targets, " +
			"as kubectl port-forward svc/<name> does. When that pod goes, it moves to another ready pod; while there " +
			"is none, connections wait. Returns the forward with its URL. The user must accept an agent's request.",
		Enabled: func(s *engine.Session, p serviceForwardParams) error {
			svc, err := cachedOfKind(s, p.Ref, kindServices)
			if err != nil {
				return err
			}
			if err := s.CanForward(p.Ref.Context); err != nil {
				return err
			}
			if len(model.NestedStringMap(svc.Object, "spec", "selector")) == 0 {
				return fmt.Errorf("service %s has no selector: there is no pod to forward to", p.Ref.Name)
			}
			other := ""
			for _, sp := range model.NestedSlice(svc.Object, "spec", "ports") {
				if n, _ := model.NestedInt64(sp, "port"); n != int64(p.Port) || p.Port == 0 {
					continue
				}
				if proto := model.NestedString(sp, "protocol"); proto != "" && proto != "TCP" {
					other = proto // a port may be served over TCP and UDP: look on
					continue
				}
				return nil
			}
			if other != "" {
				return fmt.Errorf("port %d of service %s is %s: only TCP ports can be forwarded", p.Port, p.Ref.Name, other)
			}
			return fmt.Errorf("service %s has no port %d", p.Ref.Name, p.Port)
		},
		Targets: func(s *engine.Session, p serviceForwardParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Effects: func(_ *engine.Session, p serviceForwardParams) []engine.Effect {
			return forwardEffects(p.LocalPort, fmt.Sprintf("port %d, a ready pod of the service", p.Port))
		},
		Run: func(ctx context.Context, s *engine.Session, p serviceForwardParams) (any, error) {
			return startForward(ctx, s, engine.ForwardSpec{
				Target: p.Ref, Port: p.Port, LocalPort: p.LocalPort, HTTPS: p.HTTPS, OpenInBrowser: p.OpenInBrowser,
			})
		},
	})

	// The Port Forwarding view's row menu as captured: Open, Edit, Stop, Delete.
	add(g, engine.Spec[forwardRefParams]{
		ID: "portforward.open", Label: "Open", MenuPath: "PortForward/Open", Kinds: []string{engine.PortForwardKind},
		Order: 100, Danger: engine.DangerUI,
		Description: "Returns a port forward's URL, http(s)://127.0.0.1:<port>; the GUI opens it in the browser. For an " +
			"agent nothing opens: it gets the URL.",
		Enabled: func(s *engine.Session, p forwardRefParams) error {
			f, err := cachedForward(s, p.Ref)
			if err != nil {
				return err
			}
			if !f.Listening() {
				return fmt.Errorf("the forward is %s: Edit starts it again", strings.ToLower(f.Phase))
			}
			return nil
		},
		Run: func(_ context.Context, s *engine.Session, p forwardRefParams) (any, error) {
			f, err := s.ForwardOf(p.Ref)
			if err != nil {
				return nil, err
			}
			if !f.Listening() {
				return nil, fmt.Errorf("the forward is %s: Edit starts it again", strings.ToLower(f.Phase))
			}
			return f, nil
		},
	})

	add(g, engine.Spec[editForwardParams]{
		ID: "portforward.edit", Label: "Edit", MenuPath: "PortForward/Edit", Kinds: []string{engine.PortForwardKind},
		Order: 200, Danger: engine.DangerMutating, Verbs: forwardVerbs,
		Description: "Starts a port forward again with new settings (local port, https, open in browser); a stopped or " +
			"failed forward starts again too. Settings left out stay as they are. Returns the forward with its URL. The " +
			"user must accept an agent's request.",
		Enabled: func(s *engine.Session, p editForwardParams) error {
			if _, err := cachedForward(s, p.Ref); err != nil {
				return err
			}
			return s.CanForward(p.Ref.Context)
		},
		Prefill: func(s *engine.Session, p editForwardParams) (editForwardParams, error) {
			f, err := cachedForward(s, p.Ref)
			if err != nil {
				return p, err
			}
			if p.LocalPort == nil {
				p.LocalPort = &f.Spec.LocalPort
			}
			if p.HTTPS == nil {
				p.HTTPS = &f.Spec.HTTPS
			}
			if p.OpenInBrowser == nil {
				p.OpenInBrowser = &f.Spec.OpenInBrowser
			}
			return p, nil
		},
		Targets: func(s *engine.Session, p editForwardParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Effects: func(s *engine.Session, p editForwardParams) []engine.Effect {
			f, err := cachedForward(s, p.Ref)
			if err != nil {
				return nil
			}
			local := f.Spec.LocalPort
			if p.LocalPort != nil {
				local = *p.LocalPort
			}
			return forwardEffects(local, forwardTargetText(f.Spec))
		},
		Run: func(ctx context.Context, s *engine.Session, p editForwardParams) (any, error) {
			f, err := s.ForwardOf(p.Ref)
			if err != nil {
				return nil, err
			}
			spec := f.Spec
			if p.LocalPort != nil {
				spec.LocalPort = *p.LocalPort
			}
			if p.HTTPS != nil {
				spec.HTTPS = *p.HTTPS
			}
			if p.OpenInBrowser != nil {
				spec.OpenInBrowser = *p.OpenInBrowser
			}
			return startForward(ctx, s, spec)
		},
	})

	add(g, engine.Spec[forwardRefParams]{
		ID: "portforward.stop", Label: "Stop", MenuPath: "PortForward/Stop", Kinds: []string{engine.PortForwardKind},
		Order: 300, Danger: engine.DangerUI,
		Description: "Stops a port forward: its local port closes and its connection to the pod ends. The forward stays " +
			"in the list, stopped; portforward_edit starts it again. Nothing in the cluster changes.",
		Enabled: func(s *engine.Session, p forwardRefParams) error {
			f, err := cachedForward(s, p.Ref)
			if err != nil {
				return err
			}
			if !f.Listening() {
				return fmt.Errorf("the forward is %s already", strings.ToLower(f.Phase))
			}
			return nil
		},
		Run: func(_ context.Context, s *engine.Session, p forwardRefParams) (any, error) {
			f, err := s.StopForward(p.Ref)
			if err != nil {
				return nil, err
			}
			userToast(s, engine.LevelInfo, "Port forward of "+forwardTargetText(f.Spec)+" stopped")
			return f, nil
		},
	})

	add(g, engine.Spec[forwardRefParams]{
		ID: "portforward.remove", Label: labelDelete, MenuPath: "PortForward/Delete", Kinds: []string{engine.PortForwardKind},
		Order: engine.OrderDelete, Danger: engine.DangerUI,
		Description: "Stops a port forward and removes it from the list. Nothing in the cluster changes.",
		Enabled: func(s *engine.Session, p forwardRefParams) error {
			_, err := cachedForward(s, p.Ref)
			return err
		},
		Run: func(_ context.Context, s *engine.Session, p forwardRefParams) (any, error) {
			f, err := s.RemoveForward(p.Ref)
			if err != nil {
				return nil, err
			}
			userToast(s, engine.LevelInfo, "Port forward of "+forwardTargetText(f.Spec)+" deleted")
			return f, nil
		},
	})
}

// startForward starts a forward for a confirmed command and says so.
func startForward(ctx context.Context, s *engine.Session, spec engine.ForwardSpec) (any, error) {
	b, err := writableOn(s, spec.Target.Context) // the target's cluster, shown or not (D-69)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	f, err := s.StartForward(ctx, spec)
	if err != nil {
		return nil, err
	}
	msg := "Forwarding " + f.Address + " to " + forwardTargetText(f.Spec)
	if f.Phase == engine.ForwardWaiting {
		msg += " (waiting: " + f.Message + ")"
	}
	userToast(s, engine.LevelInfo, msg)
	return f, nil
}

// cachedForward is the Enabled check of the commands on a forward: it must be one, and exist. It
// does not wait.
func cachedForward(s *engine.Session, ref model.ObjectRef) (engine.Forward, error) {
	if ref.Kind != engine.PortForwardKind {
		return engine.Forward{}, fmt.Errorf("%w: this action applies to port forwards", engine.ErrNotApplicable)
	}
	return s.LookupForward(ref)
}

// forwardEffects are the confirmation's rows: where it listens, where it goes (the object is the
// confirmation's Objects row), whose identity it uses, who can use it.
func forwardEffects(localPort uint16, to string) []engine.Effect {
	at := "127.0.0.1, a free port"
	if localPort != 0 {
		at = "127.0.0.1:" + strconv.Itoa(int(localPort))
	}
	return []engine.Effect{
		{Name: "Listens on", Value: at, Tone: engine.EffectNames},
		{Name: "Forwards to", Value: to, Tone: engine.EffectNames},
		{Name: "Identity", Value: "yours"},
		{Name: "Reachable by", Value: "every process on this computer, while it runs", Tone: engine.EffectWarning},
	}
}

func forwardTargetText(spec engine.ForwardSpec) string {
	kind := "pod"
	if spec.Target.Kind == kindServices {
		kind = "service"
	}
	return fmt.Sprintf("%s %s port %d", kind, spec.Target.Name, spec.Port)
}

// udpOnly reports whether a pod declares port only for UDP or SCTP: the port-forward protocol
// carries TCP.
func udpOnly(containers []any, field string, port uint16) bool {
	declared, tcp := false, false
	for _, c := range containers {
		for _, p := range model.NestedSlice(c, "ports") {
			if n, _ := model.NestedInt64(p, field); n != int64(port) {
				continue
			}
			declared = true
			if proto := model.NestedString(p, "protocol"); proto == "" || proto == "TCP" {
				tcp = true
			}
		}
	}
	return declared && !tcp
}
