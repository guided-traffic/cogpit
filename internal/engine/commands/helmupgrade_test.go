package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/release/common"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// openUpgradeTab opens web's Helm Upgrade tab from origin and returns its id.
func openUpgradeTab(t *testing.T, h *helmHarness, origin engine.Origin) commands.EditResult {
	t.Helper()
	res, err := h.dispatch(origin, helm.UpgradeTabCommand, map[string]any{"ref": webRef})
	if err != nil {
		t.Fatal(err)
	}
	return res.(commands.EditResult)
}

// tabRevision is a tab's text revision as editor.get reports it.
func tabRevision(t *testing.T, h *helmHarness, id string) uint64 {
	t.Helper()
	res, err := h.dispatch(engine.OriginGUI, "editor.get", map[string]any{"id": id})
	if err != nil {
		t.Fatal(err)
	}
	return res.(engine.EditorState).Revision
}

// The Upgrade tab, as Lens's (captured 2026-10-12): Upgrade sits between Rollback and Delete; the
// tab holds the chart, the release's own version as the only choice, and the user-supplied values
// from the cache, read again by editor.get; it is focused, not opened twice; Save (the tab id)
// upgrades with the edited values, which replace the old ones, records nothing in the journal once
// it finished, and moves the tab's base to the new revision.
func TestHelmUpgradeTab(t *testing.T) {
	journal := engine.NewMemoryHelmJournal()
	h := newHelmHarness(t, journal, nil)
	actions := map[string]int{}
	for _, c := range h.s.Registry().Actions(helm.ReleaseKind) {
		actions[c.ID] = c.Order
	}
	if actions["helm.release.rollback"] >= actions[helm.UpgradeTabCommand] || actions[helm.UpgradeTabCommand] >= actions["helm.release.uninstall"] {
		t.Errorf("toolbar order %v, want Rollback, Upgrade, Delete", actions)
	}
	if _, ok := actions["helm.release.upgrade"]; ok {
		t.Error("the upgrade itself is in the row menu; the tab's Save runs it")
	}

	tab := openUpgradeTab(t, h, engine.OriginGUI)
	if !tab.Created || tab.Tab.Type != engine.DockHelmUpgrade || tab.Tab.Title != "Helm Upgrade: web" || tab.Tab.Helm == nil ||
		tab.Tab.Helm.Chart != "web" || tab.Tab.Helm.Version != "0.3.0" || strings.Join(tab.Tab.Helm.Versions, ",") != "0.3.0" ||
		!tab.Tab.Helm.UserSuppliedOnly || tab.Text != "adminPassword: hunter2\nreplicas: 3\n" {
		t.Fatalf("tab %+v", tab)
	}
	if again := openUpgradeTab(t, h, engine.OriginGUI); again.Created || again.Tab.ID != tab.Tab.ID {
		t.Errorf("opened twice: %+v", again.Tab)
	}
	if ui := h.s.UIState(); ui.ActiveDock != tab.Tab.ID || len(ui.Dock) != 1 {
		t.Errorf("dock %+v", ui.Dock)
	}
	var st engine.EditorState
	res, _ := h.dispatch(engine.OriginGUI, "editor.get", map[string]any{"id": tab.Tab.ID})
	st = res.(engine.EditorState)
	if st.BaseRevision != 3 || st.CachedRevision != 3 || st.Stale || st.Text != tab.Text {
		t.Errorf("editor state %+v", st)
	}

	if _, err := h.dispatch(engine.OriginGUI, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "adminPassword: hunter3\nreplicas: 5\n"}); err != nil {
		t.Fatal(err)
	}
	res, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID, "textRevision": tabRevision(t, h, tab.Tab.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(commands.HelmResult); r.Action != "upgraded" || r.Revision != 4 {
		t.Errorf("result %+v", r)
	}
	h.waitRelease(t, "apps", "web", 4)
	h.server.mu.Lock()
	up := h.server.upgrades
	h.server.mu.Unlock()
	if len(up) != 1 || up[0].ChartVersion != "0.3.0" || up[0].Values["adminPassword"] != "hunter3" || up[0].Values["replicas"] != float64(5) {
		t.Errorf("upgrade request %+v", up)
	}
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after the upgrade: %+v", list)
	}
	res, _ = h.dispatch(engine.OriginGUI, "editor.get", map[string]any{"id": tab.Tab.ID})
	if st := res.(engine.EditorState); st.BaseRevision != 4 || st.Stale {
		t.Errorf("after the upgrade the tab is based on %d (stale %v)", st.BaseRevision, st.Stale)
	}
	// A second upgrade from the same tab is based on the new revision.
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID, "textRevision": tabRevision(t, h, tab.Tab.ID)}); err != nil {
		t.Fatal(err)
	}
	h.waitRelease(t, "apps", "web", 5)
	toasts := h.eventsOf(engine.EventToast)
	if len(toasts) == 0 || toasts[len(toasts)-1].Message != "Release web upgraded to revision 5" {
		t.Errorf("toasts %+v", toasts)
	}
	for _, ev := range toasts {
		if strings.Contains(ev.Message, "hunter") {
			t.Errorf("a toast quotes a value: %q", ev.Message)
		}
	}
}

