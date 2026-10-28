package kinds

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The port forwards are a local kind (architecture §5, "Scopes, local and virtual kinds"): Cogpit
// makes the objects itself, one per forward, and the engine's forward manager writes them into
// the backend's local store (engine/portforward.go). Their shape:
//
//	apiVersion: cogpit.io/v1
//	kind: PortForward
//	metadata: {name: pod-web-0-8080, namespace: shop, uid, creationTimestamp}
//	spec:
//	  kind: Pod                # or Service: what is forwarded
//	  name: web-0              # the pod or service
//	  container: app           # the container the port belongs to, if known
//	  port: 8080               # the pod's port, or the service's
//	  localPort: 0             # the local port asked for; 0 picks a free one
//	  protocol: http           # http or https: the scheme of the forward's URL
//	  openInBrowser: true
//	status:
//	  phase: Active            # Starting, Active, Waiting, Reconnecting, Stopped, Failed
//	  message: ...             # why it waits, reconnects or failed
//	  localPort: 54321         # the port it listens on, on 127.0.0.1
//	  address: 127.0.0.1:54321
//	  url: http://127.0.0.1:54321
//	  pod: web-7c9f-x2l4q      # the pod it is connected to (a service's pod)
//	  podPort: 8080            # the port in that pod (a service's target port)

// keyPortForwards is the port forwards' kind key.
const keyPortForwards = "portforwards"

// Column ids (and fields) of the port forwards.
const (
	idForwardKind = idKind
	idLocalPort   = "localPort"
)

// What the port items of pods and services use.
const (
	protocolTCP = "TCP"
	fieldPort   = "port"
	paramRef    = "ref" // the object a command acts on (engine.ParamRef)
)

// Port forward phases, as status.phase holds them.
const (
	forwardActive       = "Active"
	forwardStarting     = "Starting"
	forwardWaiting      = "Waiting"
	forwardReconnecting = "Reconnecting"
	forwardFailed       = "Failed"
)

var portForwards = &model.KindProfile{
	Key:        keyPortForwards,
	Group:      model.LocalGroup,
	Version:    "v1",
	Resource:   keyPortForwards,
	Kind:       "PortForward",
	Title:      "Port Forwarding",
	Namespaced: true,
	Local:      true,
	// The columns as captured (resource-parity › Network › Port Forwarding).
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: idForwardKind, Title: "Kind", Type: model.ColumnText},
		{ID: "podPort", Title: "Pod Port", Type: model.ColumnNumber},
		{ID: idLocalPort, Title: "Local Port", Type: model.ColumnNumber},
		{ID: "protocol", Title: "Protocol", Type: model.ColumnText},
		statusColumn,
	},
	Cells: portForwardCells,
	Fields: map[string]fieldFunc{
		"target": func(obj *unstructured.Unstructured) []string {
			return []string{model.NestedString(obj, "spec", "name")}
		},
		idLocalPort: func(obj *unstructured.Unstructured) []string {
			return appendUnique(nil, model.NestedIntOrString(obj, "status", idLocalPort))
		},
		"pod": func(obj *unstructured.Unstructured) []string {
			return appendUnique(nil, model.NestedString(obj, "status", "pod"))
		},
	},
	Detail: portForwardDetail,
}

// portForwardCells: Name is the forwarded pod's or service's name (the object's own name also
// carries its kind and port, to be unique); Local Port the port it listens on, else the one asked
// for.
func portForwardCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	phase, token := forwardPhase(obj)
	port, _ := model.NestedInt64(obj, "spec", "port")
	return []model.Cell{
		{Text: model.NestedString(obj, "spec", "name")},
		namespaceCell(obj),
		{Text: model.NestedString(obj, "spec", idForwardKind)},
		{Text: model.NestedIntOrString(obj, "spec", "port"), Num: port},
		forwardLocalPort(obj),
		{Text: model.NestedString(obj, "spec", "protocol")},
		{Text: phase, Status: token},
	}, token
}

