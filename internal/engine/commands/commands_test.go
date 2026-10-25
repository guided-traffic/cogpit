package commands_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The command ids the GUI and the MCP server rely on, with their danger levels.
var wantCommands = map[string]engine.Danger{
	"ui.state":           engine.DangerRead,
	"ui.openView":        engine.DangerUI,
	"ui.setNamespace":    engine.DangerUI,
	"ui.setFilter":       engine.DangerUI,
	"ui.sort":            engine.DangerUI,
	"ui.select":          engine.DangerUI,
	"ui.closeDrawer":     engine.DangerUI,
	"ui.navigate":        engine.DangerUI,
	"cluster.contexts":   engine.DangerRead,
	"cluster.connect":    engine.DangerUI,
	"cluster.connectLog": engine.DangerRead,
	"metrics.get":        engine.DangerRead,
	"ui.setMetricsRange": engine.DangerUI,
	"kinds.list":         engine.DangerRead,
	"resources.list":     engine.DangerRead,
	"resources.find":     engine.DangerRead,
	"object.get":         engine.DangerRead,
	"pod.logs":           engine.DangerRead,
	"application.logs":   engine.DangerRead,
	"pod.shell":          engine.DangerMutating,
	"terminal.open":      engine.DangerUI,
	"dock.close":         engine.DangerUI,
	"dock.activate":      engine.DangerUI,
	"deployment.restart": engine.DangerDestructive,
	"object.delete":      engine.DangerDestructive,
	"object.deleteMany":  engine.DangerDestructive, // Delete of the table's selection (TestDeleteManyOneConfirmation)
	"node.cordon":        engine.DangerMutating,
	"node.uncordon":      engine.DangerMutating,
	"node.drain":         engine.DangerDestructive,
	"node.pendingDrains": engine.DangerRead,
	"node.dismissDrain":  engine.DangerUI,
	"deployment.scale":   engine.DangerMutating, // destructive when scaling to zero (Escalate)
	"statefulset.scale":  engine.DangerMutating, // destructive when scaling to zero (Escalate)
	"cronjob.trigger":    engine.DangerMutating,
	"cronjob.suspend":    engine.DangerMutating,
	"cronjob.resume":     engine.DangerMutating,
	"pod.evict":          engine.DangerDestructive,
	"object.edit":        engine.DangerUI,
	"editor.get":         engine.DangerRead,
	"editor.setText":     engine.DangerUI,
	"editor.diff":        engine.DangerRead,
	"object.dryRun":      engine.DangerRead,
	"object.apply":       engine.DangerMutating,
	"object.create":      engine.DangerMutating,
	"resource.createTab": engine.DangerUI,
	"resource.templates": engine.DangerRead,
	// A Config Map's values edited in the drawer (F3) (TestConfigMapDataSave).
	"configMap.readData": engine.DangerRead, "configMap.reviewData": engine.DangerRead, "configMap.saveData": engine.DangerMutating,
	// Its unsaved values kept, in memory and as a draft (the owner's answer of 2026-10-15): GUI-only,
	// Cogpit's own files, never a cluster (TestConfigMapDrawerDrafts).
	"configMap.keepEdits": engine.DangerUI,

	"cluster.refreshAccess": engine.DangerRead,

	"ui.setMetricsVariant": engine.DangerUI,

	"cluster.prometheus": engine.DangerRead, "cluster.setPrometheus": engine.DangerUI,

	"pod.portForward":     engine.DangerMutating,
	"service.portForward": engine.DangerMutating,
	"portforward.open":    engine.DangerUI,
	"portforward.edit":    engine.DangerMutating,
	"portforward.stop":    engine.DangerUI,
	"portforward.remove":  engine.DangerUI,

	"secret.reveal": engine.DangerRead, // GUI only: refused for agents and the CLI
	// A Secret's values edited in the drawer (D-59): GUI only (TestSecretDrawerDataIsGUIOnly).
	engine.SecretReviewDataCommand: engine.DangerRead, engine.SecretSaveDataCommand: engine.DangerMutating,

	"helm.release.rollback":   engine.DangerDestructive,
	"helm.release.uninstall":  engine.DangerDestructive,
	"helm.pendingOperations":  engine.DangerRead,
	"helm.dismissOperation":   engine.DangerUI,
	"helm.release.upgradeTab": engine.DangerUI, "helm.release.upgrade": engine.DangerMutating,
	"helm.release.values": engine.DangerRead, "helm.release.history": engine.DangerRead,
	"helm.release.revision": engine.DangerRead, "helm.release.diff": engine.DangerRead, "ui.helm.revisions": engine.DangerUI, "ui.helm.revisions.set": engine.DangerUI,
	// Helm chart repositories (G11, D-29): read from Helm's repositories.yaml, added and removed
	// with the helm CLI only, so there is no add or remove (TestNoCommandWritesHelmsFile).
	"helm.repo.list": engine.DangerRead, "helm.repo.update": engine.DangerUI,
	"helm.repo.allowHTTPCredentials": engine.DangerUI, // GUI-only: credentials over plain http (H-40, TestAllowHTTPCredentialsIsTheUsers)
	// Installing a chart (G11): the tab is UI, the preview a read, the install mutating (D-2).
	"helm.chart.installTab": engine.DangerUI, "helm.chart.preview": engine.DangerRead, "helm.chart.install": engine.DangerMutating,
	// The chart drawer's version (HELM2): UI state of the open drawer (TestHelmChartDrawerVersion).
	"ui.setChartVersion": engine.DangerUI,

	// Workload actions and logs (G7, G8).
	"daemonset.restart":   engine.DangerDestructive,
	"statefulset.restart": engine.DangerDestructive,
	"replicaset.scale":    engine.DangerMutating, // destructive when scaling to zero (Escalate)
	"pod.attach":          engine.DangerMutating,
	"workload.logs":       engine.DangerRead,
	"logs.setOptions":     engine.DangerUI,
	"logs.controller":     engine.DangerRead,
	"logs.sources":        engine.DangerRead,
	"logs.setSource":      engine.DangerRead,
	"logs.loadOlder":      engine.DangerRead,
	"logs.jumpTo":         engine.DangerRead,
	"object.copyName":     engine.DangerUI,

	// Tables and pages (G5).
	"ui.setColumns":       engine.DangerUI,
	"ui.setColumnOrder":   engine.DangerUI, // dragging a header (TestSetColumnOrder)
	"ui.setGroups":        engine.DangerUI,
	"table.exportCsv":     engine.DangerRead,
	"workloads.overview":  engine.DangerRead,
	"cluster.overview":    engine.DangerRead,
	"ui.setOverviewRange": engine.DangerUI, "ui.setOverviewNodes": engine.DangerUI,
	// The drawer's sortable tables, a node's Pods (TestSortDrawerTable).
	"ui.sortDrawerTable": engine.DangerUI,

	// Kubeconfig sources (G2); the UI ones are GUI-only (TestKubeconfigCommandsAreGUIOnly).
	"kubeconfig.sources": engine.DangerRead, "kubeconfig.addFile": engine.DangerUI, "kubeconfig.addFolder": engine.DangerUI,
	"kubeconfig.remove": engine.DangerUI, "kubeconfig.execPlugin": engine.DangerRead,
	"kubeconfig.approveExec": engine.DangerUI, "kubeconfig.pendingApprovals": engine.DangerRead,

	// The hotbar (G13): Cogpit's own preferences, never a cluster (TestHotbarCommands).
	"hotbar.list": engine.DangerRead, "hotbar.addCluster": engine.DangerUI, "hotbar.removeCluster": engine.DangerUI,
	"hotbar.moveCluster": engine.DangerUI, "hotbar.openCluster": engine.DangerUI,

	// The command palette (G13): its open state (TestPaletteOpenState).
	"ui.palette.open": engine.DangerUI, "ui.palette.close": engine.DangerUI,

	// Preferences and cluster settings (G13): Cogpit's own files, never a cluster or a kubeconfig;
	// the shell and the icon upload GUI-only (TestPreferencesCommands,
	// TestClusterSettingsCommandsNeverTouchTheKubeconfig).
	"preferences.get": engine.DangerRead, "preferences.set": engine.DangerUI, "preferences.open": engine.DangerUI,
	"preferences.loginItem": engine.DangerRead, "preferences.setStartAtLogin": engine.DangerUI, "preferences.openLoginItems": engine.DangerUI,
	"settings.close": engine.DangerUI, "cluster.settings.get": engine.DangerRead, "cluster.settings.set": engine.DangerUI,
	"cluster.settings.uploadIcon": engine.DangerUI, "cluster.settings.resetIcon": engine.DangerUI, "cluster.settings.cutIcon": engine.DangerUI,
	"cluster.settings.open": engine.DangerUI,
	// The question of a new cluster's integrations (D-76): Cogpit's own settings (TestIntegrationsQuestionLifecycle).
	"cluster.integrations.answer": engine.DangerUI, "cluster.integrations.later": engine.DangerUI,
	// Its pre-choice, ticks and Ask again (D-82): what Cogpit looks for (TestIntegrationsConsentCommands).
	"cluster.integrations.step": engine.DangerUI, "cluster.integrations.lookFor": engine.DangerUI,
	"cluster.integrations.ask": engine.DangerUI,
	// The tab bar (G5): which views the window shows (TestTabCommands).
	"ui.tab.open": engine.DangerUI, "ui.tab.pin": engine.DangerUI, "ui.tab.close": engine.DangerUI,
	"ui.tab.select": engine.DangerUI, "ui.tab.next": engine.DangerUI, "ui.tab.previous": engine.DangerUI,
	"ui.tab.back": engine.DangerUI, "ui.tab.forward": engine.DangerUI,
	// Moving the tab bar's and the dock's tabs (GW4, TestTabMoveCommands).
	"ui.tab.move": engine.DangerUI, "ui.tab.moveLeft": engine.DangerUI, "ui.tab.moveRight": engine.DangerUI, "dock.move": engine.DangerUI,
	// The welcome page and a cluster's menu (G5, G13): the window and Cogpit's own links, never a
	// cluster or a kubeconfig file; cluster.remove GUI-only (TestClusterRemove).
	"ui.welcome.open": engine.DangerUI, "cluster.disconnect": engine.DangerUI, "cluster.links": engine.DangerRead,
	"cluster.remove": engine.DangerUI,
	// The notification log (G5): Cogpit's own state, never a cluster (TestNotificationCommands).
	"notifications.list": engine.DangerRead, "notifications.markRead": engine.DangerUI, "notifications.clear": engine.DangerUI,
	// The navigator's scope (G5, G13): what the navigator and the tab bar show
	// (TestNavigatorScopeCommand).
	"ui.setNavigatorScope": engine.DangerUI,
	// The navigator's search, keyboard row and collapsed source groups, and the palette's text
	// (D-51, A51, A54, A63): UI state only (TestNavigatorAndPaletteStateCommands).
	"ui.setNavigatorSearch": engine.DangerUI, "ui.setNavigatorRow": engine.DangerUI, "ui.setNavigatorGroup": engine.DangerUI,
	"ui.palette.setText": engine.DangerUI,
	// kubectl for terminals (D-3): GUI-only, Cogpit's own folder, never a cluster (TestKubectlDownloadCommand).
	"kubectl.download": engine.DangerUI,
	// Help › Check for Updates… (D-4): Sparkle's window, never a cluster (TestCheckForUpdatesCommand).
	"app.checkForUpdates": engine.DangerUI,
	"app.installUpdate":   engine.DangerUI,
	// The editor's drafts and Revert to original (G9): Cogpit's own files and the editor's text,
	// never a cluster (TestDraftRestoredAfterARestart, TestRevertToOriginal).
	"editor.drafts": engine.DangerRead, "editor.restoreDraft": engine.DangerUI, "editor.discardDraft": engine.DangerUI,
	"editor.revert": engine.DangerUI, "editor.undoRevert": engine.DangerUI,
	// MCP control (G17): the rows the user sees (TestViewRowsMatchTheTable).
	"view.rows": engine.DangerRead,
	// The allowance of agent writes (D-2): GUI-only, Cogpit's own settings (TestAgentWritesAllowance).
	"cluster.settings.setAgentWrites": engine.DangerUI,
	// A cluster's accent colour (D-80): GUI-only, Cogpit's own settings (TestAccentColorIsTheUsers).
	"cluster.settings.setAccentColor": engine.DangerUI,
	// How the light strip shows it (D-122): GUI-only, Cogpit's own settings (TestLightStripIsTheUsers).
	"cluster.settings.setLightStrip": engine.DangerUI,
	// The audit log in the window and for agents (TestAuditListNewestFirstAndBounded).
	"audit.list": engine.DangerRead, "audit.open": engine.DangerUI,
	// The Diagnostics view (D-13): Cogpit's own counters, no request (TestDiagnosticsReport).
	"diagnostics.get": engine.DangerRead, "diagnostics.open": engine.DangerUI,
	// Flux CD (G23, D-33): patches of Flux objects, mutating, so D-2 applies to agents
	// (TestFluxActionsAgentsNeedAcceptance, TestFluxReconcileWithSourceAgents).
	"flux.reconcile": engine.DangerMutating, "flux.suspend": engine.DangerMutating, "flux.resume": engine.DangerMutating,
	"flux.reconcileMany": engine.DangerMutating, "flux.suspendMany": engine.DangerMutating, "flux.resumeMany": engine.DangerMutating,
	"flux.reconcileWithSource": engine.DangerMutating, "flux.reconcileWithSourceMany": engine.DangerMutating,
	"flux.reset": engine.DangerMutating, "flux.resetMany": engine.DangerMutating,
	// The Flux dashboard (D-34), read from the cache (TestFluxDashboardCommand).
	"flux.dashboard": engine.DangerRead,
	// The Alertmanager integration (D-77): its alerts and board read from the last poll, the board's
	// page and folding UI state, its address an agent may only ask for (TestAlertCommands).
	"alerts.list": engine.DangerRead, "alerts.board": engine.DangerRead, "alerts.refresh": engine.DangerRead,
	"alerts.open": engine.DangerUI, "alerts.fold": engine.DangerUI,
	"cluster.alertmanager": engine.DangerRead, "cluster.setAlertmanager": engine.DangerUI,
	// Argo CD (G23, D-36): Sync is destructive with prune (Escalate) (TestArgoSyncWritesTheOperation).
	"argocd.application.sync": engine.DangerMutating, "argocd.application.syncMany": engine.DangerMutating,
	"argocd.application.refresh": engine.DangerMutating, "argocd.application.refreshMany": engine.DangerMutating,
	"argocd.dashboard": engine.DangerRead,     // the Dashboard for agents (TestArgoDashboard)
	"argocd.openUI":    engine.DangerMutating, // a port forward to argocd-server (TestArgoOpenUIFindsTheServer)
	// What the dock's tabs show, for agents (G17, D-44); a terminal's switch is GUI-only.
	"dock.read": engine.DangerRead, "dock.allowAgentRead": engine.DangerUI,
	// The status filter of every view (D-34, owner 2026-10-14) (TestStatusFilterCommand).
	"ui.setStatusFilter": engine.DangerUI,
	// The built-in agent's chat column (D-10, M12): UI state only (TestChatToggle).
	"chat.toggle": engine.DangerUI,
	// The window's layout toggles (F7): UI state only (TestLayoutToggles).
	"ui.toggleNavigator": engine.DangerUI, "ui.toggleDock": engine.DangerUI,
	// Restart of a selection of workloads (A74): destructive, as one Restart (TestRestartMany).
	"workload.restartMany": engine.DangerDestructive,
	// An Argo CD Application's and a Flux Kustomization's logs in one tab (A81) (TestArgoCDLogs, TestFluxLogs).
	"argocd.logs": engine.DangerRead, "flux.logs": engine.DangerRead,
	// A connection (D-58, D-81): Reconnect, Log in again (GUI only), a shell tab's
	// Reconnect, which opens a new shell (TestConnectionCommands).
	"cluster.reconnect": engine.DangerUI, "cluster.login": engine.DangerUI, "dock.reconnect": engine.DangerMutating,
	// A background job stopped, its tab kept (G4): UI state, as dock.close stops one (TestDeleteManyAsAJob).
	"job.cancel": engine.DangerUI,
	// The Kyverno integration's violations, from the cache (D-79) (TestKyvernoViolationsCommand).
	"kyverno.violations": engine.DangerRead,
	// An object being deleted loses its finalizers (WF43, D-98) (TestRemoveFinalizersConfirmsWhatItRemoves).
	"object.removeFinalizers": engine.DangerDestructive,
	// The program folders of a credential plugin's programs (D-120): adding GUI-only (TestProgramFoldersAddRemoveAndKeep).
	"preferences.addProgramFolder": engine.DangerUI, "preferences.removeProgramFolder": engine.DangerUI,
}

