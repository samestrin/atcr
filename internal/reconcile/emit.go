package reconcile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
)

// Reconciled artifact filenames.
const (
	FindingsTxt   = "findings.txt"
	FindingsJSON  = "findings.json"
	ReportMD      = "report.md"
	SummaryJSON   = "summary.json"
	AmbiguousJSON = "ambiguous.json"
)

// Verification is the reserved per-finding adversarial-verification block for a
// future stage (Epic 3.0). It is absent from every 1.x findings.json (the
// omitempty pointer marshals to nothing when nil); readers and renderers must
// tolerate both its absence and its presence.
//
// Epic 3.0 contract: when populating this block, the writing stage MUST
// validate Verdict against the allowed enum values (confirmed, refuted,
// unverifiable) before persisting. ReadReconciledFindings (emit.go:145) does NOT
// validate the enum so bad values are silently accepted on read — validation
// is the writer's responsibility. An empty Verdict (verdict:"") is a contract
// violation and will confuse downstream consumers.
type Verification struct {
	Verdict string `json:"verdict"` // confirmed | refuted | unverifiable
	Skeptic string `json:"skeptic"` // agent that produced the verdict
	Notes   string `json:"notes,omitempty"`
}

// Verdict enum values for Verification.Verdict (Epic 3.0). The verify stage
// validates skeptic output against this set before persisting; the gate reads
// these constants to exclude refuted findings and, under requireVerified, to
// count only confirmed ones.
const (
	VerdictConfirmed    = "confirmed"
	VerdictRefuted      = "refuted"
	VerdictUnverifiable = "unverifiable"
)

// JSONFinding is the findings.json record schema (AC 01-06). It is the stable,
// re-readable structured contract the report command renders views over.
//
// Verification is reserved for Epic 3.0 (adversarial verification) — parsed if
// present, but never populated by any v1 code path and omitted from 1.x output.
type JSONFinding struct {
	Severity     string        `json:"severity"`
	File         string        `json:"file"`
	Line         int           `json:"line"`
	Problem      string        `json:"problem"`
	Fix          string        `json:"fix"`
	Category     string        `json:"category"`
	EstMinutes   int           `json:"est_minutes"`
	Evidence     string        `json:"evidence"`
	Reviewers    []string      `json:"reviewers"`
	Confidence   string        `json:"confidence"`
	Disagreement string        `json:"disagreement,omitempty"`
	Verification *Verification `json:"verification,omitempty"` // reserved (Epic 3.0); absent in 1.x
}

// JSONFindings converts the merged findings to their JSON schema records.
func (r Result) JSONFindings() []JSONFinding {
	out := make([]JSONFinding, 0, len(r.Findings))
	for _, m := range r.Findings {
		out = append(out, JSONFinding{
			Severity:     m.Severity,
			File:         m.File,
			Line:         m.Line,
			Problem:      m.Problem,
			Fix:          m.Fix,
			Category:     m.Category,
			EstMinutes:   m.EstMinutes,
			Evidence:     m.Evidence,
			Reviewers:    m.Reviewers,
			Confidence:   m.Confidence,
			Disagreement: m.Disagreement,
			Verification: m.Verification,
		})
	}
	return out
}

// Emit writes the four reconciled artifacts plus the ambiguous.json sidecar into
// reconciledDir (created if absent). Each file is written atomically.
func Emit(reconciledDir string, r Result) error {
	if err := os.MkdirAll(reconciledDir, 0o755); err != nil {
		return fmt.Errorf("creating reconciled dir: %w", err)
	}
	writers := []struct {
		name   string
		render func(io.Writer) error
	}{
		{FindingsTxt, func(w io.Writer) error { return RenderText(w, r) }},
		{FindingsJSON, func(w io.Writer) error { return RenderJSON(w, r) }},
		{ReportMD, func(w io.Writer) error { return RenderMarkdown(w, r) }},
		{SummaryJSON, func(w io.Writer) error { return renderIndentedJSON(w, r.Summary) }},
		{AmbiguousJSON, func(w io.Writer) error { return renderIndentedJSON(w, r.Ambiguous) }},
	}
	// Render every artifact first so a render error aborts before any file is
	// published — the published set is then never partially from this run.
	rendered := make(map[string][]byte, len(writers))
	for _, x := range writers {
		var buf bytes.Buffer
		if err := x.render(&buf); err != nil {
			return fmt.Errorf("rendering %s: %w", x.name, err)
		}
		rendered[x.name] = bytes.Clone(buf.Bytes())
	}
	for _, x := range writers {
		if err := writeFileAtomic(filepath.Join(reconciledDir, x.name), rendered[x.name]); err != nil {
			return err
		}
	}
	return nil
}