// forwardLocalPort is the port a forward listens on, else the one asked for, else "-": a free
// port is picked when it starts.
func forwardLocalPort(obj any) model.Cell {
	for _, f := range [][]string{{"status", idLocalPort}, {fieldSpec, idLocalPort}} {
		if n, ok := model.NestedInt64(obj, f...); ok && n > 0 {
			return model.Cell{Text: itoa(n), Num: n}
		}
	}
	return model.Cell{Text: "-"}
}

// forwardPhase is the phase and its token (inferred: Lens's status texts are not captured).
func forwardPhase(obj any) (string, model.Status) {
	phase := model.NestedString(obj, "status", "phase")
	switch phase {
	case forwardActive:
		return phase, model.StatusOK
	case forwardStarting:
		return phase, model.StatusPending
	case forwardWaiting, forwardReconnecting:
		return phase, model.StatusWarning
	case forwardFailed:
		return phase, model.StatusError
	}
	return phase, model.StatusNone
}

// containerPortItems are a container's ports in the pod drawer, an item each (the first named
// Ports), as captured: the port is a link that forwards it to a free local port and opens the
// browser, and "Forward…" beside it opens the forward dialog (pod.portForward); a forwarded port
// shows its forward instead (forwardPort). forwards are the pod's listening forwards by port
// (listeningForwards). The port-forward protocol carries TCP only: a UDP or SCTP port has no
// actions.
func containerPortItems(pod model.ObjectRef, container string, spec any, forwards map[int64]*unstructured.Unstructured) []model.DetailItem {
	var out []model.DetailItem
	for _, p := range model.NestedSlice(spec, "ports") {
		text := containerPortText(p)
		if text == "" {
			continue
		}
		it := model.DetailItem{Value: text}
		if len(out) == 0 {
			it.Name = titlePorts
		}
		n, _ := model.NestedInt64(p, "containerPort")
		if protocol(p) == protocolTCP && n > 0 && n <= 65535 {
			forwardPort(&it, "pod.portForward", map[string]any{paramRef: pod, "container": container, fieldPort: n}, forwards[n])
		}
		out = append(out, it)
	}
	return out
}

// servicePortItems are a service's ports in its drawer, an item each (the first named Ports),
// with the same actions as a container's ports (service.portForward; inferred: Lens's service
// drawer is not captured). A service without a selector has no pods to forward to.
func servicePortItems(svc any, forwards map[int64]*unstructured.Unstructured) []model.DetailItem {
	var out []model.DetailItem
	ref := model.ObjectRef{Kind: keyServices, Namespace: model.NestedString(svc, "metadata", "namespace"), Name: model.NestedString(svc, "metadata", "name")}
	selector := len(model.NestedStringMap(svc, "spec", "selector")) > 0
	for _, p := range model.NestedSlice(svc, "spec", "ports") {
		text := servicePortText(p)
		if text == "" {
			continue
		}
		it := model.DetailItem{Value: text}
		if len(out) == 0 {
			it.Name = titlePorts
		}
		n, _ := model.NestedInt64(p, fieldPort)
		if selector && protocol(p) == protocolTCP && n > 0 && n <= 65535 {
			forwardPort(&it, "service.portForward", map[string]any{paramRef: ref, fieldPort: n}, forwards[n])
		}
		out = append(out, it)
	}
	return out
}

// forwardTargetKeys finds a forward under its pod or service, "<namespace>/<Kind>/<name>"
// (ForwardsByTarget).
func forwardTargetKeys(obj *unstructured.Unstructured) []string {
	kind, name := model.NestedString(obj, fieldSpec, idForwardKind), model.NestedString(obj, fieldSpec, "name")
	if kind == "" || name == "" {
		return nil
	}
	return []string{obj.GetNamespace() + "/" + kind + "/" + name}
}

// listeningForwards are the forwards of a pod (kind "Pod") or a service ("Service") that have a
// local port, by the port they forward: at most one per port (engine.ForwardName). A stopped or
// failed forward listens on nothing and is left out.
func listeningForwards(rel model.Related, namespace, kind, name string) map[int64]*unstructured.Unstructured {
	out := map[int64]*unstructured.Unstructured{}
	for _, f := range rel.Objects(ForwardsByTarget, namespace+"/"+kind+"/"+name) {
		if local, _ := model.NestedInt64(f, "status", idLocalPort); local > 0 {
			port, _ := model.NestedInt64(f, fieldSpec, fieldPort)
			out[port] = f
		}
	}
	return out
}

