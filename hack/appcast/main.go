// Command appcast keeps Sparkle's appcast, the update feed of the macOS app
// (docs/developer/releasing.md#the-appcast, D-4): the release pipeline drafts the feed with the new
// release's item, Sparkle's sign_update signs the disk image and the feed in the job that holds
// the update key, and this command checks the signed feed before it is deployed to GitHub Pages.
//
//	go run ./hack/appcast add -feed current.xml -pubkey <SUPublicEDKey> -out appcast.xml \
//	    -version 0.5.0 -short 0.5.0 -min 13.0 -url <the disk image's release asset> -length <bytes> \
//	    -signature <placeholder> -notes notes.md -link <the release's page> -published <RFC 3339> \
//	    [-channel beta] [-keep 10]
//	go run ./hack/appcast verify -feed appcast.xml -pubkey <SUPublicEDKey> [-dmg <disk image> -version 0.5.0] \
//	    [-live <the feed's URL>]
//
// add builds on the feed as GitHub Pages serves it (-feed; a missing file starts a new one), but
// only on a feed signed with the app's update key: an altered history would otherwise be signed
// along with the new release. The new item replaces one of the same version, the items are sorted
// newest first, and each channel keeps its -keep newest. The draft is unsigned and carries
// -signature in place of the disk image's EdDSA signature, which only the signing job can
// compute.
//
// verify checks what is about to be deployed: the feed's signature (Sparkle 2.9's signed feeds:
// an Ed25519 signature over every byte before the trailing "<!-- sparkle-signatures:" block, whose
// length it names) with the app's public key, every item, and with -dmg that the release's item
// names that disk image by its length and an EdDSA signature over its bytes the key verifies.
// With -live it also fetches the feed Pages serves now (past its cache) and refuses a feed that
// would take back a release Pages offers: each draft builds on the feed served when the draft ran,
// so a release whose appcast is approved after a newer one's would otherwise deploy its older feed
// over the newer one (2026-10-16). It refuses when a channel's newest served item is newer than the
// new feed's newest of that channel, or the channel is gone. The newer release wins: an older
// served item the new feed lacks only because its draft ran before that item was deployed (v0.19.0's
// draft before v0.18.0's deploy, 2026-10-16) is reported and left out, since Sparkle offers each
// copy the newest item it may install; an item older than every item of its channel in the new
// feed went with the channel's -keep newest and is not reported.
//
// The Windows app's feed (D-111) is the same format at another URL, read by WinSparkle: add with
// -os windows-x64 (sparkle:os on the enclosure) and -installer-args (sparkle:installerArguments,
// how WinSparkle runs the setup), its enclosure a setup (.exe), its release notes a link to the
// release's page (sparkle:releaseNotesLink: WinSparkle shows HTML, not Sparkle 2's Markdown);
// verify takes the setup with -file. A feed holds one system's items only.
//
// Standard library only: it runs in the release pipeline beside no dependency's code.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// sparkleNS is the namespace of Sparkle's appcast elements.
const sparkleNS = "http://www.andymatuschak.org/xml-namespaces/sparkle"

// Sparkle's signing block of a signed feed (SPUExtractSignedFeed.m, Signing.swift).
const (
	signingPrefix = "<!-- sparkle-signatures:\n"
	signingSuffix = "-->"
)

// maxNotes bounds the release notes embedded in an item; longer notes end with a link to the
// release's page.
const maxNotes = 32 << 10

// bundleVersion is CFBundleVersion's format (one to three period-separated integers), which
// Sparkle compares part by part.
var bundleVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)

// systemVersion is sparkle:minimumSystemVersion's format, major.minor.patch.
var systemVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type feed struct {
	XMLName xml.Name `xml:"rss"`
	Items   []item   `xml:"channel>item"`
}

