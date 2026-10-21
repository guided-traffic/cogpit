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
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The parameter dialog: what a command needs beyond its object (Scale's replicas, Drain's
// options), in a form generated from the command's JSON schema (engine.Command.DialogFields) and
// what the command says of its dialog (Command.DialogField, D-104). The owner's gate walk of
// 2026-10-20: "Deine Texte sind echt hilfreich, aber sie überladen unsere Masken teilweise enorm"
// (GW6), so a field's hint is the tooltip of a small "?" after its name, never a line under it;
// fields are as wide as what they hold; Scale's replicas have a slider from 1 to 30 with the count
// typed beside it for more (GW2). The rows are a table as every Cogpit dialog's are (D-85): the
// names muted on the left, the fields beside them. A value the schema refuses shows why under its
// field, in the error colour, and greys the confirm button until it is corrected.

// paramDialog is a shown parameter dialog.
type paramDialog struct {
	d       *dialogFrame
	rows    *fyne.Container
	fields  []*paramField
	confirm *widget.Button
	cancel  *widget.Button
}

// openParamDialog shows the dialog for cmd on ref; prefill holds the starting values
// (Command.Prefill: Scale's current replicas). Confirming dispatches the command with the form's
// values; the engine validates them and asks for confirmation where the command's danger needs one.
func (sh *shell) openParamDialog(cmd *engine.Command, ref model.ObjectRef, prefill json.RawMessage) *paramDialog {
	params := map[string]any{}
	if err := json.Unmarshal(prefill, &params); err != nil || len(params) == 0 {
		_ = json.Unmarshal(cmd.ObjectParams(ref, ""), &params)
	}
	pd := &paramDialog{}
	// The one form (WF32), shaped so that no gap follows the arrow (dialogframe.go).
	pd.d = newDialogFrame(sh, objectDialogTitle(cmd.Label, singular(sh.kindTitle(ref.Kind)), ref.Name, ""))
	rows := box(paramRowsLayout{})
	pd.rows = rows
	for _, name := range cmd.DialogFields() {
		f, err := formField(cmd, name, params[name], pd.d.tip)
		if err != nil {
			sh.toast("error", fmt.Sprintf("%s: %v", cmd.Label, err))
			return nil
		}
		f.changed = pd.validate
		pd.fields = append(pd.fields, f)
		rows.Objects = append(rows.Objects, f.label, f.value, newSpace(""), f.problem)
	}
	pd.confirm = widget.NewButton(cmd.Label, nil)
	pd.confirm.Importance = widget.HighImportance
	if cmd.Danger == engine.DangerDestructive { // the verb's button in the error colour, as its confirmation's will be
		pd.confirm.Importance = widget.DangerImportance
	}
	pd.cancel = widget.NewButton("Cancel", pd.d.Hide)
	pd.confirm.OnTapped = func() {
		for _, f := range pd.fields {
			v, err := f.get()
			if err != nil { // the button is greyed then; a value changed in between
				sh.toast("error", fmt.Sprintf("%s: %v", f.label.text, err))
				return
			}
			if v == nil {
				delete(params, f.name) // an optional field left empty
				continue
			}
			params[f.name] = v
		}
		pd.d.Hide()
		sh.dispatch(cmd.ID, params, nil)
	}
	pd.d.setContent(rows)
	pd.d.SetButtons([]fyne.CanvasObject{pd.cancel, pd.confirm})
	pd.validate()
	pd.d.Resize(pd.d.MinSize())
	pd.d.Show()
	return pd
}

// validate shows each field's problem under it and arms the confirm button while there is none.
func (pd *paramDialog) validate() {
	ok := true
	for _, f := range pd.fields {
		if _, err := f.get(); err != nil {
			f.problem.SetText(err.Error())
			f.problem.Show()
			ok = false
		} else {
			f.problem.Hide()
		}
	}
	if pd.confirm == nil {
		return
	}
	if ok {
		pd.confirm.Enable()
	} else {
		pd.confirm.Disable()
	}
	pd.rows.Refresh()
	pd.d.fit() // a problem line shown or gone
}

// schemaInteger is the JSON schema type of whole numbers.
const schemaInteger = "integer"

// paramField is one row of the dialog: the parameter, its name with the hint, its widgets, the
// line under it that says what is wrong with its value.
type paramField struct {
	name    string
	label   *fieldName
	value   fyne.CanvasObject
	problem *textLine
	entry   *widget.Entry  // a number's or a string's; nil for a check
	slider  *widget.Slider // a number's slider (DialogField.SliderMin..SliderMax); nil without
	check   *widget.Check  // a boolean's
	// get returns the value to send, nil for an optional field left empty, or why the field's
	// text cannot be sent.
	get func() (any, error)
	// changed runs after every edit (paramDialog.validate).
	changed func()
}

func (f *paramField) edited() {
	if f.changed != nil {
		f.changed()
	}
}

// formField builds one row for a parameter of cmd, prefilled with value; tip shows its hint.
func formField(cmd *engine.Command, name string, value any, tip *tooltip) (*paramField, error) {
	prop := cmd.Schema.Properties[name]
	label := prop.Title
	if label == "" {
		label = fieldLabel(name)
	}
	required := cmd.Requires(name)
	f := &paramField{name: name, label: newFieldName(label, cmd.DialogHint(name), func() *tooltip { return tip })}
	f.problem = newTextLine("", colorStatusError, theme.SizeNameCaptionText, fyne.TextStyle{})
	f.problem.Hide()
	switch typ := engine.SchemaType(prop); typ {
	case schemaInteger, "number":
		df := cmd.DialogField(name)
		hasSlider := typ == schemaInteger && df.HasSlider()
		f.entry = widget.NewEntry()
		if v, ok := value.(float64); ok {
			f.entry.SetText(strconv.FormatFloat(v, 'f', -1, 64))
		}
		// A field with a slider always has a value: the slider shows one.
		empty := func(s string) bool { return !required && !hasSlider && strings.TrimSpace(s) == "" }
		parse := func(s string) (float64, error) { return parseNumber(s, typ, prop) }
		f.get = func() (any, error) {
			if empty(f.entry.Text) {
				return nil, nil
			}
			v, err := parse(f.entry.Text)
			if err != nil {
				return nil, err
			}
			if typ == schemaInteger {
				return int64(v), nil
			}
			return v, nil
		}
		f.entry.OnChanged = func(string) { f.edited() }
		field := box(widthLayout{width: numberWidth(prop, typ)}, f.entry)
		f.value = field
		if hasSlider {
			f.slider = newBoundSlider(f.entry, df, parse)
			f.value = container.NewHBox(sized(sizeSliderWidth, "", themedSlider(f.slider)), field)
		}
	case "string":
		f.entry = widget.NewEntry()
		if s, ok := value.(string); ok {
			f.entry.SetText(s)
		}
		f.get = func() (any, error) {
			if !required && f.entry.Text == "" {
				return nil, nil
			}
			return f.entry.Text, nil
		}
		f.entry.OnChanged = func(string) { f.edited() }
		f.value = sized(sizeSearchWidth, "", f.entry)
	case "boolean":
		f.check = widget.NewCheck("", nil)
		if b, ok := value.(bool); ok {
			f.check.SetChecked(b)
		}
		f.get = func() (any, error) { return f.check.Checked, nil }
		f.value = f.check
	default:
		return nil, fmt.Errorf("the parameter %q (%s) needs a dialog Cogpit cannot generate", name, typ)
	}
	return f, nil
}

// parseNumber reads a number field's text as the schema allows it: a number, whole for an
// integer, inside the schema's bounds.
func parseNumber(s, typ string, prop *jsonschema.Schema) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("a number")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, errors.New("not a number")
	}
	if typ == schemaInteger && v != math.Trunc(v) {
		return 0, errors.New("not a whole number")
	}
	if prop.Minimum != nil && v < *prop.Minimum {
		return 0, fmt.Errorf("at least %v", *prop.Minimum)
	}
	if prop.Maximum != nil && v > *prop.Maximum {
		return 0, fmt.Errorf("at most %v", *prop.Maximum)
	}
	return v, nil
}

