package cluster

import (
	"runtime"
	"testing"
	"time"
)

func TestChildProcessRuns(t *testing.T) {
	cmd := []string{"/opt/homebrew/bin/pinniped", "login", "oidc", "--issuer=x"}
	for _, tt := range []struct {
		name string
		c    childProcess
		want bool
	}{
		{"as started", childProcess{argv: []string{"/opt/homebrew/bin/pinniped", "login", "oidc", "--issuer=x"}}, true},
		{"a bare name", childProcess{argv: []string{"pinniped", "login", "oidc", "--issuer=x"}}, true},
		{"a script", childProcess{argv: []string{"/bin/sh", "/opt/homebrew/bin/pinniped", "login", "oidc", "--issuer=x"}}, true},
		{"other arguments", childProcess{argv: []string{"/opt/homebrew/bin/pinniped", "login", "oidc", "--issuer=y"}}, false},
		{"another program", childProcess{argv: []string{"/usr/bin/kubelogin", "login", "oidc", "--issuer=x"}}, false},
		{"the same name elsewhere", childProcess{argv: []string{"/usr/local/bin/pinniped", "login", "oidc", "--issuer=x"}}, false},
		{"a script elsewhere", childProcess{argv: []string{"/bin/sh", "/tmp/pinniped", "login", "oidc", "--issuer=x"}}, false},
		{"too short", childProcess{argv: []string{"login", "oidc", "--issuer=x"}}, false},
		{"Windows", childProcess{exe: "PINNIPED.EXE"}, true},
		{"Windows, another", childProcess{exe: "kubelogin.exe"}, false},
	} {
		if runtime.GOOS == "windows" && tt.c.argv != nil {
			continue // Windows tells no command line (pluginproc_windows.go), and these paths are Unix ones
		}
		if got := tt.c.runs(cmd); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
	if (childProcess{argv: []string{"x"}}).runs(nil) {
		t.Error("no command matched a process")
	}
}

func TestLoginEndedErrorText(t *testing.T) {
	for _, tt := range []struct {
		e    *LoginEndedError
		want string
	}{
		{&LoginEndedError{Program: "pinniped", Bound: 5 * time.Minute, Cause: ErrLoginTimedOut}, "the login through pinniped did not finish within 5 minutes"},
		{&LoginEndedError{Program: "pinniped", Bound: time.Minute, Cause: ErrLoginTimedOut}, "the login through pinniped did not finish within 1 minute"},
		{&LoginEndedError{Program: "pinniped", Bound: 1500 * time.Millisecond, Cause: ErrLoginTimedOut}, "the login through pinniped did not finish within 1.5s"},
		{&LoginEndedError{Program: "kubelogin", Bound: 5 * time.Minute, Cause: ErrLoginCancelled}, "login cancelled: Cogpit ended kubelogin"},
		// WF38: Cogpit says it ended the plugin only when it found and signalled its process.
		{&LoginEndedError{Program: "walk-login.sh", Bound: 5 * time.Minute, Cause: ErrLoginCancelled, RunsOn: true},
			"login cancelled: Cogpit stopped waiting for walk-login.sh; its process was not found and may run on"},
		{&LoginEndedError{Program: "pinniped", Bound: 5 * time.Minute, Cause: ErrLoginTimedOut, RunsOn: true},
			"the login through pinniped did not finish within 5 minutes; Cogpit stopped waiting for it; its process was not found and may run on"},
		{&LoginEndedError{Program: "pinniped", Bound: 5 * time.Minute, Cause: ErrLoginCancelled, RunsOn: true, why: whyNotAllowed},
			"login cancelled: Cogpit stopped waiting for pinniped; its process could not be signalled and may run on"},
	} {
		if got := tt.e.Error(); got != tt.want {
			t.Errorf("%q, want %q", got, tt.want)
		}
	}
}

// The run's process is the one new child seen running the plugin's command line, else the one new
// child in Cogpit's process group, each only when no other run began alongside that could have
// started it (WF38).
func TestDecideProcess(t *testing.T) {
	plugin, other, shell := procID{pid: 10, start: 1}, procID{pid: 11, start: 2}, procID{pid: 12, start: 3}
	for _, tt := range []struct {
		name       string
		seen       map[procID]*sighting
		near, same bool
		want       procID
	}{
		{"by its command line", map[procID]*sighting{plugin: {group: true, matched: true}, other: {group: true}}, false, false, plugin},
		{"by its command line, another run alongside", map[procID]*sighting{plugin: {group: true, matched: true}}, true, false, plugin},
		{"by its command line, the same command line alongside", map[procID]*sighting{plugin: {group: true, matched: true}}, true, true, procID{}},
		{"two of the same command line", map[procID]*sighting{plugin: {matched: true}, other: {matched: true}}, false, false, procID{}},
		{"a wrapper after its exec", map[procID]*sighting{plugin: {group: true}, shell: {}}, false, false, plugin},
		{"a wrapper after its exec, another run alongside", map[procID]*sighting{plugin: {group: true}}, true, false, procID{}},
		{"two new children in the group", map[procID]*sighting{plugin: {group: true}, other: {group: true}}, false, false, procID{}},
		{"only a terminal's shell", map[procID]*sighting{shell: {}}, false, false, procID{}},
		{"nothing new", map[procID]*sighting{}, false, false, procID{}},
	} {
		if got := decideProcess(tt.seen, tt.near, tt.same); got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
}
