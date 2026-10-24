package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// delayRunEnds makes the goroutine that notes a plugin run's end run late by d, as a slow runner's
// scheduler may, for the rest of the test.
func delayRunEnds(t *testing.T, d time.Duration) {
	t.Helper()
	gate := func() { time.Sleep(d) }
	runEndGate.Store(&gate)
	t.Cleanup(func() { runEndGate.Store(nil) })
}

// A login that answered before the goroutine noting its end ran (open-decisions § 6, the lead's note
// of 2026-10-21): the connect's trace says it answered, with its time, and its login stage passed,
// not "still runs" and every stage after it not reached. The first request's answer means
// client-go let its lock go, so the connect waits for that goroutine, briefly.
func TestALoginThatAnsweredIsNotStillRunning(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newCountingPlugin(t, srv)
	p.give(secretToken, false)
	delayRunEnds(t, 300*time.Millisecond)

	var steps []ConnectStep
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{p.kconf}, ConnectTimeout: 5 * time.Second,
		Trace: func(s ConnectStep) { steps = append(steps, s) }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var login *ConnectStep
	var stages []Stage
	for i, s := range steps {
		if s.Name == StepLogin && s.Took > 0 {
			login = &steps[i]
		}
		if s.Name == StepStages {
			stages = s.Stages
		}
	}
	if login == nil || login.Failed || login.Text != "Login: login-plugin answered" {
		t.Errorf("the login's step %+v", login)
	}
	for _, st := range stages {
		if st.Result != StageOK && st.Result != StageSkipped {
			t.Errorf("stage %s: %s %q", st.Name, st.Result, st.Text)
		}
	}
}
