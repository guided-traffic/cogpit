package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// signFeed signs a feed as Sparkle's sign_update does: the signing block after the content.
func signFeed(content string, priv ed25519.PrivateKey) []byte {
	sig := ed25519.Sign(priv, []byte(content))
	return fmt.Appendf(nil, "%s<!-- sparkle-signatures:\nedSignature: %s\nlength: %d\n-->\n",
		content, base64.StdEncoding.EncodeToString(sig), len(content))
}

func testFeed(items ...string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
    <channel>
        <title>Cogpit</title>
` + strings.Join(items, "\n") + `
    </channel>
</rss>
`
}

func testItem(version, os, channel string) string {
	ch := ""
	if channel != "" {
		ch = "<sparkle:channel>" + channel + "</sparkle:channel>"
	}
	return `        <item>
            <title>Cogpit ` + version + `</title>
            <sparkle:version>` + version + `</sparkle:version>` + ch + `
            <enclosure url="https://example.invalid/Cogpit-` + version + `-setup.exe" length="1" type="application/octet-stream" sparkle:os="` + os + `" sparkle:edSignature="x"/>
        </item>`
}

// The newest release for this system wins, whatever the items' order; other systems, beta
// channels, the running release and older ones, and the skipped one do not count.
func TestNewestRelease(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	feed := signFeed(testFeed(
		testItem("0.36.0", "windows-x64", ""),
		testItem("0.38.0", "windows-arm64", ""),
		testItem("0.39.0", "windows-x64", "beta"),
		testItem("0.37.0", "windows", ""),
		testItem("0.35.0", "windows-x64", ""),
	), priv)
	for _, c := range []struct {
		system, running, skip string
		want                  string
		ok                    bool
	}{
		{"windows-x64", "0.35.0", "", "0.37.0", true},
		{"windows-x64", "0.35.0", "0.37.0", "0.36.0", true},
		{"windows-arm64", "0.35.0", "", "0.38.0", true},
		{"windows-x64", "0.37.0", "", "", false},
		{"windows-x64", "0.40.0", "", "", false},
		{"macos", "0.1.0", "", "", false},
	} {
		got, ok, err := newestRelease(feed, pub, c.system, c.running, c.skip)
		if err != nil || got != c.want || ok != c.ok {
			t.Errorf("newestRelease(%s, running %s, skip %q) = %q, %v, %v; want %q, %v", c.system, c.running, c.skip, got, ok, err, c.want, c.ok)
		}
	}
}

// A feed the key does not verify offers nothing: unsigned, signed with another key, altered after
// signing, cut short, or with data after its signing block.
func TestNewestReleaseRefusesAFeedTheKeyDoesNotVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	content := testFeed(testItem("0.36.0", "windows-x64", ""))
	signed := signFeed(content, priv)
	for name, data := range map[string][]byte{
		"unsigned":        []byte(content),
		"another key":     signFeed(content, other),
		"altered":         []byte(strings.Replace(string(signed), "0.36.0", "0.99.0", 1)),
		"no block end":    []byte(strings.TrimSuffix(string(signed), "-->\n")),
		"data after":      append(append([]byte{}, signed...), "<x/>"...),
		"too large":       append([]byte(strings.Repeat(" ", maxFeedSize)), signed...),
		"length mismatch": []byte(strings.Replace(string(signed), fmt.Sprintf("length: %d", len(content)), "length: 1", 1)),
	} {
		if v, ok, err := newestRelease(data, pub, "windows-x64", "0.35.0", ""); err == nil || ok {
			t.Errorf("%s: %q, %v, %v; want an error", name, v, ok, err)
		}
	}
	if _, _, err := newestRelease(signed, nil, "windows-x64", "0.35.0", ""); err == nil {
		t.Error("no key: no error")
	}
}

// make dist-windows writes the feed and the key into the app through the linker, which ignores a -X
// for a symbol that does not exist: the Makefile must name the variables winsparkle_windows.go
// declares, else a release has no updater and nothing says so.
func TestMakefileBuildsTheFeedIntoTheApp(t *testing.T) {
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("winsparkle_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^UPDATE_PKG\s*:=\s*github\.com/guided-traffic/cogpit/internal/update\s*$`).Match(mk) {
		t.Error("the Makefile's UPDATE_PKG is not this package")
	}
	for _, v := range []string{"windowsFeedURL", "edPublicKey"} {
		if !strings.Contains(string(mk), "-X $(UPDATE_PKG)."+v+"=") {
			t.Errorf("the Makefile's WIN_APP_LDFLAGS set no %s", v)
		}
		if !regexp.MustCompile(`(?m)^\s+` + v + `\s+string$`).Match(src) {
			t.Errorf("winsparkle_windows.go declares no string variable %s", v)
		}
	}
}

func TestDecodeKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	if got, err := decodeKey(" " + base64.StdEncoding.EncodeToString(pub) + "\n"); err != nil || !got.Equal(pub) {
		t.Errorf("decodeKey: %v", err)
	}
	for _, s := range []string{"", "not base64!", base64.StdEncoding.EncodeToString(pub[:16])} {
		if _, err := decodeKey(s); !errors.Is(err, ErrNoKey) {
			t.Errorf("decodeKey(%q) = %v, want ErrNoKey", s, err)
		}
	}
}
