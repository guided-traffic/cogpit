package k8seol

import (
	"strings"
	"testing"
	"time"
)

// germanDate writes a day as a German computer does (D-31), without moving it into a zone.
func germanDate(t time.Time) string { return t.Format("02.01.2006") }

// The table built into this Cogpit parses, passes its checks and reaches from before 1.19 to a
// maintained minor with a release day.
func TestEmbeddedTable(t *testing.T) {
	tab, err := ParseTable(embedded)
	if err != nil {
		t.Fatalf("releases.json: %v", err)
	}
	if Embedded().Fetched != tab.Fetched || len(Embedded().Minors) != len(tab.Minors) {
		t.Errorf("Embedded() %q with %d minors, want the file's", Embedded().Fetched, len(Embedded().Minors))
	}
	if !strings.Contains(tab.Source, "kubernetes/website") || !strings.Contains(tab.Source, "CC BY 4.0") {
		t.Errorf("source %q: want the upstream files and their licence", tab.Source)
	}
	if _, ok := tab.Lookup(Version{1, 19}); !ok {
		t.Error("the table does not reach back to 1.19")
	}
	if n := tab.Newest(); n.Released == "" || n.MaintenanceFrom == "" {
		t.Errorf("the newest minor %+v: want its release day and maintenance mode", n)
	}
}

func TestParseVersion(t *testing.T) {
	for in, want := range map[string]Version{
		"v1.36.1": {1, 36}, "v1.34.2-gke.1234": {1, 34}, "v1.31.4+k3s1": {1, 31}, "1.36": {1, 36},
		"v1.30.0-eks-abcdef": {1, 30}, " v1.9.11 ": {1, 9}, "v2.0.0": {2, 0}, "1.28+": {1, 28},
	} {
		if got, ok := ParseVersion(in); !ok || got != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "v", "1", "v1.", "v1.x", "va.1", "latest", "v-1.2"} {
		if got, ok := ParseVersion(in); ok {
			t.Errorf("ParseVersion(%q) = %v, want none", in, got)
		}
	}
}

// fixture is a small table: two ended minors, two maintained ones.
func fixture() *Table {
	return &Table{Source: "test", Fetched: "2026-10-18", Minors: []Minor{
		{Minor: "1.32", EndOfLife: "2026-02-28", FinalPatch: "1.32.13"},
		{Minor: "1.33", EndOfLife: "2026-06-28", FinalPatch: "1.33.13"},
		{Minor: "1.34", Released: "2025-08-27", MaintenanceFrom: "2026-08-27", EndOfLife: "2026-10-27"},
		{Minor: "1.35", Released: "2025-12-17", MaintenanceFrom: "2026-12-28", EndOfLife: "2027-02-28"},
	}}
}

func TestSupportOf(t *testing.T) {
	tab := fixture()
	if err := tab.Validate(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		version string
		state   State
		days    int
		text    string
	}{
		{"v1.35.3", Supported, 133, "Upstream support until 28.02.2027 (133 days)"},
		{"v1.34.2-gke.100", Maintenance, 9, "Upstream support until 27.10.2026 (9 days), maintenance mode since 27.08.2026"},
		{"v1.33.13", EndOfLife, -112, "Upstream end of life since 28.06.2026"},
		{"v1.31.0", EndOfLife, -232, "Upstream end of life since before 28.02.2026"},
		// Newer than the table: 1.35's release month + 4 months, its end of life the 28th 14 months on.
		{"v1.36.1", Supported, 253, "Upstream support until about 28.06.2027 (253 days), estimated: this Cogpit's table ends at 1.35"},
		{"v1.38.0", Supported, 498, "Upstream support until about 28.02.2028 (498 days), estimated: this Cogpit's table ends at 1.35"},
		{"v2.0.0", Unknown, 0, "No upstream support dates for this version"},
		{"", Unknown, 0, "No upstream support dates for this version"},
	} {
		s := tab.SupportOf(c.version)
		if got := s.StateAt(now); got != c.state {
			t.Errorf("%s: state %s, want %s (%+v)", c.version, got, c.state, s)
		}
		if got := s.DaysLeft(now); got != c.days {
			t.Errorf("%s: %d days left, want %d", c.version, got, c.days)
		}
		if got := s.Text(now, germanDate); got != c.text {
			t.Errorf("%s: %q\nwant %q", c.version, got, c.text)
		}
		if s.TableNewest != "1.35" || s.TableFetched != "2026-10-18" {
			t.Errorf("%s: the table's newest %q fetched %q", c.version, s.TableNewest, s.TableFetched)
		}
	}
	est := tab.SupportOf("v1.36.0")
	if !est.Estimated || est.Released != "2026-04-01" || est.MaintenanceFrom != "2027-04-28" || est.EndOfLife != "2027-06-28" {
		t.Errorf("the estimate of 1.36: %+v", est)
	}
	if got := est.Short(now, germanDate); got != "until about 28.06.2027 (253 days), estimated: this Cogpit's table ends at 1.35" {
		t.Errorf("the estimate's short form %q", got)
	}
}

