package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Helm upgrade (G11, architecture §8): Lens's Upgrade opens a "Helm Upgrade: <name>" dock tab
// with the release's values (captured 2026-10-12); helm.release.upgrade upgrades the release with
// the tab's values, or with values a caller passes (the drawer's Save), through the Helm SDK in
// Cogpit's process, mutating (privilege-footprint.md), recorded in the Helm journal first (D-11).
// The chart is the release's own, or a version of it a repository of Helm's repositories file lists, which is
// downloaded (bounded, checked against the index's digest) before anything is recorded or changed.
// The values are the user's: agents read them withheld, and no log, error, toast, audit line or
// journal entry carries them.

// orderUpgrade places Upgrade between Rollback (200) and Delete in the drawer's toolbar and the
// row menu, as Lens's drawer orders its icons (captured 2026-10-12).
const orderUpgrade = 300

type upgradeTabParams struct {
	Ref        model.ObjectRef `json:"ref" jsonschema:"the release: kind helmreleases, its namespace and name"`
	Repository string          `json:"repository,omitempty" jsonschema:"a chart repository of Helm's repositories file that lists the release's chart, whose versions the tab offers (the tab's repositories list them); empty: the tab's, or the default"`
	Version    string          `json:"version,omitempty" jsonschema:"the chart version the upgrade installs, one of the tab's versions: the release's own (its stored chart) or one of the repository's; empty: the tab's, or for a new tab the repository's newest when it is newer than the release's"`
}

type releaseParams struct {
	Ref model.ObjectRef `json:"ref" jsonschema:"the release: kind helmreleases, its namespace and name"`
}

type upgradeParams struct {
	Ref model.ObjectRef `json:"ref" jsonschema:"the release: kind helmreleases, its namespace and name"`
	Tab string          `json:"tab,omitempty" jsonschema:"a Helm Upgrade dock tab id as helm_release_upgradeTab returned it, e.g. helmUpgrade-1: upgrade with the tab's values (editor_setText changes them) and, unless version or repository is given, the tab's chart version, based on the revision the tab read the values from; give tab or values"`
	// Values is a pointer so that empty values (no user-supplied values) differ from none given.
	Values   *string `json:"values,omitempty" jsonschema:"the release's new user-supplied values as YAML, replacing the old ones whole (empty: none); give tab or values"`
	Revision int     `json:"revision,omitempty" jsonschema:"with values: the release's newest revision the values are based on (object_get or helm_release_history shows it); the upgrade is refused when the release moved past it"`
	// TextRevision binds an upgrade from a tab to the text the user reviewed: a change of the
	// tab's text after it (an agent's editor.setText while the confirmation is open) refuses it.
	TextRevision uint64 `json:"textRevision,omitempty" jsonschema:"with tab: the tab's text revision the upgrade takes the values at (editor_get and editor_setText return it); the upgrade is refused when the text changed since"`
	Version      string `json:"version,omitempty" jsonschema:"the chart version to install: empty or the release's own upgrades with the release's stored chart (unless repository is given); another one is downloaded from the repository that lists it (helmcharts objects list the versions)"`
	Repository   string `json:"repository,omitempty" jsonschema:"the chart repository of Helm's repositories file to download the chart from (helm_repo_list); empty: the tab's, else the one repository that lists the version; with an empty version its newest version that is not a prerelease"`
}

type releaseValuesParams struct {
	Ref model.ObjectRef `json:"ref" jsonschema:"the release: kind helmreleases, its namespace and name"`
	All bool            `json:"all,omitempty" jsonschema:"every value, the chart's defaults included (Lens's User-supplied values only unchecked); default: the user-supplied values only"`
}

// ReleaseHistory is what helm.release.history returns: the release's newest revision and its
// history from the cache, newest first.
type ReleaseHistory struct {
	Ref      model.ObjectRef `json:"ref"`
	Revision int             `json:"revision"`
	Status   string          `json:"status"`
	// UID is the newest revision's release Secret's UID, which a rollback's confirmation binds.
	UID     string              `json:"uid"`
	History []helm.HistoryEntry `json:"history"`
}

