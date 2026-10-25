package fyneui

import (
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/yamlview"
	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// helmRevisionsTab is a release's Revisions dock tab (plan/helm-revisions.md, a prototype for the
// owner's design review; the owner picked the dock tab, his option A, on 2026-10-25): over the
// tab the revision shown and the one it is compared with, Values or Manifest, and the actions;
// on its left the release's revisions; on its right the revision's values (user-supplied or all)
// or its rendered manifest (its objects as kubectl lists them, or manifest.yaml as Helm stored
// it). Compared with another revision, the values are the keys that changed, their line diff
// folded under them, and the objects are marked + (only in the revision shown), − (only in the one
// compared), ~ (changed) or =, a changed one's YAML as its diff. Everything is read through
// helm.release.revision and helm.release.diff from the cache; what the tab shows is the engine's
// state (DockTab.HelmRevisions), changed through ui.helm.revisions.set, so agents see and drive it.
// A rendered Secret's values are masked until the user's Show, which is the window's alone.
type helmRevisionsTab struct {
	sh  *shell
	tab engine.DockTab
	st  engine.HelmRevisionsTab
	ref model.ObjectRef

	history []helm.HistoryEntry // newest first
	newest  int

	revPick, cmpPick *selectField
	revBox, cmpBox   *fyne.Container
	revLabels        map[string]int // a picker's option → its revision
	cmpLabels        map[string]int
	modeSeg          *twoWay
	rollback         *textButton
	copyBtn, saveBtn *tipButton
	described        *textLine // the shown revision's description
	revList          *widget.List
	status           *textLine

	// Values.
	valuesSeg   *twoWay
	valuesView  *yamlview.View
	valuesFind  *findBar
	keysHead    *textLine
	keyCols     *keyRow
	keys        []helm.ValueChange
	keysList    *widget.List
	lineDiffBtn *textButton
	valuesDiff  *diffView
	valuesBody  *fyne.Container

	// Manifest.
	manifestSeg *twoWay
	counts      *textLine
	objs        []helm.ObjectChange // the objects listed: the diff's, or the revision's marked "="
	objList     *widget.List
	objSource   *textLine
	objLive     *linkText
	objShow     *textButton
	objView     *yamlview.View
	objFind     *findBar
	objDiff     *diffView
	objBody     *fyne.Container
	rawView     *yamlview.View
	rawFind     *findBar
	rawShow     *textButton
	manifestTop *fyne.Container
	manifestBox *fyne.Container

	content *fyne.Container
	root    fyne.CanvasObject

	cur     helm.Revision       // the revision shown, as read
	diff    engine.RevisionDiff // its comparison, as read
	shown   map[string]bool     // the Secrets the user showed, by object key; rawKey for manifest.yaml
	syncing bool                // the pickers follow the engine: their changes are not sent back
	loads   int
	closed  bool
}

// rawKey stands for manifest.yaml among the Secrets shown.
const rawKey = "\x00manifest.yaml"

// The tab's widths: the revision list, and the objects' table beside the selected object.
const (
	revisionsListWidth float32 = 250
	objectsListWidth   float32 = 420
)

// helmRevisionsParams mirror ui.helm.revisions.set's parameters.
type helmRevisionsParams struct {
	ID       string  `json:"id"`
	Revision *int    `json:"revision,omitempty"`
	Compare  *int    `json:"compare,omitempty"`
	Mode     *string `json:"mode,omitempty"`
	All      *bool   `json:"all,omitempty"`
	Raw      *bool   `json:"raw,omitempty"`
	Object   *string `json:"object,omitempty"`
	LineDiff *bool   `json:"lineDiff,omitempty"`
}

// The read parameters: helm.release.revision's and helm.release.diff's.
type (
	revisionReadParams struct {
		refParams
		Revision int  `json:"revision,omitempty"`
		All      bool `json:"all,omitempty"`
	}
	revisionDiffParams struct {
		refParams
		From   int    `json:"from,omitempty"`
		To     int    `json:"to,omitempty"`
		What   string `json:"what,omitempty"`
		Object string `json:"object,omitempty"`
		Masked bool   `json:"masked,omitempty"`
	}
)

func newHelmRevisionsTab(sh *shell, t engine.DockTab) *helmRevisionsTab {
	r := &helmRevisionsTab{sh: sh, tab: t, shown: map[string]bool{}}
	if t.Ref != nil {
		r.ref = *t.Ref
	}
	if t.HelmRevisions != nil {
		r.st = *t.HelmRevisions
	}

	r.revPick = newSelectField(nil, nil)
	r.revPick.OnChanged = func(label string) {
		if n, ok := r.revLabels[label]; ok && !r.syncing && n != r.st.Revision {
			r.set(helmRevisionsParams{Revision: &n})
		}
	}
	r.cmpPick = newSelectField(nil, nil)
	r.cmpPick.OnChanged = func(label string) {
		if n, ok := r.cmpLabels[label]; ok && !r.syncing && n != r.st.Compare {
			r.set(helmRevisionsParams{Compare: &n})
		}
	}
	r.modeSeg = newTwoWay(sh, "Values", "Manifest", func(i int) {
		m := []string{engine.RevisionsValues, engine.RevisionsManifest}[i]
		r.set(helmRevisionsParams{Mode: &m})
	})
	r.rollback = newTextButton("Rollback", theme.HistoryIcon(), r.rollBack)
	r.copyBtn = newTipButton(sh, theme.ContentCopyIcon(), "Copy what the tab shows", r.copyShown)
	r.saveBtn = newTipButton(sh, theme.DocumentSaveIcon(), "Save as…", r.saveShown)
	r.described = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	r.described.truncate = true
	r.status = newTextLine("Loading…", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	r.status.truncate = true

	r.revList = widget.NewList(
		func() int { return len(r.history) },
		func() fyne.CanvasObject { return newRevisionRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= 0 && i < len(r.history) {
				o.(*revisionRow).set(r.history[i], r.history[i].Revision == r.st.Revision, r.history[i].Revision == r.st.Compare, r.history[i].Revision == r.newest, r.sh.timeFormat())
			}
		})
	r.revList.HideSeparators = true
	r.revList.OnSelected = func(i widget.ListItemID) {
		r.revList.UnselectAll()
		if i >= 0 && i < len(r.history) && r.history[i].Revision != r.st.Revision {
			n := r.history[i].Revision
			r.set(helmRevisionsParams{Revision: &n})
		}
	}

	r.buildValues()
	r.buildManifest()

	r.revBox, r.cmpBox = box(fitSelect{}, r.revPick), box(fitSelect{}, r.cmpPick)
	pickers := container.NewHBox(
		centred(newTextLine("Revision", colorMuted, theme.SizeNameText, fyne.TextStyle{})), r.revBox,
		gapOf(8),
		centred(newTextLine("Compare with", colorMuted, theme.SizeNameText, fyne.TextStyle{})), r.cmpBox,
		gapOf(16), r.modeSeg.root)
	actions := container.NewHBox(r.rollback, r.copyBtn, r.saveBtn)
	headLine := container.NewBorder(nil, nil, pickers, actions)
	header := container.NewStack(newSurface(colorDock), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewVBox(headLine, r.described)))
	footer := container.NewStack(newSurface(colorDock), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, r.status))
	r.content = container.NewStack()
	left := box(widthOf{revisionsListWidth}, container.NewBorder(
		inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, newTextLine("Revisions", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})),
		nil, nil, nil, r.revList))
	body := container.NewBorder(nil, nil, container.NewHBox(left, newDivider(true)), nil, r.content)
	r.root = container.NewBorder(container.NewVBox(header, newDivider(false)), container.NewVBox(newDivider(false), footer), nil, nil, body)
	r.loadHistory()
	return r
}

