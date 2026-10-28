package fyneui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Port forwarding in the GUI (ui-parity › Port forwarding): the forward dialog after Lens's
// (capture, 2026-10-11), opened by "Forward…" beside a pod's or a service's port and by Edit in
// the Port Forwarding view; a port's link, which forwards at once to a free port and opens the
// browser; and Open, which opens a forward's URL, also from a forwarded port's link in the drawer,
// whose stop icon runs portforward.stop (D-140). The engine's commands do the work
// (internal/engine/commands/portforward.go); the GUI only asks and opens the browser.

// kindPortForwards is the Port Forwarding view's kind key (engine.PortForwardKind).
const kindPortForwards = "portforwards"

func init() {
	itemActionDialogs["pod.portForward"] = openForward
	itemActionDialogs["service.portForward"] = openForward
	itemActionDialogs["portforward.edit"] = openForward
	itemActionDialogs["portforward.open"] = func(sh *shell, _ model.ObjectRef, a model.ItemAction) {
		sh.dispatch(a.Command, a.Params, func(res any, err error) { sh.forwardDone(res, err, true) })
	}
}

// openBrowser opens a URL in the user's browser; tests replace it.
var openBrowser = func(a fyne.App, u *url.URL) error { return a.OpenURL(u) }

// forwardParams are the parameters of the forward commands the dialog reads and sets.
type forwardParams struct {
	Ref           model.ObjectRef `json:"ref"`
	Container     string          `json:"container,omitempty"`
	Port          *uint16         `json:"port,omitempty"`
	LocalPort     *uint16         `json:"localPort,omitempty"`
	HTTPS         *bool           `json:"https,omitempty"`
	OpenInBrowser *bool           `json:"openInBrowser,omitempty"`
}

// forwardResult is what the forward commands return that the GUI needs.
type forwardResult struct {
	URL  string `json:"url"`
	Spec struct {
		Target        model.ObjectRef `json:"target"`
		OpenInBrowser bool            `json:"openInBrowser"`
	} `json:"spec"`
}

// openForward runs a forward action: a port's link forwards at once to a free port and opens the
// browser; "Forward…" and Edit open the forward dialog first.
func openForward(sh *shell, ref model.ObjectRef, a model.ItemAction) {
	if a.Link {
		sh.dispatch(a.Command, a.Params, func(res any, err error) { sh.forwardDone(res, err, false) })
		return
	}
	var p forwardParams
	if len(a.Params) > 0 {
		if err := json.Unmarshal(a.Params, &p); err != nil {
			sh.toast("error", fmt.Sprintf("%s: %v", a.Label, err))
			return
		}
	}
	if p.Ref.Kind == "" {
		p.Ref = ref
	}
	sh.showForwardDialog(a.Command, p)
}

// forwardTargetName is what the dialog's title names: the pod or service, for Edit the one the
// forward goes to (its row in the Port Forwarding view says).
func (sh *shell) forwardTargetName(ref model.ObjectRef) string {
	if ref.Kind != kindPortForwards {
		return ref.Name
	}
	if snap := sh.eng.Table(kindPortForwards); snap != nil {
		for _, r := range snap.Rows {
			if r.Ref.Same(ref) && len(r.Cells) > 0 && r.Cells[0].Text != "" {
				return r.Cells[0].Text
			}
		}
	}
	return ref.Name
}

// showForwardDialog is the forward dialog as captured: titled "Port Forwarding for <pod>", a
// local port field ("Random" when empty), an "https" check (off) and an "Open in Browser" check
// (on), Cancel and Start; Fyne's dialog dims the window behind it. Edit starts from the forward's
// settings.
func (sh *shell) showForwardDialog(command string, p forwardParams) {
	local := newFieldEntry() // Escape in it cancels the dialog (escape.go)
	local.SetPlaceHolder("Random")
	if p.LocalPort != nil && *p.LocalPort != 0 {
		local.SetText(strconv.Itoa(int(*p.LocalPort)))
	}
	local.Validator = func(s string) error {
		_, err := parseLocalPort(s)
		return err
	}
	https := newFieldCheck("https")
	https.SetChecked(p.HTTPS != nil && *p.HTTPS)
	open := newFieldCheck("Open in Browser")
	open.SetChecked(p.OpenInBrowser == nil || *p.OpenInBrowser)
	items := []*widget.FormItem{
		{Text: "Local port", Widget: local},
		{Text: "", Widget: https},
		{Text: "", Widget: open},
	}
	title := "Port Forwarding for " + sh.forwardTargetName(p.Ref)
	d := dialog.NewForm(title, "Start", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		port, err := parseLocalPort(local.Text)
		if err != nil {
			sh.toast("error", err.Error())
			return
		}
		h, o := https.Checked, open.Checked
		p.LocalPort, p.HTTPS, p.OpenInBrowser = &port, &h, &o
		sh.dispatch(command, p, func(res any, err error) { sh.forwardDone(res, err, false) })
	}, sh.win)
	th := sh.app.Settings().Theme()
	d.Resize(fyne.NewSize(th.Size(sizeDialogWidth), d.MinSize().Height))
	sh.showDialog(d)
}

// parseLocalPort reads the local port field: empty is 0, a free port.
func parseLocalPort(s string) (uint16, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil || n == 0 {
		return 0, errors.New("a port from 1 to 65535, or empty for a free one")
	}
	return uint16(n), nil
}

// forwardDone handles a forward command's result on the main goroutine: an error as a toast,
// else the browser on the forward's URL when it asks for it (or always, for Open).
func (sh *shell) forwardDone(res any, err error, always bool) {
	if err != nil {
		sh.showError(err)
		return
	}
	var r forwardResult
	if decode(res, &r) != nil || r.URL == "" || (!always && !r.Spec.OpenInBrowser) {
		return
	}
	sh.openForwardURL(r.URL)
}

// openForwardURL opens a forward's URL, and only such a URL: http or https on 127.0.0.1, where
// forwards listen. Not localhost: it resolves to ::1 first, where another process may listen.
func (sh *shell) openForwardURL(raw string) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() != "127.0.0.1" {
		sh.toast("error", "not a port forward's address: "+raw)
		return
	}
	if err := openBrowser(sh.app, u); err != nil {
		sh.toast("error", "cannot open "+raw+": "+err.Error())
	}
}
