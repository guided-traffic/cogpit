package kinds

import (
	"encoding/json"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// deviceClasses has the columns of Lens's documentation (read, not captured): Name, Driver ("the
// driver whose devices the class selects"), Age; its drawer names Selectors (the CEL expressions
// the class matches devices on), Config and Extended Resource Name. Inferred: Driver read from the
// selectors' comparisons of device.driver with a literal, else from the opaque configuration's
// drivers; no status token (a class has no state); the drawer's form.
var deviceClasses = &model.KindProfile{
	Key:      keyDeviceClasses,
	Group:    groupResource,
	Version:  "v1",
	Resource: keyDeviceClasses,
	Kind:     "DeviceClass",
	Title:    "Device Classes",
	Columns: []model.Column{
		nameColumn,
		{ID: fieldDriver, Title: titleDriver, Type: model.ColumnText, Width: 240},
		ageColumn,
	},
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return []model.Cell{
			nameCell(obj),
			{Text: strings.Join(deviceClassDrivers(obj), ", ")},
			model.AgeCell(created(obj)),
		}, model.StatusNone
	},
	Fields: map[string]fieldFunc{
		fieldDriver: deviceClassDrivers,
		"selector": func(obj *unstructured.Unstructured) []string {
			return deviceClassSelectors(obj)
		},
	},
	Detail: deviceClassDetail,
}

// celDriver finds a comparison of device.driver with a string literal in a CEL expression, either
// way round: device.driver == "gpu.example.com", 'gpu.example.com' == device.driver.
var celDriver = regexp.MustCompile(`device\.driver\s*==\s*["']([^"']+)["']|["']([^"']+)["']\s*==\s*device\.driver`)

// maxDriverExpression bounds the expression text the driver is searched in: an absurdly long
// expression does not cost the row builder more than this.
const maxDriverExpression = 4096

// deviceClassDrivers returns the drivers a class selects: those its selectors compare
// device.driver with, else those its opaque configuration is for, without duplicates.
func deviceClassDrivers(obj *unstructured.Unstructured) []string {
	var out []string
	for _, e := range deviceClassSelectors(obj) {
		if len(e) > maxDriverExpression {
			e = e[:maxDriverExpression]
		}
		for _, m := range celDriver.FindAllStringSubmatch(e, -1) {
			out = appendUnique(out, m[1], m[2])
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, c := range model.NestedSlice(obj, "spec", "config") {
		out = appendUnique(out, model.NestedString(c, "opaque", fieldDriver))
	}
	return out
}

// deviceClassSelectors returns the CEL expressions of spec.selectors, each on one line.
func deviceClassSelectors(obj *unstructured.Unstructured) []string {
	var out []string
	for _, s := range model.NestedSlice(obj, "spec", "selectors") {
		if e := strings.TrimSpace(model.NestedString(s, "cel", "expression")); e != "" {
			out = append(out, singleLine(e))
		}
	}
	return out
}

func deviceClassDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	d.add(titleDriver, strings.Join(deviceClassDrivers(obj), ", "))
	d.add("Extended Resource Name", model.NestedString(obj, "spec", "extendedResourceName"))
	sections := withSection(nil, model.TitleProperties, d)

	if sel := deviceClassSelectors(obj); len(sel) > 0 {
		t := &model.DetailTable{Columns: []string{"CEL Expression"}}
		for _, e := range sel {
			t.Rows = append(t.Rows, []string{e})
		}
		sections = append(sections, model.DetailSection{Title: "Selectors", Table: t})
	}
	if t := opaqueConfigTable(model.NestedSlice(obj, "spec", "config")); t != nil {
		sections = append(sections, model.DetailSection{Title: "Config", Table: t})
	}
	return sections
}

// opaqueConfigTable lists opaque driver configurations: the driver and its parameters as compact
// JSON, cut at 1 KiB (cutText).
func opaqueConfigTable(configs []any) *model.DetailTable {
	t := &model.DetailTable{Columns: []string{titleDriver, "Parameters"}}
	for _, c := range configs {
		driver := model.NestedString(c, "opaque", fieldDriver)
		if driver == "" {
			continue
		}
		params := ""
		if v, ok := model.NestedValue(c, "opaque", "parameters"); ok && v != nil {
			if b, err := json.Marshal(v); err == nil {
				params = string(b)
			}
		}
		t.Rows = append(t.Rows, []string{driver, cutText(params)})
	}
	if len(t.Rows) == 0 {
		return nil
	}
	return t
}
