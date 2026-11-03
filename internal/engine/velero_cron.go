package engine

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// A Velero schedule's times as the Velero page reads them (velero.go), with Velero's own rules
// (pkg/controller/schedule_controller.go, the same at 1.17.1 and 1.18.4):
//
//   - The cron is parsed as cron.ParseStandard does: five fields or a descriptor, a CRON_TZ= or TZ=
//     prefix naming its zone. Without one it runs in the zone of Velero's server process, UTC
//     unless its container sets TZ. The page writes a schedule's times of day in that zone, as the
//     cron does, and names the zone where the user's shows another time (zoneNote).
//   - The next run is cron.Next(max(status.lastBackup, or the creation without one,
//     status.lastSkipped)); the controller looks once a minute and creates the backup once that
//     time has passed.
//   - A due run is not created while a backup of the same schedule (label velero.io/schedule-name)
//     is in the phase New, InProgress or none yet; it is not caught up either: once that backup
//     ends, one backup is created for every run that came due meanwhile.
//
// So a schedule is overdue when its next run lies more than veleroGrace in the past; a run that
// came due between two backups of a schedule and was absorbed by the later one, or that is due and
// not created, is a missed run (a red ring in the schedule's last runs).

// veleroGrace is how long after its time a run may still be on its way: the controller's minute,
// a slow reconcile, and the clocks of Velero and Cogpit.
const veleroGrace = 5 * time.Minute

// veleroDots is how many of a schedule's last runs the page shows.
const veleroDots = 7

// The states of a schedule's run, a dot each.
const (
	runCompleted        = "completed"
	runPartiallyFailed  = "partiallyFailed"
	runFailed           = "failed"
	runFailedValidation = "failedValidation"
	runRunning          = "running"
	runDeleting         = "deleting"
	runMissed           = "missed"
)

// Velero's backup and restore phases the page tells apart.
const (
	phaseNew              = "New"
	phaseInProgress       = "InProgress"
	phaseCompleted        = "Completed"
	phasePartiallyFailed  = "PartiallyFailed"
	phaseFailed           = "Failed"
	phaseFailedValidation = "FailedValidation"
	phaseDeleting         = "Deleting"
)

// runState is the dot of a backup's phase: Completed, PartiallyFailed, Failed and
// FailedValidation their own, Deleting its own, every other phase (none yet, New, Queued,
// ReadyToStart, InProgress, WaitingForPluginOperations, Finalizing and their PartiallyFailed
// forms) running.
func runState(phase string) string {
	switch phase {
	case phaseCompleted:
		return runCompleted
	case phasePartiallyFailed:
		return runPartiallyFailed
	case phaseFailed:
		return runFailed
	case phaseFailedValidation:
		return runFailedValidation
	case phaseDeleting:
		return runDeleting
	}
	return runRunning
}

// blocksSchedule reports whether a backup in this phase keeps its schedule from creating a due
// run (checkIfBackupInNewOrProgress).
func blocksSchedule(phase string) bool {
	return phase == "" || phase == phaseNew || phase == phaseInProgress
}

// finished reports whether a backup's phase is final for the page: it has an outcome.
func finished(state string) bool {
	switch state {
	case runCompleted, runPartiallyFailed, runFailed, runFailedValidation:
		return true
	}
	return false
}

// veleroCron is a schedule's parsed cron.
type veleroCron struct {
	sched cron.Schedule // nil when it does not parse
	zone  string        // the zone its prefix names, "" without
	loc   *time.Location
	words string
}

// parseVeleroCron parses a schedule's cron; server is the zone of Velero's process (UTC unless
// its container sets TZ), the cron's own zone without a prefix.
func parseVeleroCron(spec string, server *time.Location) veleroCron {
	c := veleroCron{loc: server}
	if server == nil {
		c.loc = time.UTC
	}
	body := strings.TrimSpace(spec)
	if strings.HasPrefix(body, "CRON_TZ=") || strings.HasPrefix(body, "TZ=") {
		i := strings.IndexByte(body, ' ')
		if i < 0 {
			c.words = spec
			return c
		}
		c.zone = body[strings.IndexByte(body, '=')+1 : i]
		body = strings.TrimSpace(body[i+1:])
		if loc, err := time.LoadLocation(c.zone); err == nil {
			c.loc = loc
		}
	}
	if s, err := cron.ParseStandard(spec); err == nil {
		c.sched = s
	}
	c.words = cronWords(body)
	if c.words == "" {
		c.words = body
	}
	return c
}