// An upgrade is refused when the release moved to another revision since its values were read:
// from a tab (the tab shows it stale), and with values and the revision they are based on.
func TestHelmUpgradeRefusedWhenTheReleaseMoved(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	tab := openUpgradeTab(t, h, engine.OriginGUI)
	h.server.put("apps", "web", 4, common.StatusDeployed) // someone else upgraded
	h.waitRelease(t, "apps", "web", 4)
	res, _ := h.dispatch(engine.OriginGUI, "editor.get", map[string]any{"id": tab.Tab.ID})
	if st := res.(engine.EditorState); !st.Stale || st.BaseRevision != 3 || st.CachedRevision != 4 {
		t.Errorf("editor state %+v", st)
	}
	_, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID, "textRevision": tabRevision(t, h, tab.Tab.ID)})
	if !errors.Is(err, engine.ErrTargetsChanged) || !strings.Contains(err.Error(), "revision 4 now") {
		t.Errorf("tab upgrade after the release moved: %v", err)
	}
	_, err = h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "a: 1", "revision": 3})
	if !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("values upgrade after the release moved: %v", err)
	}
	if calls := h.server.recorded(); len(calls) != 0 {
		t.Errorf("the upgrade ran: %v", calls)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "a: 1", "revision": 4}); err != nil {
		t.Errorf("values upgrade based on the newest revision: %v", err)
	}
}

