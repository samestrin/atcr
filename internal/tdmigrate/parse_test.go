package tdmigrate

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

const sample9Col = `# Technical Debt Tracking

## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| LOW | 1 | 0 | 0 |

### [2026-06-26] From Sprint: epic-11.2

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [ ] | LOW | internal/tools/dispatch.go:123 | substring matching rejects legit names | match on token boundaries | REGRESSION_RISK | 30 | execute-epic-independent |
| U | [/] | MEDIUM | internal/foo.go:5 | a deferred thing | do the thing | correctness | 0 | execute-sprint |
`

const sample11Col = `### [2026-06-23] From Sprint: 8.0_reconciler_library

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | HIGH | internal/reconcile/discover.go:25 | type drift | convert in adapter | correctness | 0 | execute-sprint | code-reviewer, claude | HIGH |
`

func TestParseREADME_9Col(t *testing.T) {
	shards, err := ParseREADME(sample9Col)
	if err != nil {
		t.Fatalf("ParseREADME error: %v", err)
	}
	if len(shards) != 1 {
		t.Fatalf("want 1 shard, got %d", len(shards))
	}
	s := shards[0]
	if s.Date != "2026-06-26" || s.SourceType != "Sprint" || s.Label != "epic-11.2" {
		t.Errorf("bad shard meta: %+v", s)
	}
	if len(s.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(s.Items))
	}
	i0 := s.Items[0]
	if i0.Group != "1" || i0.Status != StatusOpen || i0.Severity != "LOW" ||
		i0.EstMinutes != 30 || i0.Source != "execute-epic-independent" {
		t.Errorf("bad item0: %+v", i0)
	}
	if i0.Reviewers != nil || i0.Confidence != "" {
		t.Errorf("9-col item should have no reviewers/confidence: %+v", i0)
	}
	if s.Items[1].Status != StatusDeferred || s.Items[1].EstMinutes != 0 {
		t.Errorf("bad item1: %+v", s.Items[1])
	}
}

func TestParseREADME_11Col(t *testing.T) {
	shards, err := ParseREADME(sample11Col)
	if err != nil {
		t.Fatalf("ParseREADME error: %v", err)
	}
	it := shards[0].Items[0]
	if !reflect.DeepEqual(it.Reviewers, []string{"code-reviewer", "claude"}) {
		t.Errorf("bad reviewers: %#v", it.Reviewers)
	}
	if it.Confidence != "HIGH" {
		t.Errorf("bad confidence: %q", it.Confidence)
	}
}

func TestParseREADME_BadFieldCount(t *testing.T) {
	bad := `### [2026-06-26] From Sprint: x

| Group | | Severity |
|---|---|---|
| 1 | [ ] | LOW |
`
	if _, err := ParseREADME(bad); err == nil {
		t.Error("expected error for non-9/11 column data row")
	}
}

func TestParseREADME_BadEst(t *testing.T) {
	bad := `### [2026-06-26] From Sprint: x

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|---|---|---|---|---|---|---|---|---|
| 1 | [ ] | LOW | f.go:1 | p | fix | cat | notanumber | src |
`
	if _, err := ParseREADME(bad); err == nil {
		t.Error("expected error for non-integer est_minutes")
	}
}

// canonicalize sorts shards (and ignores ordering) so two parses can be compared
// for SEMANTIC equality regardless of section ordering.
func canonicalize(shards []Shard) []Shard {
	out := make([]Shard, len(shards))
	copy(out, shards)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].SourceType < out[j].SourceType
	})
	return out
}

func TestGenerateTable_SemanticRoundTrip(t *testing.T) {
	orig, err := ParseREADME(sample9Col + "\n" + sample11Col)
	if err != nil {
		t.Fatalf("parse orig: %v", err)
	}
	table, err := GenerateTable(orig)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	reparsed, err := ParseREADME(table)
	if err != nil {
		t.Fatalf("parse regenerated: %v", err)
	}
	if !reflect.DeepEqual(canonicalize(orig), canonicalize(reparsed)) {
		t.Errorf("round-trip mismatch:\norig=%+v\nreparsed=%+v", canonicalize(orig), canonicalize(reparsed))
	}
}

// liveREADME proves the migration against the real corpus (AC2): the live table
// must survive table -> shards (in-memory) -> regenerated table -> shards with
// zero data loss.
func TestLiveREADME_SemanticRoundTrip(t *testing.T) {
	path := filepath.Join("..", "..", ".planning", "technical-debt", "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("live README not readable: %v", err)
	}
	orig, err := ParseREADME(string(data))
	if err != nil {
		t.Fatalf("parse live README: %v", err)
	}
	if len(orig) == 0 {
		t.Fatal("parsed zero shards from live README")
	}
	total := 0
	for _, s := range orig {
		total += len(s.Items)
		if err := s.Validate(); err != nil {
			t.Errorf("live shard %s/%s invalid: %v", s.Date, s.Label, err)
		}
	}
	table, err := GenerateTable(orig)
	if err != nil {
		t.Fatalf("generate from live: %v", err)
	}
	reparsed, err := ParseREADME(table)
	if err != nil {
		t.Fatalf("re-parse generated: %v", err)
	}
	if !reflect.DeepEqual(canonicalize(orig), canonicalize(reparsed)) {
		t.Error("live README round-trip lost or altered data")
	}
	t.Logf("live README: %d shards, %d items round-tripped", len(orig), total)
}
