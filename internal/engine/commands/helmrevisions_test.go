package commands_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release/common"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// revManifest renders shop at a revision: its Secret's password and the Deployment's replicas
// change with it, and revision 3 adds a ConfigMap and drops the metrics Service.
func revManifest(rev int) string {
	m := fmt.Sprintf(`---
# Source: shop/templates/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: shop-db
stringData:
  password: hunter%d
---
# Source: shop/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: shop
spec:
  replicas: %d
`, rev, rev)
	if rev < 3 {
		m += "---\n# Source: shop/templates/metrics.yaml\napiVersion: v1\nkind: Service\nmetadata:\n  name: shop-metrics\n"
	} else {
		m += "---\n# Source: shop/templates/config.yaml\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: shop-config\ndata:\n  mode: fast\n"
	}
	return m
}

// revRecord is a release Secret of shop at a revision with its own manifest and values.
func revRecord(t *testing.T, rev int, status common.Status) *unstructured.Unstructured {
	t.Helper()
	rls := &rspb.Release{
		Name: "shop", Namespace: "apps", Version: rev,
		Info:     &rspb.Info{Status: status, LastDeployed: releaseT0.Add(time.Duration(rev) * time.Hour), Description: fmt.Sprintf("Revision %d", rev)},
		Chart:    &chart.Chart{Metadata: &chart.Metadata{Name: "shop", Version: "1.0.0"}, Values: map[string]any{"replicas": 1, "image": "shop:1"}},
		Config:   map[string]any{"replicas": rev, "db": map[string]any{"password": fmt.Sprintf("hunter%d", rev)}},
		Manifest: revManifest(rev),
	}
	raw, err := json.Marshal(rls)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	s := testkit.Object("v1", "Secret", "apps", helm.RecordName("shop", rev), map[string]any{
		"type": "helm.sh/release.v1",
		"data": map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(buf.Bytes())))},
	})
	s.SetLabels(map[string]string{"owner": "helm", "name": "shop", "version": fmt.Sprint(rev), "status": string(status)})
	return s
}

var shopRef = model.ObjectRef{Context: "test", Kind: helm.ReleaseKind, Namespace: "apps", Name: "shop"}

func newRevisionsHarness(t *testing.T) *helmHarness {
	t.Helper()
	return newHelmHarness(t, engine.NewMemoryHelmJournal(), func(s *helmServer) {
		for rev, st := range map[int]common.Status{1: common.StatusSuperseded, 2: common.StatusSuperseded, 3: common.StatusDeployed} {
			s.src.Set(testkit.SecretsGVR, revRecord(t, rev, st))
		}
		s.putIn(helm.DriverConfigMap, "apps", "cm", 1, common.StatusSuperseded)
		s.putIn(helm.DriverConfigMap, "apps", "cm", 2, common.StatusDeployed)
	})
}

// An old revision's values, user-supplied and all, and its manifest's objects, from the cache; a
// release kept in ConfigMaps the same; a revision the cluster keeps no record of is refused; and
// nothing of it asks the API server once the release records are listed.
func TestReleaseRevision(t *testing.T) {
	h := newRevisionsHarness(t)
	res, err := h.dispatch(engine.OriginGUI, "helm.release.revision", map[string]any{"ref": shopRef, "revision": 1})
	if err != nil {
		t.Fatal(err)
	}
	lists, watches := h.src.Lists(), h.src.Watches()
	r := res.(commands.RevisionResult)
	if r.Revision.Revision != 1 || r.Status != "superseded" || r.Values != "db:\n  password: hunter1\nreplicas: 1\n" || r.All {
		t.Errorf("revision 1 %+v", r.Revision)
	}
	var objs []string
	for _, o := range r.Objects {
		objs = append(objs, o.Kind+"/"+o.Name)
	}
	if strings.Join(objs, ",") != "Secret/shop-db,Service/shop-metrics,Deployment/shop" {
		t.Errorf("objects %v", objs)
	}
	if sec := r.Objects[0]; !sec.Secret || !strings.Contains(sec.YAML, "hunter1") || strings.Contains(sec.Masked, "hunter1") || !strings.Contains(r.Manifest, "hunter1") {
		t.Errorf("the window's Secret %+v", sec)
	}

	res, err = h.dispatch(engine.OriginGUI, "helm.release.revision", map[string]any{"ref": shopRef, "revision": 1, "all": true})
	if err != nil {
		t.Fatal(err)
	}
	if v := res.(commands.RevisionResult).Values; !strings.Contains(v, "image: shop:1") || !strings.Contains(v, "replicas: 1") {
		t.Errorf("all values %q", v)
	}
	res, err = h.dispatch(engine.OriginGUI, "helm.release.revision", map[string]any{"ref": shopRef})
	if err != nil || res.(commands.RevisionResult).Revision.Revision != 3 {
		t.Errorf("the newest: %v %v", res, err)
	}
	res, err = h.dispatch(engine.OriginGUI, "helm.release.revision", map[string]any{"ref": cmRef, "revision": 1})
	if err != nil || res.(commands.RevisionResult).Revision.Revision != 1 || res.(commands.RevisionResult).Chart != "cm" {
		t.Errorf("a release kept in ConfigMaps: %v %v", res, err)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.revision", map[string]any{"ref": shopRef, "revision": 9}); !errors.Is(err, helm.ErrRevisionGone) {
		t.Errorf("a revision without a record: %v", err)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.diff", map[string]any{"ref": shopRef, "from": 3, "to": 1, "what": "manifest"}); err != nil {
		t.Fatal(err)
	}
	if h.src.Lists() != lists || h.src.Watches() != watches {
		t.Errorf("requests: lists %d → %d, watches %d → %d", lists, h.src.Lists(), watches, h.src.Watches())
	}
}

