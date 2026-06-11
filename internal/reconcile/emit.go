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

// JSONFinding is the findings.json record schema (AC 01-06). It is the stable,
// re-readable structured contract the report command renders views over.
type JSONFinding struct {
	Severity     string   `json:"severity"`
	File         string   `json:"file"`
	Line         int      `json:"line"`
	Problem      string   `json:"problem"`
	Fix          string   `json:"fix"`
	Category     string   `json:"category"`
	EstMinutes   int      `json:"est_minutes"`
	Evidence     string   `json:"evidence"`
	Reviewers    []string `json:"reviewers"`
	Confidence   string   `json:"confidence"`
	Disagreement string   `json:"disagreement,omitempty"`
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

// RenderMarkdown writes the human report.md: an executive summary (counts by
// severity x confidence) followed by findings grouped by severity. Free text is
// HTML-escaped and file paths are rendered in backtick code spans so neither
// raw HTML nor markdown injection survives (AC 01-06 Security).
func RenderMarkdown(w io.Writer, r Result) error {
	var b bytes.Buffer
	b.WriteString("# atcr Reconciled Review\n\n")

	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- Total findings: %d\n", r.Summary.TotalFindings)
	fmt.Fprintf(&b, "- Sources: %s\n", joinOrNone(r.Summary.SourcesScanned))
	fmt.Fprintf(&b, "- Clusters collapsed: %d\n", r.Summary.ClustersCollapsed)
	fmt.Fprintf(&b, "- Severity disagreements: %d\n", r.Summary.SeverityDisagreements)
	if r.Summary.Partial {
		b.WriteString("- Partial: yes (a source was missing or unreadable)\n")
	}
	b.WriteString("\n")
	writeSeverityConfidenceTable(&b, r.Findings)

	if len(r.Findings) == 0 {
		b.WriteString("\nNo findings.\n")
		_, err := w.Write(b.Bytes())
		return err
	}

	b.WriteString("\n## Findings\n")
	lastSev := ""
	for _, m := range r.Findings {
		if m.Severity != lastSev {
			fmt.Fprintf(&b, "\n### %s\n\n", esc(m.Severity))
			lastSev = m.Severity
		}
		fmt.Fprintf(&b, "- `%s:%d` — confidence %s, reviewers: %s\n",
			esc(m.File), m.Line, esc(m.Confidence), esc(joinOrNone(m.Reviewers)))
		if m.Disagreement != "" {
			fmt.Fprintf(&b, "  - Severity disagreement: %s\n", esc(m.Disagreement))
		}
		fmt.Fprintf(&b, "  - Problem: %s\n", esc(m.Problem))
		if m.Fix != "" {
			fmt.Fprintf(&b, "  - Fix: %s\n", esc(m.Fix))
		}
		if m.Evidence != "" {
			fmt.Fprintf(&b, "  - Evidence: %s\n", esc(m.Evidence))
		}
	}
	_, err := w.Write(b.Bytes())
	return err
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
