package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
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

// TestQualitySignal_NoOmitEmptyOrIgnoredTags is the MECHANICAL gate for the
// "no omitempty" rule that TestQualitySignal_PayloadHasExactlyFourAllowlistedKeys
// cannot enforce alone (TD 30.0): a 5th field tagged `json:"path,omitempty"` is
// invisible to the len==4 assertion because the fixtures never populate it, so
// the field-creep privacy leak would ship green. Reflecting over the struct tags
// fails the moment ANY field is json-ignored or carries an omitempty option,
// whatever the fixtures contain — the rule stops being a doc comment.
func TestQualitySignal_NoOmitEmptyOrIgnoredTags(t *testing.T) {
	typ := reflect.TypeOf(QualitySignal{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag, ok := f.Tag.Lookup("json")
		if !ok {
			t.Errorf("field %s has no json tag — every allowlisted field must serialize explicitly", f.Name)
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			t.Errorf("field %s is json-ignored (%q) — the allowlist must serialize every field", f.Name, tag)
		}
		if name == "" {
			t.Errorf("field %s has no explicit json name (%q)", f.Name, tag)
		}
		for _, opt := range strings.Split(opts, ",") {
			if opt == "omitempty" {
				t.Errorf("field %s carries omitempty (%q) — a never-populated field would be invisible to the len==4 guard", f.Name, tag)
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

// TestClient_SendQualitySignal_EmptyPayloadNoOps locks the transport-side guard
// (TD 30.0): a nil or empty payload short-circuits inside SendQualitySignal
// before dispatch — no semaphore slot, no goroutine, no contentless beacon. The
// exported reusable API must be self-defending rather than relying on every
// caller pre-checking len(payload)==0 the way maybeSendQualitySignal does.
func TestClient_SendQualitySignal_EmptyPayloadNoOps(t *testing.T) {
	var hits int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	c.SendQualitySignal(context.Background(), nil)
	c.SendQualitySignal(context.Background(), []QualitySignal{})
	c.Wait()

	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Errorf("empty/nil payload must not dispatch a beacon, got %d request(s)", n)
	}
}

// TestNewQualitySignal_EmptyPersonaReturnsZeroSentinel locks the construction-side
// guard (TD 30.0): an empty persona is an upstream data-quality bug that must NOT
// be laundered into sha256("")=e3b0c442... — a well-known constant that looks like
// a real, stable aggregation bucket on the backend. The constructor returns the
// zero QualitySignal sentinel (empty PersonaIDHash — recognizable and droppable by
// the caller) instead of a valid-looking hash.
func TestNewQualitySignal_EmptyPersonaReturnsZeroSentinel(t *testing.T) {
	if qs := NewQualitySignal("", "claude-sonnet-4-6", 3, 1); qs != (QualitySignal{}) {
		t.Errorf("empty persona must return the zero QualitySignal sentinel, got %+v", qs)
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