// Agents read a revision's values and its rendered Secrets' values withheld, in the objects and in
// the raw manifest, and the other objects as they are; the built-in agent on a private connection
// reads them as the window does.
func TestReleaseRevisionWithheldFromAgents(t *testing.T) {
	h := newRevisionsHarness(t)
	for _, call := range []struct {
		id     string
		params map[string]any
	}{
		{"helm.release.revision", map[string]any{"ref": shopRef, "revision": 1}},
		{"helm.release.revision", map[string]any{"ref": shopRef, "revision": 1, "all": true}},
		{"helm.release.diff", map[string]any{"ref": shopRef, "from": 3, "to": 1}},
		{"helm.release.diff", map[string]any{"ref": shopRef, "from": 3, "to": 1, "what": "allValues"}},
		{"helm.release.diff", map[string]any{"ref": shopRef, "from": 3, "to": 1, "what": "manifest", "object": "Secret./apps/shop-db"}},
		{"helm.release.diff", map[string]any{"ref": shopRef, "from": 3, "to": 1, "what": "manifest", "object": "Secret./apps/shop-db", "masked": false}},
	} {
		res, err := h.dispatch(engine.OriginExternalAgent, call.id, call.params)
		if err != nil {
			t.Fatal(call.id, err)
		}
		b, _ := json.Marshal(res)
		if strings.Contains(string(b), "hunter") {
			t.Errorf("%s %v: an agent reads a value: %s", call.id, call.params, b)
		}
	}
	res, err := h.dispatch(engine.OriginExternalAgent, "helm.release.revision", map[string]any{"ref": shopRef, "revision": 3})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(commands.RevisionResult)
	if r.Values != model.Withheld || !strings.Contains(r.Manifest, "mode: fast") || !strings.Contains(r.Manifest, "password: '"+model.Withheld) ||
		!strings.Contains(r.Objects[0].YAML, model.Withheld) || !strings.Contains(r.Objects[1].YAML, "mode: fast") {
		t.Errorf("an agent's revision %+v", r)
	}
	res, err = h.dispatch(engine.OriginExternalAgent, "helm.release.diff", map[string]any{"ref": shopRef, "from": 3, "to": 1})
	if err != nil {
		t.Fatal(err)
	}
	d := res.(commands.RevisionDiffResult)
	if len(d.Changes) != 2 || d.Changes[1].Path != "replicas" || d.Changes[1].From != model.Withheld || d.Lines != nil {
		t.Errorf("an agent's values diff %+v", d.RevisionDiff)
	}
}

