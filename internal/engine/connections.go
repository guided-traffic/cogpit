package engine

import (
	"slices"
	"strings"
)

// ConnectedCluster is one cluster the engine holds a connection to (D-69), in
// UIState.Connections: the cluster the window shows, the one it is connecting, and every one it
// left connected. Agents read them in ui_state and may name any of them as a command's context.
type ConnectedCluster struct {
	// Context is the cluster's context id, as cluster_contexts lists it.
	Context string `json:"context"`
	// Connection is its connection's state (D-58: connected, interrupted, loginExpired; connecting
	// while the shown cluster's connect runs), with since, nextAttempt and reason. The shown
	// cluster's is UIState.Connection.
	Connection Connection `json:"connection"`
	// Shown is set for the cluster the window shows (UIState.Context).
	Shown bool `json:"shown,omitempty"`
	// Cold is set for a cluster that is connected but not shown, once its views' lists and watches
	// left the cache after its warm period (5 minutes): showing it again lists them anew, which
	// takes seconds. Its heartbeat, access reviews and the dock's streams go on. Within the warm
	// period it is not cold, and showing it again lists nothing.
	Cold bool `json:"cold,omitempty"`
	// LoginLapsed is set for a cluster that is not shown whose login lapsed and was not renewed:
	// its cluster setting "Renew the login while the cluster is not shown" is off, so Cogpit held
	// its credential plugin (D-69 item 4, connwatch_lapse.go). Its connection stays connected (not
	// red: whether a renewal would work is not known), with since and reason; writes to it are
	// refused, its heartbeat pauses, reads that need the cluster fail. Showing the cluster, or Log
	// in again (cluster.login with its context), renews the login.
	LoginLapsed bool `json:"loginLapsed,omitempty"`

	gen     uint64 // the connect that made it (pooled.gen): a watch writes only its own connection's state
	running bool   // its connect runs (connect_run.go): connecting, it stays when another cluster is shown
}

// connectionIndex returns the index of a context's entry of Connections, made by the connect gen
// (0: whichever), or -1.
func (u *UIState) connectionIndex(contextID string, gen uint64) int {
	return slices.IndexFunc(u.Connections, func(c ConnectedCluster) bool {
		return c.Context == contextID && (gen == 0 || c.gen == gen)
	})
}

// ConnectionOf returns a context's entry of Connections: whether the engine holds a connection to
// it, with its state.
func (u UIState) ConnectionOf(contextID string) (ConnectedCluster, bool) {
	if i := u.connectionIndex(contextID, 0); i >= 0 {
		return u.Connections[i], true
	}
	return ConnectedCluster{}, false
}

// connectionState is a context's connection state as the UI state holds it: the shown cluster's
// Connection, another connected cluster's entry, or "" when it is not connected.
func (u *UIState) connectionState(contextID string) ConnectionState {
	if contextID == "" {
		return ""
	}
	if contextID == u.Context && activeConnection(u.Connection.State) {
		return u.Connection.State
	}
	if i := u.connectionIndex(contextID, 0); i >= 0 {
		return u.Connections[i].Connection.State
	}
	return ""
}

// dropConnection removes a context's entry of Connections made by the connect gen (0: whichever).
func (u *UIState) dropConnection(contextID string, gen uint64) {
	u.Connections = slices.DeleteFunc(u.Connections, func(c ConnectedCluster) bool {
		return c.Context == contextID && (gen == 0 || c.gen == gen)
	})
	if len(u.Connections) == 0 {
		u.Connections = nil
	}
}

// syncConnections keeps Connections in step with the shown cluster after every change (UpdateUI):
// the entry of the cluster the window shows, or connects, says Shown, mirrors Connection and is
// not cold; a connect under way has an entry of its own, which stays while another cluster is
// shown (connect_run.go, D-97); one that ended without a connection leaves none. The engine adds
// the entries of connections and removes those of disconnects itself; the connection watches write
// the others' states (connwatch.go).
func (u *UIState) syncConnections() {
	shownID := ""
	if u.Context != "" && activeConnection(u.Connection.State) {
		shownID = u.Context
	}
	u.Connections = slices.DeleteFunc(u.Connections, func(c ConnectedCluster) bool {
		return c.Connection.State == Connecting && c.Context != shownID && !c.running
	})
	found := false
	for i := range u.Connections {
		c := &u.Connections[i]
		c.Shown = c.Context == shownID
		if !c.Shown {
			continue
		}
		found = true
		if u.Connection.State == Connecting && c.gen != 0 && c.Connection.State.Live() {
			continue // its connect run connected: the connect waiting for it shows it next (awaitRun)
		}
		c.Connection, c.Cold = u.Connection, false
		if u.Connection.State.Live() {
			c.gen, c.running = u.connGen, false
		}
	}
	if !found && shownID != "" {
		c := ConnectedCluster{Context: shownID, Connection: u.Connection, Shown: true}
		if u.Connection.State.Live() {
			c.gen = u.connGen
		}
		at, _ := slices.BinarySearchFunc(u.Connections, shownID, func(c ConnectedCluster, id string) int { return strings.Compare(c.Context, id) })
		u.Connections = slices.Insert(u.Connections, at, c)
	}
	if len(u.Connections) == 0 {
		u.Connections = nil
	}
}
