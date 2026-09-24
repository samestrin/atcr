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
// string (control bytes, ANSI, U+2028/U+2029, invalid UTF-8) before encoding and
// writes nothing on failure. go-axi's typed failures (*KeyCollisionError,
// *CycleError) are wrapped with %w so callers can still reach them via errors.As.
func encodeAXI(w io.Writer, v any) error {
	if err := goaxi.Encode(w, v); err != nil {
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
func renderAXI(w io.Writer, findings []reconcile.JSONFinding) error {
	return encodeAXI(w, axiFindingsPayload{Findings: axiRows(findings)})
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
// payload (only some findings carry a declared block) an absent block gets
// empty-string cells — never a misleading 0 or false — so a consumer must
// tolerate the mixed present/absent form in those columns.
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
		var survived any = ""
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
		var exitCode any = ""
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
// through verbatim (AC2). Sanitizing is left to goaxi.Encode.
func axiText(s string) string { return truncate(s, maxTextLen) }

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
