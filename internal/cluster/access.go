package cluster

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The access reviews' own request budget. Reviews are cheap for the API server (it evaluates them
// from its authorizers' memory, no storage read), and connecting may ask one rules review per
// namespace the user can see: on the cluster's shared budget (DefaultQPS, DefaultBurst) they would
// delay the first rows of a view (budget P1), so they get their own token bucket, as discovery does.
const (
	reviewQPS   = 10
	reviewBurst = 20
)

// AccessRule is one rule of a rules review: verbs on resources of API groups, optionally limited
// to named objects. "*" in a list means all; "*/log" names the subresource log of every resource.
type AccessRule struct {
	Verbs         []string `json:"verbs"`
	APIGroups     []string `json:"apiGroups,omitempty"`
	Resources     []string `json:"resources,omitempty"`
	ResourceNames []string `json:"resourceNames,omitempty"`
}

// AccessRules is what a SelfSubjectRulesReview answers for one namespace: the rules that let the
// connected user act there, those of their cluster-wide bindings included. Rules only add
// permissions, so a verb a rule names is allowed even when the list is incomplete; one no rule
// names is denied only when it is complete.
type AccessRules struct {
	Rules []AccessRule `json:"rules"`
	// Incomplete is set when an authorizer could not list its rules (a webhook, the cloud
	// providers' IAM) or the evaluation failed in part: a verb no rule names may still be allowed.
	Incomplete bool `json:"incomplete,omitempty"`
}

// RuleMatch is what rules say about one verb on one resource.
type RuleMatch int

// What rules say about a verb.
const (
	// RuleNone: no rule names the verb.
	RuleNone RuleMatch = iota
	// RuleNamesOnly: only rules limited to named objects name it; whether it applies depends on
	// the object, which the question does not name.
	RuleNamesOnly
	// RuleAllows: a rule allows it on every object.
	RuleAllows
)

// AccessAttributes name one verb on one resource for an access review. Namespace "" asks about a
// cluster-scoped resource, or about a namespaced one in every namespace.
type AccessAttributes struct {
	Namespace   string `json:"namespace,omitempty"`
	Verb        string `json:"verb"`
	Group       string `json:"group,omitempty"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
}

// String says "list pods in team-a", "patch deployments.apps/scale".
func (a AccessAttributes) String() string {
	res := a.Resource
	if a.Group != "" {
		res += "." + a.Group
	}
	if a.Subresource != "" {
		res += "/" + a.Subresource
	}
	out := a.Verb + " " + res
	if a.Namespace != "" {
		out += " in " + a.Namespace
	}
	return out
}

// Match says what the rules say about a verb on a resource (the attributes' namespace is not
// looked at: the rules are one namespace's already). Wildcards follow the API's documentation of
// ResourceRule: "*" matches every verb, group or resource, subresources included, and "*/<sub>"
// matches the subresource of every resource.
func (r AccessRules) Match(a AccessAttributes) RuleMatch {
	resource := a.Resource
	if a.Subresource != "" {
		resource += "/" + a.Subresource
	}
	out := RuleNone
	for _, rule := range r.Rules {
		if !matchesAny(rule.Verbs, a.Verb) || !matchesAny(rule.APIGroups, a.Group) || !matchesResource(rule.Resources, resource, a.Subresource) {
			continue
		}
		if len(rule.ResourceNames) == 0 || slices.Contains(rule.ResourceNames, "*") {
			return RuleAllows
		}
		out = RuleNamesOnly
	}
	return out
}

func matchesAny(list []string, v string) bool {
	return slices.Contains(list, "*") || slices.Contains(list, v)
}

func matchesResource(list []string, resource, subresource string) bool {
	for _, r := range list {
		if r == "*" || r == resource {
			return true
		}
		if subresource != "" && r == "*/"+subresource {
			return true
		}
	}
	return false
}

// reviewClient is a connection's client for access reviews, made on first use: on the
// connection's HTTP client (its connection pool and request counter) with the reviews' own budget.
type reviewClient struct {
	once    sync.Once
	client  authorizationclient.AuthorizationV1Interface
	err     error
	limiter atomic.Pointer[measuredLimiter] // the reviews' rate limit (D-133); nil until the first review, and in tests
}

func (c *Conn) reviews() (authorizationclient.AuthorizationV1Interface, error) {
	c.access.once.Do(func() {
		if c.config == nil || c.httpClient == nil {
			// A connection built around a clientset (tests): its clientset answers.
			c.access.client = c.kube.AuthorizationV1()
			return
		}
		cfg := rest.CopyConfig(c.config)
		cfg.QPS, cfg.Burst = reviewQPS, reviewBurst
		limiter := newMeasuredLimiter(BudgetReviews, reviewQPS, reviewBurst, c.diag)
		c.access.limiter.Store(limiter)
		cfg.RateLimiter = limiter
		cl, err := authorizationclient.NewForConfigAndClient(cfg, c.httpClient)
		if err != nil {
			c.access.err = fmt.Errorf("context %q: access reviews: %w", c.contextName, kubeconfig.SafeError(err))
			return
		}
		c.access.client = cl
	})
	return c.access.client, c.access.err
}

// ReviewRules asks the API server which verbs the connected user has in a namespace: one
// SelfSubjectRulesReview (create selfsubjectrulesreviews.authorization.k8s.io, which every
// authenticated user may), counted by the request counter. It writes nothing. The answer is
// meant for what Cogpit shows and offers; the API server still decides every request.
func (c *Conn) ReviewRules(ctx context.Context, namespace string) (AccessRules, error) {
	cl, err := c.reviews()
	if err != nil {
		return AccessRules{}, err
	}
	res, err := cl.SelfSubjectRulesReviews().Create(ctx, &authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{Namespace: namespace},
	}, metav1.CreateOptions{})
	if err != nil {
		return AccessRules{}, fmt.Errorf("context %q: rules review in %s: %w", c.contextName, namespace, err)
	}
	out := AccessRules{
		Rules: make([]AccessRule, 0, len(res.Status.ResourceRules)),
		// An evaluation error means some rules may be missing (a binding to a role that does not
		// exist, an authorizer that cannot list rules).
		Incomplete: res.Status.Incomplete || strings.TrimSpace(res.Status.EvaluationError) != "",
	}
	for _, r := range res.Status.ResourceRules {
		out.Rules = append(out.Rules, AccessRule{
			Verbs: r.Verbs, APIGroups: r.APIGroups, Resources: r.Resources, ResourceNames: r.ResourceNames,
		})
	}
	return out, nil
}

// ReviewAccess asks the API server whether the connected user may use one verb: one
// SelfSubjectAccessReview (create selfsubjectaccessreviews.authorization.k8s.io), counted by the
// request counter. It writes nothing. known is false when the server said no but reported an
// evaluation error: an authorizer failed, so the denial may not be the final word.
func (c *Conn) ReviewAccess(ctx context.Context, a AccessAttributes) (allowed, known bool, err error) {
	cl, err := c.reviews()
	if err != nil {
		return false, false, err
	}
	res, err := cl.SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{
			Namespace: a.Namespace, Verb: a.Verb, Group: a.Group, Resource: a.Resource, Subresource: a.Subresource,
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		return false, false, fmt.Errorf("context %q: access review of %s: %w", c.contextName, a, err)
	}
	if res.Status.Allowed {
		return true, true, nil
	}
	return false, strings.TrimSpace(res.Status.EvaluationError) == "", nil
}
