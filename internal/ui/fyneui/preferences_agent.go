package fyneui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › Agent (D-10; engine/chat.go): the built-in agent's LLM connections. The list shows
// each connection's name, kind, host and model, which one is the default and which are private;
// a connection is added, edited, made the default or deleted here. The form: name, kind (which
// prefills LM Studio's usual base URL), base URL, API key (kept in an owner-only file in Cogpit's
// settings folder, never in the keychain (the owner, 2026-10-15): empty keeps the stored key,
// "Remove key" removes it, and empty saves no key where none is stored), model (the endpoint's
// list, any name may be typed), the private connection's mark with what it unlocks and the hint
// that its conversations are not saved (H-96), whether to send all tools (for large cloud models,
// what it costs said), the four limits of a turn with their defaults, and
// "use as default"; Test connection checks the form's values layer by layer (TCP, HTTP and TLS,
// authentication, tool use) and shows each layer as it ends.
//
// Only the user edits this page. Its controls call the window's ChatAgent directly; none is a
// command, so no agent can reach them (an agent must never mark a connection private, which loosens
// what the chat reads). preferences.open {section: agent} only shows the page.

func init() { preferencesSectionLabels[engine.SectionAgent] = "Agent" }

// chatKinds are the connection kinds in the form's order, with their labels, base URL prefills and
// hints. Only LM Studio has a usual address; the others are wherever the user runs them.
var chatKinds = []struct {
	kind    engine.ChatConnectionKind
	label   string
	prefill string
	hint    string
}{
	{engine.ChatLMStudio, "LM Studio", "http://localhost:1234/v1", "LM Studio's local server with /v1: http://localhost:1234/v1 unless you changed its port."},
	{engine.ChatLiteLLM, "LiteLLM", "", "The LiteLLM proxy's address, for example http://litellm.internal:4000."},
	{engine.ChatOpenWebUI, "Open WebUI", "", "Open WebUI's address, for example https://chat.example.com: Cogpit uses its /api routes."},
	{engine.ChatOpenAI, "Other OpenAI-compatible", "", "The API's base URL, where /chat/completions follows, for example https://llm.example.com/v1."},
}

func chatKindLabel(k engine.ChatConnectionKind) string {
	for _, c := range chatKinds {
		if c.kind == k {
			return c.label
		}
	}
	return string(k)
}

// chatTestLabels name the connection test's layers.
var chatTestLabels = map[engine.ChatTestLayer]string{
	engine.ChatTestDNS: "DNS", engine.ChatTestTCP: "TCP", engine.ChatTestHTTP: "HTTP or HTTPS", engine.ChatTestAuth: "Authentication",
	engine.ChatTestModels: "Models", engine.ChatTestTools: "Tool use",
}

// Texts of the page.
const (
	privateCheckText = "Private connection: a local model or a private cloud"
	privateHelp      = "Cogpit's chat then reads what the window shows, Secret values included; writes still ask. " +
		"Without the mark it reads only what external agents may read."
	// privateNotKept is the setting's hint (H-96): a conversation in which a turn ran over a private
	// connection is never saved (agent/conversation.go has the rule).
	privateNotKept = "Conversations over a private connection are not saved: they are gone when Cogpit quits."
	// keyKept says where the agent keeps an API key (agent.KeysFile, owner-only; no keychain).
	keyKept = "Cogpit's settings folder, readable only by you"
	// allToolsText and allToolsHelp are the setting to send every command as a tool (the owner's
	// decision of 2026-10-15). The numbers are agent.TestToolsetSize's, measured 2026-10-26: 197
	// tools, about 70,000 tokens; the core set 22 tools, about 8,600.
	allToolsText = "Send all tools: every Cogpit command as a tool of its own"
	allToolsHelp = "For large cloud models: each request then carries some 200 tools, about 70,000 tokens, more than " +
		"many local models' context holds. Off: 20 core tools and a search that finds the rest, about 8,600 tokens. " +
		"The same rules either way: reads as the private mark says, writes ask."
	// caHelp is the CA file's hint (the owner's decisions of 2026-10-15 and, N141, of 2026-10-16: the
	// file replaces the system's roots for the connection; agent/cafile.go).
	caHelp = "For an https server with a private CA: a PEM file of its CA certificates, the only ones this connection " +
		"trusts, not the system's, read when connecting. Empty: the system's (on macOS its keychains)."
)

// agentPrefs is the Agent section's state.
type agentPrefs struct {
	p      *settingsPage
	list   *fyne.Container
	addB   *textButton
	formC  *fyne.Container
	form   *agentForm
	conns  []engine.ChatConnection
	loaded bool
	err    error
	seq    int
}

