package kinds

import (
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Pod status texts that the token rule and the summary refer to more than once.
const (
	podRunning     = "Running"
	podCompleted   = "Completed"
	podSucceeded   = "Succeeded"
	podPending     = "Pending"
	podFailed      = "Failed"
	podTerminating = "Terminating"
	podNotReady    = "NotReady"
	podInitPrefix  = "Init:"
)

// podsResource is the pods kind key and plural resource; podsTitle the view's title.
const (
	podsResource = "pods"
	podsTitle    = "Pods"
)

var pods = &model.KindProfile{
	Key:        podsResource,
	Version:    "v1",
	Resource:   podsResource,
	Kind:       "Pod",
	Title:      podsTitle,
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		warningColumn,
		namespaceColumn,
		cpuColumn,
		memoryColumn,
		{ID: fieldContainers, Title: "Containers", Type: model.ColumnNumber},
		{ID: "restarts", Title: "Restarts", Type: model.ColumnNumber},
		{ID: "controlledBy", Title: "Controlled By", Type: model.ColumnText},
		{ID: "node", Title: "Node", Type: model.ColumnText},
		{ID: "qos", Title: "QoS", Type: model.ColumnText},
		// Status before Age, as the running Lens Desktop shows them (capture, 2026-10-11); its
		// documentation lists them the other way round.
		statusColumn,
		ageColumn,
	},
	Cells: podCells,
	// Controlled By links an owner that is a custom resource through the definitions, which the
	// custom kinds already watch (a real cluster's pods of a Logging or a Challenge, 2026-10-16).
	Needs:      []model.RelatedIndex{CustomKindsByGroupKind},
	CellsWith:  podCellsWith,
	Usage:      model.UsagePods,
	UsageCells: podUsageCells,
	Fields: map[string]fieldFunc{
		"podIP": func(obj *unstructured.Unstructured) []string {
			return podIPs(obj, "podIP", "podIPs")
		},
		"hostIP": func(obj *unstructured.Unstructured) []string {
			return podIPs(obj, "hostIP", "hostIPs")
		},
		"node": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "nodeName"))
		},
		fieldImage: func(obj *unstructured.Unstructured) []string {
			return podSpecImages(model.NestedMap(obj, "spec"))
		},
		"phase": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "status", "phase"))
		},
		idStatus: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(summarisePod(obj).text)
		},
		"controlledBy": func(obj *unstructured.Unstructured) []string {
			kind, name := controller(obj)
			if kind == "" && name == "" {
				return nil
			}
			return []string{kind + "/" + name}
		},
		"qos": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "status", "qosClass"))
		},
	},
	Detail:      podDetail,
	DetailWith:  podDetailWith,                      // the claims' state (A105, resourceclaims_pods.go)
	DetailNeeds: []model.RelatedIndex{ClaimsByName}, // only the open drawer reads them
}

func podCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return podCellsWith(obj, model.NoRelated)
}

func podCellsWith(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
	s := summarisePod(obj)
	ns := model.NestedString(obj, "metadata", "namespace")
	ownerVersion, ownerKind, ownerName := controllerRef(obj)
	node := model.NestedString(obj, "spec", "nodeName")
	return []model.Cell{
		nameCell(obj),
		podWarning(obj, s),
		namespaceCell(obj),
		{}, // CPU, filled from the Metrics API (podUsageCells)
		{}, // Memory
		containersCell(obj, s),
		{Num: s.restarts},
		// Lens shows the owner's kind, a link to the owner (capture, 2026-10-11). An owner that is a
		// custom resource links through its definition (RefToWith), plain without one.
		{Text: ownerKind, Link: RefToWith(rel, ownerVersion, ownerKind, ns, ownerName)},
		{Text: node, Link: linkTo(keyNodes, "", node)},
		{Text: model.NestedString(obj, "status", "qosClass")},
		{Text: s.text, Status: s.status},
		model.AgeCell(created(obj)),
	}, s.status
}

// containersCell is a pod's Containers cell: "ready/total" to sort, search and export by, drawn as
// one square per container (containerMarks).
func containersCell(obj any, s podSummary) model.Cell {
	c := ratio(s.ready, s.total)
	c.Marks = containerMarks(obj)
	return c
}

