package testkit

import (
	"io"
	"net/http"
	"slices"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

// FakeAccess is the RBAC of a FakeAPIServer's user, from which it answers access reviews:
// SelfSubjectRulesReview and SelfSubjectAccessReview, as creates on authorization.k8s.io/v1.
// Rules[""] are the rules of the user's cluster-wide bindings: every namespace's rules review
// includes them, and they alone grant cluster-scoped resources and a verb in all namespaces, as on
// a real API server. Rules[ns] are those of the bindings in namespace ns.
type FakeAccess struct {
	Rules map[string][]FakeRule
	// Incomplete marks every rules review incomplete, as an authorizer that cannot list its
	// rules (a webhook) does.
	Incomplete bool
	// Enforce makes the server refuse, with 403 Forbidden, the reads (get, list, watch) the rules
	// do not allow, as RBAC does; without it every read is served (nsrefusing.go).
	Enforce bool
}

// FakeRule is one policy rule; "*" in a list means all.
type FakeRule struct {
	Verbs     []string
	APIGroups []string
	Resources []string
}

// AdminAccess is a cluster-admin's RBAC, not enforced: the access reviews answer as a real API
// server answers them for an administrator. Without Access a FakeAPIServer answers every review 404,
// and what waits for the reviews' answer (Prometheus detection, 3 seconds per detection) waits its
// bound.
func AdminAccess() *FakeAccess {
	return &FakeAccess{Rules: map[string][]FakeRule{"": {{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}}}}
}

// rulesFor returns the rules that apply in a namespace ("": cluster-wide only).
func (a *FakeAccess) rulesFor(namespace string) []FakeRule {
	out := slices.Clone(a.Rules[""])
	if namespace != "" {
		out = append(out, a.Rules[namespace]...)
	}
	return out
}

// allows decides one request the way RBAC does: a rule names the verb, the group and the resource
// (with its subresource, or "*/<subresource>").
func (a *FakeAccess) allows(attrs *authorizationv1.ResourceAttributes) bool {
	res := attrs.Resource
	if attrs.Subresource != "" {
		res += "/" + attrs.Subresource
	}
	for _, r := range a.rulesFor(attrs.Namespace) {
		if !anyOf(r.Verbs, attrs.Verb) || !anyOf(r.APIGroups, attrs.Group) {
			continue
		}
		for _, rr := range r.Resources {
			if rr == "*" || rr == res || (attrs.Subresource != "" && rr == "*/"+attrs.Subresource) {
				return true
			}
		}
	}
	return false
}

func anyOf(list []string, v string) bool {
	return slices.Contains(list, "*") || slices.Contains(list, v)
}

// serveReview answers POST /apis/authorization.k8s.io/v1/<resource>. The typed clients send
// protobuf; the answer is JSON, which they accept too. Without FakeAccess every review is 404.
func (s *FakeAPIServer) serveReview(w http.ResponseWriter, r *http.Request, resource string) {
	s.count("create", resource+".authorization.k8s.io")
	a := s.opts.Access
	if a == nil {
		writeStatus(w, http.StatusNotFound, "NotFound", "the server could not find the requested resource")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "BadRequest", "cannot read the review: "+err.Error())
		return
	}
	obj, _, err := scheme.Codecs.UniversalDeserializer().Decode(body, nil, nil)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "BadRequest", "cannot decode the review: "+err.Error())
		return
	}
	switch review := obj.(type) {
	case *authorizationv1.SelfSubjectRulesReview:
		review.APIVersion, review.Kind = "authorization.k8s.io/v1", "SelfSubjectRulesReview"
		review.Status = authorizationv1.SubjectRulesReviewStatus{
			ResourceRules: []authorizationv1.ResourceRule{}, NonResourceRules: []authorizationv1.NonResourceRule{},
			Incomplete: a.Incomplete,
		}
		for _, rule := range a.rulesFor(review.Spec.Namespace) {
			review.Status.ResourceRules = append(review.Status.ResourceRules, authorizationv1.ResourceRule{
				Verbs: rule.Verbs, APIGroups: rule.APIGroups, Resources: rule.Resources,
			})
		}
		writeJSON(w, http.StatusCreated, review)
	case *authorizationv1.SelfSubjectAccessReview:
		if review.Spec.ResourceAttributes == nil {
			writeStatus(w, http.StatusBadRequest, "BadRequest", "only resource attributes are reviewed")
			return
		}
		review.APIVersion, review.Kind = "authorization.k8s.io/v1", "SelfSubjectAccessReview"
		review.Status.Allowed = a.allows(review.Spec.ResourceAttributes)
		writeJSON(w, http.StatusCreated, review)
	default:
		writeStatus(w, http.StatusNotFound, "NotFound", "the server could not find the requested resource")
	}
}