func TestRegistryComplete(t *testing.T) {
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	cmds := r.Commands()
	if len(cmds) != len(wantCommands) {
		t.Errorf("%d commands registered, want %d", len(cmds), len(wantCommands))
	}
	for _, c := range cmds {
		want, ok := wantCommands[c.ID]
		if !ok {
			t.Errorf("unexpected command %s", c.ID)
			continue
		}
		if c.Danger != want {
			t.Errorf("%s: danger %s, want %s", c.ID, c.Danger, want)
		}
		if c.Label == "" || len(c.Description) < 20 {
			t.Errorf("%s: label %q, description %q", c.ID, c.Label, c.Description)
		}
		if c.Schema == nil || c.Schema.Type != "object" {
			t.Errorf("%s: schema is not an object: %+v", c.ID, c.Schema)
		}
		if (c.Danger == engine.DangerMutating || c.Danger == engine.DangerDestructive) && len(c.Verbs) == 0 {
			t.Errorf("%s writes but declares no verbs", c.ID)
		}
	}
	if err := commands.RegisterAll(r); err == nil {
		t.Error("registering twice succeeded")
	}
}

// TestMCPFindAndNavigate is the agent flow of architecture §7: find the Service by its cluster IP,
// show it to the user, and, once the cache is warm, do it again without a single list request
// (budget P11 in miniature).
func TestMCPFindAndNavigate(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent := engine.OriginExternalAgent
	find := map[string]any{"kind": "services", "field": "clusterIP", "value": "10.96.100.100"}

	flow := func() model.ObjectRef {
		var st commands.StateResult
		h.must(t, agent, "ui.state", nil, &st)
		var found commands.FindResult
		h.must(t, agent, "resources.find", find, &found)
		if found.Count != 1 || len(found.Matches) != 1 {
			t.Fatalf("find: %+v", found)
		}
		ref := found.Matches[0].Ref
		if ref.Kind != "services" || ref.Namespace != "shop" || ref.Name != "checkout" || ref.Context != "test" {
			t.Fatalf("found %+v", ref)
		}
		var nav commands.NavigateResult
		h.must(t, agent, "ui.navigate", map[string]any{"ref": ref}, &nav)
		if nav.View != "services" || !nav.Found {
			t.Fatalf("navigate: %+v", nav)
		}
		u := h.s.UIState()
		if u.View != "services" || len(u.Selected) != 0 || u.Drawer == nil || !u.Drawer.Same(ref) {
			t.Fatalf("UI state after navigate: view %s selected %v drawer %v", u.View, u.Selected, u.Drawer)
		}
		snap := waitTable(t, h.s, "services", func(s *engine.TableSnapshot) bool { return s.Synced && s.IndexOf(ref) >= 0 })
		if snap.Sort != engine.DefaultSort {
			t.Errorf("services sort %+v", snap.Sort)
		}
		var detail engine.DetailModel
		h.must(t, agent, "object.get", map[string]any{"ref": ref, "noYaml": true}, &detail)
		if detail.Title != "Service: checkout" || len(detail.Events) != 1 || detail.YAML != "" {
			t.Errorf("object.get: %+v", detail)
		}
		return ref
	}

	flow()
	h.must(t, engine.OriginGUI, "ui.openView", map[string]any{"view": "pods"}, nil)
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return s.Synced })
	// The baseline once every informer the first flow started has synced and opened its watch:
	// a synced view's informer may not have reached its watch yet (docs/developer/testing.md).
	h.waitSettled(t)

	lists, watches := h.src.Lists(), h.src.Watches()
	flow()
	h.waitSettled(t) // an informer the warm flow started has listed and watched by now
	if dl, dw := h.src.Lists()-lists, h.src.Watches()-watches; dl != 0 || dw != 0 {
		t.Errorf("the warm flow cost %d lists and %d watches, want 0", dl, dw)
	}
}