// buildValues makes the Values mode: User-supplied or All values over the values, or, compared,
// the changed keys and the line diff folded under them.
func (r *helmRevisionsTab) buildValues() {
	r.valuesSeg = newTwoWay(r.sh, "User-supplied", "All values", func(i int) {
		all := i == 1
		r.set(helmRevisionsParams{All: &all})
	})
	r.valuesView = readOnlyYAML()
	r.valuesFind = newViewerFindBar(r.sh, r.valuesView)
	r.keysHead = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	r.keyCols = newKeyRow()
	r.keyCols.header()
	r.keysList = widget.NewList(
		func() int { return len(r.keys) },
		func() fyne.CanvasObject { return newKeyRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= 0 && i < len(r.keys) {
				o.(*keyRow).set(r.keys[i])
			}
		})
	r.keysList.HideSeparators = true
	r.lineDiffBtn = newTextButton("Show the line diff", theme.MenuDropDownIcon(), func() {
		on := !r.st.LineDiff
		r.set(helmRevisionsParams{LineDiff: &on})
	})
	r.valuesDiff = newDiffView()
	r.valuesBody = container.NewStack()
}

// buildManifest makes the Manifest mode: the objects or manifest.yaml.
func (r *helmRevisionsTab) buildManifest() {
	r.manifestSeg = newTwoWay(r.sh, "Objects", "manifest.yaml", func(i int) {
		raw := i == 1
		r.set(helmRevisionsParams{Raw: &raw})
	})
	r.counts = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	r.objList = widget.NewList(
		func() int { return len(r.objs) },
		func() fyne.CanvasObject { return newObjectRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= 0 && i < len(r.objs) {
				o.(*objectRow).set(r.objs[i], r.objs[i].Key() == r.selectedKey(), r.st.Compare != 0)
			}
		})
	r.objList.HideSeparators = true
	r.objList.OnSelected = func(i widget.ListItemID) {
		r.objList.UnselectAll()
		if i >= 0 && i < len(r.objs) {
			k := r.objs[i].Key()
			if k != r.selectedKey() {
				r.set(helmRevisionsParams{Object: &k})
			}
		}
	}
	r.objSource = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	r.objSource.truncate = true
	r.objLive = newLinkText("Show live object", r.showLive)
	r.objShow = newTextButton(showLabel, theme.VisibilityIcon(), func() { r.toggleShown(r.selectedKey()) })
	r.objShow.setTone(colorStatusWarning)
	r.objView = readOnlyYAML()
	r.objFind = newViewerFindBar(r.sh, r.objView)
	r.objDiff = newDiffView()
	r.objBody = container.NewStack()
	r.rawView = readOnlyYAML()
	r.rawFind = newViewerFindBar(r.sh, r.rawView)
	r.rawShow = newTextButton("Show Secrets", theme.VisibilityIcon(), func() { r.toggleShown(rawKey) })
	r.rawShow.setTone(colorStatusWarning)
	r.manifestTop = container.NewHBox()
	r.manifestBox = container.NewStack()
}

