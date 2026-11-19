package kubectl

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"

	"github.com/guided-traffic/cogpit/internal/model"
)

// DefaultRekor is sigstore's public transparency log, Rekor v1, where cosign records the
// Kubernetes release's signatures (kubectl 1.37.1's for darwin/arm64 is entry 2924600063). The
// trusted root names it with its key.
const DefaultRekor = "https://rekor.sigstore.dev"

// Bounds of the log's lookup (Cogpit's defaults): the answer for kubectl 1.37.1 was 5,263 bytes,
// one entry.
const (
	maxRekorAnswer = 64 << 10
	maxLogEntries  = 4
	rekorTimeout   = 30 * time.Second
	maxRekorReason = 200
)

// ErrLogUnreachable marks a lookup that got no answer from sigstore's transparency log: the
// signature could not be checked, so nothing is downloaded.
var ErrLogUnreachable = errors.New("sigstore's transparency log could not be asked")

// LogEntry is one entry of Rekor v1's log as its API returns it (its LogEntryAnon): the
// canonicalized body, where and when the log recorded it, and the log's proofs.
type LogEntry struct {
	Body           []byte          `json:"body"` // base64 in the API's JSON
	IntegratedTime int64           `json:"integratedTime"`
	LogID          string          `json:"logID"` // hex: the SHA-256 of the log's public key
	LogIndex       int64           `json:"logIndex"`
	Verification   LogVerification `json:"verification"`
}

// LogVerification is an entry's proofs: the log's signature over the entry and the time it
// recorded it (the signed entry timestamp), and the entry's inclusion in the log's Merkle tree up to
// a checkpoint the log signed.
type LogVerification struct {
	SignedEntryTimestamp []byte          `json:"signedEntryTimestamp"` // base64 in the API's JSON
	InclusionProof       *InclusionProof `json:"inclusionProof"`
}

// InclusionProof is the path from an entry to a tree head, and the checkpoint (a signed note) that
// signs the tree head.
type InclusionProof struct {
	Checkpoint string   `json:"checkpoint"`
	Hashes     []string `json:"hashes"` // hex
	LogIndex   int64    `json:"logIndex"`
	RootHash   string   `json:"rootHash"` // hex
	TreeSize   int64    `json:"treeSize"`
}

// proto is the entry as a sigstore bundle carries it.
func (e *LogEntry) proto() (*protorekor.TransparencyLogEntry, error) {
	logID, err := hex.DecodeString(e.LogID)
	if err != nil || len(logID) == 0 {
		return nil, errors.New("its log ID is not hex")
	}
	var kind struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := json.Unmarshal(e.Body, &kind); err != nil || kind.Kind == "" || kind.APIVersion == "" {
		return nil, errors.New("its body is not a log entry")
	}
	tle := &protorekor.TransparencyLogEntry{
		LogIndex:          e.LogIndex,
		LogId:             &protocommon.LogId{KeyId: logID},
		KindVersion:       &protorekor.KindVersion{Kind: kind.Kind, Version: kind.APIVersion},
		IntegratedTime:    e.IntegratedTime,
		CanonicalizedBody: e.Body,
	}
	if len(e.Verification.SignedEntryTimestamp) > 0 {
		tle.InclusionPromise = &protorekor.InclusionPromise{SignedEntryTimestamp: e.Verification.SignedEntryTimestamp}
	}
	if p := e.Verification.InclusionProof; p != nil {
		root, err := hex.DecodeString(p.RootHash)
		if err != nil {
			return nil, errors.New("its inclusion proof's root hash is not hex")
		}
		hashes := make([][]byte, len(p.Hashes))
		for i, h := range p.Hashes {
			if hashes[i], err = hex.DecodeString(h); err != nil {
				return nil, errors.New("its inclusion proof's hashes are not hex")
			}
		}
		tle.InclusionProof = &protorekor.InclusionProof{
			LogIndex: p.LogIndex, RootHash: root, TreeSize: p.TreeSize, Hashes: hashes,
			Checkpoint: &protorekor.Checkpoint{Envelope: p.Checkpoint},
		}
	}
	return tle, nil
}

