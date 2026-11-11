package tddedupe

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	reconcile "github.com/samestrin/atcr/reconcile"
)

func mustRead(t *testing.T, tag, data string) ([]reconcile.Finding, int, string) {
	t.Helper()
	got, skipped, warn, err := ReadStream(tag, []byte(data))
	if err != nil {
		t.Fatalf("ReadStream(%q): unexpected error: %v", tag, err)
	}
	return got, skipped, warn
}

func wantFindings(t *testing.T, got, want []reconcile.Finding) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings:\n got  %+v\n want %+v", got, want)
	}
}

func TestReadStreamV1EightColumns(t *testing.T) {
	data := "# atcr-findings/v1\n" +
		"# Format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER\n" +
		"HIGH|a.go:12|nil deref|check nil|Correctness|15|line 12 derefs|claude\n" +
		"LOW|b.go:3|typo|fix it|perf|5|ev|\n"
	got, skipped, warn := mustRead(t, "claude-stream", data)
	if skipped != 0 || warn != "" {
		t.Fatalf("skipped=%d warn=%q, want 0 and no warning", skipped, warn)
	}
	wantFindings(t, got, []reconcile.Finding{
		{Severity: "HIGH", File: "a.go", Line: 12, Problem: "nil deref", Fix: "check nil", Category: "correctness", EstMinutes: 15, Evidence: "line 12 derefs", Reviewer: "claude"},
		// An empty REVIEWER takes the tag; "perf" is canonicalized (D2).
		{Severity: "LOW", File: "b.go", Line: 3, Problem: "typo", Fix: "fix it", Category: stream.CanonicalCategory("perf"), EstMinutes: 5, Evidence: "ev", Reviewer: "claude-stream"},
	})
}

func TestReadStreamV2(t *testing.T) {
	var b strings.Builder
	if err := stream.WriteSourceV2(&b, []stream.Finding{{
		Severity: "MEDIUM", File: "c.go", Line: 7, Problem: "leak", Fix: "close it",
		Category: "resource-leak", EstMinutes: 10, Evidence: "ev", Reviewer: "gpt",
	}}); err != nil {
		t.Fatal(err)
	}
	got, skipped, warn := mustRead(t, "pool", b.String())
	if skipped != 0 || warn != "" {
		t.Fatalf("skipped=%d warn=%q, want 0 and no warning", skipped, warn)
	}
	wantFindings(t, got, []reconcile.Finding{
		{Severity: "MEDIUM", File: "c.go", Line: 7, Problem: "leak", Fix: "close it", Category: "resource-leak", EstMinutes: 10, Evidence: "ev", Reviewer: "gpt"},
	})
}

// The POST_STREAM layout from cadence's execute-code-review: eight columns,
// but SOURCE and BLOCKING where width-reading would put EVIDENCE and REVIEWER.
// BLOCKING must be dropped, never shifted into the reviewer.
func TestReadStreamFormatCommentWithBlocking(t *testing.T) {
	data := "# POST_STREAM - /execute-code-review --post findings\n" +
		"# Format: SEVERITY|FILE_LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|SOURCE|BLOCKING\n" +
		"# BLOCKING: yes = blocks handoff\n" +
		"high|d.go:40|race|lock it|concurrency|30|post-review|yes\n"
	got, skipped, warn := mustRead(t, "post", data)
	if skipped != 0 {
		t.Fatalf("skipped=%d, want 0", skipped)
	}
	if !strings.Contains(warn, `"post"`) {
		t.Fatalf("warning %q does not name the tag", warn)
	}
	wantFindings(t, got, []reconcile.Finding{
		{Severity: "HIGH", File: "d.go", Line: 40, Problem: "race", Fix: "lock it", Category: "concurrency", EstMinutes: 30, Reviewer: "post"},
	})
}