// newBoundSlider is the slider beside a number entry (GW2): dragging it writes its value into the
// entry; a number typed into the entry moves it there, or to its nearer end when the number lies
// beyond its range (Scale's 50 at 30, 0 at 1: the entry holds the value sent).
func newBoundSlider(e *widget.Entry, df engine.DialogField, parse func(string) (float64, error)) *widget.Slider {
	lo, hi := float64(df.SliderMin), float64(df.SliderMax)
	s := widget.NewSlider(lo, hi)
	s.Step = 1
	syncing := false
	place := func() {
		v, err := parse(e.Text)
		if err != nil {
			return
		}
		syncing = true
		s.SetValue(min(max(v, lo), hi))
		syncing = false
	}
	place()
	if e.Text == "" { // nothing prefilled: the slider's start
		e.SetText(strconv.Itoa(df.SliderMin))
	}
	s.OnChanged = func(v float64) {
		if syncing {
			return
		}
		syncing = true
		e.SetText(strconv.FormatFloat(v, 'f', 0, 64))
		syncing = false
	}
	typed := e.OnChanged
	e.OnChanged = func(text string) {
		if !syncing {
			place()
		}
		if typed != nil {
			typed(text)
		}
	}
	return s
}

// numberWidth is a number entry's width: the widest number the schema allows (65535: five digits),
// a digit more for the caret, and the entry's padding; ten digits without a maximum.
func numberWidth(prop *jsonschema.Schema, typ string) float32 {
	digits := 10
	if prop.Maximum != nil && typ == schemaInteger {
		digits = len(strconv.FormatFloat(math.Abs(*prop.Maximum), 'f', 0, 64))
		if prop.Minimum != nil && *prop.Minimum < 0 {
			digits++ // the minus
		}
	}
	th := fyne.CurrentApp().Settings().Theme()
	text := fyne.MeasureText(strings.Repeat("0", digits+1), th.Size(theme.SizeNameText), fyne.TextStyle{}).Width
	return text + 2*th.Size(theme.SizeNameInnerPadding) + 2*th.Size(theme.SizeNameInputBorder)
}