// agentSection builds Preferences › Agent.
func (p *settingsPage) agentSection() fyne.CanvasObject {
	if !p.sh.chat.available() {
		p.agent = nil
		return container.NewVBox(p.setting("agentNone", "Agent", "", newParagraph("This Cogpit has no agent chat.", colorMuted, settingsColumn)))
	}
	a := &agentPrefs{p: p, list: container.NewVBox(), formC: container.NewVBox()}
	p.agent = a
	a.addB = newPlateButton("Add Connection", theme.ContentAddIcon(), func() {
		a.edit(engine.ChatConnection{Kind: engine.ChatLMStudio, BaseURL: chatKinds[0].prefill})
	})
	a.load()
	help := "Each conversation uses one; a new conversation the default. API keys stay in Cogpit's settings folder."
	return container.NewVBox(
		p.setting("agentConnections", "LLM connections", help, container.NewVBox(a.list, container.NewHBox(a.addB))),
		a.formC,
	)
}

// chatConnectionsChanged reads the connections again when Preferences › Agent is open (EventChat
// for the lists).
func (p *settingsPage) chatConnectionsChanged() {
	if p.agent != nil && p.visible() && p.shown.Page == engine.SettingsPreferences && p.shown.Section == engine.SectionAgent {
		p.agent.load()
	}
}

// load reads the connections off the main goroutine and lists them.
func (a *agentPrefs) load() {
	a.seq++
	seq := a.seq
	agent := a.p.sh.chat.agent
	go func() {
		defer a.p.sh.recoverTo("Preferences › Agent")
		conns, err := agent.Connections()
		a.p.sh.do(func() {
			if seq != a.seq || a.p.agent != a {
				return
			}
			a.conns, a.err, a.loaded = conns, err, true
			a.render()
		})
	}()
}

// render lists the connections.
func (a *agentPrefs) render() {
	var rows []fyne.CanvasObject
	switch {
	case a.err != nil:
		rows = append(rows, newParagraph("The connections could not be read: "+a.err.Error(), colorStatusError, settingsColumn))
	case a.loaded && len(a.conns) == 0:
		rows = append(rows, newParagraph("No connection yet. Add one to chat with Cogpit's agent: LM Studio on this computer, "+
			"LiteLLM, Open WebUI or any OpenAI-compatible server.", colorMuted, settingsColumn))
	}
	for _, c := range connectionChoices(a.conns) {
		rows = append(rows, a.row(c))
	}
	a.list.Objects = rows
	a.list.Refresh()
	a.addB.Disable()
	if a.form == nil {
		a.addB.Enable()
	}
}

// row is one connection: its mark (a lock when private), name, default, kind, host and model, and
// its actions.
func (a *agentPrefs) row(c engine.ChatConnection) fyne.CanvasObject {
	icon := theme.ComputerIcon()
	tint := colorMuted
	if c.Private {
		icon, tint = iconLock, colorStatusOK
	}
	name := newTextLine(c.Name, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	tags := container.NewHBox(name)
	if c.Default {
		tags.Add(newPill("default", colorAccent, true))
	}
	if c.Private {
		tags.Add(newPill("private", colorStatusOK, true))
	}
	if engine.ChatPlainHTTPRemote(c.BaseURL) {
		tags.Add(newPill("plain http", colorStatusWarning, true))
	}
	parts := []string{chatKindLabel(c.Kind), hostOf(c.BaseURL)}
	if c.Model != "" {
		parts = append(parts, c.Model)
	}
	if !c.HasKey {
		parts = append(parts, "no API key")
	}
	if c.AllTools {
		parts = append(parts, "all tools")
	}
	if c.CAFile != "" {
		parts = append(parts, "CA file")
	}
	detail := newTextLine(strings.Join(parts, " · "), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	detail.truncate = true
	conn := c
	edit := newTextButton("Edit", nil, func() { a.edit(conn) })
	del := newPlateButton("", theme.DeleteIcon(), func() { a.confirmDelete(conn) })
	del.Importance = widget.LowImportance
	actions := container.NewHBox(edit, del)
	if !c.Default {
		def := newTextButton("Make Default", nil, func() { a.makeDefault(conn.ID) })
		actions.Objects = append([]fyne.CanvasObject{def}, actions.Objects...)
	}
	bg := newSurface(theme.ColorNameInputBackground)
	bg.radius = theme.SizeNameInputRadius
	content := container.NewBorder(nil, nil, container.NewCenter(sized(sizeCellIcon, sizeCellIcon, widget.NewIcon(themedIcon(icon, tint)))), actions,
		container.NewVBox(tags, detail))
	return container.NewStack(bg, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, content))
}

// hostOf is a base URL's host and port, or the URL when it has none.
func hostOf(base string) string {
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Host
	}
	return base
}

// edit opens the form for a connection (a new one has no ID).
func (a *agentPrefs) edit(c engine.ChatConnection) {
	if a.form != nil {
		a.form.close()
	}
	a.form = newAgentForm(a, c)
	a.formC.Objects = []fyne.CanvasObject{a.form.root}
	a.formC.Refresh()
	a.addB.Disable()
	a.p.sh.win.Canvas().Focus(a.form.name)
}

