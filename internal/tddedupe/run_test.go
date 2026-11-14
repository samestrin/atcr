package tddedupe

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	reconcile "github.com/samestrin/atcr/reconcile"
)

// header is the v1 header and column declaration every test stream carries, so
// the rows below read by name and no stream warns.
const header = "# atcr-findings/v1\n# Format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER\n"

func mustRun(t *testing.T, opts Options, streams ...Stream) Result {
	t.Helper()
	res, err := Run(streams, opts)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	return res
}

func TestRunTwoReviewersOnOneLineGiveOneHighRowNeedingReview(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3},
		Stream{Tag: "claude", Data: []byte(header + "HIGH|a.go:10|nil deref|check nil|correctness|15|ev1|claude\n")},
		Stream{Tag: "gpt", Data: []byte(header + "MEDIUM|a.go:10|nil pointer dereference|guard the pointer|correctness|20|ev2|gpt\n")},
	)
	if len(res.Merged) != 1 {
		t.Fatalf("merged rows = %d, want 1: %+v", len(res.Merged), res.Merged)
	}
	r := res.Merged[0]
	if r.Severity != "HIGH" || r.Confidence != "HIGH" || !r.NeedsReview || r.ClusterSize != 2 || len(r.Members) != 2 {
		t.Fatalf("row = %+v, want HIGH/HIGH, needs_review, cluster_size 2, two members", r)
	}
	if r.FileLine != "a.go:10" || r.Reviewers != "claude,gpt" || r.Source != "claude" || r.EstMinutes != 20 {
		t.Fatalf("row = %+v, want file_line a.go:10, reviewers claude,gpt, source claude, est 20", r)
	}
	if r.Disagreement != "MEDIUM vs HIGH" {
		t.Fatalf("disagreement = %q, want %q", r.Disagreement, "MEDIUM vs HIGH")
	}
	// PROBLEM and FIX are pooled, not the longest: neither remedy is dropped.
	if r.Problem != "nil deref // nil pointer dereference" || r.Fix != "check nil // guard the pointer" {
		t.Fatalf("problem=%q fix=%q, want both values pooled with ' // '", r.Problem, r.Fix)
	}
	// Each member carries its own values, never the pooled ones.
	want := []Member{
		{Reviewer: "claude", Severity: "HIGH", FileLine: "a.go:10", Problem: "nil deref", Fix: "check nil", Category: "correctness", EstMinutes: 15, Evidence: "ev1"},
		{Reviewer: "gpt", Severity: "MEDIUM", FileLine: "a.go:10", Problem: "nil pointer dereference", Fix: "guard the pointer", Category: "correctness", EstMinutes: 20, Evidence: "ev2"},
	}
	if !reflect.DeepEqual(r.Members, want) {
		t.Fatalf("members:\n got  %+v\n want %+v", r.Members, want)
	}
	if !reflect.DeepEqual(res.Summary, Summary{InputRows: 2, Sources: 2, Clusters: 1, MergedRows: 1, NeedsReviewCount: 1}) {
		t.Fatalf("summary = %+v", res.Summary)
	}
}

func TestRunSingleSourceRowIsMediumWithNoMembers(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3},
		Stream{Tag: "claude", Data: []byte(header + "LOW|a.go:10|typo|fix it|style|5|ev|claude\n")},
	)
	r := res.Merged[0]
	if r.Confidence != "MEDIUM" || r.NeedsReview || r.Members != nil || r.ClusterSize != 1 || r.Disagreement != "" {
		t.Fatalf("row = %+v, want MEDIUM, no review, no members, cluster_size 1, no disagreement", r)
	}
}

func TestRunUntrustedSourceAloneIsLow(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3, Untrusted: []string{"pool"}},
		Stream{Tag: "pool", Data: []byte(header + "LOW|a.go:10|typo|fix it|style|5|ev|\n")},
		Stream{Tag: "claude", Data: []byte(header + "LOW|b.go:10|typo|fix it|style|5|ev|\n")},
	)
	got := map[string]string{}
	for _, r := range res.Merged {
		got[r.FileLine] = r.Confidence
	}
	want := map[string]string{"a.go:10": "LOW", "b.go:10": "MEDIUM"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("confidence by row = %v, want %v", got, want)
	}
}

// An untrusted source corroborated by a trusted one is not LOW; neither is
// agreement between two reviewers, as td-dedupe ranks 2+ reviewers HIGH first.
func TestRunUntrustedNeedsEveryRowUntrusted(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3, Untrusted: []string{"pool", "pool2"}},
		Stream{Tag: "pool", Data: []byte(header + "LOW|a.go:10|typo|fix it|style|5|ev|x\n" + "LOW|b.go:10|typo|fix it|style|5|ev|x\n")},
		Stream{Tag: "claude", Data: []byte(header + "LOW|a.go:10|typo|fix it|style|5|ev|x\n")},
		Stream{Tag: "pool2", Data: []byte(header + "LOW|b.go:10|typo|fix it|style|5|ev|y\n")},
	)
	got := map[string]string{}
	for _, r := range res.Merged {
		got[r.FileLine] = r.Confidence
	}
	want := map[string]string{"a.go:10": "MEDIUM", "b.go:10": "HIGH"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("confidence by row = %v, want %v", got, want)
	}
}