// widthLayout gives its objects a fixed width and their own height: a number entry sized to what
// it holds.
type widthLayout struct{ width float32 }

func (l widthLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(l.width, h)
}

func (l widthLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.Position{})
		o.Resize(size)
	}
}

// paramRowsLayout places the dialog's rows: objects in pairs, a name and its field, then an empty
// name and the field's problem line, a pair left out while its second object is hidden. The names
// take the widest name's column, the fields keep their own width, each row as tall as its taller
// object; the gaps are kvLayout's, the table every Cogpit dialog has.
type paramRowsLayout struct{}

func (paramRowsLayout) dims(objs []fyne.CanvasObject) (nameW, gap float32) {
	_, gap = kvLayout{}.dims()
	for i := 0; i+1 < len(objs); i += 2 {
		if objs[i+1].Visible() {
			nameW = max(nameW, objs[i].MinSize().Width)
		}
	}
	return nameW, gap
}

func (l paramRowsLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	nameW, gap := l.dims(objs)
	var w, h float32
	for i := 0; i+1 < len(objs); i += 2 {
		if !objs[i+1].Visible() {
			continue
		}
		n, v := objs[i].MinSize(), objs[i+1].MinSize()
		w = max(w, v.Width)
		h += max(n.Height, v.Height) + gap/2 // after the last row too, as kvLayout: room above the buttons
	}
	return fyne.NewSize(nameW+gap+w, h)
}

func (l paramRowsLayout) Layout(objs []fyne.CanvasObject, _ fyne.Size) {
	nameW, gap := l.dims(objs)
	y := float32(0)
	for i := 0; i+1 < len(objs); i += 2 {
		if !objs[i+1].Visible() {
			continue
		}
		n, v := objs[i].MinSize(), objs[i+1].MinSize()
		rowH := max(n.Height, v.Height)
		objs[i].Move(fyne.NewPos(0, y))
		objs[i].Resize(fyne.NewSize(nameW, rowH))
		objs[i+1].Move(fyne.NewPos(nameW+gap, y+(rowH-v.Height)/2))
		objs[i+1].Resize(v)
		y += rowH + gap/2
	}
}