// For agents: the tab's values and helm.release.values are withheld; an agent's upgrade waits for
// the user's acceptance (mutating, D-2) and names the release, its namespace and context; the
// audit log records it, and editor.setText on the tab, without the values; a declined upgrade
// runs nothing.
func TestHelmUpgradeAgents(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	tab := openUpgradeTab(t, h, engine.OriginExternalAgent)
	if tab.Text != model.Withheld {
		t.Errorf("an agent opened the tab and read %q", tab.Text)
	}
	res, err := h.dispatch(engine.OriginExternalAgent, "editor.get", map[string]any{"id": tab.Tab.ID})
	if err != nil || res.(engine.EditorState).Text != model.Withheld {
		t.Errorf("editor.get for an agent: %+v, %v", res, err)
	}
	res, err = h.dispatch(engine.OriginExternalAgent, "helm.release.values", map[string]any{"ref": webRef, "all": true})
	if err != nil || res.(helm.Values).YAML != model.Withheld {
		t.Errorf("helm.release.values for an agent: %+v, %v", res, err)
	}
	res, err = h.dispatch(engine.OriginGUI, "helm.release.values", map[string]any{"ref": webRef, "all": true})
	if v, _ := res.(helm.Values); err != nil || !strings.Contains(v.YAML, "adminPassword: hunter2") || !strings.Contains(v.YAML, "image: web:1") || v.Revision != 3 {
		t.Errorf("helm.release.values for the GUI: %+v, %v", res, err)
	}
	if _, err := h.dispatch(engine.OriginExternalAgent, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "adminPassword: agent-s3cr3t\n"}); err != nil {
		t.Fatal(err)
	}

	h.mu.Lock()
	h.answer = false
	h.mu.Unlock()
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID, "textRevision": tabRevision(t, h, tab.Tab.ID)}); !errors.Is(err, engine.ErrDeclined) {
		t.Errorf("declined upgrade: %v", err)
	}
	h.mu.Lock()
	h.answer = true
	h.mu.Unlock()
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "adminPassword: agent-s3cr3t-2\n", "revision": 3}); err != nil {
		t.Fatal(err)
	}
	asked := h.confirmations()
	if len(asked) != 2 {
		t.Fatalf("%d confirmations, want 2", len(asked))
	}
	// The values an agent passes inline are shown to the user before they answer, and never
	// serialised with the request (owner, 2026-10-12); from a tab, the tab is what the user reads.
	if asked[0].Review != "" || asked[1].Review != "adminPassword: agent-s3cr3t-2\n" {
		t.Errorf("reviews %q, %q", asked[0].Review, asked[1].Review)
	}
	if b, _ := json.Marshal(asked[1]); strings.Contains(string(b), "s3cr3t") {
		t.Errorf("the request serialises the values: %s", b)
	}
	// Agent writes allowed for the context: an agent's upgrade still waits for the user (A2).
	h.s.SetAgentWrites("test", true)
	h.mu.Lock()
	h.answer = false
	h.mu.Unlock()
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "replicas: 2\n", "revision": 4}); !errors.Is(err, engine.ErrDeclined) {
		t.Errorf("an agent's upgrade with agent writes allowed: %v, want ErrDeclined", err)
	}
	if more := h.confirmations(); len(more) != 3 || !more[2].AgentsAsk {
		t.Errorf("an agent's upgrade with agent writes allowed: asked %+v", more[2:])
	}
	h.mu.Lock()
	h.answer = true
	h.mu.Unlock()
	for _, req := range asked {
		if req.Danger != engine.DangerMutating || req.Context != "test" || len(req.Objects) != 1 || req.Objects[0].Name != "web" ||
			len(req.Namespaces) != 1 || req.Namespaces[0] != "apps" || strings.Contains(req.Message(), "s3cr3t") {
			t.Errorf("confirmation %+v: %s", req, req.Message())
		}
	}
	if !strings.Contains(asked[0].Message(), "Chart: web 0.3.0") || !strings.Contains(asked[0].Message(), "Values: tab "+tab.Tab.ID) {
		t.Errorf("message %q", asked[0].Message())
	}
	if calls := h.server.recorded(); len(calls) != 1 {
		t.Errorf("calls %v", calls)
	}
	lines := h.audit.Lines()
	var cmds []string
	for _, e := range lines {
		b, _ := json.Marshal(e)
		if strings.Contains(string(b), "s3cr3t") || strings.Contains(string(b), "hunter2") {
			t.Errorf("audit line with a value: %s", b)
		}
		cmds = append(cmds, e.Command)
	}
	if !strings.Contains(strings.Join(cmds, ","), "helm.release.upgrade") || !strings.Contains(strings.Join(cmds, ","), "editor.setText") {
		t.Errorf("audited %v", cmds)
	}
}

// Values that do not parse, a pending release, another chart version, or neither tab nor values
// are refused before anything runs; an upgrade the SDK refuses before it changed the release
// (ErrUnchanged) leaves no journal entry; one that fails after it is kept as failed, with its
// error scrubbed of the values.
// An upgrade from a tab is bound to the text revision the user reviewed: an agent that changes
// the tab's text while its upgrade's confirmation is open has the upgrade refused, and an
// upgrade from a tab without a text revision is refused (review of the fourth run, R-2).
func TestHelmUpgradeTabBoundToTheReviewedText(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	tab := openUpgradeTab(t, h, engine.OriginExternalAgent)
	h.mu.Lock()
	h.onAsk = func() {
		if _, err := h.dispatch(engine.OriginExternalAgent, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "image: evil:latest\n"}); err != nil {
			t.Error(err)
		}
	}
	h.mu.Unlock()
	_, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID, "textRevision": tab.Revision})
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("upgrade with the text changed during its confirmation: %v", err)
	}
	h.mu.Lock()
	h.onAsk = nil
	h.mu.Unlock()
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID}); err == nil {
		t.Error("upgrade from a tab without a text revision ran")
	}
	h.server.mu.Lock()
	n := len(h.server.upgrades)
	h.server.mu.Unlock()
	if n != 0 {
		t.Errorf("%d upgrades ran, want none", n)
	}
}

