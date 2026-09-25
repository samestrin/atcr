package report

import (
	"fmt"
	"io"
	"strings"

	goaxi "github.com/samestrin/go-axi"
	toon "github.com/toon-format/toon-go"

	"github.com/samestrin/atcr/internal/reconcile"
)

// axiFindingsPayload is the unpaginated --axi findings document: a single
// standard TOON tabular array, findings[N]{...}:.
type axiFindingsPayload struct {
	Findings []toon.Object `toon:"findings"`
}

// axiPaginatedPayload is the paginated --axi findings document. Rows are capped
// BEFORE encoding, so the header N always equals the rows emitted and the
// payload stays decodable by a stock TOON reader; the true pre-truncation count
// rides the sibling `total` key beside `truncated` (AC8). Both keys appear in
// every payload, cut or uncut, so the shape never depends on the cap.
type axiPaginatedPayload struct {
	Findings  []toon.Object `toon:"findings"`
	Total     int           `toon:"total"`
	Truncated bool          `toon:"truncated"`
}

// encodeAXI writes v as standard TOON through go-axi, which sanitizes every
// string before encoding — but only SINGLE control bytes: it strips raw ESC/C1
// control runes, U+2028/U+2029 and invalid UTF-8, while Unicode Cf format
// characters (U+202E bidi overrides, U+200B) pass through raw and a stripped
// escape leaves its CSI parameter text ("[31m") as residue. That exact scope is
// pinned by TestRenderAXI_SanitizationScope. go-axi writes nothing on failure —
// verified against v0.3.1 (Sanitize → full Marshal → one writeLine, so a marshal
// fault precedes any byte) and pinned by TestEncodeAXI_WritesNothingOnFailure;
// its typed failures (*KeyCollisionError, *CycleError) are wrapped with %w so
// callers can still reach them via errors.As.
//
// The encode is goaxi.EncodeChecked, not bare Encode: a value whose encoding
// silently loses data (a TextMarshaler field toon-go drops, e.g.) fails loudly
// here at runtime instead of emitting a lossy payload that still parses.
// EncodeChecked writes nothing unless the verdict is OK, so the all-or-nothing
// guarantee above holds on the guard path too (pinned by
// TestEncodeAXI_FailsLoudlyOnLossyValue). The ~14% encode cost is a ratified
// tradeoff (2026-09-24): this is a cold admin-output path, and the test-only
// Check guard (axi_verdict_test.go) cannot see a payload shape added after it
// was written.
func encodeAXI(w io.Writer, v any) error {
	if _, err := goaxi.EncodeChecked(w, v); err != nil {
		return fmt.Errorf("axi encoder: %w", err)
	}
	return nil
}

// renderAXI re-encodes findings as a standard TOON (Token-Optimized Object
// Notation) tabular array — the token-dense machine payload for the
// agent-experience (--axi) mode. The base columns mirror the atcr-findings/v1
// reconciled 9-column contract field-for-field, so the payload is a re-encoding
// of the same machine contract a --format json consumer sees, not a new schema.
//
// The optional per-finding signals (disagreement, verification.*,
// evidence_exec.*, fix_warning, fix_review) are additive columns declared only
// when some finding carries them (axiColumnsFor). Rows are ordered toon.Objects
// so the dynamic column set keeps its fixed order under goaxi.Encode. Zero
// findings encode as the TOON empty-array form findings[0]: (AC 01-01 Edge
// Case 1), never a human "No findings." sentence.
//
// axi.md design-tension resolutions (AC 01-02 Scenario 3):
//   - Principle 2 ("3–4 default fields") is deliberately NOT applied — the full
//     9-column field set is retained because tabular TOON rows are already
//     token-lean, and dropping columns would make axi a lossy subset of the JSON
//     contract rather than a faithful re-encoding.
//   - Principle 4 ("pre-computed aggregates") is honored via the array header's
//     count, the paginated payload's `total` key, and the run metadata carried on
//     the review path (AC 01-03) — not a separate aggregation pass here.
func renderAXI(w io.Writer, findings []reconcile.JSONFinding) error {
	return encodeAXI(w, axiFindingsDoc(findings))
}

// axiFindingsDoc builds the unpaginated findings document renderAXI encodes.
// Split out so tests can run goaxi.Check over the exact value that is emitted.
func axiFindingsDoc(findings []reconcile.JSONFinding) axiFindingsPayload {
	return axiFindingsPayload{Findings: axiRows(findings)}
}

// axiRows builds one ordered TOON row per finding. The slice is never nil, so an
// empty list encodes as findings[0]: rather than a null value.
func axiRows(findings []reconcile.JSONFinding) []toon.Object {
	cols := axiColumnsFor(findings)
	rows := make([]toon.Object, 0, len(findings))
	for _, f := range findings {
		rows = append(rows, axiRow(f, cols))
	}
	return rows
}

