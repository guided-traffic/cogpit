package fyneui

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The create dialogs (G7). On the views of Namespaces, Service Accounts, Roles, Cluster Roles,
// Role Bindings, Cluster Role Bindings and Resource Quotas the "+" opens a dialog that asks for the
// new object's fields, as Lens's does, instead of a Create resource tab. Lens's dialogs are not
// captured: which fields they ask for and in which order is inferred; the layout and the texts are
// Cogpit's. A dialog dispatches its kind's create command (internal/engine/commands/
// createdialogs.go), which checks the fields, dry-runs and creates the object; an error keeps the
// dialog open with the reason, so that the user can correct the field. Roles, cluster roles,
// service accounts and namespaces are offered from Cogpit's cache (resources.list; the namespace
// filter's list), never listed by the dialog itself; a name not offered can still be typed. When
// the access reviews deny the create in the chosen namespace, Create is greyed and the dialog says
// why.

// createDialogSpec is one kind's create dialog.
type createDialogSpec struct {
	command    string
	namespaced bool
	wide       bool // rows of several columns: the dialog is wider
	build      func(f *createForm)
}

// createDialogs are the create dialogs by the kind key of the view whose "+" opens them.
var createDialogs = map[string]createDialogSpec{
	kindNamespaces: {command: "namespace.create", build: func(f *createForm) { f.addName("team-a") }},
	"serviceaccounts": {command: "serviceAccount.create", namespaced: true, build: func(f *createForm) {
		f.addName("ci-deployer")
		f.addNamespace()
	}},
	"roles":               {command: "role.create", namespaced: true, wide: true, build: func(f *createForm) { buildRoleForm(f, false) }},
	"clusterroles":        {command: "clusterRole.create", wide: true, build: func(f *createForm) { buildRoleForm(f, true) }},
	"rolebindings":        {command: "roleBinding.create", namespaced: true, wide: true, build: func(f *createForm) { buildBindingForm(f, false) }},
	"clusterrolebindings": {command: "clusterRoleBinding.create", wide: true, build: func(f *createForm) { buildBindingForm(f, true) }},
	"resourcequotas":      {command: "resourceQuota.create", namespaced: true, wide: true, build: buildQuotaForm},
}

// The kinds a create dialog offers names of, as resources.list knows them.
const (
	kindRoles           = "roles"
	kindClusterRoles    = "clusterroles"
	kindServiceAccounts = "serviceaccounts"
)

// The subject kinds and role kinds of the binding dialogs, as the API names them.
const (
	subjectUser           = "User"
	subjectGroup          = "Group"
	subjectServiceAccount = "ServiceAccount"
	roleKindRole          = "Role"
	roleKindClusterRole   = "ClusterRole"
)

// createListLimit is the most names a dialog offers of one kind.
const createListLimit = 1000

// The parameter names the create commands share.
const (
	paramName      = "name"
	paramNamespace = "namespace"
	paramKind      = "kind"
)

