//go:build !nodemo

package main

import "testing"

// --demo-events 0 is a static demo cluster, not the default rate the options' zero stands for;
// the flag's default stays 1,000 events per second.
func TestDemoEventsZeroIsAStaticCluster(t *testing.T) {
	if o := demoOptions(500, 0); !o.NoChurn {
		t.Errorf("--demo-events 0: %+v, want NoChurn", o)
	}
	for _, rate := range []float64{1000, 0.5, -1} {
		if o := demoOptions(500, rate); o.NoChurn || o.EventsPerSecond != rate {
			t.Errorf("--demo-events %v: %+v, want the rate and churn", rate, o)
		}
	}
}
