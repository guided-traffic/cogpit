package kinds

import (
	"math"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// clock is the time the Cron Jobs view computes Next execution from. Tests fix it.
var clock = time.Now

// titleActive is the Active column's title.
const titleActive = "Active"

// Warning texts of the Cron Jobs view (the rule is inferred, not captured from Lens).
const (
	cronBadSchedule = "Unparsable schedule: no job runs"
	cronBadZone     = "Unknown time zone: no job runs"
	cronLastFailed  = "The last scheduled job did not succeed"
)

var cronJobs = &model.KindProfile{
	Key:        "cronjobs",
	Group:      "batch",
	Version:    "v1",
	Resource:   "cronjobs",
	Kind:       "CronJob",
	Title:      "Cron Jobs",
	Namespaced: true,
	// Columns, titles and texts as captured from Lens (2026-10-11).
	Columns: []model.Column{
		nameColumn,
		signalsColumn,
		namespaceColumn,
		{ID: "schedule", Title: "Schedule", Type: model.ColumnText},
		{ID: "suspend", Title: "Suspend", Type: model.ColumnText},
		{ID: "active", Title: titleActive, Type: model.ColumnNumber},
		{ID: "lastSchedule", Title: "Last schedule", Type: model.ColumnAge},
		{ID: "nextExecution", Title: "Next execution", Type: model.ColumnUntil},
		{ID: "timeZone", Title: "Time zone", Type: model.ColumnText},
		ageColumn,
	},
	Cells: cronJobCells,
	Fields: map[string]fieldFunc{
		idImage: func(obj *unstructured.Unstructured) []string {
			return podSpecImages(model.NestedMap(obj, "spec", "jobTemplate", "spec", "template", "spec"))
		},
		"schedule": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "schedule"))
		},
		"timeZone": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "timeZone"))
		},
		"suspend": func(obj *unstructured.Unstructured) []string {
			return []string{cronSuspendText(obj)}
		},
	},
	Detail: cronJobDetail,
	// The Pods table in the drawer, through the jobs (workloadpods.go, D-40).
	DetailNeeds: cronJobPodsNeeds,
	DetailWith:  withPods(cronJobDetail, &JobsByOwner),
}

func cronJobCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	next, problem := cronJobNext(obj, clock())
	warning, status := cronJobHealth(obj, problem)
	zone := model.NestedString(obj, "spec", "timeZone")
	if zone == "" {
		zone = "-" // as captured
	}
	nextCell := model.Cell{}
	if !next.IsZero() {
		nextCell.Num = next.Unix()
	}
	return []model.Cell{
		nameCell(obj),
		warning,
		namespaceCell(obj),
		{Text: model.NestedString(obj, "spec", "schedule")},
		{Text: cronSuspendText(obj)},
		{Num: int64(len(model.NestedSlice(obj, "status", "active")))},
		model.AgeCell(model.NestedTime(obj, "status", "lastScheduleTime")),
		nextCell,
		{Text: zone},
		model.AgeCell(created(obj)),
	}, status
}

// cronSuspendText is the Suspend column: "true" or "false", as captured.
func cronSuspendText(obj any) string {
	if suspended, _ := model.NestedBool(obj, "spec", "suspend"); suspended {
		return textTrue
	}
	return "false"
}

// cronJobHealth returns the warning cell and the row's token. The rule is inferred: a schedule or
// time zone the controller cannot use is an error, because no job will ever run; a last scheduled
// job that did not succeed is a warning; a suspended cron job has nothing to colour.
func cronJobHealth(obj any, problem string) (model.Cell, model.Status) {
	switch {
	case problem != "":
		return stateCell(model.StatusError, problem), model.StatusError
	case cronLastRunFailed(obj):
		return stateCell(model.StatusWarning, cronLastFailed), model.StatusWarning
	}
	if suspended, _ := model.NestedBool(obj, "spec", "suspend"); suspended {
		return model.Cell{}, model.StatusNone
	}
	return model.Cell{}, model.StatusOK
}

// cronLastRunFailed reports whether the job of the last schedule finished without succeeding: it
// is no longer active, and the last success (a completion time) predates the last schedule.
func cronLastRunFailed(obj any) bool {
	last := model.NestedTime(obj, "status", "lastScheduleTime")
	if last.IsZero() || len(model.NestedSlice(obj, "status", "active")) > 0 {
		return false
	}
	return model.NestedTime(obj, "status", "lastSuccessfulTime").Before(last)
}

// cronJobNext returns the cron job's next scheduled run after now, or the zero time when there is
// none, with the reason when the controller cannot use the schedule at all.
//
// Rows are rebuilt only when the object changes, so the time is computed once per change and the
// table renders it relative to the moment it draws. A run that comes due changes the cron job's
// status (the controller records the job and the schedule time) and so rebuilds the row with the
// run after it. Until that happens the cell reads "now": for the moment the controller takes, and
// for a suspended cron job, whose status no run changes, until anything else changes the object.
func cronJobNext(obj any, now time.Time) (time.Time, string) {
	sched, problem := cronSchedule(obj)
	if sched == nil {
		return time.Time{}, problem
	}
	switch s := sched.(type) {
	case *cron.SpecSchedule:
		if !cronMatchable(s) {
			return time.Time{}, ""
		}
		return s.Next(now), ""
	case cron.ConstantDelaySchedule:
		// "@every 2h" counts from the last schedule, or from the creation before the first one,
		// as the controller does.
		base := firstTime(model.NestedTime(obj, "status", "lastScheduleTime"), created(obj), now)
		return everyNext(s.Delay, base, now), ""
	}
	return time.Time{}, ""
}

