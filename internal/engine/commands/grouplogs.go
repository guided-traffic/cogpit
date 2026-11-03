package commands

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Logs of a GitOps object in one merged tab (A81, the owner's decision of 2026-10-14 evening): every
// workload it deploys, each line starting with "<workload>/<pod>/<container>", a filter per source
// (the tab's workload, pod and container selectors), for the objects that name the workloads they
// deploy: an Argo CD Application (its status.resources) and a Flux Kustomization (its
// status.inventory). engine/grouplogs.go streams them.

// groupLogsLabel is Logs of a group.
const groupLogsLabel = "Logs"

// fluxKustomizationsKey is the Kustomizations' kind key.
const fluxKustomizationsKey = "kustomizations.kustomize.toolkit.fluxcd.io"

// groupLogVerbs are what a group's logs cost besides a workload's: the group's kind read through
// the cache (list and watch), and its workloads' kinds to find their pods.
var groupLogVerbs = append([]engine.Verb{{Verb: nwVerbList, Resource: "*"}, {Verb: nwVerbWatch, Resource: "*"}}, workloadLogVerbs...)

func registerGroupLogs(g *registrar) {
	add(g, groupLogsSpec("argocd.logs", "Argo CD/Logs", kinds.ArgoApplicationsKey,
		"Opens the logs of an Argo CD Application in one dock tab: every container of every pod of the workloads its "+
			"status.resources lists (Deployments, Stateful Sets, Daemon Sets, Replica Sets, Jobs, in their namespaces), "+
			"merged, each line starting with <workload>/<pod>/<container>, and follows them: pods, restarted containers and "+
			"workloads that the application gains or loses join or leave the tab. The tab narrows to one workload, pod and "+
			"container (logs_setSource; logs_sources lists them). It starts with up to 1,000 lines of history shared by the "+
			"workloads' containers and streams about 40 containers at once, at most 20 workloads. Activates the tab already "+
			"open on the Application. Greyed for an Application that deploys to another cluster. Returns the tab.",
		func(app *unstructured.Unstructured) error {
			if elsewhere := kinds.ArgoAppElsewhere(app); elsewhere != "" {
				return fmt.Errorf("%s deploys to another cluster (%s), whose logs Cogpit does not read through this one",
					app.GetName(), elsewhere)
			}
			return nil
		}))
	add(g, groupLogsSpec("flux.logs", "Flux/Logs", fluxKustomizationsKey,
		"Opens the logs of a Flux Kustomization in one dock tab, as argocd_logs does for an Argo CD Application: every "+
			"container of the workloads its status.inventory lists (Deployments, Stateful Sets, Daemon Sets, Replica Sets, "+
			"Jobs, in their namespaces), merged, each line starting with <workload>/<pod>/<container>; workloads it applies "+
			"or prunes later join or leave the tab. Returns the tab.",
		nil))
}

// groupLogsSpec is Logs of a group kind: greyed, with why, when the group names no workload whose
// logs a tab shows (or check says why not); a dock tab merging every one of them.
func groupLogsSpec(id, menuPath, kind, desc string, check func(*unstructured.Unstructured) error) engine.Spec[refParams] {
	// Lens's documented orders put Logs after the kind's other actions (workload.logs: 300).
	return engine.Spec[refParams]{
		ID: id, Label: groupLogsLabel, MenuPath: menuPath, Kinds: []string{kind}, Order: 300,
		Danger: engine.DangerRead, Verbs: groupLogVerbs, Description: desc,
		Enabled: func(s *engine.Session, p refParams) error {
			if p.Ref.Kind != kind {
				return fmt.Errorf("%w: %s takes a %s reference", engine.ErrNotApplicable, id, kind)
			}
			if _, err := streamableOn(s, p.Ref.Context, "logs"); err != nil {
				return err
			}
			ref, _, err := s.ResolveRef(p.Ref)
			if err != nil {
				return err
			}
			obj, err := s.Lookup(ref)
			if err != nil {
				return err
			}
			if check != nil {
				if err := check(obj); err != nil {
					return err
				}
			}
			if len(engine.LogGroupWorkloads(ref, obj)) == 0 {
				return fmt.Errorf("%w: %s names no workload whose logs a tab shows (%v)", engine.ErrNotApplicable, obj.GetName(), engine.WorkloadLogKinds)
			}
			return nil
		},
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			tab, created, err := s.OpenGroupLogsTab(ctx, p.Ref)
			if err != nil {
				return nil, err
			}
			return DockResult{Tab: tab, Created: created}, nil
		},
	}
}
