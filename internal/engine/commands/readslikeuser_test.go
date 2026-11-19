package commands_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The built-in agent over a connection the user marked private reads like the user (D-10, H-94;
// the owner's decision): every read path that withholds from agents gives such a turn what the
// window shows, while every write rule, the GUI-only commands, a Secret's Edit and Create tabs and
// a terminal tab's switch stay as they are for any agent.

// reader is who dispatches a read: an origin and Invocation.ReadsLikeUser as set in Go.
type reader struct {
	name     string
	origin   engine.Origin
	mark     bool
	likeUser bool // the reads return what the window shows
}

// readers are the four the owner's rule names, and an external agent whose Invocation carries the
// mark in Go: ignored, filtered as any external agent.
var readers = []reader{
	{"external agent", engine.OriginExternalAgent, false, false},
	{"external agent with the mark set in Go", engine.OriginExternalAgent, true, false},
	{"built-in agent", engine.OriginBuiltInAgent, false, false},
	{"built-in agent over a private connection", engine.OriginBuiltInAgent, true, true},
	{"window", engine.OriginGUI, false, true},
}

// privateReader is the built-in agent over a private connection.
var privateReader = readers[3]

// dispatchAs runs a command as r.
func dispatchAs(s *engine.Session, r reader, id string, params any) (any, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: r.origin, ReadsLikeUser: r.mark})
}

// jsonOf is a result as an MCP client or the model reads it, its <withheld…> markers unescaped.
func jsonOf(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return b.String()
}

// checkReads checks one read for every reader: the window's text (value) for those who read like
// the user, never for the others, who read withheld instead when it is set.
func checkReads(t *testing.T, s *engine.Session, site, id string, params any, value, withheld string) {
	t.Helper()
	for _, r := range readers {
		res, err := dispatchAs(s, r, id, params)
		if err != nil {
			t.Errorf("%s, %s: %v", site, r.name, err)
			continue
		}
		got := jsonOf(res)
		switch {
		case r.likeUser && !strings.Contains(got, value):
			t.Errorf("%s, %s: the value is missing: %s", site, r.name, got)
		case !r.likeUser && strings.Contains(got, value):
			t.Errorf("%s, %s: reads the value: %s", site, r.name, got)
		case !r.likeUser && withheld != "" && !strings.Contains(got, withheld):
			t.Errorf("%s, %s: %q missing: %s", site, r.name, withheld, got)
		}
	}
}

// TestPrivateTurnReadsLikeTheUser: the cached Secrets' values (H-82) in an object, a logs tab and
// a window toast, Flux's credential fields (H-63) in an object, an Edit tab and its diff, and the
// text of a Secret's Edit tab the user opened, for every reader.
func TestPrivateTurnReadsLikeTheUser(t *testing.T) {
	h := newAgentViewHarness(t)
	const placeholder = "«secret shop/db/password»"
	// An agent's first read takes the Secrets (H-82, option b): every later filter knows them.
	checkReads(t, h.s, "object_get of a ConfigMap quoting a Secret's value", "object.get", map[string]any{"ref": appCMRef()}, dbPass, placeholder)

	// A logs tab's lines.
	w := &fakeWindow{}
	h.s.SetDockOutputReader(w)
	logs := openDockTab(t, h, "pod.logs", map[string]any{"ref": model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}})
	w.show(logs, "connecting to postgres://app:"+dbPass+"@db:5432/app")
	checkReads(t, h.s, "dock_read of a logs tab", "dock.read", map[string]any{"id": logs}, dbPass, placeholder)

	// A toast the window raised itself (A19).
	h.s.RecordWindowToast(engine.LevelError, "Apply failed: the server said password "+dbPass+" is too short")
	checkReads(t, h.s, "notifications_list of a window toast", "notifications.list", struct{}{}, dbPass, placeholder)

	// Flux's credential fields.
	checkReads(t, h.s, "object_get of a Flux Provider", "object.get", map[string]any{"ref": providerRef()}, hookToken, model.Withheld)
	checkReads(t, h.s, "object_get of a Flux GitRepository", "object.get", map[string]any{"ref": gitRef()}, gitToken, "<withheld>")
	res, err := dispatchAs(h.s, privateReader, "object.edit", map[string]any{"ref": hrRef()})
	if err != nil || !strings.Contains(res.(commands.EditResult).Text, hrValuePass) {
		t.Fatalf("object_edit of a Flux HelmRelease for the private turn: %v %s", err, jsonOf(res))
	}
	edit := res.(commands.EditResult)
	checkReads(t, h.s, "editor_get of a Flux HelmRelease's Edit tab", "editor.get", map[string]any{"id": edit.Tab.ID}, hrValuePass, model.Withheld)
	var st engine.EditorState
	h.must(t, engine.OriginGUI, "editor.get", map[string]any{"id": edit.Tab.ID}, &st)
	h.must(t, engine.OriginGUI, "editor.setText", map[string]any{"id": edit.Tab.ID, "yaml": strings.Replace(st.Text, hrValuePass, "changed-by-user-1", 1)}, nil)
	checkReads(t, h.s, "editor_diff of a Flux HelmRelease's Edit tab", "editor.diff", map[string]any{"id": edit.Tab.ID}, "changed-by-user-1", "")

	// The text of a Secret's Edit tab the user opened: the private turn reads it (the lead's
	// decision for the owner to confirm), the other agents read <withheld>.
	secretRef := model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "shop", Name: "db"}
	var secretTab commands.EditResult
	h.must(t, engine.OriginGUI, "object.edit", map[string]any{"ref": secretRef}, &secretTab)
	encoded := base64.StdEncoding.EncodeToString([]byte(dbPass))
	checkReads(t, h.s, "editor_get of a Secret's Edit tab", "editor.get", map[string]any{"id": secretTab.Tab.ID}, encoded, model.Withheld)
}

