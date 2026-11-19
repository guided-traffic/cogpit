package cluster

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	authenticationv1 "k8s.io/api/authentication/v1"
	authenticationv1beta1 "k8s.io/api/authentication/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// Who the connection is logged in as (D-68, the owner): what `kubectl auth whoami` asks, without a
// kubectl process. One SelfSubjectReview (create selfsubjectreviews in authentication.k8s.io, which
// stores nothing; the API server answers with the user it authenticated the request as):
// authentication.k8s.io/v1 (GA since Kubernetes 1.28), and on an API server that does not serve it,
// v1beta1 (1.27). The v1alpha1 of 1.26 sits behind a feature gate that is off by default and is not
// asked. Every authenticated user may create one: the default RBAC's system:basic-user role, bound
// to system:authenticated, allows it (verified in k8s.io/kubernetes v1.37.1's bootstrap policy).
//
// Only the username and the groups are kept: the review's UID and extra fields (which may carry a
// token's credential id or an identity provider's claims) are dropped here and never reach the
// engine, a log or an agent.

// UserInfo is who a connection is logged in as.
type UserInfo struct {
	// Username is the user the API server authenticated the review as ("alice@example.com",
	// "system:serviceaccount:team-a:deployer"); without Confirmed, the name of the context's user
	// entry in its kubeconfig, a label that says nothing of who the API server sees.
	Username string
	// Groups are the user's groups as the API server answered them, in its order; none without
	// Confirmed.
	Groups []string
	// Confirmed says that the API server answered the review.
	Confirmed bool
	// Reason says, in a sentence without credentials, why the API server did not confirm the user:
	// it does not serve the review (404, older than 1.27), refused it (403), or failed.
	Reason string
}

// whoAmI is a connection's last answer, kept on the connection (ReviewUser, User).
type whoAmI struct {
	mu   sync.Mutex
	user *UserInfo
}

// Errors that tell the fallback's reasons apart.
var (
	errReviewNotServed = errors.New("the API server does not serve SelfSubjectReview (404: Kubernetes older than 1.27, or the API turned off)")
	errReviewRefused   = errors.New("the API server refused the SelfSubjectReview (403 Forbidden)")
)

// ReviewUser asks the API server who the connection is logged in as and keeps the answer on the
// connection (User). It never fails: when the server does not serve the review, refuses it or
// fails, the answer is the kubeconfig's user entry's name, not Confirmed, with the reason. It
// costs one request (two on a 1.27 API server: v1, then v1beta1), counted by the request counter.
func (c *Conn) ReviewUser(ctx context.Context) UserInfo {
	u, err := c.selfSubjectReview(ctx)
	if err != nil {
		u = UserInfo{Username: c.kubeconfigUser(), Reason: reviewFailure(err)}
	}
	kept := u
	kept.Groups = slices.Clone(u.Groups)
	c.who.mu.Lock()
	c.who.user = &kept
	c.who.mu.Unlock()
	return u
}

// User returns the answer ReviewUser kept; false before the first.
func (c *Conn) User() (UserInfo, bool) {
	c.who.mu.Lock()
	defer c.who.mu.Unlock()
	if c.who.user == nil {
		return UserInfo{}, false
	}
	u := *c.who.user
	u.Groups = slices.Clone(u.Groups)
	return u, true
}

// selfSubjectReview creates one SelfSubjectReview: v1, else v1beta1 where v1 is not served.
func (c *Conn) selfSubjectReview(ctx context.Context) (UserInfo, error) {
	if c.kube == nil {
		return UserInfo{}, errors.New("no client")
	}
	res, err := c.kube.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authenticationv1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err == nil {
		return confirmedUser(res.Status.UserInfo), nil
	}
	if !apierrors.IsNotFound(err) {
		return UserInfo{}, err
	}
	beta, err := c.kube.AuthenticationV1beta1().SelfSubjectReviews().Create(ctx, &authenticationv1beta1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err != nil {
		return UserInfo{}, err
	}
	return confirmedUser(beta.Status.UserInfo), nil
}

// confirmedUser keeps the username and the groups of an answer; UID and extra are dropped.
func confirmedUser(u authenticationv1.UserInfo) UserInfo {
	groups := make([]string, 0, len(u.Groups))
	for _, g := range u.Groups {
		if g = strings.TrimSpace(g); g != "" && !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}
	return UserInfo{Username: strings.TrimSpace(u.Username), Groups: groups, Confirmed: true}
}

// reviewFailure is the fallback's reason, on one line and without credentials.
func reviewFailure(err error) string {
	switch {
	case apierrors.IsNotFound(err):
		return errReviewNotServed.Error()
	case apierrors.IsForbidden(err):
		return errReviewRefused.Error()
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrNoAnswer):
		return "no answer from the API server to the SelfSubjectReview"
	}
	msg := strings.Join(strings.Fields(kubeconfig.SafeError(err).Error()), " ")
	return "the SelfSubjectReview failed: " + kubeconfig.DisplayText(msg, 300)
}

// kubeconfigUser is the name of the context's user entry in its kubeconfig files, read again from
// the files the connection was made from: a label for the fallback. "" when the files no longer
// name the context.
func (c *Conn) kubeconfigUser() string {
	if len(c.paths) == 0 {
		return ""
	}
	ctxs, _ := ListContexts(c.paths) // a file that went bad is skipped, the others still answer
	for _, k := range ctxs {
		if k.Name == c.contextName {
			return k.User
		}
	}
	return ""
}