// readOnlyYAML is a YAML view the user reads and selects in, with line numbers.
func readOnlyYAML() *yamlview.View {
	v := yamlview.New()
	v.SetReadOnly(true)
	return v
}

// loadHistory reads the release's revisions (helm.release.history), then what the tab shows.
func (r *helmRevisionsTab) loadHistory() {
	r.sh.dispatch("helm.release.history", refParams{Ref: r.ref}, func(res any, err error) {
		if r.closed {
			return
		}
		var h struct {
			Revision int                 `json:"revision"`
			History  []helm.HistoryEntry `json:"history"`
		}
		if err == nil {
			err = decode(res, &h)
		}
		if err != nil {
			r.setStatus("Could not read the release's history: "+errText(err), colorStatusError)
			return
		}
		r.history, r.newest = h.History, h.Revision
		r.showPickers()
		r.revList.Refresh()
		r.load()
	})
}

// update follows the engine's tab: another revision, comparison or mode.
func (r *helmRevisionsTab) update(t engine.DockTab) {
	r.tab = t
	if t.HelmRevisions == nil || *t.HelmRevisions == r.st {
		return
	}
	old := r.st
	r.st = *t.HelmRevisions
	r.showPickers()
	r.revList.Refresh()
	if old.Revision != r.st.Revision || old.Compare != r.st.Compare || old.All != r.st.All || old.Mode != r.st.Mode ||
		(r.st.Mode == engine.RevisionsManifest && old.Object != r.st.Object) {
		r.load()
		return
	}
	r.render()
}

func (r *helmRevisionsTab) remote(uint64) {}

func (r *helmRevisionsTab) close() { r.closed = true }

// set sends a change of the tab's state to the engine; the tab follows its answer (update).
func (r *helmRevisionsTab) set(p helmRevisionsParams) {
	p.ID = r.tab.ID
	r.sh.dispatch("ui.helm.revisions.set", p, nil)
}

// showPickers puts the engine's state into the pickers and the segments.
func (r *helmRevisionsTab) showPickers() {
	r.syncing = true
	defer func() { r.syncing = false }()
	r.revLabels, r.cmpLabels = map[string]int{}, map[string]int{}
	none := "None"
	revs, cmps := make([]string, 0, len(r.history)), make([]string, 0, len(r.history)+2)
	cmps = append(cmps, none)
	r.cmpLabels[none] = 0
	before := 0
	for _, e := range r.history {
		label := revisionLabel(e, e.Revision == r.newest)
		revs = append(revs, label)
		r.revLabels[label] = e.Revision
		if e.Revision < r.st.Revision && before == 0 {
			before = e.Revision
		}
	}
	if before != 0 { // the quick pick: what this revision changed
		label := "The revision before (" + strconv.Itoa(before) + ")"
		cmps = append(cmps, label)
		r.cmpLabels[label] = before
	}
	for _, e := range r.history {
		if e.Revision == r.st.Revision {
			continue
		}
		label := revisionLabel(e, e.Revision == r.newest)
		cmps = append(cmps, label)
		r.cmpLabels[label] = e.Revision
	}
	r.revPick.Options, r.cmpPick.Options = revs, cmps
	r.revBox.Layout, r.cmpBox.Layout = fitSelect{options: revs}, fitSelect{options: cmps}
	for label, n := range r.revLabels {
		if n == r.st.Revision {
			r.revPick.SetSelected(label)
		}
	}
	selected := none
	for _, label := range cmps {
		if r.cmpLabels[label] == r.st.Compare && (r.st.Compare == 0 || !strings.HasPrefix(label, "The revision before")) {
			selected = label
		}
	}
	r.cmpPick.SetSelected(selected)
	r.revPick.Refresh()
	r.cmpPick.Refresh()
	r.revBox.Refresh()
	r.cmpBox.Refresh()
	r.modeSeg.choose(map[bool]int{false: 0, true: 1}[r.st.Mode == engine.RevisionsManifest])
	r.valuesSeg.choose(map[bool]int{false: 0, true: 1}[r.st.All])
	r.manifestSeg.choose(map[bool]int{false: 0, true: 1}[r.st.Raw])
	r.rollback.SetText("Rollback to " + strconv.Itoa(r.st.Revision))
	if r.st.Revision == r.newest || r.st.Revision == 0 {
		r.rollback.Disable()
	} else {
		r.rollback.Enable()
	}
	for _, e := range r.history {
		if e.Revision == r.st.Revision {
			r.described.SetText(e.Description)
			r.described.SetColor(map[bool]fyne.ThemeColorName{true: colorStatusError, false: colorMuted}[helm.StatusToken(e.Status) == model.StatusError])
		}
	}
}