// RenderText writes the reconciled 9-column findings.txt. The disagreement
// annotation, which has no column of its own, is folded into EVIDENCE so the
// machine contract preserves it.
func RenderText(w io.Writer, r Result) error {
	findings := make([]stream.Finding, 0, len(r.Findings))
	for _, m := range r.Findings {
		f := m.Finding
		if m.Disagreement != "" {
			if f.Evidence != "" {
				f.Evidence += " "
			}
			f.Evidence += "(disagreement: " + m.Disagreement + ")"
		}
		findings = append(findings, f)
	}
	return stream.WriteReconciled(w, findings)
}

// RenderJSON writes findings.json: the structured, re-readable record array.
func RenderJSON(w io.Writer, r Result) error {
	return renderIndentedJSON(w, r.JSONFindings())
}

// ReadReconciledFindings loads reviewDir/reconciled/findings.json — the reader
// counterpart to RenderJSON, shared by the CLI report command and the MCP
// report handler so the findings.json contract has one loader. A missing file
// is returned as the raw os.ErrNotExist sentinel so each caller phrases its own
// "run reconcile first" guidance; an empty or malformed file is a parse error.
func ReadReconciledFindings(reviewDir string) ([]JSONFinding, error) {
	path := filepath.Join(reviewDir, reconciledSubdir, FindingsJSON)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err // includes os.ErrNotExist
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("reconciled findings.json is empty")
	}
	var findings []JSONFinding
	if err := json.Unmarshal(data, &findings); err != nil {
		return nil, fmt.Errorf("parsing reconciled findings: %w", err)
	}
	return findings, nil
}

// RenderMarkdown writes the human report.md: an executive summary (counts by
// severity x confidence) followed by findings grouped by severity. Findings
// annotated out-of-scope are listed in their own section (AC 06-04) — they do
// not gate, so mixing them into the main list (or the summary table) would
// misread as gate-relevant. Free text is HTML-escaped and file paths are
// rendered in backtick code spans so neither raw HTML nor markdown injection
// survives (AC 01-06 Security).
func RenderMarkdown(w io.Writer, r Result) error {
	inScope := make([]Merged, 0, len(r.Findings))
	var outOfScope []Merged
	for _, m := range r.Findings {
		if m.Category == CategoryOutOfScope {
			outOfScope = append(outOfScope, m)
		} else {
			inScope = append(inScope, m)
		}
	}

	var b bytes.Buffer
	b.WriteString("# atcr Reconciled Review\n\n")

	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- Total findings: %d\n", r.Summary.TotalFindings)
	fmt.Fprintf(&b, "- Sources: %s\n", joinOrNone(r.Summary.SourcesScanned))
	fmt.Fprintf(&b, "- Clusters collapsed: %d\n", r.Summary.ClustersCollapsed)
	fmt.Fprintf(&b, "- Severity disagreements: %d\n", r.Summary.SeverityDisagreements)
	if len(outOfScope) > 0 {
		fmt.Fprintf(&b, "- Out-of-scope findings: %d (annotated, excluded from the gate)\n", len(outOfScope))
	}
	if r.Summary.Partial {
		b.WriteString("- Partial: yes (a source was missing or unreadable)\n")
	}
	b.WriteString("\n")
	writeSeverityConfidenceTable(&b, inScope)

	if len(r.Findings) == 0 {
		b.WriteString("\nNo findings.\n")
		_, err := w.Write(b.Bytes())
		return err
	}

	if len(inScope) > 0 {
		b.WriteString("\n## Findings\n")
		writeFindingsList(&b, inScope)
	}
	if len(outOfScope) > 0 {
		b.WriteString("\n## Out-of-Scope Findings\n\nPre-existing issues outside the reviewed change — annotated for the record, excluded from the severity gate.\n")
		writeFindingsList(&b, outOfScope)
	}
	_, err := w.Write(b.Bytes())
	return err
}

