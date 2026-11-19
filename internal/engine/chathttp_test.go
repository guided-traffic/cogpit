package engine

import "testing"

// TestChatPlainHTTPRemote: plain http to another machine is warned about (the owner's decision,
// H-95); localhost and every loopback address are exempt, https never warned about.
func TestChatPlainHTTPRemote(t *testing.T) {
	for base, want := range map[string]bool{
		"http://192.168.1.20:1234/v1":     true,
		"http://litellm.internal:4000":    true,
		"HTTP://LLM.example.com":          true,
		"http://[fd00::3]:1234/v1":        true,
		"http://localhost.example.com/v1": true,
		"http://192.168.1.21":             true,
		"http://localhost:1234/v1":        false,
		"http://LOCALHOST.:1234/v1":       false,
		"http://127.0.0.1:1234/v1":        false,
		"http://127.0.0.2:1234/v1":        false,
		"http://[::1]:1234/v1":            false,
		"https://192.168.1.20:1234/v1":    false,
		"https://llm.example.com":         false,
		"":                                false,
		"localhost:1234":                  false,
		"http://":                         false,
	} {
		if got := ChatPlainHTTPRemote(base); got != want {
			t.Errorf("ChatPlainHTTPRemote(%q) = %v, want %v", base, got, want)
		}
	}
}
