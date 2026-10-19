package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Installing a chart (G11, architecture §8): Install on a chart (the Charts view's row menu and
// drawer) opens a "Helm Install: <chart>" dock tab with the chart's values.yaml from its
// repository; helm.chart.preview renders the install against the cluster without changing it and
// names the objects; helm.chart.install installs the chart through the Helm SDK in Cogpit's
// process, mutating (privilege-footprint.md), recorded in the Helm journal first (D-11). The
// values are the user's: agents read them withheld (the built-in agent over a private connection
// as the window shows them, H-94), and no log, error, toast, audit line or journal entry carries
// them; an agent's inline values are shown to the user before they answer (D-27).

// orderInstall is Install's place in a chart's row menu and drawer: its only action.
const orderInstall = 100

type installTabParams struct {
	Ref     model.ObjectRef `json:"ref" jsonschema:"the chart: kind helmcharts, name the chart's id as resources_list lists it (<repository>.<chart>, every dot of the repository's name doubled: charts..example.nginx for repository charts.example)"`
	Version string          `json:"version,omitempty" jsonschema:"the chart version; empty: the version the chart's drawer shows when it shows this chart (ui_setChartVersion), else the version the chart is listed with (the newest that is not a prerelease)"`
}

type installParams struct {
	engine.OnCluster
	Tab string `json:"tab,omitempty" jsonschema:"a Helm Install dock tab id as helm_chart_installTab returned it, e.g. helmInstall-1: install its chart and version with its values (editor_setText changes them); give tab, or repository, chart, version and values"`
	// TextRevision binds an install from a tab to the text the user reviewed.
	TextRevision uint64 `json:"textRevision,omitempty" jsonschema:"with tab: the tab's text revision the install takes the values at (editor_get and editor_setText return it); the install is refused when the text changed since"`
	Repository   string `json:"repository,omitempty" jsonschema:"without tab: the chart's repository as helm_repo_list lists it"`
	Chart        string `json:"chart,omitempty" jsonschema:"without tab: the chart's name"`
	Version      string `json:"version,omitempty" jsonschema:"without tab: the chart version (the helmcharts object's spec.versions lists them)"`
	// Values is a pointer so that empty values (the chart's defaults alone) differ from none given.
	Values          *string `json:"values,omitempty" jsonschema:"without tab: the release's values as YAML, which become its user-supplied values (empty: the chart's defaults alone)"`
	Namespace       string  `json:"namespace" jsonschema:"the namespace to install the release into"`
	Name            string  `json:"name" jsonschema:"the release's name: lowercase letters, digits, '-' and '.', at most 53 characters"`
	CreateNamespace bool    `json:"createNamespace,omitempty" jsonschema:"create the namespace if it does not exist (helm install --create-namespace); default: no"`
}

// InstallPreview is what helm.chart.preview returns: the release, the chart and version with the
// archive's digest, and the objects the install would create, by kind and name.
type InstallPreview struct {
	Release    model.ObjectRef `json:"release"`
	Repository string          `json:"repository"`
	Chart      string          `json:"chart"`
	Version    string          `json:"version"`
	Digest     string          `json:"digest"`
	Verified   bool            `json:"verified"`
	// Origin is how the archive came when nothing authenticated it (H-41, A14): "http" or
	// "insecure-tls"; absent for https with the certificate checked. Warning says what it means.
	Origin  string `json:"origin,omitempty"`
	Warning string `json:"warning,omitempty"`
	// Offline: rendered without the cluster (agents and the CLI): no lookup, the default
	// capabilities, no check for objects that exist already.
	Offline bool `json:"offline,omitempty"`
	helm.InstallPreview
}

// installSource is what an install installs: the chart and version, the origin it came from, and
// the values text.
type installSource struct {
	repository, chart, version string
	verified                   bool
	origin                     string      // kinds.HelmOriginHTTP, kinds.HelmOriginInsecureTLS or "" (H-41)
	archive                    *helm.Chart // nil until fetched (inline installs)
	text                       string
}

func registerHelmInstall(g *registrar) {
	add(g, engine.Spec[installTabParams]{
		ID: "helm.chart.installTab", Label: "Install", MenuPath: "Helm/Install", Kinds: []string{kinds.HelmChartsKey},
		Order: orderInstall, Danger: engine.DangerUI,
		Description: "Opens a Helm Install dock tab on a chart of a repository of Helm's repositories file, or activates the one already " +
			"open on it (at another version: loads that version): Cogpit downloads the chart's archive (checked against the " +
			"index's digest when it lists one) and shows its values.yaml in an editor, with the versions the repository " +
			"lists. Writes nothing to a cluster. Agents read the tab's values withheld (editor_get) but may replace them " +
			"(editor_setText); helm_chart_preview and helm_chart_install with the tab id preview and install it." + privateReads,
		Enabled: func(s *engine.Session, p installTabParams) error {
			if _, _, err := chartOf(s, p.Ref); err != nil {
				return err
			}
			if _, err := helmRepos(s); err != nil {
				return err
			}
			b, err := writableOn(s, p.Ref.Context) // the cluster the tab installs into (D-69)
			if err != nil {
				return err
			}
			_, err = b.HelmInstaller()
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p installTabParams) (any, error) {
			repo, chart, err := chartOf(s, p.Ref)
			if err != nil {
				return nil, err
			}
			if p.Version == "" {
				p.Version = s.DrawerChartVersionOf(p.Ref) // Install from the drawer installs the version it shows
			}
			st, created, err := s.OpenHelmInstallTab(ctx, p.Ref.Context, repo, chart, p.Version)
			if err != nil {
				return nil, err
			}
			if engine.ReadsAsAgent(ctx) { // a private turn reads the values as the window shows them (H-94)
				st.Text = model.Withheld
			}
			return EditResult{Tab: st.Tab, Created: created, Text: st.Text, Revision: st.Revision}, nil
		},
	})

	add(g, engine.Spec[installParams]{
		ID: "helm.chart.preview", Label: "Preview Helm Install", Danger: engine.DangerRead,
		Verbs: []engine.Verb{{Verb: nwVerbGet, Resource: "*"}},
		Description: "Renders a chart install without changing anything and returns the objects, hooks and custom resource " +
			"definitions it would create, by kind, namespace and name, never their content. For agents and the CLI it " +
			"renders without the cluster (helm template: no lookup, default capabilities, offline in the result); in " +
			"Cogpit's window against the cluster (--dry-run=server), which also finds objects that exist already. Takes " +
			"the same parameters as helm_chart_install." + privateReads,
		Enabled: func(s *engine.Session, p installParams) error { return installEnabled(s, p) },
		Run: func(ctx context.Context, s *engine.Session, p installParams) (any, error) {
			b, err := writableOn(s, installContext(s, p))
			if err != nil {
				return nil, err
			}
			installer, err := b.HelmInstaller()
			if err != nil {
				return nil, err
			}
			src, err := installSourceOf(ctx, s, p, false)
			if err != nil {
				return nil, err
			}
			vals, err := helm.ParseValues(src.text)
			if err != nil {
				return nil, fmt.Errorf("release %s: %w; nothing was rendered", p.Name, err)
			}
			// Only the user's own window renders against the cluster: an agent's or the CLI's preview
			// is offline, so that a chart's lookup cannot hand them what the cluster holds. The
			// built-in agent over a private connection reads like the user (H-94): its preview
			// renders as the window's, and its error is kept out of the audit log (userOnly).
			offline := !engine.ReadsLikeUser(ctx)
			pre, err := installer.PreviewInstall(ctx, helm.InstallRequest{Namespace: p.Namespace, Name: p.Name, Chart: src.archive, Values: vals,
				CreateNamespace: p.CreateNamespace, Offline: offline, Driver: s.HelmDriver()})
			if err != nil {
				err = helm.Scrubbed(err, src.archive.UserValues(vals))
				if engine.PrivateTurn(ctx) {
					return nil, userOnly(err, fmt.Sprintf("the preview of release %s failed", p.Name))
				}
				return nil, err
			}
			return InstallPreview{Release: engine.HelmReleaseRef(b.Context, p.Namespace, p.Name), Repository: src.repository, Chart: src.chart,
				Version: src.version, Digest: src.archive.Digest, Verified: src.archive.Verified, Origin: src.origin,
				Warning: kinds.HelmOriginWarning(src.repository, src.origin), Offline: offline, InstallPreview: pre}, nil
		},
	})

	add(g, engine.Spec[installParams]{
		ID: "helm.chart.install", Label: "Install", Danger: engine.DangerMutating, Verbs: releaseSecretVerbs("list", "get", "create", "update"),
		Description: "Installs a chart from a repository of Helm's repositories file as a new release, as helm install does with its " +
			"defaults (hooks waited for, 5 minutes; no --create-namespace unless createNamespace): the chart and version of a " +
			"Helm Install tab with its values (tab), or a chart, version and values given here. The archive is checked " +
			"against its index's digest when the index lists one. Refused when a release of that name exists in the " +
			"namespace. The release's records go to Secrets, or to ConfigMaps when the HELM_DRIVER preference says configmap " +
			"(preferences_get). Besides the release's records it needs whatever verbs creating the chart's objects needs (server-side " +
			"apply: patch, and create), create on namespaces with createNamespace and on customresourcedefinitions for a " +
			"chart's crds/ folder; helm_chart_preview names the objects first. An agent's request always waits for the user's " +
			"acceptance, which shows values given here, also where the user allowed agent writes (a chart's templates can " +
			"create any object). An install a crash of Cogpit interrupts, or that fails, stays in " +
			"helm_pendingOperations with its release's state; Cogpit offers nothing to finish it: the user decides " +
			"in the Helm Releases view.",
		// An agent's install always waits for the user, whatever the chart holds (A2, owner 2026-10-14
		// evening).
		AgentsAsk: func(*engine.Session, installParams) bool { return true },
		Enabled: func(s *engine.Session, p installParams) error {
			if err := installEnabled(s, p); err != nil {
				return err
			}
			if s.HelmOps() == nil {
				return engine.ErrNoHelmJournal
			}
			if b := s.BackendOf(installContext(s, p)); b != nil && s.HelmOps().Running(b.Context, p.Namespace, p.Name) {
				return fmt.Errorf("a Helm action on release %s is running", p.Name)
			}
			return nil
		},
		Targets: func(s *engine.Session, p installParams) ([]model.ObjectRef, error) {
			b, err := connectedOn(s, installContext(s, p))
			if err != nil {
				return nil, err
			}
			return []model.ObjectRef{engine.HelmReleaseRef(b.Context, p.Namespace, p.Name)}, nil
		},
		Effects: installEffects,
		Warning: installWarning, // a chart nothing authenticates (H-41, A14)
		// The values an agent passes inline are shown to the user before they answer (D-27); from
		// a tab, the tab is what the user reviews.
		Review: func(_ *engine.Session, p installParams) string {
			switch {
			case p.Tab != "" || p.Values == nil:
				return ""
			case strings.TrimSpace(*p.Values) == "":
				return "# no values: the chart's defaults"
			}
			return *p.Values
		},
		Run: runInstall,
	})
}

// chartOf resolves a chart reference (kind helmcharts) to its repository and chart name; the chart
// must be listed.
func chartOf(s *engine.Session, ref model.ObjectRef) (repository, chart string, err error) {
	if ref.Kind != kinds.HelmChartsKey {
		return "", "", fmt.Errorf("%w: %s is not a Helm chart", engine.ErrNotApplicable, ref.String())
	}
	repository, chart, ok := kinds.SplitHelmChartName(ref.Name)
	if !ok {
		return "", "", fmt.Errorf("%q names no chart: a chart's id is <repository>.<chart>, every dot of the repository's name doubled", ref.Name)
	}
	if _, err := s.Lookup(ref); err != nil {
		return "", "", err
	}
	return repository, chart, nil
}

// installEnabled checks what the preview and the install share: a cluster that can be written to,
// the repositories, the names, and the tab or the chart.
func installEnabled(s *engine.Session, p installParams) error {
	if _, err := helmRepos(s); err != nil {
		return err
	}
	b, err := writableOn(s, installContext(s, p))
	if err != nil {
		return err
	}
	if _, err := b.HelmInstaller(); err != nil {
		return err
	}
	if err := helm.CheckReleaseName(p.Name); err != nil {
		return err
	}
	if err := helm.CheckNamespace(p.Namespace); err != nil {
		return err
	}
	switch {
	case p.Tab != "" && (p.Values != nil || p.Repository != "" || p.Chart != "" || p.Version != ""):
		return errors.New("give tab, or repository, chart, version and values, not both")
	case p.Tab != "":
		t, err := s.HelmInstallTab(p.Tab)
		if err != nil {
			return err
		}
		if t.Tab.Context != b.Context {
			return fmt.Errorf("%w: the tab %s belongs to context %s", engine.ErrWrongCluster, p.Tab, t.Tab.Context)
		}
	case p.Repository == "" || p.Chart == "" || p.Version == "":
		return errors.New("give a Helm Install tab (tab), or the chart's repository, chart and version")
	case p.Values == nil:
		return errors.New("give the values (empty: the chart's defaults alone)")
	}
	return nil
}

// installSourceOf resolves what an install installs: a tab's chart, version and values (with
// bound, the text revision the request names must be the tab's), or a chart version downloaded
// from its repository now, checked against the index's digest.
// installContext is the cluster an install goes to: its context parameter, else its Helm Install
// tab's cluster, else the shown one (D-69).
func installContext(s *engine.Session, p installParams) string {
	if p.Context == "" && p.Tab != "" {
		return tabContext(s, p.Tab)
	}
	return p.Context
}

// TargetDockTab makes an install from a tab the tab's cluster's for Dispatch (target.go).
func (p installParams) TargetDockTab() string { return p.Tab }

func installSourceOf(ctx context.Context, s *engine.Session, p installParams, bound bool) (installSource, error) {
	if p.Tab != "" {
		t, err := s.HelmInstallTab(p.Tab)
		if err != nil {
			return installSource{}, err
		}
		if bound {
			if p.TextRevision == 0 {
				return installSource{}, fmt.Errorf("give textRevision, the revision of the tab %s's text the install takes (editor_get shows it); nothing was done", p.Tab)
			}
			if t.TextRevision != p.TextRevision {
				return installSource{}, fmt.Errorf("%w: the values in the tab %s changed since text revision %d (now %d); nothing was done: review them and install again",
					engine.ErrTargetsChanged, p.Tab, p.TextRevision, t.TextRevision)
			}
		}
		hi := t.Tab.HelmInstall
		return installSource{repository: hi.Repository, chart: hi.Chart, version: hi.Version, verified: hi.Verified, origin: hi.Origin,
			archive: t.Chart, text: t.Values}, nil
	}
	repos, err := helmRepos(s)
	if err != nil {
		return installSource{}, err
	}
	ch, _, err := repos.FetchChart(ctx, p.Repository, p.Chart, p.Version)
	if err != nil {
		return installSource{}, err
	}
	return installSource{repository: p.Repository, chart: p.Chart, version: p.Version, verified: ch.Verified, origin: ch.Origin,
		archive: ch, text: *p.Values}, nil
}

// installWarning is the confirmation's warning (H-41, A14): what it means that nothing
// authenticates the chart's repository, for a tab the origin its archive came from, else its
// repository's as Helm's file says it now; "" for https with the certificate checked.
func installWarning(s *engine.Session, p installParams) string {
	if p.Tab != "" {
		if t, err := s.HelmInstallTab(p.Tab); err == nil && t.Tab.HelmInstall != nil {
			return kinds.HelmOriginWarning(t.Tab.HelmInstall.Repository, t.Tab.HelmInstall.Origin)
		}
		return ""
	}
	return kinds.HelmOriginWarning(p.Repository, s.HelmRepos().Origin(p.Repository))
}

// installEffects are the confirmation's rows: the chart, its version and how its archive was
// checked, a namespace it creates, where the values come from, whose identity creates the objects
// and, with Helm's ConfigMaps driver, who can read the release's records. The objects name the
// release, its namespace and the context.
func installEffects(s *engine.Session, p installParams) []engine.Effect {
	repo, chart, version := p.Repository, p.Chart, p.Version
	var archive *engine.Effect
	unchecked := engine.Effect{Name: effectArchive, Value: "not checked: the index lists no digest", Tone: engine.EffectWarning}
	if p.Tab != "" {
		if t, err := s.HelmInstallTab(p.Tab); err == nil && t.Tab.HelmInstall != nil {
			hi := t.Tab.HelmInstall
			repo, chart, version = hi.Repository, hi.Chart, hi.Version
			archive = &unchecked
			if hi.Verified {
				archive = &engine.Effect{Name: effectArchive, Value: "matched the index's digest"}
			}
		}
	} else if repos := s.HelmRepos(); repos != nil {
		if _, _, v, err := repos.ChartVersion(p.Repository, p.Chart, p.Version); err == nil {
			archive = &unchecked
			if v.Digest != "" {
				archive = &engine.Effect{Name: effectArchive, Value: "checked against the index's digest"}
			}
		}
	}
	out := []engine.Effect{{Name: "Chart", Value: fmt.Sprintf("%s/%s %s", repo, chart, version), Tone: engine.EffectNames}}
	if archive != nil {
		out = append(out, *archive)
	}
	if p.CreateNamespace {
		out = append(out, engine.Effect{Name: "Creates", Value: "namespace " + p.Namespace + ", if it does not exist", Tone: engine.EffectNames})
	}
	values := engine.Effect{Name: effectValues, Value: "below"}
	if p.Tab != "" {
		values.Value = fmt.Sprintf("tab %s at text revision %d (review them there; a change refuses the install)", p.Tab, p.TextRevision)
	}
	out = append(out, values, engine.Effect{Name: "Created as", Value: "you, with your identity (helm_chart_preview lists what it creates)"})
	if s.HelmDriver() == helm.DriverConfigMap {
		out = append(out, engine.Effect{Name: "Records", Value: "in ConfigMaps (HELM_DRIVER), values included: readable by whoever may read " +
			"ConfigMaps in the namespace", Tone: engine.EffectWarning})
	}
	return out
}

func runInstall(ctx context.Context, s *engine.Session, p installParams) (any, error) {
	b, err := writableOn(s, installContext(s, p))
	if err != nil {
		return nil, err
	}
	installer, err := b.HelmInstaller()
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	ref := engine.HelmReleaseRef(b.Context, p.Namespace, p.Name)
	if _, err := s.Lookup(ref); err == nil {
		return nil, fmt.Errorf("a release named %s exists in namespace %s; nothing was done", p.Name, p.Namespace)
	}
	src, err := installSourceOf(ctx, s, p, true)
	if err != nil {
		return nil, err
	}
	// What the user accepted is what installs: a chart that came another way than the
	// confirmation said (its repository's entry in Helm's file changed while it was open) is
	// refused (H-41, A14).
	if req, ok := engine.Confirmed(ctx); ok && req.Detail != "" && req.Warning != kinds.HelmOriginWarning(src.repository, src.origin) {
		return nil, fmt.Errorf("%w: how chart %s/%s reaches Cogpit changed in Helm's repositories file since the install was confirmed; "+
			"nothing was done: review it and install again", engine.ErrTargetsChanged, src.repository, src.chart)
	}
	vals, err := helm.ParseValues(src.text)
	if err != nil {
		return nil, fmt.Errorf("release %s: %w; nothing was done", p.Name, err)
	}
	scrub := src.archive.UserValues(vals)
	op := engine.PendingHelmOp{
		Context: b.Context, Namespace: p.Namespace, Release: p.Name, Action: engine.HelmInstall,
		Chart: src.repository + "/" + src.chart, ChartVersion: src.version, Started: s.Now().UTC(), StartedBy: startedBy(ctx),
	}
	var revision int
	err = runHelmOp(ctx, s, op, fmt.Sprintf("Installing release %s from %s/%s %s…", p.Name, src.repository, src.chart, src.version), src.text, func(ctx context.Context) (err error) {
		defer func() {
			if r := recover(); r != nil { // scrubbed here: helmSafely would quote the panic as it is
				err = helm.Scrubbed(fmt.Errorf("internal error: %v", r), scrub)
			}
		}()
		revision, err = installer.InstallChart(ctx, helm.InstallRequest{Namespace: p.Namespace, Name: p.Name, Chart: src.archive, Values: vals,
			CreateNamespace: p.CreateNamespace, Driver: s.HelmDriver()}) // the HELM_DRIVER preference (helmdriver.go)
		return helm.Scrubbed(err, scrub)
	})
	if err != nil {
		return nil, forCaller(ctx, "install", p.Name, err)
	}
	if revision < 1 {
		revision = 1
	}
	userToast(s, engine.LevelInfo, fmt.Sprintf("Release %s installed in namespace %s from %s/%s %s", p.Name, p.Namespace, src.repository, src.chart, src.version))
	return HelmResult{Ref: ref, Action: "installed", Revision: revision}, nil
}