// writeFindingsList renders findings grouped by severity heading, in the order
// given (the caller passes sortMerged-ordered slices).
func writeFindingsList(b *bytes.Buffer, findings []Merged) {
	lastSev := ""
	for _, m := range findings {
		if m.Severity != lastSev {
			fmt.Fprintf(b, "\n### %s\n\n", esc(m.Severity))
			lastSev = m.Severity
		}
		fmt.Fprintf(b, "- %s — confidence %s, reviewers: %s\n",
			codeSpan(m.File, m.Line), esc(m.Confidence), esc(joinOrNone(m.Reviewers)))
		if m.Disagreement != "" {
			fmt.Fprintf(b, "  - Severity disagreement: %s\n", esc(m.Disagreement))
		}
		fmt.Fprintf(b, "  - Problem: %s\n", esc(m.Problem))
		if m.Fix != "" {
			fmt.Fprintf(b, "  - Fix: %s\n", esc(m.Fix))
		}
		if m.Evidence != "" {
			fmt.Fprintf(b, "  - Evidence: %s\n", esc(m.Evidence))
		}
	}
}

// writeSeverityConfidenceTable writes the counts-by-severity x confidence grid.
func writeSeverityConfidenceTable(b *bytes.Buffer, findings []Merged) {
	type cell struct{ high, medium, low int }
	order := []string{SevCritical, SevHigh, SevMedium, SevLow}
	counts := map[string]*cell{}
	for _, s := range order {
		counts[s] = &cell{}
	}
	for _, m := range findings {
		c, ok := counts[m.Severity]
		if !ok {
			continue
		}
		switch m.Confidence {
		case ConfHigh:
			c.high++
		case ConfMedium:
			c.medium++
		default:
			c.low++
		}
	}
	b.WriteString("| Severity | HIGH conf | MEDIUM conf | LOW conf |\n")
	b.WriteString("|----------|-----------|-------------|----------|\n")
	for _, s := range order {
		c := counts[s]
		fmt.Fprintf(b, "| %s | %d | %d | %d |\n", s, c.high, c.medium, c.low)
	}
}

// renderIndentedJSON writes v as 2-space-indented JSON with a trailing newline.
func renderIndentedJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// newlineFlattener collapses CR/LF to a space so a free-text field cannot break
// out of its markdown list item onto fresh lines (forged headings/bullets).
var newlineFlattener = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

// codeSpan renders FILE:LINE inside a backtick code span so a normal path is
// byte-identical (unicode-safe). A path containing a backtick (a valid filename
// character) would close the span and let trailing text inject live
// markdown/HTML, so such paths — and any with CR/LF — fall back to
// HTML-escaping instead. Same defense as internal/report/render.go codeSpan
// (AC 01-06 Security).
func codeSpan(file string, line int) string {
	if strings.ContainsRune(file, '`') || strings.ContainsAny(file, "\r\n") {
		return esc(fmt.Sprintf("%s:%d", file, line))
	}
	return fmt.Sprintf("`%s:%d`", file, line)
}

// esc makes a free-text field safe in markdown: newlines are flattened (so a
// field cannot inject markdown structure) and HTML metacharacters are escaped
// (so raw HTML never renders). html.EscapeString alone leaves newlines intact.
func esc(s string) string { return html.EscapeString(newlineFlattener.Replace(s)) }

// joinOrNone joins names with ", " or returns "(none)" for an empty list.
func joinOrNone(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	out := names[0]
	for _, n := range names[1:] {
		out += ", " + n
	}
	return out
}

// writeFileAtomic writes data to a sibling temp file (0644) then renames it over
// path so a reader never sees a partial write.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
