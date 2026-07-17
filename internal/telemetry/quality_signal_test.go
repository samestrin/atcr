package telemetry

import (
	"encoding/json"
	"testing"

	"github.com/samestrin/atcr/internal/scorecard"
)

// TestQualitySignal_PayloadHasExactlyFourAllowlistedKeys locks the quality-signal
// wire schema to exactly {persona_id_hash, model, dismissed_count, confirmed_count}
// with no omitempty ambiguity — an accidental new field (e.g. a file path or a
// finding excerpt) fails this immediately (AC 01-05). Mirrors
// TestClient_Send_PayloadHasExactlyFourAllowlistedKeys for Event.
func TestQualitySignal_PayloadHasExactlyFourAllowlistedKeys(t *testing.T) {
	cases := []QualitySignal{
		{PersonaIDHash: "abc123", Model: "claude-sonnet-4-6", DismissedCount: 3, ConfirmedCount: 1},
		{}, // zero value: all four keys must still serialize (no omitempty)
	}
	allowed := []string{"persona_id_hash", "model", "dismissed_count", "confirmed_count"}
	for _, qs := range cases {
		raw, err := json.Marshal(qs)
		if err != nil {
			t.Fatalf("marshal QualitySignal: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if len(m) != 4 {
			t.Fatalf("payload has %d keys, want exactly 4: %s", len(m), raw)
		}
		for _, k := range allowed {
			if _, ok := m[k]; !ok {
				t.Errorf("missing allowlisted key %q in %s", k, raw)
			}
		}
	}
}

// TestQualitySignal_ZeroValueStillSerializesAllFourKeys locks AC 01-05 Edge Case
// 2: a zero-value struct (including a legitimately-zero count) still serializes
// every key — a maintainer must be able to distinguish "zero dismissals" from
// "field absent".
func TestQualitySignal_ZeroValueStillSerializesAllFourKeys(t *testing.T) {
	raw, err := json.Marshal(QualitySignal{})
	if err != nil {
		t.Fatalf("marshal zero QualitySignal: %v", err)
	}
	for _, k := range []string{"persona_id_hash", "model", "dismissed_count", "confirmed_count"} {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if _, ok := m[k]; !ok {
			t.Errorf("zero value dropped key %q: %s", k, raw)
		}
	}
}

// TestQualitySignal_PersonaHashedNotRaw locks AC 01-05 Scenario 3: the
// construction function hashes the raw persona via HashPersonaID and never
// carries the raw persona name in cleartext.
func TestQualitySignal_PersonaHashedNotRaw(t *testing.T) {
	const raw = "security-reviewer"
	qs := NewQualitySignal(raw, "claude-sonnet-4-6", 3, 1)

	if qs.PersonaIDHash != scorecard.HashPersonaID(raw) {
		t.Errorf("PersonaIDHash = %q, want HashPersonaID(%q)", qs.PersonaIDHash, raw)
	}
	if qs.PersonaIDHash == raw {
		t.Errorf("PersonaIDHash must never equal the raw persona name %q", raw)
	}
	if qs.Model != "claude-sonnet-4-6" || qs.DismissedCount != 3 || qs.ConfirmedCount != 1 {
		t.Errorf("non-persona fields must pass through unchanged, got %+v", qs)
	}
}