// containerMarks are the squares of a pod's Containers cell, one per container in the order the
// spec lists them, init containers first, as Lens draws them (owner, 2026-10-12): a running
// container in the Running colour, or orange while it is not ready (the owner's F23 of
// 2026-10-17), a stopped one grey (a finished init container too), one that waits in the pending
// colour, or red when it waits on an error (CrashLoopBackOff, ImagePullBackOff). A container
// without a status yet waits. The ephemeral containers follow (ephemeralMarks). Each mark names
// its container and state for the tooltip.
func containerMarks(obj any) *[]model.CellMark {
	var out []model.CellMark
	for _, group := range [][2]string{{fieldInitContainers, "initContainerStatuses"}, {"containers", "containerStatuses"}} {
		specs := model.NestedSlice(obj, "spec", group[0])
		statuses := model.NestedSlice(obj, "status", group[1])
		if len(specs) == 0 {
			specs = statuses // no spec (a hand-made object): the statuses in their order
		}
		for _, spec := range specs {
			name := model.NestedString(spec, "name")
			// A plain init container is never ready while it runs (Kubernetes marks it ready when it
			// has finished): readiness is asked of the pod's containers and of sidecars only, init
			// containers that keep running (restartPolicy Always).
			readiness := group[0] != fieldInitContainers || model.NestedString(spec, "restartPolicy") == "Always"
			out = append(out, containerMark(name, byName(statuses, name), readiness))
		}
	}
	out = append(out, ephemeralMarks(obj)...)
	if len(out) == 0 {
		return nil
	}
	return &out
}

// ephemeralMarks are the squares of a pod's ephemeral (debug) containers, after the others (the
// owner, 2026-10-19; D-102): blue (model.StatusEphemeral) while one runs or starts, a blue outline
// once it has finished, whatever its exit code (an interactive shell's exit code says how the
// user's last command ended, not that the pod has a problem), and red while it waits on an error
// (its image cannot be pulled: it will not start as it is). An ephemeral container is never
// restarted and stays in the pod's status as long as the pod exists, so a pod debugged often
// collects finished ones: those still alive come first, so that the finished ones are what the
// cell's count hides when it has no room for all (fitMarks). Each names itself ephemeral, its
// state and its target (targetContainerName) for the tooltip: "debugger: ephemeral, running,
// target app". They count in neither the ratio nor the restarts, as in kubectl's READY.
func ephemeralMarks(obj any) []model.CellMark {
	specs := model.NestedSlice(obj, "spec", fieldEphemeralContainers)
	statuses := model.NestedSlice(obj, "status", "ephemeralContainerStatuses")
	if len(specs) == 0 {
		specs = statuses // no spec (a hand-made object): the statuses in their order
	}
	var live, done []model.CellMark
	for _, spec := range specs {
		name := model.NestedString(spec, "name")
		m := containerMark(name, byName(statuses, name), false)
		m.Text = name + ": ephemeral, " + strings.TrimPrefix(m.Text, name+": ")
		if target := model.NestedString(spec, fieldTargetContainer); target != "" {
			m.Text += ", target " + target
		}
		switch m.Status {
		case model.StatusStopped:
			m.Status, m.Hollow = model.StatusEphemeral, true
			done = append(done, m)
			continue
		case model.StatusError: // waits on an error: red, as any container that will not start
		default:
			m.Status = model.StatusEphemeral
		}
		live = append(live, m)
	}
	return append(live, done...)
}

// containerMark is one container's square: its colour and what the tooltip says about it. A
// running container whose readiness counts (readiness) is a warning while it is not ready.
func containerMark(name string, status any, readiness bool) model.CellMark {
	switch {
	case status == nil:
		return model.CellMark{Text: name + ": waiting to start", Status: model.StatusPending}
	case model.NestedMap(status, "state", "running") != nil:
		if ready, _ := model.NestedBool(status, "ready"); !ready && readiness {
			return model.CellMark{Text: name + ": running, not ready", Status: model.StatusWarning}
		}
		return model.CellMark{Text: name + ": running", Status: model.StatusOK}
	}
	if term := model.NestedMap(status, "state", "terminated"); term != nil {
		reason := model.NestedString(term, "reason")
		code, _ := model.NestedInt64(term, "exitCode")
		text := name + ": terminated"
		switch {
		case reason != "":
			text += " (" + reason + ", exit code " + itoa(code) + ")"
		default:
			text += " (exit code " + itoa(code) + ")"
		}
		return model.CellMark{Text: text, Status: model.StatusStopped}
	}
	reason := model.NestedString(status, "state", "waiting", "reason")
	st := model.StatusPending
	if isErrorReason(reason) {
		st = model.StatusError
	}
	if reason == "" {
		return model.CellMark{Text: name + ": waiting", Status: st}
	}
	return model.CellMark{Text: name + ": waiting (" + reason + ")", Status: st}
}

// keyNodes is the kind key node cells link to.
const keyNodes = "nodes"