// axiRow encodes one finding. est_minutes, exit_code and challenge_survived are
// typed numbers/booleans; every free-text field is capped by axiText. In a mixed
// payload (only some findings carry a declared block) an absent block encodes
// as null — TOON's absence form — so a stock typed decoder with pointer fields
// accepts the whole document; "" in a typed column would make toon.Unmarshal
// reject the payload ("cannot assign string to int"). The legacy pipe path
// keeps its quoted empty-string cells, which are frozen bytes.
func axiRow(f reconcile.JSONFinding, cols axiColumns) toon.Object {
	fields := []toon.Field{
		{Key: "severity", Value: axiText(f.Severity)},
		{Key: "file:line", Value: axiText(fmt.Sprintf("%s:%d", f.File, f.Line))},
		{Key: "problem", Value: axiText(f.Problem)},
		{Key: "fix", Value: axiText(f.Fix)},
		{Key: "category", Value: axiText(f.Category)},
		{Key: "est_minutes", Value: f.EstMinutes},
		{Key: "evidence", Value: axiText(f.Evidence)},
		{Key: "reviewers", Value: axiText(strings.Join(f.Reviewers, ","))},
		{Key: "confidence", Value: axiText(f.Confidence)},
	}
	if cols.disagreement {
		fields = append(fields, toon.Field{Key: "disagreement", Value: axiText(f.Disagreement)})
	}
	if cols.verification {
		var verdict, skeptic, notes string
		var survived any = nil
		if v := f.Verification; v != nil {
			verdict, skeptic, notes, survived = v.Verdict, v.Skeptic, v.Notes, v.ChallengeSurvived
		}
		fields = append(fields,
			toon.Field{Key: "verification.verdict", Value: axiText(verdict)},
			toon.Field{Key: "verification.skeptic", Value: axiText(skeptic)},
			toon.Field{Key: "verification.notes", Value: axiText(notes)},
			toon.Field{Key: "verification.challenge_survived", Value: survived})
	}
	if cols.evidence {
		var command, excerpt string
		var exitCode any = nil
		if e := f.EvidenceExec; e != nil {
			command, excerpt, exitCode = e.Command, e.OutputExcerpt, e.ExitCode
		}
		fields = append(fields,
			toon.Field{Key: "evidence_exec.command", Value: axiText(command)},
			toon.Field{Key: "evidence_exec.exit_code", Value: exitCode},
			toon.Field{Key: "evidence_exec.output_excerpt", Value: axiText(excerpt)})
	}
	if cols.fixWarning {
		fields = append(fields, toon.Field{Key: "fix_warning", Value: axiText(f.FixWarning)})
	}
	if cols.fixReview {
		fields = append(fields, toon.Field{Key: "fix_review", Value: axiText(f.FixReview)})
	}
	return toon.NewObject(fields...)
}

// axiText caps a free-text cell at maxTextLen runes (the same bound the
// md/checklist views apply). A findings row is one physical line, so without a
// per-cell cap one reviewer-controlled field could render as a multi-megabyte
// line and blow an agent consumer's context budget. Over-cap fields are
// intentionally not length-faithful; fields of at most maxTextLen runes pass
// through verbatim (AC2). The go-axi sanitizer runs BEFORE the cap so the bound
// applies to the emitted text: invalid UTF-8 is stripped (never turned into
// U+FFFD by the []rune conversion an over-cap field would hit) and control
// bytes do not eat the cap (a field whose sanitized text fits is not cut).
func axiText(s string) string { return truncate(goaxi.SanitizeString(s), maxTextLen) }

// singleRowAXI builds a one-row standard TOON array named name. header is the
// single source of the column order, so a header/value width mismatch is an
// internal bug reported as an error rather than a misaligned payload.
func singleRowAXI(name string, header []string, values []any) (toon.Object, error) {
	if len(values) != len(header) {
		return toon.Object{}, fmt.Errorf("axi encoder: %s row has %d columns, header declares %d", name, len(values), len(header))
	}
	fields := make([]toon.Field, len(header))
	for i, h := range header {
		fields[i] = toon.Field{Key: h, Value: values[i]}
	}
	return toon.NewObject(toon.Field{Key: name, Value: []toon.Object{toon.NewObject(fields...)}}), nil
}

// reviewSummaryAXIDoc builds the one-row review_summary document. Identity
// fields are strings; counts are bare TOON integers.
func reviewSummaryAXIDoc(s ReviewSummaryAXI) (toon.Object, error) {
	return singleRowAXI("review_summary", reviewSummaryAXIHeader, []any{
		s.ID, s.Dir, s.AgentsSucceeded, s.AgentsTotal, s.AgentsFailed, s.AgentsTimedOut,
		s.APICalls, s.FindingsTotal, s.FindingsCritical, s.FindingsHigh, s.FindingsMedium, s.FindingsLow,
	})
}

// homeViewAXIDoc builds the one-row home document. All four fields are strings.
func homeViewAXIDoc(s HomeViewAXI) (toon.Object, error) {
	return singleRowAXI("home", homeViewAXIHeader, []any{s.ExecPath, s.Description, s.ReviewID, s.ReviewStatus})
}
