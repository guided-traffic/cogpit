package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// Who Cogpit is logged in as (D-68, the owner). At every connect to a real cluster the connection
// asks the API server who it is (cluster.Conn.ReviewUser: one SelfSubjectReview, what `kubectl auth
// whoami` asks), and asks again whenever the connection comes back from an interruption or a
// renewed login (connwatch.go's reconnect). The navigator's root row shows the username, its groups
// behind a box's tooltip; agents read the same in ui_state's user. When the API server does not
// know the review (older than 1.27) or refuses it, the kubeconfig's user entry's name stands in,
// marked as such; while the cluster is not connected, the last username the API server confirmed
// for the context, with when it was last seen (whoami_store.go). A review that fails never fails
// the connect.

// Where ClusterUser.Username comes from.
const (
	// UserFromAPIServer: the API server answered the connection's SelfSubjectReview.
	UserFromAPIServer = "apiServer"
	// UserFromKubeconfig: it did not (ClusterUser.Reason says why); Username is the name of the
	// context's user entry in its kubeconfig, a label that says nothing of who the server sees.
	UserFromKubeconfig = "kubeconfig"
	// UserLastSeen: the cluster is not connected (or its connect has not asked yet); Username is the
	// last one the API server confirmed for the context, LastSeen when Cogpit last saw it so.
	UserLastSeen = "lastSeen"
)

// userReviewTimeout bounds the review at a connect: a server that does not answer it delays the
// connect by at most this long (the heartbeat's own bound, D-58), and the fallback stands in.
const userReviewTimeout = DefaultProbeTimeout

// ClusterUser is who Cogpit is logged in as on the shown cluster: UIState.User.
type ClusterUser struct {
	// Context is the context id the user belongs to, as UIState.Context names it.
	Context string `json:"context"`
	// Username is the user's name; empty when the kubeconfig names no user either.
	Username string `json:"username"`
	// Groups are the user's groups as the API server answered them; only with UserFromAPIServer.
	Groups []string `json:"groups,omitempty"`
	// Source is UserFromAPIServer, UserFromKubeconfig or UserLastSeen.
	Source string `json:"source"`
	// Reason says why the API server did not confirm the user (UserFromKubeconfig).
	Reason string `json:"reason,omitempty"`
	// LastSeen is when Cogpit last saw the API server confirm the user (UserLastSeen).
	LastSeen time.Time `json:"lastSeen,omitzero"`
}

func (u *ClusterUser) clone() *ClusterUser {
	if u == nil {
		return nil
	}
	c := *u
	c.Groups = slices.Clone(u.Groups)
	return &c
}

// clusterUserOf turns a connection's answer into what the UI state shows.
func clusterUserOf(context string, info cluster.UserInfo) ClusterUser {
	if info.Confirmed {
		return ClusterUser{Context: context, Username: info.Username, Groups: slices.Clone(info.Groups), Source: UserFromAPIServer}
	}
	return ClusterUser{Context: context, Username: info.Username, Source: UserFromKubeconfig, Reason: info.Reason}
}

// syncUser keeps User the shown cluster's user, after every change: the connection's answer while
// it is live, else the context's last known user. A connection that showed a confirmed user and
// ended (a disconnect, a failed reconnect, another cluster) records that its user was seen at now.
func (u *UIState) syncUser(old UIState, book *userBook, now time.Time) {
	if ou := old.User; ou != nil && ou.Source == UserFromAPIServer && old.Connection.State.Live() &&
		(u.Context != old.Context || !u.Connection.State.Live()) {
		book.seen(ou.Context, ou.Username, now)
	}
	u.User = book.shown(u.Context, u.Connection.State.Live())
}

// learnUser asks who a new connection is logged in as, before the session publishes it, and writes
// the answer into the connect log. The demo cluster and backends without a connection have none.
func (s *Session) learnUser(ctx context.Context, name string, b *Backend) {
	if b == nil {
		return
	}
	if b.Conn == nil {
		s.users.forget(b.Context)
		return
	}
	u, ok := s.reviewUser(ctx, b)
	if !ok {
		return
	}
	s.state.logs.add(name, LogInfo, userLogLine(u, false))
}

// renewUser asks again, in the background, who a connection is logged in as: it came back from an
// interruption or a renewed login, which may be another user's now. The UI state shows the answer
// while the connection is still the session's.
func (s *Session) renewUser(b *Backend) {
	if b == nil || b.Conn == nil {
		return
	}
	s.goSafe("the user review", func() {
		u, ok := s.reviewUser(context.Background(), b)
		if !ok || !s.state.holds(b) {
			return
		}
		s.state.logs.add(b.Context, LogInfo, userLogLine(u, true))
		s.UpdateUI(func(*UIState) {}) // syncUser shows the new answer
	})
}

// reviewUser runs the connection's review, bounded, and records its answer; false when it panicked.
func (s *Session) reviewUser(ctx context.Context, b *Backend) (u ClusterUser, ok bool) {
	rctx, cancel := context.WithTimeout(ctx, userReviewTimeout)
	defer cancel()
	var info cluster.UserInfo
	if err := s.safeCall("the user review", func() error { info = b.Conn.ReviewUser(rctx); return nil }); err != nil {
		s.log.Warn("engine: the user review failed", "context", b.Context, "err", err)
		return ClusterUser{}, false
	}
	u = clusterUserOf(b.Context, info)
	s.users.setLive(u, s.Now())
	s.users.flush(s)
	return u, true
}

// userLogLine is the connect log's line about the user, without credentials: the username and its
// groups, or why the API server did not confirm it and what stands in.
func userLogLine(u ClusterUser, again bool) string {
	verb := "Logged in as "
	if again {
		verb = "Logged in again as "
	}
	if u.Source == UserFromAPIServer {
		line := verb + kubeconfig.DisplayText(u.Username, 200) + " (confirmed by the API server)"
		if len(u.Groups) > 0 {
			line += "; groups: " + kubeconfig.DisplayText(strings.Join(u.Groups, ", "), 300)
		}
		return line
	}
	stand := "the kubeconfig names no user"
	if u.Username != "" {
		stand = fmt.Sprintf("Cogpit shows the kubeconfig's user entry %q, which only names it", kubeconfig.DisplayText(u.Username, 200))
	}
	return "The API server did not confirm the user: " + u.Reason + "; " + stand
}