// closeForm closes the form.
func (a *agentPrefs) closeForm() {
	if a.form != nil {
		a.form.close()
	}
	a.form = nil
	a.formC.Objects = nil
	a.formC.Refresh()
	a.addB.Enable()
}

func (a *agentPrefs) confirmDelete(c engine.ChatConnection) {
	sh := a.p.sh
	d := dialog.NewConfirm("Delete "+c.Name+"?", "The connection and its API key are deleted. "+
		"Conversations that used it keep their history and take another connection.", func(ok bool) {
		if !ok {
			return
		}
		agent := sh.chat.agent
		go func() {
			defer sh.recoverTo("Preferences › Agent")
			err := agent.DeleteConnection(c.ID)
			sh.do(func() {
				if err != nil {
					sh.toast("error", "Could not delete "+c.Name+": "+errorText(err))
				}
				if a.form != nil && a.form.conn.ID == c.ID {
					a.closeForm()
				}
				a.load()
				sh.chat.requestLists()
			})
		}()
	}, sh.win)
	d.SetConfirmText("Delete")
	sh.showDialog(d)
}

func (a *agentPrefs) makeDefault(id string) {
	sh := a.p.sh
	agent := sh.chat.agent
	go func() {
		defer sh.recoverTo("Preferences › Agent")
		err := agent.SetDefaultConnection(id)
		sh.do(func() {
			if err != nil {
				sh.toast("error", "Could not make it the default: "+errorText(err))
			}
			a.load()
			sh.chat.requestLists()
		})
	}()
}

// agentForm is the form of one connection.
type agentForm struct {
	a    *agentPrefs
	conn engine.ChatConnection // as stored; ID empty for a new one
	root fyne.CanvasObject

	name      *fieldEntry // the form's fields: Escape is the form's Cancel (D-116)
	kind      *selectField
	baseURL   *fieldEntry
	urlHint   *textLine
	plainWarn *paragraph // plain http to another machine (H-95)
	caFile    *fieldEntry
	caB       *textButton
	key       *fieldEntry
	keyHelp   *textLine
	removeKey *textButton
	keyGone   bool // "Remove key" was pressed: saving removes the stored key
	model     *fieldSelectEntry
	modelsB   *textButton
	modelNote *textLine
	modelsSeq int    // the model list asked last; an older answer is dropped
	listedFor string // the endpoint, CA file and typed key the list was asked with (listKey)
	private   *widget.Check
	allTools  *widget.Check
	limits    [4]*fieldEntry
	def       *widget.Check
	errLine   *textLine
	saveB     *textButton
	testB     *textButton
	steps     *fyne.Container
	stepRows  map[engine.ChatTestLayer]*agentTestRow
	testWarn  *paragraph // a layer's warning, under the test's rows

	lastKind engine.ChatConnectionKind
	textW    float32 // the form's text width, for the test rows' details
	testing  context.CancelFunc
	testSeq  int
	saving   bool
}