func TestUICommands(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	gui := engine.OriginGUI
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return s.Synced && len(s.Rows) == 4 })

	if err := h.run(t, gui, "ui.openView", map[string]any{"view": "widgets"}, nil); err == nil || !strings.Contains(err.Error(), "pods") {
		t.Errorf("unknown view: %v", err)
	}
	h.must(t, gui, "ui.setNamespace", map[string]any{"namespaces": []string{"shop", " shop ", ""}}, nil)
	if u := h.s.UIState(); !slices.Equal(u.Namespaces, []string{"shop"}) {
		t.Errorf("namespaces %v", u.Namespaces)
	}
	h.must(t, gui, "ui.setFilter", map[string]any{"filter": "web-1"}, nil)
	snap := waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return s.Filter == "web-1" && len(s.Namespaces) == 1 })
	if len(snap.Rows) != 1 || snap.Rows[0].Ref.Key() != "shop/web-1" {
		t.Errorf("filtered rows %+v", snap.Rows)
	}
	if err := h.run(t, gui, "ui.sort", map[string]any{"column": "nope"}, nil); err == nil || !strings.Contains(err.Error(), "node") {
		t.Errorf("unknown column: %v", err)
	}
	h.must(t, gui, "ui.sort", map[string]any{"column": "node", "desc": true}, nil)

	// Opening a view clears selection, drawer and search; keeps namespaces; restores its sort.
	h.must(t, gui, "ui.openView", map[string]any{"view": "services"}, nil)
	u := h.s.UIState()
	if u.View != "services" || u.Filter != "" || !slices.Equal(u.Namespaces, []string{"shop"}) || u.Sort != engine.DefaultSort {
		t.Errorf("after openView: %+v", u)
	}
	h.must(t, gui, "ui.openView", map[string]any{"view": "pods"}, nil)
	if u := h.s.UIState(); u.Sort != (engine.SortSpec{Column: "node", Desc: true}) {
		t.Errorf("pods sort not remembered: %+v", u.Sort)
	}

	web0 := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	web1 := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-1"}
	h.must(t, gui, "ui.select", map[string]any{"refs": []model.ObjectRef{web0, web1, web0}}, nil)
	u = h.s.UIState()
	if len(u.Selected) != 2 || u.Drawer == nil || !u.Drawer.Same(web0) || u.Drawer.Context != "test" {
		t.Errorf("select: selected %v drawer %v", u.Selected, u.Drawer)
	}
	h.must(t, gui, "ui.select", map[string]any{"refs": []model.ObjectRef{web1}, "noDrawer": true}, nil)
	if u := h.s.UIState(); len(u.Selected) != 1 || !u.Drawer.Same(web0) {
		t.Errorf("select without drawer moved the drawer: %v", u.Drawer)
	}
	svc := model.ObjectRef{Kind: "services", Namespace: "shop", Name: "checkout"}
	if err := h.run(t, gui, "ui.select", map[string]any{"refs": []model.ObjectRef{svc}}, nil); err == nil || !strings.Contains(err.Error(), "ui_navigate") {
		t.Errorf("select of another kind: %v", err)
	}
	h.must(t, gui, "ui.closeDrawer", nil, nil)
	if h.s.UIState().Drawer != nil {
		t.Error("drawer still open")
	}
	h.must(t, gui, "ui.setNamespace", map[string]any{}, nil)
	if u := h.s.UIState(); u.Namespaces != nil {
		t.Errorf("empty namespaces: %v", u.Namespaces)
	}

	var st commands.StateResult
	h.must(t, engine.OriginExternalAgent, "ui.state", map[string]any{}, &st)
	if st.View != "pods" || st.ViewTitle != "Pods" || st.Context != "test" || st.Connection.State != engine.Connected {
		t.Errorf("ui.state %+v", st)
	}
}

