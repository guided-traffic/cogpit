package fyneui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Create Secret (G9, D-19): the Secrets view's "+" opens the eighth create dialog. Its fields are
// inferred from Lens's documentation of the Secrets view (the dialog is not captured): the name,
// the namespace, the type (Kubernetes' built-in types, Opaque first), the data as rows of a key and
// a value, and labels and annotations as rows. A value is masked while typed or pasted, as a
// password field is, and the eye at its end shows it (Fyne's revealer); it may span several lines,
// for a PEM certificate or key. Choosing a type fills in the keys it needs (tls.crt and tls.key,
// ...) and a service account token's annotation, in empty rows or new ones, never over what was
// typed (Cogpit's default). The dialog keeps no draft and dispatches secret.create, which only the
// user may run (engine.ErrSecretCreateGUIOnly); its values go nowhere else.

// kindSecrets is the Secrets view's kind key.
const kindSecrets = "secrets"

func init() {
	createDialogs[kindSecrets] = createDialogSpec{command: engine.SecretCreateCommand, namespaced: true, wide: true, build: buildSecretForm}
}

// keyValueColumns are the captions of the dialog's rows.
var keyValueColumns = []string{"Key", "Value"}

// The parameter names of the rows secret.create takes.
const (
	paramKey   = "key"
	paramValue = "value"
)

// secretForm is the Create Secret dialog's own fields, for its type's prefill and for tests.
type secretForm struct {
	typ         *selectField
	data        *rowEditor
	labels      *rowEditor
	annotations *rowEditor
}

// buildSecretForm builds the Create Secret dialog.
func buildSecretForm(f *createForm) {
	f.addName("db-credentials")
	f.addNamespace()
	sf := &secretForm{}
	f.caption("TYPE")
	sf.typ = newSelectField(engine.SecretTypes, nil)
	f.content.Add(sf.typ)
	sf.data = f.addRows("DATA", keyValueColumns, "Add Data", func() *editorRow { return newSecretDataRow(f.sh) })
	f.addHint("Values stay masked until you show them with the eye; they are sent base64-encoded in the Secret's data and are " +
		"never written to the audit log, a log or a draft.")
	sf.labels = f.addRows("LABELS", keyValueColumns, "Add Label", func() *editorRow { return newKeyValueRow("app.kubernetes.io/name", "web") })
	sf.annotations = f.addRows("ANNOTATIONS", keyValueColumns, "Add Annotation", func() *editorRow { return newKeyValueRow("example.com/owner", "team-a") })
	sf.typ.OnChanged = func(t string) {
		prefillKeys(sf.data, engine.SecretTypeKeys(t))
		prefillKeys(sf.annotations, engine.SecretTypeAnnotations(t))
		if f.d != nil { // not while the dialog is built
			f.fit()
		}
	}
	sf.typ.SetSelected(engine.SecretTypeOpaque)
	f.secret = sf
	f.writers = append(f.writers, func(p map[string]any) error {
		p["type"] = sf.typ.Selected
		p["data"] = sf.data.values()
		p["labels"] = sf.labels.values()
		p["annotations"] = sf.annotations.values()
		return nil
	})
}

// newSecretDataRow is one data row: the key, and the value masked in a field that takes several
// lines, with the eye that shows it.
func newSecretDataRow(sh *shell) *editorRow {
	key := newFieldEntry()
	key.SetPlaceHolder("password")
	value := newSecretEntry(sh)
	value.SetMinRowsVisible(1)
	value.SetPlaceHolder("value")
	return &editorRow{fields: []fyne.CanvasObject{key, value}, value: func() (any, bool) {
		k := strings.TrimSpace(key.Text)
		if k == "" && value.Text == "" {
			return nil, false
		}
		return map[string]any{paramKey: k, paramValue: value.Text}, true
	}}
}

// newKeyValueRow is one label or annotation row.
func newKeyValueRow(keyHint, valueHint string) *editorRow {
	key, value := newFieldEntry(), newFieldEntry()
	key.SetPlaceHolder(keyHint)
	value.SetPlaceHolder(valueHint)
	return &editorRow{fields: []fyne.CanvasObject{key, value}, value: func() (any, bool) {
		k := strings.TrimSpace(key.Text)
		if k == "" && value.Text == "" {
			return nil, false
		}
		return map[string]any{paramKey: k, paramValue: value.Text}, true
	}}
}

// rowKey is a row's key field.
func rowKey(r *editorRow) *fieldEntry { return r.fields[0].(*fieldEntry) }

// prefillKeys puts each of keys that no row has yet into the first empty row, else into a new one.
func prefillKeys(e *rowEditor, keys []string) {
	for _, k := range keys {
		var empty *editorRow
		found := false
		for _, r := range e.rows {
			if _, ok := r.value(); !ok && empty == nil {
				empty = r
			}
			found = found || strings.TrimSpace(rowKey(r).Text) == k
		}
		if found {
			continue
		}
		if empty == nil {
			empty = e.add()
		}
		rowKey(empty).SetText(k)
	}
}

// secretEntry is a Secret value's field. What the eye shows can be selected and copied (⌘C, ⌘X or
// the field's menu, whose items call the field's TypedShortcut): the copy goes to the clipboard as
// a copy of a Secret's value from the drawer does, marked concealed for clipboard managers and
// cleared again after 60 seconds if the clipboard still holds it (H-70). A masked value copies
// nothing, as Fyne's password field does.
type secretEntry struct {
	widget.Entry
	sh *shell
}

// CreateRenderer implements fyne.Widget: the wheel passed on (wheelpass.go).
func (e *secretEntry) CreateRenderer() fyne.WidgetRenderer {
	return passWheel(e, e.Entry.CreateRenderer(), func() bool { return e.MultiLine })
}

func newSecretEntry(sh *shell) *secretEntry {
	e := &secretEntry{sh: sh}
	e.MultiLine, e.Password, e.Wrapping = true, true, fyne.TextWrapOff
	e.ExtendBaseWidget(e)
	return e
}

// TypedKey implements fyne.Focusable: Escape goes on to the dialog's Cancel (escape.go).
func (e *secretEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		passEscape(e)
		return
	}
	e.Entry.TypedKey(ev)
}

// TypedShortcut sends a copy or cut of the shown value through the shell's clipboard for Secret
// values; every other shortcut is the entry's.
func (e *secretEntry) TypedShortcut(s fyne.Shortcut) {
	_, isCopy := s.(*fyne.ShortcutCopy)
	_, isCut := s.(*fyne.ShortcutCut)
	if (!isCopy && !isCut) || e.sh == nil {
		e.Entry.TypedShortcut(s)
		return
	}
	text := e.SelectedText()
	if e.Password || text == "" {
		return
	}
	e.sh.copyText(text, true)
	e.sh.copyToast("info", fmt.Sprintf("Copied. The clipboard is cleared in %s.", ttlText(e.sh.secretClipTTL)))
	if isCut { // the entry removes the selection; its own copy goes nowhere
		e.Entry.TypedShortcut(&fyne.ShortcutCut{Clipboard: discardClipboard{}})
	}
}

// discardClipboard is a clipboard that keeps nothing.
type discardClipboard struct{}

func (discardClipboard) Content() string   { return "" }
func (discardClipboard) SetContent(string) {}