// A release the server holds at a newer revision than the cache has seen yet (a watch that lags)
// is neither upgraded nor rolled back: the actions name the cached revision and the server's
// refusal reads as ErrTargetsChanged, with nothing left in the journal (review of the fourth run,
// R-1).
func TestHelmActionsRefusedWhenTheServerMovedUnseen(t *testing.T) {
	journal := engine.NewMemoryHelmJournal()
	h := newHelmHarness(t, journal, nil)
	tab := openUpgradeTab(t, h, engine.OriginGUI)
	h.server.moveUnseen("apps", "web", 4)
	_, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "tab": tab.Tab.ID, "textRevision": tabRevision(t, h, tab.Tab.ID)})
	if !errors.Is(err, engine.ErrTargetsChanged) || !strings.Contains(err.Error(), "revision 4 on the server") {
		t.Errorf("upgrade while the cache lags: %v", err)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": webRef, "revision": 1}); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("rollback while the cache lags: %v", err)
	}
	h.server.mu.Lock()
	ran := len(h.server.upgrades)
	h.server.mu.Unlock()
	if ran != 0 || len(h.server.recorded()) != 0 {
		t.Errorf("actions ran: %d upgrades, calls %v", ran, h.server.recorded())
	}
	if ops, _ := journal.List(); len(ops) != 0 {
		t.Errorf("journal holds %+v", ops)
	}
}

func TestHelmUpgradeFailures(t *testing.T) {
	journal := engine.NewMemoryHelmJournal()
	h := newHelmHarness(t, journal, nil)
	for _, params := range []map[string]any{
		{"ref": webRef, "values": "a: [hunter2\n", "revision": 3},
		{"ref": webRef, "values": "a: 1", "revision": 3, "version": "9.9.9"},
		{"ref": webRef},
		{"ref": webRef, "values": "a: 1"},
		{"ref": webRef, "tab": "helmUpgrade-99"},
	} {
		_, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", params)
		if err == nil || strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%v: %v", params, err)
		}
	}
	h.server.put("apps", "api", 2, common.StatusPendingUpgrade)
	h.waitRelease(t, "apps", "api", 2)
	apiRef := model.ObjectRef{Context: "test", Kind: helm.ReleaseKind, Namespace: "apps", Name: "api"}
	var dis *engine.DisabledError
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": apiRef, "values": "", "revision": 2}); !errors.As(err, &dis) ||
		!strings.Contains(dis.Reason, "pending-upgrade") {
		t.Errorf("upgrade of a pending release: %v", err)
	}
	if calls := h.server.recorded(); len(calls) != 0 {
		t.Errorf("refused upgrades ran: %v", calls)
	}

	h.server.mu.Lock()
	h.server.failErr = fmt.Errorf("execution error: cannot use hunter3: %w", helm.ErrUnchanged)
	h.server.mu.Unlock()
	_, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "pw: hunter3\n", "revision": 3})
	if !errors.Is(err, helm.ErrUnchanged) || strings.Contains(err.Error(), "hunter3") {
		t.Errorf("an upgrade the SDK refused: %v", err)
	}
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after an unchanged failure: %+v", list)
	}

	h.server.mu.Lock()
	h.server.failErr = errors.New("cannot patch \"web\": the server rejected hunter3")
	h.server.mu.Unlock()
	_, err = h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "pw: hunter3\n", "revision": 3})
	if err == nil || strings.Contains(err.Error(), "hunter3") || !strings.Contains(err.Error(), "may be left pending") {
		t.Errorf("a failed upgrade: %v", err)
	}
	list, _ := journal.List()
	if len(list) != 1 || list[0].Action != engine.HelmUpgrade || list[0].State != engine.HelmOpFailed || list[0].FromRevision != 3 ||
		list[0].Chart != "web" || list[0].ChartVersion != "0.3.0" || strings.Contains(list[0].Error, "hunter3") {
		t.Errorf("journal after a failure: %+v", list)
	}
	for _, ev := range h.eventsOf(engine.EventToast) {
		if strings.Contains(ev.Message, "hunter3") {
			t.Errorf("a toast quotes a value: %q", ev.Message)
		}
	}
}

