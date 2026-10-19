package fyneui

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// openParamDialog asks for the parameters a command needs beyond its object, in a form generated
// from the command's JSON schema (engine.Command.DialogFields): an entry per integer, number and
// string field, a check per boolean, the field's schema description as its hint. prefill holds
// the starting values (Command.Prefill: Scale's current replicas). Confirming dispatches the
// command with the form's values; the engine validates them and asks for confirmation where the
// command's danger needs one.
func (sh *shell) openParamDialog(cmd *engine.Command, ref model.ObjectRef, prefill json.RawMessage) {
	params := map[string]any{}
	if err := json.Unmarshal(prefill, &params); err != nil || len(params) == 0 {
		_ = json.Unmarshal(cmd.ObjectParams(ref, ""), &params)
	}
	var items []*widget.FormItem
	var read []func() error
	for _, name := range cmd.DialogFields() {
		prop := cmd.Schema.Properties[name]
		item, get, err := formField(name, prop, params[name], cmd.Requires(name))
		if err != nil {
			sh.toast("error", fmt.Sprintf("%s: %v", cmd.Label, err))
			return
		}
		items = append(items, item)
		read = append(read, func() error {
			v, err := get()
			if err != nil {
				return fmt.Errorf("%s: %w", item.Text, err)
			}
			if v == nil {
				delete(params, name) // an optional field left empty
				return nil
			}
			params[name] = v
			return nil
		})
	}
	title := cmd.Label + " " + objectText(singular(sh.kindTitle(ref.Kind)), ref.Name) // the one form (WF32)
	d := dialog.NewForm(title, cmd.Label, "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		for _, r := range read {
			if err := r(); err != nil {
				sh.toast("error", err.Error())
				return
			}
		}
		sh.dispatch(cmd.ID, params, nil)
	}, sh.win)
	th := sh.app.Settings().Theme()
	d.Resize(fyne.NewSize(th.Size(sizeDialogWidth), d.MinSize().Height))
	d.Show()
}

// schemaInteger is the JSON schema type of whole numbers.
const schemaInteger = "integer"

// formField builds one form row for a parameter: the widget, prefilled with value, and a reader
// that returns the value to send, or nil for an optional field left empty.
func formField(name string, prop *jsonschema.Schema, value any, required bool) (*widget.FormItem, func() (any, error), error) {
	label := prop.Title
	if label == "" {
		label = fieldLabel(name)
	}
	item := &widget.FormItem{Text: label, HintText: prop.Description}
	switch typ := engine.SchemaType(prop); typ {
	case schemaInteger, "number":
		e := widget.NewEntry()
		if f, ok := value.(float64); ok {
			e.SetText(strconv.FormatFloat(f, 'f', -1, 64))
		}
		empty := func(s string) bool { return !required && strings.TrimSpace(s) == "" }
		parse := func(s string) (float64, error) {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
				return 0, errors.New("not a number")
			}
			if typ == schemaInteger && f != math.Trunc(f) {
				return 0, errors.New("not a whole number")
			}
			if prop.Minimum != nil && f < *prop.Minimum {
				return 0, fmt.Errorf("at least %v", *prop.Minimum)
			}
			if prop.Maximum != nil && f > *prop.Maximum {
				return 0, fmt.Errorf("at most %v", *prop.Maximum)
			}
			return f, nil
		}
		e.Validator = func(s string) error {
			if empty(s) {
				return nil
			}
			_, err := parse(s)
			return err
		}
		item.Widget = e
		return item, func() (any, error) {
			if empty(e.Text) {
				return nil, nil
			}
			f, err := parse(e.Text)
			if err != nil {
				return nil, err
			}
			if typ == schemaInteger {
				return int64(f), nil
			}
			return f, nil
		}, nil
	case "string":
		e := widget.NewEntry()
		if s, ok := value.(string); ok {
			e.SetText(s)
		}
		item.Widget = e
		return item, func() (any, error) {
			if !required && e.Text == "" {
				return nil, nil
			}
			return e.Text, nil
		}, nil
	case "boolean":
		c := widget.NewCheck("", nil)
		if b, ok := value.(bool); ok {
			c.SetChecked(b)
		}
		item.Widget = c
		return item, func() (any, error) { return c.Checked, nil }, nil
	default:
		return nil, nil, fmt.Errorf("the parameter %q (%s) needs a dialog Cogpit cannot generate", name, typ)
	}
}

// fieldLabel turns a parameter name into a form label: "replicas" → "Replicas", "gracePeriod" →
// "Grace period".
func fieldLabel(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case i == 0:
			b.WriteRune(unicode.ToUpper(r))
		case unicode.IsUpper(r):
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
