package engine

import (
	"fmt"
	"time"

	"github.com/guided-traffic/cogpit/internal/model/k8seol"
)

// The API server's version while connected (the owner's colleague's case of 2026-10-25: his
// cluster was upgraded while Cogpit ran, and the version Cogpit showed never changed; the owner's
// "Option A" of 2026-10-26). The heartbeat asks GET /version every 15 s anyway (connwatch.go), and
// cluster.Conn keeps what it read (SeenVersion), so following the version costs no request:
//
//   - A version the heartbeat read twice in a row that differs from the one Cogpit holds becomes
//     it (Backend.ServerVersion): the navigator's version, its upstream support, the Nodes list's
//     version check and a new terminal's kubectl skew follow. Twice, because during a rolling
//     upgrade of a control plane with several API servers behind one address the answers alternate
//     between the old and the new version for minutes; one read would make the version flap.
//   - Within the minor the connection was made for (a patch: no API changes), nothing else
//     happens; the connection log notes it.
//   - Another minor: the connection's discovery, which kinds and API versions the server serves,
//     is still the connect's, and a minor may add kinds and stop serving beta versions. The
//     connection keeps working, and Connection.Upgraded says so until a reconnect (cluster.reconnect,
//     which keeps the tabs, D-81) makes a connection for the new version; the banner offers it. A
//     version that returns to the connect's minor (an upgrade rolled back) clears it.

// ServerUpgrade is Connection.Upgraded: the API server answers with another minor than the one
// the connection was made for.
type ServerUpgrade struct {
	// From is the version the connection was made for, whose kinds and API versions Cogpit knows:
	// "v1.35.3".
	From string `json:"from"`
	// To is the version the API server answers with now: "v1.36.1".
	To string `json:"to"`
	// Since is when Cogpit took To.
	Since time.Time `json:"since"`
}

// versionSeer is a prober that keeps the version its last heartbeat read: *cluster.Conn, or a fake
// in tests.
type versionSeer interface {
	SeenVersion() string
}

// noteVersion follows the version the last heartbeat read; the watch's loop calls it after a
// heartbeat that answered.
func (w *connWatch) noteVersion() {
	vs, ok := w.p.(versionSeer)
	if !ok {
		return
	}
	v := vs.SeenVersion()
	held := w.b.ServerVersion()
	w.mu.Lock()
	switch {
	case v == "" || v == held:
		w.candidate = ""
		w.mu.Unlock()
		return
	case v != w.candidate:
		w.candidate = v // the first read: the next heartbeat confirms it
		w.mu.Unlock()
		return
	}
	w.candidate = ""
	w.mu.Unlock()
	w.s.serverVersionChanged(w, held, v)
}

// serverVersionChanged makes v the backend's version: its version check on the nodes, the UI
// state's ClusterVersion, the connection log, and Connection.Upgraded when its minor is not the
// connect's.
func (s *Session) serverVersionChanged(w *connWatch, from, to string) {
	b := w.b
	b.setServerVersion(to)
	s.versions.set(b.Context, to)
	b.overlays.putOwn(&versionCheck{table: s.releases(), server: to, now: s.Now, format: s.TimeFormat})
	made := b.connectVersion()
	var up *ServerUpgrade
	if !sameMinor(made, to) {
		up = &ServerUpgrade{From: made, To: to, Since: s.Now()}
	}
	w.mu.Lock()
	w.upgrade = up
	w.mu.Unlock()
	text := fmt.Sprintf("The API server's version changed from %s to %s (two heartbeats in a row)", from, to)
	if up != nil {
		text += fmt.Sprintf("; the connection knows the kinds and API versions of %s until a reconnect", made)
	}
	w.logs().add(b.Context, LogInfo, text)
	s.log.Info("engine: the API server's version changed", "context", b.Context, "from", from, "to", to)
	w.publish()
}

// sameMinor reports whether two versions share their major and minor; a version without one
// counts as the same, so that nothing is claimed of it.
func sameMinor(a, b string) bool {
	va, okA := k8seol.ParseVersion(a)
	vb, okB := k8seol.ParseVersion(b)
	return !okA || !okB || va == vb
}
