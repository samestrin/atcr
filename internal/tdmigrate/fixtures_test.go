package tdmigrate

import (
	"strings"
	"testing"
)

// footgunItem builds a valid item whose long-form fields carry a YAML footgun
// value, so a marshal -> strict-decode round-trip proves the value survives as
// a string (yaml.v3 quotes/escapes by construction).
func footgunItem(problem, fix, category string) Item {
	return Item{
		Group: "1", Status: StatusDeferred, Severity: "LOW",
		File: "internal/foo.go:1", Problem: problem, Fix: fix,
		Category: category, EstMinutes: 5, Source: "code-review",
	}
}

func roundTripItem(t *testing.T, it Item) Item {
	t.Helper()
	s := Shard{Date: "2026-06-26", SourceType: "Sprint", Label: "footgun", Items: []Item{it}}
	data, err := MarshalShard(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := DecodeShardStrict(data)
	if err != nil {
		t.Fatalf("strict decode failed for marshaled output:\n%s\nerr=%v", data, err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got.Items))
	}
	return got.Items[0]
}

// TestFootguns_RoundTripAsStrings covers the adversarial YAML corpus from the
// epic: every value that *looks* like a non-string YAML type must survive a
// marshal -> strict-load round-trip unchanged.
func TestFootguns_RoundTripAsStrings(t *testing.T) {
	cases := map[string]struct{ problem, fix, category string }{
		"norway-no":        {"no", "yes", "on"},                    // bool-ish words
		"norway-off":       {"off", "true", "false"},               // more bool-ish words
		"version-float":    {"version 1.10 ships", "1.20", "v2.0"}, // float-ish
		"leading-zeros":    {"id 007 failed", "00755", "0x1F"},     // octal/hex/zero-pad
		"colon-in-value":   {"key: value here", "a:b:c", "x: y"},   // mapping-looking
		"leading-dash":     {"- item one", "-5 things", "--flag"},  // seq/scalar-looking
		"unicode":          {"café ☃ 你好 €", "naïve fix", "tëst"},   // non-ASCII
		"numeric-string":   {"42", "3.14159", "1e10"},              // pure numbers
		"null-ish":         {"null", "~", "NULL"},                  // null tokens
		"empty-ish-braces": {"{not a map}", "[not a seq]", "&anchor"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			in := footgunItem(c.problem, c.fix, c.category)
			out := roundTripItem(t, in)
			if out.Problem != c.problem {
				t.Errorf("problem drifted: %q -> %q", c.problem, out.Problem)
			}
			if out.Fix != c.fix {
				t.Errorf("fix drifted: %q -> %q", c.fix, out.Fix)
			}
			if out.Category != c.category {
				t.Errorf("category drifted: %q -> %q", c.category, out.Category)
			}
		})
	}
}

// TestFootgun_MultilineBlockScalar proves multi-line text survives round-trip.
func TestFootgun_MultilineBlockScalar(t *testing.T) {
	multi := "first line\nsecond line\n\nfourth after blank\n  indented detail"
	out := roundTripItem(t, footgunItem(multi, "single", "correctness"))
	if out.Problem != multi {
		t.Errorf("multi-line problem drifted:\nwant %q\ngot  %q", multi, out.Problem)
	}
}

// TestFootgun_EstMinutesZeroStaysInt guards the "0"-string-vs-int trap: a literal
// est_minutes 0 must decode as the int 0, not be coerced into or out of a string.
func TestFootgun_EstMinutesZeroStaysInt(t *testing.T) {
	it := footgunItem("p", "f", "c")
	it.EstMinutes = 0
	out := roundTripItem(t, it)
	if out.EstMinutes != 0 {
		t.Errorf("est_minutes 0 drifted to %d", out.EstMinutes)
	}
}

// TestStrictLoad_RejectsTabIndentation proves the strict gate rejects a tab in
// indentation (a classic YAML malformation), failing loudly.
func TestStrictLoad_RejectsTabIndentation(t *testing.T) {
	// The items list is indented with a hard tab — invalid YAML.
	withTab := "date: \"2026-06-26\"\nsource_type: Sprint\nlabel: x\nitems:\n\t- group: \"1\"\n"
	if _, err := DecodeShardStrict([]byte(withTab)); err == nil {
		t.Error("expected strict decode to reject tab-indented YAML")
	}
}

// TestStrictLoad_RejectsUnknownField proves KnownFields(true) rejects a typo'd or
// extraneous field rather than silently ignoring it.
func TestStrictLoad_RejectsUnknownField(t *testing.T) {
	withUnknown := "date: \"2026-06-26\"\nsource_type: Sprint\nlabel: x\nseverity_typo: LOW\nitems: []\n"
	err := func() error { _, e := DecodeShardStrict([]byte(withUnknown)); return e }()
	if err == nil || !strings.Contains(err.Error(), "field") {
		t.Errorf("expected unknown-field rejection, got %v", err)
	}
}

// TestStrictLoad_RejectsMultipleDocuments proves a shard containing more than
// one YAML document is rejected, rather than silently discarding the tail.
func TestStrictLoad_RejectsMultipleDocuments(t *testing.T) {
	item := "  - group: \"1\"\n    status: open\n    severity: LOW\n    file: f.go:1\n    problem: p\n    fix: f\n    category: c\n    est_minutes: 5\n    source: s\n"
	multi := "date: \"2026-06-26\"\nsource_type: Sprint\nlabel: x\nitems:\n" + item + "---\ndate: \"2026-06-27\"\nsource_type: Sprint\nlabel: y\nitems:\n" + item
	if _, err := DecodeShardStrict([]byte(multi)); err == nil {
		t.Error("expected strict decode to reject a multi-document shard")
	}
}

// TestStrictLoad_RejectsSchemaViolation proves a well-formed YAML that violates
// the schema (bad enum) is rejected by the gate.
func TestStrictLoad_RejectsSchemaViolation(t *testing.T) {
	badEnum := "date: \"2026-06-26\"\nsource_type: Sprint\nlabel: x\nitems:\n  - group: \"1\"\n    status: deferred\n    severity: SPICY\n    file: f.go:1\n    problem: p\n    fix: f\n    category: c\n    est_minutes: 5\n    source: s\n"
	if _, err := DecodeShardStrict([]byte(badEnum)); err == nil {
		t.Error("expected schema rejection for invalid severity enum")
	}
}