type item struct {
	Title                string      `xml:"title"`
	Link                 string      `xml:"link"`
	Version              string      `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version"`
	ShortVersion         string      `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle shortVersionString"`
	MinimumSystemVersion string      `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle minimumSystemVersion"`
	Channel              string      `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle channel"`
	FullReleaseNotesLink string      `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle fullReleaseNotesLink"`
	ReleaseNotesLink     string      `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle releaseNotesLink"`
	PubDate              string      `xml:"pubDate"`
	Description          description `xml:"description"`
	Enclosure            enclosure   `xml:"enclosure"`
}

type description struct {
	Format string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle format,attr"`
	Text   string `xml:",chardata"`
}

type enclosure struct {
	URL                string `xml:"url,attr"`
	Length             int64  `xml:"length,attr"`
	Type               string `xml:"type,attr"`
	EdSignature        string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle edSignature,attr"`
	OS                 string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle os,attr"`
	InstallerArguments string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle installerArguments,attr"`
}

// windows reports whether the item is the Windows app's (sparkle:os windows, windows-x64, …).
func (it item) windows() bool { return strings.HasPrefix(it.Enclosure.OS, "windows") }

func main() {
	if len(os.Args) < 2 {
		fail(errors.New("usage: appcast add|verify [flags]"))
	}
	var err error
	switch os.Args[1] {
	case "add":
		err = runAdd(os.Args[2:], os.Stdout)
	case "verify":
		err = runVerify(os.Args[2:], os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q: add or verify", os.Args[1])
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	if os.Getenv("GITHUB_ACTIONS") != "" {
		fmt.Fprintf(os.Stderr, "::error::appcast: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "appcast: %v\n", err)
	}
	os.Exit(1)
}

// addOptions are add's flags.
type addOptions struct {
	feed, pubkey, out, version, short, min, url, signature, notes, link, published, channel string
	system, installerArgs                                                                   string
	length                                                                                  int64
	keep                                                                                    int
}

func runAdd(args []string, stdout io.Writer) error {
	var o addOptions
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.StringVar(&o.feed, "feed", "", "the feed as Pages serves it; a missing file starts a new feed")
	fs.StringVar(&o.pubkey, "pubkey", "", "the app's SUPublicEDKey (base64): the feed must be signed with its key")
	fs.StringVar(&o.out, "out", "", "write the draft here")
	fs.StringVar(&o.version, "version", "", "the app's CFBundleVersion")
	fs.StringVar(&o.short, "short", "", "the app's CFBundleShortVersionString")
	fs.StringVar(&o.min, "min", "", "the app's LSMinimumSystemVersion")
	fs.StringVar(&o.url, "url", "", "the disk image's download URL (the release's asset)")
	fs.Int64Var(&o.length, "length", 0, "the disk image's size in bytes")
	fs.StringVar(&o.signature, "signature", "", "the placeholder the signing job replaces with the disk image's EdDSA signature")
	fs.StringVar(&o.notes, "notes", "", "the release notes (Markdown); the part from a line --- on is left out")
	fs.StringVar(&o.link, "link", "", "the release's page")
	fs.StringVar(&o.published, "published", "", "when the release was published (RFC 3339)")
	fs.StringVar(&o.channel, "channel", "", "empty (the default channel, Stable) or beta")
	fs.IntVar(&o.keep, "keep", 10, "the items each channel keeps")
	fs.StringVar(&o.system, "os", "", "empty for the macOS app's feed; windows-x64 or windows-arm64 (sparkle:os) for the Windows app's")
	fs.StringVar(&o.installerArgs, "installer-args", "", "with -os windows-…: how WinSparkle runs the setup (sparkle:installerArguments)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	data, err := add(o)
	if err != nil {
		return err
	}
	if err := os.WriteFile(o.out, data, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "appcast: %s with %s (%s), drafted in %s\n", o.short, o.version, channelName(o.channel), o.out)
	return nil
}

func channelName(ch string) string {
	if ch == "" {
		return "stable"
	}
	return ch
}

// add returns the draft: the history of o.feed with o's item.
func add(o addOptions) ([]byte, error) {
	pub, err := publicKey(o.pubkey)
	if err != nil {
		return nil, err
	}
	if o.out == "" || o.signature == "" || o.link == "" {
		return nil, errors.New("-out, -signature and -link are required")
	}
	var history []item
	switch data, err := os.ReadFile(o.feed); {
	case o.feed == "" || errors.Is(err, os.ErrNotExist):
		// The first release with an update key starts the feed.
	case err != nil:
		return nil, err
	default:
		content, err := verifySigned(data, pub)
		if err != nil {
			return nil, fmt.Errorf("the feed %s: %w; refusing to sign a history the update key did not sign", o.feed, err)
		}
		f, err := parse(content)
		if err != nil {
			return nil, fmt.Errorf("the feed %s: %w", o.feed, err)
		}
		history = f.Items
	}
	published, err := time.Parse(time.RFC3339, o.published)
	if err != nil {
		return nil, fmt.Errorf("-published %q: %w", o.published, err)
	}
	notes, err := releaseNotes(o.notes, o.link)
	if err != nil {
		return nil, err
	}
	next := item{
		Title: "Cogpit " + o.short, Link: o.link, Version: o.version, ShortVersion: o.short,
		MinimumSystemVersion: threePart(o.min), Channel: o.channel,
		FullReleaseNotesLink: releasesPage(o.link), PubDate: published.UTC().Format(time.RFC1123Z),
		Description: description{Format: "markdown", Text: notes},
		Enclosure: enclosure{URL: o.url, Length: o.length, Type: "application/octet-stream", EdSignature: o.signature,
			OS: o.system, InstallerArguments: o.installerArgs},
	}
	if next.windows() {
		// WinSparkle shows a description as HTML and knows no Markdown: the release's page instead.
		next.Description, next.FullReleaseNotesLink, next.ReleaseNotesLink = description{}, "", o.link
	}
	for _, it := range history {
		if it.Enclosure.OS != next.Enclosure.OS {
			return nil, fmt.Errorf("the feed %s holds items for %q, not %q: one feed for each system", o.feed, it.Enclosure.OS, next.Enclosure.OS)
		}
	}
	items := merge(history, next, o.keep)
	if err := check(items, o.signature); err != nil {
		return nil, err
	}
	return render(items)
}

// releasesPage is the releases' list beside a release's page (…/releases/tag/v1 → …/releases).
func releasesPage(link string) string {
	if i := strings.Index(link, "/releases/tag/"); i >= 0 {
		return link[:i] + "/releases"
	}
	return link
}

// threePart writes a system version with three parts, as Sparkle's documentation asks (13.0 →
// 13.0.0).
func threePart(v string) string {
	parts := strings.Split(v, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".")
}

// releaseNotes reads the release's Markdown notes up to a line "---" (the release pipeline
// appends its download note after one), bounded by maxNotes, and links the release's page.
func releaseNotes(file, link string) (string, error) {
	var text string
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		text = strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	lines := strings.Split(text, "\n")
	if i := slices.IndexFunc(lines, func(l string) bool { return strings.TrimSpace(l) == "---" }); i >= 0 {
		lines = lines[:i]
	}
	text = strings.TrimSpace(strings.Join(lines, "\n"))
	more := "[The release on GitHub](" + link + ")"
	if len(text) > maxNotes {
		cut := strings.LastIndex(text[:maxNotes], "\n")
		if cut < 0 {
			cut = maxNotes
		}
		text = strings.TrimSpace(text[:cut]) + "\n\n…"
		more = "[The whole release notes on GitHub](" + link + ")"
	}
	if text == "" {
		return more, nil
	}
	return text + "\n\n" + more, nil
}

// merge puts next into the history in place of an item of its version, newest first, and keeps
// each channel's keep newest items.
func merge(history []item, next item, keep int) []item {
	items := slices.DeleteFunc(slices.Clone(history), func(it item) bool { return it.Version == next.Version })
	items = append(items, next)
	slices.SortStableFunc(items, func(a, b item) int { return compareVersions(b.Version, a.Version) })
	kept := map[string]int{}
	return slices.DeleteFunc(items, func(it item) bool {
		kept[it.Channel]++
		return keep > 0 && kept[it.Channel] > keep
	})
}

// compareVersions compares dotted versions part by part, numerically where both parts are
// numbers, as Sparkle's standard comparator does for these.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil && nx != ny:
			return nx - ny
		case (ex != nil || ey != nil) && x != y:
			return strings.Compare(x, y)
		}
	}
	return 0
}

// check refuses a feed Sparkle would not offer as Cogpit means it: every item a version in
// CFBundleVersion's format, once; a short version; a three-part minimum system version; an https
// download with its length and an EdSignature of 64 bytes (placeholder: the draft's one item
// without it yet); the stable or beta channel; an RFC 1123 date.
func check(items []item, placeholder string) error {
	seen := map[string]bool{}
	drafts := 0
	for _, it := range items {
		what := "item " + it.Version
		switch {
		case !bundleVersion.MatchString(it.Version):
			return fmt.Errorf("%s: sparkle:version is not one to three period-separated integers", what)
		case seen[it.Version]:
			return fmt.Errorf("%s: twice in the feed", what)
		case it.ShortVersion == "":
			return fmt.Errorf("%s: no sparkle:shortVersionString", what)
		case !systemVersion.MatchString(it.MinimumSystemVersion):
			return fmt.Errorf("%s: sparkle:minimumSystemVersion %q is not major.minor.patch", what, it.MinimumSystemVersion)
		case it.Channel != "" && it.Channel != "beta":
			return fmt.Errorf("%s: channel %q", what, it.Channel)
		case it.Enclosure.Length <= 0:
			return fmt.Errorf("%s: the enclosure has no length", what)
		}
		seen[it.Version] = true
		suffix, kind := ".dmg", "a disk image"
		switch {
		case it.windows():
			suffix, kind = ".exe", "a setup"
			if it.Enclosure.InstallerArguments == "" || it.ReleaseNotesLink == "" {
				return fmt.Errorf("%s: a Windows item needs sparkle:installerArguments and sparkle:releaseNotesLink", what)
			}
		case it.Enclosure.OS != "":
			return fmt.Errorf("%s: sparkle:os %q", what, it.Enclosure.OS)
		}
		if u, err := url.Parse(it.Enclosure.URL); err != nil || u.Scheme != "https" || u.Host == "" || !strings.HasSuffix(u.Path, suffix) {
			return fmt.Errorf("%s: the enclosure %q is not an https URL of %s", what, it.Enclosure.URL, kind)
		}
		if _, err := time.Parse(time.RFC1123Z, it.PubDate); err != nil {
			return fmt.Errorf("%s: pubDate %q: %w", what, it.PubDate, err)
		}
		if placeholder != "" && it.Enclosure.EdSignature == placeholder {
			drafts++
			continue
		}
		if sig, err := base64.StdEncoding.DecodeString(it.Enclosure.EdSignature); err != nil || len(sig) != ed25519.SignatureSize {
			return fmt.Errorf("%s: sparkle:edSignature is not an Ed25519 signature", what)
		}
	}
	if placeholder != "" && drafts != 1 {
		return fmt.Errorf("the draft holds %d items without their signature, want 1", drafts)
	}
	return nil
}

// publicKey decodes SUPublicEDKey: the base64 of an Ed25519 public key.
func publicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("-pubkey is not an Ed25519 public key in base64 (SUPublicEDKey)")
	}
	return ed25519.PublicKey(b), nil
}