func TestNavigateMakesTheObjectVisible(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	gui := engine.OriginGUI
	h.must(t, gui, "ui.setNamespace", map[string]any{"namespaces": []string{"infra"}}, nil)
	h.must(t, gui, "ui.setFilter", map[string]any{"filter": "web-1"}, nil)

	// Same view: the namespace filter hides shop/web-0 and the search text hides it too.
	ref := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	h.must(t, gui, "ui.navigate", map[string]any{"ref": ref}, nil)
	u := h.s.UIState()
	if u.Namespaces != nil || u.Filter != "" || !u.Drawer.Same(ref) {
		t.Errorf("after navigate: namespaces %v filter %q drawer %v", u.Namespaces, u.Filter, u.Drawer)
	}
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return s.Filter == "" && s.IndexOf(ref) >= 0 })

	// A search text that shows the object stays.
	h.must(t, gui, "ui.setFilter", map[string]any{"filter": "web"}, nil)
	h.must(t, gui, "ui.navigate", map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "infra", Name: "web-1"}}, nil)
	if u := h.s.UIState(); u.Filter != "web" {
		t.Errorf("a matching filter was cleared: %q", u.Filter)
	}

	// A missing object: the view and drawer are set, found is false.
	var nav commands.NavigateResult
	h.must(t, gui, "ui.navigate", map[string]any{"ref": model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "gone"}}, &nav)
	if nav.Found || nav.View != "deployments" {
		t.Errorf("navigate to a missing object: %+v", nav)
	}
	// Into a cluster that is not connected: refused, nothing connects (D-69).
	if err := h.run(t, gui, "ui.navigate", map[string]any{"ref": model.ObjectRef{Context: "prod", Kind: "pods", Namespace: "shop", Name: "web-0"}}, nil); !errors.Is(err, engine.ErrNotConnected) {
		t.Errorf("navigate into another cluster: %v", err)
	}
}

