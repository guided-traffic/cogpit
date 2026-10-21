package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const placeholder = "COGPIT-DMG-SIGNATURE"

// testKey is a throwaway Ed25519 key and its SUPublicEDKey.
func testKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, base64.StdEncoding.EncodeToString(pub)
}

// signFeed signs a feed as Sparkle's sign_update does: the content, then the signing block.
func signFeed(priv ed25519.PrivateKey, content []byte) []byte {
	sig := ed25519.Sign(priv, content)
	block := fmt.Sprintf("%sedSignature: %s\nlength: %d\n%s\n", signingPrefix, base64.StdEncoding.EncodeToString(sig), len(content), signingSuffix)
	return append(bytes.Clone(content), block...)
}

// fill replaces the draft's placeholder with sig, as the signing job does.
func fill(draft []byte, sig []byte) []byte {
	return bytes.Replace(draft, []byte(placeholder), []byte(base64.StdEncoding.EncodeToString(sig)), 1)
}

func baseOptions(t *testing.T, pub, version string) addOptions {
	t.Helper()
	dir := t.TempDir()
	return addOptions{pubkey: pub, out: filepath.Join(dir, "appcast.xml"), version: version, short: version, min: "13.0",
		url:    "https://github.com/guided-traffic/cogpit/releases/download/v" + version + "/Cogpit-" + version + "-macos-arm64.dmg",
		length: 1234, signature: placeholder, link: "https://github.com/guided-traffic/cogpit/releases/tag/v" + version,
		published: "2026-10-14T12:00:00Z", keep: 10}
}

