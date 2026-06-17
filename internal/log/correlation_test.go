package log

import (
	"bytes"
	"encoding/json"
	"testing"
)

// decodeLines parses newline-delimited JSON log output into records.
func decodeLines(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("invalid JSON log line %q: %v", line, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

func TestWithReviewID_AttachesAttribute(t *testing.T) {
	var buf bytes.Buffer
	base, err := New("info", "json", &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	WithReviewID(base, "rev-123").Info("hi")
	recs := decodeLines(t, buf.Bytes())
	if recs[0][AttrReviewID] != "rev-123" {
		t.Fatalf("expected review_id=rev-123, got %v", recs[0])
	}
}

func TestWithAgent_AttachesAttribute(t *testing.T) {
	var buf bytes.Buffer
	base, err := New("info", "json", &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	WithAgent(base, "security-skeptic").Info("hi")
	recs := decodeLines(t, buf.Bytes())
	if recs[0][AttrAgentName] != "security-skeptic" {
		t.Fatalf("expected agent_name=security-skeptic, got %v", recs[0])
	}
}

func TestWithReviewID_PreservesExistingAttributes(t *testing.T) {
	var buf bytes.Buffer
	base, err := New("info", "json", &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	withFoo := base.With("foo", "bar")
	WithReviewID(withFoo, "rev-9").Info("hi")
	recs := decodeLines(t, buf.Bytes())
	if recs[0]["foo"] != "bar" {
		t.Fatalf("existing attribute lost: %v", recs[0])
	}
	if recs[0][AttrReviewID] != "rev-9" {
		t.Fatalf("review_id missing: %v", recs[0])
	}
}

func TestCorrelation_Chaining(t *testing.T) {
	var buf bytes.Buffer
	base, err := New("info", "json", &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	WithAgent(WithReviewID(base, "rev-7"), "perf-skeptic").Info("hi")
	recs := decodeLines(t, buf.Bytes())
	if recs[0][AttrReviewID] != "rev-7" || recs[0][AttrAgentName] != "perf-skeptic" {
		t.Fatalf("chaining did not attach both attributes: %v", recs[0])
	}
}

func TestWithReviewID_NilLoggerReturnsNil(t *testing.T) {
	if WithReviewID(nil, "x") != nil {
		t.Fatal("WithReviewID(nil, ...) should return nil")
	}
}

func TestWithAgent_NilLoggerReturnsNil(t *testing.T) {
	if WithAgent(nil, "x") != nil {
		t.Fatal("WithAgent(nil, ...) should return nil")
	}
}

func TestCorrelation_EmptyStringStillAttaches(t *testing.T) {
	var buf bytes.Buffer
	base, err := New("info", "json", &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	WithReviewID(base, "").Info("hi")
	recs := decodeLines(t, buf.Bytes())
	if _, ok := recs[0][AttrReviewID]; !ok {
		t.Fatalf("review_id attribute should be present even when empty: %v", recs[0])
	}
}

func TestCorrelation_OriginalLoggerImmutable(t *testing.T) {
	var buf bytes.Buffer
	base, err := New("info", "json", &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = WithReviewID(base, "rev-1") // derive but log with the original
	base.Info("hi")
	recs := decodeLines(t, buf.Bytes())
	if _, ok := recs[0][AttrReviewID]; ok {
		t.Fatalf("original logger was mutated, review_id leaked: %v", recs[0])
	}
}