// revisionLabel is a revision as the pickers name it: "9 · Failed · shop-web-1.5.0", the newest
// marked "(current)".
func revisionLabel(e helm.HistoryEntry, newest bool) string {
	s := strconv.Itoa(e.Revision) + " · " + helm.StatusText(e.Status) + " · " + e.Chart + "-" + e.ChartVersion
	if newest {
		s += " (current)"
	}
	return s
}

// load reads the revision shown and, compared, its comparison; an older read's answer is dropped.
func (r *helmRevisionsTab) load() {
	r.loads++
	n, st := r.loads, r.st
	r.setStatus("Reading revision "+strconv.Itoa(st.Revision)+"…", colorMuted)
	r.sh.dispatch("helm.release.revision", revisionReadParams{refParams: refParams{Ref: r.ref}, Revision: st.Revision, All: st.All}, func(res any, err error) {
		if r.closed || n != r.loads {
			return
		}
		var rev helm.Revision
		if err == nil {
			err = decode(res, &rev)
		}
		if err != nil {
			r.setStatus("Could not read revision "+strconv.Itoa(st.Revision)+": "+errText(err), colorStatusError)
			return
		}
		r.cur = rev
		if st.Compare == 0 {
			r.diff = engine.RevisionDiff{}
			r.render()
			return
		}
		what := engine.DiffValues
		switch {
		case st.Mode == engine.RevisionsManifest:
			what = engine.DiffManifest
		case st.All:
			what = engine.DiffAllValues
		}
		key := r.selectedKey()
		p := revisionDiffParams{refParams: refParams{Ref: r.ref}, From: st.Compare, To: st.Revision, What: what, Object: key, Masked: !r.shown[key]}
		r.sh.dispatch("helm.release.diff", p, func(res any, err error) {
			if r.closed || n != r.loads {
				return
			}
			var d engine.RevisionDiff
			if err == nil {
				err = decode(res, &d)
			}
			if err != nil {
				r.setStatus("Could not compare with revision "+strconv.Itoa(st.Compare)+": "+errText(err), colorStatusError)
				return
			}
			r.diff = d
			r.render()
		})
	})
}

// selectedKey is the object selected: the tab's, or the first of the list.
func (r *helmRevisionsTab) selectedKey() string {
	if r.st.Object != "" {
		return r.st.Object
	}
	if r.st.Compare != 0 && r.diff.What == engine.DiffManifest && len(r.diff.Objects) > 0 {
		return r.diff.Objects[0].Key()
	}
	if len(r.cur.Objects) > 0 {
		return r.cur.Objects[0].Key()
	}
	return ""
}

// render shows what was read in the tab's mode.
func (r *helmRevisionsTab) render() {
	if r.st.Mode == engine.RevisionsManifest {
		r.renderManifest()
	} else {
		r.renderValues()
	}
	r.revList.Refresh()
}

func (r *helmRevisionsTab) renderValues() {
	top := container.NewHBox(r.valuesSeg.root)
	if r.st.Compare == 0 || r.diff.What == engine.DiffManifest {
		text := r.cur.Values
		if text == "" {
			text = "# no values\n"
		}
		r.valuesView.SetText(text)
		r.valuesFind.textChanged()
		r.valuesBody.Objects = []fyne.CanvasObject{container.NewBorder(r.valuesFind.root, nil, nil, nil, yamlWell(r.valuesView))}
		kind := "user-supplied values"
		if r.st.All {
			kind = "values, the chart's defaults included"
		}
		r.setStatus(fmt.Sprintf("Revision %d: its %s, as helm get values%s --revision %d shows them.", r.cur.Revision, kind, map[bool]string{true: " --all", false: ""}[r.st.All], r.cur.Revision), colorMuted)
	} else {
		r.keys = r.diff.Changes
		switch n := len(r.keys); n {
		case 0:
			r.keysHead.SetText(fmt.Sprintf("The values of revision %d and revision %d are the same", r.diff.From, r.diff.To))
		case 1:
			r.keysHead.SetText(fmt.Sprintf("1 key differs between revision %d and revision %d", r.diff.From, r.diff.To))
		default:
			r.keysHead.SetText(fmt.Sprintf("%d keys differ between revision %d and revision %d", n, r.diff.From, r.diff.To))
		}
		r.keyCols.titles(r.diff.From, r.diff.To)
		r.keysList.Refresh()
		r.valuesDiff.set(r.diff.Lines)
		parts := []fyne.CanvasObject{r.keysHead}
		if len(r.keys) > 0 {
			parts = append(parts, r.keyCols)
		}
		head := container.NewVBox(parts...)
		if r.st.LineDiff {
			r.lineDiffBtn.SetText("Hide the line diff")
			r.lineDiffBtn.SetIcon(theme.MenuDropUpIcon())
			keysBox := box(heightOf{keysHeight(len(r.keys))}, r.keysList)
			r.valuesBody.Objects = []fyne.CanvasObject{container.NewBorder(container.NewVBox(head, keysBox, container.NewHBox(r.lineDiffBtn), newDivider(false)), nil, nil, nil, r.valuesDiff.list)}
		} else {
			r.lineDiffBtn.SetText("Show the line diff")
			r.lineDiffBtn.SetIcon(theme.MenuDropDownIcon())
			r.valuesBody.Objects = []fyne.CanvasObject{container.NewBorder(head, container.NewHBox(r.lineDiffBtn), nil, nil, r.keysList)}
		}
		if len(r.keys) == 0 {
			r.valuesBody.Objects = []fyne.CanvasObject{container.NewVBox(head)}
		}
		r.setStatus(fmt.Sprintf("From revision %d (compared) to revision %d (shown): what rolling back to revision %d would change in the values.", r.diff.From, r.diff.To, r.diff.To), colorMuted)
		if r.diff.To > r.diff.From {
			r.setStatus(fmt.Sprintf("From revision %d (compared) to revision %d (shown): what revision %d changed in the values.", r.diff.From, r.diff.To, r.diff.To), colorMuted)
		}
	}
	r.valuesBody.Refresh()
	r.content.Objects = []fyne.CanvasObject{inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, container.NewBorder(top, nil, nil, nil, r.valuesBody))}
	r.content.Refresh()
}

