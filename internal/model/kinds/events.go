package kinds

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

const eventWarning = "Warning"

// events is the core v1 Event. The API serves one object in two shapes: the classic fields
// (source, firstTimestamp, lastTimestamp, count) that older clients write, and the newer ones
// (eventTime, series, reportingComponent, reportingInstance) that the events.k8s.io API writes.
// Every reader here tries the classic field, then the newer one, and also accepts the field names
// the events.k8s.io API uses (regarding, note, reportingController, deprecated*), so an object
// cached from either API renders the same.
var events = &model.KindProfile{
	Key:        "events",
	Version:    "v1",
	Resource:   "events",
	Kind:       "Event",
	Title:      "Events",
	Namespaced: true,
	Columns: []model.Column{
		// Lens shows no name for events. The hidden name column keeps the model's rule that
		// the first column is the name, which views and agents rely on to identify a row.
		{ID: "name", Title: "Name", Type: model.ColumnText, Hidden: true},
		typeColumn,
		{ID: "message", Title: "Message", Type: model.ColumnText},
		namespaceColumn,
		{ID: "involvedObject", Title: "Involved Object", Type: model.ColumnText},
		{ID: "source", Title: "Source", Type: model.ColumnText},
		{ID: "count", Title: "Count", Type: model.ColumnNumber},
		ageColumn,
		// Lens shows Last Seen as a timestamp ("2026-10-11T16:04:00Z", in UTC), not an age
		// (capture); Cogpit writes it in the user's zone and format when it is drawn (D-31) and
		// sorts it by time.
		{ID: "lastSeen", Title: "Last Seen", Type: model.ColumnTime},
	},
	// An event's state is its Type, Normal or Warning (the status filter, D-34).
	States: []model.StateColumn{{ID: typeColumn.ID}},
	Cells:  eventCells,
	Fields: map[string]fieldFunc{
		"involvedObject": func(obj *unstructured.Unstructured) []string {
			kind, name := eventObject(obj)
			if kind == "" && name == "" {
				return nil
			}
			return []string{kind + "/" + name}
		},
		"reason": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "reason"))
		},
		idType: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "type"))
		},
		"source": func(obj *unstructured.Unstructured) []string {
			component, host := eventSource(obj)
			return appendUnique(nil, component, host)
		},
	},
	Detail: eventDetail,
	// An involved custom resource links to its kind's view through its definition (Lens links
	// custom kinds too, capture); the definitions are the custom kinds' own watch.
	Needs:      []model.RelatedIndex{CustomKindsByGroupKind},
	CellsWith:  eventCellsWith,
	DetailWith: eventDetailWith,
}

func eventCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return eventCellsWith(obj, model.NoRelated)
}

func eventCellsWith(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
	typ := model.NestedString(obj, "type")
	rowToken := model.StatusNone
	msg := model.Cell{Text: singleLine(eventMessage(obj))}
	if typ == eventWarning {
		// As in Lens: a warning's message is red; the Type cell and normal events stay
		// uncoloured, so warnings stand out (capture).
		rowToken = model.StatusWarning
		msg.Status = model.StatusError
	}
	lastSeen := model.Cell{}
	if t := eventLastSeen(obj); !t.IsZero() {
		lastSeen = model.Cell{Num: t.Unix()}
	}
	return []model.Cell{
		nameCell(obj),
		{Text: typ},
		msg,
		namespaceCell(obj),
		{Text: eventObjectText(obj), Link: eventObjectLink(obj, rel)},
		{Text: eventSourceText(obj)},
		{Num: eventCount(obj)},
		model.AgeCell(eventFirstSeen(obj)),
		lastSeen,
	}, rowToken
}

// eventTypeToken colours the event type in the drawer: Normal ok, Warning warning, anything newer
// uncoloured. The table leaves the Type cell plain and colours a warning's message instead.
func eventTypeToken(typ string) model.Status {
	switch typ {
	case "Normal":
		return model.StatusOK
	case eventWarning:
		return model.StatusWarning
	}
	return model.StatusNone
}

func eventMessage(obj any) string {
	if m := model.NestedString(obj, "message"); m != "" {
		return m
	}
	return model.NestedString(obj, "note")
}

// eventObject returns the kind and name of the object the event is about.
func eventObject(obj any) (kind, name string) {
	ref := model.NestedMap(obj, "involvedObject")
	if ref == nil {
		ref = model.NestedMap(obj, "regarding")
	}
	return model.NestedString(ref, "kind"), model.NestedString(ref, "name")
}

