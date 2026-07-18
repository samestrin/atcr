// Package report renders human and machine views over reconciled findings.
// It is the view layer for `atcr report`; the canonical reconciled artifacts are
// written by the reconcile package. report depends on reconcile for the
// findings.json record type (reconcile.JSONFinding).
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
)

// Supported output formats.
const (
	FormatMarkdown  = "md"
	FormatJSON      = "json"
	FormatChecklist = "checklist"
	FormatSarif     = "sarif"
	// FormatAXI is the Agent eXperience Interface output: a token-dense TOON
	// (Token-Optimized Object Notation) re-encoding of the findings for
	// consumption by autonomous agents (--axi). It is a valid CLI --format, so it
	// lives in FormatList/ValidFormat, but it is deliberately excluded from the
	// MCP atcr_report enum (AC 01-05, Phase 2): surfacing a token-frugal format
	// through the token-heavy MCP JSON-RPC envelope would be self-defeating.
	FormatAXI = "axi"
)

// maxTextLen bounds PROBLEM/FIX/EVIDENCE in the md and checklist views; the json
// view is never truncated (AC 01-06 Edge Case 2). File paths are never truncated.
const maxTextLen = 500

// ValidFormat reports whether s names a supported format.
func ValidFormat(s string) bool {
	switch s {
	case FormatMarkdown, FormatJSON, FormatChecklist, FormatSarif, FormatAXI:
		return true
	default:
		return false
	}
}

// FormatList returns the supported output formats as a slice. It is the single
// source of truth for human-readable listings and CLI --format validation. The
// MCP schema enum derives from this list MINUS FormatAXI (AC 01-05, Phase 2):
// axi is a CLI-only format.
func FormatList() []string {
	return []string{FormatMarkdown, FormatJSON, FormatChecklist, FormatSarif, FormatAXI}
}

// Formats lists the supported formats for error messages.
func Formats() string {
	return strings.Join(FormatList(), ", ")
}

// Render writes findings to w in the given format. An unknown format is an error
// (the caller validates first; this is the defensive backstop).
func Render(w io.Writer, findings []reconcile.JSONFinding, format string) error {
	switch format {
	case FormatMarkdown:
		// The plain markdown report carries no radar section (empty
		// DisagreementsFile); callers that want the radar use
		// RenderMarkdownWithDisagreements. This keeps Render's md output
		// byte-identical to its pre-3.2 form for every existing caller.
		return renderMarkdown(w, findings, reconcile.DisagreementsFile{})
	case FormatJSON:
		return renderJSON(w, findings)
	case FormatChecklist:
		return renderChecklist(w, findings)
	case FormatSarif:
		return renderSarif(w, findings)
	case FormatAXI:
		return renderAXI(w, findings)
	default:
		return fmt.Errorf("unknown format %q: supported formats are %s", format, Formats())
	}
}