// verifySigned checks a signed feed's signing block with pub and returns the signed content.
func verifySigned(data []byte, pub ed25519.PublicKey) ([]byte, error) {
	i := bytes.LastIndex(data, []byte(signingPrefix))
	if i < 0 {
		return nil, errors.New("not signed: no sparkle-signatures block")
	}
	content, rest := data[:i], data[i+len(signingPrefix):]
	end := bytes.Index(rest, []byte(signingSuffix))
	if end < 0 {
		return nil, errors.New("the sparkle-signatures block does not end")
	}
	var sig []byte
	length := int64(-1)
	for _, line := range strings.Split(string(rest[:end]), "\n") {
		k, v, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(k) {
		case "edSignature":
			b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
			if err != nil {
				return nil, errors.New("the feed's edSignature is not base64")
			}
			sig = b
		case "length":
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return nil, errors.New("the feed's signed length is not a number")
			}
			length = n
		}
	}
	switch {
	case len(sig) != ed25519.SignatureSize:
		return nil, errors.New("the feed carries no Ed25519 signature")
	case length != int64(len(content)):
		return nil, fmt.Errorf("the feed's signed length is %d, its content %d bytes", length, len(content))
	case !ed25519.Verify(pub, content, sig):
		return nil, errors.New("the feed's signature does not verify with the app's update key")
	}
	if tail := bytes.TrimSpace(rest[end+len(signingSuffix):]); len(tail) > 0 {
		return nil, errors.New("the feed has data after its signing block")
	}
	return content, nil
}

