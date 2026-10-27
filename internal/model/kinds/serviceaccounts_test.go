package kinds_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzServiceAccounts(f *testing.F) { fuzzProfile(f, "serviceaccounts") }

// TestServiceAccountColumns pins the columns of Lens's documentation: Name, Namespace, Age.
func TestServiceAccountColumns(t *testing.T) {
	assertColumnOrder(t, "serviceaccounts", []model.Column{
		{ID: "name", Title: "Name"},
		{ID: "namespace", Title: "Namespace"},
		{ID: "age", Title: "Age"},
	})
}

// TestServiceAccountDetail: Automount Token as set, else the API's default; the Secrets and Image
// Pull Secrets by name, each once, as chips that link to the Secret in the account's namespace.
func TestServiceAccountDetail(t *testing.T) {
	p, _ := kinds.ByKey("serviceaccounts")
	cases := map[string]*model.DetailSection{}
	for _, c := range loadCases(t, "serviceaccounts") {
		s := p.Detail(c.obj)
		if len(s) != 1 || s[0].Title != model.TitleProperties {
			t.Fatalf("%s: sections %+v", c.name, s)
		}
		cases[c.name] = &s[0]
	}
	if got := accessItem(t, cases["healthy"], "Automount Token").Value; got != "false" {
		t.Errorf("healthy automount %q", got)
	}
	if got := accessItem(t, cases["minimal"], "Automount Token").Value; got != "true (default)" {
		t.Errorf("minimal automount %q", got)
	}
	pull := accessItem(t, cases["failing"], "Image Pull Secrets")
	if pull.Value != "registry-credentials\nmirror-credentials" || len(pull.Chips) != 2 {
		t.Fatalf("image pull secrets %+v", pull)
	}
	if l := pull.Chips[0].Link; l == nil || *l != (model.ObjectRef{Kind: "secrets", Namespace: "ci", Name: "registry-credentials"}) {
		t.Errorf("image pull secret link %+v", l)
	}
	if got := accessItem(t, cases["failing"], "Secrets").Value; got != "build-bot-token-x7k2p" {
		t.Errorf("secrets %q", got)
	}
	for _, name := range []string{"Secrets", "Image Pull Secrets"} {
		for _, it := range cases["minimal"].Items {
			if it.Name == name {
				t.Errorf("minimal shows an empty %s", name)
			}
		}
	}
}

// accessItem returns the item of a drawer section with the given name.
func accessItem(t *testing.T, s *model.DetailSection, name string) model.DetailItem {
	t.Helper()
	for _, it := range s.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("section %s has no item %q: %+v", s.Title, name, s.Items)
	return model.DetailItem{}
}