// podWarning is the pod's cell of the warning column. Lens shows an orange triangle on a pod with
// problems (captured on a pod with many restarts and a container not ready, 2026-10-11); the rule
// is inferred. Cogpit's rule: a pod that is neither finished nor being deleted warns when its
// status is an error (CrashLoopBackOff, ImagePullBackOff, Evicted, OOMKilled, ...), when it runs
// with containers that are not ready, or when the scheduler cannot place it. The reason names
// each problem and, with one, the restart count. Restarts alone do not warn.
func podWarning(obj any, s podSummary) model.Cell {
	if s.status == model.StatusSucceeded || s.status == model.StatusTerminating || deleting(obj) {
		return warningCell(model.StatusNone, "")
	}
	var reasons []string
	if s.status == model.StatusError {
		reasons = append(reasons, s.text)
	}
	for _, c := range model.NestedSlice(obj, "status", "conditions") {
		if model.NestedString(c, "type") == "PodScheduled" && model.NestedString(c, "status") == condFalse {
			reason := model.NestedString(c, "reason")
			if reason == "" {
				reason = "Not scheduled"
			}
			reasons = append(reasons, reason)
		}
	}
	if model.NestedString(obj, "status", "phase") == podRunning && s.ready < s.total {
		var names []string
		for _, cs := range model.NestedSlice(obj, "status", "containerStatuses") {
			if ready, _ := model.NestedBool(cs, "ready"); !ready {
				names = appendUnique(names, model.NestedString(cs, "name"))
			}
		}
		if len(names) > 0 {
			reasons = append(reasons, "Containers not ready: "+strings.Join(names, ", "))
		} else {
			reasons = append(reasons, itoa(s.total-s.ready)+" of "+itoa(s.total)+" containers not ready")
		}
	}
	if len(reasons) == 0 {
		return warningCell(model.StatusNone, "")
	}
	switch s.restarts {
	case 0:
	case 1:
		reasons = append(reasons, "1 restart")
	default:
		reasons = append(reasons, itoa(s.restarts)+" restarts")
	}
	return warningCell(model.StatusWarning, strings.Join(reasons, "; "))
}

// podSummary is what the pod table derives from containers and their statuses.
type podSummary struct {
	text     string // the Status column: Running, CrashLoopBackOff, Init:1/2, Terminating
	status   model.Status
	ready    int64 // ready app and sidecar containers
	total    int64 // app and sidecar containers
	restarts int64
}

// summarisePod derives the Status column the way kubectl users read it. In order of precedence:
//
//  1. A deletion timestamp makes the pod Terminating (Unknown if its node was lost).
//  2. A pod-level reason (Evicted, NodeAffinity, Shutdown) explains the pod as a whole, so it
//     beats whatever its containers were doing when it stopped.
//  3. While init containers run, the first one that has not finished decides: Init:N/M while it
//     works, Init:<reason> or Init:ExitCode:N when it fails.
//  4. Otherwise the first app container that waits or has terminated decides with its reason
//     (CrashLoopBackOff, ImagePullBackOff, OOMKilled, Completed). A pod whose finished
//     containers sit next to running ones is Running when the pod is Ready, NotReady otherwise.
//  5. Otherwise the phase.
//
// Sidecars (init containers with restartPolicy Always) run alongside the app containers, so they
// count towards ready, total and restarts once started, as app containers do. Restarts count the
// containers that matter now: init containers while initialising, app containers afterwards.
func summarisePod(obj any) podSummary {
	var s podSummary
	specContainers := model.NestedSlice(obj, "spec", "containers")
	specInit := model.NestedSlice(obj, "spec", fieldInitContainers)
	statuses := model.NestedSlice(obj, "status", "containerStatuses")
	initStatuses := model.NestedSlice(obj, "status", "initContainerStatuses")
	phase := model.NestedString(obj, "status", "phase")
	podReason := model.NestedString(obj, "status", "reason")

	appCount := len(specContainers)
	if appCount == 0 {
		appCount = len(statuses)
	}
	for _, c := range specInit {
		if isSidecar(c) {
			s.total++
		}
	}
	s.total += int64(appCount)

	var appRestarts, initRestarts, sidecarRestarts int64
	for _, cs := range statuses {
		if ready, _ := model.NestedBool(cs, "ready"); ready {
			s.ready++
		}
		appRestarts += nonNegative(cs, "restartCount")
	}

	initCount := len(specInit)
	if initCount == 0 {
		initCount = len(initStatuses)
	}
	initText := ""
	for i, cs := range initStatuses {
		sidecar := isSidecar(byName(specInit, model.NestedString(cs, "name")))
		if sidecar {
			sidecarRestarts += nonNegative(cs, "restartCount")
			if ready, _ := model.NestedBool(cs, "ready"); ready {
				s.ready++
			}
		} else {
			initRestarts += nonNegative(cs, "restartCount")
		}
		if initText != "" || initContainerDone(cs, sidecar) {
			continue
		}
		initText = initContainerText(cs, i, initCount)
	}
	// The Initialized condition outranks the init container statuses: after a node restart the
	// kubelet may report them afresh although the pod is long past initialisation.
	initialising := initText != "" && conditionStatus(obj, "Initialized", "status", "conditions") != condTrue
	if initialising {
		s.restarts = initRestarts + sidecarRestarts
	} else {
		s.restarts = appRestarts + sidecarRestarts
	}

	text := phase
	switch {
	case podReason != "":
		text = podReason
	case initialising:
		text = initText
	default:
		containerText, hasRunning := appContainersText(statuses)
		if containerText != "" {
			text = containerText
		}
		if text == podCompleted && hasRunning {
			text = podNotReady
			if conditionStatus(obj, "Ready", "status", "conditions") == condTrue {
				text = podRunning
			}
		}
	}
	if text == "" {
		text = textUnknown
	}
	if deleting(obj) {
		text = podTerminating
		if podReason == "NodeLost" {
			text = textUnknown
		}
	}
	s.text = text
	s.status = podToken(text, phase, s.ready, s.total)
	return s
}