// releaseCacheReads are the reads of the commands that read releases from the cache: the watches
// on the release Secrets and the release ConfigMaps (the access check keeps the release's own
// driver's, engine.Session.releaseVerbs).
var releaseCacheReads = []engine.Verb{
	{Verb: nwVerbList, Resource: kindSecrets}, {Verb: nwVerbWatch, Resource: kindSecrets},
	{Verb: nwVerbList, Resource: kindConfigMaps}, {Verb: nwVerbWatch, Resource: kindConfigMaps},
}

func registerHelmUpgrade(g *registrar) {
	add(g, engine.Spec[upgradeTabParams]{
		ID: helm.UpgradeTabCommand, Label: "Upgrade", MenuPath: "Helm/Upgrade", Kinds: []string{helm.ReleaseKind},
		Order: orderUpgrade, Danger: engine.DangerUI, Verbs: releaseCacheReads,
		Description: "Opens a release's Helm Upgrade dock tab, or activates the one already open on it: the release's chart, " +
			"the version the upgrade installs and the versions to choose from, and its user-supplied values in an editor, read " +
			"from Cogpit's cache. When a chart repository of Helm's repositories file lists the release's chart (by name), the versions are " +
			"that repository's (repositories: every one that lists it) and a new tab starts with the newest that is not a " +
			"prerelease when it is newer than the release's; otherwise the release's own version only. repository and version " +
			"change the tab's choice. Writes nothing. Agents read the tab's values withheld (editor_get) but may replace them " +
			"(editor_setText); helm_release_upgrade with the tab id upgrades the release." + privateReads,
		Enabled: func(s *engine.Session, p upgradeTabParams) error {
			if _, err := cachedOfKind(s, p.Ref, helm.ReleaseKind); err != nil {
				return err
			}
			b, err := writableOn(s, p.Ref.Context) // the release's cluster, shown or not (D-69)
			if err != nil {
				return err
			}
			_, err = b.HelmWriter()
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p upgradeTabParams) (any, error) {
			st, created, err := s.OpenHelmUpgradeTab(ctx, p.Ref, engine.HelmUpgradePick{Repository: p.Repository, Version: p.Version})
			if err != nil {
				return nil, err
			}
			if engine.ReadsAsAgent(ctx) { // a private turn reads the values as the window shows them (H-94)
				st.Text = model.Withheld
			}
			return EditResult{Tab: st.Tab, Created: created, Text: st.Text, Revision: st.Revision}, nil
		},
	})

	add(g, engine.Spec[upgradeParams]{
		ID: "helm.release.upgrade", Label: "Upgrade", Kinds: []string{helm.ReleaseKind},
		Danger: engine.DangerMutating, Verbs: releaseSecretVerbs("list", "get", "create", "update", "delete"),
		Description: "Upgrades a Helm release as helm upgrade does with its defaults (hooks waited for, ten revisions kept), " +
			"with new user-supplied values, which replace the old ones whole: the values of a Helm Upgrade tab (tab), or values " +
			"given here with the revision they are based on. The chart is the release's own, or another version of it that a " +
			"chart repository of Helm's repositories file lists (version, repository; from a tab, the tab's choice), downloaded first and " +
			"checked against the index's digest when it lists one. Refused when the release moved to another revision since " +
			"the values were read, and while another Helm action on it is pending. Besides the release's records (its Secrets, or its ConfigMaps when Helm keeps it there) it needs " +
			"whatever verbs the release's objects need, and create and delete for objects another chart version adds or drops. " +
			"An agent's request always waits for the user's acceptance, which names the chart version from and to, also where " +
			"the user allowed agent writes (a chart's templates can create any object). An upgrade a crash " +
			"of Cogpit interrupts stays in helm_pendingOperations; helm_release_rollback to its fromRevision finishes it.",
		// What a chart's templates write is not something the allowance of agent writes was given
		// for: an agent's upgrade always waits for the user (A2, owner 2026-10-14 evening).
		AgentsAsk: func(*engine.Session, upgradeParams) bool { return true },
		Enabled: func(s *engine.Session, p upgradeParams) error {
			obj, err := cachedOfKind(s, p.Ref, helm.ReleaseKind)
			if err != nil {
				return err
			}
			if st := helm.ReleaseStatus(obj); helm.Pending(st) {
				return fmt.Errorf("release %s is %s: another Helm action on it did not finish; finish or roll it back first", p.Ref.Name, st)
			}
			moved := false
			switch {
			case p.Tab != "" && p.Values != nil:
				return errors.New("give tab or values, not both")
			case p.Tab != "":
				tab, err := upgradeTab(s, p)
				if err != nil {
					return err
				}
				moved = tab.CheckTabBase(obj) != nil
			case p.Values == nil:
				return errors.New("give the values: a Helm Upgrade tab (tab) or values with the revision they are based on")
			case p.Revision < 1:
				return errors.New("give the revision the values are based on (the release's newest revision when you read them)")
			default:
				moved = engine.CheckReleaseBase(obj, p.Revision, "") != nil
			}
			if !moved { // a release that moved is refused when the command runs, as ErrTargetsChanged
				if _, err := resolveUpgradeTarget(s, p, obj); err != nil {
					return err
				}
			}
			return helmWritable(s, p.Ref)
		},
		Targets: func(s *engine.Session, p upgradeParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Effects: upgradeEffects,
		Warning: upgradeWarning, // a chart from a repository nothing authenticates (H-41, A14)
		// The values an agent passes inline are shown to the user before they answer (owner,
		// 2026-10-12: R-2's remainder); from a tab, the tab is what the user reviews.
		Review: func(_ *engine.Session, p upgradeParams) string {
			switch {
			case p.Tab != "" || p.Values == nil:
				return ""
			case strings.TrimSpace(*p.Values) == "":
				return "# no user-supplied values"
			}
			return *p.Values
		},
		Run: runUpgrade,
	})

	add(g, engine.Spec[releaseValuesParams]{
		ID: "helm.release.values", Label: "Release Values", Danger: engine.DangerRead, Verbs: releaseCacheReads,
		Description: "Returns a release's values from Cogpit's cache, the newest revision's: the user-supplied ones, or all " +
			"of them with the chart's defaults (all), with the revision and chart they are of. Values can hold credentials: " +
			"agents read them withheld; the user sees them in Cogpit's own window." + privateReads,
		Enabled: func(s *engine.Session, p releaseValuesParams) error {
			_, err := cachedOfKind(s, p.Ref, helm.ReleaseKind)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p releaseValuesParams) (any, error) {
			v, err := s.ReleaseValues(ctx, p.Ref, p.All)
			if err != nil {
				return nil, err
			}
			if engine.ReadsAsAgent(ctx) {
				v.YAML = model.Withheld
			}
			return v, nil
		},
	})

	add(g, engine.Spec[releaseParams]{
		ID: "helm.release.history", Label: "Release History", Danger: engine.DangerRead, Verbs: releaseCacheReads,
		Description: "Returns a release's revisions from Cogpit's cache, newest first (as many as Helm keeps, ten by default): " +
			"each revision's chart and version, app version, status, when it was deployed and its description, with the " +
			"release's newest revision and status. Rollback picks one of them.",
		Enabled: func(s *engine.Session, p releaseParams) error {
			_, err := cachedOfKind(s, p.Ref, helm.ReleaseKind)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p releaseParams) (any, error) {
			ref, _, err := s.ResolveRef(p.Ref)
			if err != nil {
				return nil, err
			}
			obj, err := s.Object(ctx, ref)
			if err != nil {
				return nil, err
			}
			h := helm.ReleaseHistory(obj)
			if h == nil {
				h = []helm.HistoryEntry{}
			}
			return ReleaseHistory{Ref: ref, Revision: helm.ReleaseRevision(obj), Status: helm.ReleaseStatus(obj), UID: string(obj.GetUID()), History: h}, nil
		},
	})
}

// upgradeTab returns the Helm Upgrade tab an upgrade names, which must be one on its release.
func upgradeTab(s *engine.Session, p upgradeParams) (engine.HelmUpgradeTabInfo, error) {
	t, err := s.HelmUpgradeTab(p.Tab)
	if err != nil {
		return t, err
	}
	if !t.Tab.Ref.Same(p.Ref) {
		return t, fmt.Errorf("the tab %s upgrades release %s/%s, not %s/%s", p.Tab, t.Tab.Ref.Namespace, t.Tab.Ref.Name, p.Ref.Namespace, p.Ref.Name)
	}
	return t, nil
}

// upgradeTarget is the chart an upgrade installs: the release's chart from version from to version
// to, from repository (downloaded) or, with repository "", the release's own stored chart.
type upgradeTarget struct {
	chart, from, to string
	repository      string
	// origin is the repository's when nothing authenticates it (H-41): kinds.HelmOriginHTTP,
	// kinds.HelmOriginInsecureTLS, or "".
	origin string
	// digest: the repository's index lists the version's digest, which the download is checked
	// against.
	digest bool
}

// own reports whether the upgrade keeps the release's stored chart.
func (t upgradeTarget) own() bool { return t.repository == "" }

// journalChart is the chart as the journal records it: an install's form, "<repository>/<chart>",
// for a downloaded one.
func (t upgradeTarget) journalChart() string {
	if t.own() {
		return t.chart
	}
	return t.repository + "/" + t.chart
}

// resolveUpgradeTarget resolves the chart an upgrade installs, from the cache and the indexes
// Cogpit holds (no download): version and repository as given; from a tab without either, the
// tab's choice; a version other than the release's own from the repository given, else the tab's,
// else the one repository that lists it. A version that no repository lists is refused with
// helm.ErrNoRepository.
func resolveUpgradeTarget(s *engine.Session, p upgradeParams, obj *unstructured.Unstructured) (upgradeTarget, error) {
	chart, own := helm.ReleaseChart(obj)
	t := upgradeTarget{chart: chart, from: own, to: p.Version, repository: p.Repository}
	hint := ""
	if p.Tab != "" {
		if tab, err := s.HelmUpgradeTab(p.Tab); err == nil && tab.Tab.Helm != nil {
			if t.to == "" && t.repository == "" {
				t.to = tab.Tab.Helm.Version
			}
			hint = tab.Tab.Helm.Repository
		}
	}
	if t.repository == "" && (t.to == "" || t.to == own) {
		t.to = own
		return t, nil
	}
	repos := s.HelmRepos()
	if repos == nil {
		return t, fmt.Errorf("chart %s %s: %w (the release's own is %s)", chart, t.to, helm.ErrNoRepository, own)
	}
	if t.repository == "" {
		names, err := repos.ChartRepositories(chart, t.to)
		if err != nil {
			return t, err
		}
		switch {
		case hint != "" && slices.Contains(names, hint):
			t.repository = hint
		case len(names) == 1:
			t.repository = names[0]
		case len(names) == 0:
			return t, fmt.Errorf("chart %s %s: %w (the release's own is %s)", chart, t.to, helm.ErrNoRepository, own)
		default:
			return t, fmt.Errorf("chart %s %s is listed by the repositories %s: give repository", chart, t.to, strings.Join(names, ", "))
		}
	}
	v, err := repos.IndexedVersion(t.repository, chart, t.to)
	if err != nil {
		return t, err
	}
	t.to, t.digest, t.origin = v.Version, v.Digest != "", repos.Origin(t.repository)
	return t, nil
}

// upgradeWarning is the confirmation's warning (H-41, A14): what it means that nothing
// authenticates the repository the upgrade downloads its chart from; "" for the release's own
// chart or a repository over https with the certificate checked. runUpgrade checks that it is
// still true when it runs.
func upgradeWarning(s *engine.Session, p upgradeParams) string {
	obj, err := s.Lookup(p.Ref)
	if err != nil {
		return ""
	}
	t, err := resolveUpgradeTarget(s, p, obj)
	if err != nil || t.own() {
		return ""
	}
	return kinds.HelmOriginWarning(t.repository, t.origin)
}

// upgradeEffects are the confirmation's rows: the chart version from and to, where the chart comes
// from and how its archive is checked, and where the values come from. The objects name the
// release, its namespace and the context. runUpgrade checks that the rows still hold when it runs
// (their text, the confirmation's Detail).
func upgradeEffects(s *engine.Session, p upgradeParams) []engine.Effect {
	values := engine.Effect{Name: effectValues, Value: "below, replacing the release's user-supplied values whole"}
	if p.Tab != "" {
		values.Value = fmt.Sprintf("tab %s at text revision %d (review them there; a change refuses the upgrade)", p.Tab, p.TextRevision)
	}
	obj, err := s.Lookup(p.Ref)
	if err != nil {
		return []engine.Effect{values}
	}
	t, err := resolveUpgradeTarget(s, p, obj)
	if err != nil {
		return []engine.Effect{values}
	}
	chart := engine.Effect{Name: "Chart", Value: fmt.Sprintf("%s %s → %s", t.chart, t.from, t.to), Tone: engine.EffectNames}
	if t.own() {
		return []engine.Effect{chart, {Name: "Source", Value: "the release's own chart, nothing downloaded"}, values}
	}
	if c, ok := helm.CompareVersions(t.to, t.from); ok && c < 0 {
		chart.Value += ", an older version"
	}
	archive := engine.Effect{Name: effectArchive, Value: "checked against the index's digest"}
	if !t.digest {
		archive = engine.Effect{Name: effectArchive, Value: "not checked: the index lists no digest", Tone: engine.EffectWarning}
	}
	return []engine.Effect{chart, {Name: "Repository", Value: t.repository, Tone: engine.EffectNames}, archive, values}
}

// rebaseWait bounds how long a successful upgrade from a tab waits for the cache to show the new
// revision, whose release Secret's UID becomes the tab's base.
const rebaseWait = 3 * time.Second

func runUpgrade(ctx context.Context, s *engine.Session, p upgradeParams) (any, error) {
	w, ref, obj, err := releaseTarget(ctx, s, p.Ref)
	if err != nil {
		return nil, err
	}
	text := ""
	if p.Tab != "" {
		tab, err := upgradeTab(s, p)
		if err != nil {
			return nil, err
		}
		if p.TextRevision == 0 {
			return nil, fmt.Errorf("give textRevision, the revision of the tab %s's text the upgrade takes (editor_get shows it); nothing was done", p.Tab)
		}
		if tab.TextRevision != p.TextRevision {
			return nil, fmt.Errorf("%w: the values in the tab %s changed since text revision %d (now %d); nothing was done: review them and upgrade again",
				engine.ErrTargetsChanged, p.Tab, p.TextRevision, tab.TextRevision)
		}
		if err := tab.CheckTabBase(obj); err != nil {
			return nil, err
		}
		text = tab.Values
	} else {
		if p.Values == nil {
			return nil, errors.New("give tab or values")
		}
		if err := engine.CheckReleaseBase(obj, p.Revision, ""); err != nil {
			return nil, err
		}
		text = *p.Values
	}
	if st := helm.ReleaseStatus(obj); helm.Pending(st) {
		return nil, fmt.Errorf("release %s is %s: another Helm action on it did not finish; nothing was done", ref.Name, st)
	}
	target, err := resolveUpgradeTarget(s, p, obj)
	if err != nil {
		return nil, fmt.Errorf("release %s: %w; nothing was done", ref.Name, err)
	}
	// What the user accepted is what runs: a confirmation whose rows (the chart version from
	// and to, the repository, the digest check) or warning (the repository's origin, H-41) would
	// read differently now, because the tab's choice, an index or Helm's file changed while it
	// was open, is refused.
	if req, ok := engine.Confirmed(ctx); ok && req.Detail != "" && (req.Detail != engine.EffectsText(upgradeEffects(s, p)) || req.Warning != upgradeWarning(s, p)) {
		return nil, fmt.Errorf("%w: the chart the upgrade installs changed since it was confirmed; nothing was done: review it and upgrade again", engine.ErrTargetsChanged)
	}
	// The window's tab marks its repository's origin: one that changed in Helm's file since the tab
	// showed it is marked anew, and an upgrade that downloads from it waits for the user to see it.
	if p.Tab != "" && s.RefreshHelmUpgradeOrigin(p.Tab) && !target.own() {
		return nil, fmt.Errorf("%w: how repository %s reaches Cogpit changed in Helm's repositories file since the tab showed it; "+
			"nothing was done: review the tab and upgrade again", engine.ErrTargetsChanged, target.repository)
	}
	vals, err := helm.ParseValues(text)
	if err != nil {
		return nil, fmt.Errorf("release %s: %w; nothing was done", ref.Name, err)
	}
	var downloaded *helm.Chart
	if !target.own() {
		if downloaded, err = fetchUpgradeChart(ctx, s, target); err != nil {
			return nil, fmt.Errorf("release %s: %w; nothing was done", ref.Name, err)
		}
	}
	cur := helm.ReleaseRevision(obj)
	op := engine.PendingHelmOp{
		Context: ref.Context, Namespace: ref.Namespace, Release: ref.Name, Action: engine.HelmUpgrade,
		FromRevision: cur, Chart: target.journalChart(), ChartVersion: target.to, Started: s.Now().UTC(), StartedBy: startedBy(ctx),
	}
	starting := fmt.Sprintf("Upgrading release %s…", ref.Name)
	if !target.own() {
		starting = fmt.Sprintf("Upgrading release %s to %s %s from %s…", ref.Name, target.chart, target.to, target.repository)
	}
	var revision int
	err = runHelmOp(ctx, s, op, starting, text, func(ctx context.Context) (err error) {
		defer func() {
			if r := recover(); r != nil { // scrubbed here: helmSafely would quote the panic as it is
				err = helm.Scrubbed(fmt.Errorf("internal error: %v", r), vals)
			}
		}()
		revision, err = w.UpgradeRelease(ctx, ref.Namespace, ref.Name, engine.HelmUpgradeRequest{ChartVersion: target.to, Chart: downloaded, Values: vals,
			FromRevision: cur, Driver: helm.ReleaseStorage(obj)})
		return helm.Scrubbed(err, vals)
	})
	if err != nil {
		return nil, forCaller(ctx, "upgrade", ref.Name, movedErr(err))
	}
	if revision <= cur {
		revision = cur + 1
	}
	if p.Tab != "" {
		s.RebaseHelmUpgradeTab(p.Tab, revision, newRevisionUID(ctx, s, ref, revision), target.to)
	}
	done := fmt.Sprintf("Release %s upgraded to revision %d", ref.Name, revision)
	if !target.own() {
		done += fmt.Sprintf(", chart %s %s from %s", target.chart, target.to, target.repository)
	}
	userToast(s, engine.LevelInfo, done)
	return HelmResult{Ref: ref, Action: "upgraded", Revision: revision}, nil
}

// fetchUpgradeChart downloads the chart version an upgrade installs from its repository (bounded,
// checked against the index's digest when it lists one, the archive holding the chart and version
// the index names). An index that listed a digest when the target was resolved and lists none now
// is refused: the confirmation said the archive is checked.
func fetchUpgradeChart(ctx context.Context, s *engine.Session, t upgradeTarget) (*helm.Chart, error) {
	repos := s.HelmRepos()
	if repos == nil {
		return nil, engine.ErrNoHelmRepos
	}
	ch, _, err := repos.FetchChart(ctx, t.repository, t.chart, t.to)
	if err != nil {
		return nil, err
	}
	if t.digest && !ch.Verified {
		return nil, fmt.Errorf("%w: chart %s/%s %s: the index no longer lists the digest the archive was to be checked against", engine.ErrTargetsChanged, t.repository, t.chart, t.to)
	}
	if ch.Origin != t.origin { // the repository's entry changed between the check and the download (H-41)
		return nil, fmt.Errorf("%w: chart %s/%s %s came another way than the upgrade said", engine.ErrTargetsChanged, t.repository, t.chart, t.to)
	}
	return ch, nil
}

// newRevisionUID waits, bounded by rebaseWait, for the cache to show a release at a revision and
// returns its release Secret's UID, or "" when the cache has not seen it yet.
func newRevisionUID(ctx context.Context, s *engine.Session, ref model.ObjectRef, revision int) string {
	deadline := time.Now().Add(rebaseWait)
	for {
		if obj, err := s.Lookup(ref); err == nil && helm.ReleaseRevision(obj) == revision {
			return string(obj.GetUID())
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return ""
		}
		time.Sleep(20 * time.Millisecond)
	}
}
