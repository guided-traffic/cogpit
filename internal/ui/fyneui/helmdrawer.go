package fyneui

import (
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/yamlview"
)

// The release drawer's values, as Lens's (captured 2026-10-12): a "User-supplied values only"
// checkbox, unchecked at first, the values in an editor and Save as a bar the drawer's width under
// the editor. The drawer model's Values item is cut for the drawer and withheld from agents; the
// editor reads the whole values with helm.release.values (from the cache, for the GUI only) and
// Save upgrades the release with the editor's text through helm.release.upgrade, based on the
// revision it read (inferred from Lens: its Save upgrades the release). Unchecked, the editor shows
// every value, the chart's defaults merged in, and Save makes all of them user-supplied values,
// which the status line says once the text is edited; checked, it shows the user-supplied ones.

func init() {
	drawerSections[helm.ReleaseKind+"\x00"+helm.TitleValues] = releaseValuesSection
	drawerSections[helm.ReleaseKind+"\x00"+helm.TitleNotes] = releaseNotesSection
}

// releaseNotesSection renders the chart's notes the full width of the drawer, as the chart wrote
// them, selectable so that a command in them can be copied.
func releaseNotesSection(_ *drawer, _ model.ObjectRef, sec model.DetailSection) fyne.CanvasObject {
	text := ""
	if len(sec.Items) > 0 {
		text = sec.Items[0].Value
	}
	l := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	l.Wrapping = fyne.TextWrapBreak
	l.Selectable = true
	return l
}

// releaseValuesKey is where the drawer keeps a release's values editor across rebuilds.
const releaseValuesKey = "helm.values"

// valuesEditorLines is how many lines of values the drawer's editor shows.
const valuesEditorLines = 16

// helmValuesParams and helmUpgradeParams mirror helm.release.values' and helm.release.upgrade's
// parameters.
type (
	helmValuesParams struct {
		refParams
		All bool `json:"all,omitempty"`
	}
	helmUpgradeParams struct {
		refParams
		Tab          string  `json:"tab,omitempty"`
		TextRevision uint64  `json:"textRevision,omitempty"`
		Values       *string `json:"values,omitempty"`
		Revision     int     `json:"revision,omitempty"`
		Version      string  `json:"version,omitempty"`    // the Upgrade tab's chosen chart version
		Repository   string  `json:"repository,omitempty"` // where another version is downloaded from
	}
)

// releaseValues is the editor of one release's values in the drawer.
type releaseValues struct {
	sh       *shell
	ref      model.ObjectRef
	view     *yamlview.View
	userOnly *widget.Check
	save     *textButton
	status   *textLine
	root     fyne.CanvasObject

	loaded   string // the text as read: typing makes the editor dirty
	revision int    // the revision the text is of
	all      bool   // the text holds every value
	busy     bool
	loads    int // the current read; an older one's answer is dropped
	closed   bool
}

// releaseValuesSection renders the Values section of a release's drawer: the editor kept for the
// shown release, refreshed from the cache unless the user has typed.
func releaseValuesSection(d *drawer, ref model.ObjectRef, _ model.DetailSection) fyne.CanvasObject {
	rv, ok := d.kept[releaseValuesKey].(*releaseValues)
	if !ok || rv.ref != ref {
		rv = newReleaseValues(d.sh, ref)
		d.kept[releaseValuesKey] = rv
	}
	if !rv.dirty() && !rv.busy {
		rv.load()
	}
	return rv.root
}

