package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// titleResourceClaims heads the pod drawer's claims section.
const titleResourceClaims = "Resource Claims"

// titleContainers heads the column of the containers that request a claim.
const titleContainers = "Containers"

// podResourceClaimsSection is the pod drawer's Resource Claims section, shown when the pod uses a
// claim (Lens's documentation, read 2026-10-13: each claim, whether the pod references it directly
// or through a template, its state, and the containers that request it). Built: each of the pod's
// claims with the claim it resolves to (named directly, or generated from a template and recorded
// in status.resourceClaimStatuses), a link to it and to the template, and the containers and init
// containers that request it ("app" or "app (gpu)" for one request of the claim); the claim's state
// (A105, podDetailWith), from claims only the open drawer reads. The section's
// place, after Pod Volumes, is inferred.
// With rel (not model.NoRelated) a State column shows each claim's state (A105).
func podResourceClaimsSection(pod any, namespace string, rel model.Related) (model.DetailSection, bool) {
	states := rel != nil && rel != model.NoRelated
	claims := model.NestedSlice(pod, "spec", "resourceClaims")
	if len(claims) == 0 {
		return model.DetailSection{}, false
	}
	generated := map[string]string{} // the pod's claim name → the generated claim
	for _, s := range model.NestedSlice(pod, "status", "resourceClaimStatuses") {
		generated[model.NestedString(s, fieldName)] = model.NestedString(s, "resourceClaimName")
	}
	cols := []model.Column{
		{Title: titleName, Type: model.ColumnText}, {Title: titleClaim, Type: model.ColumnText},
		{Title: "Template", Type: model.ColumnText}, {Title: titleContainers, Type: model.ColumnText},
	}
	if states {
		cols = append(cols, model.Column{Title: "State", Type: model.ColumnText})
	}
	rows := make([][]model.Cell, 0, len(claims))
	for _, c := range claims {
		name := model.NestedString(c, fieldName)
		if name == "" {
			continue
		}
		claim := model.NestedString(c, "resourceClaimName")
		template := model.NestedString(c, "resourceClaimTemplateName")
		claimCell := model.Cell{Text: claim, Link: linkTo(keyResourceClaims, namespace, claim)}
		if claim == "" && template != "" {
			if g := generated[name]; g != "" {
				claimCell = model.Cell{Text: g, Link: linkTo(keyResourceClaims, namespace, g)}
			} else {
				claimCell = model.Cell{Text: "(not generated yet)"}
			}
		}
		row := []model.Cell{
			{Text: name},
			claimCell,
			{Text: template, Link: linkTo(keyResourceClaimTemplates, namespace, template)},
			{Text: strings.Join(claimContainers(pod, name), "\n")},
		}
		if states {
			resolved := ""
			if claimCell.Link != nil {
				resolved = claimCell.Text
			}
			row = append(row, claimStateCell(namespace, resolved, rel))
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return model.DetailSection{}, false
	}
	return model.DetailSection{Title: titleResourceClaims, Table: model.CellTable(cols, rows)}, true
}

// claimContainers names the init containers and containers whose resources.claims use the pod's
// claim: "app", or "app (gpu)" when the container asks for one request of it.
func claimContainers(pod any, claim string) []string {
	var out []string
	for _, field := range []string{fieldInitContainers, fieldContainers} {
		for _, c := range model.NestedSlice(pod, "spec", field) {
			for _, rc := range model.NestedSlice(c, "resources", "claims") {
				if model.NestedString(rc, fieldName) != claim {
					continue
				}
				text := model.NestedString(c, fieldName)
				if req := model.NestedString(rc, "request"); req != "" {
					text += " (" + req + ")"
				}
				out = appendUnique(out, text)
			}
		}
	}
	return out
}

// The claims an open drawer reads (A105, A108; owner 2026-10-14 evening): a pod's claims by name,
// for their state in its Resource Claims section, and the claims by the devices allocated to them,
// for the claim that holds each device of a node. Only the drawer reads them (DetailNeeds), so no
// list keeps a watch on every claim.
var (
	// ClaimsByName finds a resource claim by "<namespace>/<name>".
	ClaimsByName = model.RelatedIndex{Kind: keyResourceClaims, Name: "byName", Keys: func(obj *unstructured.Unstructured) []string {
		return []string{obj.GetNamespace() + "/" + obj.GetName()}
	}}
	// ClaimsByDevice finds the claims a device is allocated to by "<driver>/<pool>/<device>".
	ClaimsByDevice = model.RelatedIndex{Kind: keyResourceClaims, Name: "byDevice", Keys: func(obj *unstructured.Unstructured) []string {
		var keys []string
		for _, r := range claimResults(obj) {
			keys = appendUnique(keys, r.driver+"/"+r.pool+"/"+r.device)
		}
		return keys
	}}
)

// podDetailWith is podDetail with the state of each claim the pod uses (A105), from the claims the
// open drawer reads.
func podDetailWith(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	return podDetailRel(obj, rel)
}

// claimStateCell is a pod's claim's state as the Resource Claims view shows it, "Loading…" while
// the claims are still listing, "(not found)" for one the listed claims do not hold.
func claimStateCell(namespace, claim string, rel model.Related) model.Cell {
	if claim == "" {
		return model.Cell{}
	}
	if found := rel.Objects(ClaimsByName, namespace+"/"+claim); len(found) > 0 {
		text, token := claimState(found[0])
		return model.Cell{Text: text, Status: token}
	}
	if !rel.Listed(ClaimsByName) {
		return model.Cell{Text: "Loading…"}
	}
	return model.Cell{Text: "(not found)"}
}

// deviceClaimCell is the claim that holds a node's device (A108), linked; empty when none does.
func deviceClaimCell(driver, pool, device string, rel model.Related) model.Cell {
	found := rel.Objects(ClaimsByDevice, driver+"/"+pool+"/"+device)
	if len(found) == 0 {
		return model.Cell{}
	}
	c := found[0]
	return model.Cell{Text: c.GetNamespace() + "/" + c.GetName(), Link: linkTo(keyResourceClaims, c.GetNamespace(), c.GetName())}
}
