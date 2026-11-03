package kinds_test

import (
	"testing"
	"time"
	_ "time/tzdata" // the Europe/Berlin cases must not depend on the machine's zone database

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Cron Jobs view computes Next execution from the clock when it builds a row; the goldens and
// the fuzz targets build rows at the fixed now they render against.
func init() {
	kinds.SetClock(func() time.Time { return now })
}

func FuzzCronJobs(f *testing.F) { fuzzProfile(f, "cronjobs") }

func TestCronJobsStatusTexts(t *testing.T) {
	const (
		none    = model.StatusNone
		ok      = model.StatusOK
		warning = model.StatusWarning
		bad     = model.StatusError
	)
	checkStatusTexts(t, []statusText{
		// As captured from Lens (2026-10-11): the form of Next execution, Time zone "-" when unset,
		// Suspend "true" on a suspended job, which still shows a Next execution.
		{"cronjobs", "healthy", "nextExecution", "In about 1 hour 57 min", none, ok},
		{"cronjobs", "healthy", "timeZone", "-", none, ok},
		{"cronjobs", "healthy", "suspend", "false", none, ok},
		{"cronjobs", "suspended", "suspend", "true", none, none},
		{"cronjobs", "suspended", "nextExecution", "In about 14 hours", none, none},

		{"cronjobs", "healthy", "schedule", "57 13 * * *", none, ok},
		{"cronjobs", "healthy", "lastSchedule", "22h", none, ok},
		{"cronjobs", "healthy", "signals", "", none, ok},
		{"cronjobs", "time-zone", "timeZone", "Europe/Berlin", none, ok},
		{"cronjobs", "time-zone", "nextExecution", "In about 13 hours 30 min", none, ok},
		{"cronjobs", "minimal", "nextExecution", "In about 1 hour", none, ok},
		{"cronjobs", "minimal", "lastSchedule", "", none, ok},
		{"cronjobs", "every", "nextExecution", "In about 40 min", none, ok},
		{"cronjobs", "active", "active", "1", none, ok},
		{"cronjobs", "active", "signals", "", none, ok},
		{"cronjobs", "terminating", "schedule", "0 4 * * 0", none, model.StatusTerminating},

		// The warning icon's rule is inferred, not captured.
		{"cronjobs", "failing", "signals", "The last scheduled job did not succeed", warning, warning},
		{"cronjobs", "failing", "nextExecution", "In about 15 min", none, warning},
		{"cronjobs", "invalid-schedule", "signals", "Unparsable schedule: no job runs", bad, bad},
		{"cronjobs", "invalid-schedule", "nextExecution", "", none, bad},
		{"cronjobs", "unknown-zone", "signals", "Unknown time zone: no job runs", bad, bad},
		{"cronjobs", "unknown-zone", "timeZone", "Mars/Olympus_Mons", none, bad},
	})
}
