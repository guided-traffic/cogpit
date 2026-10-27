package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSplitRowKeepsEscapedPipes(t *testing.T) {
	got := splitRow("| a | `x\\|y` | c |")
	if !slices.Equal(got, []string{"a", "`x\\|y`", "c"}) {
		t.Fatalf("cells %q", got)
	}
}

func TestParseTablesSectionsAndFences(t *testing.T) {
	text := "# Title\n## One\n\n| A | Status |\n|---|---|\n| x | done |\n\n```text\n| not | a table |\n|---|---|\n```\n### Two\n| B |\n|---|\n| y |\n"
	ts := parseTables("p.md", text)
	if len(ts) != 2 || ts[0].section != "One" || ts[1].section != "Two" || ts[0].rows[0].line != 6 || ts[1].rows[0].cell(0) != "y" {
		t.Fatalf("tables %+v", ts)
	}
}

func TestParseStatus(t *testing.T) {
	for cell, want := range map[string]status{
		"done": stDone, "partial: macOS keys": stPartial, "done (2026-10-11; …)": stDone,
		"missing (opens …)": stMissing, "not a target": stNotTarget, "– (nothing to build)": stNothing, "-": stNothing,
	} {
		if got, ok := parseStatus(cell); !ok || got != want {
			t.Errorf("%q: %q %v, want %q", cell, got, ok, want)
		}
	}
	for _, cell := range []string{"partially", "built", ""} {
		if got, ok := parseStatus(cell); ok {
			t.Errorf("%q read as %q", cell, got)
		}
	}
}

func TestHasEvidence(t *testing.T) {
	for text, want := range map[string]bool{
		"[`x.go`](../../internal/x.go)":           true,
		"`TestThing`":                             true,
		"`TestThing/sub_case`":                    true,
		"issue #12":                               true,
		"[PNG](../../internal/ui/testdata/a.png)": true,
		"see [decisions](../developer/decisions.md#d-1-ui-toolkit)": false,
		"`$KUBECONFIG` only":              false,
		"[Lens](https://docs.lenshq.io/)": false,
		"–":                               false,
	} {
		if got := hasEvidence(text); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}

func TestCommandIn(t *testing.T) {
	ns := map[string]bool{"helm": true, "object": true, "resource": true, "ui": true}
	for span, want := range map[string]string{
		"object.edit": "object.edit", "ui_navigate": "ui.navigate", "helm.release.upgrade": "helm.release.upgrade",
		"helm.json": "", "resource.k8s.io": "", "drains.json": "", "spec.nodeName": "", "apps/v1": "",
	} {
		got, ok := commandIn(span, ns)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v, want %q", span, got, ok, want)
		}
	}
}