func newAgentForm(a *agentPrefs, c engine.ChatConnection) *agentForm {
	f := &agentForm{a: a, conn: c, lastKind: c.Kind, stepRows: map[engine.ChatTestLayer]*agentTestRow{}}
	p := a.p
	title := "New connection"
	if c.ID != "" {
		title = "Edit " + c.Name
	}
	f.name = f.field(newFieldEntry())
	f.name.SetPlaceHolder("LM Studio on this Mac")
	f.name.SetText(c.Name)

	labels := make([]string, len(chatKinds))
	for i, k := range chatKinds {
		labels[i] = k.label
	}
	f.baseURL = f.field(newFieldEntry())
	f.baseURL.SetText(c.BaseURL)
	f.urlHint = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	f.urlHint.truncate = true
	textW := settingsColumn - 4*p.sh.app.Settings().Theme().Size(theme.SizeNameInnerPadding)
	f.textW = textW
	f.plainWarn = newParagraph(engine.ChatPlainHTTPWarning, colorStatusWarning, textW)
	f.baseURL.OnChanged = func(string) { f.showPlainWarning() }
	f.caFile = f.field(newFieldEntry())
	placeholder, _ := caFileExamples(runtime.GOOS)
	f.caFile.SetPlaceHolder(placeholder)
	f.caFile.SetText(c.CAFile)
	f.caB = newTextButton("Choose…", nil, f.chooseCAFile)
	f.kind = newSelectField(labels, func(label string) { f.kindChanged(label) })
	f.kind.SetSelected(chatKindLabel(orKind(c.Kind)))

	f.key = f.field(newFieldPasswordEntry())
	f.keyHelp = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	f.keyHelp.truncate = true
	f.removeKey = newTextButton("Remove Key", nil, func() {
		f.keyGone = true
		f.key.SetText("")
		f.showKeyState()
	})
	f.key.OnChanged = func(string) { f.showKeyState() }
	f.showKeyState()

	f.model = newFieldSelectEntry(nil)
	f.model.onEscape = f.escape
	f.model.SetPlaceHolder("qwen3.6-27b")
	f.model.SetText(c.Model)
	f.modelNote = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	f.modelNote.truncate = true
	f.modelsB = newPlateButton("", theme.ViewRefreshIcon(), f.loadModels)
	f.modelsB.Importance = widget.LowImportance
	f.baseURL.onLeave, f.key.onLeave = f.modelsAfterEdit, f.modelsAfterEdit
	if c.ID == "" {
		f.modelNote.SetText(modelsFirstNote)
	} else {
		f.loadModels()
	}

	f.private = widget.NewCheck(privateCheckText, nil)
	f.private.SetChecked(c.Private)
	f.allTools = widget.NewCheck(allToolsText, nil)
	f.allTools.SetChecked(c.AllTools)

	lim := c.Limits
	def := engine.DefaultChatLimits
	values := [4]int{lim.RepeatStop, lim.MaxToolCalls, lim.MaxTokens, int(lim.MaxDuration / time.Minute)}
	defaults := [4]int{def.RepeatStop, def.MaxToolCalls, def.MaxTokens, int(def.MaxDuration / time.Minute)}
	limitLabels := [4]string{"Stop at identical tool calls", "Tool calls", "Tokens", "Minutes"}
	grid := make([]fyne.CanvasObject, 0, len(f.limits))
	for i := range f.limits {
		e := f.field(newFieldEntry())
		e.SetPlaceHolder(formatCount(int64(defaults[i])) + " (default)")
		if values[i] > 0 {
			e.SetText(strconv.Itoa(values[i]))
		}
		f.limits[i] = e
		grid = append(grid, container.NewVBox(newTextLine(limitLabels[i], theme.ColorNameForeground, theme.SizeNameCaptionText, fyne.TextStyle{}), e))
	}

	f.def = widget.NewCheck("Use for new conversations (the default)", nil)
	f.def.SetChecked(c.Default)
	if c.Default {
		f.def.Disable() // another connection becomes the default from its own row
	}

	f.errLine = newTextLine("", colorStatusError, theme.SizeNameText, fyne.TextStyle{})
	f.errLine.Hide()
	f.steps = container.NewVBox()
	f.testB = newPlateButton("Test Connection", theme.MediaPlayIcon(), f.test)
	f.saveB = newPlateButton("Save", theme.DocumentSaveIcon(), f.save)
	f.saveB.Importance = widget.HighImportance
	cancel := newPlateButton("Cancel", nil, a.closeForm)

	bg := newSurface(theme.ColorNameInputBackground)
	bg.radius, bg.stroke = theme.SizeNameInputRadius, colorHairline
	form := container.NewVBox(
		newTextLine(title, theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true}),
		newSpace(theme.SizeNamePadding),
		p.setting("agentName", "Name", "How the chat's connection menu names it.", f.name),
		p.setting("agentKind", "Kind", "Every kind speaks OpenAI's Chat Completions API with tool calling.", f.kind),
		p.setting("agentBaseURL", "Base URL", "", container.NewVBox(f.baseURL, f.urlHint, f.plainWarn)),
		p.setting("agentKey", "API key", "", container.NewVBox(container.NewBorder(nil, nil, nil, f.removeKey, f.key), f.keyHelp)),
		p.setting("agentModel", "Model", "", container.NewVBox(container.NewBorder(nil, nil, nil, f.modelsB, f.model), f.modelNote)),
		p.setting("agentCAFile", "CA file", "", container.NewVBox(container.NewBorder(nil, nil, nil, f.caB, f.caFile),
			newParagraph(caHelp, colorMuted, textW))),
		p.setting("agentPrivate", "Private connection", privateNotKept, container.NewVBox(f.private,
			newParagraph(privateHelp, colorMuted, textW))),
		p.setting("agentAllTools", "Tools", "", container.NewVBox(f.allTools, newParagraph(allToolsHelp, colorMuted, textW))),
		p.setting("agentLimits", "Limits per turn", "Empty takes the default; a turn that reaches one stops with a note.",
			container.NewGridWithColumns(4, grid...)),
		f.def,
		newSpace(theme.SizeNameInnerPadding),
		f.errLine,
		container.NewHBox(f.testB, layout.NewSpacer(), cancel, f.saveB),
		f.steps,
	)
	f.root = container.NewStack(bg, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, form))
	f.testWarn = newParagraph("", colorStatusWarning, textW)
	f.showPlainWarning()
	return f
}

// field makes a text field of the form close the form on Escape, as its Cancel (D-116: Escape
// closes the innermost thing open; the card closes with the next Escape).
func (f *agentForm) field(e *fieldEntry) *fieldEntry {
	e.onEscape = f.escape
	return e
}