// The day counts in the zone of now: the end of life's day itself is still supported, the next
// is not; maintenance mode starts on its day.
func TestStateAtTheEdges(t *testing.T) {
	s := fixture().SupportOf("v1.34.1")
	berlin := time.FixedZone("CEST", 2*3600)
	for _, c := range []struct {
		now   time.Time
		state State
		short string
	}{
		{time.Date(2026, 8, 26, 23, 59, 0, 0, berlin), Supported, "until 27.10.2026 (62 days)"},
		{time.Date(2026, 8, 27, 0, 0, 0, 0, berlin), Maintenance, "until 27.10.2026 (61 days), maintenance mode since 27.08.2026"},
		{time.Date(2026, 10, 26, 0, 30, 0, 0, berlin), Maintenance, "until 27.10.2026 (1 day), maintenance mode since 27.08.2026"},
		{time.Date(2026, 10, 27, 23, 59, 0, 0, berlin), Maintenance, "until 27.10.2026 (today), maintenance mode since 27.08.2026"},
		{time.Date(2026, 10, 28, 0, 0, 0, 0, berlin), EndOfLife, "ended 27.10.2026"},
	} {
		if got := s.StateAt(c.now); got != c.state {
			t.Errorf("%v: %s, want %s", c.now, got, c.state)
		}
		if got := s.Short(c.now, germanDate); got != c.short {
			t.Errorf("%v: %q, want %q", c.now, got, c.short)
		}
	}
	// 22:30 UTC on the 27th is already the 28th in Berlin and still the 27th in New York.
	at := time.Date(2026, 10, 27, 22, 30, 0, 0, time.UTC)
	if got := s.StateAt(at.In(berlin)); got != EndOfLife {
		t.Errorf("Berlin: %s", got)
	}
	if got := s.StateAt(at.In(time.FixedZone("EDT", -4*3600))); got != Maintenance {
		t.Errorf("New York: %s", got)
	}
}

func TestKubeletSkew(t *testing.T) {
	for _, c := range []struct {
		kubelet, server string
		outside         bool
		text            string
	}{
		{"v1.36.1", "v1.36.4", false, ""},
		{"v1.33.0", "v1.36.1", false, ""},
		{"v1.32.9", "v1.36.1", true, "Kubelet 1.32 is 4 minors behind the API server 1.36: the version skew policy allows 3"},
		{"v1.37.0", "v1.36.1", true, "Kubelet 1.37 is newer than the API server 1.36: outside the version skew policy"},
		{"v1.22.4", "v1.24.0", false, ""},
		{"v1.21.4", "v1.24.0", true, "Kubelet 1.21 is 3 minors behind the API server 1.24: the version skew policy allows 2"},
		{"v1.24.4", "v1.27.0", true, "Kubelet 1.24 is 3 minors behind the API server 1.27: the version skew policy allows 2"},
		{"v1.25.4", "v1.28.0", false, ""},
	} {
		k, ok := KubeletSkew(c.kubelet, c.server)
		if !ok || k.Outside() != c.outside || k.Text() != c.text {
			t.Errorf("%s against %s: ok %v outside %v %q; want %v %q", c.kubelet, c.server, ok, k.Outside(), k.Text(), c.outside, c.text)
		}
		if (k.Short() != "") != c.outside {
			t.Errorf("%s against %s: short %q", c.kubelet, c.server, k.Short())
		}
	}
	if k, _ := KubeletSkew("v1.32.9", "v1.36.1"); k.Short() != "4 minors behind the API server 1.36, the policy allows 3" {
		t.Errorf("short %q", k.Short())
	}
	if k, _ := KubeletSkew("v1.37.0", "v1.36.1"); k.Short() != "newer than the API server 1.36" {
		t.Errorf("short %q", k.Short())
	}
	for _, pair := range [][2]string{{"", "v1.36.1"}, {"v1.36.1", "garbage"}, {"v2.0.0", "v1.36.0"}} {
		if _, ok := KubeletSkew(pair[0], pair[1]); ok {
			t.Errorf("%q against %q compared", pair[0], pair[1])
		}
	}
}