// The values compared: the changed keys, from the compared revision to the one shown, and their
// line diff; the manifest compared: every object marked, the counts, an object's diff, a Secret's
// masked as the window shows it before Show.
func TestReleaseRevisionDiff(t *testing.T) {
	h := newRevisionsHarness(t)
	res, err := h.dispatch(engine.OriginGUI, "helm.release.diff", map[string]any{"ref": shopRef, "from": 3, "to": 1})
	if err != nil {
		t.Fatal(err)
	}
	d := res.(commands.RevisionDiffResult)
	if d.From != 3 || d.To != 1 || len(d.Changes) != 2 || d.Changes[0] != (helm.ValueChange{Path: "db.password", From: "hunter3", To: "hunter1"}) ||
		d.Changes[1] != (helm.ValueChange{Path: "replicas", From: "3", To: "1"}) || !engine.DiffChanged(d.Lines) {
		t.Errorf("values diff %+v", d.RevisionDiff)
	}

	res, err = h.dispatch(engine.OriginGUI, "helm.release.diff", map[string]any{"ref": shopRef, "from": 3, "to": 1, "what": "manifest", "object": "Deployment.apps/apps/shop"})
	if err != nil {
		t.Fatal(err)
	}
	d = res.(commands.RevisionDiffResult)
	var marks []string
	for _, c := range d.Objects {
		marks = append(marks, c.Change+c.Kind)
		if c.YAML != "" || c.Was != "" {
			t.Errorf("an object's YAML in the diff's list: %+v", c)
		}
	}
	if strings.Join(marks, ",") != "~Secret,+Service,~Deployment,-ConfigMap" || d.Added != 1 || d.Removed != 1 || d.Changed != 2 || d.Same != 0 {
		t.Errorf("objects %v %+v", marks, d.RevisionDiff)
	}
	var lines []string
	for _, l := range d.Lines {
		if l.Op != engine.DiffSame {
			lines = append(lines, string(l.Op)+l.Text)
		}
	}
	if strings.Join(lines, "|") != "-  replicas: 3|+  replicas: 1" {
		t.Errorf("the Deployment's diff %q", lines)
	}

	// Masked, hunter3 and hunter1 are alike (seven bytes each): no line differs.
	for masked, want := range map[bool]string{true: "", false: "-  password: hunter3|+  password: hunter1"} {
		res, err = h.dispatch(engine.OriginGUI, "helm.release.diff", map[string]any{"ref": shopRef, "from": 3, "to": 1, "what": "manifest", "object": "Secret./apps/shop-db", "masked": masked})
		if err != nil {
			t.Fatal(err)
		}
		lines = lines[:0]
		for _, l := range res.(commands.RevisionDiffResult).Lines {
			if l.Op != engine.DiffSame {
				lines = append(lines, string(l.Op)+l.Text)
			}
		}
		if strings.Join(lines, "|") != want {
			t.Errorf("masked %v: the Secret's diff %q", masked, lines)
		}
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.release.diff", map[string]any{"ref": shopRef, "what": "notes"}); err == nil {
		t.Error("an unknown comparison was run")
	}
}

// ui.helm.revisions opens one Revisions tab per release, compared with the newest revision by
// default (none for the newest), and activates it again at another revision; ui.helm.revisions.set
// changes what it shows; agents drive it as the window does.
func TestRevisionsTab(t *testing.T) {
	h := newRevisionsHarness(t)
	res, err := h.dispatch(engine.OriginGUI, commands.RevisionsCommand, map[string]any{"ref": shopRef, "revision": 1})
	if err != nil {
		t.Fatal(err)
	}
	first := res.(commands.DockResult)
	st := first.Tab.HelmRevisions
	if !first.Created || first.Tab.Type != engine.DockHelmRevisions || first.Tab.Title != "Revisions · shop" || st == nil ||
		*st != (engine.HelmRevisionsTab{Revision: 1, Compare: 3, Mode: engine.RevisionsValues}) {
		t.Fatalf("tab %+v %+v", first, st)
	}
	if ui := h.s.UIState(); ui.ActiveDock != first.Tab.ID || !ui.DockOpen {
		t.Errorf("the tab is not shown: %+v", ui.ActiveDock)
	}
	res, err = h.dispatch(engine.OriginExternalAgent, commands.RevisionsCommand, map[string]any{"ref": shopRef, "mode": "manifest"})
	if err != nil {
		t.Fatal(err)
	}
	again := res.(commands.DockResult)
	if again.Created || again.Tab.ID != first.Tab.ID || *again.Tab.HelmRevisions != (engine.HelmRevisionsTab{Revision: 3, Mode: engine.RevisionsManifest}) {
		t.Errorf("activated %+v %+v", again, again.Tab.HelmRevisions)
	}
	zero := 0
	res, err = h.dispatch(engine.OriginGUI, commands.RevisionsCommand, map[string]any{"ref": shopRef, "revision": 2, "compare": zero})
	if err != nil || res.(commands.DockResult).Tab.HelmRevisions.Compare != 0 {
		t.Errorf("compared with none: %v %v", res, err)
	}

	set := func(p map[string]any) (*engine.HelmRevisionsTab, error) {
		p["id"] = first.Tab.ID
		res, err := h.dispatch(engine.OriginExternalAgent, "ui.helm.revisions.set", p)
		if err != nil {
			return nil, err
		}
		return res.(commands.DockResult).Tab.HelmRevisions, nil
	}
	got, err := set(map[string]any{"compare": 1, "object": "Deployment.apps/apps/shop", "raw": true, "all": true, "lineDiff": true})
	if err != nil || *got != (engine.HelmRevisionsTab{Revision: 2, Compare: 1, Mode: engine.RevisionsManifest, All: true, Raw: true, Object: "Deployment.apps/apps/shop", LineDiff: true}) {
		t.Errorf("set %+v %v", got, err)
	}
	if got, err = set(map[string]any{"revision": 1}); err != nil || got.Compare != 0 || got.Object != "" {
		t.Errorf("the revision compared becomes the one shown: %+v %v", got, err)
	}
	for _, bad := range []map[string]any{{"revision": 9}, {"compare": 9}, {"compare": 1}, {"mode": "notes"}} {
		if _, err := set(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if _, err := h.dispatch(engine.OriginGUI, "ui.helm.revisions.set", map[string]any{"id": "logs-9"}); err == nil {
		t.Error("an unknown tab was set")
	}
}
