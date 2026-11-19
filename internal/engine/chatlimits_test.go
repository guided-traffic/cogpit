package engine

import (
	"testing"
	"time"
)

// TestChatLimitsDefaults: a zero field takes the default, a set one stays; a ClearTo that is not
// below the ClearAt it meets becomes half of it; the token limit of a turn stays above what a turn
// of every tool call with a full result sums up to, with requests that each carry the whole
// conversation (D-167).
func TestChatLimitsDefaults(t *testing.T) {
	d := ChatLimits{}.WithDefaults()
	if d != DefaultChatLimits {
		t.Fatalf("defaults %+v, want %+v", d, DefaultChatLimits)
	}
	if d.ContextWindow != 300_000 || d.ClearAt != 70 || d.ClearTo != 35 || d.MaxToolResultKiB != 32 {
		t.Errorf("the context's defaults %+v", d)
	}
	// Request i of a turn carries the tools and the system prompt (some 9,000 tokens) and i results.
	result := d.MaxToolResultKiB << 10 / 4
	sum := 0
	for i := 0; i <= d.MaxToolCalls; i++ {
		sum += 9_000 + i*result
	}
	if d.MaxTokens < sum {
		t.Errorf("a turn of %d full tool results sums up to %d tokens, above the limit of %d", d.MaxToolCalls, sum, d.MaxTokens)
	}
	if last := 9_000 + d.MaxToolCalls*result; last > d.ContextWindow {
		t.Errorf("such a turn's last request has %d tokens, more than the window of %d", last, d.ContextWindow)
	}

	set := ChatLimits{RepeatStop: 5, MaxToolCalls: 7, MaxTokens: 9, MaxDuration: time.Minute, ContextWindow: 32_000, ClearAt: 80,
		ClearTo: 50, MaxToolResultKiB: 8}
	if got := set.WithDefaults(); got != set {
		t.Errorf("set limits %+v, want %+v", got, set)
	}
	if got := (ChatLimits{ClearAt: 30}).WithDefaults(); got.ClearAt != 30 || got.ClearTo != 15 {
		t.Errorf("ClearAt 30 with the default ClearTo: %+v", got)
	}
	if got := (ChatLimits{ClearAt: 250}).WithDefaults(); got.ClearAt != 100 {
		t.Errorf("ClearAt above 100: %+v", got)
	}
}

// TestChatLimitsValidate: negative limits, shares above 100 percent and a ClearTo not below its
// ClearAt are refused; the shares each on their own are held against the other's default.
func TestChatLimitsValidate(t *testing.T) {
	for _, ok := range []ChatLimits{{}, DefaultChatLimits, {ClearAt: 100, ClearTo: 99}, {ClearTo: 69}, {ClearAt: 36}, {ClearAt: 20}} {
		if err := ok.Validate(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for _, bad := range []ChatLimits{{MaxTokens: -1}, {ContextWindow: -1}, {ClearAt: -1}, {ClearTo: -1}, {MaxToolResultKiB: -1},
		{ClearAt: 101}, {ClearTo: 101}, {ClearAt: 50, ClearTo: 50}, {ClearAt: 50, ClearTo: 60}, {ClearTo: 70}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}
