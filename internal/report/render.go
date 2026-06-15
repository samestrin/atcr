// Package report renders human and machine views over reconciled findings.
// It is the view layer for `atcr report`; the canonical reconciled artifacts are
// written by the reconcile package. report depends on reconcile for the
// findings.json record type (reconcile.JSONFinding).
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/reconcile"
)

// Supported output formats.
const (
	FormatMarkdown  = "md"
	FormatJSON      = "json"
	FormatChecklist = "checklist"
)

// maxTextLen bounds PROBLEM/FIX/EVIDENCE in the md and checklist views; the json
// view is never truncated (AC 01-06 Edge Case 2). File paths are never truncated.
const maxTextLen = 500

// ValidFormat reports whether s names a supported format.
func ValidFormat(s string) bool {
	switch s {
	case FormatMarkdown, FormatJSON, FormatChecklist:
		return true
	default:
		return false
	}
}

// Formats lists the supported formats for error messages.
func Formats() string { return FormatMarkdown + ", " + FormatJSON + ", " + FormatChecklist }

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

// renderMarkdown writes a human report: a severity x confidence summary grid then
// findings grouped by severity. Free text is HTML-escaped and newline-flattened
// (so neither raw HTML nor markdown structure can be injected) and truncated to
// maxTextLen; file paths render verbatim inside backtick code spans (no escape,
// no truncation — preserving unicode paths byte-for-byte, AC 01-06 Edge Case 3).
func renderMarkdown(w io.Writer, findings []reconcile.JSONFinding, df reconcile.DisagreementsFile) error {
	var b bytes.Buffer
	b.WriteString("# atcr Review Report\n\n")
	verified := anyVerification(findings)
	writeSummaryGrid(&b, findings, verified)

	// Disagreement radar above the consensus findings (Epic 3.2). Empty df →
	// nothing written → output identical to the plain report.
	writeRadarSection(&b, df)

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
	sort.SliceStable(sorted, func(i, j int) bool {
		return severityRankOf(sorted[i].Severity) > severityRankOf(sorted[j].Severity)
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
		if f.Disagreement != "" {
			fmt.Fprintf(&b, "  - Severity disagreement: %s\n", esc(f.Disagreement))
		}
		fmt.Fprintf(&b, "  - Problem: %s\n", escTrunc(f.Problem))
		if f.Fix != "" {
			fmt.Fprintf(&b, "  - Fix: %s\n", escTrunc(f.Fix))
		}
		if f.Evidence != "" {
			fmt.Fprintf(&b, "  - Evidence: %s\n", escTrunc(f.Evidence))
		}
		// Skeptic section: only for findings the verify stage touched (AC 06-01
		// Scenario 1). A nil block (v1 finding) renders nothing extra (AC 06-02).
		if f.Verification != nil {
			writeSkepticBlock(&b, f.Verification)
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
	order := []string{reconcile.SevCritical, reconcile.SevHigh, reconcile.SevMedium, reconcile.SevLow}
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
		c, ok := counts[f.Severity]
		if !ok {
			c = otherSev
		}
		switch canonicalize(f.Confidence) {
		case confVerified:
			c.verified++
		case reconcile.ConfHigh:
			c.high++
		case reconcile.ConfMedium:
			c.medium++
		case reconcile.ConfLow:
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
		canonicalize(f.Verification.Verdict) == canonicalize(reconcile.VerdictRefuted)
}

// writeSkepticBlock renders the per-finding Skeptic section: name, verdict, an
// annotation when the verdict is unverifiable, and the reasoning (omitted when
// empty, AC 06-01 Edge Case 3). All free text is HTML-escaped and newline-
// flattened so skeptic output cannot inject markup or escape the section.
func writeSkepticBlock(b *bytes.Buffer, v *reconcile.Verification) {
	annotation := ""
	if canonicalize(v.Verdict) == canonicalize(reconcile.VerdictUnverifiable) {
		annotation = " (skeptic could not verify)"
	}
	fmt.Fprintf(b, "  - Skeptic: %s — %s%s\n", esc(v.Skeptic), esc(v.Verdict), annotation)
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
		fmt.Fprintf(b, "  - Problem: %s\n", escTrunc(f.Problem))
		if f.Verification != nil && strings.TrimSpace(f.Verification.Notes) != "" {
			fmt.Fprintf(b, "  - Reasoning: %s\n", escTrunc(f.Verification.Notes))
		}
	}
	b.WriteString("\n</details>\n")
}

// skepticName returns the skeptic that produced a verdict, or "(unknown)".
func skepticName(v *reconcile.Verification) string {
	if v == nil || strings.TrimSpace(v.Skeptic) == "" {
		return "(unknown)"
	}
	return v.Skeptic
}

// newlineFlattener collapses CR/LF so a field cannot inject markdown structure.
var newlineFlattener = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

// esc flattens newlines then HTML-escapes free text so it renders inert.
// Backticks are also escaped so reviewer-controlled fields cannot open an
// inline code span inside a normal bullet.
func esc(s string) string {
	return strings.ReplaceAll(html.EscapeString(newlineFlattener.Replace(s)), "`", "&#96;")
}

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

// severityRank maps canonical severities to their display ordering. Unknown
// severities sort last (rank 0) so they do not interleave canonical groups.
var severityRank = map[string]int{
	reconcile.SevCritical: 4,
	reconcile.SevHigh:     3,
	reconcile.SevMedium:   2,
	reconcile.SevLow:      1,
}

// severityRankOf returns the display rank for a severity string.
func severityRankOf(s string) int {
	if r, ok := severityRank[s]; ok {
		return r
	}
	return 0
}
