package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

const (
	httpUser = "carol-http-user"
	httpPass = "pw-http-77"
)

// plainRepoWithLogin is a repository over plain http behind a login, and its entry in Helm's file.
func plainRepoWithLogin(t *testing.T) (*testkit.HelmRepo, *repo.Entry) {
	t.Helper()
	r := testkit.NewPlainHelmRepo(t, testkit.TestChart{Name: "web", Version: "0.1.0",
		Templates: map[string]string{"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"}})
	r.RequireBasicAuth(httpUser, httpPass)
	return r, &repo.Entry{Name: "plain", URL: r.URL, Username: httpUser, Password: httpPass}
}

func infoOf(t *testing.T, h *HelmRepos, name string) HelmRepoInfo {
	t.Helper()
	l, err := h.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range l.Repositories {
		if r.Name == name {
			raw, _ := json.Marshal(r)
			if strings.Contains(string(raw), httpPass) || strings.Contains(string(raw), httpUser) {
				t.Fatalf("the list shows a credential: %s", raw)
			}
			return r
		}
	}
	t.Fatalf("no repository %s in %+v", name, l.Repositories)
	return HelmRepoInfo{}
}

// H-40 (owner, 2026-10-14): a repository over plain http with a user and password downloads
// nothing, neither on an update nor in the background nor for a chart or its README, until the user
// allowed it; the list says why and offers a token. A wrong token allows nothing. Once allowed it
// downloads as helm does, the background refresh included, and the allowance outlives the session
// (its file). A changed URL or user asks again; a changed password does not.
func TestHelmCredentialsOverHTTPWaitForTheUser(t *testing.T) {
	r, entry := plainRepoWithLogin(t)
	approvals := filepath.Join(t.TempDir(), "config", HelmHTTPApprovalsFile)
	h, f, _ := helmReposOn(t, r, WithHTTPApprovals(approvals))
	f.Write(entry)
	mustSync(t, h)

	info := infoOf(t, h, "plain")
	if info.HTTPCredentials != HTTPCredentialsRequired || info.HTTPToken == "" || !strings.Contains(info.Error, "plain http") {
		t.Fatalf("waiting: %+v", info)
	}
	_, err := h.Update(context.Background(), nil)
	var he *HTTPCredentialsError
	if !errors.As(err, &he) || !errors.Is(err, helm.ErrCredentialsOverHTTP) || he.State != HTTPCredentialsRequired {
		t.Fatalf("an update before the allowance: %v", err)
	}
	h.refreshStale(context.Background())
	if _, _, err := h.FetchChart(context.Background(), "plain", "web", "0.1.0"); err == nil {
		t.Error("a chart downloaded before the allowance")
	}
	if n := r.Requests("/index.yaml"); n != 0 {
		t.Fatalf("the repository saw %d requests before the allowance", n)
	}
	if _, err := os.Stat(approvals); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an allowances file without an allowance: %v", err)
	}

	if _, err := h.AllowHTTP([]HelmHTTPAllowItem{{Name: "plain", Token: "0123"}}); !errors.Is(err, ErrHelmHTTPChangedSinceShown) {
		t.Fatalf("a wrong token: %v", err)
	}
	if got := infoOf(t, h, "plain"); got.HTTPCredentials != HTTPCredentialsRequired {
		t.Fatalf("a wrong token allowed it: %+v", got)
	}

	infos, err := h.AllowHTTP([]HelmHTTPAllowItem{{Name: "plain", Token: info.HTTPToken}})
	if err != nil || len(infos) != 1 || infos[0].HTTPCredentials != HTTPCredentialsAllowed || infos[0].HTTPToken != "" {
		t.Fatalf("allowed: %+v %v", infos, err)
	}
	if !h.state["plain"].fetchNow {
		t.Error("the allowed repository is not downloaded at once")
	}
	h.refreshStale(context.Background()) // the background refresh, as while the Charts view is open
	if n, a := r.Requests("/index.yaml"), r.Authorized("/index.yaml"); n != 1 || a != 1 {
		t.Fatalf("after the allowance the repository saw %d requests, %d with credentials", n, a)
	}
	if got := infoOf(t, h, "plain"); got.Charts != 1 || got.Error != "" {
		t.Fatalf("after the allowance: %+v", got)
	}
	if _, _, err := h.FetchChart(context.Background(), "plain", "web", "0.1.0"); err != nil {
		t.Errorf("a chart after the allowance: %v", err)
	}
	raw, err := os.ReadFile(approvals)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), httpPass) || strings.Contains(string(raw), httpUser) {
		t.Errorf("the allowances file holds a credential: %s", raw)
	}
	checkMode(t, approvals, 0o600)

	// A new session reads the allowance.
	h2, f2, _ := helmReposOn(t, r, WithHTTPApprovals(approvals))
	f2.Write(entry)
	mustSync(t, h2)
	if got := infoOf(t, h2, "plain"); got.HTTPCredentials != HTTPCredentialsAllowed {
		t.Errorf("the next session: %+v", got)
	}

	// A changed password keeps it, a changed user or URL asks again.
	changed := *entry
	changed.Password = "pw-http-78"
	f2.Write(&changed)
	mustSync(t, h2)
	if got := infoOf(t, h2, "plain"); got.HTTPCredentials != HTTPCredentialsAllowed {
		t.Errorf("a changed password: %+v", got)
	}
	for what, e := range map[string]repo.Entry{
		"user": {Name: "plain", URL: r.URL, Username: "dave-http-user", Password: httpPass},
		"URL":  {Name: "plain", URL: r.URL + "/other", Username: httpUser, Password: httpPass},
	} {
		f2.Write(&e)
		mustSync(t, h2)
		got := infoOf(t, h2, "plain")
		if got.HTTPCredentials != HTTPCredentialsChanged || got.HTTPToken == "" || !strings.Contains(got.Error, "another URL or user name") {
			t.Errorf("a changed %s: %+v", what, got)
		}
		before := r.Requests("/other/index.yaml") + r.Requests("/index.yaml")
		if _, err := h2.Update(context.Background(), nil); !errors.Is(err, helm.ErrCredentialsOverHTTP) {
			t.Errorf("a changed %s downloads: %v", what, err)
		}
		if after := r.Requests("/other/index.yaml") + r.Requests("/index.yaml"); after != before {
			t.Errorf("a changed %s: %d requests", what, after-before)
		}
	}
}

