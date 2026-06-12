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
		return renderMarkdown(w, findings)
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
func renderMarkdown(w io.Writer, findings []reconcile.JSONFinding) error {
	var b bytes.Buffer
	b.WriteString("# atcr Review Report\n\n")
	writeSummaryGrid(&b, findings)

	if len(findings) == 0 {
		b.WriteString("\nNo findings.\n")
		_, err := w.Write(b.Bytes())
		return err
	}

	b.WriteString("\n## Findings\n")
	lastSev := ""
	for _, f := range findings {
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
	}
	_, err := w.Write(b.Bytes())
	return err
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
func writeSummaryGrid(b *bytes.Buffer, findings []reconcile.JSONFinding) {
	type cell struct{ high, medium, low int }
	order := []string{reconcile.SevCritical, reconcile.SevHigh, reconcile.SevMedium, reconcile.SevLow}
	counts := map[string]*cell{}
	for _, s := range order {
		counts[s] = &cell{}
	}
	for _, f := range findings {
		c, ok := counts[f.Severity]
		if !ok {
			continue
		}
		switch f.Confidence {
		case reconcile.ConfHigh:
			c.high++
		case reconcile.ConfMedium:
			c.medium++
		default:
			c.low++
		}
	}
	fmt.Fprintf(b, "Total findings: %d\n\n", len(findings))
	b.WriteString("| Severity | HIGH conf | MEDIUM conf | LOW conf |\n")
	b.WriteString("|----------|-----------|-------------|----------|\n")
	for _, s := range order {
		c := counts[s]
		fmt.Fprintf(b, "| %s | %d | %d | %d |\n", s, c.high, c.medium, c.low)
	}
}

// newlineFlattener collapses CR/LF so a field cannot inject markdown structure.
var newlineFlattener = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

// esc flattens newlines then HTML-escapes free text so it renders inert.
func esc(s string) string { return html.EscapeString(newlineFlattener.Replace(s)) }

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

// joinReviewers joins reviewer names or returns "(none)".
func joinReviewers(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