// eventObjectLink links the involved object ("Kind: name", a link in Lens, custom kinds included,
// capture). Built-in kinds link by their key; a custom resource's key needs its plural, which an
// event does not carry, so it links through its definition in rel (refToWith) and stays plain
// without one. A namespaced object without a namespace in the reference lives in the event's
// namespace.
func eventObjectLink(obj any, rel model.Related) *model.ObjectRef {
	ref := model.NestedMap(obj, "involvedObject")
	if ref == nil {
		ref = model.NestedMap(obj, "regarding")
	}
	ns := model.NestedString(ref, "namespace")
	if ns == "" {
		ns = model.NestedString(obj, "metadata", "namespace")
	}
	return RefToWith(rel, model.NestedString(ref, "apiVersion"), model.NestedString(ref, "kind"), ns, model.NestedString(ref, "name"))
}

// eventObjectText renders the involved object as "Kind: name".
func eventObjectText(obj any) string {
	kind, name := eventObject(obj)
	if kind == "" {
		return name
	}
	return kind + ": " + name
}

// eventSource returns who reported the event: the classic source (component and host) when it
// is set, else the reporting controller and instance.
func eventSource(obj any) (component, host string) {
	if component, host = classicSource(obj, "source"); component != "" || host != "" {
		return component, host
	}
	if component, host = classicSource(obj, "deprecatedSource"); component != "" || host != "" {
		return component, host
	}
	component = model.NestedString(obj, "reportingComponent")
	if component == "" {
		component = model.NestedString(obj, "reportingController")
	}
	host = model.NestedString(obj, "reportingInstance")
	if host == component {
		host = "" // some controllers report their name as the instance too
	}
	return component, host
}

func classicSource(obj any, field string) (component, host string) {
	return model.NestedString(obj, field, "component"), model.NestedString(obj, field, "host")
}

// eventSourceText renders the source as "component, host".
func eventSourceText(obj any) string {
	component, host := eventSource(obj)
	switch {
	case component == "":
		return host
	case host == "":
		return component
	}
	return component + ", " + host
}

// eventCount is the number of occurrences: the series count for a newer event, the classic
// count otherwise, and 1 when neither is set, since an event that exists happened once.
func eventCount(obj any) int64 {
	if n := nonNegative(obj, "series", "count"); n > 0 {
		return n
	}
	if n := nonNegative(obj, "count"); n > 0 {
		return n
	}
	if n := nonNegative(obj, "deprecatedCount"); n > 0 {
		return n
	}
	return 1
}

// eventFirstSeen is when the event first happened: the classic first timestamp, else the
// newer event time, else when the object was created.
func eventFirstSeen(obj any) time.Time {
	return firstTime(
		model.NestedTime(obj, "firstTimestamp"),
		model.NestedTime(obj, "deprecatedFirstTimestamp"),
		model.NestedTime(obj, "eventTime"),
		created(obj),
	)
}

// eventLastSeen is when the event last happened: the classic last timestamp, else the series'
// last observation, else the event time, else when the object was created.
func eventLastSeen(obj any) time.Time {
	return firstTime(
		model.NestedTime(obj, "lastTimestamp"),
		model.NestedTime(obj, "deprecatedLastTimestamp"),
		model.NestedTime(obj, "series", "lastObservedTime"),
		model.NestedTime(obj, "eventTime"),
		created(obj),
	)
}

// eventDetail adds the event's fields to the drawer's Properties, after the generic metadata.
func eventDetail(obj *unstructured.Unstructured) []model.DetailSection {
	return eventDetailWith(obj, model.NoRelated)
}

func eventDetailWith(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	var d detailItems
	typ := model.NestedString(obj, "type")
	d.addStatus("Type", typ, eventTypeToken(typ))
	d.add("Reason", model.NestedString(obj, "reason"))
	d.add("Message", eventMessage(obj))
	d.put(linkedItem("Involved Object", eventObjectText(obj), eventObjectLink(obj, rel)))
	d.add("Source", eventSourceText(obj))
	d.add("Count", itoa(eventCount(obj)))
	d.add("First Seen", timestamp(eventFirstSeen(obj)))
	d.add("Last Seen", timestamp(eventLastSeen(obj)))
	return []model.DetailSection{{Title: model.TitleProperties, Items: d}}
}