// fixture writes the pages the checker reads into a temporary repository and returns a checker
// over it with the given registered commands and tests.
func fixture(t *testing.T, pages map[string]string, commands, tests []string) *checker {
	t.Helper()
	root := t.TempDir()
	for _, p := range progress {
		text, ok := pages[p]
		if !ok {
			text = "# " + p + "\n"
		}
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "x.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &checker{root: root, commands: map[string]bool{}, tests: map[string]bool{}}
	for _, id := range commands {
		c.commands[id] = true
	}
	for _, n := range tests {
		c.tests[n] = true
	}
	return c
}

func messages(c *checker) string {
	var out []string
	for _, p := range c.problems {
		out = append(out, p.String())
	}
	return strings.Join(out, "\n")
}

func TestRowChecks(t *testing.T) {
	c := fixture(t, map[string]string{
		pageUI: "# UI\n## Layout\n| Behaviour | Plan | Status | Evidence |\n|---|---|---|---|\n" +
			"| Title bar | free | partial | [`x.go`](../../internal/x.go) |\n" +
			"| Title bar | free | missing | – |\n" + // a duplicate
			"| Status bar | free | partial | the bar exists |\n" + // no evidence
			"| Drawer | free | done | `TestDrawer`, [`y.go`](../../internal/y.go) |\n" + // unknown test, missing file
			"| Copy | free | partial: `object.copyName` and `object.rename` (`TestCopy`) | – |\n" + // unknown command
			"| Odd | free | built | – |\n" + // unreadable status
			"## Revision history\n| Date | Change |\n|---|---|\n| 2026-10-11 | x |\n| 2026-10-11 | x |\n",
		pageResources: "# R\n## Workloads\n| View | API | Lens columns | Actions | Status |\n|---|---|---|---|---|\n" +
			"| Pods | `v1` Pod | Name | Shell `pod.shell`, Share `pod.share` › (not a target), Upgrade `helm.release.upgrade` | partial ([`x.go`](../../internal/x.go)) |\n",
	}, []string{"object.copyName", "pod.shell"}, []string{"TestCopy"})
	c.run(false)
	got := messages(c)
	for _, want := range []string{
		"ui-parity.md:6: the same first cell as line 5",
		"ui-parity.md:7: marked partial without evidence",
		"ui-parity.md:8: names TestDrawer, which no _test.go file declares",
		"ui-parity.md:8: links to ../../internal/y.go, which does not exist",
		"ui-parity.md:9: names the command object.rename, which the registry does not know",
		"ui-parity.md:10: the status must start with",
		"ui-parity.md:15: the same row as line 14",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing problem %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "resource-parity.md") {
		t.Errorf("a planned action in the Actions column is not a problem:\n%s", got)
	}
	if len(c.problems) != 7 {
		t.Errorf("%d problems, want 7:\n%s", len(c.problems), got)
	}
}

func TestCounts(t *testing.T) {
	c := fixture(t, map[string]string{
		pageResources: "# R\n## Every kind\n| Behaviour | Status | Evidence |\n|---|---|---|\n" +
			"| Edit (`object.edit`) | partial | `TestEdit` |\n" +
			"## Network\n| View | API | Lens columns | Actions | Status |\n|---|---|---|---|---|\n" +
			"| Services | `v1` | Name | Forward `service.portForward` | partial ([`x.go`](../../internal/x.go)) |\n" +
			"| Gateway API › A, B, C (3 views; capture) | CRDs | – | – | partial ([`x.go`](../../internal/x.go)) |\n" +
			"| Routes | CRDs | – | Edit `object.edit`, Share `pod.share` (not a target) | missing |\n" +
			"## Paid Lens Desktop views\n| View | Lens contents | Actions | Status |\n|---|---|---|---|\n| Flux | x | Sync `flux.sync` | missing |\n",
		pageUI: "# UI\n## Keys\n| Action | Plan | Status | Evidence |\n|---|---|---|---|\n" +
			"| Copy | free | done | `TestEdit` |\n| Hotbar | paid | missing | – |\n| Teamwork | free | not a target | – |\n| Palette | free | missing | – |\n",
		pagePlatforms: "# P\n## Cluster authentication and connectivity\n| Method | Status |\n|---|---|\n" +
			"| Tokens | partial (`TestEdit`) |\n| Cloud accounts (paid) | missing |\n| Azure | – (nothing to build) |\n| OIDC | missing |\n",
	}, []string{"object.edit"}, []string{"TestEdit"})
	n, _ := c.run(false)
	if c.problems != nil {
		t.Fatalf("problems:\n%s", messages(c))
	}
	if n.views != (tally{total: 5, exist: 4, partial: 4}) {
		t.Errorf("views %+v", n.views)
	}
	if n.actions.total != 2 || n.actions.exist != 1 || !slices.Equal(n.plannedActions, []string{"service.portForward"}) || !slices.Equal(n.notTargetActions, []string{"pod.share"}) {
		t.Errorf("actions %+v planned %v not a target %v", n.actions, n.plannedActions, n.notTargetActions)
	}
	if n.uiFree != (tally{total: 2, exist: 1, done: 1}) || n.uiPaid.total != 1 || n.uiOutside != 1 {
		t.Errorf("interface free %+v paid %+v outside %d", n.uiFree, n.uiPaid, n.uiOutside)
	}
	if n.connect != (tally{total: 2, exist: 1, partial: 1}) || n.platformOutside != 2 {
		t.Errorf("connectivity %+v outside %d", n.connect, n.platformOutside)
	}
}

func TestClaimsCompareWithTheCounts(t *testing.T) {
	c := fixture(t, map[string]string{
		pageResources: "# R\n\n**Summary (measured):** 1 of 2 resource views exist and 0 of 0 actions\nexist.\n\n" +
			"## Network\n| View | API | Lens columns | Actions | Status |\n|---|---|---|---|---|\n" +
			"| Services | `v1` | Name | – | partial ([`x.go`](../../internal/x.go)) |\n| Endpoints | `v1` | Name | – | partial ([`x.go`](../../internal/x.go)) |\n",
	}, nil, nil)
	n, _ := c.run(false)
	c.checkClaims(n, 0)
	got := messages(c)
	if !strings.Contains(got, `resource-parity.md:3: the views: the page says "1 of 2 resource views exist", the checklists count [2 2]`) {
		t.Errorf("no mismatch for the views:\n%s", got)
	}
	if strings.Contains(got, "the actions:") {
		t.Errorf("the actions match and the line break inside the sentence is allowed:\n%s", got)
	}
	if !strings.Contains(got, "ROADMAP.md:0: the page no longer reports the feature breadth where the checker looks") {
		t.Errorf("a claim whose sentence is gone is a problem:\n%s", got)
	}
}

func TestSlugIsGitHubsAnchor(t *testing.T) {
	for h, want := range map[string]string{
		"Every kind":                     "every-kind",
		"Cluster overview and charts":    "cluster-overview-and-charts",
		"Tables":                         "tables",
		"Flux CD › Helm Releases (paid)": "flux-cd--helm-releases-paid",
		"Detail drawer: `object.get`":    "detail-drawer-objectget",
	} {
		if got := slug(h); got != want {
			t.Errorf("slug(%q) = %q, want %q", h, got, want)
		}
	}
}

func TestRowTextIsPlainAndShort(t *testing.T) {
	if got := rowText("**Pods** with [link](x.md) and `code` a|b"); got != `Pods with link and code a\|b` {
		t.Errorf("rowText %q", got)
	}
	long := strings.Repeat("x", maxRowText+10)
	if got := []rune(rowText(long)); len(got) != maxRowText || got[len(got)-1] != '…' {
		t.Errorf("a long row: %d runes", len(got))
	}
}

// The generated page is written when it differs and refused under -check.
func TestChecklistPageWrittenOrRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/parity"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &checker{root: root, pages: map[string][]table{
		pageUI: parseTables(pageUI, "## Tables\n\n| Row | Plan | Status | Evidence |\n|---|---|---|---|\n| Sort by a column | free | done | `TestSort` |\n"),
	}}
	n := counts{}
	if c.syncChecklist(n, false); len(c.problems) != 1 {
		t.Fatalf("a missing page under -check: %v", c.problems)
	}
	c.problems = nil
	if !c.syncChecklist(n, true) || len(c.problems) != 0 {
		t.Fatalf("not written: %v", c.problems)
	}
	b, err := os.ReadFile(filepath.Join(root, pageChecklist))
	if err != nil || !strings.Contains(string(b), "| Sort by a column | done | [Tables](ui-parity.md#tables) |") {
		t.Fatalf("page %q, %v", b, err)
	}
	if c.syncChecklist(n, false); len(c.problems) != 0 {
		t.Fatalf("a current page refused: %v", c.problems)
	}
}