// TestPrivateTurnReadsASecret: object_get of a Secret gives the private turn each value as the
// drawer's Show reveals it and the YAML as an Edit tab holds it; every other agent reads the keys,
// masked values and redacted YAML; the window's own object_get keeps the drawer's masked values,
// and only its secret.reveal shows one, which stays the window's.
func TestPrivateTurnReadsASecret(t *testing.T) {
	h := newAgentViewHarness(t)
	ref := model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "shop", Name: "db"}
	encoded := base64.StdEncoding.EncodeToString([]byte(dbPass))
	for _, r := range readers {
		res, err := dispatchAs(h.s, r, "object.get", map[string]any{"ref": ref})
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		m := res.(*engine.DetailModel)
		got := jsonOf(m)
		if r == privateReader {
			if v := dataValue(m, "password"); v != dbPass || !strings.Contains(m.YAML, encoded) || dataValue(m, "username") != "postgres" {
				t.Errorf("%s: password %q, YAML %s", r.name, v, m.YAML)
			}
			continue
		}
		if strings.Contains(got, dbPass) || strings.Contains(got, encoded) || dataValue(m, "password") == "" ||
			!strings.HasPrefix(dataValue(m, "password"), engine.MaskedValue) || !strings.Contains(m.YAML, model.Redacted) {
			t.Errorf("%s: %s", r.name, got)
		}
	}

	// secret.reveal stays a window action: refused to the private turn before anything is read.
	h.must(t, engine.OriginGUI, "ui.navigate", map[string]any{"ref": ref}, nil)
	reveal := map[string]any{"ref": ref, "key": "password"}
	var ge *engine.GUIOnlyError
	if _, err := dispatchAs(h.s, privateReader, engine.RevealCommand, reveal); !errors.As(err, &ge) {
		t.Errorf("secret.reveal for the private turn: %v", err)
	}
	var v engine.SecretValue
	h.must(t, engine.OriginGUI, engine.RevealCommand, reveal, &v)
	if v.Value != dbPass {
		t.Errorf("the window's reveal: %+v", v)
	}
}

// dataValue is the value of key in a Secret drawer's Data section.
func dataValue(m *engine.DetailModel, key string) string {
	for _, sec := range m.Sections {
		if sec.Title != "Data" {
			continue
		}
		for _, it := range sec.Items {
			if it.Name == key {
				return it.Value
			}
		}
	}
	return ""
}