// keysHeight is the changed keys' list's height above an unfolded line diff: all of them, at most
// eight rows.
func keysHeight(n int) float32 {
	return float32(min(max(n, 1), 8)) * (theme.TextSize() + 2*theme.InnerPadding() + 2)
}

func (r *helmRevisionsTab) renderManifest() {
	compared := r.st.Compare != 0 && r.diff.What == engine.DiffManifest
	r.objs = r.objs[:0]
	if compared {
		r.objs = append(r.objs, r.diff.Objects...)
		r.counts.SetText(fmt.Sprintf("%d changed · %d added · %d removed · %d the same, against revision %d", r.diff.Changed, r.diff.Added, r.diff.Removed, r.diff.Same, r.diff.From))
	} else {
		for _, o := range r.cur.Objects {
			r.objs = append(r.objs, helm.ObjectChange{ManifestObject: o, Change: helm.ObjectSame})
		}
		r.counts.SetText(fmt.Sprintf("%d objects", len(r.objs)))
	}
	r.objList.Refresh()
	r.manifestTop.Objects = []fyne.CanvasObject{r.manifestSeg.root, gapOf(12), centred(r.counts)}
	if r.st.Raw {
		r.manifestTop.Objects = []fyne.CanvasObject{r.manifestSeg.root}
		r.renderRaw()
	} else {
		r.renderObject(compared)
		list := box(widthOf{objectsListWidth}, container.NewBorder(objectsHeader(compared), nil, nil, nil, r.objList))
		r.manifestBox.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil, container.NewHBox(list, newDivider(true)), nil, r.objBody)}
	}
	r.manifestTop.Refresh()
	r.manifestBox.Refresh()
	r.content.Objects = []fyne.CanvasObject{inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, container.NewBorder(r.manifestTop, nil, nil, nil, r.manifestBox))}
	r.content.Refresh()
}

// renderObject shows the selected object: its template, and its YAML under it or, changed or
// removed against the revision compared, its diff; a Secret masked until Show.
func (r *helmRevisionsTab) renderObject(compared bool) {
	key := r.selectedKey()
	var obj *helm.ObjectChange
	for i := range r.objs {
		if r.objs[i].Key() == key {
			obj = &r.objs[i]
		}
	}
	if obj == nil {
		r.objBody.Objects = []fyne.CanvasObject{centred(newTextLine("No object selected", colorMuted, theme.SizeNameText, fyne.TextStyle{}))}
		r.objBody.Refresh()
		return
	}
	shown := r.shown[key]
	r.objSource.SetText(obj.Kind + " " + obj.Name)
	tools := []fyne.CanvasObject{}
	if obj.Change != helm.ObjectRemoved && model.RefTo(obj.APIVersion, obj.Kind, obj.Namespace, obj.Name) != nil {
		tools = append(tools, centred(fitLink(r.objLive)))
	}
	if obj.Secret {
		r.objShow.SetText(map[bool]string{true: hideLabel, false: showLabel}[shown])
		r.objShow.SetIcon(map[bool]fyne.Resource{true: theme.VisibilityOffIcon(), false: theme.VisibilityIcon()}[shown])
		tools = append(tools, r.objShow)
	}
	head := container.NewBorder(nil, nil, nil, container.NewHBox(tools...), r.objSource)
	var view fyne.CanvasObject
	switch {
	case compared && (obj.Change == helm.ObjectChanged || obj.Change == helm.ObjectRemoved):
		r.objDiff.set(r.diff.Lines)
		view = container.NewStack(newSurface(theme.ColorNameInputBackground), r.objDiff.list)
		r.setStatus(changeText(obj, r.diff.From, r.diff.To), colorMuted)
		if obj.Secret && !shown && !engine.DiffChanged(r.diff.Lines) { // masked, its values' sizes alike
			view = container.NewStack(newSurface(theme.ColorNameInputBackground), centred(newTextLine(
				"Its values differ, their sizes do not: Show compares them.", colorStatusWarning, theme.SizeNameText, fyne.TextStyle{})))
		}
	default:
		yamlText := r.objectYAML(key)
		if obj.Secret && !shown {
			yamlText = helm.MaskSecretYAML(yamlText, helm.MaskSize)
		}
		r.objView.SetText("# Source: " + obj.Source + "\n" + yamlText)
		r.objFind.textChanged()
		view = container.NewBorder(r.objFind.root, nil, nil, nil, yamlWell(r.objView))
		if compared {
			r.setStatus(changeText(obj, r.diff.From, r.diff.To), colorMuted)
		} else {
			r.setStatus(fmt.Sprintf("Revision %d: %s %s as Helm rendered it (helm get manifest --revision %d).", r.cur.Revision, obj.Kind, obj.Name, r.cur.Revision), colorMuted)
		}
	}
	r.objBody.Objects = []fyne.CanvasObject{container.NewBorder(head, nil, nil, nil, view)}
	r.objBody.Refresh()
}

