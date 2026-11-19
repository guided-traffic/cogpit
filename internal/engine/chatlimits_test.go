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

// TestLocalConnectionsHaveLongerTurns (D-169): a turn over a local model, an LM Studio or a base URL
// on this machine or a private network, has 60 minutes by default and any other 10; a limit the
// user set stays; nothing else of the defaults differs.
func TestLocalConnectionsHaveLongerTurns(t *testing.T) {
	for _, c := range []struct {
		kind  ChatConnectionKind
		url   string
		local bool
	}{
		{ChatLMStudio, "http://localhost:1234/v1", true},
		{ChatLMStudio, "https://studio.example.com/v1", true},
		{ChatOpenAI, "http://localhost:11434/v1", true},
		{ChatOpenAI, "http://127.0.0.1:8080/v1", true},
		{ChatOpenAI, "http://[::1]:8080/v1", true},
		{ChatOpenAI, "http://192.168.1.20:1234/v1", true},
		{ChatLiteLLM, "http://10.0.3.7:4000", true},
		{ChatOpenWebUI, "http://172.16.0.5:3000", true},
		{ChatOpenAI, "http://studio.local:1234/v1", true},
		{ChatOpenAI, "http://169.254.10.1/v1", true},
		{ChatOpenAI, "https://api.openai.com/v1", false},
		{ChatLiteLLM, "https://litellm.example.com", false},
		{ChatOpenAI, "http://8.8.8.8/v1", false},
		{ChatOpenAI, "http://172.32.0.1/v1", false},
		{ChatOpenAI, "http://notlocal.localhost.example.com/v1", false},
		{ChatOpenAI, "", false},
		{ChatOpenAI, "://", false},
	} {
		conn := ChatConnection{Kind: c.kind, BaseURL: c.url}
		want := DefaultChatLimits
		if c.local {
			want.MaxDuration = LocalChatMaxDuration
		}
		if conn.Local() != c.local || conn.DefaultLimits() != want || conn.EffectiveLimits() != want {
			t.Errorf("%s %q: local %v, limits %+v; want local %v, %+v", c.kind, c.url, conn.Local(), conn.EffectiveLimits(), c.local, want)
		}
	}
	if LocalChatMaxDuration != 60*time.Minute || DefaultChatLimits.MaxDuration != 10*time.Minute {
		t.Errorf("the defaults: %s local, %s else", LocalChatMaxDuration, DefaultChatLimits.MaxDuration)
	}
	set := ChatConnection{Kind: ChatLMStudio, BaseURL: "http://localhost:1234/v1", Limits: ChatLimits{MaxDuration: 3 * time.Minute, MaxToolCalls: 7}}
	if got := set.EffectiveLimits(); got.MaxDuration != 3*time.Minute || got.MaxToolCalls != 7 || got.MaxTokens != DefaultChatLimits.MaxTokens {
		t.Errorf("a set limit: %+v", got)
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