// zoneNote names the cron's zone after a time the page writes in it ("daily 02:00 UTC", "due 02:00
// UTC") when the user's zone (user; nil: UTC) shows another time of day at at; "" when both agree,
// so that a time of day on the page is never read in the wrong zone.
func (c veleroCron) zoneNote(user *time.Location, at time.Time) string {
	if user == nil {
		user = time.UTC
	}
	if at.IsZero() {
		at = time.Now()
	}
	_, mine := at.In(user).Zone()
	name, theirs := at.In(c.loc).Zone()
	if mine == theirs {
		return ""
	}
	if s := c.loc.String(); s != "Local" && s != "" {
		name = s
	}
	return " " + name
}

// next is the first run after t, in the cron's zone; zero when the cron does not parse.
func (c veleroCron) next(t time.Time) time.Time {
	if c.sched == nil {
		return time.Time{}
	}
	return c.sched.Next(t.In(c.loc))
}

// fires are the runs after `after` up to and including `until`, at most n.
func (c veleroCron) fires(after, until time.Time, n int) []time.Time {
	var out []time.Time
	for f := c.next(after); !f.IsZero() && !f.After(until) && len(out) < n; f = c.next(f) {
		out = append(out, f)
	}
	return out
}

// The weekdays as cron numbers them, and their names in cron.
var (
	weekdays    = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	cronDayName = map[string]string{"SUN": "0", "MON": "1", "TUE": "2", "WED": "3", "THU": "4", "FRI": "5", "SAT": "6"}
)

// cronWords writes a cron in words for the shapes schedules have: "daily 02:00", "hourly :05",
// "every 6 h", "every 15 min", "Sun 03:00", "Mon–Fri 01:30", "monthly, day 1 04:00"; "" for any
// other shape, which the page writes as it is.
func cronWords(body string) string {
	if strings.HasPrefix(body, "@") {
		return descriptorWords(body)
	}
	f := strings.Fields(body)
	if len(f) != 5 {
		return ""
	}
	minute, hour, dom, month, dow := f[0], f[1], f[2], f[3], f[4]
	if month != "*" || !anyField(dom) && (!isNumber(dom) || !anyField(dow)) {
		return ""
	}
	days, ok := dowWords(dow)
	if !ok {
		return ""
	}
	if isNumber(dom) {
		if !isNumber(minute) || !isNumber(hour) {
			return ""
		}
		return "monthly, day " + dom + " " + clock(hour, minute)
	}
	if t, ok := timesOfDay(minute, hour); ok {
		if days == "" {
			return "daily " + t
		}
		return days + " " + t
	}
	if days != "" {
		return ""
	}
	return everyWords(minute, hour)
}

// everyWords writes the repeating shapes: hourly at a minute, every n hours, every n minutes.
func everyWords(minute, hour string) string {
	switch {
	case isNumber(minute) && hour == "*":
		return "hourly :" + twoDigits(minute)
	case isNumber(minute) && stepOf(hour) != "":
		w := "every " + stepOf(hour) + " h"
		if m, _ := strconv.Atoi(minute); m != 0 {
			w += " at :" + twoDigits(minute)
		}
		return w
	case stepOf(minute) != "" && hour == "*":
		return "every " + stepOf(minute) + " min"
	case minute == "*" && hour == "*":
		return "every minute"
	}
	return ""
}

// descriptorWords writes cron's descriptors.
func descriptorWords(d string) string {
	switch d {
	case "@yearly", "@annually":
		return "yearly, Jan 1 00:00"
	case "@monthly":
		return "monthly, day 1 00:00"
	case "@weekly":
		return "Sun 00:00"
	case "@daily", "@midnight":
		return "daily 00:00"
	case "@hourly":
		return "hourly :00"
	}
	if every, ok := strings.CutPrefix(d, "@every "); ok {
		return "every " + strings.TrimSpace(every)
	}
	return ""
}

// timesOfDay writes fixed times of day: one minute and one or more hours ("02:00, 14:00").
func timesOfDay(minute, hour string) (string, bool) {
	if !isNumber(minute) {
		return "", false
	}
	hours := strings.Split(hour, ",")
	out := make([]string, 0, len(hours))
	for _, h := range hours {
		if !isNumber(h) {
			return "", false
		}
		out = append(out, clock(h, minute))
	}
	return strings.Join(out, ", "), true
}