// isSidecar reports whether an init container spec is a sidecar: restartPolicy Always.
func isSidecar(spec any) bool {
	return model.NestedString(spec, "restartPolicy") == "Always"
}

// initContainerDone reports whether an init container no longer holds up initialisation: it
// exited with code 0, or it is a sidecar that has started.
func initContainerDone(status any, sidecar bool) bool {
	if term := model.NestedMap(status, "state", "terminated"); term != nil {
		code, _ := model.NestedInt64(term, "exitCode")
		return code == 0
	}
	if sidecar {
		started, _ := model.NestedBool(status, "started")
		return started
	}
	return false
}

// initContainerText describes the init container that blocks initialisation; index is its
// position, which is also the number of init containers done before it.
func initContainerText(status any, index, count int) string {
	if term := model.NestedMap(status, "state", "terminated"); term != nil {
		return terminatedText(term, podInitPrefix)
	}
	if reason := model.NestedString(status, "state", "waiting", "reason"); reason != "" && reason != "PodInitializing" {
		return podInitPrefix + reason
	}
	return podInitPrefix + strconv.Itoa(index) + "/" + strconv.Itoa(count)
}

// appContainersText returns the reason of the first app container that waits or has
// terminated, and whether any container runs and is ready.
func appContainersText(statuses []any) (text string, hasRunning bool) {
	for _, cs := range statuses {
		if waiting := model.NestedMap(cs, "state", "waiting"); waiting != nil {
			if reason := model.NestedString(waiting, "reason"); reason != "" && text == "" {
				text = reason
			}
			continue
		}
		if term := model.NestedMap(cs, "state", "terminated"); term != nil {
			if text == "" {
				text = terminatedText(term, "")
			}
			continue
		}
		if model.NestedMap(cs, "state", "running") != nil {
			if ready, _ := model.NestedBool(cs, "ready"); ready {
				hasRunning = true
			}
		}
	}
	return text, hasRunning
}

// terminatedText names a terminated state: its reason, else the signal, else the exit code.
func terminatedText(term map[string]any, prefix string) string {
	if reason := model.NestedString(term, "reason"); reason != "" {
		return prefix + reason
	}
	if signal, _ := model.NestedInt64(term, "signal"); signal != 0 {
		return prefix + "Signal:" + itoa(signal)
	}
	code, _ := model.NestedInt64(term, "exitCode")
	return prefix + "ExitCode:" + itoa(code)
}

// podToken colours a pod status text. Texts it does not know fall back to the phase, so a reason
// a newer kubelet invents still gets a sensible colour.
func podToken(text, phase string, ready, total int64) model.Status {
	switch text {
	case podRunning:
		if ready >= total {
			return model.StatusOK
		}
		return model.StatusWarning
	case podCompleted, podSucceeded:
		return model.StatusSucceeded
	case podPending, "ContainerCreating", "PodInitializing":
		return model.StatusPending
	case podTerminating:
		return model.StatusTerminating
	case textUnknown, "NodeLost":
		return model.StatusUnknown
	case podNotReady:
		return model.StatusWarning
	}
	if rest, ok := strings.CutPrefix(text, podInitPrefix); ok {
		if isInitProgress(rest) {
			return model.StatusPending
		}
		return model.StatusError
	}
	if isErrorReason(text) || strings.HasPrefix(text, "Signal:") || strings.HasPrefix(text, "ExitCode:") {
		return model.StatusError
	}
	switch phase {
	case podFailed:
		return model.StatusError
	case podPending:
		return model.StatusPending
	case podRunning:
		return model.StatusWarning
	case podSucceeded:
		return model.StatusSucceeded
	}
	return model.StatusUnknown
}

// isErrorReason reports the container and pod reasons that mean the pod cannot run as it is.
func isErrorReason(reason string) bool {
	switch reason {
	case "CrashLoopBackOff", "Error", podFailed, "OOMKilled", "Evicted",
		"ImagePullBackOff", "ErrImagePull", "ErrImageNeverPull", "InvalidImageName",
		"ImageInspectError", "RegistryUnavailable", "SignatureValidationFailed",
		"CreateContainerConfigError", "CreateContainerError", "RunContainerError",
		"PreStartHookError", "PostStartHookError", "ContainerCannotRun",
		"ContainerStatusUnknown", "DeadlineExceeded", "UnexpectedAdmissionError":
		return true
	}
	return false
}

