package update

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The Windows updater's own reading of its feed (winsparkle_windows.go, D-111). WinSparkle shows
// its window as soon as a check of its own finds a release, and tells its host neither that nor
// which, so the Update button (F7), which macOS gets from Sparkle's gentle reminders, comes from
// Cogpit reading the feed itself: the newest release the feed offers for this system and
// architecture, newer than the running one. The feed is a Sparkle 2 signed feed, signed with the
// update key whose public half the release carries; one that the key does not verify offers
// nothing. Only the button comes from here: what installs is WinSparkle's, which checks the
// setup's own EdDSA signature before it runs it. Platform-neutral, so that every system's tests
// cover it.

// Sparkle's signing block of a signed feed, as hack/appcast writes and checks it.
const (
	feedSigningPrefix = "<!-- sparkle-signatures:\n"
	feedSigningSuffix = "-->"
)

// maxFeedSize bounds a feed Cogpit reads: Cogpit's keeps its ten newest releases, a few KiB each.
const maxFeedSize = 1 << 20

type feedDoc struct {
	XMLName xml.Name   `xml:"rss"`
	Items   []feedItem `xml:"channel>item"`
}

type feedItem struct {
	Version   string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version"`
	Channel   string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle channel"`
	Enclosure struct {
		URL     string `xml:"url,attr"`
		OS      string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle os,attr"`
		Version string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version,attr"`
	} `xml:"enclosure"`
}

// newestRelease returns the newest version the signed feed offers for system (sparkle:os, such as
// windows-x64; an enclosure for any Windows, "windows", counts too) on the default channel, newer
// than running and not skip (the version the user skipped in WinSparkle's window), and ok when
// there is one. A feed that is not signed with pub, or does not parse, is an error.
func newestRelease(data []byte, pub ed25519.PublicKey, system, running, skip string) (version string, ok bool, err error) {
	if len(data) > maxFeedSize {
		return "", false, fmt.Errorf("the update feed is larger than %d bytes", maxFeedSize)
	}
	content, err := verifyFeed(data, pub)
	if err != nil {
		return "", false, err
	}
	var doc feedDoc
	if err := xml.Unmarshal(content, &doc); err != nil {
		return "", false, fmt.Errorf("the update feed: %w", err)
	}
	family, _, _ := strings.Cut(system, "-")
	for _, it := range doc.Items {
		v := it.Version
		if v == "" {
			v = it.Enclosure.Version
		}
		if v == "" || it.Channel != "" || it.Enclosure.URL == "" {
			continue // a beta channel's, or not an item Sparkle would offer
		}
		if os := it.Enclosure.OS; os != system && os != family {
			continue
		}
		if compareVersions(v, running) <= 0 || v == skip {
			continue
		}
		if !ok || compareVersions(v, version) > 0 {
			version, ok = v, true
		}
	}
	return version, ok, nil
}

// verifyFeed checks a signed feed's signing block with pub and returns the signed content: an
// Ed25519 signature over every byte before the block, whose length it names (Sparkle 2.9's signed
// feeds; hack/appcast's verifySigned, which checks the feed before it is deployed, reads the same).
func verifyFeed(data []byte, pub ed25519.PublicKey) ([]byte, error) {
	i := bytes.LastIndex(data, []byte(feedSigningPrefix))
	if i < 0 {
		return nil, errors.New("the update feed is not signed")
	}
	content, rest := data[:i], data[i+len(feedSigningPrefix):]
	end := bytes.Index(rest, []byte(feedSigningSuffix))
	if end < 0 {
		return nil, errors.New("the update feed's signing block does not end")
	}
	var sig []byte
	length := int64(-1)
	for line := range strings.SplitSeq(string(rest[:end]), "\n") {
		k, v, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(k) {
		case "edSignature":
			sig, _ = base64.StdEncoding.DecodeString(strings.TrimSpace(v))
		case "length":
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				length = n
			}
		}
	}
	switch {
	case len(pub) != ed25519.PublicKeySize:
		return nil, errors.New("no update key")
	case len(sig) != ed25519.SignatureSize:
		return nil, errors.New("the update feed carries no Ed25519 signature")
	case length != int64(len(content)):
		return nil, errors.New("the update feed's signed length is not its length")
	case !ed25519.Verify(pub, content, sig):
		return nil, errors.New("the update feed's signature does not verify with the update key")
	case len(bytes.TrimSpace(rest[end+len(feedSigningSuffix):])) > 0:
		return nil, errors.New("the update feed has data after its signing block")
	}
	return content, nil
}

// decodeKey reads an update key's public half as Sparkle's generate_keys prints it: the base64 of
// its 32 bytes.
func decodeKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, ErrNoKey
	}
	return ed25519.PublicKey(b), nil
}