func parse(data []byte) (feed, error) {
	var f feed
	d := xml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&f); err != nil {
		return feed{}, err
	}
	return f, nil
}

var feedTemplate = template.Must(template.New("feed").Funcs(template.FuncMap{"x": escape, "cdata": cdata}).Parse(
	`<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="` + sparkleNS + `">
    <channel>
        <title>Cogpit</title>
        <link>https://github.com/guided-traffic/cogpit</link>
        <description>{{.About}}</description>
        <language>en</language>
{{- range .Items}}
        <item>
            <title>{{x .Title}}</title>
            <link>{{x .Link}}</link>
            <sparkle:version>{{x .Version}}</sparkle:version>
            <sparkle:shortVersionString>{{x .ShortVersion}}</sparkle:shortVersionString>
            <sparkle:minimumSystemVersion>{{x .MinimumSystemVersion}}</sparkle:minimumSystemVersion>
{{- if .Channel}}
            <sparkle:channel>{{x .Channel}}</sparkle:channel>
{{- end}}
{{- if .FullReleaseNotesLink}}
            <sparkle:fullReleaseNotesLink>{{x .FullReleaseNotesLink}}</sparkle:fullReleaseNotesLink>
{{- end}}
{{- if .ReleaseNotesLink}}
            <sparkle:releaseNotesLink>{{x .ReleaseNotesLink}}</sparkle:releaseNotesLink>
{{- end}}
            <pubDate>{{x .PubDate}}</pubDate>
{{- if .Description.Text}}
            <description{{if .Description.Format}} sparkle:format="{{x .Description.Format}}"{{end}}>{{cdata .Description.Text}}</description>
{{- end}}
            <enclosure url="{{x .Enclosure.URL}}" length="{{.Enclosure.Length}}" type="{{x .Enclosure.Type}}"
{{- if .Enclosure.OS}} sparkle:os="{{x .Enclosure.OS}}"{{end}}
{{- if .Enclosure.InstallerArguments}} sparkle:installerArguments="{{x .Enclosure.InstallerArguments}}"{{end}} sparkle:edSignature="{{x .Enclosure.EdSignature}}"/>
        </item>
{{- end}}
    </channel>
</rss>
`))

