// Package json implements the reconcile-json/v1 format adapter: it decodes an
// external JSON finding stream into []reconcile.Source and encodes a
// reconcile.Result back into a versioned JSON document.
//
// The schema family reconcile-json/v1 is versioned INDEPENDENTLY of ATCR's
// internal atcr-findings/v1 wire format. Field names are the library Finding's
// JSON struct tags. Path-validation fields (path_valid/path_warning/
// path_suggestion and ATCR's cluster_merged) are ATCR-internal and are
// structurally excluded from this schema because the library Finding type does
// not carry them. Evolution within v1 is additive-only: unknown producer fields
// are ignored on decode rather than rejected.
//
// The adapter is stdlib-only (encoding/json + time); it has no ATCR coupling and
// never imports the ATCR boundary adapter.
package json

import (
	"bytes"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samestrin/atcr/reconcile"
)

// SchemaVersion is the independently-versioned schema family this adapter reads
// and writes. It is intentionally distinct from ATCR's atcr-findings/v1.
const SchemaVersion = "reconcile-json/v1"

// utf8BOM is the byte-order mark some producers prepend; encoding/json does not
// skip it, so the adapter strips it before sniffing/unmarshaling.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// decodeEnvelope is one input source object in the reconcile-json/v1 schema:
// {version, source, findings[]}. Unknown producer fields are ignored.
type decodeEnvelope struct {
	Version  string              `json:"version"`
	Source   string              `json:"source"`
	Findings []reconcile.Finding `json:"findings"`
}

// encodeEnvelope is the reconcile-json/v1 output document. Field order is fixed
// by declaration order (version, reconciled_at, findings, summary, ambiguous);
// the top-level findings and ambiguous arrays are never omitempty so absence is
// byte-stable ("[]" rather than a missing key).
type encodeEnvelope struct {
	Version      string                       `json:"version"`
	ReconciledAt string                       `json:"reconciled_at"`
	Findings     []reconcile.Merged           `json:"findings"`
	Summary      reconcile.Summary            `json:"summary"`
	Ambiguous    []reconcile.AmbiguousCluster `json:"ambiguous"`
}

// Decode parses a reconcile-json/v1 document — either a single source object or
// an array of them — into []reconcile.Source. Each object's version must equal
// SchemaVersion (the contract field is strict; unknown extra fields are
// tolerated). A source with no findings yields a non-nil empty slice so it
// mirrors the always-present findings array of the encode envelope.
//
// Decode operates on the caller's fully-buffered bytes: nesting depth is bounded
// by encoding/json, but the caller is responsible for bounding total input size
// (for example, with an io.LimitReader before reading untrusted input).
func Decode(data []byte) ([]reconcile.Source, error) {
	data = bytes.TrimPrefix(data, utf8BOM)
	first := firstNonSpace(data)
	if first == 0 {
		return nil, errors.New("reconcile-json: empty input")
	}

	var envelopes []decodeEnvelope
	if first == '[' {
		if err := stdjson.Unmarshal(data, &envelopes); err != nil {
			return nil, fmt.Errorf("reconcile-json: %w", err)
		}
	} else {
		var one decodeEnvelope
		if err := stdjson.Unmarshal(data, &one); err != nil {
			return nil, fmt.Errorf("reconcile-json: %w", err)
		}
		envelopes = []decodeEnvelope{one}
	}

	sources := make([]reconcile.Source, 0, len(envelopes))
	for i := range envelopes {
		if envelopes[i].Version != SchemaVersion {
			return nil, fmt.Errorf("reconcile-json: source[%d] version must be %q, got %q", i, SchemaVersion, envelopes[i].Version)
		}
		findings := envelopes[i].Findings
		if findings == nil {
			findings = []reconcile.Finding{}
		}
		sources = append(sources, reconcile.Source{Name: envelopes[i].Source, Findings: findings})
	}
	return sources, nil
}

// Encode serializes a reconcile.Result into a reconcile-json/v1 document. The
// reconciled_at timestamp is opts.ReconciledAt (RFC3339, UTC-normalized) when
// set, otherwise time.Now().UTC(). Identical inputs produce byte-identical
// output: field order is fixed by struct declaration and optional per-finding
// fields (disagreement/verification) rely on their omitempty tags.
func Encode(result reconcile.Result, opts reconcile.Options) ([]byte, error) {
	at := opts.ReconciledAt
	if at.IsZero() {
		at = time.Now().UTC()
	}

	findings := result.Findings
	if findings == nil {
		findings = []reconcile.Merged{}
	}
	ambiguous := result.Ambiguous
	if ambiguous == nil {
		ambiguous = []reconcile.AmbiguousCluster{}
	}

	return stdjson.Marshal(encodeEnvelope{
		Version:      SchemaVersion,
		ReconciledAt: at.UTC().Format(time.RFC3339),
		Findings:     findings,
		Summary:      result.Summary,
		Ambiguous:    ambiguous,
	})
}

// firstNonSpace returns the first byte of data that is not JSON insignificant
// whitespace, or 0 if there is none.
func firstNonSpace(data []byte) byte {
	for _, c := range data {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return c
		}
	}
	return 0
}
