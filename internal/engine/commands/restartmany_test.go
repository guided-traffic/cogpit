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
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestRestartMany (A74): Restart on a selection of Deployments asks once for every origin, naming
// them all, then restarts each bound to its UID; declined, nothing is restarted; one created again
// under its name while the user was asked is not restarted, the others are; the selection's menu
// offers it for workloads only.
func TestRestartMany(t *testing.T) {
	var asked []engine.ConfirmRequest
	answer := false
	var during func()
	h := newHarness(t, seed(), engine.Options{Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		if during != nil {
			during()
		}
		return answer, nil
	})})
	refs := []model.ObjectRef{{Kind: "deployments", Namespace: "shop", Name: "web"}, {Kind: "deployments", Namespace: "infra", Name: "web"}}
	for _, r := range refs {
		waitUntil(t, r.String()+" in the cache", func() bool { _, err := h.s.Lookup(r); return err == nil })
	}
	params := map[string]any{"refs": refs}
	if err := h.run(t, engine.OriginGUI, "workload.restartMany", params, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("declined: %v", err)
	}
	if len(asked) != 1 || asked[0].Danger != engine.DangerDestructive || len(asked[0].Objects) != 2 {
		t.Fatalf("asked %+v", asked)
	}
	for _, c := range h.writer.Calls() {
		if strings.HasPrefix(c, "restart") {
			t.Fatalf("a declined restart ran: %v", h.writer.Calls())
		}
	}
	answer = true
	var res struct {
		Restarted []model.ObjectRef `json:"restarted"`
	}
	h.must(t, engine.OriginGUI, "workload.restartMany", params, &res)
	if len(res.Restarted) != 2 {
		t.Errorf("restarted %+v", res.Restarted)
	}
	n := 0
	for _, c := range h.writer.Calls() {
		if strings.HasPrefix(c, "restart") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("restart calls %v", h.writer.Calls())
	}
	if got := h.writer.UIDs(); !slices.Equal(got, []string{"uid-shop-web", "uid-infra-web"}) {
		t.Errorf("the restarts carried the UIDs %q, want the confirmed ones", got)
	}

	// shop/web is created again under its name while the user is asked.
	during = func() {
		again := testkit.Object("apps/v1", "Deployment", "shop", "web", nil)
		again.SetUID("uid-shop-web-again")
		h.src.Set(testkit.DeploymentsGVR, again)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if !testkit.WaitFor(ctx, func() bool {
			o, err := h.s.Lookup(refs[0])
			return err == nil && o.GetUID() == "uid-shop-web-again"
		}) {
			t.Error("the cache never showed shop/web created again") // inside the command: no Fatal
		}
	}
	before := len(h.writer.UIDs())
	err := h.run(t, engine.OriginGUI, "workload.restartMany", params, nil)
	var re *commands.RestartManyError
	if !errors.As(err, &re) || len(re.Restarted) != 1 || !re.Restarted[0].Same(refs[1]) || len(re.Failed) != 1 ||
		!re.Failed[0].Ref.Same(refs[0]) || !strings.Contains(re.Failed[0].Error, "no longer the object that was confirmed") {
		t.Fatalf("a selection with a recreated workload: %v (%+v)", err, re)
	}
	if got := h.writer.UIDs()[before:]; !slices.Equal(got, []string{"uid-infra-web"}) {
		t.Errorf("restarts after the recreation carried %q, want infra/web's alone", got)
	}
	during = nil

	ids := func(kinds []string) []string {
		var out []string
		for _, c := range h.s.Registry().SelectionActions(kinds) {
			out = append(out, c.ID)
		}
		return out
	}
	if !slices.Contains(ids([]string{"deployments"}), "workload.restartMany") || slices.Contains(ids([]string{"pods"}), "workload.restartMany") {
		t.Errorf("the selection's menu: deployments %v, pods %v", ids([]string{"deployments"}), ids([]string{"pods"}))
	}
	if err := h.run(t, engine.OriginGUI, "workload.restartMany", map[string]any{"refs": []model.ObjectRef{{Kind: "pods", Namespace: "shop", Name: "web-0"}}}, nil); err == nil {
		t.Error("a pod was accepted")
	}
}