// cronSchedule parses a cron job's schedule the way the CronJob controller does: the standard
// five fields or a descriptor (@hourly, @every 2h), in spec.timeZone when it is set. A schedule
// that names a zone itself ("TZ=…", which the API no longer accepts) is passed on unchanged, as
// the controller does. Without a time zone the controller uses kube-controller-manager's local
// zone, which Cogpit cannot see; UTC is what it is in almost every cluster (inferred).
func cronSchedule(obj any) (cron.Schedule, string) {
	spec := model.NestedString(obj, "spec", "schedule")
	if strings.Contains(spec, "TZ") {
		if s, ok := parseCron(spec); ok {
			return s, ""
		}
		return nil, cronBadSchedule
	}
	loc := time.UTC
	if zone := model.NestedString(obj, "spec", "timeZone"); zone != "" {
		l, ok := timeZone(zone)
		if !ok {
			return nil, cronBadZone
		}
		loc = l
	}
	s, ok := parseCron(spec)
	if !ok {
		return nil, cronBadSchedule
	}
	if ss, isSpec := s.(*cron.SpecSchedule); isSpec {
		withZone := *ss
		withZone.Location = loc
		return &withZone, ""
	}
	return s, ""
}

// parseCron is cron.ParseStandard without its panics: robfig/cron v3.0.1 slices out of range on
// a "TZ=" prefix without a following space.
func parseCron(spec string) (s cron.Schedule, ok bool) {
	defer func() {
		if recover() != nil {
			s, ok = nil, false
		}
	}()
	if (strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ=")) && !strings.Contains(spec, " ") {
		return nil, false
	}
	s, err := cron.ParseStandard(spec)
	return s, err == nil
}

// cronStarBit is the bit robfig/cron sets in a field written as "*" or "?".
const cronStarBit = 1 << 63

// cronMatchable reports whether every field of a schedule allows at least one value. A field such
// as "," parses to none, and searching five years for a time that cannot match takes millions of
// steps.
func cronMatchable(s *cron.SpecSchedule) bool {
	for _, bits := range [...]uint64{s.Second, s.Minute, s.Hour, s.Dom, s.Month, s.Dow} {
		if bits&^cronStarBit == 0 {
			return false
		}
	}
	return true
}

// everyNext returns the first time after now on the grid base + k·delay (k ≥ 1), base truncated
// to the second as robfig/cron does; the zero time when it does not fit in a time.Duration.
func everyNext(delay time.Duration, base, now time.Time) time.Time {
	if delay <= 0 {
		return time.Time{}
	}
	base = base.Truncate(time.Second)
	if now.Before(base) {
		return base.Add(delay)
	}
	k := int64(now.Sub(base)/delay) + 1
	if k > math.MaxInt64/int64(delay) {
		return time.Time{}
	}
	return base.Add(time.Duration(k) * delay)
}

// zones caches the time zones that loaded, by name: time.LoadLocation reads the zone database
// from disk on every call. Only zones that exist are kept, so it holds at most the database.
var zones sync.Map

// timeZone loads a cron job's time zone. "Local" is refused as the API refuses it: it would be
// Cogpit's own zone, not the controller's.
func timeZone(name string) (*time.Location, bool) {
	if strings.EqualFold(name, "Local") {
		return nil, false
	}
	if l, ok := zones.Load(name); ok {
		loc, isLoc := l.(*time.Location)
		return loc, isLoc
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, false
	}
	zones.Store(name, loc)
	return loc, true
}

// cronHistoryLimits renders "3 successful / 1 failed", the API's defaults standing in for a limit
// that is not set; "" when neither is.
func cronHistoryLimits(obj any) string {
	successful, hasSuccessful := model.NestedInt64(obj, "spec", "successfulJobsHistoryLimit")
	failed, hasFailed := model.NestedInt64(obj, "spec", "failedJobsHistoryLimit")
	if !hasSuccessful && !hasFailed {
		return ""
	}
	if !hasSuccessful {
		successful = 3
	}
	if !hasFailed {
		failed = 1
	}
	return itoa(successful) + " successful / " + itoa(failed) + " failed"
}

func cronJobDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	next, problem := cronJobNext(obj, clock())
	warning, _ := cronJobHealth(obj, problem)
	d.addStatus("Warning", warning.Text, warning.Status)
	d.add("Schedule", model.NestedString(obj, "spec", "schedule"))
	d.add("Time Zone", model.NestedString(obj, "spec", "timeZone"))
	if suspended, _ := model.NestedBool(obj, "spec", "suspend"); suspended {
		d.add("Suspend", condTrue)
	}
	d.add("Concurrency Policy", model.NestedString(obj, "spec", "concurrencyPolicy"))
	if n, ok := model.NestedInt64(obj, "spec", "startingDeadlineSeconds"); ok {
		d.add("Starting Deadline", itoa(n)+"s")
	}
	d.add("History Limits", cronHistoryLimits(obj))
	d.add("Last Schedule", timestamp(model.NestedTime(obj, "status", "lastScheduleTime")))
	d.add("Last Successful", timestamp(model.NestedTime(obj, "status", "lastSuccessfulTime")))
	d.add("Next Execution", timestamp(next))
	var active []string
	for _, j := range model.NestedSlice(obj, "status", "active") {
		active = appendUnique(active, model.NestedString(j, "name"))
	}
	d.add("Active Jobs", strings.Join(active, "\n"))
	d.add("Images", strings.Join(podSpecImages(
		model.NestedMap(obj, "spec", "jobTemplate", "spec", "template", "spec")), "\n"))
	return []model.DetailSection{{Title: "Cron Job", Items: d}}
}