func TestValidateRefuses(t *testing.T) {
	for name, change := range map[string]func(*Table){
		"no fetch day":          func(t *Table) { t.Fetched = "" },
		"no minors":             func(t *Table) { t.Minors = nil },
		"a gap":                 func(t *Table) { t.Minors = append(t.Minors[:1], t.Minors[2:]...) },
		"out of order":          func(t *Table) { t.Minors[0], t.Minors[1] = t.Minors[1], t.Minors[0] },
		"not a minor":           func(t *Table) { t.Minors[0].Minor = "1.32.1" },
		"not Kubernetes 1":      func(t *Table) { t.Minors[0].Minor = "2.32" },
		"a bad date":            func(t *Table) { t.Minors[1].EndOfLife = "28.06.2026" },
		"an earlier end":        func(t *Table) { t.Minors[1].EndOfLife = "2026-01-28" },
		"support too short":     func(t *Table) { t.Minors[3].EndOfLife = "2026-10-28" },
		"support too long":      func(t *Table) { t.Minors[3].EndOfLife = "2027-08-28" },
		"maintenance after end": func(t *Table) { t.Minors[3].MaintenanceFrom = "2027-03-28" },
		"maintenance too early": func(t *Table) { t.Minors[3].MaintenanceFrom = "2025-11-28" },
		"newest unreleased":     func(t *Table) { t.Minors[3].Released = "" },
	} {
		tab := fixture()
		change(tab)
		if err := tab.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseTable([]byte("{")); err == nil {
		t.Error("broken JSON accepted")
	}
}

// githubReleased are the days the Kubernetes project published each minor's .0 release on GitHub
// (`gh api repos/kubernetes/kubernetes/releases/tags/v1.<n>.0 --jq .published_at`, read
// 2026-10-18, UTC), for the minors whose release day the table does not hold.
var githubReleased = map[string]string{
	"1.19": "2020-08-26", "1.20": "2020-12-08", "1.21": "2021-04-08", "1.22": "2021-08-04", "1.23": "2021-12-07",
	"1.24": "2022-05-03", "1.25": "2022-08-23", "1.26": "2022-12-09", "1.27": "2023-04-11", "1.28": "2023-08-15",
	"1.29": "2023-12-13", "1.30": "2024-04-17", "1.31": "2024-08-13", "1.32": "2024-12-11", "1.33": "2025-04-23",
	"1.34": "2025-08-27", "1.35": "2025-12-17", "1.36": "2026-04-22", "1.37": "2026-08-26",
}

// The estimate's rule against upstream's own ends of life since 1.19, when the 14-month policy
// began (KEP-1498), as D-91 records it. From a minor's own release month (the rule's end of life,
// the 28th fourteen months on): 13 of 19 exact, five within 18 days (1.27 +18, 1.30 +17, 1.31 +14,
// 1.28 -6, 1.34 -1: upstream's later or earlier), 1.22 41 days later (extended). From a known
// minor's release, one to three minors ahead (the case of a table older than the cluster): 10 of
// 18 exact one ahead; the estimate at most 41 days early (1.22, extended) and at most 31 days late
// (1.25 from 1.24: 1.24 came five months after 1.23, 1.25 three after 1.24), the warning coming
// that much after upstream's end of life; two ahead -41 to +29, three ahead -41 to +12.
func TestEstimateRuleAgainstUpstream(t *testing.T) {
	tab := Embedded()
	released := func(m Minor) time.Time {
		day := m.Released
		if day == "" {
			day = githubReleased[m.Minor]
		}
		d, err := ParseDate(day)
		if err != nil {
			t.Fatalf("%s: no release day", m.Minor)
		}
		return d
	}
	exact := 0
	var minors []Minor
	for n := 19; ; n++ {
		m, ok := tab.Lookup(Version{1, n})
		if !ok {
			break
		}
		minors = append(minors, m)
		rel := released(m)
		rule := time.Date(rel.Year(), rel.Month()+supportMonths, estimateDay, 0, 0, 0, 0, time.UTC)
		eol, _ := ParseDate(m.EndOfLife)
		off := int(eol.Sub(rule).Hours() / 24)
		t.Logf("%s released %s: rule %s, upstream %s (%+d days)", m.Minor, rel.Format(DateLayout), rule.Format(DateLayout), m.EndOfLife, off)
		switch {
		case off == 0:
			exact++
		case m.Minor == "1.22" && off == 41:
		case off < -18 || off > 18:
			t.Errorf("%s: upstream's end of life is %d days off the rule", m.Minor, off)
		}
	}
	if len(minors) < 19 || exact < 13 {
		t.Errorf("%d minors since 1.19, %d of them exact; want at least 19 and 13", len(minors), exact)
	}
	oneAheadExact := 0
	for ahead := 1; ahead <= 3; ahead++ {
		for i := 0; i+ahead < len(minors); i++ {
			known, target := minors[i], minors[i+ahead]
			k := known
			k.Released = released(known).Format(DateLayout)
			_, _, est, _ := EstimateFrom(k, ahead)
			eol, _ := ParseDate(target.EndOfLife)
			off := int(est.Sub(eol).Hours() / 24) // > 0: the estimate is later than upstream
			if ahead == 1 && off == 0 {
				oneAheadExact++
			}
			if off > 31 || off < -41 {
				t.Errorf("%s from %s (%d ahead): estimated %s, upstream %s (%+d days)", target.Minor, known.Minor, ahead,
					est.Format(DateLayout), target.EndOfLife, off)
			}
		}
	}
	if oneAheadExact < 10 {
		t.Errorf("one minor ahead: %d estimates exact, want at least 10", oneAheadExact)
	}
}
