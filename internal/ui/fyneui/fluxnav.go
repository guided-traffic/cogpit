package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The Flux CD group of the navigator (ui-parity › Cluster sidebar; G23): under the heading
// Integrations, before Argo CD (the owner, 2026-11-03), the Dashboard first, then Helm Releases and
// Kustomizations, and the sub-groups Image Automation, Notifications and Sources, each kind under
// its plural title. Its kinds are the Flux toolkit's CRD kinds with Cogpit's curated profiles
// (internal/model/kinds/flux*.go): only those the cluster serves appear, a sub-group without any
// is left out, and so is the whole group, the Dashboard included, on a cluster without Flux.
// The toolkit's API groups stay listed under Custom Resources too.

// Flux toolkit API groups, as kind keys end ("kustomizations" + fluxKustomize).
const (
	fluxSource       = ".source.toolkit.fluxcd.io"
	fluxKustomize    = ".kustomize.toolkit.fluxcd.io"
	fluxHelm         = ".helm.toolkit.fluxcd.io"
	fluxImage        = ".image.toolkit.fluxcd.io"
	fluxNotification = ".notification.toolkit.fluxcd.io"
)

// fluxGroup is the Flux CD group's node id.
const fluxGroup = groupPrefix + "flux"

// fluxNav is the Flux CD group of navLayout.
var fluxNav = navGroup("flux", "Flux CD",
	navEntry{id: engine.FluxDashboard, title: "Dashboard", page: true},
	titled("helmreleases"+fluxHelm, "Helm Releases"),
	titled("kustomizations"+fluxKustomize, "Kustomizations"),
	navGroup("flux/images", "Image Automation",
		titled("imagerepositories"+fluxImage, "Image Repositories"),
		titled("imagepolicies"+fluxImage, "Image Policies"),
		titled("imageupdateautomations"+fluxImage, "Image Update Automations")),
	navGroup("flux/notifications", "Notifications",
		titled("alerts"+fluxNotification, "Alerts"),
		titled("providers"+fluxNotification, "Providers"),
		titled("receivers"+fluxNotification, "Receivers")),
	navGroup("flux/sources", "Sources",
		titled("gitrepositories"+fluxSource, "Git Repositories"),
		titled("ocirepositories"+fluxSource, "OCI Repositories"),
		titled("helmrepositories"+fluxSource, "Helm Repositories"),
		titled("buckets"+fluxSource, "Buckets"),
		titled("externalartifacts"+fluxSource, "External Artifacts")))

// iconFlux is Cogpit's own icon for the Flux CD group, drawn for Cogpit (not Flux's logo): two
// arrows chasing each other around a circle, reconciliation.
var iconFlux = fyne.NewStaticResource("cogpit-flux.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
	`<path d="M12 3a9 9 0 0 1 8.5 6H23l-4 5-4-5h2.3A6.5 6.5 0 0 0 6.2 8.2L4.4 6.4A9 9 0 0 1 12 3Z"/>`+
	`<path d="M12 21a9 9 0 0 1-8.5-6H1l4-5 4 5H6.7a6.5 6.5 0 0 0 11.1 2.8l1.8 1.8A9 9 0 0 1 12 21Z"/></svg>`))

func init() {
	navIcons[fluxGroup] = func() fyne.Resource { return iconFlux }
	// The drawer's toolbar icons of the Flux actions (commandIcons), as Restart's, Suspend's and
	// Resume's of the built-in kinds.
	commandIcons["flux.reconcile"] = theme.IconNameViewRefresh
	// Cogpit's choices (D-33, owner 2026-10-14): the source is fetched first; a reset starts over.
	commandIcons["flux.reconcileWithSource"] = theme.IconNameDownload
	commandIcons["flux.reset"] = theme.IconNameMediaReplay
	commandIcons["flux.suspend"] = theme.IconNameMediaPause
	commandIcons["flux.resume"] = theme.IconNameMediaPlay
}