// isInitProgress reports whether s is "N/M": init containers progressing normally.
func isInitProgress(s string) bool {
	done, total, ok := strings.Cut(s, "/")
	return ok && isDigits(done) && isDigits(total)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// controller returns the kind and name of the object's controlling owner, or of its first owner
// when none is marked as the controller.
func controller(obj any) (kind, name string) {
	_, kind, name = controllerRef(obj)
	return kind, name
}

// controllerRef returns the API version, kind and name of the object's controlling owner, or of
// its first owner when none is marked as the controller.
func controllerRef(obj any) (apiVersion, kind, name string) {
	owners := model.NestedSlice(obj, "metadata", "ownerReferences")
	var first any
	for _, o := range owners {
		if _, ok := o.(map[string]any); !ok {
			continue
		}
		if first == nil {
			first = o
		}
		if isController, _ := model.NestedBool(o, "controller"); isController {
			first = o
			break
		}
	}
	if first == nil {
		return "", "", ""
	}
	return model.NestedString(first, "apiVersion"), model.NestedString(first, "kind"), model.NestedString(first, "name")
}

// podIPs returns a pod's addresses: the plural list (dual-stack) and the singular field, which
// repeats its first entry.
func podIPs(obj any, single, plural string) []string {
	var out []string
	for _, e := range model.NestedSlice(obj, "status", plural) {
		out = appendUnique(out, model.NestedString(e, "ip"))
	}
	return appendUnique(out, model.NestedString(obj, "status", single))
}

// podDetail is the pod's drawer as Lens shows it (capture, 2026-10-11): Properties (after the
// generic metadata: Status, Node, Pod IP, Pod IPs, Service Account, QoS Class, Conditions,
// Tolerations, Secrets, Pod Anti Affinities), Pod Volumes, then the init containers and the
// containers. Lens's per-container metrics need a metrics provider (M7) and are not built.
func podDetail(obj *unstructured.Unstructured) []model.DetailSection {
	return podDetailRel(obj, model.NoRelated)
}

// podDetailRel is podDetail with rel for the claims' state (A105; model.NoRelated: none).
func podDetailRel(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	s := summarisePod(obj)
	ns := model.NestedString(obj, "metadata", "namespace")
	var props detailItems
	props.addStatus(titleStatus, s.text, s.status)
	node := model.NestedString(obj, "spec", "nodeName")
	props.put(linkedItem("Node", node, linkTo(keyNodes, "", node)))
	props.add("Pod IP", model.NestedString(obj, "status", "podIP"))
	ips := podIPs(obj, "podIP", "podIPs")
	ipChips := make([]model.Chip, 0, len(ips))
	for _, ip := range ips {
		ipChips = append(ipChips, model.Chip{Text: ip})
	}
	props.put(model.ChipsItem("Pod IPs", ipChips))
	account := model.NestedString(obj, "spec", "serviceAccountName")
	if account == "" {
		account = model.NestedString(obj, "spec", "serviceAccount") // deprecated alias
	}
	props.put(linkedItem("Service Account", account, linkTo("serviceaccounts", ns, account)))
	props.add("QoS Class", model.NestedString(obj, "status", "qosClass"))

	// Conditions as chips in their colours; the text keeps each condition's reason and message.
	conds := conditions(obj, "status", "conditions")
	condChips := make([]model.Chip, 0, len(conds))
	condLines := make([]string, 0, len(conds))
	for _, c := range conds {
		condChips = append(condChips, model.Chip{Text: c.Type, Status: podConditionToken(c)})
		condLines = append(condLines, c.Type+": "+c.text())
	}
	if len(condChips) > 0 {
		props.put(model.DetailItem{Name: itemConditions, Value: strings.Join(condLines, "\n"), Chips: condChips})
	}

	var tolerations []string
	for _, t := range model.NestedSlice(obj, "spec", "tolerations") {
		if text := tolerationText(t); text != "" {
			tolerations = append(tolerations, text)
		}
	}
	props.put(model.DetailItem{Name: "Tolerations", Value: strings.Join(tolerations, "\n"), Fold: "Tolerations"})

	secrets := podSecrets(obj)
	secretChips := make([]model.Chip, 0, len(secrets))
	for _, name := range secrets {
		secretChips = append(secretChips, model.Chip{Text: name, Link: linkTo("secrets", ns, name)})
	}
	props.put(model.ChipsItem("Secrets", secretChips))

	anti := len(model.NestedSlice(obj, "spec", "affinity", "podAntiAffinity", "requiredDuringSchedulingIgnoredDuringExecution")) +
		len(model.NestedSlice(obj, "spec", "affinity", "podAntiAffinity", "preferredDuringSchedulingIgnoredDuringExecution"))
	if anti > 0 {
		props.add("Pod Anti Affinities", itoa(int64(anti)))
	}
	sections := []model.DetailSection{{Title: model.TitleProperties, Items: props}}

	if volumes := podVolumes(obj, ns); len(volumes) > 0 {
		sections = append(sections, model.DetailSection{Title: "Pod Volumes", Items: volumes})
	}
	if claims, ok := podResourceClaimsSection(obj, ns, rel); ok { // resourceclaims_pods.go
		sections = append(sections, claims)
	}

	// Readiness applies to app containers and sidecars only; init and ephemeral containers have
	// no probes, so a running one is fine without being ready. Lens shows the init containers
	// before the containers (capture, 2026-10-11); ephemeral containers are Cogpit's addition,
	// each with its Target (containerSection).
	groups := []struct {
		title, spec, status string
		probed              bool
	}{
		{model.GroupInitContainers, fieldInitContainers, "initContainerStatuses", false},
		{model.GroupContainers, fieldContainers, "containerStatuses", true},
		{model.GroupEphemeralContainers, fieldEphemeralContainers, "ephemeralContainerStatuses", false},
	}
	podRef := model.ObjectRef{Kind: podsResource, Namespace: ns, Name: obj.GetName()}
	for _, g := range groups {
		statuses := model.NestedSlice(obj, "status", g.status)
		for _, c := range model.NestedSlice(obj, "spec", g.spec) {
			name := model.NestedString(c, "name")
			probed := g.probed || isSidecar(c)
			sec := containerSection(podRef, name, c, byName(statuses, name), probed)
			sec.Group = g.title
			sections = append(sections, sec)
		}
	}
	return sections
}

// tolerationText renders a toleration as "key=value:Effect", "key:Effect" for Exists, "*" for a
// toleration of every taint, with " for 300s" when it is bounded.
func tolerationText(t any) string {
	key, op := model.NestedString(t, "key"), model.NestedString(t, "operator")
	text := key
	switch {
	case key == "" && op == opExists:
		text = "*"
	case op != opExists:
		if v := model.NestedString(t, "value"); v != "" {
			text += "=" + v
		}
	}
	if e := model.NestedString(t, "effect"); e != "" {
		text += ":" + e
	}
	if text == "" {
		return ""
	}
	if secs, ok := model.NestedInt64(t, "tolerationSeconds"); ok {
		text += " for " + itoa(secs) + "s"
	}
	return text
}

// podSecrets returns the names of the Secrets a pod refers to, in order of appearance: volumes
// (also inside projected volumes), image pull secrets, and the environment of every container.
// Which references Lens counts is inferred.
func podSecrets(obj any) []string {
	var out []string
	for _, v := range model.NestedSlice(obj, "spec", "volumes") {
		out = appendUnique(out, model.NestedString(v, "secret", "secretName"))
		for _, src := range model.NestedSlice(v, "projected", "sources") {
			out = appendUnique(out, model.NestedString(src, "secret", "name"))
		}
	}
	for _, s := range model.NestedSlice(obj, "spec", "imagePullSecrets") {
		out = appendUnique(out, model.NestedString(s, "name"))
	}
	for _, group := range []string{fieldInitContainers, fieldContainers, "ephemeralContainers"} {
		for _, c := range model.NestedSlice(obj, "spec", group) {
			for _, e := range model.NestedSlice(c, "envFrom") {
				out = appendUnique(out, model.NestedString(e, "secretRef", "name"))
			}
			for _, e := range model.NestedSlice(c, "env") {
				out = appendUnique(out, model.NestedString(e, "valueFrom", "secretKeyRef", "name"))
			}
		}
	}
	return out
}

// volumeTypes names the volume sources as the drawer groups them; a source not listed here is
// shown under its field name.
var volumeTypes = []struct{ field, title, linkKind, nameField string }{
	{"configMap", "ConfigMap", configMaps.Key, fieldName},
	{"secret", "Secret", "secrets", "secretName"},
	{"persistentVolumeClaim", "PersistentVolumeClaim", persistentVolumeClaims.Key, "claimName"},
	{"projected", "Projected", "", ""},
	{"emptyDir", "EmptyDir", "", ""},
	{"hostPath", "HostPath", "", ""},
	{"downwardAPI", "DownwardAPI", "", ""},
	{"ephemeral", "Ephemeral", "", ""},
	{"csi", "CSI", "", ""},
	{"nfs", "NFS", "", ""},
	{fieldImage, "Image", "", ""},
}

// podVolumes groups a pod's volumes by type, one folded item per type with the volume names as
// chips ("2 volumes"); a ConfigMap, Secret or claim volume links to its object. Lens shows a count
// per volume type (capture, 2026-10-11); the folded names are inferred.
func podVolumes(obj any, ns string) []model.DetailItem {
	byType := map[string][]model.Chip{}
	var order []string
	for _, v := range model.NestedSlice(obj, "spec", "volumes") {
		name := model.NestedString(v, "name")
		m, _ := v.(map[string]any)
		if name == "" || m == nil {
			continue
		}
		title, chip := "", model.Chip{Text: name}
		for _, t := range volumeTypes {
			if _, ok := m[t.field]; !ok {
				continue
			}
			title = t.title
			if t.linkKind != "" {
				chip.Link = linkTo(t.linkKind, ns, model.NestedString(m, t.field, t.nameField))
			}
			break
		}
		if title == "" {
			for _, k := range sortedKeys(m) {
				if k != fieldName {
					title = k
					break
				}
			}
		}
		if title == "" {
			continue
		}
		if _, seen := byType[title]; !seen {
			order = append(order, title)
		}
		byType[title] = append(byType[title], chip)
	}
	out := make([]model.DetailItem, 0, len(order))
	for _, title := range order {
		it := model.ChipsItem(title, byType[title])
		it.Fold = "volumes"
		out = append(out, it)
	}
	return out
}

// podConditionToken colours a pod condition. A False condition is a warning, not an error: it
// is also what a pod that is still starting shows, and the Status item says whether it failed.
func podConditionToken(c condition) model.Status {
	switch {
	case c.Reason == "PodCompleted":
		return model.StatusSucceeded // a finished pod is not ready, and that is fine
	case c.Type == "DisruptionTarget":
		return conditionToken(c.Status, model.StatusWarning, model.StatusOK)
	}
	return conditionToken(c.Status, model.StatusOK, model.StatusWarning)
}

// containerSection renders one container as Lens's drawer does (capture, 2026-10-11): Status, Last
// Status, Image, Ports, Environment, Mounts, Command, Arguments, Requests, Limits; an ephemeral
// container's Target after its Status (Cogpit's, D-102). Environment
// values from Secrets and ConfigMaps are named, never resolved. probed says whether readiness
// applies to the container; a running probed container's status says whether it is ready. Each
// port is an item of its own, which forwards it (portforwards.go).
func containerSection(pod model.ObjectRef, name string, spec, status any, probed bool) model.DetailSection {
	var d detailItems
	if status != nil {
		ready, _ := model.NestedBool(status, "ready")
		text, token := containerState(model.NestedMap(status, "state"), ready || !probed)
		if rest, ok := strings.CutPrefix(text, podRunning); ok && probed {
			text = podRunning + ", " + map[bool]string{true: "ready", false: "not ready"}[ready] + rest
		}
		d.addStatus(titleStatus, text, token)
		last, _ := containerState(model.NestedMap(status, "lastState"), false)
		d.add("Last Status", last)
	}
	// An ephemeral container's target: the container whose processes it sees (the owner's "Fix
	// bauen", 2026-10-19, D-102). Plain text: the drawer has no jump to a section of its own.
	d.add("Target", model.NestedString(spec, fieldTargetContainer))
	image := model.NestedString(spec, "image")
	if image == "" {
		image = model.NestedString(status, "image")
	}
	d.add("Image", image)
	if isSidecar(spec) {
		d.add("Restart Policy", "Always")
	}
	for _, it := range containerPortItems(pod, name, spec) {
		d.put(it)
	}
	d.put(model.DetailItem{Name: "Environment", Value: containerEnv(spec), Fold: "variables"})
	d.put(model.DetailItem{Name: "Mounts", Value: containerMounts(spec), Fold: "mounts"})
	d.add("Command", shellJoin(model.NestedStrings(spec, "command")))
	d.add("Arguments", shellJoin(model.NestedStrings(spec, "args")))
	d.add("Requests", resourceAmounts(spec, "requests"))
	d.add("Limits", resourceAmounts(spec, "limits"))
	return model.DetailSection{Title: name, Items: d}
}

// resourceAmounts renders a container's resource requests or limits: "cpu: 100m, memory: 128.0
// MiB", bytes in binary units (D-32).
func resourceAmounts(spec any, field string) string {
	m := model.NestedMap(spec, "resources", field)
	parts := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		if v := model.NestedIntOrString(m, k); v != "" {
			parts = append(parts, k+": "+model.FormatResource(k, v))
		}
	}
	return strings.Join(parts, ", ")
}