// TestPrivateTurnKeepsTheWriteRules: what the private turn reads changes nothing it may do. A
// Secret's Edit tab the user opened is the user's to change, check and apply; the private turn
// cannot open one, create a Secret, write back a placeholder, or read a terminal tab whose switch
// is off.
func TestPrivateTurnKeepsTheWriteRules(t *testing.T) {
	h := newAgentViewHarness(t)
	secretRef := model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "shop", Name: "db"}
	for _, ref := range []model.ObjectRef{secretRef, appCMRef()} { // waits for the kinds to be listed
		h.must(t, engine.OriginExternalAgent, "object.get", map[string]any{"ref": ref}, nil)
	}
	var tab commands.EditResult
	h.must(t, engine.OriginGUI, "object.edit", map[string]any{"ref": secretRef}, &tab)
	secretYAML := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: x\n  namespace: shop\nstringData:\n  k: v-0123456789\n"
	for _, c := range []struct {
		id     string
		params any
	}{
		{"object.edit", map[string]any{"ref": secretRef}},
		{"editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "data: {}\n"}},
		{"editor.diff", map[string]any{"id": tab.Tab.ID}},
		{"object.dryRun", map[string]any{"id": tab.Tab.ID}},
		{"object.apply", map[string]any{"id": tab.Tab.ID, "textRevision": 1}},
		{"editor.revert", map[string]any{"id": tab.Tab.ID}},
		{"resource.createTab", map[string]any{"yaml": secretYAML}},
		{"object.create", map[string]any{"yaml": secretYAML}},
	} {
		if _, err := dispatchAs(h.s, privateReader, c.id, c.params); !errors.Is(err, engine.ErrSecretGUIOnly) {
			t.Errorf("%s for the private turn: %v, want ErrSecretGUIOnly", c.id, err)
		}
	}
	var ge *engine.GUIOnlyError
	if _, err := dispatchAs(h.s, privateReader, engine.SecretCreateCommand, tlsSecretParams("shop")); !errors.As(err, &ge) || !errors.Is(err, engine.ErrSecretGUIOnly) {
		t.Errorf("secret.create for the private turn: %v", err)
	}

	// A placeholder from a kept conversation's scrubbed history is not written back.
	var cm commands.EditResult
	h.must(t, engine.OriginGUI, "object.edit", map[string]any{"ref": appCMRef()}, &cm)
	if _, err := dispatchAs(h.s, privateReader, "editor.setText", map[string]any{"id": cm.Tab.ID, "yaml": "data:\n  dsn: «secret shop/db/password»\n"}); !errors.Is(err, engine.ErrAgentMarkerWrite) {
		t.Errorf("a placeholder written back by the private turn: %v", err)
	}

	// A terminal tab still needs the user's switch (H-83).
	w := &fakeWindow{}
	h.s.SetDockOutputReader(w)
	term := openDockTab(t, h, "terminal.open", struct{}{})
	w.show(term, "$ export TOKEN="+dbPass)
	if out, err := readDock(t, h, engine.OriginGUI, map[string]any{"id": term}); err != nil || !strings.Contains(strings.Join(out.Lines, "\n"), dbPass) {
		t.Fatalf("the window's read: %q %v", out.Lines, err)
	}
	reads := w.reads
	res, err := dispatchAs(h.s, privateReader, "dock.read", map[string]any{"id": term})
	if !errors.Is(err, engine.ErrTerminalOutputWithheld) || strings.Contains(jsonOf(res), dbPass) || w.reads != reads {
		t.Errorf("a terminal tab without its switch for the private turn: %v, %s, %d reads", err, jsonOf(res), w.reads-reads)
	}
	if _, err := dispatchAs(h.s, privateReader, "dock.allowAgentRead", map[string]any{"id": term, "allowed": true}); !errors.As(err, &ge) {
		t.Errorf("the switch from the private turn: %v", err)
	}
}

// TestPrivateTurnReadsHelmValues: a release's values (helm.release.values, the drawer's private
// items) and a Helm Upgrade tab's text for every reader; the private turn's audit lines are marked
// and carry no value.
func TestPrivateTurnReadsHelmValues(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	const value = "adminPassword: hunter2"
	checkReads(t, h.s, "helm.release.values", "helm.release.values", map[string]any{"ref": webRef}, value, model.Withheld)
	checkReads(t, h.s, "object_get of a release", "object.get", map[string]any{"ref": webRef}, "hunter2", model.Withheld)
	tab := openUpgradeTab(t, h, engine.OriginGUI)
	checkReads(t, h.s, "editor_get of a Helm Upgrade tab", "editor.get", map[string]any{"id": tab.Tab.ID}, value, model.Withheld)
	res, err := dispatchAs(h.s, privateReader, helm.UpgradeTabCommand, map[string]any{"ref": webRef})
	if err != nil || !strings.Contains(res.(commands.EditResult).Text, value) {
		t.Errorf("the Upgrade tab opened by the private turn: %v %s", err, jsonOf(res))
	}

	marked := 0
	for _, e := range h.audit.Lines() {
		if b := jsonOf(e); strings.Contains(b, "hunter2") {
			t.Errorf("audit line with a value: %s", b)
		}
		if e.ReadsLikeUser {
			marked++
			if e.Origin != engine.OriginBuiltInAgent {
				t.Errorf("a %s line marked: %+v", e.Origin, e)
			}
		}
	}
	if marked != 4 { // three reads and the tab
		t.Errorf("%d audit lines marked readsLikeUser, want 4", marked)
	}
}