func TestResourcesCommands(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent := engine.OriginExternalAgent

	var kinds []commands.KindInfo
	h.must(t, agent, "kinds.list", nil, &kinds)
	if len(kinds) != 5 || kinds[0].Kind != "pods" || !slices.Equal(kinds[0].Fields, []string{"name", "namespace", "label", "node", "podIP"}) {
		t.Errorf("kinds %+v", kinds)
	}

	var list commands.ListResult
	h.must(t, agent, "resources.list", map[string]any{"kind": "pods", "namespaces": []string{"shop"}, "limit": 1}, &list)
	if list.Total != 2 || list.Returned != 1 || list.Rows[0].Ref.Name != "web-0" || list.Rows[0].Cells["Node"] != "node-1" {
		t.Errorf("list %+v", list)
	}
	h.must(t, agent, "resources.list", map[string]any{"kind": "pods", "filter": "web-1"}, &list)
	if list.Total != 2 || list.Returned != 2 {
		t.Errorf("filtered list %+v", list)
	}
	if err := h.run(t, agent, "resources.list", map[string]any{"kind": "pods", "limit": -1}, nil); err == nil {
		t.Error("negative limit accepted")
	}

	var found commands.FindResult
	h.must(t, agent, "resources.find", map[string]any{"field": "namespace", "value": "SHOP"}, &found)
	if found.Count != 6 { // 2 pods, 1 deployment, 2 services, 1 event; namespace objects have none
		t.Errorf("find by namespace over every kind: %+v", found)
	}
	h.must(t, agent, "resources.find", map[string]any{"kind": "services", "field": "clusterIP", "value": "10.96.100", "match": "prefix", "limit": 1}, &found)
	if found.Count != 2 || len(found.Matches) != 1 || !found.Truncated {
		t.Errorf("prefix find: %+v", found)
	}
	h.must(t, agent, "resources.find", map[string]any{"field": "label", "value": "app", "match": "contains"}, &found)
	if found.Count != 0 {
		t.Errorf("label find: %+v", found)
	}
	if err := h.run(t, agent, "resources.find", map[string]any{"kind": "pods", "field": "clusterIP", "value": "x"}, nil); err == nil || !strings.Contains(err.Error(), "podIP") {
		t.Errorf("unknown field: %v", err)
	}
	if err := h.run(t, agent, "resources.find", map[string]any{"field": "name", "value": "x", "match": "regex"}, nil); err == nil {
		t.Error("unknown match mode accepted")
	}

	var detail engine.DetailModel
	h.must(t, agent, "object.get", map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}}, &detail)
	if detail.Title != "Pod: web-0" || !slices.Equal(detail.Containers, []string{"app"}) || !strings.Contains(detail.YAML, "web-0") {
		t.Errorf("object.get %+v", detail)
	}
	h.must(t, agent, "object.get", map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "nope"}}, &detail)
	if !detail.Missing {
		t.Error("missing object not reported")
	}
}

