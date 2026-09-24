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
	// FormatPipe is the deprecated legacy pipe-delimited AXI encoding
	// (findings[N|]{...}:), kept as a temporary fallback for consumers not yet on
	// standard TOON. Like FormatAXI it is CLI-only: the MCP allow list excludes it.
	FormatPipe = "pipe"
)

// maxTextLen bounds PROBLEM/FIX/EVIDENCE in the md and checklist views; the json
// view is never truncated (AC 01-06 Edge Case 2). File paths are never truncated.
const maxTextLen = 500

// ValidFormat reports whether s names a supported format.
func ValidFormat(s string) bool {
	switch s {
	case FormatMarkdown, FormatJSON, FormatChecklist, FormatSarif, FormatAXI, FormatPipe:
		return true
	default:
		return false
	}
}

// FormatList returns the supported output formats as a slice. It is the single
// source of truth for human-readable listings and CLI --format validation. The
// MCP schema enum is a separate explicit allow list (internal/mcp
// mcpAllowedFormats) that excludes FormatAXI and FormatPipe (AC 01-05): both are
// CLI-only formats, and any future CLI-only format added here stays off the MCP surface
// unless deliberately opted in there.
func FormatList() []string {
	return []string{FormatMarkdown, FormatJSON, FormatChecklist, FormatSarif, FormatAXI, FormatPipe}
}

// Formats lists the supported formats for error messages.
func Formats() string {
	return strings.Join(FormatList(), ", ")
}

// Render writes findings to w in the given format. An unknown format is an error
// (the caller validates first; this is the defensive backstop).
//
// FormatPipe is deprecated and Render emits no notice for it: the deprecation
// warning is the caller's job, because only the caller knows its output sinks.
// The CLI (cli/report.go) writes it to stderr; any other caller selecting
// FormatPipe must surface its own notice.
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
		// Base Render(FormatAXI) is the PURE schema encoder: it emits every finding
		// uncapped and adds no truncated flag, so it stays the byte-stable schema
		// fixture (the report.axi golden) and never silently drops rows. Pagination
		// (AC 03-01) and the `truncated` flag (AC 03-02) are the FormatAXI *dispatch
		// path*'s concern, applied by RenderAXIPaginated — the single shared step the
		// CLI commands (report/review --axi) route through (AC 03-04, wired in
		// Phase 3 element 4). Capping here instead would silently truncate any
		// Render(FormatAXI) caller with no signal (3.5.A adversarial finding).
		return renderAXI(w, findings)
	case FormatPipe:
		// The legacy pipe encoder, uncapped — the same schema-fixture role
		// Render(FormatAXI) plays for the standard path. The CLI paginates via
		// RenderPipeAXIPaginated.
		return renderPipeAXI(w, findings)
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

// axiColumns records which optional per-finding signals a payload declares. A
// column appears only when at least one finding carries the signal, so a plain
// findings list stays at the 9-column width — the same omitempty discipline the
// JSON contract uses. Shared by the standard and legacy pipe encoders — though
// only these boolean flags are shared: the column names/order are duplicated in
// header() (legacy) and axiRow's literal keys (standard), an equality pinned by
// TestAXIRowKeysMatchColumnHeader rather than guaranteed by construction.
type axiColumns struct {
	disagreement, verification, evidence, fixWarning, fixReview bool
}

// axiColumnsFor scans findings for the optional signals.
func axiColumnsFor(findings []reconcile.JSONFinding) axiColumns {
	var c axiColumns
	for _, f := range findings {
		c.disagreement = c.disagreement || f.Disagreement != ""
		c.verification = c.verification || f.Verification != nil
		c.evidence = c.evidence || f.EvidenceExec != nil
		c.fixWarning = c.fixWarning || f.FixWarning != ""
		c.fixReview = c.fixReview || f.FixReview != ""
	}
	return c
}