// hashedRekord is the entry cosign uploads for a signed blob, as the log's search takes it.
type hashedRekord struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Spec       struct {
		Data struct {
			Hash struct {
				Algorithm string `json:"algorithm"`
				Value     string `json:"value"`
			} `json:"hash"`
		} `json:"data"`
		Signature struct {
			Content   []byte `json:"content"` // base64 of the ASN.1 signature
			PublicKey struct {
				Content []byte `json:"content"` // base64 of the PEM certificate
			} `json:"publicKey"`
		} `json:"signature"`
	} `json:"spec"`
}

// lookupRekor asks the log at base for its entries that record exactly s, as cosign verify-blob
// does when it is given a signature and a certificate: the hashedrekord entry of the digest, the
// signature and the certificate, found by its content (POST /api/v1/log/entries/retrieve). The
// request carries only what the release publishes. It takes at most rekorTimeout and reads at most
// maxRekorAnswer bytes. No answer, a 429 or a 5xx is an ErrLogUnreachable; another refusal or an
// answer that does not parse an ErrSignature; an empty answer is no entries and no error.
func lookupRekor(ctx context.Context, client *http.Client, base string, s *Signed) ([]LogEntry, error) {
	var e hashedRekord
	e.Kind, e.APIVersion = "hashedrekord", "0.0.1"
	e.Spec.Data.Hash.Algorithm, e.Spec.Data.Hash.Value = "sha256", hex.EncodeToString(s.Digest)
	e.Spec.Signature.Content, e.Spec.Signature.PublicKey.Content = s.Sig, s.certPEM()
	body, err := json.Marshal(map[string][]hashedRekord{"entries": {e}})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, rekorTimeout)
	defer cancel()
	u := strings.TrimRight(base, "/") + "/api/v1/log/entries/retrieve"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w (%s): %v", ErrLogUnreachable, base, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRekorAnswer+1))
	if err != nil {
		return nil, fmt.Errorf("%w (%s): %v", ErrLogUnreachable, base, err)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: %s answered %s%s", ErrLogUnreachable, base, resp.Status, rekorReason(data))
	case resp.StatusCode != http.StatusOK:
		// The log checks the entry it is asked about: a 400 says the three do not fit together.
		return nil, fmt.Errorf("%w: sigstore's transparency log (%s) answered %s%s", ErrSignature, base, resp.Status, rekorReason(data))
	}
	if len(data) > maxRekorAnswer {
		return nil, fmt.Errorf("%w: the transparency log's answer is larger than %s", ErrSignature, model.FormatBytes(maxRekorAnswer))
	}
	return parseRekorAnswer(data)
}

// parseRekorAnswer reads the search's answer: a list of {uuid: entry} objects, at most
// maxLogEntries entries in all.
func parseRekorAnswer(data []byte) ([]LogEntry, error) {
	var found []map[string]LogEntry
	if err := json.Unmarshal(data, &found); err != nil {
		return nil, fmt.Errorf("%w: the transparency log's answer does not parse: %v", ErrSignature, err)
	}
	var entries []LogEntry
	for _, m := range found {
		for _, e := range m {
			entries = append(entries, e)
		}
	}
	if len(entries) > maxLogEntries {
		return nil, fmt.Errorf("%w: the transparency log answered %d entries for one signature", ErrSignature, len(entries))
	}
	return entries, nil
}

// rekorReason is the message of the log's error answer, for an error: one line, printable, at
// most maxRekorReason bytes; "" when it has none.
func rekorReason(data []byte) string {
	var answer struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &answer) != nil || answer.Message == "" {
		return ""
	}
	msg := strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return ' '
	}, answer.Message)
	if len(msg) > maxRekorReason {
		msg = strings.ToValidUTF8(msg[:maxRekorReason], "") + "…"
	}
	return fmt.Sprintf(" (%q)", msg)
}