// escape is Escape in a field of the form: the form's Cancel, the keyboard taken off the field that
// goes with it.
func (f *agentForm) escape() {
	if c := fyne.CurrentApp().Driver().CanvasForObject(f.root); c != nil {
		c.Unfocus()
	}
	f.a.closeForm()
}

// showPlainWarning shows, while the base URL is plain http to another machine, that not only the
// key but the conversation and every secret the agent reads cross the network unencrypted (the
// owner's decision of 2026-10-15, H-95); localhost and loopback addresses are exempt.
func (f *agentForm) showPlainWarning() {
	if f.plainWarn == nil {
		return
	}
	f.plainWarn.Hidden = !engine.ChatPlainHTTPRemote(f.baseURL.Text)
	f.plainWarn.Refresh()
}

// chooseCAFileWithDialog picks the CA file in Fyne's file dialog: chooseCAFile's fallback where
// the system has no panel (filepicker_choosers.go).
func (f *agentForm) chooseCAFileWithDialog() {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		path := filepath.FromSlash(r.URI().Path()) // a file URI's path has slashes, on Windows too (C:/Users/…)
		_ = r.Close()
		f.caFile.SetText(path)
	}, f.a.p.sh.win)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".pem", ".crt", ".cer"}))
	f.a.p.sh.showDialog(d)
}

// caFileExamples are the CA file field's placeholder and the absolute path its error suggests, as
// goos writes paths: a Windows path begins with its drive, and /Users/you/… is not absolute there.
func caFileExamples(goos string) (placeholder, example string) {
	if goos == "windows" {
		return `C:\path\to\private-ca.pem`, `C:\Users\you\certs\private-ca.pem`
	}
	return "/path/to/private-ca.pem", "/Users/you/certs/private-ca.pem"
}

// caFilePath is the CA file's field as a path: "~/" is the home folder.
func (f *agentForm) caFilePath() string {
	p := strings.TrimSpace(f.caFile.Text)
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, rest)
		}
	}
	return p
}

func orKind(k engine.ChatConnectionKind) engine.ChatConnectionKind {
	if k == "" {
		return engine.ChatLMStudio
	}
	return k
}

// kindChanged shows the kind's hint and prefills its usual base URL where the field is empty or
// holds another kind's prefill.
func (f *agentForm) kindChanged(label string) {
	for _, k := range chatKinds {
		if k.label != label {
			continue
		}
		f.urlHint.SetText(k.hint)
		f.baseURL.SetPlaceHolder(k.prefill)
		cur := strings.TrimSpace(f.baseURL.Text)
		prefilled := cur == ""
		for _, o := range chatKinds {
			if o.prefill != "" && cur == o.prefill {
				prefilled = true
			}
		}
		if prefilled {
			f.baseURL.SetText(k.prefill)
		}
		f.lastKind = k.kind
	}
}

// showKeyState says what saving does with the key.
func (f *agentForm) showKeyState() {
	switch {
	case f.key.Text != "":
		f.keyGone = false
		f.keyHelp.SetText("Saving keeps this key in " + keyKept + ".")
	case f.keyGone:
		f.keyHelp.SetText("Saving removes the stored key.")
	case f.conn.HasKey:
		f.keyHelp.SetText("The key is kept in " + keyKept + ". Empty keeps it.")
	default:
		f.keyHelp.SetText("Kept in " + keyKept + ". Empty: no key (LM Studio needs none).")
	}
	f.removeKey.Hidden = !f.conn.HasKey || f.keyGone || f.key.Text != ""
	f.removeKey.Refresh()
}

// apiKey is the key argument of SaveConnection and TestConnection: nil keeps the stored key, a
// pointer to "" removes it, any other text replaces it. A saved connection without a stored key
// saves "" for an empty field, as its hint says ("Empty: no key"): a key the agent no longer has
// (one kept in the macOS keychain before 2026-10-15) is then given up, not asked for again.
func (f *agentForm) apiKey() *string {
	switch {
	case f.key.Text != "":
		k := f.key.Text
		return &k
	case f.keyGone, f.conn.ID != "" && !f.conn.HasKey:
		k := ""
		return &k
	}
	return nil
}

// values reads the form into a connection; an error names the field that is wrong.
func (f *agentForm) values() (engine.ChatConnection, error) {
	if strings.TrimSpace(f.name.Text) == "" {
		return f.conn, errors.New("the connection needs a name")
	}
	c, err := f.endpoint()
	if err != nil {
		return c, err
	}
	c.Name = strings.TrimSpace(f.name.Text)
	c.Private = f.private.Checked
	c.AllTools = f.allTools.Checked
	var n [4]int
	for i, e := range f.limits {
		t := strings.NewReplacer(",", "", ".", "", " ", "").Replace(strings.TrimSpace(e.Text))
		if t == "" {
			continue
		}
		v, err := strconv.Atoi(t)
		if err != nil || v < 1 {
			return c, fmt.Errorf("a limit must be a whole number above 0, not %q", e.Text)
		}
		n[i] = v
	}
	c.Limits = engine.ChatLimits{RepeatStop: n[0], MaxToolCalls: n[1], MaxTokens: n[2], MaxDuration: time.Duration(n[3]) * time.Minute}
	return c, nil
}

