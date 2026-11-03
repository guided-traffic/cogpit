package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Dynamic resource allocation (Device Resources, resource.k8s.io/v1, Kubernetes 1.34 and later):
// what claims and their templates request, read the same way for both. Lens's documentation
// (read 2026-10-13, not captured) lists the views' columns; the texts, colours and drawers here
// are inferred unless a comment says otherwise.

// Kind keys and API group of the Device Resources views.
const (
	groupResource               = "resource.k8s.io"
	keyResourceClaims           = "resourceclaims"
	keyResourceClaimTemplates   = "resourceclaimtemplates"
	keyDeviceClasses            = "deviceclasses"
	keyResourceSlices           = "resourceslices"
	idDeviceClasses             = "deviceClasses"
	titleDeviceClasses          = "Device Classes"
	titleDevice                 = "Device"
	titleDriver                 = "Driver"
	titlePool                   = "Pool"
	itemRequests                = "Requests"
	titleDevices                = "Devices"
	titleClaim                  = "Claim"
	titleCapacity               = "Capacity"
	fieldDriver                 = "driver"
	draCountAll                 = "all" // a request for every matching device
	draConditionError           = "Error"
	draAllocationAll            = "All" // a request's allocationMode that asks for every matching device
	draClaimPending             = "Pending"
	draClaimAllocated           = "Allocated"
	draMaxDevicesShown          = 3 // the Devices cell lists this many devices, then "+ N more"
	draDeviceConditionReady     = "Ready"
	draDeviceConditionHealthy   = "Healthy"
	draDeviceConditionAvailable = "Available"
)

// draRequest is one device request of a claim's spec, or one alternative of a request that offers
// several (firstAvailable): its name ("gpu", or "gpu/large" for an alternative), the device class,
// how many devices it asks for, the CEL selectors and admin access.
type draRequest struct {
	name, class, count string
	selectors          []string
	adminAccess        bool
}

// draRequests reads spec.devices.requests of a claim spec (a claim's spec, a template's
// spec.spec): an exact request as one entry, a request with alternatives as one entry per
// alternative, in order.
func draRequests(spec any) []draRequest {
	var out []draRequest
	for _, r := range model.NestedSlice(spec, "devices", "requests") {
		name := model.NestedString(r, fieldName)
		if alts := model.NestedSlice(r, "firstAvailable"); len(alts) > 0 {
			for _, a := range alts {
				out = append(out, draRequestOf(name+"/"+model.NestedString(a, fieldName), a))
			}
			continue
		}
		out = append(out, draRequestOf(name, model.NestedMap(r, "exactly")))
	}
	return out
}

func draRequestOf(name string, r any) draRequest {
	req := draRequest{name: name, class: model.NestedString(r, "deviceClassName")}
	switch {
	case model.NestedString(r, "allocationMode") == draAllocationAll:
		req.count = draCountAll
	default:
		n, ok := model.NestedInt64(r, "count")
		if !ok || n < 1 {
			n = 1 // the API's default for ExactCount
		}
		req.count = itoa(n)
	}
	for _, s := range model.NestedSlice(r, "selectors") {
		if e := strings.TrimSpace(model.NestedString(s, "cel", "expression")); e != "" {
			req.selectors = append(req.selectors, singleLine(e))
		}
	}
	req.adminAccess, _ = model.NestedBool(r, "adminAccess")
	return req
}

// draClasses returns the device classes the requests ask for, without duplicates, in order.
func draClasses(reqs []draRequest) []string {
	var out []string
	for _, r := range reqs {
		out = appendUnique(out, r.class)
	}
	return out
}

// draRequestsTable is the drawer's table of a claim spec's requests.
func draRequestsTable(reqs []draRequest) *model.DetailTable {
	if len(reqs) == 0 {
		return nil
	}
	t := &model.DetailTable{Columns: []string{"Request", "Device Class", "Count", "Selectors", "Admin Access"}}
	for _, r := range reqs {
		admin := ""
		if r.adminAccess {
			admin = textTrue
		}
		t.Rows = append(t.Rows, []string{r.name, r.class, r.count, strings.Join(r.selectors, "\n"), admin})
	}
	return t
}