// The independent-review layout from cadence's execute-epic: a different
// column order and the DESCRIPTION / RECOMMENDED_FIX aliases.
func TestReadStreamFormatCommentAliases(t *testing.T) {
	data := "# Independent Review Findings\n" +
		"# Format: SEVERITY|FILE:LINE|CATEGORY|DESCRIPTION|RECOMMENDED_FIX\n" +
		"LOW|e.go:1|style|long line|wrap it\n"
	got, _, _ := mustRead(t, "indep", data)
	wantFindings(t, got, []reconcile.Finding{
		{Severity: "LOW", File: "e.go", Line: 1, Problem: "long line", Fix: "wrap it", Category: "style", Reviewer: "indep"},
	})
}

func TestReadStreamFormatCommentOtherAliases(t *testing.T) {
	data := "# Format: SEVERITY|PATH|PROBLEM|FIX|CATEGORY|EST|ORIGIN|RISK_CLASS|REVIEWERS\n" +
		"MEDIUM|f.go|p|f|security|4|model|high|ann\n" +
		"MEDIUM|f.go:9|p|f|security|4|model|high\n" // short row: REVIEWERS absent, tag fills it
	got, skipped, _ := mustRead(t, "t", data)
	if skipped != 0 {
		t.Fatalf("skipped=%d, want 0", skipped)
	}
	wantFindings(t, got, []reconcile.Finding{
		{Severity: "MEDIUM", File: "f.go", Problem: "p", Fix: "f", Category: "security", EstMinutes: 4, Reviewer: "ann"},
		{Severity: "MEDIUM", File: "f.go", Line: 9, Problem: "p", Fix: "f", Category: "security", EstMinutes: 4, Reviewer: "t"},
	})
}

// A blank line before the `# Format:` line is not the end of the header block.
func TestReadStreamBlankLineBeforeFormat(t *testing.T) {
	data := "\n# Format: SEVERITY|FILE_LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|SOURCE|BLOCKING\n\n" +
		"HIGH|g.go:2|p|f|correctness|1|src|no\n"
	got, _, _ := mustRead(t, "blank", data)
	if len(got) != 1 || got[0].Reviewer != "blank" || got[0].Evidence != "" {
		t.Fatalf("got %+v, want one row read by name (reviewer from tag, no evidence)", got)
	}
}

func TestReadStreamHeaderlessWidths(t *testing.T) {
	cases := []struct {
		name string
		row  string
		want reconcile.Finding
	}{
		{"5 columns", "HIGH|h.go:5|p|f|correctness",
			reconcile.Finding{Severity: "HIGH", File: "h.go", Line: 5, Problem: "p", Fix: "f", Category: "correctness", Reviewer: "legacy"}},
		{"6 columns", "low|h.go:6|p|f|style|bob",
			reconcile.Finding{Severity: "LOW", File: "h.go", Line: 6, Problem: "p", Fix: "f", Category: "style", Reviewer: "bob"}},
		{"8 columns", "MEDIUM|h.go:8|p|f|testing|12|ev|carol",
			reconcile.Finding{Severity: "MEDIUM", File: "h.go", Line: 8, Problem: "p", Fix: "f", Category: "testing", EstMinutes: 12, Evidence: "ev", Reviewer: "carol"}},
		{"10 columns", "CRITICAL|model|high|h.go:10|p|f|security|20|ev|claude",
			reconcile.Finding{Severity: "CRITICAL", File: "h.go", Line: 10, Problem: "p", Fix: "f", Category: "security", EstMinutes: 20, Evidence: "ev", Reviewer: "legacy"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, skipped, warn := mustRead(t, "legacy", "# some legacy stream\n"+tc.row+"\n")
			if skipped != 0 {
				t.Fatalf("skipped=%d, want 0", skipped)
			}
			if warn == "" {
				t.Fatal("headerless stream gave no warning")
			}
			wantFindings(t, got, []reconcile.Finding{tc.want})
		})
	}
}