// endpoint reads what a request to the endpoint needs: the kind, the base URL, the CA file and the
// model; the model list asks with it before the connection has a name.
func (f *agentForm) endpoint() (engine.ChatConnection, error) {
	c := f.conn
	c.Kind = f.lastKind
	if c.Kind == "" {
		c.Kind = engine.ChatLMStudio
	}
	c.BaseURL = strings.TrimSpace(f.baseURL.Text)
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != schemeWeb && u.Scheme != schemeWebSecure) || u.Host == "" {
		return c, errors.New("the base URL must be an http or https address, such as http://localhost:1234/v1")
	}
	c.CAFile = f.caFilePath()
	switch {
	case c.CAFile != "" && u.Scheme != schemeWebSecure:
		return c, errors.New("a CA file is for an https base URL: plain http checks no certificate")
	case c.CAFile != "" && !filepath.IsAbs(c.CAFile):
		_, example := caFileExamples(runtime.GOOS)
		return c, errors.New("the CA file must be an absolute path, such as " + example)
	}
	c.Model = strings.TrimSpace(f.model.Text)
	return c, nil
}

func (f *agentForm) showError(err error) {
	if err == nil {
		f.errLine.Hide()
		return
	}
	f.errLine.SetText(errorText(err))
	f.errLine.Show()
}

// save stores the connection (and the default, when asked) and closes the form.
func (f *agentForm) save() {
	c, err := f.values()
	f.showError(err)
	if err != nil || f.saving {
		return
	}
	makeDefault := f.def.Checked && !f.conn.Default
	key := f.apiKey()
	sh := f.a.p.sh
	agent := sh.chat.agent
	f.saving = true
	f.saveB.Disable()
	go func() {
		defer sh.recoverTo("Preferences › Agent")
		saved, err := agent.SaveConnection(c, key)
		if err == nil && makeDefault {
			err = agent.SetDefaultConnection(saved.ID)
		}
		sh.do(func() {
			f.saving = false
			f.saveB.Enable()
			if err != nil {
				f.showError(err)
				return
			}
			if f.a.form == f {
				f.a.closeForm()
			}
			f.a.load()
			sh.chat.requestLists()
		})
	}()
}

// modelsFirstNote is the model's note in a new connection, before a list was asked for.
const modelsFirstNote = "With the base URL and the key typed, ↻ lists the endpoint's models; any model name may be typed."

// loadModels fills the model list from the form's values: the base URL, kind and CA file as typed,
// the key typed or else the stored one (the owner's request of 2026-10-25: a colleague's list
// stayed empty after he typed his credentials, read from the saved connection only, and its button
// was off until the connection was saved).
func (f *agentForm) loadModels() {
	c, err := f.endpoint()
	if err != nil {
		f.showModels(nil, err)
		return
	}
	key := f.apiKey()
	f.listedFor = f.listKey()
	f.modelsSeq++
	seq := f.modelsSeq
	sh := f.a.p.sh
	f.modelsB.Disable()
	f.modelNote.SetColor(colorMuted)
	f.modelNote.SetText("Listing the endpoint's models…")
	go func() {
		defer sh.recoverTo("Preferences › Agent")
		models, err := sh.chat.modelsOf(c, key)
		sh.do(func() {
			if seq == f.modelsSeq {
				f.modelsB.Enable()
				f.showModels(models, err)
			}
		})
	}()
}

// modelsAfterEdit lists the models again when the base URL or the key field is left changed. Not
// while the stored key would go to a base URL other than the saved one: that only on ↻ or Test
// Connection, the user's own request (H-143).
func (f *agentForm) modelsAfterEdit() {
	if f.listKey() == f.listedFor {
		return
	}
	if strings.TrimSpace(f.baseURL.Text) == "" {
		return
	}
	if f.key.Text == "" && f.conn.HasKey && !f.keyGone && strings.TrimSpace(f.baseURL.Text) != f.conn.BaseURL {
		f.modelNote.SetColor(colorMuted)
		f.modelNote.SetText("A new base URL: ↻ lists its models with the stored key, or type the key.")
		return
	}
	f.loadModels()
}

// listKey names what a model list depends on, the typed key only by whether one is typed: when it
// changes, the list is asked for again.
func (f *agentForm) listKey() string {
	return strings.Join([]string{string(f.lastKind), strings.TrimSpace(f.baseURL.Text), f.caFilePath(), f.key.Text, strconv.FormatBool(f.keyGone)}, "\x00")
}

