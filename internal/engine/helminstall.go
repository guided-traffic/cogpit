package engine

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Helm Install dock tab (G11, architecture §8): a chart's values.yaml from its repository in
// an editor whose text lives in the engine (editor.get, editor.setText, as a Helm Upgrade tab's),
// with the chart, its version and the versions the repository lists on the tab (DockTab.HelmInstall).
// Opening the tab downloads the chart's archive (checked against the index's digest) and keeps it
// in memory with the tab: the install installs exactly the archive whose values the tab shows.
// helm.chart.preview renders it against the cluster without changing it; helm.chart.install
// installs it, crash-safe through the Helm journal (helmops.go).

// helmInstallTitle starts a Helm Install tab's title (inferred from Lens's "Helm Upgrade: <name>",
// captured; Lens's install tab is not captured).
const helmInstallTitle = "Helm Install: "

// HelmInstallTab is what a Helm Install tab shows beside its values editor (inferred: Lens's
// "Helm Install: <chart>" tab is not captured): the chart and the repository it comes from, the
// version the install takes and the versions its picker offers, whether the archive matched its
// index's digest, and the namespace and release name the tab starts with. The tab's Ref is the
// chart (kind helmcharts); its values are the tab's editor text (editor.get, editor.setText),
// which agents read withheld (model.Withheld; a private turn as the window shows it, H-94), as a
// Helm Upgrade tab's.
type HelmInstallTab struct {
	Repository string `json:"repository"`
	Chart      string `json:"chart"`
	Version    string `json:"version"`
	AppVersion string `json:"appVersion,omitempty"`
	// Versions are the picker's choices: every version the repository's index lists, newest first.
	Versions []string `json:"versions"`
	// Digest is the downloaded archive's SHA-256; Verified says it matched the index's digest
	// (false: the index lists none).
	Digest   string `json:"digest"`
	Verified bool   `json:"verified"`
	// Origin is how the archive came when nothing authenticated it (H-41, A14): kinds.HelmOriginHTTP
	// or kinds.HelmOriginInsecureTLS, its repository's at the download; empty for https with the
	// certificate checked. The tab marks it in the warning colour, and the install's preview and
	// confirmation say it (kinds.HelmOriginWarning).
	Origin string `json:"origin,omitempty"`
	// Namespace and ReleaseName are what the tab offers at first: the namespace filter's single
	// namespace (else the context's, else "default") and a name made from the chart's.
	Namespace   string `json:"namespace"`
	ReleaseName string `json:"releaseName"`
}

// helmInstallBase is the chart a Helm Install tab installs, as downloaded when the tab opened (or
// its version changed). Never changed in place: a reload sets a new one.
type helmInstallBase struct {
	chart *helm.Chart
}

// HelmInstaller renders and installs charts. The Helm SDK on the backend's connection implements
// it; tests put a fake in Backend.Actions, which the engine reaches with a type assertion on
// Backend.Writer, as it reaches HelmWriter.
type HelmInstaller interface {
	// PreviewInstall renders an install against the cluster without changing it (the SDK's dry
	// run) and names the objects it would create. Errors never quote a value the user changed.
	PreviewInstall(ctx context.Context, req helm.InstallRequest) (helm.InstallPreview, error)
	// InstallChart installs a chart as a new release and returns its revision. An error that left
	// no release matches helm.ErrUnchanged; errors never quote a value the user changed.
	InstallChart(ctx context.Context, req helm.InstallRequest) (revision int, err error)
}

// HelmInstaller returns what installs charts on the backend: the writer in Backend.Actions if it
// implements HelmInstaller, else the Helm SDK on the backend's connection.
func (b *Backend) HelmInstaller() (HelmInstaller, error) {
	w := b.Writer()
	if hi, ok := w.(HelmInstaller); ok {
		return hi, nil
	}
	if w == nil || b.Conn == nil {
		return nil, ErrNoHelmWriter
	}
	return sdkHelm{helm.NewActions(b.Conn, b.logger())}, nil
}

