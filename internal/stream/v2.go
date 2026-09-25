package stream

import (
	"fmt"
	"io"

	goaxi "github.com/samestrin/go-axi"
)

// VersionV2 is the header of the lossless findings file (findings.toon). The
// body is a go-axi document: a TOON table by default, or the
// {"axi_format":"json",...} envelope when TOON would lose data.
//
// v2 lives in its own file, apart from the v1 pipe writer, so retiring v1 later
// is mostly a delete. Nothing here may call escapeField or fieldReplacer: v2
// substitutes nothing, so '|', quotes, and line breaks survive. The one rewrite
// is go-axi's sanitizer, which runs on both paths and strips control bytes
// (other than tab, LF, CR), U+2028/U+2029, and invalid UTF-8.
const VersionV2 = "# atcr-findings/v2"

// v2Row is one per-source finding in the v2 body. The json tags equal the toon
// tags because go-axi's JSON fallback marshals the row with encoding/json, which
// ignores toon tags; matching them keeps the envelope's keys identical to the
// TOON header's column names.
type v2Row struct {
	Severity   string `toon:"severity" json:"severity"`
	FileLine   string `toon:"file_line" json:"file_line"`
	Problem    string `toon:"problem" json:"problem"`
	Fix        string `toon:"fix" json:"fix"`
	Category   string `toon:"category" json:"category"`
	EstMinutes int    `toon:"est_minutes" json:"est_minutes"`
	Evidence   string `toon:"evidence" json:"evidence"`
	Reviewer   string `toon:"reviewer" json:"reviewer"`
}

type v2Payload struct {
	Findings []v2Row `toon:"findings" json:"findings"`
}

// WriteSourceV2 writes per-source findings (single REVIEWER) as a v2 document.
// There is no reconciled v2 shape: nothing re-reads reconciled/findings.txt,
// and a reviewer list would need its own delimiter rule.
func WriteSourceV2(w io.Writer, findings []Finding) error {
	rows := make([]v2Row, len(findings))
	for i, f := range findings {
		rows[i] = v2Row{
			Severity:   f.Severity,
			FileLine:   fmt.Sprintf("%s:%d", f.File, f.Line),
			Problem:    f.Problem,
			Fix:        f.Fix,
			Category:   f.Category,
			EstMinutes: f.EstMinutes,
			Evidence:   f.Evidence,
			Reviewer:   f.Reviewer,
		}
	}
	return encodeV2(w, v2Payload{Findings: rows})
}

// encodeV2 is the single encode chokepoint for v2 output. It takes any payload
// so a test can drive go-axi's JSON fallback, which real []Finding input never
// reaches.
//
// The TOON-vs-JSON choice is made once for the WHOLE document, not per field:
// one value TOON cannot carry flips every row to the JSON envelope. That is
// go-axi's contract, not a bug, and it is why a reader routes on the literal
// {"axi_format prefix of the body rather than on a leading '{' or '['.
func encodeV2(w io.Writer, payload any) error {
	if _, err := fmt.Fprintln(w, VersionV2); err != nil {
		return fmt.Errorf("writing v2 findings header: %w", err)
	}
	if err := goaxi.EncodeOrJSON(w, payload); err != nil {
		return fmt.Errorf("encoding v2 findings: %w", err)
	}
	return nil
}
