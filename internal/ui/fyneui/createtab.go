package fyneui

import (
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// maxCreateLines is how many per-object results a Create resource tab lists before it sums up.
const maxCreateLines = 6

// createTab is a Create resource dock tab: save and search buttons (createtab_save.go), a template
// picker ("Select Template …", Cogpit's own templates under a "Cogpit" heading), the YAML editor,
// and Create and Create & Close, which create every object of the text (object.create) and list
// what happened to each.
type createTab struct {
	sh   *shell
	tab  engine.DockTab
	pane *editorPane

	picker    *textButton
	templates []engine.Template
	menu      *widget.PopUp
	status    *textLine
	results   *fyne.Container
	createBtn *textButton
	closeBtn  *textButton
	root      fyne.CanvasObject
	busy      bool

	// The save and search buttons before the picker (captured), the find widget below the editor,
	// and the file save dialog (createtab_save.go; a test replaces it).
	saveBtn, searchBtn *tipButton
	find               *findBar
	saveDialog         func(name string, data []byte)
}

func newCreateTab(sh *shell, t engine.DockTab) *createTab {
	c := &createTab{sh: sh, tab: t, pane: newEditorPane(sh, t.ID)}
	c.picker = newPlateButton("Select Template …", chevronDownIcon(), c.showTemplates) // a list's caret: the chevron (VW4)
	c.picker.IconPlacement = widget.ButtonIconTrailingText
	c.status = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	c.status.truncate = true
	c.results = container.NewVBox()
	c.createBtn = newPlateButton("Create", theme.ContentAddIcon(), func() { c.create(false) })
	c.createBtn.Importance = widget.HighImportance
	c.closeBtn = newPlateButton("Create & Close", nil, func() { c.create(true) })
	c.newTools()

	toolbar := container.NewStack(newSurface(colorDock), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewBorder(nil, nil, container.NewHBox(c.saveBtn, c.searchBtn, newDivider(true), c.picker), nil)))
	footer := container.NewStack(newSurface(colorDock), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewBorder(c.results, nil, nil, container.NewHBox(c.closeBtn, c.createBtn), c.status)))
	c.root = container.NewBorder(
		container.NewVBox(toolbar, newDivider(false)),
		container.NewVBox(newDivider(false), footer), nil, nil,
		container.NewBorder(nil, c.find.root, nil, nil,
			container.NewStack(newSurface(theme.ColorNameInputBackground), c.pane.view)))
	c.pane.load(func(_ engine.EditorState, err error) {
		if err != nil {
			c.setStatus("Could not open the editor: "+err.Error(), colorStatusError)
		}
	})
	sh.dispatch("resource.templates", struct{}{}, func(res any, err error) {
		if err == nil {
			_ = decode(res, &c.templates) // without templates the picker offers none
		}
	})
	return c
}

func (c *createTab) setStatus(text string, col fyne.ThemeColorName) {
	c.status.SetText(text)
	c.status.SetColor(col)
}