// createListParams are resources.list's parameters as the create dialogs send them.
type createListParams struct {
	Kind       string   `json:"kind"`
	Namespaces []string `json:"namespaces,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

// quotaResources are the resource names a quota's limit offers: Kubernetes' standard ones and
// object counts; another can be typed.
var quotaResources = []string{
	"pods", "services", "configmaps", "secrets", "persistentvolumeclaims", "replicationcontrollers",
	"resourcequotas", "services.loadbalancers", "services.nodeports",
	"requests.cpu", "requests.memory", "requests.storage", "requests.ephemeral-storage",
	"limits.cpu", "limits.memory", "limits.ephemeral-storage",
	"count/deployments.apps", "count/statefulsets.apps", "count/jobs.batch", "count/cronjobs.batch",
}

// createForm is one shown create dialog. Main goroutine only.
type createForm struct {
	sh      *shell
	cmd     *engine.Command
	spec    createDialogSpec
	width   float32
	content *fyne.Container
	d       *dialogFrame
	create  *widget.Button
	cancel  *widget.Button
	status  *paragraph

	name *widget.Entry
	ns   *widget.SelectEntry // nil for a cluster-scoped kind
	// The binding dialogs' role (roleKind nil in a cluster role binding's) and every list of rows.
	roleKind *selectField
	roleName *widget.SelectEntry
	editors  []*rowEditor
	secret   *secretForm                      // the Create Secret dialog's own fields (createdialog_secret.go)
	offered  map[*widget.SelectEntry][]string // what each field offers from the cache (offer)
	// writers add the dialog's other fields to the parameters, or say what is wrong with them.
	writers []func(map[string]any) error
	// onNamespace runs when the namespace changes: options that depend on it are offered again.
	onNamespace []func(string)
	// nsFields are the fields that offer the namespaces; they follow the names while the dialog is
	// open (stopNS ends it), so that a namespace created since is offered (WF10).
	nsFields []*widget.SelectEntry
	stopNS   func()

	shown    bool
	denied   string // the access reviews' reason; Create is greyed while it is set
	message  string // the outcome of the last Create, or why it could not be sent
	msgColor fyne.ThemeColorName
	busy     bool // a Create is on its way
	checkSeq int  // the latest access check; older answers are dropped
	fields   int  // fields added so far, for the gap before each caption
}

// openCreateDialog opens the create dialog of a view's kind and reports whether it has one: the
// "+" opens a Create resource tab otherwise.
func (sh *shell) openCreateDialog(kind string) bool {
	_, ok := sh.createDialog(kind)
	return ok
}

// createDialog builds the create dialog of a kind and shows it, or reports that the kind has none
// (as in a build whose registry lacks the command). A namespaced kind's dialog starts from the
// namespace its command would choose (its Prefill: the namespace filter's single namespace, else
// the context's default), asked off the main goroutine before the dialog shows.
func (sh *shell) createDialog(kind string) (*createForm, bool) {
	spec, ok := createDialogs[kind]
	if !ok {
		return nil, false
	}
	cmd, ok := sh.eng.Registry().Get(spec.command)
	if !ok {
		return nil, false
	}
	f := sh.newCreateForm(cmd, spec)
	if !spec.namespaced {
		f.show()
		return f, true
	}
	params := formJSON(f.paramsAsIs())
	go func() {
		defer sh.recoverTo(spec.command)
		var pre struct {
			Namespace string `json:"namespace"`
		}
		_ = decode(sh.prefill(cmd, params), &pre)
		sh.do(func() {
			if pre.Namespace != "" {
				f.setNamespace(pre.Namespace)
			}
			f.show()
		})
	}()
	return f, true
}

// newCreateForm builds a create dialog's widgets; show shows it.
func (sh *shell) newCreateForm(cmd *engine.Command, spec createDialogSpec) *createForm {
	th := sh.app.Settings().Theme()
	f := &createForm{sh: sh, cmd: cmd, spec: spec, width: th.Size(sizeDialogWidth), content: container.NewVBox()}
	if spec.wide {
		f.width *= 1.5
	}
	f.status = newParagraph("", colorMuted, f.width)
	spec.build(f)
	f.content.Add(newSpace(theme.SizeNamePadding))
	f.content.Add(f.status)
	f.create = widget.NewButton("Create", f.submit)
	f.create.Importance = widget.HighImportance
	f.cancel = widget.NewButton("Cancel", nil)
	f.d = newDialogFrame(sh, dialogTitle(cmd.Label)) // a card with a header and a footer (dialogframe.go)
	f.d.setContent(f.content)
	f.d.SetButtons([]fyne.CanvasObject{f.cancel, f.create})
	f.cancel.OnTapped = f.d.Hide
	f.d.SetOnClosed(func() {
		f.shown = false
		if f.stopNS != nil {
			f.stopNS()
			f.stopNS = nil
		}
	})
	return f
}

// namespaceField returns a field that offers the namespaces the window knows, kept current while
// the dialog is open.
func (f *createForm) namespaceField() *widget.SelectEntry {
	e := widget.NewSelectEntry(slices.Clone(f.sh.namespaces))
	f.nsFields = append(f.nsFields, e)
	return e
}

// show shows the dialog, puts the cursor in its first field and asks the access reviews.
func (f *createForm) show() {
	f.fit()
	f.d.Show()
	f.shown = true
	if len(f.nsFields) > 0 && f.stopNS == nil {
		f.stopNS = f.sh.followNamespaces(func(names []string) {
			for _, e := range f.nsFields {
				e.SetOptions(names)
			}
		})
		f.sh.refreshNamespaces()
	}
	if f.name != nil {
		f.sh.win.Canvas().Focus(f.name)
	}
	f.check()
}

// fit sizes the dialog to its content at the form's width.
func (f *createForm) fit() {
	f.d.Resize(fyne.NewSize(f.width, f.d.MinSize().Height))
}

// formJSON marshals the parameters a form built; they always marshal.
func formJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

// caption adds a field's caption, with a gap before every caption but the first.
func (f *createForm) caption(text string) {
	if f.fields > 0 {
		f.content.Add(newSpace(theme.SizeNamePadding))
	}
	f.fields++
	f.content.Add(newTextLine(text, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true}))
}

// addName adds the object's name.
func (f *createForm) addName(placeholder string) {
	f.caption("NAME")
	f.name = widget.NewEntry()
	f.name.SetPlaceHolder(placeholder)
	f.name.OnSubmitted = func(string) { f.submit() }
	f.content.Add(f.name)
}

// addNamespace adds the namespace, offering the namespaces the namespace filter lists; until the
// command's Prefill answers it shows the filter's single namespace, else "default".
func (f *createForm) addNamespace() {
	f.caption("NAMESPACE")
	f.ns = f.namespaceField()
	ns := "default"
	if filter := f.sh.ui.Namespaces; len(filter) == 1 {
		ns = filter[0]
	}
	f.ns.SetText(ns)
	f.ns.OnChanged = func(ns string) {
		for _, fn := range f.onNamespace {
			fn(strings.TrimSpace(ns))
		}
		f.check()
	}
	f.content.Add(f.ns)
}

// setNamespace sets the namespace; the entry's OnChanged tells those that depend on it.
func (f *createForm) setNamespace(ns string) {
	if f.ns != nil {
		f.ns.SetText(ns)
	}
}

// namespace is the namespace the object lands in; "" for a cluster-scoped kind.
func (f *createForm) namespace() string {
	if f.ns == nil {
		return ""
	}
	return strings.TrimSpace(f.ns.Text)
}

// params are the command's parameters from the dialog's fields. The command checks them; an error
// here is one the parameters cannot even express (a resource limited twice).
func (f *createForm) params() (map[string]any, error) {
	p := map[string]any{}
	if f.name != nil {
		p[paramName] = strings.TrimSpace(f.name.Text)
	}
	if f.ns != nil {
		p[paramNamespace] = f.namespace()
	}
	for _, w := range f.writers {
		if err := w(p); err != nil {
			return p, err
		}
	}
	return p, nil
}

// paramsAsIs are the parameters as far as the fields give them, for the access check and the
// Prefill, which judge the namespace only.
func (f *createForm) paramsAsIs() map[string]any {
	p, _ := f.params()
	return p
}

// check asks the command's Enabled, off the main goroutine, whether it may run in the chosen
// namespace: the access reviews' denial greys Create and says why.
func (f *createForm) check() {
	f.checkSeq++
	seq := f.checkSeq
	raw := formJSON(f.paramsAsIs())
	go func() {
		defer f.sh.recoverTo(f.cmd.ID)
		err := f.sh.commandEnabled(f.cmd.ID, raw)
		var perr *engine.ParamsError
		reason := ""
		if err != nil && !errors.As(err, &perr) { // incomplete fields are the command's to judge on Create
			reason = err.Error()
		}
		f.sh.do(func() {
			if seq == f.checkSeq {
				f.denied = reason
				f.refresh()
			}
		})
	}()
}

// setMessage shows the outcome of a Create, or why it could not be sent.
func (f *createForm) setMessage(text string, c fyne.ThemeColorName) {
	f.message, f.msgColor = text, c
	f.refresh()
}

// refresh shows the dialog's state: the denial first, else the last message; Create greyed while
// denied or busy.
func (f *createForm) refresh() {
	text, c := f.message, f.msgColor
	if f.denied != "" {
		text, c = f.denied, colorStatusError
	}
	if f.status.text != text || f.status.color != c {
		f.status.text, f.status.color = text, c
		f.status.Refresh()
		f.fit()
	}
	if f.denied != "" || f.busy {
		f.create.Disable()
	} else {
		f.create.Enable()
	}
}

// submit dispatches the command with the dialog's fields. The dialog closes when the object was
// created; otherwise it stays with the reason (a declined confirmation is the user's answer).
func (f *createForm) submit() {
	if f.busy || f.denied != "" {
		return
	}
	params, err := f.params()
	if err != nil {
		f.setMessage(err.Error(), colorStatusError)
		return
	}
	f.busy = true
	f.setMessage("Creating…", colorMuted)
	f.sh.dispatch(f.cmd.ID, params, func(_ any, err error) {
		f.busy = false
		var dis *engine.DisabledError
		switch {
		case err == nil:
			f.message = ""
			f.d.Hide()
			return
		case errors.Is(err, engine.ErrDeclined):
			f.setMessage("Not created: the confirmation was declined.", colorMuted)
		case errors.As(err, &dis):
			f.setMessage(dis.Reason, colorStatusError)
		default:
			f.setMessage(err.Error(), colorStatusError)
		}
	})
}

// loadNames offers the names of a kind's objects from Cogpit's cache (resources.list), in the
// given namespaces (none: every namespace). On an error nothing is offered: the user types the
// name.
func (f *createForm) loadNames(kind string, namespaces []string, done func([]model.ObjectRef)) {
	f.sh.dispatch("resources.list", createListParams{Kind: kind, Namespaces: namespaces, Limit: createListLimit}, func(res any, err error) {
		if err != nil {
			return
		}
		var out struct {
			Rows []struct {
				Ref model.ObjectRef `json:"ref"`
			} `json:"rows"`
		}
		if decode(res, &out) != nil {
			return
		}
		refs := make([]model.ObjectRef, 0, len(out.Rows))
		for _, r := range out.Rows {
			if r.Ref.Kind == kind {
				refs = append(refs, r.Ref)
			}
		}
		done(refs)
	})
}

// offer sets the names a field offers, and remembers them (Fyne's SelectEntry does not say).
func (f *createForm) offer(e *widget.SelectEntry, options []string) {
	if f.offered == nil {
		f.offered = map[*widget.SelectEntry][]string{}
	}
	f.offered[e] = options
	e.SetOptions(options)
}

// names returns the refs' names in their order.
func names(refs []model.ObjectRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Name)
	}
	return out
}

// splitList splits a comma-separated field into its entries, trimmed, without empty ones.
func splitList(text string) []string {
	out := []string{}
	for _, s := range strings.Split(text, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// apiGroupsOf reads a rule's API groups: empty is the core group, as is "core" (no API group has
// that name).
func apiGroupsOf(text string) []string {
	groups := splitList(text)
	if len(groups) == 0 {
		return []string{""}
	}
	for i, g := range groups {
		if g == "core" || g == `""` {
			groups[i] = ""
		}
	}
	return groups
}

// rowEditor is an editable list of rows under column captions: each row a few fields and a remove
// button, an add button below. Rows whose fields are all empty are left out of the parameters.
type rowEditor struct {
	f      *createForm
	newRow func() *editorRow
	rows   []*editorRow
	list   *fyne.Container
	scroll *container.Scroll
}

// editorRow is one row of a rowEditor.
type editorRow struct {
	fields []fyne.CanvasObject
	value  func() (any, bool) // the row's value; false when the row is empty
	box    fyne.CanvasObject
}

// addRows adds a list of rows with its captions, an add button labelled add, and one empty row.
func (f *createForm) addRows(caption string, columns []string, add string, newRow func() *editorRow) *rowEditor {
	f.caption(caption)
	e := &rowEditor{f: f, newRow: newRow, list: container.NewVBox()}
	heads := make([]fyne.CanvasObject, 0, len(columns))
	for _, c := range columns {
		heads = append(heads, newTextLine(c, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
	}
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(removeButton(nil).MinSize())
	f.content.Add(container.NewBorder(nil, nil, nil, gap, container.NewGridWithColumns(len(columns), heads...)))
	e.scroll = container.NewVScroll(e.list)
	f.content.Add(e.scroll)
	addButton := widget.NewButtonWithIcon(add, theme.ContentAddIcon(), func() { e.add(); f.fit() })
	addButton.Importance = widget.LowImportance
	f.content.Add(container.NewHBox(addButton))
	e.add()
	f.editors = append(f.editors, e)
	return e
}

// removeButton is a row's remove button.
func removeButton(tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon("", theme.DeleteIcon(), tapped)
	b.Importance = widget.LowImportance
	return b
}

// add adds an empty row.
func (e *rowEditor) add() *editorRow {
	r := e.newRow()
	r.box = container.NewBorder(nil, nil, nil, removeButton(func() { e.remove(r) }), container.NewGridWithColumns(len(r.fields), r.fields...))
	e.rows = append(e.rows, r)
	e.list.Add(r.box)
	e.sized()
	return r
}

// remove removes a row.
func (e *rowEditor) remove(r *editorRow) {
	e.rows = slices.DeleteFunc(e.rows, func(x *editorRow) bool { return x == r })
	e.list.Remove(r.box)
	e.sized()
	e.f.fit()
}

// sized lets the rows scroll beyond the height of the tallest list a popup shows.
func (e *rowEditor) sized() {
	most := e.f.sh.app.Settings().Theme().Size(sizePopupMaxList)
	e.scroll.SetMinSize(fyne.NewSize(0, min(e.list.MinSize().Height, most)))
}

// values returns the rows' values, empty rows left out.
func (e *rowEditor) values() []any {
	out := make([]any, 0, len(e.rows))
	for _, r := range e.rows {
		if v, ok := r.value(); ok {
			out = append(out, v)
		}
	}
	return out
}

// addHint adds a muted line that explains the fields above it.
func (f *createForm) addHint(text string) {
	f.content.Add(newParagraph(text, colorMuted, f.width))
}

// buildRoleForm builds the Role and Cluster Role dialogs: the name, a role's namespace, and its
// rules, one per row: API groups, resources and verbs, each comma-separated. In a cluster role a
// resource that starts with "/" is a non-resource URL.
func buildRoleForm(f *createForm, cluster bool) {
	f.addName("pod-reader")
	if !cluster {
		f.addNamespace()
	}
	resourcesHint := "pods, services"
	if cluster {
		resourcesHint = "pods, /healthz"
	}
	rules := f.addRows("RULES", []string{"API groups", "Resources", "Verbs"}, "Add Rule", func() *editorRow {
		groups, resources, verbs := widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
		groups.SetPlaceHolder("core")
		resources.SetPlaceHolder(resourcesHint)
		verbs.SetPlaceHolder("get, list, watch")
		return &editorRow{fields: []fyne.CanvasObject{groups, resources, verbs}, value: func() (any, bool) {
			if strings.TrimSpace(groups.Text+resources.Text+verbs.Text) == "" {
				return nil, false
			}
			rule := map[string]any{"verbs": splitList(verbs.Text)}
			var urls, res []string
			for _, r := range splitList(resources.Text) {
				if cluster && strings.HasPrefix(r, "/") {
					urls = append(urls, r)
				} else {
					res = append(res, r)
				}
			}
			if len(urls) > 0 {
				rule["nonResourceURLs"] = urls
				if strings.TrimSpace(groups.Text) == "" && len(res) == 0 {
					return rule, true // a URL rule names no API group
				}
			}
			rule["apiGroups"], rule["resources"] = apiGroupsOf(groups.Text), append([]string{}, res...)
			return rule, true
		}}
	})
	hint := "Separate entries with commas. API groups: empty for the core group (pods, services), apps for deployments, * for every group."
	if cluster {
		hint += " A resource that starts with / is a non-resource URL."
	}
	f.addHint(hint)
	f.writers = append(f.writers, func(p map[string]any) error {
		p["rules"] = rules.values()
		return nil
	})
}

// buildBindingForm builds the Role Binding and Cluster Role Binding dialogs: a role binding's
// namespace, the role (a role binding's of either kind, offered from the cache), the binding's
// name (the role's until the user types one), and its subjects, one per row: kind, name and, for a
// service account, its namespace.
func buildBindingForm(f *createForm, cluster bool) {
	var (
		roleKind  *selectField
		roleName  = widget.NewSelectEntry(nil)
		autoName  string
		accounts  = map[string][]string{} // service account names by namespace, from the cache
		roleSeq   int
		subjects  *rowEditor
		refreshes []func()
	)
	f.roleName = roleName
	roleName.SetPlaceHolder("view")
	if cluster {
		f.caption("CLUSTER ROLE")
		f.content.Add(roleName)
	} else {
		f.addNamespace()
		f.caption("ROLE")
		roleKind = newSelectField([]string{roleKindRole, roleKindClusterRole}, nil)
		f.roleKind = roleKind
		roleKind.SetSelected(roleKindRole)
		f.content.Add(container.NewBorder(nil, nil, roleKind, nil, roleName))
	}
	loadRoles := func() {
		roleSeq++
		seq := roleSeq
		kind, namespaces := kindClusterRoles, []string(nil)
		if roleKind != nil && roleKind.Selected == roleKindRole {
			kind, namespaces = kindRoles, []string{f.namespace()}
		}
		f.loadNames(kind, namespaces, func(refs []model.ObjectRef) {
			if seq == roleSeq {
				f.offer(roleName, names(refs))
			}
		})
	}
	if roleKind != nil {
		roleKind.OnChanged = func(string) { loadRoles() }
		f.onNamespace = append(f.onNamespace, func(string) {
			if roleKind.Selected == roleKindRole {
				loadRoles()
			}
			for _, fn := range refreshes {
				fn()
			}
		})
	}
	f.addName("")
	f.name.SetPlaceHolder("the role's name")
	roleName.OnChanged = func(role string) {
		role = strings.TrimSpace(role)
		if t := strings.TrimSpace(f.name.Text); t == "" || t == autoName {
			f.name.SetText(role)
		}
		autoName = role
	}
	subjects = f.addRows("SUBJECTS", []string{"Kind", "Name", "Namespace"}, "Add Subject", func() *editorRow {
		kind := newSelectField([]string{subjectUser, subjectGroup, subjectServiceAccount}, nil)
		name, ns := widget.NewSelectEntry(nil), f.namespaceField()
		hint := "the binding's" // a role binding's service account defaults to its namespace
		if cluster {
			hint = "its namespace"
		}
		sync := func() {
			if kind.Selected != subjectServiceAccount {
				ns.SetPlaceHolder("") // users and groups have no namespace
				ns.Disable()
				f.offer(name, nil)
				return
			}
			ns.SetPlaceHolder(hint)
			ns.Enable()
			where := strings.TrimSpace(ns.Text)
			if where == "" {
				where = f.namespace()
			}
			f.offer(name, accounts[where])
		}
		kind.OnChanged = func(string) { sync() }
		ns.OnChanged = func(string) { sync() }
		refreshes = append(refreshes, sync)
		kind.SetSelected(subjectUser)
		return &editorRow{fields: []fyne.CanvasObject{kind, name, ns}, value: func() (any, bool) {
			n := strings.TrimSpace(name.Text)
			if n == "" {
				return nil, false
			}
			sub := map[string]any{paramKind: kind.Selected, paramName: n}
			if where := strings.TrimSpace(ns.Text); kind.Selected == subjectServiceAccount && where != "" {
				sub[paramNamespace] = where
			}
			return sub, true
		}}
	})
	f.addHint("The subjects hold the role's permissions as soon as the binding exists.")
	loadRoles()
	f.loadNames(kindServiceAccounts, nil, func(refs []model.ObjectRef) {
		for _, r := range refs {
			accounts[r.Namespace] = append(accounts[r.Namespace], r.Name)
		}
		for _, fn := range refreshes {
			fn()
		}
	})
	f.writers = append(f.writers, func(p map[string]any) error {
		role := strings.TrimSpace(roleName.Text)
		if cluster {
			p["clusterRole"] = role
		} else {
			p["roleRef"] = map[string]any{paramKind: roleKind.Selected, paramName: role}
		}
		p["subjects"] = subjects.values()
		return nil
	})
}

// buildQuotaForm builds the Resource Quota dialog: the name, the namespace and its hard limits,
// one per row: a resource (Kubernetes' standard names offered) and its quantity.
func buildQuotaForm(f *createForm) {
	f.addName("compute")
	f.addNamespace()
	limits := f.addRows("HARD LIMITS", []string{"Resource", "Limit"}, "Add Limit", func() *editorRow {
		res, qty := widget.NewSelectEntry(quotaResources), widget.NewEntry()
		res.SetPlaceHolder("pods")
		qty.SetPlaceHolder("10")
		return &editorRow{fields: []fyne.CanvasObject{res, qty}, value: func() (any, bool) {
			r, q := strings.TrimSpace(res.Text), strings.TrimSpace(qty.Text)
			if r == "" && q == "" {
				return nil, false
			}
			return [2]string{r, q}, true
		}}
	})
	f.addHint("Quantities as Kubernetes writes them: 10, 500m, 4Gi.")
	f.writers = append(f.writers, func(p map[string]any) error {
		hard := map[string]string{}
		for _, v := range limits.values() {
			kv := v.([2]string)
			if _, twice := hard[kv[0]]; twice {
				return fmt.Errorf("%s is limited twice", kv[0])
			}
			hard[kv[0]] = kv[1]
		}
		p["hard"] = hard
		return nil
	})
}