// containerState renders a container state: "Running, started …", "Waiting: Reason (message)",
// "Terminated: Reason (exit code N), started …, finished …". An empty state renders as "". A
// running container that is not ready is a warning.
func containerState(state map[string]any, ready bool) (string, model.Status) {
	if running := model.NestedMap(state, "running"); running != nil {
		text := podRunning
		if started := timestamp(model.NestedTime(running, "startedAt")); started != "" {
			text += ", started " + started
		}
		if ready {
			return text, model.StatusOK
		}
		return text, model.StatusWarning
	}
	if waiting := model.NestedMap(state, "waiting"); waiting != nil {
		text := "Waiting"
		reason := model.NestedString(waiting, "reason")
		if reason != "" {
			text += ": " + reason
		}
		if msg := model.NestedString(waiting, "message"); msg != "" {
			text += " (" + msg + ")"
		}
		if isErrorReason(reason) {
			return text, model.StatusError
		}
		return text, model.StatusPending
	}
	if term := model.NestedMap(state, "terminated"); term != nil {
		text := "Terminated"
		if reason := model.NestedString(term, "reason"); reason != "" {
			text += ": " + reason
		}
		code, _ := model.NestedInt64(term, "exitCode")
		text += " (exit code " + itoa(code)
		if signal, _ := model.NestedInt64(term, "signal"); signal != 0 {
			text += ", signal " + itoa(signal)
		}
		text += ")"
		if started := timestamp(model.NestedTime(term, "startedAt")); started != "" {
			text += ", started " + started
		}
		if finished := timestamp(model.NestedTime(term, "finishedAt")); finished != "" {
			text += ", finished " + finished
		}
		if code == 0 {
			return text, model.StatusSucceeded
		}
		return text, model.StatusError
	}
	return "", model.StatusNone
}

