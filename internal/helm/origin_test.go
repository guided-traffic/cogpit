package helm

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A repository's origin says when nothing authenticates its content (H-41, A14): plain http, or
// https whose entry says insecure_skip_tls_verify; plain http wins over the entry's TLS setting,
// which does not apply to it.
func TestRepositoryOrigin(t *testing.T) {
	ca := filepath.Join(t.TempDir(), "ca.pem") // absolute on every system; the file is not read
	for _, c := range []struct{ entry, want string }{
		{"- name: a\n  url: https://charts.example.com\n", ""},
		{"- name: a\n  url: https://charts.example.com\n  caFile: " + ca + "\n", ""},
		{"- name: a\n  url: http://charts.example.com\n", kinds.HelmOriginHTTP},
		{"- name: a\n  url: HTTP://charts.example.com:8080/stable\n", kinds.HelmOriginHTTP},
		{"- name: a\n  url: https://charts.example.com\n  insecure_skip_tls_verify: true\n", kinds.HelmOriginInsecureTLS},
		{"- name: a\n  url: http://charts.example.com\n  insecure_skip_tls_verify: true\n", kinds.HelmOriginHTTP},
		{"- name: a.b\n  url: https://charts.example.com\n  insecure_skip_tls_verify: true\n", kinds.HelmOriginInsecureTLS},
	} {
		if got := readEntry(t, c.entry).Origin(); got != c.want {
			t.Errorf("%q: origin %q, want %q", c.entry, got, c.want)
		}
	}
	if (*Repository)(nil).Origin() != "" {
		t.Error("a nil repository has an origin")
	}
	for _, o := range []string{kinds.HelmOriginHTTP, kinds.HelmOriginInsecureTLS} {
		if kinds.HelmOriginMark(o) == "" || kinds.HelmOriginWarning("lab", o) == "" {
			t.Errorf("origin %q has no mark or warning", o)
		}
	}
}

// A chart keeps the origin of the repository it was downloaded from: what the Install tab, the
// preview and the confirmations mark.
func TestChartOrigin(t *testing.T) {
	tlsRepo := testkit.NewHelmRepo(t, webChart)
	plainRepo := testkit.NewPlainHelmRepo(t, webChart)
	insecure := PublicRepository("edge", tlsRepo.URL)
	insecure.InsecureSkipTLSVerify = true
	for _, c := range []struct {
		repo   *Repository
		client *testkit.HelmRepo
		want   string
	}{
		{PublicRepository("checked", tlsRepo.URL), tlsRepo, ""},
		{insecure, tlsRepo, kinds.HelmOriginInsecureTLS},
		{PublicRepository("lab", plainRepo.URL), plainRepo, kinds.HelmOriginHTTP},
	} {
		f := &Fetcher{Client: c.client.Client()}
		idx, err := f.DownloadIndex(context.Background(), c.repo, filepath.Join(t.TempDir(), "index.yaml"))
		if err != nil {
			t.Fatalf("%s: %v", c.repo.Name, err)
		}
		web, _ := idx.Chart("web")
		v, _ := web.Latest()
		ch, err := f.FetchChart(context.Background(), c.repo, "web", v)
		if err != nil {
			t.Fatalf("%s: %v", c.repo.Name, err)
		}
		if ch.Origin != c.want || ch.Repository != c.repo.Name {
			t.Errorf("%s: the chart's origin %q, want %q", c.repo.Name, ch.Origin, c.want)
		}
	}
}