func TestRunHighAndLowGiveDisagreement(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3},
		Stream{Tag: "claude", Data: []byte(header + "HIGH|a.go:10|p|f|correctness|5|ev|claude\n")},
		Stream{Tag: "gpt", Data: []byte(header + "LOW|a.go:12|p|f|correctness|5|ev|gpt\n")},
	)
	if got := res.Merged[0].Disagreement; got != "LOW vs HIGH" {
		t.Fatalf("disagreement = %q, want %q", got, "LOW vs HIGH")
	}
}

func TestRunToleranceDecidesTheCluster(t *testing.T) {
	data := []byte(header + "LOW|a.go:10|p|f|style|5|ev|claude\n" + "LOW|a.go:15|q|g|style|5|ev|gpt\n")
	if n := len(mustRun(t, Options{Tolerance: 3}, Stream{Tag: "s", Data: data}).Merged); n != 2 {
		t.Fatalf("tolerance 3: merged rows = %d, want 2", n)
	}
	if n := len(mustRun(t, Options{Tolerance: 5}, Stream{Tag: "s", Data: data}).Merged); n != 1 {
		t.Fatalf("tolerance 5: merged rows = %d, want 1", n)
	}
}

// Rows are ordered by file, then line; a row with no line keeps file_line as
// the bare file and sorts before the file's lined rows, as td-dedupe's does.
func TestRunOrdersByFileThenLine(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 0},
		Stream{Tag: "s", Data: []byte(header +
			"LOW|b.go:1|p|f|style|5|ev|x\n" +
			"LOW|a.go:20|p|f|style|5|ev|x\n" +
			"LOW|a.go|p|f|style|5|ev|x\n" +
			"LOW|a.go:3|p|f|style|5|ev|x\n")},
	)
	var got []string
	for _, r := range res.Merged {
		got = append(got, r.FileLine)
	}
	want := []string{"a.go", "a.go:3", "a.go:20", "b.go:1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRunSummaryCountsSkippedRowsAndWarnings(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3},
		Stream{Tag: "legacy", Data: []byte("HIGH|a.go:1|p|f|correctness\nonly|three|cols\n")},
		Stream{Tag: "v1", Data: []byte(header)},
	)
	s := res.Summary
	if s.InputRows != 1 || s.Sources != 2 || s.Skipped != 1 || len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], `"legacy"`) {
		t.Fatalf("summary = %+v, want 1 row, 2 sources, 1 skipped, one warning naming legacy", s)
	}
}

func TestRunUnsupportedVersionIsAnError(t *testing.T) {
	_, err := Run([]Stream{{Tag: "bad", Data: []byte("# atcr-findings/v9\nHIGH|a.go:1|p|f|c|1|e|r\n")}}, Options{Tolerance: 3})
	if err == nil || !strings.Contains(err.Error(), `"bad"`) {
		t.Fatalf("err = %v, want an error naming the stream", err)
	}
}

// No streams with rows still print an empty merged list, never null.
func TestRunEmptyInputPrintsEmptyList(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3}, Stream{Tag: "s", Data: []byte(header)})
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"merged":[]`) {
		t.Fatalf("json = %s, want an empty merged list", raw)
	}
}

// The JSON keys are td-dedupe's, so the cadence skill changes only its command.
func TestRunJSONKeysAreTdDedupes(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3},
		Stream{Tag: "legacy", Data: []byte("HIGH|a.go:1|p|f|correctness\n")},
		Stream{Tag: "gpt", Data: []byte(header + "LOW|a.go:1|q|g|correctness|5|ev|gpt\n")},
	)
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Merged  []map[string]json.RawMessage `json:"merged"`
		Summary map[string]json.RawMessage   `json:"summary"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	assertKeys(t, "top", top, "merged", "summary")
	assertKeys(t, "summary", doc.Summary, "input_rows", "sources", "clusters", "merged_rows", "needs_review_count", "skipped", "warnings")
	assertKeys(t, "merged row", doc.Merged[0], "severity", "file_line", "problem", "fix", "category", "est_minutes", "source", "reviewers", "confidence", "disagreement", "cluster_size", "needs_review", "members", "evidence")
	var members []map[string]json.RawMessage
	if err := json.Unmarshal(doc.Merged[0]["members"], &members); err != nil {
		t.Fatal(err)
	}
	assertKeys(t, "member", members[0], "reviewer", "severity", "file_line", "problem", "fix", "category", "est_minutes", "evidence")
}

func assertKeys(t *testing.T, what string, m map[string]json.RawMessage, want ...string) {
	t.Helper()
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s keys = %v, want %v", what, got, want)
	}
}

