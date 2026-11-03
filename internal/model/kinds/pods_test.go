package kinds_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// TestPodWarningTexts pins the warning column of recorded pods. Lens's rule is not captured (an
// orange triangle on a pod with many restarts and a container not ready); this is Cogpit's.
func TestPodWarningTexts(t *testing.T) {
	checkStatusTexts(t, []statusText{
		{"pods", "healthy", "signals", "", model.StatusNone, model.StatusOK},
		{"pods", "failing", "signals", "CrashLoopBackOff; Containers not ready: worker; 7 restarts", model.StatusWarning, model.StatusError},
		{"pods", "not-ready", "signals", "Containers not ready: auth-proxy; 2 restarts", model.StatusWarning, model.StatusWarning},
		{"pods", "unschedulable", "signals", "Unschedulable", model.StatusWarning, model.StatusPending},
		{"pods", "evicted", "signals", "Evicted", model.StatusWarning, model.StatusError},
		{"pods", "completed-job", "signals", "", model.StatusNone, model.StatusSucceeded},
		{"pods", "terminating", "signals", "", model.StatusNone, model.StatusTerminating},
		{"pods", "container-creating", "signals", "", model.StatusNone, model.StatusPending},
		{"pods", "init-progress", "signals", "", model.StatusNone, model.StatusPending},
	})
}