// showModels shows a model list or why there is none.
func (f *agentForm) showModels(models []string, err error) {
	if err != nil {
		f.modelNote.SetText("The models could not be listed: " + errorText(err))
		f.modelNote.SetColor(colorStatusError)
		return
	}
	f.model.SetOptions(models)
	f.modelNote.SetColor(colorMuted)
	if len(models) == 0 {
		f.modelNote.SetText("The endpoint lists no models; any model name may be typed.")
		return
	}
	f.modelNote.SetText(pluralOf(len(models), "model") + " at the endpoint; any model name may be typed.")
}

// test runs the connection test with the form's values; each layer shows as it ends.
func (f *agentForm) test() {
	c, err := f.values()
	f.showError(err)
	if err != nil {
		return
	}
	if f.testing != nil {
		f.testing()
	}
	sh := f.a.p.sh
	agent := sh.chat.agent
	ctx, cancel := context.WithTimeout(sh.ctx, chatTestTimeout)
	f.testing = cancel
	f.testSeq++
	seq := f.testSeq
	key := f.apiKey()
	f.testB.Disable()
	f.testB.SetText("Testing…")
	initial := make([]engine.ChatTestStep, len(engine.ChatTestLayers))
	for i, l := range engine.ChatTestLayers {
		initial[i] = engine.ChatTestStep{Layer: l, State: engine.ChatTestPending}
	}
	f.showSteps(initial)
	go func() {
		defer sh.recoverTo("Preferences › Agent")
		defer cancel()
		final := agent.TestConnection(ctx, c, key, func(steps []engine.ChatTestStep) {
			sh.do(func() {
				if seq == f.testSeq {
					f.showSteps(steps)
				}
			})
		})
		sh.do(func() {
			if seq != f.testSeq {
				return
			}
			f.testing = nil
			f.testB.Enable()
			f.testB.SetText("Test Connection")
			f.showSteps(final)
			f.modelsFromTest(final)
		})
	}()
}

// close ends a test under way.
func (f *agentForm) close() {
	if f.testing != nil {
		f.testing()
		f.testing = nil
	}
	f.testSeq++
}

// modelsFromTest fills the model list from a test whose Models layer passed: the list the test read
// with the same values.
func (f *agentForm) modelsFromTest(steps []engine.ChatTestStep) {
	for _, s := range steps {
		if s.Layer == engine.ChatTestModels && s.State == engine.ChatTestPass {
			f.modelsSeq++ // a list asked before the test is older
			f.modelsB.Enable()
			f.listedFor = f.listKey()
			f.showModels(s.Models, nil)
		}
	}
}