// resourceClaims has the columns of Lens's documentation (read 2026-10-13, not captured): Name, a
// warning icon titled Status, Namespace, State, Device Classes, Age. As documented: State reads
// "Pending", "Allocated" or "Allocated (reserved: N)" with N the claim's consumers, and the icon
// shows when a driver reports an unhealthy device, its tooltip "Device <device> (<driver>) reports
// <condition>=<status>" and the driver's message. Inferred: which conditions count as unhealthy
// (draDeviceProblem), the colours, the hidden Devices and Reserved For columns, and the drawer.
var resourceClaims = &model.KindProfile{
	Key:        keyResourceClaims,
	Group:      groupResource,
	Version:    "v1",
	Resource:   keyResourceClaims,
	Kind:       "ResourceClaim",
	Title:      "Resource Claims",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		signalsColumn,
		namespaceColumn,
		{ID: "state", Title: "State", Type: model.ColumnText, Width: 170},
		{ID: idDeviceClasses, Title: titleDeviceClasses, Type: model.ColumnText, Width: 220},
		{ID: "devices", Title: titleDevices, Type: model.ColumnText, Hidden: true},
		{ID: "reservedFor", Title: "Reserved For", Type: model.ColumnText, Hidden: true},
		ageColumn,
	},
	Cells: resourceClaimCells,
	Fields: map[string]fieldFunc{
		"state": func(obj *unstructured.Unstructured) []string {
			text, _ := claimState(obj)
			return nonEmpty(text)
		},
		"deviceClass": func(obj *unstructured.Unstructured) []string {
			return draClasses(draRequests(model.NestedMap(obj, "spec")))
		},
		fieldDriver: func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, r := range claimResults(obj) {
				out = appendUnique(out, r.driver)
			}
			return out
		},
		"device": func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, r := range claimResults(obj) {
				out = appendUnique(out, r.device)
			}
			return out
		},
		"reservedFor": func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, c := range claimConsumers(obj) {
				out = appendUnique(out, c.name)
			}
			return out
		},
	},
	Detail: resourceClaimDetail,
}

func resourceClaimCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	state, token := claimState(obj)
	warn, warnToken := draDeviceProblem(obj)
	status := token
	if warnToken != model.StatusNone {
		status = warnToken
	}
	return []model.Cell{
		nameCell(obj),
		stateCell(warnToken, warn),
		namespaceCell(obj),
		{Text: state, Status: token},
		{Text: strings.Join(draClasses(draRequests(model.NestedMap(obj, "spec"))), ", ")},
		claimDevicesCell(claimResults(obj)),
		claimConsumersCell(obj),
		model.AgeCell(created(obj)),
	}, status
}

// claimState is the State column as Lens's documentation describes it: "Pending" while the claim
// has no allocation, "Allocated", or "Allocated (reserved: N)" while N consumers (pods) hold a
// reservation. Colours inferred: pending waits, an allocation is fine.
func claimState(obj any) (string, model.Status) {
	if model.NestedMap(obj, "status", "allocation") == nil {
		return draClaimPending, model.StatusPending
	}
	if n := len(model.NestedSlice(obj, "status", "reservedFor")); n > 0 {
		return draClaimAllocated + " (reserved: " + itoa(int64(n)) + ")", model.StatusOK
	}
	return draClaimAllocated, model.StatusOK
}

// draResult is one allocated device of a claim: the request it serves and the device by driver,
// pool and name.
type draResult struct {
	request, driver, pool, device string
}

func claimResults(obj any) []draResult {
	list := model.NestedSlice(obj, "status", "allocation", "devices", "results")
	out := make([]draResult, 0, len(list))
	for _, r := range list {
		res := draResult{
			request: model.NestedString(r, "request"),
			driver:  model.NestedString(r, fieldDriver),
			pool:    model.NestedString(r, "pool"),
			device:  model.NestedString(r, "device"),
		}
		if res.device != "" {
			out = append(out, res)
		}
	}
	return out
}

// claimDevicesCell lists the allocated devices by name, the first few, then "+ N more"; it sorts by
// their number.
func claimDevicesCell(results []draResult) model.Cell {
	names := make([]string, 0, min(len(results), draMaxDevicesShown))
	for _, r := range results[:min(len(results), draMaxDevicesShown)] {
		names = append(names, r.device)
	}
	text := strings.Join(names, ", ")
	if more := len(results) - len(names); more > 0 {
		text += " + " + itoa(int64(more)) + " more"
	}
	return model.Cell{Text: text, Num: int64(len(results))}
}

// draConsumer is one entry of a claim's status.reservedFor: a pod, usually.
type draConsumer struct {
	resource, name string
	link           *model.ObjectRef
}

func claimConsumers(obj *unstructured.Unstructured) []draConsumer {
	list := model.NestedSlice(obj, "status", "reservedFor")
	out := make([]draConsumer, 0, len(list))
	for _, e := range list {
		c := draConsumer{resource: model.NestedString(e, "resource"), name: model.NestedString(e, fieldName)}
		if c.name == "" {
			continue
		}
		if c.resource == podsResource && model.NestedString(e, "apiGroup") == "" {
			c.link = linkTo(podsResource, obj.GetNamespace(), c.name) // consumers share the claim's namespace
		}
		out = append(out, c)
	}
	return out
}

// claimConsumersCell names the consumers; a single pod is a link to it.
func claimConsumersCell(obj *unstructured.Unstructured) model.Cell {
	cs := claimConsumers(obj)
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.name)
	}
	cell := model.Cell{Text: strings.Join(names, ", ")}
	if len(cs) == 1 {
		cell.Link = cs[0].link
	}
	return cell
}