// objectYAML is an object's YAML in the revision shown.
func (r *helmRevisionsTab) objectYAML(key string) string {
	for _, o := range r.cur.Objects {
		if o.Key() == key {
			return o.YAML
		}
	}
	return ""
}

// changeText says what the comparison found for an object.
func changeText(o *helm.ObjectChange, from, to int) string {
	what := o.Kind + " " + o.Name
	switch o.Change {
	case helm.ObjectAdded:
		return fmt.Sprintf("%s: in revision %d, not in revision %d.", what, to, from)
	case helm.ObjectRemoved:
		return fmt.Sprintf("%s: in revision %d, not in revision %d; shown as removed.", what, from, to)
	case helm.ObjectChanged:
		return fmt.Sprintf("%s: changed from revision %d (−) to revision %d (+).", what, from, to)
	}
	return fmt.Sprintf("%s: the same in revision %d and revision %d.", what, from, to)
}

// renderRaw shows manifest.yaml as Helm stored it, its Secrets masked until Show.
func (r *helmRevisionsTab) renderRaw() {
	text := r.cur.Manifest
	hasSecret := slices.ContainsFunc(r.cur.Objects, func(o helm.ManifestObject) bool { return o.Secret })
	if hasSecret && !r.shown[rawKey] {
		text = helm.MaskManifest(text, helm.MaskSize)
	}
	r.rawView.SetText(strings.TrimPrefix(text, "\n"))
	r.rawFind.textChanged()
	top := r.rawFind.root
	if hasSecret {
		r.rawShow.SetText(map[bool]string{true: "Hide Secrets", false: "Show Secrets"}[r.shown[rawKey]])
		top = container.NewBorder(nil, nil, nil, r.rawShow, r.rawFind.root)
	}
	r.manifestBox.Objects = []fyne.CanvasObject{container.NewBorder(top, nil, nil, nil, yamlWell(r.rawView))}
	r.setStatus(fmt.Sprintf("Revision %d: the manifest as Helm stored it (helm get manifest --revision %d), %d documents.", r.cur.Revision, r.cur.Revision, len(r.cur.Objects)), colorMuted)
}

// yamlWell is a YAML view on the input background.
func yamlWell(v *yamlview.View) fyne.CanvasObject {
	return container.NewStack(newSurface(theme.ColorNameInputBackground), v)
}

// toggleShown shows a Secret's values in the window, or masks them again: the window's alone.
func (r *helmRevisionsTab) toggleShown(key string) {
	r.shown[key] = !r.shown[key]
	if key != rawKey && r.st.Compare != 0 {
		r.load() // its diff, masked or not
		return
	}
	r.render()
}

// showLive goes to the object the cluster runs, as a drawer's link does.
func (r *helmRevisionsTab) showLive() {
	for _, o := range r.objs {
		if o.Key() == r.selectedKey() {
			if ref := model.RefTo(o.APIVersion, o.Kind, o.Namespace, o.Name); ref != nil {
				r.sh.followLink(*ref, r.ref.Context, false)
			}
		}
	}
}

// rollBack rolls the release back to the revision shown: the engine's command and its confirmation.
func (r *helmRevisionsTab) rollBack() {
	r.sh.dispatch(rollbackCommand, struct {
		Ref      model.ObjectRef `json:"ref"`
		Revision int             `json:"revision"`
	}{r.ref, r.st.Revision}, nil)
}

// shownText is what the tab shows as text, and a file name for it.
func (r *helmRevisionsTab) shownText() (string, string) {
	base := fmt.Sprintf("%s-revision-%d", r.ref.Name, r.st.Revision)
	switch {
	case r.st.Mode != engine.RevisionsManifest:
		if r.st.All {
			return r.cur.Values, base + "-all-values.yaml"
		}
		return r.cur.Values, base + "-values.yaml"
	case r.st.Raw:
		return r.rawView.Text(), base + "-manifest.yaml"
	}
	return r.objView.Text(), base + "-" + strings.ToLower(strings.ReplaceAll(r.selectedKey(), "/", "-")) + ".yaml"
}

// copyShown copies what the tab shows: the values, the object or manifest.yaml, a Secret as shown.
func (r *helmRevisionsTab) copyShown() {
	text, _ := r.shownText()
	r.sh.copyText(text, false)
	r.sh.toast(engine.LevelInfo, "Copied")
}