// fieldName is a dialog field's name, muted as every dialog table's names are, with a small "?"
// after it whose tooltip is the field's hint (GW6); no "?" without a hint. The whole cell, the
// name's column wide and the row tall, shows the hint: the pointer need not hit the icon. A click
// shows it at once, a second one hides it. The tooltip is asked for at the pointer's event, not
// kept from the start: the navigator's foot builds its "?" before the shell has its tooltip, and
// a hover there ended Cogpit (v0.31.0, the owner's report of 2026-10-20).
type fieldName struct {
	widget.BaseWidget
	text, hint string
	tipOf      func() *tooltip
}

var (
	_ desktop.Hoverable = (*fieldName)(nil)
	_ fyne.Tappable     = (*fieldName)(nil)
)

func newFieldName(text, hint string, tipOf func() *tooltip) *fieldName {
	n := &fieldName{text: text, hint: hint, tipOf: tipOf}
	n.ExtendBaseWidget(n)
	return n
}

// tip is the tooltip the hint shows in; nil before there is one.
func (n *fieldName) tip() *tooltip {
	if n.tipOf == nil {
		return nil
	}
	return n.tipOf()
}

// MouseIn implements desktop.Hoverable.
func (n *fieldName) MouseIn(ev *desktop.MouseEvent) {
	if t := n.tip(); t != nil && n.hint != "" {
		t.hover(n, n.hint, ev.AbsolutePosition)
	}
}

// MouseMoved implements desktop.Hoverable.
func (n *fieldName) MouseMoved(ev *desktop.MouseEvent) {
	if t := n.tip(); t != nil {
		t.moved(n, ev.AbsolutePosition)
	}
}

// MouseOut implements desktop.Hoverable.
func (n *fieldName) MouseOut() {
	if t := n.tip(); t != nil {
		t.hideFor(n)
	}
}

// Tapped implements fyne.Tappable.
func (n *fieldName) Tapped(ev *fyne.PointEvent) {
	if t := n.tip(); t != nil && n.hint != "" {
		t.now(n, n.hint, ev.AbsolutePosition)
	}
}

// CreateRenderer implements fyne.Widget.
func (n *fieldName) CreateRenderer() fyne.WidgetRenderer {
	text := newTextLine(n.text, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	objs := []fyne.CanvasObject{text}
	var icon *widget.Icon
	if n.hint != "" {
		icon = widget.NewIcon(themedIcon(theme.QuestionIcon(), colorMuted))
		objs = append(objs, icon)
	}
	return &fieldNameRenderer{text: text, icon: icon, objs: objs}
}

type fieldNameRenderer struct {
	text *textLine
	icon *widget.Icon
	objs []fyne.CanvasObject
}

func (r *fieldNameRenderer) sizes() (iconW, gap float32) {
	if r.icon == nil {
		return 0, 0
	}
	th := fyne.CurrentApp().Settings().Theme()
	return th.Size(sizeHintIcon), th.Size(theme.SizeNamePadding)
}

func (r *fieldNameRenderer) Layout(size fyne.Size) {
	ts := r.text.MinSize()
	r.text.Move(fyne.NewPos(0, (size.Height-ts.Height)/2))
	r.text.Resize(fyne.NewSize(ts.Width, ts.Height))
	if r.icon != nil {
		iconW, gap := r.sizes()
		r.icon.Move(fyne.NewPos(ts.Width+gap, (size.Height-iconW)/2))
		r.icon.Resize(fyne.NewSquareSize(iconW))
	}
}

func (r *fieldNameRenderer) MinSize() fyne.Size {
	ts := r.text.MinSize()
	iconW, gap := r.sizes()
	return fyne.NewSize(ts.Width+gap+iconW, max(ts.Height, iconW))
}

func (r *fieldNameRenderer) Refresh()                     { r.text.Refresh() }
func (r *fieldNameRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *fieldNameRenderer) Destroy()                     {}

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