// render writes the feed, items in their order.
func render(items []item) ([]byte, error) {
	about := "Cogpit's releases for the macOS app's updater (Sparkle 2)."
	if len(items) > 0 && items[0].windows() {
		about = "Cogpit's releases for the Windows app's updater (WinSparkle)."
	}
	var buf bytes.Buffer
	if err := feedTemplate.Execute(&buf, struct {
		About string
		Items []item
	}{about, items}); err != nil {
		return nil, err
	}
	if _, err := parse(buf.Bytes()); err != nil { // well-formed, or a bug here
		return nil, fmt.Errorf("the rendered feed: %w", err)
	}
	return buf.Bytes(), nil
}

func escape(s string) (string, error) {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return "", err
	}
	return b.String(), nil
}

// cdata wraps text in CDATA sections; a "]]>" in it is split across two.
func cdata(s string) string {
	return "<![CDATA[" + strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>") + "]]>"
}

// verifyOptions are verify's flags; live is the served feed's content (nil: none served).
type verifyOptions struct {
	feed, pubkey, dmg, version string
	live                       []byte
}

func runVerify(args []string, stdout io.Writer) error {
	var o verifyOptions
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.StringVar(&o.feed, "feed", "", "the signed feed")
	fs.StringVar(&o.pubkey, "pubkey", "", "the app's SUPublicEDKey (base64)")
	fs.StringVar(&o.dmg, "dmg", "", "the release's disk image, which the item of -version must name")
	fs.StringVar(&o.dmg, "file", "", "the release's download (the disk image, or the Windows setup), which the item of -version must name")
	fs.StringVar(&o.version, "version", "", "the release's CFBundleVersion")
	liveURL := fs.String("live", "", "the feed's URL on Pages: refuse to drop an item it serves")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *liveURL != "" {
		live, err := fetchLive(http.DefaultClient, *liveURL)
		if err != nil {
			return err
		}
		if live == nil {
			fmt.Fprintf(stdout, "appcast: no feed on Pages yet (%s): nothing to keep\n", *liveURL)
		}
		o.live = live
	}
	items, gaps, err := verify(o)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "appcast: %s verifies with the update key, %d items:\n", o.feed, len(items))
	for _, it := range items {
		fmt.Fprintf(stdout, "  %s (%s, %s) %s, %d bytes\n", it.ShortVersion, it.Version, channelName(it.Channel), it.Enclosure.URL, it.Enclosure.Length)
	}
	if len(gaps) > 0 {
		warn(stdout, fmt.Sprintf("the feed leaves out %s, which Pages serves now: its draft ran before that release's appcast was "+
			"deployed; the newer release is deployed, and copies are offered it. Re-run this release's \"Appcast (draft)\" "+
			"to keep the left-out items in the feed's history", strings.Join(gaps, ", ")))
	}
	return nil
}

