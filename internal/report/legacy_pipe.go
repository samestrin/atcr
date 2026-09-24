package report

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/reconcile"
)

// Legacy pipe-delimited AXI encoder.
//
// Before Epic 35.16.11.1 every --axi payload was a custom TOON variant using the
// pipe as its tabular delimiter (findings[N|]{a|b}:). Code snippets routinely
// carry `|`, `||` and regex alternations, so the default --axi output moved to
// standard TOON via go-axi (standard_axi.go). This file keeps the old encoder
// reachable only through the deprecated fallback — `atcr report --format pipe`,
// `--legacy-pipe` on review/home, or ATCR_LEGACY_PIPE=1 — for consumers not yet
// migrated. Its bytes are frozen by the testdata/legacy_pipe goldens, so nothing
// here may change output; it is slated for removal once the fallback is retired.

// pipeDelim is the legacy TOON tabular-array delimiter: the pipe, chosen in
// Sprint 31.0 so a row sat structurally adjacent to the atcr-findings/v1
// SEVERITY|FILE:LINE|... grammar.
const pipeDelim = '|'

// renderPipeAXI re-encodes findings as the legacy pipe-delimited TOON tabular
// array. The base columns mirror the atcr-findings/v1 reconciled 9-column
// contract; the optional disagreement, verification.*, evidence_exec.*,
// fix_warning and fix_review columns appear only when at least one finding
// carries them, with empty cells for findings that lack the signal.
func renderPipeAXI(w io.Writer, findings []reconcile.JSONFinding) error {
	var b bytes.Buffer
	if len(findings) == 0 {
		b.WriteString("findings[0]:\n")
		_, err := w.Write(b.Bytes())
		return err
	}
	cols := axiColumnsFor(findings)
	header := cols.header()
	quotedHeader := make([]string, len(header))
	for i, h := range header {
		quotedHeader[i] = pipeQuote(h)
	}
	fmt.Fprintf(&b, "findings[%d%c]{%s}:\n", len(findings), pipeDelim, strings.Join(quotedHeader, string(pipeDelim)))
	for _, f := range findings {
		row := pipeRow(f, cols)
		// A row must carry exactly as many columns as the header declares; a
		// mismatch is an internal encoder bug, so fail rather than emit a
		// misaligned payload.
		if len(row) != len(header) {
			return fmt.Errorf("axi encoder: row has %d columns, header declares %d", len(row), len(header))
		}
		b.WriteString("  ")
		b.WriteString(strings.Join(row, string(pipeDelim)))
		b.WriteByte('\n')
	}
	_, err := w.Write(b.Bytes())
	return err
}

// pipeRow encodes one finding into a legacy row. est_minutes, exit_code and
// challenge_survived are bare numbers/booleans; every free-text field goes
// through pipeText. An absent block contributes quoted empty cells.
func pipeRow(f reconcile.JSONFinding, cols axiColumns) []string {
	row := []string{
		pipeText(f.Severity),
		pipeText(fmt.Sprintf("%s:%d", f.File, f.Line)),
		pipeText(f.Problem),
		pipeText(f.Fix),
		pipeText(f.Category),
		strconv.Itoa(f.EstMinutes),
		pipeText(f.Evidence),
		pipeText(strings.Join(f.Reviewers, ",")),
		pipeText(f.Confidence),
	}
	if cols.disagreement {
		row = append(row, pipeText(f.Disagreement))
	}
	if cols.verification {
		if f.Verification != nil {
			row = append(row, pipeText(f.Verification.Verdict), pipeText(f.Verification.Skeptic),
				pipeText(f.Verification.Notes), strconv.FormatBool(f.Verification.ChallengeSurvived))
		} else {
			row = append(row, pipeText(""), pipeText(""), pipeText(""), pipeText(""))
		}
	}
	if cols.evidence {
		if f.EvidenceExec != nil {
			row = append(row, pipeText(f.EvidenceExec.Command), strconv.Itoa(f.EvidenceExec.ExitCode), pipeText(f.EvidenceExec.OutputExcerpt))
		} else {
			row = append(row, pipeText(""), pipeText(""), pipeText(""))
		}
	}
	if cols.fixWarning {
		row = append(row, pipeText(f.FixWarning))
	}
	if cols.fixReview {
		row = append(row, pipeText(f.FixReview))
	}
	return row
}

// pipeText is pipeQuote with the maxTextLen per-cell rune cap.
func pipeText(s string) string { return pipeQuote(truncate(s, maxTextLen)) }

// RenderPipeAXIPaginated is the legacy analogue of RenderAXIPaginated. It keeps
// the pre-migration contract: the rendered payload is line-capped by PaginateAXI,
// so when truncated the header still declares the true total N while fewer rows
// are present, followed by a `truncated: <bool>` line.
func RenderPipeAXIPaginated(w io.Writer, findings []reconcile.JSONFinding, maxLines int) error {
	var buf bytes.Buffer
	if err := renderPipeAXI(&buf, findings); err != nil {
		return err
	}
	out, truncated, _ := PaginateAXI(buf.Bytes(), maxLines)
	if _, err := w.Write(out); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "truncated: %t\n", truncated)
	return err
}

