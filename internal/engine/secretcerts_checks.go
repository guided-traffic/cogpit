package engine

import (
	"crypto/x509"
	"encoding/base64"
	"runtime"
	"strconv"
	"sync"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The checks of a Secret's certificates (D-30, the owner's answer): whether the private key a
// certificate key pairs with belongs to its first certificate, and whether a chain is in order. The
// private key is decoded from the cached Secret, compared inside model.DecodedValue.MatchPrivateKey
// and zeroed; the outcome is yes, no or a fixed reason, never the key, so no section, result,
// error, log line or audit line can carry it.

// Texts of the checks section.
const (
	certChecksTitle = "Checks"
	keyMatchItem    = "Key Matches"
	chainItem       = "Chain In Order"
	anchorItem      = "Chain Leads To"
)

var (
	systemRootsOnce sync.Once
	systemRootsPool *x509.CertPool
)

// thisComputer names the computer in the anchor's text.
func thisComputer() string {
	if runtime.GOOS == "darwin" {
		return "this Mac"
	}
	return "this computer"
}

// systemRoots is the computer's trust store, read once (nil where it cannot be read: the anchor
// then never says "trusted on this computer").
func systemRoots() *x509.CertPool {
	systemRootsOnce.Do(func() { systemRootsPool, _ = x509.SystemCertPool() })
	return systemRootsPool
}

// certificateChecks sets, on each key with certificates, the private key it pairs with and
// whether that key matches (pairedKey), and whether its chain is in order. The chain is checked
// for a value that pairs with a private key or whose first certificate is no CA (a leaf followed
// by its chain); a CA bundle, several roots one after the other, is not a chain (Cogpit's
// default).
func certificateChecks(keys []model.CertificateKey, decoded map[string]model.DecodedValue, data map[string]any) {
	for i := range keys {
		ck := &keys[i]
		d, ok := decoded[ck.Key]
		if !ok {
			continue
		}
		pk := pairedKey(ck.Key, data, len(decoded))
		if pk == "" && combinedPEM(data[ck.Key]) { // the key in the same value (A99)
			pk = ck.Key
		}
		if pk != "" {
			if m, compared := matchPrivateKey(d, data[pk]); compared {
				ck.PairedKey, ck.KeyMatches = pk, &m
			}
		}
		leaf := len(ck.Certificates) > 0 && !ck.Certificates[0].Invalid && !ck.Certificates[0].IsCA
		if ck.PairedKey != "" || leaf {
			if a, ok := d.Anchor(systemRoots()); ok { // R1: a chain's anchor, not a CA bundle's
				ck.Anchor = &a
			}
			if inOrder, checked, broken := d.ChainInOrder(); checked {
				ck.ChainInOrder, ck.ChainBreak = &inOrder, broken
			}
		}
	}
}

// combinedPEM reports whether a Secret's value holds a private key block besides its
// certificates (A99, owner): the key check then compares the value's own key.
func combinedPEM(v any) bool {
	raw, ok := decodeValue(v)
	defer wipeBytes(raw)
	return ok && model.HoldsPrivateKeyBlock(raw)
}

// matchPrivateKey decodes a Secret's value that holds a private key, compares it with the first
// certificate of d and zeroes it. compared is false when d has no first certificate.
func matchPrivateKey(d model.DecodedValue, v any) (model.KeyMatch, bool) {
	if !d.HasLeaf() {
		return model.KeyMatch{}, false
	}
	s, _ := v.(string)
	if base64.StdEncoding.DecodedLen(len(s)) > model.MaxPrivateKeyBytes {
		return model.KeyMatch{Reason: model.KeyTooLarge}, true
	}
	raw, ok := decodeValue(v)
	defer wipeBytes(raw)
	if !ok {
		return model.KeyMatch{Reason: model.KeyNotReadable}, true
	}
	return d.MatchPrivateKey(raw)
}

// pairedKey returns the Secret's key that holds the private key of the certificates under k,
// where the pairing is unambiguous (Cogpit's default): for a key named <base>.crt, .pem, .cer or
// .der (tls.crt's base is tls), the one of <base>.key and <base>-key.pem present when exactly one
// is; otherwise, when the Secret has exactly one key with certificates (certKeys) and exactly one
// key named like a private key, that one. "" when nothing pairs.
func pairedKey(k string, data map[string]any, certKeys int) string {
	if base, ok := certificateBase(k); ok {
		var found []string
		for _, cand := range []string{base + ".key", base + "-key.pem"} {
			if _, ok := data[cand]; ok {
				found = append(found, cand)
			}
		}
		switch len(found) {
		case 1:
			return found[0]
		case 2:
			return ""
		}
	}
	if certKeys != 1 {
		return ""
	}
	var keys []string
	for name := range data {
		if privateKeyName(name) {
			keys = append(keys, name)
		}
	}
	if len(keys) != 1 {
		return ""
	}
	return keys[0]
}

// certChecksSection is the "Checks" section of a key's group: whether the paired private key
// matches the first certificate (No is an error, a key that cannot be read unknown) and whether
// the chain is in order. ok is false when nothing was checked.
func certChecksSection(ck model.CertificateKey, group string) (model.DetailSection, bool) {
	var items []model.DetailItem
	if m := ck.KeyMatches; m != nil {
		it := model.DetailItem{Name: keyMatchItem}
		where := ck.PairedKey
		if where == ck.Key {
			where = "in this value" // a combined PEM (A99)
		}
		switch {
		case m.Reason == model.KeyEncrypted: // A100: no TLS server can read it as it stands
			it.Value, it.Status = "Encrypted key: TLS and Ingress cannot use it ("+where+")", model.StatusWarning
		case m.Reason != "":
			it.Value, it.Status = "Key not readable ("+where+": "+m.Reason+")", model.StatusUnknown
		case m.Matches:
			it.Value, it.Status = "Yes ("+where+")", model.StatusOK
		default:
			it.Value, it.Status = "No ("+where+" holds another key)", model.StatusError
		}
		items = append(items, it)
	}
	if a := ck.Anchor; a != nil {
		it := model.DetailItem{Name: anchorItem}
		switch a.Kind {
		case model.AnchorPublic:
			it.Value, it.Status = "A public CA ("+a.Root+")", model.StatusOK
		case model.AnchorSystem:
			it.Value, it.Status = "A CA "+thisComputer()+" trusts, not a public one ("+a.Root+")", model.StatusOK
		default:
			it.Value = "An internal or self-signed CA (" + a.Root + ")"
		}
		items = append(items, it)
	}
	if ck.ChainInOrder != nil {
		it := model.DetailItem{Name: chainItem, Value: "Yes, each certificate is signed by the next", Status: model.StatusOK}
		if !*ck.ChainInOrder {
			it.Value = "No, certificate " + strconv.Itoa(ck.ChainBreak) + " is not signed by certificate " + strconv.Itoa(ck.ChainBreak+1)
			it.Status = model.StatusError
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return model.DetailSection{}, false
	}
	return model.DetailSection{Title: certChecksTitle, Group: group, Items: items}, true
}