// containerPorts renders "name: 8080/TCP" lines, with the host port when one is bound.
func containerPorts(spec any) string {
	var lines []string
	for _, p := range model.NestedSlice(spec, "ports") {
		if line := containerPortText(p); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// containerPortText renders one port as Lens's drawer does (capture, 2026-10-11): "name:
// 8080/TCP", with the host port when one is bound; "" for a port without a number.
func containerPortText(p any) string {
	port := model.NestedIntOrString(p, "containerPort")
	if port == "" {
		return ""
	}
	line := port + "/" + protocol(p)
	if name := model.NestedString(p, "name"); name != "" {
		line = name + ": " + line
	}
	if host := model.NestedIntOrString(p, "hostPort"); host != "" && host != "0" {
		line += " (host port " + host + ")"
	}
	return line
}

// protocol returns a port's protocol; the API defaults it to TCP.
func protocol(port any) string {
	if p := model.NestedString(port, "protocol"); p != "" {
		return p
	}
	return "TCP"
}

// containerEnv renders the environment: literal values as NAME=value, references by what they
// point at. References are never resolved: the drawer must not become a way to read Secrets.
func containerEnv(spec any) string {
	var lines []string
	for _, e := range model.NestedSlice(spec, "envFrom") {
		var line string
		switch {
		case model.NestedMap(e, "secretRef") != nil:
			line = "from secret " + model.NestedString(e, "secretRef", "name")
		case model.NestedMap(e, "configMapRef") != nil:
			line = "from config map " + model.NestedString(e, "configMapRef", "name")
		default:
			continue
		}
		if prefix := model.NestedString(e, "prefix"); prefix != "" {
			line += " (prefix " + prefix + ")"
		}
		lines = append(lines, line)
	}
	for _, e := range model.NestedSlice(spec, "env") {
		name := model.NestedString(e, "name")
		if name == "" {
			continue
		}
		from := model.NestedMap(e, "valueFrom")
		if from == nil {
			lines = append(lines, name+"="+model.NestedString(e, "value"))
			continue
		}
		lines = append(lines, name+": "+envSource(from))
	}
	return strings.Join(lines, "\n")
}

func envSource(from map[string]any) string {
	switch {
	case model.NestedMap(from, "secretKeyRef") != nil:
		return "from secret " + model.NestedString(from, "secretKeyRef", "name") + "/" +
			model.NestedString(from, "secretKeyRef", "key")
	case model.NestedMap(from, "configMapKeyRef") != nil:
		return "from config map " + model.NestedString(from, "configMapKeyRef", "name") + "/" +
			model.NestedString(from, "configMapKeyRef", "key")
	case model.NestedMap(from, "fieldRef") != nil:
		return "from field " + model.NestedString(from, "fieldRef", "fieldPath")
	case model.NestedMap(from, "resourceFieldRef") != nil:
		return "from resource " + model.NestedString(from, "resourceFieldRef", "resource")
	case model.NestedMap(from, "fileKeyRef") != nil:
		return "from file " + model.NestedString(from, "fileKeyRef", "volumeName") + "/" +
			model.NestedString(from, "fileKeyRef", "path") + " key " +
			model.NestedString(from, "fileKeyRef", "key")
	}
	return "from an unknown source"
}

// containerMounts renders "/path from volume (ro, sub-path x)" lines.
func containerMounts(spec any) string {
	var lines []string
	for _, m := range model.NestedSlice(spec, "volumeMounts") {
		path := model.NestedString(m, "mountPath")
		if path == "" {
			continue
		}
		line := path + " from " + model.NestedString(m, "name")
		var opts []string
		if ro, _ := model.NestedBool(m, "readOnly"); ro {
			opts = append(opts, "ro")
		}
		if sub := model.NestedString(m, "subPath"); sub != "" {
			opts = append(opts, "sub-path "+sub)
		}
		if len(opts) > 0 {
			line += " (" + strings.Join(opts, ", ") + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// shellJoin renders an argument vector so that arguments with spaces stay recognisable.
func shellJoin(args []string) string {
	if len(args) == 0 {
		return ""
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\") {
			quoted[i] = strconv.Quote(a)
		} else {
			quoted[i] = a
		}
	}
	return strings.Join(quoted, " ")
}