// RenderReviewSummaryPipe writes s as the legacy single-row pipe-delimited
// review_summary[1|]{...}: payload.
func RenderReviewSummaryPipe(w io.Writer, s ReviewSummaryAXI) error {
	return writePipeSingleRow(w, "review_summary", reviewSummaryAXIHeader, []string{
		pipeQuote(s.ID),
		pipeQuote(s.Dir),
		strconv.FormatInt(s.AgentsSucceeded, 10),
		strconv.FormatInt(s.AgentsTotal, 10),
		strconv.FormatInt(s.AgentsFailed, 10),
		strconv.FormatInt(s.AgentsTimedOut, 10),
		strconv.FormatInt(s.APICalls, 10),
		strconv.FormatInt(s.FindingsTotal, 10),
		strconv.FormatInt(s.FindingsCritical, 10),
		strconv.FormatInt(s.FindingsHigh, 10),
		strconv.FormatInt(s.FindingsMedium, 10),
		strconv.FormatInt(s.FindingsLow, 10),
	})
}

// RenderHomeViewPipe writes s as the legacy single-row pipe-delimited
// home[1|]{...}: payload.
func RenderHomeViewPipe(w io.Writer, s HomeViewAXI) error {
	return writePipeSingleRow(w, "home", homeViewAXIHeader, []string{
		pipeQuote(s.ExecPath),
		pipeQuote(s.Description),
		pipeQuote(s.ReviewID),
		pipeQuote(s.ReviewStatus),
	})
}

// writePipeSingleRow writes a legacy one-row tabular array, failing if the row
// width does not match the header.
func writePipeSingleRow(w io.Writer, name string, header, row []string) error {
	if len(row) != len(header) {
		return fmt.Errorf("axi encoder: %s row has %d columns, header declares %d", name, len(row), len(header))
	}
	var b bytes.Buffer
	quotedHeader := make([]string, len(header))
	for i, h := range header {
		quotedHeader[i] = pipeQuote(h)
	}
	fmt.Fprintf(&b, "%s[1%c]{%s}:\n", name, pipeDelim, strings.Join(quotedHeader, string(pipeDelim)))
	b.WriteString("  ")
	b.WriteString(strings.Join(row, string(pipeDelim)))
	b.WriteByte('\n')
	_, err := w.Write(b.Bytes())
	return err
}

// pipeQuote returns s formatted per the legacy must-quote rules. A string is
// quoted when it is empty, has leading/trailing whitespace, equals a reserved
// token (true/false/null), starts with '-', looks like a number, contains the
// pipe delimiter or a TOON special character (: " \ [ ] { }), contains a
// control/separator character, or is invalid UTF-8. Quoting applies the five
// valid TOON escapes (\\ \" \n \r \t), strips every other control byte (ANSI
// \x1b, U+2028/U+2029, …) and writes U+FFFD for each invalid UTF-8 byte.
//
// The U+FFFD replacement is load-bearing: go-axi drops invalid bytes instead, so
// this helper must never delegate to it or the frozen legacy bytes change.
func pipeQuote(s string) string {
	if !pipeMustQuote(s) {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if pipeUnsafeRune(r) {
				continue // no valid TOON escape → strip, never emit a raw control byte
			}
			b.WriteRune(r) // an invalid byte decodes to utf8.RuneError → U+FFFD
		}
	}
	b.WriteByte('"')
	return b.String()
}

// pipeMustQuote reports whether s must be quoted under the legacy pipe
// delimiter. Invalid UTF-8 is checked explicitly: ranging over a raw C1 byte
// (e.g. 8-bit CSI 0x9b) yields U+FFFD, which is not a control rune, so without
// the check the raw byte would be returned unquoted and reach stdout.
func pipeMustQuote(s string) bool {
	if s == "" || strings.TrimSpace(s) != s {
		return true
	}
	switch s {
	case "true", "false", "null":
		return true
	}
	if strings.HasPrefix(s, "-") || looksLikeNumber(s) {
		return true
	}
	if strings.ContainsRune(s, pipeDelim) || strings.ContainsAny(s, ":\"\\[]{}") {
		return true
	}
	if strings.IndexFunc(s, pipeUnsafeRune) >= 0 {
		return true
	}
	return !utf8.ValidString(s)
}

// pipeUnsafeRune reports whether r is a control/separator character TOON cannot
// carry as a raw byte. U+2028/U+2029 are separators, not Unicode "control", so
// they are named explicitly.
func pipeUnsafeRune(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}

// looksLikeNumber reports whether s would be parsed as a number by a conforming
// TOON reader (e.g. "42", "-3.14", "1e-6", "05").
func looksLikeNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