func TestReadStreamHeaderlessWarningNamesTag(t *testing.T) {
	_, _, warn := mustRead(t, "codex-run", "HIGH|a.go:1|p|f|correctness\n")
	want := "stream \"codex-run\": no `# atcr-findings/v1` header: parsing as a legacy stream. " +
		"Add the header so a consumer never silently reads incompatible data."
	if warn != want {
		t.Fatalf("warning:\n got  %q\n want %q", warn, want)
	}
}

func TestReadStreamUnsupportedVersion(t *testing.T) {
	for _, header := range []string{"# atcr-findings/v3", "# atcr-findings/v1x"} {
		_, _, _, err := ReadStream("future", []byte("\n"+header+"\nHIGH|a.go:1|p|f|c|1|e|r\n"))
		if err == nil {
			t.Fatalf("%s: want an error, got nil", header)
		}
		if !strings.Contains(err.Error(), `"future"`) {
			t.Fatalf("%s: error %q does not name the tag", header, err)
		}
	}
	_, _, _, err := ReadStream("future", []byte("# atcr-findings/v3\n"))
	if !errors.Is(err, stream.ErrUnknownVersion) {
		t.Fatalf("v3: error %v is not ErrUnknownVersion", err)
	}
}

func TestReadStreamSkippedRows(t *testing.T) {
	data := "HIGH|a.go:1|p|f|correctness\n" +
		"HIGH|a.go:2|too short\n" // fewer than 5 columns
	got, skipped, _ := mustRead(t, "s", data)
	if skipped != 1 || len(got) != 1 {
		t.Fatalf("skipped=%d findings=%d, want 1 and 1", skipped, len(got))
	}
}

func TestReadStreamSkipsEmptySeverityOrFile(t *testing.T) {
	data := "|a.go:1|p|f|correctness\n" +
		"HIGH||p|f|correctness\n" +
		"# Format: ignored here, past the header block\n" +
		"HIGH|a.go:3|p|f|correctness\n"
	got, skipped, _ := mustRead(t, "s", data)
	if skipped != 2 || len(got) != 1 || got[0].Line != 3 {
		t.Fatalf("skipped=%d findings=%+v, want 2 skipped and the a.go:3 row", skipped, got)
	}

	named := "# Format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY\nHIGH||p|f|c\n"
	if _, skipped, _ := mustRead(t, "s", named); skipped != 1 {
		t.Fatalf("by name: skipped=%d, want 1", skipped)
	}

	v1 := "# atcr-findings/v1\nHIGH||p|f|c|1|e|r\nHIGH|a.go:1|p|f|c|1|e|r|extra\n"
	got, skipped, _ = mustRead(t, "s", v1)
	if skipped != 2 || len(got) != 0 {
		t.Fatalf("v1: skipped=%d findings=%d, want 2 and 0", skipped, len(got))
	}
}

func TestReadStreamShortFormatFallsBackToWidth(t *testing.T) {
	// A `# Format:` naming fewer than 5 columns declares nothing usable.
	data := "# Format: A|B\nHIGH|a.go:1|p|f|style|bob\n"
	got, _, _ := mustRead(t, "w", data)
	if len(got) != 1 || got[0].Reviewer != "bob" {
		t.Fatalf("got %+v, want the 6-column width read", got)
	}
}

func TestReadStreamEstMinutes(t *testing.T) {
	data := "HIGH|a.go:1|p|f|c|2.6|e|r\nHIGH|a.go:2|p|f|c|-4|e|r\nHIGH|a.go:3|p|f|c|soon|e|r\n"
	got, _, _ := mustRead(t, "e", data)
	if got[0].EstMinutes != 3 || got[1].EstMinutes != 0 || got[2].EstMinutes != 0 {
		t.Fatalf("est minutes = %d, %d, %d; want 3, 0, 0", got[0].EstMinutes, got[1].EstMinutes, got[2].EstMinutes)
	}
}

func TestReadStreamEmpty(t *testing.T) {
	got, skipped, warn := mustRead(t, "empty", "")
	if len(got) != 0 || skipped != 0 || warn == "" {
		t.Fatalf("got %+v skipped=%d warn=%q", got, skipped, warn)
	}
}