// warn writes a warning, as a GitHub Actions annotation in a workflow.
func warn(w io.Writer, msg string) {
	if os.Getenv("GITHUB_ACTIONS") != "" {
		fmt.Fprintf(w, "::warning::appcast: %s\n", msg)
	} else {
		fmt.Fprintf(w, "appcast: warning: %s\n", msg)
	}
}

// verify returns the feed's items and, with o.live, the served items it leaves out below its
// channels' newest (keepsLive).
func verify(o verifyOptions) (items []item, gaps []string, err error) {
	pub, err := publicKey(o.pubkey)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(o.feed)
	if err != nil {
		return nil, nil, err
	}
	content, err := verifySigned(data, pub)
	if err != nil {
		return nil, nil, err
	}
	f, err := parse(content)
	if err != nil {
		return nil, nil, err
	}
	if len(f.Items) == 0 {
		return nil, nil, errors.New("the feed has no items")
	}
	if err := check(f.Items, ""); err != nil {
		return nil, nil, err
	}
	if o.live != nil {
		if gaps, err = keepsLive(o.live, f.Items, pub); err != nil {
			return nil, nil, err
		}
	}
	if o.dmg == "" {
		return f.Items, gaps, nil
	}
	i := slices.IndexFunc(f.Items, func(it item) bool { return it.Version == o.version })
	if i < 0 {
		return nil, nil, fmt.Errorf("the feed has no item of version %q", o.version)
	}
	it := f.Items[i]
	dmg, err := os.ReadFile(o.dmg)
	if err != nil {
		return nil, nil, err
	}
	sig, _ := base64.StdEncoding.DecodeString(it.Enclosure.EdSignature)
	switch u, _ := url.Parse(it.Enclosure.URL); {
	case path.Base(u.Path) != filepath.Base(o.dmg):
		return nil, nil, fmt.Errorf("item %s names %s, not %s", it.Version, it.Enclosure.URL, filepath.Base(o.dmg))
	case it.Enclosure.Length != int64(len(dmg)):
		return nil, nil, fmt.Errorf("item %s says %d bytes, %s has %d", it.Version, it.Enclosure.Length, filepath.Base(o.dmg), len(dmg))
	case !ed25519.Verify(pub, dmg, sig):
		return nil, nil, fmt.Errorf("item %s: the signature of %s does not verify with the app's update key", it.Version, filepath.Base(o.dmg))
	}
	return f.Items, gaps, nil
}

