package kinds

import "github.com/guided-traffic/cogpit/internal/model"

// Names the drawer items of several kinds share, and field names the pod's drawer reads often.
const (
	itemConditions      = "Conditions"
	itemTaints          = "Taints"
	opExists            = "Exists" // a toleration's or selector's operator
	fieldImage          = "image"
	fieldContainers     = "containers"
	fieldInitContainers = "initContainers"
	// fieldEphemeralContainers and fieldTargetContainer: a pod's ephemeral (debug) containers and the
	// container one targets, whose processes it sees (D-102).
	fieldEphemeralContainers = "ephemeralContainers"
	fieldTargetContainer     = "targetContainerName"
)

// put appends a prepared item (chips, a link, a fold), dropping it when its value is empty, as add
// does for plain values.
func (d *detailItems) put(it model.DetailItem) {
	if it.Value == "" {
		return
	}
	*d = append(*d, it)
}

// linkedItem is a value that names an object: a link to it, or plain text when link is nil.
func linkedItem(name, value string, link *model.ObjectRef) model.DetailItem {
	return model.DetailItem{Name: name, Value: value, Link: link}
}