func (h sdkHelm) PreviewInstall(ctx context.Context, req helm.InstallRequest) (helm.InstallPreview, error) {
	return h.a.PreviewInstall(ctx, req)
}

func (h sdkHelm) InstallChart(ctx context.Context, req helm.InstallRequest) (int, error) {
	return h.a.Install(ctx, req)
}

// OpenHelmInstallTab opens a Helm Install tab on a chart of a repository at a version ("": the
// version the chart is listed with), its editor filled with the chart's values.yaml: the chart is
// downloaded (bounded, checked against the index's digest) and kept with the tab. A tab already
// open on the chart is activated, and reloaded at version when that is another one.
func (s *Session) OpenHelmInstallTab(ctx context.Context, contextID, repository, chart, version string) (EditorState, bool, error) {
	repos := s.opts.HelmRepos
	if repos == nil {
		return EditorState{}, false, ErrNoHelmRepos
	}
	b, err := s.TargetBackend(contextID) // the cluster it installs into, "" the shown one (D-69)
	if err != nil {
		return EditorState{}, false, err
	}
	for _, t := range s.UIState().Dock {
		if t.Type != DockHelmInstall || t.HelmInstall == nil || t.Context != b.Context ||
			t.HelmInstall.Repository != repository || t.HelmInstall.Chart != chart {
			continue
		}
		if version != "" && version != t.HelmInstall.Version {
			st, err := s.ReloadHelmInstallTab(ctx, t.ID, version)
			return st, false, err
		}
		id := t.ID
		s.UpdateUI(func(u *UIState) { u.ShowDockTab(id) })
		st, err := s.EditorState(ctx, id)
		return st, false, err
	}
	ch, ic, err := repos.FetchChart(ctx, repository, chart, version)
	if err != nil {
		return EditorState{}, false, err
	}
	defaults := ch.DefaultValues()
	if len(defaults) > MaxManifestBytes {
		return EditorState{}, false, fmt.Errorf("chart %s/%s: the values are larger than the %s an editor takes", repository, chart, model.FormatBytes(MaxManifestBytes))
	}
	ref := HelmChartRef(b.Context, repository, chart)
	ui := s.UIState()
	e := &editor{
		tab: DockTab{
			ID: s.NewDockID(DockHelmInstall), Type: DockHelmInstall, Title: helmInstallTitle + chart, Context: b.Context, Ref: &ref,
			HelmInstall: installTabOf(ch, ic, installNamespace(ui, b), defaultReleaseName(chart)),
		},
		text: defaults, rev: 1, install: &helmInstallBase{chart: ch},
	}
	s.putEditor(e)
	tab, created := e.tab, true
	ui = s.UpdateUI(func(u *UIState) {
		for _, x := range u.Dock {
			if x.Type == DockHelmInstall && x.HelmInstall != nil && x.Context == b.Context &&
				x.HelmInstall.Repository == repository && x.HelmInstall.Chart == chart {
				tab, created = x, false // opened meanwhile
				break
			}
		}
		if created {
			u.Dock = append(u.Dock, tab)
		}
		u.ShowDockTab(tab.ID)
	})
	s.settleEditor(e.tab.ID, created, ui.Revision)
	st, err := s.EditorState(ctx, tab.ID)
	return st, created, err
}

