package commands_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// H-40 (owner, 2026-10-14; D-2): the allowance to send a Helm repository's user and password over
// plain http is the user's, in the window. An agent and the CLI read why the repository waits
// (helm.repo.list) and are refused helm.repo.allowHTTPCredentials whatever they pass, the token
// included: nothing is recorded, nothing is requested, and the refusal is audited. The window's
// call allows it, and the repository then downloads with its credentials.
func TestAllowHTTPCredentialsIsTheUsers(t *testing.T) {
	const user, pass = "erin-http-user", "pw-http-93"
	h := newChartHarness(t, nil)
	plain := testkit.NewPlainHelmRepo(t, webChartV("0.1.0"))
	plain.RequireBasicAuth(user, pass)
	h.helmFile.Write(&repo.Entry{Name: "plain", URL: plain.URL, Username: user, Password: pass})

	var info engine.HelmRepoInfo
	var last engine.HelmReposList
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		last = h.list(t, engine.OriginExternalAgent)
		for _, r := range last.Repositories {
			if r.Name == "plain" {
				info = r
				return true
			}
		}
		return false
	}) {
		t.Fatalf("the repository was not listed: Helm's file %s %q", last.Status, last.Error)
	}
	if info.HTTPCredentials != engine.HTTPCredentialsRequired || info.HTTPToken == "" || !strings.Contains(info.Error, "agents cannot allow it") {
		t.Fatalf("an agent's list: %+v", info)
	}

	params := map[string]any{"repositories": []map[string]string{{"name": "plain", "token": info.HTTPToken}}}
	for _, o := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		_, err := h.dispatch(o, "helm.repo.allowHTTPCredentials", params)
		var ge *engine.GUIOnlyError
		if !errors.As(err, &ge) || !errors.Is(err, engine.ErrHelmHTTPGUIOnly) {
			t.Errorf("%s: %v", o, err)
		}
	}
	if got := h.list(t, engine.OriginGUI).Repositories[0]; got.HTTPCredentials != engine.HTTPCredentialsRequired {
		t.Fatalf("an agent's call allowed it: %+v", got)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.repo.update", struct{}{}); err != nil {
		t.Fatal(err)
	}
	if n := plain.Requests("/index.yaml"); n != 0 {
		t.Fatalf("the repository saw %d requests before the user allowed it", n)
	}
	if !strings.Contains(bufText(h.audit), "helm.repo.allowHTTPCredentials") {
		t.Error("the agent's refused call is not in the audit log")
	}

	res, err := h.dispatch(engine.OriginGUI, "helm.repo.allowHTTPCredentials", params)
	if err != nil {
		t.Fatal(err)
	}
	if infos := res.([]engine.HelmRepoInfo); len(infos) != 1 || infos[0].HTTPCredentials != engine.HTTPCredentialsAllowed {
		t.Fatalf("the user's call: %+v", infos)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.repo.update", struct{}{}); err != nil {
		t.Fatal(err)
	}
	if n, a := plain.Requests("/index.yaml"), plain.Authorized("/index.yaml"); n != 1 || a != 1 {
		t.Errorf("after the allowance: %d requests, %d with credentials", n, a)
	}
	for _, where := range []string{bufText(h.audit), bufText(h.logs)} {
		if strings.Contains(where, pass) || strings.Contains(where, user) {
			t.Error("a credential reached the audit log or the log")
		}
	}
}
