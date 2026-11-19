package helm

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/kube"
	rspb "helm.sh/helm/v4/pkg/release/v1"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// Defaults of the release actions, those of the helm CLI (inferred: Lens's are not captured),
// except the history an upgrade keeps, which is Lens's (captured).
const (
	// DefaultTimeout bounds the wait for a release's hooks (helm --timeout 5m0s).
	DefaultTimeout = 5 * time.Minute
	// MaxHistory is how many revisions a rollback or an upgrade keeps (helm --history-max 10;
	// Lens keeps ten, captured: 12 revisions before a rollback, 10 after): the SDK deletes the
	// oldest release Secrets beyond it.
	MaxHistory = 10
)

// Actions runs Helm release actions with the Helm SDK in Cogpit's process (D-3), on one
// connection's own client configuration (NewActionConfig: its credentials, rate limiter and
// request counter, no kubeconfig file). The SDK reads and writes the release's records itself
// (list, get, create, update, delete secrets, or configmaps for a release kept in ConfigMaps, in
// the release's namespace) and changes the release's objects with whatever verbs their kinds need:
// those requests are part of the action, not reads of the cache.
type Actions struct {
	// config returns the SDK's action configuration for a namespace and a storage driver, logging
	// to log (nil: the connection's logger).
	config func(namespace string, driver Driver, log *slog.Logger) (*action.Configuration, error)
	// log is the logger the SDK logs to.
	log *slog.Logger
	// Timeout bounds hooks; zero means DefaultTimeout.
	Timeout time.Duration
	// Driver is the storage driver of the release a rollback, an uninstall or an upgrade acts on,
	// the one its records are in (ReleaseStorage); zero means DriverSecret. An install takes its
	// own (InstallRequest.Driver).
	Driver Driver
}

// NewActions returns the release actions on a connection.
func NewActions(conn *cluster.Conn, log *slog.Logger) *Actions {
	if log == nil {
		log = slog.Default()
	}
	return &Actions{log: log, config: func(namespace string, driver Driver, l *slog.Logger) (*action.Configuration, error) {
		if conn == nil {
			return nil, errors.New("no cluster connection")
		}
		return NewActionConfigFor(conn.RESTConfig(), namespace, driver, l)
	}}
}

// driver is the storage driver of the release acted on.
func (a *Actions) driver() Driver {
	if a.Driver == DriverConfigMap {
		return DriverConfigMap
	}
	return DriverSecret
}

func (a *Actions) timeout() time.Duration {
	if a.Timeout > 0 {
		return a.Timeout
	}
	return DefaultTimeout
}

func (a *Actions) logger() *slog.Logger {
	if a.log == nil {
		return slog.Default()
	}
	return a.log
}

// Rollback rolls a release back to an earlier revision, as helm rollback <name> <revision> does
// with its defaults: a new revision with that revision's chart, values and manifest, the objects
// updated to it, hooks run and waited for (not the objects' readiness), history kept to ten
// revisions. Until it finishes the new revision is pending-rollback; a process that dies before
// leaves it so (D-11). The Helm SDK takes no context: the action runs to its end or its timeout.
// A release left pending-upgrade by an interrupted upgrade can be rolled back (the SDK's rollback
// does not refuse a pending release).
//
// from is the revision the caller saw as the newest (its cache's): when the server holds another
// one, the rollback is refused with ErrReleaseMoved and ErrUnchanged before anything changes; 0
// checks nothing.
func (a *Actions) Rollback(namespace, name string, revision, from int) (err error) {
	defer recoverAction(&err, "rollback", namespace, name)
	cfg, err := a.config(namespace, a.driver(), a.logger())
	if err != nil {
		return fmt.Errorf("helm rollback %s/%s: %w", namespace, name, err)
	}
	if from > 0 {
		rls, err := lastRelease(cfg, name)
		if err == nil {
			err = movedFrom(rls, from)
		}
		if err != nil {
			return fmt.Errorf("helm rollback %s/%s: %w; %w", namespace, name, err, ErrUnchanged)
		}
	}
	rb := action.NewRollback(cfg)
	rb.Version = revision
	rb.WaitStrategy = kube.HookOnlyStrategy
	rb.Timeout = a.timeout()
	rb.MaxHistory = MaxHistory
	if err := rb.Run(name); err != nil {
		return fmt.Errorf("helm rollback %s/%s to revision %d: %w", namespace, name, revision, err)
	}
	return nil
}

// Uninstall uninstalls a release, as helm uninstall <name> does with its defaults: hooks run, the
// release's objects deleted (background propagation), its history deleted. Until it finishes the
// release is uninstalling; a process that dies before leaves it so (D-11). The Helm SDK takes no
// context: the action runs to its end or its timeout.
func (a *Actions) Uninstall(namespace, name string) (err error) {
	defer recoverAction(&err, "uninstall", namespace, name)
	cfg, err := a.config(namespace, a.driver(), a.logger())
	if err != nil {
		return fmt.Errorf("helm uninstall %s/%s: %w", namespace, name, err)
	}
	un := action.NewUninstall(cfg)
	un.WaitStrategy = kube.HookOnlyStrategy
	un.Timeout = a.timeout()
	un.DeletionPropagation = "background"
	if _, err := un.Run(name); err != nil {
		return fmt.Errorf("helm uninstall %s/%s: %w", namespace, name, err)
	}
	return nil
}

// ErrUnchanged marks an upgrade that failed before it changed the release: no revision was made,
// and nothing is left to finish or roll back.
var ErrUnchanged = errors.New("the release is unchanged")

// ErrReleaseMoved marks an action refused because the server holds another newest revision than
// the one the caller based it on: its cache had not seen a newer one yet.
var ErrReleaseMoved = errors.New("the release moved")

// movedFrom fails with ErrReleaseMoved when rls is not at revision from (0: no check).
func movedFrom(rls *rspb.Release, from int) error {
	if from > 0 && rls.Version != from {
		return fmt.Errorf("%w: it is at revision %d on the server, the action is based on revision %d", ErrReleaseMoved, rls.Version, from)
	}
	return nil
}

// lastRelease returns a release's newest revision with its chart, as the SDK reads it (a list of
// the release's Secrets: part of the action).
func lastRelease(cfg *action.Configuration, name string) (*rspb.Release, error) {
	last, err := cfg.Releases.Last(name)
	if err != nil {
		return nil, err
	}
	rls, ok := last.(*rspb.Release)
	if !ok || rls == nil || rls.Chart == nil || rls.Chart.Metadata == nil {
		return nil, errors.New("the release's newest revision holds no chart")
	}
	return rls, nil
}

// recoverAction turns a panic in the SDK into the action's error: the goroutine boundary of a
// cluster write (D-11).
func recoverAction(err *error, what, namespace, name string) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("helm %s %s/%s: internal error: %v", what, namespace, name, r)
	}
}