// The first release starts the feed: one item with the release's versions, the minimum system
// version in three parts, the disk image's URL and length, the placeholder for its signature, the
// release notes up to the pipeline's note, and the links to the release; the draft parses and
// renders the same again.
func TestAddStartsTheFeed(t *testing.T) {
	_, pub := testKey(t)
	o := baseOptions(t, pub, "0.5.0")
	o.feed = filepath.Join(t.TempDir(), "missing.xml")
	o.notes = filepath.Join(t.TempDir(), "notes.md")
	notes := "## [0.5.0](https://example.invalid) (2026-10-14)\n\n### Features\n\n* updates ]]> safely\n\n---\n\n**Downloads.** the pipeline's note\n"
	if err := os.WriteFile(o.notes, []byte(notes), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := add(o)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parse(data)
	if err != nil || len(f.Items) != 1 {
		t.Fatalf("draft %s: %v", data, err)
	}
	it := f.Items[0]
	want := item{Title: "Cogpit 0.5.0", Link: o.link, Version: "0.5.0", ShortVersion: "0.5.0", MinimumSystemVersion: "13.0.0",
		FullReleaseNotesLink: "https://github.com/guided-traffic/cogpit/releases", PubDate: "Wed, 14 Oct 2026 12:00:00 +0000",
		Description: description{Format: "markdown", Text: "## [0.5.0](https://example.invalid) (2026-10-14)\n\n### Features\n\n* updates ]]> safely\n\n[The release on GitHub](" + o.link + ")"},
		Enclosure:   enclosure{URL: o.url, Length: 1234, Type: "application/octet-stream", EdSignature: placeholder}}
	if it != want {
		t.Errorf("item\n got %+v\nwant %+v", it, want)
	}
	if bytes.Count(data, []byte(placeholder)) != 1 || bytes.Contains(data, []byte("Downloads.")) {
		t.Errorf("draft %s", data)
	}
	again, err := render(f.Items)
	if err != nil || !bytes.Equal(again, data) {
		t.Errorf("rendered again differs (%v):\n%s\n%s", err, again, data)
	}
}

// add builds on a feed signed with the app's key only; an unsigned, altered or foreign-signed
// feed is refused, so its history is never signed again.
func TestAddRefusesAnUnsignedHistory(t *testing.T) {
	priv, pub := testKey(t)
	other, _ := testKey(t)
	first := baseOptions(t, pub, "0.5.0")
	draft, err := add(first)
	if err != nil {
		t.Fatal(err)
	}
	content := fill(draft, ed25519.Sign(priv, []byte("dmg")))
	for name, data := range map[string][]byte{
		"unsigned":       content,
		"other key":      signFeed(other, content),
		"altered":        bytes.Replace(signFeed(priv, content), []byte("Cogpit 0.5.0"), []byte("Cogpit 0.5.1"), 1),
		"appended after": append(signFeed(priv, content), "<rss/>"...),
	} {
		t.Run(name, func(t *testing.T) {
			o := baseOptions(t, pub, "0.6.0")
			o.feed = filepath.Join(t.TempDir(), "appcast.xml")
			if err := os.WriteFile(o.feed, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := add(o); err == nil || !strings.Contains(err.Error(), "refusing") {
				t.Errorf("built on it: %v", err)
			}
		})
	}
}

// A signed history keeps its items: a release of the same version replaces its item, the items
// are newest first by their versions' numbers (0.10.0 after 0.9.0), and each channel keeps its
// newest.
func TestAddMergesTheHistory(t *testing.T) {
	priv, pub := testKey(t)
	var feed []byte
	publish := func(version, channel string, keep int) {
		t.Helper()
		o := baseOptions(t, pub, version)
		o.channel, o.keep = channel, keep
		if feed != nil {
			o.feed = filepath.Join(t.TempDir(), "appcast.xml")
			if err := os.WriteFile(o.feed, feed, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		draft, err := add(o)
		if err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		feed = signFeed(priv, fill(draft, ed25519.Sign(priv, []byte(version))))
	}
	for _, v := range []string{"0.8.0", "0.10.0", "0.9.0", "0.9.0"} {
		publish(v, "", 10)
	}
	publish("0.11.0", "beta", 10)
	publish("0.10.1", "", 3)
	content, err := verifySigned(feed, mustKey(t, pub))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := parse(content)
	var got []string
	for _, it := range f.Items {
		got = append(got, it.Version+"/"+channelName(it.Channel))
	}
	if want := "0.11.0/beta 0.10.1/stable 0.10.0/stable 0.9.0/stable"; strings.Join(got, " ") != want {
		t.Errorf("items %v, want %s", got, want)
	}
}

func mustKey(t *testing.T, s string) ed25519.PublicKey {
	t.Helper()
	k, err := publicKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// verify accepts the signed feed whose release item names the disk image by its length and a
// signature over its bytes, and refuses each way it can be wrong.
func TestVerify(t *testing.T) {
	priv, pub := testKey(t)
	dir := t.TempDir()
	dmg := filepath.Join(dir, "Cogpit-0.5.0-macos-arm64.dmg")
	image := []byte("a disk image's bytes")
	if err := os.WriteFile(dmg, image, 0o600); err != nil {
		t.Fatal(err)
	}
	o := baseOptions(t, pub, "0.5.0")
	o.length = int64(len(image))
	draft, err := add(o)
	if err != nil {
		t.Fatal(err)
	}
	write := func(data []byte) string {
		p := filepath.Join(t.TempDir(), "appcast.xml")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write(signFeed(priv, fill(draft, ed25519.Sign(priv, image))))
	if items, _, err := verify(verifyOptions{feed: good, pubkey: pub, dmg: dmg, version: "0.5.0"}); err != nil || len(items) != 1 {
		t.Fatalf("a good feed: %v", err)
	}
	_, otherPub := testKey(t)
	for name, o := range map[string]verifyOptions{
		"another key":         {feed: good, pubkey: otherPub, dmg: dmg, version: "0.5.0"},
		"no item":             {feed: good, pubkey: pub, dmg: dmg, version: "0.6.0"},
		"draft unsigned":      {feed: write(draft), pubkey: pub},
		"image not signed":    {feed: write(signFeed(priv, fill(draft, ed25519.Sign(priv, []byte("other"))))), pubkey: pub, dmg: dmg, version: "0.5.0"},
		"placeholder signed":  {feed: write(signFeed(priv, draft)), pubkey: pub},
		"another disk image":  {feed: good, pubkey: pub, dmg: filepath.Join(dir, "x.dmg"), version: "0.5.0"},
		"public key not key":  {feed: good, pubkey: "bm90IGEga2V5"},
		"signed length wrong": {feed: write(bytes.Replace(signFeed(priv, fill(draft, ed25519.Sign(priv, image))), []byte("length: "), []byte("length: 1"), 1)), pubkey: pub},
	} {
		if _, _, err := verify(o); err == nil {
			t.Errorf("%s: verified", name)
		}
	}
}

// Long release notes end at a line boundary with a link to the whole notes; none at all give the
// link alone.
func TestReleaseNotesBounded(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.md")
	long := strings.Repeat("* a change of a release with many commits\n", 2000)
	if err := os.WriteFile(p, []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}
	notes, err := releaseNotes(p, "https://example.invalid/r")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) > maxNotes+200 || !strings.HasSuffix(notes, "…\n\n[The whole release notes on GitHub](https://example.invalid/r)") {
		t.Errorf("%d bytes, ending %q", len(notes), notes[len(notes)-120:])
	}
	if none, _ := releaseNotes("", "https://example.invalid/r"); none != "[The release on GitHub](https://example.invalid/r)" {
		t.Errorf("no notes: %q", none)
	}
}

// signedFeed builds a feed of the versions (in that order, each added on the one before) signed
// with priv, its items' disk images signed too.
func signedFeed(t *testing.T, priv ed25519.PrivateKey, pub, channel string, versions ...string) []byte {
	t.Helper()
	var feed []byte
	for _, v := range versions {
		o := baseOptions(t, pub, v)
		o.channel = channel
		if feed != nil {
			o.feed = filepath.Join(t.TempDir(), "current.xml")
			if err := os.WriteFile(o.feed, feed, 0o600); err != nil {
				t.Fatal(err)
			}
		} else {
			o.feed = filepath.Join(t.TempDir(), "missing.xml")
		}
		draft, err := add(o)
		if err != nil {
			t.Fatal(err)
		}
		feed = signFeed(priv, fill(draft, ed25519.Sign(priv, []byte(v))))
	}
	return feed
}

// verify with the served feed refuses a feed that would take back a newer release Pages offers,
// the way v0.15.0's appcast, approved after v0.16.0's, replaced the feed with 0.15.0 alone on
// 2026-10-16; the newer release wins over an older item its draft never saw, the way v0.19.0's
// draft ran before v0.18.0's appcast was deployed (2026-10-16), and that item is reported; an item
// older than every item its channel keeps goes silently, and a re-run replaces its own item.
func TestVerifyKeepsTheServedFeed(t *testing.T) {
	priv, pub := testKey(t)
	write := func(data []byte) string {
		p := filepath.Join(t.TempDir(), "appcast.xml")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	otherPriv, _ := testKey(t)
	for _, tt := range []struct {
		name       string
		live, next []byte
		ok         bool
		gaps       string
	}{
		{"a release after the served ones", signedFeed(t, priv, pub, "", "0.15.0", "0.16.0"), signedFeed(t, priv, pub, "", "0.15.0", "0.16.0", "0.17.0"), true, ""},
		{"a re-run of the newest", signedFeed(t, priv, pub, "", "0.15.0", "0.16.0"), signedFeed(t, priv, pub, "", "0.15.0", "0.16.0"), true, ""},
		{"the oldest left out by the channel's keep", signedFeed(t, priv, pub, "", "0.15.0", "0.16.0"), signedFeed(t, priv, pub, "", "0.16.0", "0.17.0"), true, ""},
		{"a newer release drafted before an older one was deployed", signedFeed(t, priv, pub, "", "0.17.0", "0.18.0"), signedFeed(t, priv, pub, "", "0.17.0", "0.19.0"), true, "0.18.0 (stable)"},
		{"a newer release in another channel's gap", signedFeed(t, priv, pub, "beta", "0.16.0", "0.17.0", "0.18.0"), signedFeed(t, priv, pub, "beta", "0.16.0", "0.17.0", "0.19.0"), true, "0.18.0 (beta)"},
		{"an older release approved after a newer one", signedFeed(t, priv, pub, "", "0.16.0"), signedFeed(t, priv, pub, "", "0.15.0"), false, ""},
		{"an older release drafted on an older feed", signedFeed(t, priv, pub, "", "0.17.0", "0.18.0", "0.19.0"), signedFeed(t, priv, pub, "", "0.17.0", "0.18.0"), false, ""},
		{"a channel dropped", signedFeed(t, priv, pub, "beta", "0.16.0"), signedFeed(t, priv, pub, "", "0.17.0"), false, ""},
		{"a served feed another key signed", signedFeed(t, otherPriv, pub, "", "0.16.0"), signedFeed(t, priv, pub, "", "0.17.0"), false, ""},
	} {
		_, gaps, err := verify(verifyOptions{feed: write(tt.next), pubkey: pub, live: tt.live})
		if (err == nil) != tt.ok {
			t.Errorf("%s: %v", tt.name, err)
		}
		if got := strings.Join(gaps, ", "); got != tt.gaps {
			t.Errorf("%s: left out %q, want %q", tt.name, got, tt.gaps)
		}
		if !tt.ok && err != nil && strings.Contains(tt.name, "approved after") && !strings.Contains(err.Error(), "would take back 0.16.0 (stable)") {
			t.Errorf("%s: the error does not name the newer item: %v", tt.name, err)
		}
	}
}

// fetchLive reads the served feed past Pages' cache, and treats a 404 as no feed yet.
func TestFetchLive(t *testing.T) {
	var query string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("cogpit-run")
		switch r.URL.Path {
		case "/appcast.xml":
			_, _ = w.Write([]byte("the feed"))
		case "/broken.xml":
			w.WriteHeader(http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("GITHUB_RUN_ID", "42")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	if b, err := fetchLive(srv.Client(), srv.URL+"/appcast.xml"); err != nil || string(b) != "the feed" || query != "42-2" {
		t.Errorf("served: %q, %v, query %q", b, err, query)
	}
	if b, err := fetchLive(srv.Client(), srv.URL+"/missing.xml"); err != nil || b != nil {
		t.Errorf("not served yet: %q, %v", b, err)
	}
	if _, err := fetchLive(srv.Client(), srv.URL+"/broken.xml"); err == nil {
		t.Error("a 502 read as a feed")
	}
	if _, err := fetchLive(srv.Client(), "http://example.invalid/appcast.xml"); err == nil {
		t.Error("an http URL fetched")
	}
}

// The Windows app's feed (D-111): its items name the setup with sparkle:os and the arguments
// WinSparkle runs it with, link the release's page for the notes instead of embedding Markdown,
// say so in the feed's description, and verify with the setup's signature; the next release builds
// on it, and a feed of the other system's items is refused.
func TestAddWindowsFeed(t *testing.T) {
	priv, pub := testKey(t)
	windows := func(version string) addOptions {
		o := baseOptions(t, pub, version)
		o.min, o.system, o.installerArgs = "10.0.17763", "windows-x64", "/S /relaunch"
		o.url = "https://github.com/guided-traffic/cogpit/releases/download/v" + version + "/Cogpit-" + version + "-windows-x64-setup.exe"
		return o
	}
	first := windows("0.36.0")
	draft, err := add(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`<description>Cogpit's releases for the Windows app's updater (WinSparkle).</description>`,
		`<sparkle:releaseNotesLink>` + first.link + `</sparkle:releaseNotesLink>`,
		`sparkle:os="windows-x64" sparkle:installerArguments="/S /relaunch" sparkle:edSignature="` + placeholder + `"`,
		`<sparkle:minimumSystemVersion>10.0.17763</sparkle:minimumSystemVersion>`,
	} {
		if !bytes.Contains(draft, []byte(s)) {
			t.Errorf("the draft has no %s:\n%s", s, draft)
		}
	}
	if bytes.Contains(draft, []byte("<description sparkle:format")) || bytes.Contains(draft, []byte("fullReleaseNotesLink")) {
		t.Errorf("a Windows item embeds notes:\n%s", draft)
	}

	dir := t.TempDir()
	setup := filepath.Join(dir, "Cogpit-0.36.0-windows-x64-setup.exe")
	if err := os.WriteFile(setup, []byte("a setup's bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	first.length = int64(len("a setup's bytes"))
	if draft, err = add(first); err != nil {
		t.Fatal(err)
	}
	feed := filepath.Join(dir, "appcast-windows.xml")
	if err := os.WriteFile(feed, signFeed(priv, fill(draft, ed25519.Sign(priv, []byte("a setup's bytes")))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verify(verifyOptions{feed: feed, pubkey: pub, dmg: setup, version: "0.36.0"}); err != nil {
		t.Errorf("verify: %v", err)
	}

	next := windows("0.37.0")
	next.feed = feed
	if _, err := add(next); err != nil {
		t.Errorf("the next release on the Windows feed: %v", err)
	}
	mac := baseOptions(t, pub, "0.37.0")
	mac.feed = feed
	if _, err := add(mac); err == nil || !strings.Contains(err.Error(), "one feed for each system") {
		t.Errorf("a macOS item on the Windows feed: %v", err)
	}
	noArgs := windows("0.37.0")
	noArgs.installerArgs = ""
	if _, err := add(noArgs); err == nil {
		t.Error("a Windows item without installer arguments: no error")
	}
}