// templateList is the picker's content: the "Cogpit" heading above a scrolling list of the
// templates, one per kind in alphabetical order. Choosing one replaces the tab's text.
func (c *createTab) templateList() (heading *textLine, list *widget.List) {
	heading = newTextLine(engine.TemplateGroup, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	list = widget.NewList(
		func() int { return len(c.templates) },
		func() fyne.CanvasObject {
			return inset(theme.SizeNameInnerPadding, "", newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= 0 && i < len(c.templates) {
				o.(*fyne.Container).Objects[0].(*textLine).SetText(c.templates[i].Name)
			}
		})
	list.HideSeparators = true
	list.OnSelected = func(i widget.ListItemID) {
		if i >= 0 && i < len(c.templates) {
			c.useTemplate(c.templates[i])
		}
		if c.menu != nil {
			c.menu.Hide()
		}
	}
	return heading, list
}

// showTemplates opens the picker below its button, or above it when the dock leaves no room.
func (c *createTab) showTemplates() {
	cnv := fyne.CurrentApp().Driver().CanvasForObject(c.picker)
	if cnv == nil || len(c.templates) == 0 {
		return
	}
	th := c.picker.Theme()
	heading, list := c.templateList()
	pad := th.Size(theme.SizeNamePadding)
	width := c.picker.Size().Width
	for _, t := range c.templates {
		width = fyne.Max(width, fyne.MeasureText(t.Name, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width+8*pad)
	}
	rowH := list.MinSize().Height
	height := fyne.Min(th.Size(sizePopupMaxList), float32(len(c.templates))*(rowH+th.Size(theme.SizeNameSeparatorThickness))+heading.MinSize().Height+4*pad)
	content := container.NewBorder(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, heading), nil, nil, nil, list)
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(c.picker)
	below := at.Y + c.picker.Size().Height
	pos := fyne.NewPos(at.X, below)
	if room := cnv.Size().Height - below - pad; room < height {
		if at.Y-pad > room {
			height = fyne.Min(height, at.Y-pad)
			pos.Y = at.Y - height
		} else {
			height = room
		}
	}
	c.menu = widget.NewPopUp(content, cnv)
	m := c.menu
	onShown(cnv, func() { m.ShowAtPosition(pos) }, m.Hide) // Escape closes it (escape.go)
	c.menu.Resize(fyne.NewSize(width, height))
}

// useTemplate replaces the text with a template's and sends it to the engine.
func (c *createTab) useTemplate(t engine.Template) {
	c.picker.SetText(t.Name)
	c.pane.view.SetText(t.Text)
	c.pane.dirty = true
	c.pane.push()
	c.results.Objects = nil
	c.results.Refresh()
	c.setStatus("", colorMuted)
}

// create creates every object of the text and lists what happened to each.
func (c *createTab) create(closeAfter bool) {
	if c.busy {
		return
	}
	text := c.pane.view.Text()
	c.pane.push()
	c.busy = true
	c.createBtn.Disable()
	c.closeBtn.Disable()
	c.setStatus("Creating…", colorMuted)
	c.sh.dispatch("object.create", createParams{YAML: text, Context: c.tab.Context}, func(res any, err error) {
		c.busy = false
		c.createBtn.Enable()
		c.closeBtn.Enable()
		var cr engine.CreateResult
		if res != nil {
			_ = decode(res, &cr) // a failed create may still report what it created
		}
		c.showResults(cr)
		switch {
		case errors.Is(err, engine.ErrDeclined):
			c.setStatus("Nothing created.", colorMuted)
		case err != nil && len(cr.Items) == 0:
			c.setStatus("Nothing created: "+errText(err), colorStatusError)
		case cr.Failed > 0:
			c.setStatus(fmt.Sprintf("Created %d of %d objects.", cr.Created, len(cr.Items)), colorStatusWarning)
		default:
			c.setStatus(fmt.Sprintf("Created %d object%s.", cr.Created, plural(cr.Created)), colorStatusOK)
			if closeAfter {
				c.sh.dispatch("dock.close", dockParams{ID: c.tab.ID}, nil)
			}
		}
	})
}

func (c *createTab) showResults(cr engine.CreateResult) {
	c.results.Objects = nil
	for i, it := range cr.Items {
		if i == maxCreateLines {
			c.results.Add(newTextLine(fmt.Sprintf("… and %d more", len(cr.Items)-i), colorMuted, theme.SizeNameText, fyne.TextStyle{}))
			break
		}
		what := it.Ref.Kind + " " + it.Ref.Key()
		line := newTextLine("✓ "+what+" created", colorStatusOK, theme.SizeNameText, fyne.TextStyle{})
		if !it.Created {
			line = newTextLine("✗ "+what+": "+it.Error, colorStatusError, theme.SizeNameText, fyne.TextStyle{})
		}
		line.truncate = true
		c.results.Add(line)
	}
	c.results.Refresh()
}

func (c *createTab) remote(rev uint64) { c.pane.remote(rev) }

func (c *createTab) close() { c.pane.close() }