// header returns the ordered column names: the base 9 columns mirroring the
// atcr-findings/v1 reconciled contract, then the additive disagreement,
// verification.*, evidence_exec.*, fix_warning and fix_review columns.
func (c axiColumns) header() []string {
	h := []string{"severity", "file:line", "problem", "fix", "category", "est_minutes", "evidence", "reviewers", "confidence"}
	if c.disagreement {
		h = append(h, "disagreement")
	}
	if c.verification {
		h = append(h, "verification.verdict", "verification.skeptic", "verification.notes", "verification.challenge_survived")
	}
	if c.evidence {
		h = append(h, "evidence_exec.command", "evidence_exec.exit_code", "evidence_exec.output_excerpt")
	}
	if c.fixWarning {
		h = append(h, "fix_warning")
	}
	if c.fixReview {
		h = append(h, "fix_review")
	}
	return h
}

// ReviewSummaryAXI is the run-level metadata carried by the --axi review/resume
// summary payload: review identity plus per-attempt agent counts and a findings
// total — the token-dense analogue of the human end-of-review summary block
// (cli/review_summary.go). It is deliberately distinct from the findings
// table renderAXI emits: a bare `atcr review --axi` runs no reconcile stage, so it
// has a run summary but no findings list. The two payloads share this package's one
// TOON encoder (encodeAXI) rather than a second, divergent serializer (AC 01-03;
// sprint-design Architecture).
type ReviewSummaryAXI struct {
	ID              string
	Dir             string
	AgentsSucceeded int64
	AgentsTotal     int64
	AgentsFailed    int64
	AgentsTimedOut  int64
	APICalls        int64
	FindingsTotal   int64
	// Per-severity finding counts, the token-dense analogue of the human summary's
	// severityBreakdown. Carried so an agent consuming AXI can make a fail-on-severity
	// decision from the run summary — parity with what a human reads on stdout.
	FindingsCritical int64
	FindingsHigh     int64
	FindingsMedium   int64
	FindingsLow      int64
}

// reviewSummaryAXIHeader is the fixed column order of the review-summary payload.
// Kept as one slice so the header line and the row are guaranteed the same width
// and order (the same defensive invariant renderAXI enforces for findings).
var reviewSummaryAXIHeader = []string{
	"id", "dir", "agents_succeeded", "agents_total",
	"agents_failed", "agents_timed_out", "api_calls", "findings_total",
	"findings_critical", "findings_high", "findings_medium", "findings_low",
}

// RenderReviewSummaryAXI writes s as a single-row standard TOON tabular array
// (review_summary[1]{...}:) through the same go-axi encoder as the findings
// payload, so it carries the same no-ANSI / no-Markdown structural guarantee and
// stays byte-identical between `atcr review --axi` and `atcr resume --axi` for
// equivalent data (AC 01-03/01-04).
func RenderReviewSummaryAXI(w io.Writer, s ReviewSummaryAXI) error {
	doc, err := reviewSummaryAXIDoc(s)
	if err != nil {
		return err
	}
	return encodeAXI(w, doc)
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
		// Fix-review annotation (Epic 35.3 fix_review): a fix the diff-smell gate
		// accepted despite a SOFT over-simplification smell. Unlike a fix warning the
		// fix above IS usable, so this renders in ADDITION to it, never instead of it.
		if f.FixReview != "" {
			fmt.Fprintf(&b, "  - 🔍 Fix review: %s\n", escTrunc(f.FixReview))
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
	// A verdict reached from a shortened read still STANDS — the ceiling that
	// truncated it was derived from the agent's window, not declared by an
	// operator — so this is a caveat appended to whatever the verdict is, never a
	// downgrade and never a replacement for the unverifiable note above. The two
	// facts are independent: a skeptic can fail to verify AND have been truncated.
	if v.Truncated {
		annotation += " (answered from a truncated read)"
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
		// The truncation caveat matters MOST here. This section is the only place
		// a refuted finding appears, refuted is the verdict reconcile's gate
		// excludes, and refuting a false positive off a few large reads is the
		// exact case the derived-ceiling exemption was built to allow — so a
		// refuted-from-a-shortened-read drops a finding out of CI, and this line
		// is the only place a human would see why to look twice.
		truncated := ""
		if f.Verification != nil && f.Verification.Truncated {
			truncated = " (answered from a truncated read)"
		}
		fmt.Fprintf(b, "- %s — confidence %s, skeptic: %s%s\n",
			codeSpan(f.File, f.Line), esc(f.Confidence), esc(skepticName(f.Verification)), truncated)
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
