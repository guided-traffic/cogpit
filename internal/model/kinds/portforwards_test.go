package kinds_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utiljson "k8s.io/apimachinery/pkg/util/json"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// localProfile returns a local kind's profile (kinds.ByKey has the built-in ones only).
func localProfile(t testing.TB, key string) *model.KindProfile {
	t.Helper()
	for _, p := range kinds.Local() {
		if p.Key == key {
			return p
		}
	}
	t.Fatalf("no local profile %s", key)
	return nil
}

// TestLocalGolden is TestGolden for the local kinds: every recorded object against its golden
// file (-update rewrites them).
func TestLocalGolden(t *testing.T) {
	for _, p := range kinds.Local() {
		for _, c := range loadCases(t, p.Key) {
			t.Run(p.Key+"/"+c.name, func(t *testing.T) {
				got := encode(t, render(t, p, c.obj))
				path := strings.TrimSuffix(c.path, ".yaml") + ".golden.json"
				if *update {
					if err := os.WriteFile(path, got, 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%v (run go test ./internal/model/... -update)", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s differs from the golden file:\n%s", path, lineDiff(string(want), string(got)))
				}
			})
		}
	}
}

// The port forwards are Cogpit's own objects: a local kind of group cogpit.io, never an API
// object, with the columns captured from Lens's Port Forwarding view.
func TestPortForwardsProfile(t *testing.T) {
	p := localProfile(t, "portforwards")
	if !p.Local || p.Group != model.LocalGroup || p.Kind != "PortForward" || p.APIObject() || !p.Namespaced || p.Title != "Port Forwarding" {
		t.Errorf("profile %+v", p)
	}
	var titles []string
	for _, c := range p.Columns {
		titles = append(titles, c.Title)
	}
	if got := strings.Join(titles, ", "); got != "Name, Namespace, Kind, Pod Port, Local Port, Protocol, Status" {
		t.Errorf("columns %s", got)
	}
	if _, builtin := kinds.ByKey("portforwards"); builtin {
		t.Error("the port forwards are listed as a built-in kind")
	}
}

func TestPortForwardCells(t *testing.T) {
	p := localProfile(t, "portforwards")
	cases := map[string]*unstructured.Unstructured{}
	for _, c := range loadCases(t, p.Key) {
		cases[c.name] = c.obj
	}
	for _, tt := range []struct {
		name, column, text string
		status             model.Status
	}{
		{"active-pod", "name", "web-0", model.StatusOK},
		{"active-pod", "kind", "Pod", model.StatusOK},
		{"active-pod", "podPort", "8080", model.StatusOK},
		{"active-pod", "localPort", "54321", model.StatusOK},
		{"active-pod", "protocol", "http", model.StatusOK},
		{"active-pod", "status", "Active", model.StatusOK},
		{"active-service", "kind", "Service", model.StatusOK},
		{"active-service", "protocol", "https", model.StatusOK},
		{"reconnecting", "status", "Reconnecting", model.StatusWarning},
		{"waiting", "status", "Waiting", model.StatusWarning},
		{"stopped", "status", "Stopped", model.StatusNone},
		{"stopped", "localPort", "-", model.StatusNone}, // a free port is picked when it starts
		{"failed", "status", "Failed", model.StatusError},
		{"failed", "localPort", "8080", model.StatusError}, // the port it asked for
	} {
		row := model.BuildRow(p, "test", cases[tt.name])
		i := p.ColumnIndex(tt.column)
		if got := model.CellText(p.Columns[i], row.Cells[i], now); got != tt.text || row.Status != tt.status {
			t.Errorf("%s %s: %q (%q), want %q (%q)", tt.name, tt.column, got, row.Status, tt.text, tt.status)
		}
	}
}

// The URL in the drawer opens the forward: an item action that is the value's link.
func TestPortForwardURLOpens(t *testing.T) {
	p := localProfile(t, "portforwards")
	for _, c := range loadCases(t, p.Key) {
		if c.name != "active-pod" {
			continue
		}
		for _, it := range p.Detail(c.obj)[0].Items {
			if it.Name != "URL" {
				continue
			}
			if len(it.Actions) != 1 || !it.Actions[0].Link || it.Actions[0].Command != "portforward.open" {
				t.Fatalf("URL actions %+v", it.Actions)
			}
			var params struct{ Ref model.ObjectRef }
			if err := json.Unmarshal(it.Actions[0].Params, &params); err != nil || params.Ref.Name != "pod-web-0-8080" || params.Ref.Kind != "portforwards" {
				t.Errorf("params %s: %v", it.Actions[0].Params, err)
			}
			return
		}
		t.Fatal("no URL item")
	}
}

