package helm

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/guided-traffic/cogpit/internal/model"
)

// UpgradeTabCommand is the command that opens a release's Helm Upgrade dock tab: Lens's Upgrade,
// the icon in the drawer's title bar and the button beside the drawer's Chart (captured
// 2026-10-12).
const UpgradeTabCommand = "helm.release.upgradeTab"

// clock is the drawer's clock, which Updated's age counts from; a var for tests.
var clock = time.Now

// detail is the profile's Detail: Lens's release drawer (captured 2026-10-12). Properties holds
// Chart ("<chart>-<version>", with an Upgrade button), Updated, Namespace (plain text), Version
// (the chart's version, inferred: the list's Version column) and Status (a neutral grey pill,
// written as Lens writes it; the texts captured 2026-10-12), and Cogpit's Description; then the
// newest revision's user-supplied values, which the GUI shows in an editor with Save and
// "User-supplied values only"; the chart's notes; the release's resources, one table per kind; and
// at the end Cogpit's History (historySection; the owner placed it there, 2026-10-25). The values
// and the notes are private: agents read model.Withheld in their place (object_get), because values
// can hold credentials and notes are rendered from them. The engine adds no generic metadata and no
// events to a virtual kind's drawer (architecture §4.4); the title is "Release: <name>".
func (m *memo) detail(o *unstructured.Unstructured) []model.DetailSection {
	status := model.NestedString(o, fieldStatus)
	ns := o.GetNamespace()
	chart, version := ReleaseChart(o)
	var props []model.DetailItem
	add := func(it model.DetailItem) {
		if it.Value != "" {
			props = append(props, it)
		}
	}
	chartItem := model.DetailItem{Name: titleChart, Value: chartText(chart, version)}
	if params, err := json.Marshal(map[string]any{"ref": model.ObjectRef{Kind: ReleaseKind, Namespace: ns, Name: o.GetName()}}); err == nil {
		chartItem.Actions = []model.ItemAction{{Label: "Upgrade", Command: UpgradeTabCommand, Params: params}}
	}
	add(chartItem)
	if t := model.NestedTime(o, fieldUpdated); !t.IsZero() {
		add(model.DetailItem{Name: titleUpdated, Value: UpdatedText(t, clock())})
	}
	add(model.DetailItem{Name: titleNamespace, Value: ns})
	add(model.DetailItem{Name: "Version", Value: version})
	add(model.DetailItem{Name: titleStatus, Value: StatusText(status), Pill: true})
	add(model.DetailItem{Name: titleDescription, Value: model.NestedString(o, fieldDescription), Wrap: true})
	add(storageItem(o))
	add(model.DetailItem{Name: "Error", Value: model.NestedString(o, "error"), Status: model.StatusError})
	sections := []model.DetailSection{{Title: model.TitleProperties, Items: props}}

	h := m.heavyOf(o)
	values := "Not available: the release record could not be read"
	if h != nil {
		values = h.values
		if values == "" {
			values = "No user-supplied values"
		}
	}
	sections = append(sections, model.DetailSection{Title: TitleValues, Items: []model.DetailItem{{Value: values, Private: true}}})
	if h != nil {
		if h.notes != "" {
			sections = append(sections, model.DetailSection{Title: TitleNotes, Items: []model.DetailItem{{Value: h.notes, Private: true}}})
		}
		sections = append(sections, resourceSections(h.resources)...)
	}
	if hist, ok := historySection(o); ok {
		sections = append(sections, hist)
	}
	return sections
}

// TitleHistory titles the drawer's History, Cogpit's addition (beyond Lens: Lens's drawer, captured
// 2026-10-12, has none, and its Rollback dialog lists the revisions without their status or
// description): every revision the release's records hold, newest first, as helm history and
// k9s's history view list them, so that a failed upgrade a rollback covered stays visible (the
// owner's colleague, 2026-10-25).
const TitleHistory = "History"

// titleDescription titles a revision's description: the line Helm writes into its record, "Upgrade
// complete", "Rollback to 8", or the error a failed install, upgrade or rollback ended with,
// scrubbed of the revision's values (decodeRevision).
const titleDescription = "Description"