// renderJSON re-emits the findings as indented JSON, never truncated — the
// machine contract for downstream tooling.
func renderJSON(w io.Writer, findings []reconcile.JSONFinding) error {
	if findings == nil {
		findings = []reconcile.JSONFinding{}
	}
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// axiDelim is the TOON tabular-array delimiter for the axi payload: the pipe,
// chosen so a row is visually and structurally adjacent to the existing
// atcr-findings/v1 SEVERITY|FILE:LINE|... grammar rather than fragmenting the
// machine-format surface (AC 01-01 Edge Case 2; toon-format-reference.md).
const axiDelim = '|'

// renderAXI re-encodes findings as a TOON (Token-Optimized Object Notation)
// tabular array — the token-dense machine payload for the agent-experience
// (--axi) mode. The base columns mirror the atcr-findings/v1 reconciled 9-column
// contract (SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|
// REVIEWERS|CONFIDENCE) field-for-field, so the axi payload is a re-encoding of
// the same machine contract a --format json consumer sees, not a new schema.
//
// The array header declares its element count (findings[N|]{...}:) and the pipe
// delimiter. Free-text fields are quoted per TOON's must-quote rules (toonQuote);
// only the five valid TOON escapes (\\ \" \n \r \t) are ever emitted and every
// other control byte (ANSI \x1b, U+2028/U+2029, …) is stripped, so a raw escape
// sequence can never ride the payload — the structural half of the axi no-ANSI
// guarantee (AC 01-01 Security).
//
// axi.md design-tension resolutions (AC 01-02 Scenario 3):
//   - Principle 2 ("3–4 default fields") is deliberately NOT applied — the full
//     9-column field set is retained because pipe-delimited TOON rows are already
//     token-lean, and dropping columns would make axi a lossy subset of the JSON
//     contract rather than a faithful re-encoding.
//   - Principle 4 ("pre-computed aggregates") is honored via the array header's
//     declared true total N (independent of emitted row count once Story 3's
//     pagination caps it) plus the run metadata carried on the review path
//     (AC 01-03) — not a separate aggregation pass here.
//
// The optional per-finding signals — a severity Disagreement annotation and the
// Verification / EvidenceExec blocks — are surfaced as additive columns
// (disagreement, verification.*, evidence_exec.*) only when at least one finding
// carries them, so a plain findings list stays at the 9-column width and
// byte-identical to the pre-verification form — the same omitempty discipline the
// JSON contract uses. When present, findings lacking a signal get empty cells so
// every row keeps the header's declared width. This keeps the axi payload a
// superset — never a lossy subset — of the JSON form.
func renderAXI(w io.Writer, findings []reconcile.JSONFinding) error {
	var b bytes.Buffer
	if len(findings) == 0 {
		// TOON empty-array form: a well-formed payload for a zero-findings review,
		// not an error or a human "No findings." sentence (AC 01-01 Edge Case 1).
		b.WriteString("findings[0]:\n")
		_, err := w.Write(b.Bytes())
		return err
	}
	hasDisagreement, hasVerification, hasEvidence := false, false, false
	for _, f := range findings {
		if f.Disagreement != "" {
			hasDisagreement = true
		}
		if f.Verification != nil {
			hasVerification = true
		}
		if f.EvidenceExec != nil {
			hasEvidence = true
		}
	}
	header := []string{"severity", "file:line", "problem", "fix", "category", "est_minutes", "evidence", "reviewers", "confidence"}
	if hasDisagreement {
		header = append(header, "disagreement")
	}
	if hasVerification {
		header = append(header, "verification.verdict", "verification.skeptic", "verification.notes", "verification.challenge_survived")
	}
	if hasEvidence {
		header = append(header, "evidence_exec.command", "evidence_exec.exit_code", "evidence_exec.output_excerpt")
	}
	quotedHeader := make([]string, len(header))
	for i, h := range header {
		quotedHeader[i] = toonQuote(h)
	}
	fmt.Fprintf(&b, "findings[%d%c]{%s}:\n", len(findings), axiDelim, strings.Join(quotedHeader, string(axiDelim)))
	for _, f := range findings {
		row := axiRow(f, hasDisagreement, hasVerification, hasEvidence)
		// Defensive invariant (AC 01-02 Error Scenario 1): a row must carry exactly
		// as many columns as the header declares. A mismatch is an internal encoder
		// bug, never user input — fail deterministically rather than emit a
		// structurally malformed payload a consumer would misalign.
		if len(row) != len(header) {
			return fmt.Errorf("axi encoder: row has %d columns, header declares %d", len(row), len(header))
		}
		b.WriteString("  ")
		b.WriteString(strings.Join(row, string(axiDelim)))
		b.WriteByte('\n')
	}
	_, err := w.Write(b.Bytes())
	return err
}

// axiRow encodes one finding into a TOON row: the base 9 columns mirroring the
// atcr-findings/v1 reconciled column order, plus the additive disagreement,
// verification.* and evidence_exec.* columns when the payload declares them.
// FILE:LINE is one combined column (as in v1); est_minutes, exit_code and
// challenge_survived are emitted as bare numbers/booleans; every free-text field
// is routed through toonQuote. A finding missing a declared signal contributes
// empty cells (an empty exit_code cell, never a misleading 0) so the row keeps the
// header width.
func axiRow(f reconcile.JSONFinding, hasDisagreement, hasVerification, hasEvidence bool) []string {
	row := []string{
		toonQuote(f.Severity),
		toonQuote(fmt.Sprintf("%s:%d", f.File, f.Line)),
		toonQuote(f.Problem),
		toonQuote(f.Fix),
		toonQuote(f.Category),
		strconv.Itoa(f.EstMinutes),
		toonQuote(f.Evidence),
		toonQuote(strings.Join(f.Reviewers, ",")),
		toonQuote(f.Confidence),
	}
	if hasDisagreement {
		row = append(row, toonQuote(f.Disagreement))
	}
	if hasVerification {
		if f.Verification != nil {
			// challenge_survived is emitted as a bare TOON boolean; an absent block
			// gets an empty cell (distinct from a real false) so the additive block
			// stays a faithful superset of the JSON verification object.
			row = append(row, toonQuote(f.Verification.Verdict), toonQuote(f.Verification.Skeptic),
				toonQuote(f.Verification.Notes), strconv.FormatBool(f.Verification.ChallengeSurvived))
		} else {
			row = append(row, toonQuote(""), toonQuote(""), toonQuote(""), toonQuote(""))
		}
	}
	if hasEvidence {
		if f.EvidenceExec != nil {
			row = append(row, toonQuote(f.EvidenceExec.Command), strconv.Itoa(f.EvidenceExec.ExitCode), toonQuote(f.EvidenceExec.OutputExcerpt))
		} else {
			row = append(row, toonQuote(""), toonQuote(""), toonQuote(""))
		}
	}
	return row
}

// ReviewSummaryAXI is the run-level metadata carried by the --axi review/resume
// summary payload: review identity plus per-attempt agent counts and a findings
// total — the token-dense analogue of the human end-of-review summary block
// (cmd/atcr/review_summary.go). It is deliberately distinct from the findings
// table renderAXI emits: a bare `atcr review --axi` runs no reconcile stage, so it
// has a run summary but no findings list. The two payloads share this package's one
// TOON encoder (toonQuote/axiDelim) rather than a second, divergent serializer
// (AC 01-03; sprint-design Architecture).
type ReviewSummaryAXI struct {
	ID              string
	Dir             string
	AgentsSucceeded int64
	AgentsTotal     int64
	AgentsFailed    int64
	AgentsTimedOut  int64
	APICalls        int64
	FindingsTotal   int64
}

// reviewSummaryAXIHeader is the fixed column order of the review-summary payload.
// Kept as one slice so the header line and the row are guaranteed the same width
// and order (the same defensive invariant renderAXI enforces for findings).
var reviewSummaryAXIHeader = []string{
	"id", "dir", "agents_succeeded", "agents_total",
	"agents_failed", "agents_timed_out", "api_calls", "findings_total",
}

// RenderReviewSummaryAXI writes s as a single-row TOON tabular array
// (review_summary[1|]{...}:) reusing the axi findings encoder's pipe delimiter,
// must-quote rules and control-byte stripping (toonQuote), so the review-summary
// payload carries the same no-ANSI / no-Markdown structural guarantee as
// renderAXI and stays byte-identical between `atcr review --axi` and
// `atcr resume --axi` for equivalent data (AC 01-03/01-04). Free-text identity
// fields are quoted; counts are emitted as bare TOON integers.
func RenderReviewSummaryAXI(w io.Writer, s ReviewSummaryAXI) error {
	var b bytes.Buffer
	quotedHeader := make([]string, len(reviewSummaryAXIHeader))
	for i, h := range reviewSummaryAXIHeader {
		quotedHeader[i] = toonQuote(h)
	}
	fmt.Fprintf(&b, "review_summary[1%c]{%s}:\n", axiDelim, strings.Join(quotedHeader, string(axiDelim)))
	row := []string{
		toonQuote(s.ID),
		toonQuote(s.Dir),
		strconv.FormatInt(s.AgentsSucceeded, 10),
		strconv.FormatInt(s.AgentsTotal, 10),
		strconv.FormatInt(s.AgentsFailed, 10),
		strconv.FormatInt(s.AgentsTimedOut, 10),
		strconv.FormatInt(s.APICalls, 10),
		strconv.FormatInt(s.FindingsTotal, 10),
	}
	b.WriteString("  ")
	b.WriteString(strings.Join(row, string(axiDelim)))
	b.WriteByte('\n')
	_, err := w.Write(b.Bytes())
	return err
}

// toonQuote returns s formatted per TOON's must-quote rules. A string is quoted
// (with the five valid TOON escapes applied, all other control bytes stripped)
// when it is empty, has leading/trailing whitespace, equals a reserved token
// (true/false/null), looks like a number, equals or starts with '-', contains a
// TOON special character (: " \ [ ] { }), contains a control character, or
// contains the active delimiter. Otherwise it is returned verbatim — unicode and
// emoji are safe unquoted.
func toonQuote(s string) string {
	if toonMustQuote(s) {
		return toonEscape(s)
	}
	return s
}

// toonMustQuote reports whether s must be quoted under the axi delimiter, per the
// full TOON must-quote set (toon-format-reference.md). Quoting a value that
// equals a reserved token, looks like a number, or starts with '-' is what keeps
// the axi payload a faithful re-encoding of the findings: without it a conforming
// TOON parser would read a string field back as a bool/null/number and break the
// round-trip contract renderAXI promises.
func toonMustQuote(s string) bool {
	if s == "" {
		return true
	}
	if strings.TrimSpace(s) != s {
		return true
	}
	// Reserved tokens (case-sensitive) and number-like strings would be read back
	// as a non-string type unless quoted.
	switch s {
	case "true", "false", "null":
		return true
	}
	if strings.HasPrefix(s, "-") { // equals or starts with '-'
		return true
	}
	if looksLikeNumber(s) {
		return true
	}
	if strings.ContainsRune(s, axiDelim) {
		return true
	}
	// TOON special characters that force quoting regardless of the delimiter.
	if strings.ContainsAny(s, ":\"\\[]{}") {
		return true
	}
	// Any control character (newline/CR/tab and the escape-less ones like \x1b)
	// forces quoting; toonEscape then escapes the representable ones and strips
	// the rest. U+2028/U+2029 are separators, not Unicode "control", so are
	// checked explicitly (mirroring cmd/atcr sanitizeDisplay).
	if strings.IndexFunc(s, isTOONControl) >= 0 {
		return true
	}
	return false
}

// looksLikeNumber reports whether s would be parsed as a number by a conforming
// TOON reader (e.g. "42", "-3.14", "1e-6", "05"), in which case a string field
// holding that value must be quoted to survive the round-trip.
func looksLikeNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// isTOONControl reports whether r is a control/separator character that TOON
// cannot carry as a raw byte.
func isTOONControl(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}

// toonEscape returns s wrapped in double quotes with the five valid TOON escape
// sequences applied (\\ \" \n \r \t). TOON defines no \x/\u escape, so any other
// control byte (e.g. a raw ANSI \x1b) is dropped rather than smuggled through as
// a raw byte — this is what makes the payload structurally escape-free.
func toonEscape(s string) string {
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
			if isTOONControl(r) {
				continue // no valid TOON escape → strip, never emit a raw control byte
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// renderMarkdown writes a human report: a severity x confidence summary grid then
// findings grouped by severity. Free text is HTML-escaped and newline-flattened
// (so neither raw HTML nor markdown structure can be injected) and truncated to
// maxTextLen; file paths render verbatim inside backtick code spans (no escape,
// no truncation — preserving unicode paths byte-for-byte, AC 01-06 Edge Case 3).
func renderMarkdown(w io.Writer, findings []reconcile.JSONFinding, df reconcile.DisagreementsFile) error {
	return renderMarkdownFull(w, findings, df, ContestedReport{})
}

// renderMarkdownFull is the markdown renderer with the optional contested-findings
// section (Epic 6.0). An empty ContestedReport writes no contested section, so the
// output is byte-identical to the pre-6.0 report for every existing caller.
func renderMarkdownFull(w io.Writer, findings []reconcile.JSONFinding, df reconcile.DisagreementsFile, cr ContestedReport) error {
	var b bytes.Buffer
	b.WriteString("# atcr Review Report\n\n")
	verified := anyVerification(findings)
	writeSummaryGrid(&b, findings, verified)

	// Disagreement radar above the consensus findings (Epic 3.2). Empty df →
	// nothing written → output identical to the plain report. The display report
	// passes escTrunc (500-rune cap) to the shared reconcile renderer; the
	// reconciled report.md passes esc (verbatim) through the same code path.
	reconcile.WriteRadarSection(&b, df, escTrunc)

	// Contested-findings section (Epic 6.0): judge rulings over debated disputes.
	// Empty cr → nothing written → output identical to the pre-6.0 report.
	writeContestedSection(&b, cr)

	if len(findings) == 0 {
		b.WriteString("\nNo findings.\n")
		_, err := w.Write(b.Bytes())
		return err
	}

	// Refuted findings are demoted out of the main list and shown only in the
	// collapsed Refuted section at the bottom (AC 06-01 Edge Case 2). When no
	// finding carries a verification block this partition is skipped and the
	// output is byte-identical to the pre-Epic-3.0 report (AC 06-02).
	main, refuted := findings, []reconcile.JSONFinding(nil)
	if verified {
		main = make([]reconcile.JSONFinding, 0, len(findings))
		for _, f := range findings {
			if isRefuted(f) {
				refuted = append(refuted, f)
			} else {
				main = append(main, f)
			}
		}
	}

	// Render severity groups in a fixed canonical order regardless of input
	// ordering. This prevents duplicate headers when findings.json is hand-edited
	// or produced by an external source (TD item: main-list severity ordering).
	sorted := make([]reconcile.JSONFinding, len(main))
	copy(sorted, main)
	// Precompute rank per unique severity to avoid per-comparison string allocations in the sort closure.
	rankCache := make(map[string]int, 4)
	for _, f := range sorted {
		if _, ok := rankCache[f.Severity]; !ok {
			rankCache[f.Severity] = severityRankOf(f.Severity)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		return rankCache[sorted[i].Severity] > rankCache[sorted[j].Severity]
	})
	main = sorted

	b.WriteString("\n## Findings\n")
	if len(main) == 0 {
		b.WriteString("\nAll findings were refuted — see the Refuted Findings section below.\n")
	}
	lastSev := ""
	for _, f := range main {
		if f.Severity != lastSev {
			fmt.Fprintf(&b, "\n### %s\n\n", esc(f.Severity))
			lastSev = f.Severity
		}
		fmt.Fprintf(&b, "- %s — confidence %s, reviewers: %s\n",
			codeSpan(f.File, f.Line), esc(f.Confidence), esc(joinReviewers(f.Reviewers)))
		writePathWarning(&b, f)
		if f.Disagreement != "" {
			fmt.Fprintf(&b, "  - Severity disagreement: %s\n", esc(f.Disagreement))
		}
		fmt.Fprintf(&b, "  - Problem: %s\n", escTrunc(f.Problem))
		if f.Fix != "" {
			fmt.Fprintf(&b, "  - Fix: %s\n", escTrunc(f.Fix))
		}
		// Fix-generation warning (Epic 7.0 fix_warning, incl. the 7.1 invalid_syntax
		// flag): surface it so a flagged or absent fix is visible to the reader.
		if f.FixWarning != "" {
			fmt.Fprintf(&b, "  - ⚠️ Fix warning: %s\n", escTrunc(f.FixWarning))
		}
		if f.Evidence != "" {
			fmt.Fprintf(&b, "  - Evidence: %s\n", escTrunc(f.Evidence))
		}
		// Skeptic section: only for findings the verify stage touched (AC 06-01
		// Scenario 1). A nil block (v1 finding) renders nothing extra (AC 06-02).
		if f.Verification != nil {
			writeSkepticBlock(&b, f.Verification)
		}
		// Execution-reproduction badge (Epic 11.0): a finding carrying an
		// evidence_exec block was demonstrated by running code in the sandbox.
		// Gate the badge on an actually-reproduced failure: a confirmed verdict
		// AND a non-zero exit code. repro.Stamp attaches EvidenceExec even on an
		// unverifiable verdict (timeout, disagreeing exits, both-zero deterministic
		// PASS), so without this guard a finding that did NOT reproduce would render
		// a green "Reproduced: cmd (exit 0)" badge — a lie to the operator.
		if f.EvidenceExec != nil && f.EvidenceExec.ExitCode != 0 &&
			f.Verification != nil &&
			canonicalize(f.Verification.Verdict) == canonicalize(reclib.VerdictConfirmed) {
			writeReproducedBlock(&b, f.EvidenceExec)
		}
	}
	writeRefutedSection(&b, refuted)
	_, err := w.Write(b.Bytes())
	return err
}

// RenderMarkdownWithDisagreements writes the standard markdown report with the
// disagreement radar injected above the consensus findings (Epic 3.2). When df
// has no items the output is byte-identical to the plain markdown report, so a
// review with no disagreements is unchanged.
func RenderMarkdownWithDisagreements(w io.Writer, findings []reconcile.JSONFinding, df reconcile.DisagreementsFile) error {
	return renderMarkdown(w, findings, df)
}

// renderChecklist writes a render-only markdown checkbox list — one "- [ ]" item
// per finding, no numbering, no persistence, no state (AC 01-06). Suitable for
// pasting into a PR comment.
func renderChecklist(w io.Writer, findings []reconcile.JSONFinding) error {
	var b bytes.Buffer
	b.WriteString("# Review Checklist\n\n")
	if len(findings) == 0 {
		b.WriteString("No findings.\n")
		_, err := w.Write(b.Bytes())
		return err
	}
	for _, f := range findings {
		fmt.Fprintf(&b, "- [ ] **%s** %s — %s (confidence: %s)\n",
			esc(f.Severity), codeSpan(f.File, f.Line), escTrunc(f.Problem), esc(f.Confidence))
		writePathWarning(&b, f)
	}
	_, err := w.Write(b.Bytes())
	return err
}

// writeSummaryGrid writes the counts-by-severity x confidence grid plus totals.
// When verified is true (any finding carries a verification block) the grid gains
// a leftmost VERIFIED column, reflecting the v2 ordering VERIFIED > HIGH > MEDIUM >
// LOW. When false it renders the exact pre-Epic-3.0 four-column grid (AC 06-02): no
// finding has VERIFIED confidence in that case, so the count would be zero anyway.
func writeSummaryGrid(b *bytes.Buffer, findings []reconcile.JSONFinding, verified bool) {
	type cell struct{ verified, high, medium, low, other int }
	order := []string{reclib.SevCritical, reclib.SevHigh, reclib.SevMedium, reclib.SevLow}
	counts := map[string]*cell{}
	for _, s := range order {
		counts[s] = &cell{}
	}
	refutedCount := 0
	otherSev := &cell{}
	for _, f := range findings {
		if verified && isRefuted(f) {
			refutedCount++
		}
		c, ok := counts[canonicalize(f.Severity)]
		if !ok {
			c = otherSev
		}
		switch canonicalize(f.Confidence) {
		case confVerified:
			c.verified++
		case reclib.ConfHigh:
			c.high++
		case reclib.ConfMedium:
			c.medium++
		case reclib.ConfLow:
			c.low++
		default:
			c.other++
		}
	}
	// Show the VERIFIED column when the verify stage ran (param) OR when any
	// finding actually carries VERIFIED confidence. The latter guards a desync:
	// a finding with VERIFIED confidence but a nil verification block (a writer
	// contract violation) would otherwise be counted in the total yet vanish
	// from every column of the v1 grid. Pure v1 input has neither, so the
	// four-column grid is rendered byte-identically (AC 06-02).
	totalVerified := 0
	for _, s := range order {
		totalVerified += counts[s].verified
	}
	totalVerified += otherSev.verified
	hasOtherConf := false
	for _, s := range order {
		hasOtherConf = hasOtherConf || counts[s].other > 0
	}
	hasOtherConf = hasOtherConf || otherSev.other > 0
	showVerified := verified || totalVerified > 0

	if refutedCount > 0 {
		fmt.Fprintf(b, "Total findings: %d (%d refuted, shown below)\n\n", len(findings), refutedCount)
	} else {
		fmt.Fprintf(b, "Total findings: %d\n\n", len(findings))
	}

	headers := []string{"Severity"}
	if showVerified {
		headers = append(headers, "VERIFIED conf")
	}
	headers = append(headers, "HIGH conf", "MEDIUM conf", "LOW conf")
	if hasOtherConf {
		headers = append(headers, "OTHER conf")
	}
	seps := make([]string, len(headers))
	for i, h := range headers {
		seps[i] = strings.Repeat("-", len(h)+2)
	}
	fmt.Fprintf(b, "| %s |\n", strings.Join(headers, " | "))
	fmt.Fprintf(b, "|%s|\n", strings.Join(seps, "|"))

	writeRow := func(label string, c *cell) {
		vals := []string{label}
		if showVerified {
			vals = append(vals, strconv.Itoa(c.verified))
		}
		vals = append(vals, strconv.Itoa(c.high), strconv.Itoa(c.medium), strconv.Itoa(c.low))
		if hasOtherConf {
			vals = append(vals, strconv.Itoa(c.other))
		}
		fmt.Fprintf(b, "| %s |\n", strings.Join(vals, " | "))
	}

	for _, s := range order {
		writeRow(s, counts[s])
	}
	if otherSev.verified+otherSev.high+otherSev.medium+otherSev.low+otherSev.other > 0 {
		writeRow("OTHER", otherSev)
	}
}

// confVerified is the confidence-v2 tier a skeptic-confirmed finding carries in
// findings.json. The verify stage owns the v2 axis and writes this token into
// Confidence; the report renders it verbatim. Defined locally so the view layer
// does not import the verify package.
const confVerified = "VERIFIED"

// anyVerification reports whether any finding carries a verification block, which
// switches the renderer into v2 mode (VERIFIED grid column, Skeptic sections,
// collapsed Refuted section). With none, output is byte-identical to v1.
func anyVerification(findings []reconcile.JSONFinding) bool {
	for _, f := range findings {
		if f.Verification != nil {
			return true
		}
	}
	return false
}

// isRefuted reports whether a skeptic refuted the finding (case-insensitive, the
// same normalization the gate and confidence-v2 mapping use).
func isRefuted(f reconcile.JSONFinding) bool {
	return f.Verification != nil &&
		canonicalize(f.Verification.Verdict) == canonicalize(reclib.VerdictRefuted)
}

// writePathWarning emits the hallucinated-path warning line for a finding whose
// file failed existence validation (Epic 5.0). A no-op when the path is valid or
// was never validated (PathWarning empty). When the candidate index produced a
// correction (Epic 5.4), a "(did you mean …)" clause points at the real file
// while the original cited path is preserved. Both paths are HTML-escaped so a
// reviewer-controlled path cannot inject markup; this single helper backs every
// report view (markdown, checklist, and the refuted section).
func writePathWarning(b *bytes.Buffer, f reconcile.JSONFinding) {
	if f.PathWarning == "" {
		return
	}
	// Default to the canonical "File not found" label for the standard
	// PathNotFoundWarning value (keeping the human report byte-stable), but render
	// the actual PathWarning for any non-default warning so the human report
	// tracks the machine field (path_warning) instead of a frozen string.
	label := "File not found"
	if f.PathWarning != stream.PathNotFoundWarning {
		label = esc(f.PathWarning)
	}
	if f.PathSuggestion != "" {
		fmt.Fprintf(b, "  - ⚠️ %s: %s (did you mean %s?)\n", label, esc(f.File), esc(f.PathSuggestion))
		return
	}
	fmt.Fprintf(b, "  - ⚠️ %s: %s\n", label, esc(f.File))
}

// writeSkepticBlock renders the per-finding verdict-attribution section: the
// agent name, verdict, an annotation when the verdict is unverifiable, and the
// reasoning (omitted when empty, AC 06-01 Edge Case 3). For findings that
// survived cross-examination (ChallengeSurvived) the agent is the judge and is
// labelled "Judge" so it is not mistaken for a skeptic-produced verdict.
// All free text is HTML-escaped and newline-flattened so reviewer-controlled
// fields cannot inject markup or escape the section.
// writeReproducedBlock renders the execution-reproduction evidence (Epic 11.0)
// as a "Reproduced" badge: the command that was run, its exit code, and a
// truncated output excerpt. It is rendered as a LABEL (not a new verdict tier) —
// a reproduced finding is already VERIFIED via its confirmed verdict — so the
// library Verification type stays unchanged. The output excerpt is escaped and
// truncated like every other free-text field.
func writeReproducedBlock(b *bytes.Buffer, e *reconcile.EvidenceExec) {
	fmt.Fprintf(b, "  - ✅ Reproduced: %s (exit %d)\n", codeSpanText(e.Command), e.ExitCode)
	if strings.TrimSpace(e.OutputExcerpt) != "" {
		fmt.Fprintf(b, "    - Output: %s\n", escTrunc(e.OutputExcerpt))
	}
}

func writeSkepticBlock(b *bytes.Buffer, v *reclib.Verification) {
	annotation := ""
	if canonicalize(v.Verdict) == canonicalize(reclib.VerdictUnverifiable) {
		annotation = " (skeptic could not verify)"
	}
	label := "Skeptic"
	if v.ChallengeSurvived {
		label = "Judge"
	}
	fmt.Fprintf(b, "  - %s: %s — %s%s\n", label, esc(v.Skeptic), esc(v.Verdict), annotation)
	if strings.TrimSpace(v.Notes) != "" {
		fmt.Fprintf(b, "    - Reasoning: %s\n", escTrunc(v.Notes))
	}
}

// writeRefutedSection renders refuted findings in a collapsed <details> block at
// the bottom of the report (AC 06-01 Scenario 2). Omitted entirely when none are
// refuted (Edge Case 1). A refuted finding is never deleted — it stays in the
// report so a wrong refutation is visible to the human. The collapsed view is
// intentionally abbreviated to the AC 06-01 Scenario 2 field set (file:line,
// confidence, skeptic, problem, reasoning); Fix/Evidence are not repeated here.
// The <details>/<summary> tags are static; every dynamic field is routed through
// esc()/escTrunc().
func writeRefutedSection(b *bytes.Buffer, refuted []reconcile.JSONFinding) {
	if len(refuted) == 0 {
		return
	}
	b.WriteString("\n## Refuted Findings\n\n")
	fmt.Fprintf(b, "<details>\n<summary>Refuted Findings (%d)</summary>\n\n", len(refuted))
	for _, f := range refuted {
		fmt.Fprintf(b, "- %s — confidence %s, skeptic: %s\n",
			codeSpan(f.File, f.Line), esc(f.Confidence), esc(skepticName(f.Verification)))
		writePathWarning(b, f)
		fmt.Fprintf(b, "  - Problem: %s\n", escTrunc(f.Problem))
		if f.Verification != nil && strings.TrimSpace(f.Verification.Notes) != "" {
			fmt.Fprintf(b, "  - Reasoning: %s\n", escTrunc(f.Verification.Notes))
		}
	}
	b.WriteString("\n</details>\n")
}

// skepticName returns the skeptic that produced a verdict, or "(unknown)".
func skepticName(v *reclib.Verification) string {
	if v == nil || strings.TrimSpace(v.Skeptic) == "" {
		return "(unknown)"
	}
	return v.Skeptic
}

// esc delegates to reconcile.Esc so the reconciled-report and display-report
// escaping contracts share a single source of truth and cannot drift apart.
func esc(s string) string { return reconcile.Esc(s) }

// escTrunc truncates to maxTextLen runes (with an ellipsis) then escapes.
func escTrunc(s string) string { return esc(truncate(s, maxTextLen)) }

// truncate shortens s to at most n runes, appending "..." when it was longer.
// Rune-based so multibyte characters are never split. Guarded against n < 3 so
// the ellipsis math can never underflow the slice bound.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	if n < 3 {
		if n < 0 {
			n = 0
		}
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}

// codeSpan renders FILE:LINE inside a backtick code span so a normal path is
// byte-identical (unicode-safe). A path containing a backtick (a valid filename
// character) would close the span and let trailing text inject live
// markdown/HTML, so such paths — and any with CR/LF — fall back to HTML-escaping
// instead. Byte-identity is preserved for every path that does not contain a
// backtick or newline (the overwhelming common case).
func codeSpan(file string, line int) string {
	if strings.ContainsRune(file, '`') || strings.ContainsAny(file, "\r\n") {
		return esc(fmt.Sprintf("%s:%d", file, line))
	}
	return fmt.Sprintf("`%s:%d`", file, line)
}

// codeSpanText renders s inside a backtick code span, mirroring codeSpan. It
// keeps the raw text for the common case and falls back to HTML-escaping only
// when s contains a backtick or newline (which would break the span).
func codeSpanText(s string) string {
	if strings.ContainsRune(s, '`') || strings.ContainsAny(s, "\r\n") {
		return esc(s)
	}
	return fmt.Sprintf("`%s`", s)
}

// joinReviewers joins reviewer names with ", " or returns "(none)". Reviewer
// names are assumed not to contain commas; if that assumption is ever violated
// the rendered list becomes ambiguous. Callers that need comma-safe output
// should join with a non-comma delimiter (or escape each name individually and
// use a delimiter that cannot appear in a name).
func joinReviewers(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// canonicalize normalizes a free-text token to a trimmed, upper-cased form so
// that mixed-case or padded enum values match the canonical constants used by
// the report layer.
func canonicalize(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// severityRankOf returns the display rank for a severity string using the
// canonical rank owned by internal/stream (NormalizeSeverity-keyed) so the
// report view and the radar sort never drift, even on mixed-case input.
func severityRankOf(s string) int {
	if r, ok := reclib.SeverityRank[reclib.NormalizeSeverity(s)]; ok {
		return r
	}
	return 0
}