func TestDestructiveCommandsNeedConfirmation(t *testing.T) {
	audit := &syncBuffer{}
	h := newHarness(t, seed(), engine.Options{Audit: audit})
	pod := map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}}
	deploy := map[string]any{"ref": model.ObjectRef{Kind: "deployments", Namespace: "infra", Name: "web"}}
	if _, err := h.s.Objects(context.Background(), "deployments", nil); err != nil { // warm: Enabled does not wait
		t.Fatal(err)
	}
	if _, err := h.s.Objects(context.Background(), "pods", nil); err != nil {
		t.Fatal(err)
	}

	// Headless: refused before anything runs.
	if err := h.run(t, engine.OriginGUI, "object.delete", pod, nil); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("headless delete: %v", err)
	}

	var asked []engine.ConfirmRequest
	answer := false
	h.s.SetConfirmer(engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return answer, nil
	}))
	if err := h.run(t, engine.OriginGUI, "object.delete", pod, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Errorf("declined delete: %v", err)
	}
	if len(h.writer.Calls()) != 0 {
		t.Fatalf("a declined command ran: %v", h.writer.Calls())
	}
	req := asked[0]
	if req.Command != "object.delete" || req.Context != "test" || !slices.Equal(req.Namespaces, []string{"shop"}) ||
		len(req.Objects) != 1 || req.Objects[0].Name != "web-0" || req.Objects[0].Context != "test" {
		t.Errorf("confirmation request %+v", req)
	}
	if msg := req.Message(); !strings.Contains(msg, "Delete Pod web-0") || !strings.Contains(msg, "shop") || !strings.Contains(msg, "test") {
		t.Errorf("confirmation message %q", msg)
	}

	answer = true
	var res commands.ActionResult
	h.must(t, engine.OriginExternalAgent, "deployment.restart", deploy, &res)
	h.must(t, engine.OriginGUI, "object.delete", pod, nil)
	if got := h.writer.Calls(); !slices.Equal(got, []string{"restart infra/web", "delete shop/web-0"}) {
		t.Errorf("writes %v", got)
	}
	if res.Action != "restarted" || asked[len(asked)-2].Origin != engine.OriginExternalAgent {
		t.Errorf("restart result %+v, origin %s", res, asked[len(asked)-2].Origin)
	}

	// Only agent commands are audited: a write with a "requested" line before it runs and a
	// "done" line with its outcome, both naming the confirmed context.
	lines := audit.Lines()
	if len(lines) != 2 || lines[0].Phase != "requested" || lines[1].Phase != "done" ||
		lines[1].Command != "deployment.restart" || !lines[1].OK || lines[1].Origin != engine.OriginExternalAgent ||
		lines[0].Context != "test" || lines[1].Context != "test" {
		t.Errorf("audit %+v", lines)
	}

	// Disabled with a reason: objects not in the cache, other kinds.
	var dis *engine.DisabledError
	if err := h.run(t, engine.OriginGUI, "object.delete", map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "gone"}}, nil); !errors.As(err, &dis) || !strings.Contains(dis.Reason, "not found") {
		t.Errorf("delete a missing pod: %v", err)
	}
	if err := h.run(t, engine.OriginGUI, "deployment.restart", map[string]any{"ref": model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "gone"}}, nil); !errors.As(err, &dis) || !strings.Contains(dis.Reason, "not found") {
		t.Errorf("restart a missing deployment: %v", err)
	}
	if err := h.run(t, engine.OriginGUI, "deployment.restart", pod, nil); !errors.As(err, &dis) {
		t.Errorf("restart a pod: %v", err)
	}
}