// Every TCP port of a container, init containers too, is a link that forwards it and has
// Forward… beside it; a UDP port has neither.
func TestContainerPortsForward(t *testing.T) {
	pods, _ := kinds.ByKey("pods")
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "web-0", "namespace": "shop"},
		"spec": map[string]any{
			"initContainers": []any{map[string]any{"name": "warmup", "ports": []any{map[string]any{"containerPort": int64(9000)}}}},
			"containers": []any{map[string]any{"name": "app", "ports": []any{
				map[string]any{"name": "dns", "containerPort": int64(53), "protocol": "UDP"},
				map[string]any{"name": "http", "containerPort": int64(8080)},
			}}},
		},
	}}
	got := map[string]string{}
	for _, sec := range pods.Detail(obj) {
		if sec.Group == "" {
			continue
		}
		for _, it := range sec.Items {
			if !strings.Contains(it.Value, "/TCP") && !strings.Contains(it.Value, "/UDP") {
				continue
			}
			desc := it.Name + "|"
			for _, a := range it.Actions {
				kind := "button"
				if a.Link {
					kind = "link"
				}
				desc += a.Command + ":" + string(a.Params) + ":" + kind + ";"
			}
			got[sec.Title+" "+it.Value] = desc
		}
	}
	want := map[string]string{
		"warmup 9000/TCP": `Ports|pod.portForward:{"container":"warmup","openInBrowser":true,"port":9000,"ref":{"kind":"pods","namespace":"shop","name":"web-0"}}:link;` +
			`pod.portForward:{"container":"warmup","port":9000,"ref":{"kind":"pods","namespace":"shop","name":"web-0"}}:button;`,
		"app dns: 53/UDP": "Ports|",
		"app http: 8080/TCP": `|pod.portForward:{"container":"app","openInBrowser":true,"port":8080,"ref":{"kind":"pods","namespace":"shop","name":"web-0"}}:link;` +
			`pod.portForward:{"container":"app","port":8080,"ref":{"kind":"pods","namespace":"shop","name":"web-0"}}:button;`,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s:\n got %s\nwant %s", k, got[k], w)
		}
	}
}

// TestForwardedPortShowsItsForward: a port whose forward listens shows the forward's local
// address, opens the forward and stops it, and has no Forward…; a port whose forward stopped, and
// the forward of another pod at the same port, leave a port as it is without one. A service's port
// does the same, its address coloured while the forward reconnects.
func TestForwardedPortShowsItsForward(t *testing.T) {
	active := recorded(t, "portforwards", "active-pod") // web-0 port 8080, Active on 127.0.0.1:54321
	stopped := recorded(t, "portforwards", "stopped")
	stopped.SetName("pod-web-0-9090")
	_ = unstructured.SetNestedField(stopped.Object, "web-0", "spec", "name")
	_ = unstructured.SetNestedField(stopped.Object, int64(9090), "spec", "port")
	other := recorded(t, "portforwards", "active-pod")
	other.SetName("pod-web-1-8080")
	_ = unstructured.SetNestedField(other.Object, "web-1", "spec", "name")
	_ = unstructured.SetNestedField(other.Object, int64(61000), "status", "localPort")
	_ = unstructured.SetNestedField(other.Object, "127.0.0.1:61000", "status", "address")
	rel := fakeRelated{"portforwards": {active, stopped, other, recorded(t, "portforwards", "reconnecting")}}

	if keys := kinds.ForwardsByTarget.Keys(active); !slices.Equal(keys, []string{"shop/Pod/web-0"}) {
		t.Errorf("keys %v", keys)
	}
	pods, _ := kinds.ByKey("pods")
	pod := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "web-0", "namespace": "shop"},
		"spec": map[string]any{"containers": []any{map[string]any{"name": "app", "ports": []any{
			map[string]any{"name": "http", "containerPort": int64(8080)},
			map[string]any{"name": "metrics", "containerPort": int64(9090)},
		}}}},
	}}
	svc := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "api", "namespace": "shop"},
		"spec": map[string]any{"selector": map[string]any{"app": "api"}, "ports": []any{
			map[string]any{"name": "http", "port": int64(80), "targetPort": int64(8080)},
		}},
	}}
	services, _ := kinds.ByKey("services")
	got := map[string]string{}
	for _, sec := range append(pods.DetailSections(pod, rel), services.DetailSections(svc, rel)...) {
		for _, it := range sec.Items {
			if !strings.Contains(it.Value, "/TCP") {
				continue
			}
			desc := it.Aside + "|" + string(it.AsideStatus) + "|"
			for _, a := range it.Actions {
				kind := "button"
				switch {
				case a.Link:
					kind = "link"
				case a.Icon:
					kind = "icon"
				}
				desc += a.Command + ":" + string(a.Params) + ":" + kind + ";"
			}
			got[it.Value] = desc
		}
	}
	want := map[string]string{
		"http: 8080/TCP": `127.0.0.1:54321|` + string(model.StatusOK) + `|` +
			`portforward.open:{"ref":{"kind":"portforwards","namespace":"shop","name":"pod-web-0-8080"}}:link;` +
			`portforward.stop:{"ref":{"kind":"portforwards","namespace":"shop","name":"pod-web-0-8080"}}:icon;`,
		"metrics: 9090/TCP": `||pod.portForward:{"container":"app","openInBrowser":true,"port":9090,"ref":{"kind":"pods","namespace":"shop","name":"web-0"}}:link;` +
			`pod.portForward:{"container":"app","port":9090,"ref":{"kind":"pods","namespace":"shop","name":"web-0"}}:button;`,
		"http: 80/TCP -> 8080": `127.0.0.1:61002|` + string(model.StatusWarning) + `|` +
			`portforward.open:{"ref":{"kind":"portforwards","namespace":"shop","name":"service-api-80"}}:link;` +
			`portforward.stop:{"ref":{"kind":"portforwards","namespace":"shop","name":"service-api-80"}}:icon;`,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s:\n got %s\nwant %s", k, got[k], w)
		}
	}
}

func FuzzPortForwards(f *testing.F) {
	p := localProfile(f, "portforwards")
	for _, c := range loadCases(f, p.Key) {
		f.Add(c.json)
	}
	for _, seed := range []string{`{}`, `{"metadata":null,"spec":null,"status":null}`,
		`{"spec":{"port":-1,"localPort":1e9,"kind":7},"status":{"localPort":"x","phase":[]}}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var m map[string]any
		if utiljson.Unmarshal(data, &m) != nil || m == nil {
			return
		}
		if err := exercise(p, &unstructured.Unstructured{Object: m}); err != nil {
			t.Fatal(err)
		}
	})
}