// D-11 for upgrades: an upgrade a crash interrupted (the release left pending-upgrade above the
// revision it started from) is offered on the next connect and finished by a rollback to that
// revision; one the cache shows deployed above it is dropped as finished; one whose release is
// still at that revision never reached the cluster and is dropped too.
func TestHelmUpgradeCrashJournal(t *testing.T) {
	journal := engine.NewMemoryHelmJournal()
	started := releaseT0.Add(48 * time.Hour)
	for _, op := range []engine.PendingHelmOp{
		{Context: "test", Namespace: "apps", Release: "web", Action: engine.HelmUpgrade, FromRevision: 3, Chart: "web", ChartVersion: "0.3.0",
			Started: started, StartedBy: engine.OriginGUI, State: engine.HelmOpRunning},
		{Context: "test", Namespace: "apps", Release: "api", Action: engine.HelmUpgrade, FromRevision: 1, Chart: "api", ChartVersion: "0.1.0",
			Started: started, StartedBy: engine.OriginExternalAgent, State: engine.HelmOpRunning},
		{Context: "test", Namespace: "apps", Release: "db", Action: engine.HelmUpgrade, FromRevision: 2, Chart: "db", ChartVersion: "0.2.0",
			Started: started, StartedBy: engine.OriginGUI, State: engine.HelmOpRunning},
	} {
		if err := journal.Put(op); err != nil {
			t.Fatal(err)
		}
	}
	h := newHelmHarness(t, journal, func(s *helmServer) {
		s.put("apps", "web", 1, common.StatusSuperseded)
		s.put("apps", "web", 2, common.StatusSuperseded)
		s.put("apps", "web", 3, common.StatusSuperseded)
		s.put("apps", "web", 4, common.StatusPendingUpgrade) // what the crash left
		s.put("apps", "api", 1, common.StatusSuperseded)
		s.put("apps", "api", 2, common.StatusDeployed) // finished before the crash was recorded
		s.put("apps", "db", 1, common.StatusSuperseded)
		s.put("apps", "db", 2, common.StatusDeployed) // never reached the cluster
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		for _, ev := range h.eventsOf(engine.EventHelm) {
			if strings.Contains(ev.Message, "The upgrade of release web in context test did not finish") {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("no offer: %+v", h.eventsOf(engine.EventHelm))
	}
	var notes []string
	for _, ev := range h.eventsOf(engine.EventToast) {
		notes = append(notes, ev.Message)
	}
	if n := strings.Join(notes, "\n"); !strings.Contains(n, "upgrade of release api (context test): the cluster shows it finished") ||
		!strings.Contains(n, "upgrade of release db (context test): it never reached the cluster") {
		t.Errorf("notes %q", n)
	}
	var pending []engine.PendingHelmOpInfo
	res, _ := h.dispatch(engine.OriginExternalAgent, "helm.pendingOperations", struct{}{})
	b, _ := json.Marshal(res)
	_ = json.Unmarshal(b, &pending)
	if len(pending) != 1 || pending[0].Action != engine.HelmUpgrade || pending[0].ReleaseStatus != "pending-upgrade" ||
		pending[0].FromRevision != 3 || pending[0].ChartVersion != "0.3.0" {
		t.Fatalf("pending %s", b)
	}
	// An upgrade is refused while the release is pending; the rollback to fromRevision finishes it.
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.upgrade", map[string]any{"ref": webRef, "values": "", "revision": 4}); err == nil {
		t.Error("an upgrade of a release left pending-upgrade ran")
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.rollback", map[string]any{"ref": webRef, "revision": pending[0].FromRevision}); err != nil {
		t.Fatal(err)
	}
	h.waitRelease(t, "apps", "web", 5)
	if list, _ := journal.List(); len(list) != 0 {
		t.Errorf("journal after finishing: %+v", list)
	}
	if calls := h.server.recorded(); len(calls) != 1 || calls[0] != "rollback apps/web to 3" {
		t.Errorf("calls %v", calls)
	}
}

// The rollback dialog's history: every revision from the cache, newest first, with the newest
// revision and its status.
func TestHelmReleaseHistory(t *testing.T) {
	h := newHelmHarness(t, engine.NewMemoryHelmJournal(), nil)
	res, err := h.dispatch(engine.OriginExternalAgent, "helm.release.history", map[string]any{"ref": webRef})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(commands.ReleaseHistory)
	if r.Revision != 3 || r.Status != "deployed" || len(r.History) != 3 || r.History[0].Revision != 3 || r.History[2].ChartVersion != "0.1.0" ||
		r.History[0].Chart != "web" || r.History[0].Updated.IsZero() {
		t.Errorf("history %+v", r)
	}
}