// ReloadHelmInstallTab downloads another version of a Helm Install tab's chart and replaces the
// tab's chart, version and values with that version's values.yaml: the user's changes to the
// values go, as they would with another chart.
func (s *Session) ReloadHelmInstallTab(ctx context.Context, id, version string) (EditorState, error) {
	e, err := s.editorOf(id)
	if err != nil {
		return EditorState{}, err
	}
	if e.tab.Type != DockHelmInstall || e.tab.HelmInstall == nil {
		return EditorState{}, fmt.Errorf("dock tab %q is a %s tab, not a Helm Install tab", id, e.tab.Type)
	}
	repos := s.opts.HelmRepos
	if repos == nil {
		return EditorState{}, ErrNoHelmRepos
	}
	old := e.tab.HelmInstall
	ch, ic, err := repos.FetchChart(ctx, old.Repository, old.Chart, version)
	if err != nil {
		return EditorState{}, err
	}
	defaults := ch.DefaultValues()
	if len(defaults) > MaxManifestBytes {
		return EditorState{}, fmt.Errorf("chart %s/%s: the values are larger than the %s an editor takes", old.Repository, old.Chart, model.FormatBytes(MaxManifestBytes))
	}
	info := installTabOf(ch, ic, old.Namespace, old.ReleaseName)
	if _, err := s.updateEditor(id, func(e *editor) {
		e.install = &helmInstallBase{chart: ch} // a new value: copies of the editor share the old one
		e.tab.HelmInstall = info
		if e.text != defaults {
			e.text = defaults
			e.rev++
		}
	}); err != nil {
		return EditorState{}, err
	}
	s.UpdateUI(func(u *UIState) {
		for i, t := range u.Dock {
			if t.ID == id {
				u.Dock[i].HelmInstall = info
			}
		}
		u.ShowDockTab(id)
	})
	return s.EditorState(ctx, id)
}

// installTabOf describes a downloaded chart for its tab.
func installTabOf(ch *helm.Chart, ic helm.IndexChart, namespace, release string) *HelmInstallTab {
	t := &HelmInstallTab{
		Repository: ch.Repository, Chart: ch.Name, Version: ch.Version, AppVersion: ch.AppVersion,
		Digest: ch.Digest, Verified: ch.Verified, Origin: ch.Origin, Namespace: namespace, ReleaseName: release, Versions: []string{},
	}
	for _, v := range ic.Versions {
		t.Versions = append(t.Versions, v.Version)
	}
	return t
}

// installNamespace is the namespace a Helm Install tab starts with: the namespace filter's, when
// it names one, else the context's default namespace, else "default".
func installNamespace(u UIState, b *Backend) string {
	switch {
	case len(u.Namespaces) == 1:
		return u.Namespaces[0]
	case b.Namespace != "":
		return b.Namespace
	}
	return defaultNamespace
}

// defaultReleaseName is a release name made from a chart's name and five random letters and
// digits, as Kubernetes generates names (inferred: helm install --generate-name appends a
// timestamp, and Lens's default is not captured).
func defaultReleaseName(chart string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(chart) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0:
			b.WriteByte('-')
		}
		if b.Len() >= 46 {
			break
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "release"
	}
	const alphabet = "bcdfghjklmnpqrstvwxz2456789"
	suffix := make([]byte, 5)
	_, _ = rand.Read(suffix)
	for i, c := range suffix {
		suffix[i] = alphabet[int(c)%len(alphabet)]
	}
	return name + "-" + string(suffix)
}

// HelmInstallTabInfo is a Helm Install tab as helm.chart.install uses it: the tab, its values,
// the text revision they are at, and the chart it installs.
type HelmInstallTabInfo struct {
	Tab          DockTab
	Values       string
	TextRevision uint64
	Chart        *helm.Chart
}

// HelmInstallTab returns an open Helm Install tab's state.
func (s *Session) HelmInstallTab(id string) (HelmInstallTabInfo, error) {
	e, err := s.editorOf(id)
	if err != nil {
		return HelmInstallTabInfo{}, err
	}
	if e.tab.Type != DockHelmInstall || e.install == nil || e.tab.HelmInstall == nil {
		return HelmInstallTabInfo{}, fmt.Errorf("dock tab %q is a %s tab, not a Helm Install tab", id, e.tab.Type)
	}
	return HelmInstallTabInfo{Tab: e.tab, Values: e.text, TextRevision: e.rev, Chart: e.install.chart}, nil
}

// HelmValuesTab reports whether a dock tab type holds Helm values in its editor (an Upgrade or an
// Install tab): agents read the text withheld, and the audit log records it by size only.
func HelmValuesTab(t DockType) bool { return t == DockHelmUpgrade || t == DockHelmInstall }

// HelmReleaseRef is the reference of a release in the connected context.
func HelmReleaseRef(context, namespace, name string) model.ObjectRef {
	return model.ObjectRef{Context: context, Kind: helm.ReleaseKind, Namespace: namespace, Name: name}
}