func TestConfirmationForAnotherClusterDoesNotApply(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	if _, err := h.s.Objects(context.Background(), "pods", nil); err != nil {
		t.Fatal(err)
	}
	// While the dialog is open the user switches to another cluster that has the same pod.
	h.s.SetConfirmer(engine.ConfirmFunc(func(ctx context.Context, _ engine.ConfirmRequest) (bool, error) {
		if err := h.s.Connect(ctx, "other"); err != nil {
			return false, err
		}
		return true, nil
	}))
	err := h.run(t, engine.OriginGUI, "object.delete", map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}}, nil)
	if !errors.Is(err, engine.ErrWrongCluster) {
		t.Errorf("delete after a cluster switch: %v", err)
	}
	if len(h.writer.Calls()) != 0 {
		t.Errorf("the delete reached the other cluster: %v", h.writer.Calls())
	}
}

func TestDockCommands(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	gui := engine.OriginGUI
	pod := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}

	var first, again commands.DockResult
	h.must(t, gui, "pod.logs", map[string]any{"ref": pod}, &first)
	h.must(t, gui, "pod.logs", map[string]any{"ref": pod, "container": "app"}, &again)
	if !first.Created || again.Created || first.Tab.ID != "logs-1" || again.Tab.ID != "logs-1" || first.Tab.Container != "app" {
		t.Errorf("logs tabs %+v %+v", first, again)
	}
	if err := h.run(t, gui, "pod.logs", map[string]any{"ref": pod, "container": "sidecar"}, nil); err == nil || !strings.Contains(err.Error(), "app") {
		t.Errorf("unknown container: %v", err)
	}

	var term commands.DockResult
	h.must(t, gui, "terminal.open", nil, &term)
	if term.Tab.ID != "terminal-1" || term.Tab.Title != "Terminal 1" || term.Tab.Context != "test" {
		t.Errorf("terminal tab %+v", term.Tab)
	}

	// A shell from the GUI runs; from an agent it needs the user's acceptance.
	var shell commands.DockResult
	h.must(t, gui, "pod.shell", map[string]any{"ref": pod}, &shell)
	if shell.Tab.Type != engine.DockShell || shell.Tab.ID != "shell-1" {
		t.Errorf("shell tab %+v", shell.Tab)
	}
	if err := h.run(t, engine.OriginExternalAgent, "pod.shell", map[string]any{"ref": pod}, nil); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("agent shell without a GUI: %v", err)
	}
	h.s.SetAgentWrites("test", true)
	h.must(t, engine.OriginExternalAgent, "pod.shell", map[string]any{"ref": pod}, nil)

	u := h.s.UIState()
	if len(u.Dock) != 4 || u.ActiveDock != "shell-2" {
		t.Fatalf("dock %+v active %s", u.Dock, u.ActiveDock)
	}
	h.must(t, gui, "dock.activate", map[string]any{"id": "logs-1"}, nil)
	h.must(t, gui, "dock.close", map[string]any{"id": "logs-1"}, nil)
	if u := h.s.UIState(); len(u.Dock) != 3 || u.ActiveDock != "terminal-1" {
		t.Errorf("after close: %+v active %s", u.Dock, u.ActiveDock)
	}
	if err := h.run(t, gui, "dock.close", map[string]any{"id": "logs-1"}, nil); err == nil {
		t.Error("closing a closed tab succeeded")
	}
	if err := h.run(t, gui, "dock.activate", map[string]any{"id": "nope"}, nil); err == nil {
		t.Error("activating an unknown tab succeeded")
	}
}