// saveShown saves what the tab shows into the file the user picks in the system's save panel.
func (r *helmRevisionsTab) saveShown() {
	text, name := r.shownText()
	if !r.sh.files.save(nativedialog.SaveOptions{Title: "Save as", Name: name, Types: yamlExtensions}, func(path string) {
		r.sh.writePicked(path, []byte(text), func(err error) {
			if err != nil {
				r.setStatus("Not saved: "+err.Error(), colorStatusError)
				return
			}
			r.setStatus("Saved to "+path+".", colorStatusOK)
		})
	}) {
		r.setStatus("Save as… needs the system's save panel, which this prototype uses only on macOS.", colorStatusWarning)
	}
}

func (r *helmRevisionsTab) setStatus(text string, c fyne.ThemeColorName) {
	r.status.SetText(text)
	r.status.SetColor(c)
}

// objectsHeader titles the objects' table as kubectl get titles its columns.
func objectsHeader(compared bool) fyne.CanvasObject {
	h := newObjectRow()
	h.header(compared)
	return h
}

// twoWay is the Overview's segmented control (instrument_segment.go) over two words, one chosen
// as the engine's state says.
type twoWay struct {
	seg     *segmented
	buttons [2]*tipButton
	chosen  int
	root    fyne.CanvasObject
}

func newTwoWay(sh *shell, a, b string, chose func(int)) *twoWay {
	t := &twoWay{chosen: -1}
	for i, l := range []string{a, b} {
		t.buttons[i] = newTipButton(sh, nil, "", func() {
			if i != t.chosen {
				chose(i)
			}
		})
		t.buttons[i].Text = l
	}
	t.seg = newSegmented(sh, t.buttons[0], t.buttons[1])
	t.root = t.seg.root
	t.choose(0)
	return t
}

// choose marks segment i chosen.
func (t *twoWay) choose(i int) {
	if i == t.chosen {
		return
	}
	t.chosen = i
	for j, b := range t.buttons {
		b.Importance = widget.MediumImportance
		if j == i {
			b.Importance = widget.HighImportance
		}
		b.Refresh()
	}
	t.seg.choose(i)
}

// widthOf gives its content a fixed width and the height it is given.
type widthOf struct{ width float32 }

func (l widthOf) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(l.width, h)
}

func (l widthOf) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
}

// heightOf gives its content a fixed height and the width it is given.
type heightOf struct{ height float32 }

func (l heightOf) MinSize(objs []fyne.CanvasObject) fyne.Size {
	w := float32(0)
	for _, o := range objs {
		w = max(w, o.MinSize().Width)
	}
	return fyne.NewSize(w, l.height)
}

func (l heightOf) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
}

func centred(o fyne.CanvasObject) fyne.CanvasObject { return container.NewCenter(o) }

// revisionRow is one revision in the tab's list: its number, status and chart, the marks of the
// revision shown (●) and compared (◆), and under them when it was deployed and its description.
type revisionRow struct {
	widget.BaseWidget
	bg            *canvas.Rectangle
	num, status   *textLine
	chart, mark   *textLine
	when          *textLine
	selectedShown bool
}

func newRevisionRow() *revisionRow {
	r := &revisionRow{
		bg:     canvas.NewRectangle(color.Transparent),
		num:    newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}),
		status: newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}),
		chart:  newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{}),
		mark:   newTextLine("", colorAccent, theme.SizeNameText, fyne.TextStyle{Bold: true}),
		when:   newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}),
	}
	r.chart.truncate, r.when.truncate = true, true
	r.ExtendBaseWidget(r)
	return r
}

func (r *revisionRow) set(e helm.HistoryEntry, shown, compared, newest bool, tf model.TimeFormat) {
	r.num.SetText(strconv.Itoa(e.Revision))
	r.status.SetText(helm.StatusText(e.Status))
	r.status.SetColor(statusColorName(helm.StatusToken(e.Status)))
	r.chart.SetText(e.ChartVersion)
	mark := ""
	switch {
	case shown:
		mark = "●"
	case compared:
		mark = "◆"
	}
	r.mark.SetText(mark)
	when := tf.Format(e.Updated)
	if newest {
		when = "current · " + when
	}
	r.when.SetText(when)
	r.selectedShown = shown
	if shown {
		r.bg.FillColor = themeColor(r.Theme(), colorRowSelected)
	} else {
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
}

func (r *revisionRow) CreateRenderer() fyne.WidgetRenderer {
	top := container.NewBorder(nil, nil, container.NewHBox(box(widthOf{28}, r.num), box(widthOf{86}, r.status)), r.mark, r.chart)
	content := inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, container.NewVBox(top, r.when))
	return widget.NewSimpleRenderer(container.NewStack(r.bg, content))
}

// keyRow is one changed value: its key path, its value in the revision compared and in the one
// shown.
type keyRow struct {
	widget.BaseWidget
	path, from, arrow, to *textLine
}

func newKeyRow() *keyRow {
	r := &keyRow{
		path:  newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Monospace: true}),
		from:  newTextLine("", colorStatusError, theme.SizeNameText, fyne.TextStyle{Monospace: true}),
		arrow: newTextLine("→", colorMuted, theme.SizeNameText, fyne.TextStyle{}),
		to:    newTextLine("", colorStatusOK, theme.SizeNameText, fyne.TextStyle{Monospace: true}),
	}
	r.path.truncate, r.from.truncate, r.to.truncate = true, true, true
	r.ExtendBaseWidget(r)
	return r
}