// TestPrivateTurnReadsAHelmError: a failed Helm install's error, what helm_pendingOperations says
// of it, and a preview rendered against the cluster as the window's: the private turn reads them
// as the window does, every other agent the withheld text and an offline preview; the private
// turn's audit lines say what failed, never the text.
func TestPrivateTurnReadsAHelmError(t *testing.T) {
	h, fi := installHarness(t)
	h.answer = true
	const lookedUp = "LOOKED-UP-SECRET-DATA"
	fi.err = errors.New("helm install: template: web/templates/cm.yaml: name " + lookedUp + " is not valid")
	installErr := errors.New("helm install: template: web/templates/cm.yaml: name " + lookedUp + " is not valid")
	params := func(i int) map[string]any {
		return map[string]any{"repository": "test", "chart": "web", "version": "0.2.0", "values": "a: b\n", "namespace": "apps", "name": fmt.Sprintf("web-%d", i)}
	}
	// The preview renders against the cluster only for those who read like the user: an agent's
	// is offline, so that a chart's lookup has nothing to quote (the fake fails either way, so its
	// error is set for the private turn's preview alone).
	for i, r := range readers {
		fi.err = nil
		if r == privateReader {
			fi.err = installErr
		}
		previews := len(fi.offline)
		_, err := dispatchAs(h.s, r, "helm.chart.preview", params(i))
		if len(fi.offline) != previews+1 || fi.offline[previews] == r.likeUser {
			t.Errorf("%s: previews offline %v, want offline %v", r.name, fi.offline[previews:], !r.likeUser)
		}
		if r == privateReader && (err == nil || !strings.Contains(err.Error(), lookedUp)) {
			t.Errorf("the private turn's preview error: %v", err)
		}
	}
	fi.err = installErr
	for i, r := range readers {
		_, err := dispatchAs(h.s, r, "helm.chart.install", params(i))
		switch {
		case err == nil:
			t.Errorf("%s: the install did not fail", r.name)
		case r.likeUser != strings.Contains(err.Error(), lookedUp):
			t.Errorf("%s: the install's error %v", r.name, err)
		}
	}
	for _, r := range readers {
		res, err := dispatchAs(h.s, r, "helm.pendingOperations", struct{}{})
		list, _ := res.([]engine.PendingHelmOpInfo)
		quoted := slices.ContainsFunc(list, func(op engine.PendingHelmOpInfo) bool { return strings.Contains(op.Error, lookedUp) })
		if err != nil || len(list) != len(readers) || quoted != r.likeUser {
			t.Errorf("%s: helm_pendingOperations %+v %v", r.name, list, err)
		}
	}
	if raw := bufText(h.audit); strings.Contains(raw, lookedUp) {
		t.Errorf("the audit log carries the error's text:\n%s", raw)
	}
	failed := 0
	for _, e := range h.audit.Lines() {
		if e.ReadsLikeUser && e.Phase == "done" && strings.Contains(e.Error, "failed; its error") {
			failed++
		}
	}
	if failed != 2 { // the install and the preview
		t.Errorf("%d marked audit lines say what failed, want 2:\n%s", failed, bufText(h.audit))
	}
}

// TestPrivateTurnReadmeStaysFramed (A31, H-86): a chart's README reaches the private turn framed
// as third-party text, as it reaches every agent; the window reads it unframed.
func TestPrivateTurnReadmeStaysFramed(t *testing.T) {
	h := newChartHarness(t, nil)
	h.repo.Add(docsChart("1.0.0", docsReadme))
	h.addRepo(t)
	h.charts(t, 2)
	for _, r := range readers {
		res, err := dispatchAs(h.s, r, "object.get", map[string]any{"ref": docsRef, "noYaml": true, "related": r.mark})
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		var m engine.DetailModel
		if err := json.Unmarshal([]byte(jsonOf(res)), &m); err != nil {
			t.Fatal(err)
		}
		framed := engine.ReadmeFrameStart + "\n" + docsReadme + "\n" + engine.ReadmeFrameEnd
		if got := readmeText(&m); (r.origin == engine.OriginGUI && got != docsReadme) || (r.origin != engine.OriginGUI && got != framed) {
			t.Errorf("%s: README %q", r.name, got)
		}
	}
}