// A clustering that loses a row fails the run rather than printing a short
// result: the accounting check is the guard against silent data loss.
func TestRunFailsFatalWhenClustersLoseARow(t *testing.T) {
	orig := clusterWithin
	t.Cleanup(func() { clusterWithin = orig })
	clusterWithin = func(f []reconcile.Finding, tol int) [][]reconcile.Finding {
		return orig(f, tol)[1:]
	}
	_, err := Run([]Stream{{Tag: "s", Data: []byte(header + "LOW|a.go:1|p|f|style|5|ev|x\nLOW|b.go:1|p|f|style|5|ev|x\n")}}, Options{Tolerance: 3})
	if err == nil || !strings.HasPrefix(err.Error(), "FATAL") {
		t.Fatalf("err = %v, want a FATAL accounting error", err)
	}
}

// A clustering that repeats a row is caught as well: equal counts are not
// enough when one row is lost and another doubled.
func TestRunFailsFatalWhenClustersRepeatARow(t *testing.T) {
	orig := clusterWithin
	t.Cleanup(func() { clusterWithin = orig })
	clusterWithin = func(f []reconcile.Finding, tol int) [][]reconcile.Finding {
		c := orig(f, tol)
		c[1] = c[0]
		return c
	}
	_, err := Run([]Stream{{Tag: "s", Data: []byte(header + "LOW|a.go:1|p|f|style|5|ev|x\nLOW|b.go:1|p|f|style|5|ev|x\n")}}, Options{Tolerance: 3})
	if err == nil || !strings.HasPrefix(err.Error(), "FATAL") {
		t.Fatalf("err = %v, want a FATAL accounting error", err)
	}
}

// EVIDENCE survives the merge: the merged row leads with the highest-severity
// member's evidence and joins a differing one with " / ", and each member keeps
// its own, so a split cluster stays cited.
func TestRunKeepsEvidenceOnTheMergedRowAndEachMember(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3},
		Stream{Tag: "gpt", Data: []byte(header + "MEDIUM|a.go:10|nil pointer|guard it|correctness|20|ptr read before the check|gpt\n")},
		Stream{Tag: "claude", Data: []byte(header + "HIGH|a.go:11|nil deref|check nil|correctness|15|deref on line 11|claude\n")},
	)
	if len(res.Merged) != 1 || len(res.Merged[0].Members) != 2 {
		t.Fatalf("merged = %+v, want one row with two members", res.Merged)
	}
	r := res.Merged[0]
	if want := "deref on line 11 / ptr read before the check"; r.Evidence != want {
		t.Fatalf("row evidence = %q, want %q (highest severity first)", r.Evidence, want)
	}
	got := map[string]string{}
	for _, m := range r.Members {
		got[m.Reviewer] = m.Evidence
	}
	want := map[string]string{"claude": "deref on line 11", "gpt": "ptr read before the check"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("member evidence = %v, want %v", got, want)
	}
}

// Identical evidence is not repeated, and an empty one adds no separator.
func TestRunEvidenceSkipsRepeatsAndEmpties(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3},
		Stream{Tag: "a", Data: []byte(header + "LOW|a.go:10|p|f|style|5|same quote|a\n")},
		Stream{Tag: "b", Data: []byte(header + "HIGH|a.go:10|p|f|style|5|same quote|b\n")},
		Stream{Tag: "c", Data: []byte(header + "CRITICAL|a.go:10|p|f|style|5||c\n")},
	)
	r := res.Merged[0]
	if r.Evidence != "same quote" {
		t.Fatalf("row evidence = %q, want %q", r.Evidence, "same quote")
	}
	for _, m := range r.Members {
		if m.Reviewer == "c" && m.Evidence != "" {
			t.Fatalf("member c evidence = %q, want empty", m.Evidence)
		}
	}
}

// A row with no EVIDENCE stays empty, alone or merged with another empty one.
func TestRunEmptyEvidenceStaysEmpty(t *testing.T) {
	res := mustRun(t, Options{Tolerance: 3},
		Stream{Tag: "legacy", Data: []byte("HIGH|a.go:1|p|f|correctness\nLOW|b.go:1|p|f|style\n")},
		Stream{Tag: "gpt", Data: []byte(header + "LOW|b.go:2|q|g|style|5||gpt\n")},
	)
	if len(res.Merged) != 2 {
		t.Fatalf("merged = %+v, want two rows", res.Merged)
	}
	for _, r := range res.Merged {
		if r.Evidence != "" {
			t.Fatalf("row %s evidence = %q, want empty", r.FileLine, r.Evidence)
		}
		for _, m := range r.Members {
			if m.Evidence != "" {
				t.Fatalf("member %s evidence = %q, want empty", m.FileLine, m.Evidence)
			}
		}
	}
	raw, err := json.Marshal(res.Merged[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"evidence":""`) {
		t.Fatalf("json = %s, want an empty evidence key", raw)
	}
}