// forwardPort gives a port item its actions. Without a forward (fwd nil): the port forwards at
// once, and "Forward…" asks first (forwardActions). While its forward listens: the port opens the
// forward in the browser (portforward.open), the local address follows it (Aside, coloured while
// the forward waits or reconnects), and a stop icon ends it (portforward.stop). A port has one
// forward (engine.ForwardName): starting it again only restarts it on another local port, so a
// forwarded port offers no "Forward…" (D-140).
func forwardPort(it *model.DetailItem, command string, params map[string]any, fwd *unstructured.Unstructured) {
	if fwd == nil {
		it.Actions = forwardActions(command, params)
		return
	}
	ref, err := json.Marshal(map[string]any{paramRef: model.ObjectRef{Kind: keyPortForwards, Namespace: fwd.GetNamespace(), Name: fwd.GetName()}})
	if err != nil {
		return
	}
	_, token := forwardPhase(fwd)
	it.Aside, it.AsideStatus = model.NestedString(fwd, "status", "address"), token
	it.Actions = []model.ItemAction{
		{Label: "Open the port forward in the browser", Command: "portforward.open", Params: ref, Link: true},
		{Label: "Stop forwarding", Command: "portforward.stop", Params: ref, Icon: true},
	}
}

// forwardActions are a port's two actions: the port itself, a link that forwards it to a free
// local port and opens the browser, and "Forward…", which asks first.
func forwardActions(command string, params map[string]any) []model.ItemAction {
	asked, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	params["openInBrowser"] = true
	quick, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	return []model.ItemAction{
		{Label: "Forward to a free port and open it", Command: command, Params: quick, Link: true},
		{Label: "Forward…", Command: command, Params: asked},
	}
}

// portForwardDetail: what is forwarded (a link to it), where it listens and the URL, which opens
// the forward in the browser (portforward.open), and its state.
func portForwardDetail(obj *unstructured.Unstructured) []model.DetailSection {
	ns := model.NestedString(obj, "metadata", "namespace")
	kind, name := model.NestedString(obj, "spec", idForwardKind), model.NestedString(obj, "spec", "name")
	var d detailItems
	switch kind {
	case "Pod":
		d.put(linkedItem("Pod", name, linkTo(podsResource, ns, name)))
		d.add("Container", model.NestedString(obj, "spec", "container"))
		d.add("Pod Port", model.NestedIntOrString(obj, "spec", "port"))
	case "Service":
		d.put(linkedItem("Service", name, linkTo(keyServices, ns, name)))
		d.add("Service Port", model.NestedIntOrString(obj, "spec", "port"))
		pod := model.NestedString(obj, "status", "pod")
		d.put(linkedItem("Pod", pod, linkTo(podsResource, ns, pod)))
		d.add("Pod Port", model.NestedIntOrString(obj, "status", "podPort"))
	}
	if lp := forwardLocalPort(obj); lp.Num > 0 {
		d.add("Local Port", lp.Text)
	} else {
		d.add("Local Port", "Random")
	}
	d.add("Address", model.NestedString(obj, "status", "address"))
	if url := model.NestedString(obj, "status", "url"); url != "" {
		params, _ := json.Marshal(map[string]any{paramRef: model.ObjectRef{Kind: keyPortForwards, Namespace: ns, Name: model.NestedString(obj, "metadata", "name")}})
		d.put(model.DetailItem{Name: "URL", Value: url, Actions: []model.ItemAction{
			{Label: "Open", Command: "portforward.open", Params: params, Link: true},
		}})
	}
	d.add("Protocol", model.NestedString(obj, "spec", "protocol"))
	phase, token := forwardPhase(obj)
	d.addStatus(titleStatus, phase, token)
	d.add("Message", model.NestedString(obj, "status", "message"))
	return []model.DetailSection{{Title: model.TitleProperties, Items: d}}
}
