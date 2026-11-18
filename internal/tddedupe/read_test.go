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

// A row with an empty severity does not start with a severity, so it is prose
// and ignored, as stream.parse ignores it (EST-7267 Q1); a severity-prefixed
// malformed row still counts.
func TestReadStreamSkipsEmptySeverityOrFile(t *testing.T) {
	data := "|a.go:1|p|f|correctness\n" +
		"HIGH||p|f|correctness\n" +
		"LOW|a.go:2|p|f|correctness|x|y\n" + // 7 columns: no layout

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
	data := "HIGH|a.go:1|p|f|c|2.6|e|r\nHIGH|a.go:2|p|f|c|-4|e|r\nHIGH|a.go:3|p|f|c|soon|e|r\n" +
		"HIGH|a.go:4|p|f|c|1e300|e|r\n"
	got, _, _ := mustRead(t, "e", data)
	// 1e300 clamps to one week, the bound stream.ParseSource applies to v1 rows.
	if got[0].EstMinutes != 3 || got[1].EstMinutes != 0 || got[2].EstMinutes != 0 || got[3].EstMinutes != 7*24*60 {
		t.Fatalf("est minutes = %d, %d, %d, %d; want 3, 0, 0, 10080",
			got[0].EstMinutes, got[1].EstMinutes, got[2].EstMinutes, got[3].EstMinutes)
	}
	v1, _, _ := mustRead(t, "e", "# atcr-findings/v1\nHIGH|a.go:1|p|f|c|999999|e|r\n")
	if v1[0].EstMinutes != got[3].EstMinutes {
		t.Fatalf("v1 clamps to %d, legacy to %d; want the same bound", v1[0].EstMinutes, got[3].EstMinutes)
	}
}

func TestReadStreamEmpty(t *testing.T) {
	got, skipped, warn := mustRead(t, "empty", "")
	if len(got) != 0 || skipped != 0 || warn == "" {
		t.Fatalf("got %+v skipped=%d warn=%q", got, skipped, warn)
	}
}

// By name, a row shorter than 5 columns or wider than the declaration is
// skipped, as it is by width; a row with no severity is prose and ignored
// (EST-7267 Q1).
func TestReadStreamByNameSkipsShortAndNoSeverity(t *testing.T) {
	data := "# Format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY\n" +
		"HIGH|a.go:1\n" +
		"|a.go:2|p|f|c\n" +
		"HIGH|a.go:2|p|f|c|extra\n" +
		"HIGH|a.go:3|p|f|c\n"
	got, skipped, _ := mustRead(t, "n", data)
	if skipped != 2 || len(got) != 1 || got[0].Line != 3 {
		t.Fatalf("skipped=%d findings=%+v, want 2 skipped and the a.go:3 row", skipped, got)
	}
}

// The canonical `# Format:` line names EVIDENCE; by name it lands in Evidence.
func TestReadStreamByNameEvidence(t *testing.T) {
	data := "# Format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER\n" +
		"LOW|a.go:1|p|f|style|2|the evidence|ann\n"
	got, _, _ := mustRead(t, "n", data)
	wantFindings(t, got, []reconcile.Finding{
		{Severity: "LOW", File: "a.go", Line: 1, Problem: "p", Fix: "f", Category: "style", EstMinutes: 2, Evidence: "the evidence", Reviewer: "ann"},
	})
}

// A bare trailing colon drops the colon; a non-numeric suffix keeps the whole
// column as the file. Both give line 0, by width and by name.
func TestReadStreamFileLineEdges(t *testing.T) {
	rows := "HIGH|a.go:|p|f|c\nHIGH|C:/x/a.go:12x|p|f|c\n"
	for name, data := range map[string]string{
		"width": rows,
		"name":  "# Format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY\n" + rows,
	} {
		got, _, _ := mustRead(t, "e", data)
		if len(got) != 2 || got[0].File != "a.go" || got[0].Line != 0 || got[1].File != "C:/x/a.go:12x" || got[1].Line != 0 {
			t.Fatalf("%s: got %+v, want a.go:0 and C:/x/a.go:12x:0", name, got)
		}
	}
}