// fetchLive reads the feed Pages serves now, past its cache (ten minutes) with a query of the run,
// as the draft does; nil when none is served yet (404).
func fetchLive(c *http.Client, feedURL string) ([]byte, error) {
	u, err := url.Parse(feedURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("-live %q is not an https URL", feedURL)
	}
	q := u.Query()
	q.Set("cogpit-run", os.Getenv("GITHUB_RUN_ID")+"-"+os.Getenv("GITHUB_RUN_ATTEMPT"))
	u.RawQuery = q.Encode()
	resp, err := c.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("fetching the feed on Pages: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	case http.StatusNotFound:
		return nil, nil
	}
	return nil, fmt.Errorf("fetching the feed on Pages: HTTP %d", resp.StatusCode)
}

// keepsLive refuses a feed that would take back a release the served one (live) offers: a channel
// whose newest served item is newer than the feed's newest of that channel, or a channel the feed
// lacks. It returns the served items the feed leaves out below its channels' newest, each newer
// than the oldest item its channel keeps in the feed (older ones went with the channel's keep).
// The served feed must verify with the same key, as the draft requires of the feed it builds on.
func keepsLive(live []byte, next []item, pub ed25519.PublicKey) ([]string, error) {
	content, err := verifySigned(live, pub)
	if err != nil {
		return nil, fmt.Errorf("the feed on Pages: %w", err)
	}
	served, err := parse(content)
	if err != nil {
		return nil, fmt.Errorf("the feed on Pages: %w", err)
	}
	oldest, newest := map[string]string{}, map[string]string{}
	has := map[string]bool{}
	for _, it := range next {
		has[it.Version] = true
		if o, ok := oldest[it.Channel]; !ok || compareVersions(it.Version, o) < 0 {
			oldest[it.Channel] = it.Version
		}
		if n, ok := newest[it.Channel]; !ok || compareVersions(it.Version, n) > 0 {
			newest[it.Channel] = it.Version
		}
	}
	var withdrawn, gaps []string
	for _, it := range served.Items {
		what := it.ShortVersion + " (" + channelName(it.Channel) + ")"
		n, ok := newest[it.Channel]
		switch {
		case has[it.Version]:
		case !ok || compareVersions(it.Version, n) > 0:
			withdrawn = append(withdrawn, what)
		case compareVersions(it.Version, oldest[it.Channel]) > 0:
			gaps = append(gaps, what)
		}
	}
	if len(withdrawn) > 0 {
		return nil, fmt.Errorf("the feed would take back %s, which Pages serves now and which is newer than this "+
			"release: its draft ran before that release's appcast was deployed; re-run this release's \"Appcast (draft)\", "+
			"which builds on the feed Pages serves now and adds this release to its history", strings.Join(withdrawn, ", "))
	}
	return gaps, nil
}