// draDeviceProblem is the warning icon's reason, as Lens's documentation words it: "Device <device>
// (<driver>) reports <condition>=<status>", then the driver's message; several problems are joined
// by "; ". Which conditions count is inferred: Ready, Healthy or Available not True, or a condition
// whose type names a problem (Unhealthy, Degraded, Failed, Error) reported True.
func draDeviceProblem(obj any) (string, model.Status) {
	var problems []string
	for _, d := range model.NestedSlice(obj, "status", "devices") {
		for _, c := range conditions(d, "conditions") {
			if !draUnhealthy(c) {
				continue
			}
			p := "Device " + model.NestedString(d, "device") + " (" + model.NestedString(d, fieldDriver) + ") reports " + c.Type + "=" + c.Status
			if c.Message != "" {
				p += ": " + singleLine(c.Message)
			}
			problems = append(problems, p)
		}
	}
	if len(problems) == 0 {
		return "", model.StatusNone
	}
	return strings.Join(problems, "; "), model.StatusWarning
}

// draProblemWords mark a device condition whose True status is a problem.
var draProblemWords = []string{"Unhealthy", "Degraded", "Failed", "Failure", draConditionError}

func draUnhealthy(c condition) bool {
	switch c.Type {
	case draDeviceConditionReady, draDeviceConditionHealthy, draDeviceConditionAvailable:
		return c.Status != condTrue
	}
	if c.Status != condTrue {
		return false
	}
	for _, bad := range draProblemWords {
		if strings.Contains(c.Type, bad) {
			return true
		}
	}
	return false
}

// claimNode is the node an allocation binds the claim to: the node name of a node selector that
// names one node by its metadata.name field, as the scheduler writes it for node-local devices.
func claimNode(obj any) string {
	terms := model.NestedSlice(obj, "status", "allocation", "nodeSelector", "nodeSelectorTerms")
	if len(terms) != 1 {
		return ""
	}
	for _, f := range model.NestedSlice(terms[0], "matchFields") {
		values := model.NestedStrings(f, "values")
		if model.NestedString(f, "key") == "metadata.name" && model.NestedString(f, "operator") == "In" && len(values) == 1 {
			return values[0]
		}
	}
	return ""
}

func resourceClaimDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	state, token := claimState(obj)
	d.addStatus("State", state, token)
	if warn, wt := draDeviceProblem(obj); wt != model.StatusNone {
		d.addStatus("Warning", warn, wt)
	}
	d.put(linkedItem("Node", claimNode(obj), linkTo(keyNodes, "", claimNode(obj))))
	d.add("Allocated", timestamp(model.NestedTime(obj, "status", "allocation", "allocationTimestamp")))
	sections := withSection(nil, model.TitleProperties, d)

	if t := draRequestsTable(draRequests(model.NestedMap(obj, "spec"))); t != nil {
		sections = append(sections, model.DetailSection{Title: itemRequests, Table: t})
	}
	if results := claimResults(obj); len(results) > 0 {
		t := &model.DetailTable{Columns: []string{"Request", titleDriver, titlePool, titleDevice}}
		for _, r := range results {
			t.Rows = append(t.Rows, []string{r.request, r.driver, r.pool, r.device})
		}
		sections = append(sections, model.DetailSection{Title: "Allocation", Table: t})
	}
	if cs := claimConsumers(obj); len(cs) > 0 {
		cols := []model.Column{{Title: "Resource", Type: model.ColumnText}, {Title: titleName, Type: model.ColumnText}}
		rows := make([][]model.Cell, 0, len(cs))
		for _, c := range cs {
			rows = append(rows, []model.Cell{{Text: c.resource}, {Text: c.name, Link: c.link}})
		}
		sections = append(sections, model.DetailSection{Title: "Reserved For", Table: model.CellTable(cols, rows)})
	}
	if t := claimDeviceStatusTable(obj); t != nil {
		sections = append(sections, model.DetailSection{Title: "Device Status", Table: t})
	}
	return sections
}

// claimDeviceStatusTable lists what the drivers report about the allocated devices: each device's
// conditions and network data.
func claimDeviceStatusTable(obj any) *model.DetailTable {
	list := model.NestedSlice(obj, "status", "devices")
	if len(list) == 0 {
		return nil
	}
	t := &model.DetailTable{Columns: []string{titleDevice, titleDriver, titlePool, itemConditions, "Network"}}
	for _, d := range list {
		var conds []string
		for _, c := range conditions(d, "conditions") {
			conds = append(conds, c.Type+"="+c.Status)
		}
		var network []string
		if iface := model.NestedString(d, "networkData", "interfaceName"); iface != "" {
			network = append(network, iface)
		}
		network = append(network, model.NestedStrings(d, "networkData", "ips")...)
		if mac := model.NestedString(d, "networkData", "hardwareAddress"); mac != "" {
			network = append(network, mac)
		}
		t.Rows = append(t.Rows, []string{
			model.NestedString(d, "device"), model.NestedString(d, fieldDriver), model.NestedString(d, "pool"),
			strings.Join(conds, "\n"), strings.Join(network, "\n"),
		})
	}
	return t
}