// header makes the row the list's column titles.
func (r *keyRow) header() {
	for _, t := range []*textLine{r.path, r.from, r.arrow, r.to} {
		t.color, t.size, t.style = colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true}
		t.Refresh()
	}
	r.arrow.SetText("")
}

// titles names the columns by the revisions compared.
func (r *keyRow) titles(from, to int) {
	r.path.SetText("KEY")
	r.from.SetText(fmt.Sprintf("REVISION %d (COMPARED)", from))
	r.to.SetText(fmt.Sprintf("REVISION %d (SHOWN)", to))
}

func (r *keyRow) set(c helm.ValueChange) {
	r.path.SetText(c.Path)
	r.from.SetText(orNotSet(c.From))
	r.to.SetText(orNotSet(c.To))
}

// orNotSet is a value, or a key's absence on that side.
func orNotSet(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func (r *keyRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(inset(theme.SizeNameInnerPadding, theme.SizeNameLineSpacing,
		container.New(&keyRowLayout{}, r.path, r.from, r.arrow, r.to)))
}

// keyRowLayout gives the key path a third of the width and each value the same share of the rest.
type keyRowLayout struct{}

func (keyRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(200, h)
}

func (keyRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	arrow := objs[2].MinSize().Width + 2*theme.Padding()
	pathW := size.Width * 0.32
	valW := (size.Width - pathW - arrow) / 2
	xs := []float32{0, pathW, pathW + valW, pathW + valW + arrow}
	ws := []float32{pathW - theme.Padding(), valW, arrow, valW}
	for i, o := range objs {
		o.Resize(fyne.NewSize(ws[i], size.Height))
		o.Move(fyne.NewPos(xs[i], 0))
		if i == 2 {
			o.Move(fyne.NewPos(xs[i]+theme.Padding(), 0))
		}
	}
}

// objectRow is one object of a manifest in the tab's table: Kind, Name, Namespace and, compared,
// its mark.
type objectRow struct {
	widget.BaseWidget
	bg                    *canvas.Rectangle
	kind, name, ns, delta *textLine
}

func newObjectRow() *objectRow {
	r := &objectRow{
		bg:    canvas.NewRectangle(color.Transparent),
		kind:  newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}),
		name:  newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}),
		ns:    newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{}),
		delta: newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{Bold: true, Monospace: true}),
	}
	r.kind.truncate, r.name.truncate, r.ns.truncate = true, true, true
	r.ExtendBaseWidget(r)
	return r
}

// header makes the row the table's column titles.
func (r *objectRow) header(compared bool) {
	for _, t := range []*textLine{r.kind, r.name, r.ns, r.delta} {
		t.color, t.size, t.style = colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true}
		t.Refresh()
	}
	r.kind.SetText("KIND")
	r.name.SetText("NAME")
	r.ns.SetText("NAMESPACE")
	if compared {
		r.delta.SetText("Δ")
	}
}

func (r *objectRow) set(c helm.ObjectChange, selected, compared bool) {
	r.kind.SetText(c.Kind)
	r.name.SetText(c.Name)
	r.ns.SetText(c.Namespace)
	r.delta.SetText("")
	if compared {
		r.delta.SetText(map[string]string{helm.ObjectRemoved: "−"}[c.Change] + strings.TrimPrefix(c.Change, helm.ObjectRemoved))
		r.delta.SetColor(map[string]fyne.ThemeColorName{
			helm.ObjectAdded: colorStatusOK, helm.ObjectRemoved: colorStatusError, helm.ObjectChanged: colorStatusWarning, helm.ObjectSame: colorMuted,
		}[c.Change])
	}
	nameColor := theme.ColorNameForeground
	if c.Change == helm.ObjectRemoved {
		nameColor = colorMuted
	}
	r.name.SetColor(nameColor)
	r.kind.SetColor(nameColor)
	if selected {
		r.bg.FillColor = themeColor(r.Theme(), colorRowSelected)
	} else {
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
}

func (r *objectRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(r.bg, inset(theme.SizeNameInnerPadding, theme.SizeNameLineSpacing,
		container.New(&objectRowLayout{}, r.kind, r.name, r.ns, r.delta))))
}

// objectRowLayout: Kind 30 %, Name the rest, Namespace 24 %, the mark a column of its own.
type objectRowLayout struct{}

func (objectRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(200, h)
}

func (objectRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	mark := float32(22)
	rest := size.Width - mark
	ws := []float32{rest * 0.3, rest * 0.46, rest * 0.24, mark}
	x := float32(0)
	for i, o := range objs {
		o.Resize(fyne.NewSize(ws[i]-theme.Padding(), size.Height))
		o.Move(fyne.NewPos(x, 0))
		x += ws[i]
	}
}

// linkFit gives a link its text's width: a link asks for none, as a truncating line does, which a
// row of buttons would squeeze to nothing.
type linkFit struct{ l *linkText }

func fitLink(l *linkText) fyne.CanvasObject { return box(linkFit{l}, l) }

func (f linkFit) MinSize([]fyne.CanvasObject) fyne.Size {
	s := fyne.MeasureText(f.l.text, f.l.Theme().Size(f.l.size), fyne.TextStyle{})
	return fyne.NewSize(s.Width+1, s.Height)
}

func (f linkFit) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}