// dowWords writes a weekday field: "" for every day, "Sun", "Mon–Fri", "Mon, Wed, Fri".
func dowWords(dow string) (string, bool) {
	if anyField(dow) {
		return "", true
	}
	day := func(s string) (string, bool) {
		if n, ok := cronDayName[strings.ToUpper(s)]; ok {
			s = n
		}
		i, err := strconv.Atoi(s)
		if err != nil || i < 0 || i > 7 {
			return "", false
		}
		return weekdays[i%7], true
	}
	if lo, hi, ok := strings.Cut(dow, "-"); ok {
		a, okA := day(lo)
		b, okB := day(hi)
		return a + "–" + b, okA && okB
	}
	parts := strings.Split(dow, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		d, ok := day(p)
		if !ok {
			return "", false
		}
		out = append(out, d)
	}
	return strings.Join(out, ", "), true
}

func anyField(f string) bool { return f == "*" || f == "?" }

func isNumber(f string) bool {
	_, err := strconv.Atoi(f)
	return err == nil && f != "" && f[0] != '-' && f[0] != '+'
}

// stepOf is n of "*/n" and "0/n", "" for any other field.
func stepOf(f string) string {
	for _, prefix := range []string{"*/", "0/"} {
		if n, ok := strings.CutPrefix(f, prefix); ok && isNumber(n) {
			return n
		}
	}
	return ""
}

func twoDigits(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

func clock(hour, minute string) string { return twoDigits(hour) + ":" + twoDigits(minute) }

// backupRun is one backup of a schedule: its name, phase and the time its run began.
type backupRun struct {
	name  string
	phase string
	at    time.Time
}

// scheduleRuns are a schedule's last runs, oldest first, at most veleroDots: its backups, the runs
// cron expected between two of them that the later one absorbed, and the runs due since the last
// one that were never created. backups are sorted by at, oldest first; base is the controller's
// base for the next run (max(lastBackup or creation, lastSkipped)); skipped is lastSkipped.
func scheduleRuns(c veleroCron, backups []backupRun, base, skipped, now time.Time) []VeleroRun {
	var out []VeleroRun // newest first
	if c.sched != nil {
		due := c.fires(base, now.Add(-veleroGrace), veleroDots)
		for i := len(due) - 1; i >= 0; i-- {
			out = append(out, VeleroRun{State: runMissed, At: due[i]})
		}
	}
	for i := len(backups) - 1; i >= 0 && len(out) < veleroDots; i-- {
		b := backups[i]
		out = append(out, VeleroRun{State: runState(b.phase), At: b.at, Backup: b.name})
		if i == 0 || c.sched == nil {
			continue
		}
		from := backups[i-1].at
		if skipped.After(from) && skipped.Before(b.at) {
			from = skipped // the runs before a skip were left out on purpose
		}
		fires := c.fires(from, b.at, veleroDots+1)
		for j := len(fires) - 2; j >= 0; j-- { // the last one is the run b made
			out = append(out, VeleroRun{State: runMissed, At: fires[j]})
		}
	}
	out = out[:min(len(out), veleroDots)]
	slices.Reverse(out)
	return out
}

// veleroDuration writes a run's length: "45s", "14m", "1h 47m", "2d 3h".
func veleroDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if m == 0 {
			return strconv.Itoa(h) + "h"
		}
		return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
	}
	days, h := int(d.Hours())/24, int(d.Hours())%24
	if h == 0 {
		return strconv.Itoa(days) + "d"
	}
	return strconv.Itoa(days) + "d " + strconv.Itoa(h) + "h"
}

// veleroTTL writes a backup's time to live: "30d", "36h", "90m".
func veleroTTL(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d%(24*time.Hour) == 0:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	case d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/time.Minute)) + "m"
}

// runTimeOf is when a backup's run began: the time in its name when the schedule named it
// (<schedule>-YYYYMMDDHHMMSS, Velero's clock at submission, kept by a backup synced from its
// storage location where the creation time is the sync's), else its start, else its creation.
func runTimeOf(name, schedule string, start, created time.Time, server *time.Location) time.Time {
	if stamp, ok := strings.CutPrefix(name, schedule+"-"); ok && len(stamp) == 14 && isNumber(stamp) {
		if server == nil {
			server = time.UTC
		}
		if t, err := time.ParseInLocation("20060102150405", stamp, server); err == nil {
			return t
		}
	}
	if !start.IsZero() {
		return start
	}
	return created
}