// A version header anywhere in the leading comment block routes the stream to
// stream.ParseSource (row afc6605a9b54d0e6): a v1 header under a comment line
// reads as v1 with no legacy warning, and an unknown version under a comment
// line is still an error, never a legacy read.
func TestReadStreamHeaderAfterLeadingComment(t *testing.T) {
	data := "# written by the claude reviewer\n\n# atcr-findings/v1\n" +
		"HIGH|a.go:12|nil deref|check nil|correctness|15|ev|claude\n" +
		"HIGH|a.go:13|p|f|c|1|e|r|extra\n"
	got, skipped, warn := mustRead(t, "c", data)
	if warn != "" {
		t.Fatalf("warn=%q, want no legacy warning for a v1 stream", warn)
	}
	if skipped != 1 {
		t.Fatalf("skipped=%d, want the 9-column row counted as stream.ParseSource does", skipped)
	}
	wantFindings(t, got, []reconcile.Finding{
		{Severity: "HIGH", File: "a.go", Line: 12, Problem: "nil deref", Fix: "check nil", Category: "correctness", EstMinutes: 15, Evidence: "ev", Reviewer: "claude"},
	})

	_, _, _, err := ReadStream("c", []byte("# a comment\n# atcr-findings/v3\nHIGH|a.go:1|p|f|c|1|e|r\n"))
	if !errors.Is(err, stream.ErrUnknownVersion) {
		t.Fatalf("v3 under a comment: error %v, want ErrUnknownVersion", err)
	}

	// A header after the first row is past the comment block: legacy read.
	late := "HIGH|a.go:1|p|f|c\n# atcr-findings/v1\n"
	if _, _, warn := mustRead(t, "c", late); warn == "" {
		t.Fatal("a header after a row must not route to stream.ParseSource")
	}
}

// A headerless line that does not start with a severity is prose, ignored as
// stream.parse ignores it (row 887ced04802f1e39, EST-7267 Q1); a
// severity-prefixed malformed row is still counted in skipped.
func TestReadStreamProseNotSkipped(t *testing.T) {
	data := "Here are my findings:\n" +
		"GARBAGE|too|few\n" +
		"HIGH|a.go:1|p|f|correctness\n" +
		"HIGH|a.go:2|too short\n" +
		"That is all.\n"
	got, skipped, _ := mustRead(t, "s", data)
	if skipped != 1 || len(got) != 1 {
		t.Fatalf("skipped=%d findings=%d, want 1 (the short HIGH row) and 1", skipped, len(got))
	}

	named := "# Format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY\nsome prose\nLOW|a.go:1\nLOW|a.go:2|p|f|c\n"
	got, skipped, _ = mustRead(t, "s", named)
	if skipped != 1 || len(got) != 1 {
		t.Fatalf("by name: skipped=%d findings=%d, want 1 and 1", skipped, len(got))
	}
}

// Without `# Format:` only the 5, 6, 8 and 10-column layouts are read; any
// other width is an unescaped `|` and counts in skipped, as stream.parse
// counts an over-wide row (row 66632b546b9f8255, EST-7267 Q2). Trailing empty
// fields are padding, so a row written with a trailing `|` still reads.
func TestReadStreamUnsupportedWidthSkipped(t *testing.T) {
	for _, row := range []string{
		"HIGH|a.go:1|p|f|correctness|x|y",               // 7
		"HIGH|a.go:1|p|f|correctness|1|e|r|extra",       // 9
		"HIGH|m|h|a.go:1|p|f|security|2|e|claude|extra", // 11
	} {
		got, skipped, _ := mustRead(t, "w", row+"\n")
		if skipped != 1 || len(got) != 0 {
			t.Fatalf("%q: skipped=%d findings=%d, want 1 and 0", row, skipped, len(got))
		}
	}
	got, skipped, _ := mustRead(t, "w", "HIGH|a.go:1|p|f|correctness|bob|\nHIGH|a.go:2|p|f|c|1|e|r|\n")
	if skipped != 0 || len(got) != 2 || got[0].Reviewer != "bob" || got[1].Reviewer != "r" {
		t.Fatalf("trailing pipes: skipped=%d findings=%+v, want both rows read", skipped, got)
	}
}

// With `# Format:` a row wider than the declaration counts in skipped instead
// of keeping only the declared columns; trailing empty fields are padding.
func TestReadStreamByNameTooWideSkipped(t *testing.T) {
	data := "# Format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|REVIEWER\n" +
		"HIGH|a.go:1|p|with|pipe|f|correctness|ann\n" +
		"HIGH|a.go:2|p|f|correctness|ann||\n"
	got, skipped, _ := mustRead(t, "n", data)
	if skipped != 1 || len(got) != 1 || got[0].Line != 2 || got[0].Reviewer != "ann" {
		t.Fatalf("skipped=%d findings=%+v, want the over-wide row skipped and a.go:2 read", skipped, got)
	}
}

// The prose test reads the column `# Format:` names SEVERITY, so a stream that
// declares it second is not read as all prose.
func TestReadStreamByNameSeverityNotFirst(t *testing.T) {
	data := "# Format: FILE:LINE|SEVERITY|PROBLEM|FIX|CATEGORY\n" +
		"a.go:1|HIGH|p|f|correctness\n" +
		"HIGH|a.go:2|p|f|correctness\n" + // severity in the file column: prose
		"a.go:3|LOW|short\n"
	got, skipped, _ := mustRead(t, "n", data)
	if skipped != 1 || len(got) != 1 || got[0].Line != 1 || got[0].Severity != "HIGH" {
		t.Fatalf("skipped=%d findings=%+v, want a.go:1 read and the short row skipped", skipped, got)
	}
}