func newReleaseValues(sh *shell, ref model.ObjectRef) *releaseValues {
	rv := &releaseValues{sh: sh, ref: ref, view: yamlview.New(), all: true}
	rv.view.SetReadOnly(false)
	rv.view.OnChanged = rv.changed
	rv.userOnly = widget.NewCheck("User-supplied values only", func(on bool) {
		if on == !rv.all {
			return
		}
		rv.all = !on
		rv.load()
	})
	rv.save = newPlateButton("Save", theme.DocumentSaveIcon(), rv.upgrade)
	rv.save.Importance = widget.HighImportance
	rv.save.Disable()
	rv.status = newTextLine("Loading…", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	rv.status.truncate = true
	editor := box(linesHeight{lines: valuesEditorLines}, container.NewStack(newSurface(theme.ColorNameInputBackground), rv.view))
	rv.root = container.NewVBox(rv.userOnly, editor, rv.save, rv.status)
	return rv
}

// dirty reports whether the user changed the text since it was read.
func (rv *releaseValues) dirty() bool { return rv.view.Text() != rv.loaded }

func (rv *releaseValues) changed() {
	switch {
	case rv.dirty() && rv.all:
		rv.setStatus(fmt.Sprintf("Edited, based on revision %d: Save upgrades the release and makes every value in the editor a user-supplied value.", rv.revision), colorStatusWarning)
	case rv.dirty():
		rv.setStatus(fmt.Sprintf("Edited, based on revision %d: Save upgrades the release.", rv.revision), colorMuted)
	}
}

// load reads the values (user-supplied or all) from the cache.
func (rv *releaseValues) load() {
	rv.loads++
	n, all := rv.loads, rv.all
	rv.sh.dispatch("helm.release.values", helmValuesParams{refParams: refParams{Ref: rv.ref}, All: all}, func(res any, err error) {
		if rv.closed || n != rv.loads {
			return
		}
		var v helm.Values
		if err == nil {
			err = decode(res, &v)
		}
		if err != nil {
			rv.setStatus("Could not read the values: "+errText(err), colorStatusError)
			return
		}
		if v.YAML == rv.loaded && v.Revision == rv.revision && v.All == all {
			return // a rebuild of the drawer: nothing changed
		}
		rv.loaded, rv.revision = v.YAML, v.Revision
		rv.view.SetText(v.YAML)
		rv.save.Enable()
		rv.showLoaded()
	})
}

// showLoaded says what the editor holds.
func (rv *releaseValues) showLoaded() {
	switch {
	case rv.all:
		rv.setStatus(fmt.Sprintf("Revision %d, every value, the chart's defaults included.", rv.revision), colorMuted)
	case rv.loaded == "":
		rv.setStatus(fmt.Sprintf("Revision %d: no user-supplied values.", rv.revision), colorMuted)
	default:
		rv.setStatus(fmt.Sprintf("Revision %d: the user-supplied values.", rv.revision), colorMuted)
	}
}

// upgrade saves: the release upgraded with the editor's text, based on the revision it was read
// from. A release that moved since is refused by the engine and read again.
func (rv *releaseValues) upgrade() {
	if rv.busy || rv.revision == 0 {
		return
	}
	text := rv.view.Text()
	rv.busy = true
	rv.save.Disable()
	rv.setStatus("Upgrading…", colorMuted)
	rv.sh.dispatch("helm.release.upgrade", helmUpgradeParams{refParams: refParams{Ref: rv.ref}, Values: &text, Revision: rv.revision}, func(res any, err error) {
		if rv.closed {
			return
		}
		rv.busy = false
		rv.save.Enable()
		if errors.Is(err, engine.ErrTargetsChanged) {
			rv.rebase()
			return
		}
		if err != nil {
			rv.setStatus("Not upgraded: "+errText(err), colorStatusError)
			return
		}
		var r struct {
			Revision int `json:"revision"`
		}
		_ = decode(res, &r)
		rv.loaded, rv.revision = text, r.Revision
		rv.setStatus(fmt.Sprintf("Upgraded to revision %d.", r.Revision), colorStatusOK)
	})
}

// rebase follows a release that moved to another revision since the values were read, as an Edit
// tab follows a conflict: the typed text stays, now based on the newest revision, and saving it
// again takes another click.
func (rv *releaseValues) rebase() {
	rv.loads++
	n := rv.loads
	rv.sh.dispatch("helm.release.values", helmValuesParams{refParams: refParams{Ref: rv.ref}, All: rv.all}, func(res any, err error) {
		var v helm.Values
		if rv.closed || n != rv.loads || err != nil || decode(res, &v) != nil {
			return
		}
		moved := v.Revision != rv.revision
		rv.loaded, rv.revision = v.YAML, v.Revision
		if moved {
			rv.setStatus(fmt.Sprintf("The release moved to revision %d since the values were read; nothing was upgraded. Save again to upgrade it with this text.", v.Revision), colorStatusWarning)
		}
	})
}

func (rv *releaseValues) setStatus(text string, c fyne.ThemeColorName) {
	rv.status.SetText(text)
	rv.status.SetColor(c)
}

// close drops the editor's pending reads when the drawer moves on.
func (rv *releaseValues) close() { rv.closed = true }

// linesHeight gives its objects the height of a number of text lines and the width they ask for.
type linesHeight struct{ lines int }

func (l linesHeight) MinSize(objs []fyne.CanvasObject) fyne.Size {
	th := fyne.CurrentApp().Settings().Theme()
	var w float32
	for _, o := range objs {
		w = fyne.Max(w, o.MinSize().Width)
	}
	line := fyne.MeasureText("M", th.Size(theme.SizeNameText), fyne.TextStyle{Monospace: true}).Height
	return fyne.NewSize(w, float32(l.lines)*line+2*th.Size(theme.SizeNameInnerPadding))
}

func (l linesHeight) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}