// showSteps shows the test's layers: passed, failed with the reason, not reached, running.
func (f *agentForm) showSteps(steps []engine.ChatTestStep) {
	if len(f.steps.Objects) == 0 {
		objs := make([]fyne.CanvasObject, 0, len(engine.ChatTestLayers)+2)
		objs = append(objs, heading("Connection test"))
		th := f.a.p.sh.app.Settings().Theme()
		detailW := f.textW - th.Size(sizeCellIcon) - th.Size(sizeDrawerLabel) - agentTestTookWidth - 4*th.Size(theme.SizeNamePadding)
		exchangeW := f.textW - th.Size(sizeCellIcon) - th.Size(theme.SizeNameInlineIcon) - 2*th.Size(theme.SizeNameInnerPadding) -
			3*th.Size(theme.SizeNamePadding)
		for _, l := range engine.ChatTestLayers {
			r := newAgentTestRow(f.a.p.sh, l, detailW, exchangeW)
			f.stepRows[l] = r
			objs = append(objs, r.root)
		}
		objs = append(objs, f.testWarn,
			newTextLine("The test sends no cluster data: one harmless tool and a fixed question.", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
		f.steps.Objects = objs
	}
	warning := ""
	for _, s := range steps {
		if r := f.stepRows[s.Layer]; r != nil {
			r.set(s)
		}
		if s.State == engine.ChatTestPass && s.Warning != "" {
			warning = s.Warning
		}
	}
	f.testWarn.text = warning
	f.testWarn.Hidden = warning == ""
	f.testWarn.Refresh()
	f.steps.Refresh()
}

// agentTestRow is one layer of the connection test. Its detail is selectable, and a failed layer
// that sent a request shows that request and its answer below it (Exchange, curl -v's form), in a
// selectable block with a copy icon (the owner, 2026-10-26: "WELCHER ENDPUNKT WURDE WIE
// AUFGERUFEN???").
type agentTestRow struct {
	sh        *shell
	root      fyne.CanvasObject
	icon      *widget.Icon
	label     *textLine
	detail    *selParagraph   // wrapped, never cut: a certificate's issuer and end, the auth's two answers (2026-10-25)
	detailBox *fyne.Container // holds detail, made anew when its text or colour changes (a selParagraph wraps once)
	detailW   float32
	exchange  *fyne.Container // a failed layer's request and answer
	xRow      *fyne.Container // the exchange under the detail, hidden without one
	exchangeW float32
	shownX    string // the exchange shown
	took      *textLine
	step      engine.ChatTestStep
}

// agentTestTookWidth is the room a test row keeps at its right for the time a layer took.
const agentTestTookWidth float32 = 48

func newAgentTestRow(sh *shell, l engine.ChatTestLayer, detailW, exchangeW float32) *agentTestRow {
	r := &agentTestRow{sh: sh, icon: widget.NewIcon(nil), detailW: detailW, exchangeW: exchangeW}
	r.label = newTextLine(chatTestLabels[l], theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	r.detailBox = container.NewStack()
	r.setDetail("", colorMuted)
	r.took = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	label := box(sizedLayout{width: sizeDrawerLabel}, r.label)
	head := container.NewHBox(container.NewVBox(sized(sizeCellIcon, "", r.icon)), container.NewVBox(label))
	r.exchange = container.NewStack()
	line := container.NewBorder(nil, nil, head, container.NewVBox(r.took), r.detailBox)
	r.xRow = container.NewBorder(nil, nil, box(sizedLayout{width: sizeCellIcon}), nil, r.exchange)
	r.xRow.Hide()
	r.root = container.NewVBox(line, r.xRow)
	return r
}

// setDetail shows the detail's text in its colour, selectable.
func (r *agentTestRow) setDetail(text string, c fyne.ThemeColorName) {
	if r.detail != nil && r.detail.p.text == text && r.detail.p.color == c {
		return
	}
	r.detail = newSelParagraph(newParagraph(text, c, r.detailW), theme.ColorNameSelection, func(sel string) { r.sh.copyText(sel, false) })
	r.detailBox.Objects = []fyne.CanvasObject{r.detail}
	r.detailBox.Refresh()
}

// setExchange shows a failed layer's request and answer, or hides the block when there is none.
func (r *agentTestRow) setExchange(x string) {
	if x == r.shownX {
		return
	}
	r.shownX = x
	if x == "" {
		r.exchange.Objects = nil
		r.xRow.Hide()
		return
	}
	p := newParagraphStyled(x, theme.ColorNameForeground, r.exchangeW, fyne.TextStyle{Monospace: true})
	p.size, p.hard = theme.SizeNameCaptionText, true // a request's JSON body is one long word
	text := newSelParagraph(p, theme.ColorNameSelection, func(sel string) { r.sh.copyText(sel, false) })
	glyph := newCopyGlyph(r.sh, x, theme.ColorNameForeground)
	bg := newSurface(theme.ColorNameInputBackground)
	bg.radius = theme.SizeNameInputRadius
	r.exchange.Objects = []fyne.CanvasObject{bg, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewBorder(nil, nil, nil, container.NewVBox(glyphSlot(glyph)), text))}
	r.xRow.Show()
	r.exchange.Refresh()
}

// set shows a step.
func (r *agentTestRow) set(s engine.ChatTestStep) {
	r.step = s
	icon, c, text := theme.RadioButtonIcon(), colorMuted, "Waiting"
	switch s.State {
	case engine.ChatTestRunning:
		icon, c, text = theme.ViewRefreshIcon(), colorAccent, "Checking…"
	case engine.ChatTestPass:
		icon, c, text = theme.ConfirmIcon(), colorStatusOK, "Passed"
		if s.Warning != "" { // passed, with a caution shown under the rows
			icon, c = theme.WarningIcon(), colorStatusWarning
		}
	case engine.ChatTestFail:
		icon, c, text = theme.ErrorIcon(), colorStatusError, "Failed"
	case engine.ChatTestNotReached:
		icon, c, text = theme.RadioButtonIcon(), colorMuted, "Not reached: an earlier layer failed"
	}
	if s.Detail != "" && s.State != engine.ChatTestNotReached {
		text += ": " + s.Detail
	}
	r.icon.SetResource(themedIcon(icon, c))
	if s.State == engine.ChatTestFail {
		c = colorStatusError
	}
	r.setDetail(text, c)
	x := ""
	if s.State == engine.ChatTestFail {
		x = s.Exchange
	}
	r.setExchange(x)
	r.took.SetText("")
	if s.Duration > 0 && (s.State == engine.ChatTestPass || s.State == engine.ChatTestFail) {
		r.took.SetText(formatTook(s.Duration))
	}
}

// formatTook writes a test step's time: "38 ms", "1.2 s".
func formatTook(d time.Duration) string {
	if d < time.Second {
		return strconv.FormatInt(max(d.Milliseconds(), 1), 10) + " ms"
	}
	return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + " s"
}