func TestDemoClusterDisablesStreamsAndWrites(t *testing.T) {
	src := seed()
	s := engine.New(engine.Options{Connector: &fakeConnector{src: src, writer: &fakeWriter{}, demo: true}, SnapshotInterval: 10 * time.Millisecond})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	pod := []byte(`{"ref":{"kind":"pods","namespace":"shop","name":"web-0"}}`)
	for id, params := range map[string][]byte{
		"pod.logs": pod, "pod.shell": pod, "terminal.open": nil, "object.delete": pod,
		"deployment.restart": []byte(`{"ref":{"kind":"deployments","namespace":"shop","name":"web"}}`),
	} {
		_, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: params, Origin: engine.OriginGUI})
		var dis *engine.DisabledError
		if !errors.As(err, &dis) || !strings.Contains(dis.Reason, "demo") {
			t.Errorf("%s on the demo cluster: %v", id, err)
		}
	}
}

func TestClusterCommands(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	var ctxs []engine.ContextInfo
	h.must(t, engine.OriginExternalAgent, "cluster.contexts", nil, &ctxs)
	if len(ctxs) != 2 || ctxs[1].Name != "test" || !ctxs[1].Current {
		t.Errorf("contexts %+v", ctxs)
	}
	var res commands.ConnectResult
	h.must(t, engine.OriginExternalAgent, "cluster.connect", map[string]any{"context": "broken"}, &res)
	if res.State != engine.ConnectError || !strings.Contains(res.Error, "refused") || res.Context != "broken" {
		t.Errorf("connect broken: %+v", res)
	}
	var st commands.StateResult
	if err := h.run(t, engine.OriginGUI, "ui.openView", map[string]any{"view": "pods"}, nil); !strings.Contains(errString(err), "no cluster connected") {
		t.Errorf("openView while disconnected: %v", err)
	}
	h.must(t, engine.OriginExternalAgent, "cluster.connect", map[string]any{"context": "other"}, &res)
	h.must(t, engine.OriginExternalAgent, "ui.state", nil, &st)
	if res.State != engine.Connected || st.Context != "other" || st.View != "pods" {
		t.Errorf("connect other: %+v, state %+v", res, st)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