// historySection is the drawer's History: Revision, Status (in its colour), Chart
// ("<chart>-<version>"), App Version, Updated (a mark the engine writes in the user's zone and
// format, D-31) and Description, the last on a line of its own under its row, wrapped rather than
// cut (a 600-point drawer has no room for Helm's error beside five columns), a failed revision's in
// the error colour. False for a release object without history.
func historySection(o *unstructured.Unstructured) (model.DetailSection, bool) {
	h := ReleaseHistory(o)
	if len(h) == 0 {
		return model.DetailSection{}, false
	}
	cols := []model.Column{
		{Title: titleRevision, Type: model.ColumnText}, {Title: titleStatus, Type: model.ColumnText},
		{Title: titleChart, Type: model.ColumnText}, {Title: titleAppVersion, Type: model.ColumnText},
		{Title: titleUpdated, Type: model.ColumnText}, {Title: titleDescription, Type: model.ColumnText},
	}
	rows := make([][]model.Cell, 0, len(h))
	for _, e := range h {
		rows = append(rows, []model.Cell{
			{Text: strconv.Itoa(e.Revision), Num: int64(e.Revision)},
			{Text: StatusText(e.Status), Status: StatusToken(e.Status)},
			{Text: chartText(e.Chart, e.ChartVersion)},
			{Text: e.AppVersion},
			{Text: model.MarkTime(e.Updated)},
			{Text: e.Description, Status: failedStatus(e.Status)},
		})
	}
	t := model.CellTable(cols, rows)
	t.LastUnder = true
	return model.DetailSection{Title: TitleHistory, Table: t}, true
}

// failedStatus colours a revision's description in the History: the error colour for a failed
// revision's, which is Helm's error message, else none.
func failedStatus(status string) model.Status {
	if StatusToken(status) == model.StatusError {
		return model.StatusError
	}
	return model.StatusNone
}

// TitleStorage titles the drawer's Storage, Cogpit's addition (inferred: Lens's drawer, captured
// 2026-10-12, does not say where a release is kept): the Helm storage driver whose records hold
// the release, "Secrets" (Helm's default) or "ConfigMaps" (HELM_DRIVER=configmap).
const TitleStorage = "Storage"

// storageItem is the drawer's Storage: the driver, and a note in the warning colour when the other
// driver holds a release of the same name in the namespace, which the drawer does not show
// (groupReleases): "also stored in the <other> driver (older)", the owner's wording (A39,
// 2026-10-14 evening). The release shown is the one deployed last; when both were deployed at the
// same moment, the note says so instead of "older" (Cogpit's wording: the tie goes to Secrets,
// Helm's default).
func storageItem(o *unstructured.Unstructured) model.DetailItem {
	text := map[Driver]string{DriverSecret: "Secrets", DriverConfigMap: "ConfigMaps"}
	it := model.DetailItem{Name: TitleStorage, Value: text[ReleaseStorage(o)]}
	if other := Driver(model.NestedString(o, fieldHidden)); other != "" {
		age := "older"
		if older, _ := model.NestedBool(o, fieldHiddenOlder); !older {
			age = "deployed at the same time; the Secrets' one is shown, Helm's default"
		}
		it.Value += fmt.Sprintf("; also stored in the %s driver (%s)", text[other], age)
		it.Status = model.StatusWarning
	}
	return it
}

// heavyOf returns the values, notes and resources the memo keeps for a release object's newest
// revision, or nil.
func (m *memo) heavyOf(o *unstructured.Unstructured) *heavy {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.get(o.GetUID(), o.GetResourceVersion()); r != nil && r.heavy != nil {
		h := *r.heavy
		return &h
	}
	return nil
}

// UpdatedText is a release's Updated as Lens's drawer writes it, the age and then the time in
// parentheses, "68s ago (12 Oct 2026, 8:30:16 CEST)" (captured 2026-10-12), with the time as a
// mark that the engine writes in the user's zone and format (D-31): "68s ago (12.10.2026
// 08:30:16)". The age is kubectl's human duration, which writes 68 seconds as "68s" as Lens does
// (the other lengths inferred).
func UpdatedText(t, now time.Time) string {
	return fmt.Sprintf("%s ago (%s)", duration.HumanDuration(now.Sub(t)), model.MarkTime(t))
}

// RevisionText is a revision as Lens's Rollback dialog lists it (captured 2026-10-12), "12 -
// lens-history-test-0.1.0 - 1.0, updated: 12/10/2026, 08:30:16", with the time written in f, the
// user's zone and format (D-31).
func RevisionText(e HistoryEntry, f model.TimeFormat) string {
	return fmt.Sprintf("%d - %s - %s, updated: %s", e.Revision, chartText(e.Chart, e.ChartVersion), e.AppVersion, f.Format(e.Updated))
}