// A repository over plain http without credentials needs no allowance (only trust-boundaries H-41
// applies), and an https repository with credentials none either.
func TestHelmHTTPWithoutCredentialsNeedsNoAllowance(t *testing.T) {
	r := testkit.NewPlainHelmRepo(t, testkit.TestChart{Name: "web", Version: "0.1.0",
		Templates: map[string]string{"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"}})
	h, f, _ := helmReposOn(t, r)
	f.Write(&repo.Entry{Name: "public", URL: r.URL})
	mustSync(t, h)
	if got := infoOf(t, h, "public"); got.HTTPCredentials != "" || got.HTTPToken != "" || !got.PlainHTTP {
		t.Fatalf("a public http repository: %+v", got)
	}
	if _, err := h.Update(context.Background(), nil); err != nil || r.Requests("/index.yaml") != 1 {
		t.Fatalf("a public http repository: %v, %d requests", err, r.Requests("/index.yaml"))
	}
	if _, err := h.AllowHTTP([]HelmHTTPAllowItem{{Name: "public", Token: "x"}}); err == nil {
		t.Error("allowed a repository that sends no credentials")
	}

	tls := webRepo(t)
	tls.RequireBasicAuth(httpUser, httpPass)
	h2, f2, _ := helmReposOn(t, tls)
	f2.Write(&repo.Entry{Name: "secure", URL: tls.URL, Username: httpUser, Password: httpPass})
	mustSync(t, h2)
	if got := infoOf(t, h2, "secure"); got.HTTPCredentials != "" {
		t.Fatalf("an https repository: %+v", got)
	}
	if _, err := h2.Update(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

// An allowances file that cannot be read allows nothing and says why; it is never overwritten.
func TestHelmHTTPUnreadableApprovals(t *testing.T) {
	r, entry := plainRepoWithLogin(t)
	approvals := filepath.Join(t.TempDir(), HelmHTTPApprovalsFile)
	if err := os.WriteFile(approvals, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, f, _ := helmReposOn(t, r, WithHTTPApprovals(approvals))
	f.Write(entry)
	mustSync(t, h)
	got := infoOf(t, h, "plain")
	if got.HTTPCredentials != HTTPCredentialsRequired || !strings.Contains(got.Error, "unreadable") {
		t.Fatalf("an unreadable file: %+v", got)
	}
	h.refreshStale(context.Background())
	if _, err := h.Update(context.Background(), nil); err == nil || r.Requests("/index.yaml") != 0 {
		t.Fatalf("an unreadable file: %v, %d requests", err, r.Requests("/index.yaml"))
	}
	raw, _ := os.ReadFile(approvals)
	if string(raw) != "{not json" {
		t.Error("the unreadable file was overwritten")
	}
}
