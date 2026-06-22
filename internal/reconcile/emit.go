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

	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/stream"
)

// Reconciled artifact filenames.
const (
	FindingsTxt   = "findings.txt"
	FindingsJSON  = "findings.json"
	ReportMD      = "report.md"
	SummaryJSON   = "summary.json"
	AmbiguousJSON = "ambiguous.json"
	// DisagreementsJSON is the disagreement-radar handoff artifact (Epic 3.2) —
	// the stable, versioned queue Epic 6.0 (Cross-Examination) consumes directly.
	DisagreementsJSON = "disagreements.json"
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
	// Notes is populated only from the winning verdict during a cluster-merge;
	// minority-verdict reasoning is intentionally not preserved.
	Notes string `json:"notes,omitempty"`
	// ChallengeSurvived marks a finding upheld by the cross-examination stage
	// (Epic 6.0): the judge ruled uphold or split, so the finding survived hostile
	// challenge. omitempty keeps every pre-6.0 and non-debated findings.json block
	// byte-identical — the marker appears only on a debated, surviving finding. It
	// rides alongside Verdict (uphold→confirmed, split→confirmed at a settled
	// severity, overturn→refuted), so the gate keys on Verdict as before and this
	// is a display/audit marker, never a separate confidence tier.
	ChallengeSurvived bool `json:"challenge_survived,omitempty"`
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
// The schema is additively extended by later epics (path-validation 5.x,
// cross-examination 6.x); all such fields are omitempty for backward
// byte-compatibility with pre-extension findings.json.
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
	// PathValid / PathWarning carry file-existence validation (Epic 5.0). Both
	// are omitempty so a finding that was never validated (or whose path exists)
	// serializes byte-identically to pre-5.0 findings.json — only a flagged
	// hallucinated path adds path_warning.
	//
	// CONTRACT: path_warning is the authoritative signal — a non-empty value
	// means the path did not resolve under the reviewed repo root. path_valid is
	// auxiliary and MUST NOT be read in isolation: under omitempty, "validated
	// but missing" and "never validated" both serialize as an absent path_valid
	// (false), so the two states are indistinguishable from path_valid alone.
	// Consumers and the report layer key display off path_warning.
	PathValid   bool   `json:"path_valid,omitempty"`
	PathWarning string `json:"path_warning,omitempty"`
	// PathSuggestion is the candidate-index correction for a hallucinated path
	// (Epic 5.4): the real tracked file the finding most likely meant. omitempty
	// keeps findings.json byte-identical to pre-5.4 output when no suggestion is
	// present. It is advisory only — File (the original cited path) is never
	// rewritten — and is set only alongside a non-empty path_warning.
	PathSuggestion string `json:"path_suggestion,omitempty"`
	// ClusterMerged marks the record that resulted from inline application of a
	// judge gray-zone "merge" ruling (Epic 6.1): the cross-examination stage
	// unioned a gray-zone cluster's members into this single finding. omitempty
	// keeps every non-merged findings.json record byte-identical. It is the
	// idempotency marker the debate radar filters on so a re-run never re-merges an
	// already-applied cluster (AC4); it is never set by any reconcile-time path.
	ClusterMerged bool `json:"cluster_merged,omitempty"`
	// ClusterID is the stable, content-addressed AmbiguousCluster.ID of the
	// gray-zone cluster that produced an inline-merged survivor (Epic 6.2). It is
	// stamped alongside ClusterMerged by the cross-examination apply path and lets
	// the debate radar key merge idempotency on cluster identity rather than
	// File+Line alone — so a second DISTINCT cluster co-located at the same
	// canonical File+Line is no longer over-suppressed once the first is merged.
	// omitempty keeps every non-merged record byte-identical to pre-6.2
	// findings.json; it is never set by any reconcile-time path.
	ClusterID string `json:"cluster_id,omitempty"`
}

// JSONFindings converts the merged findings to their JSON schema records.
func (r Result) JSONFindings() []JSONFinding {
	out := make([]JSONFinding, 0, len(r.Findings))
	for _, m := range r.Findings {
		out = append(out, JSONFinding{
			Severity:       m.Severity,
			File:           m.File,
			Line:           m.Line,
			Problem:        m.Problem,
			Fix:            m.Fix,
			Category:       m.Category,
			EstMinutes:     m.EstMinutes,
			Evidence:       m.Evidence,
			Reviewers:      m.Reviewers,
			Confidence:     m.Confidence,
			Disagreement:   m.Disagreement,
			Verification:   m.Verification,
			PathValid:      m.PathValid,
			PathWarning:    m.PathWarning,
			PathSuggestion: m.PathSuggestion,
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
	df := BuildDisagreements(r.JSONFindings(), r.Ambiguous)
	writers := []struct {
		name   string
		render func(io.Writer) error
	}{
		{FindingsTxt, func(w io.Writer) error { return RenderText(w, r) }},
		{FindingsJSON, func(w io.Writer) error { return RenderJSON(w, r) }},
		{ReportMD, func(w io.Writer) error { return renderMarkdown(w, r, df) }},
		{SummaryJSON, func(w io.Writer) error { return renderIndentedJSON(w, r.Summary) }},
		{AmbiguousJSON, func(w io.Writer) error { return renderIndentedJSON(w, r.Ambiguous) }},
		{DisagreementsJSON, func(w io.Writer) error { return renderIndentedJSON(w, df) }},
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
		if err := atomicfs.WriteFileAtomic(filepath.Join(reconciledDir, x.name), rendered[x.name]); err != nil {
			return err
		}
	}
	return nil
}

// RenderText writes the reconciled 9-column findings.txt. The disagreement
// annotation, which has no column of its own, is folded into EVIDENCE so the
// machine contract preserves it.
//
// PathValid/PathWarning (the Epic 5.0 hallucinated-path signal) are
// intentionally NOT carried here: findings.txt is a fixed 9-column schema with
// no field for them, and — unlike the disagreement annotation — they are not
// folded into EVIDENCE so the column contract stays byte-stable for existing
// parsers. A consumer that needs the path-validation warning must read
// findings.json (path_warning) or report.md (the rendered "File not found"
// line), which are the authoritative carriers.
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

// ReadAmbiguousClusters loads reviewDir/reconciled/ambiguous.json — the gray-zone
// sidecar the disagreement radar reads. A missing or empty file returns
// (nil, nil): the radar degrades to a findings-only view rather than erroring,
// since ambiguous.json is absent whenever a review produced no gray-zone pairs.
// A present-but-unparseable file is an error.
func ReadAmbiguousClusters(reviewDir string) ([]AmbiguousCluster, error) {
	path := filepath.Join(reviewDir, reconciledSubdir, AmbiguousJSON)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var clusters []AmbiguousCluster
	if err := json.Unmarshal(data, &clusters); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", AmbiguousJSON, err)
	}
	return clusters, nil
}

// ReadDisagreements loads reviewDir/reconciled/disagreements.json — the Epic 6.0
// cross-exam handoff queue written by Emit. A missing or empty file returns a
// zero DisagreementsFile (no error): a review with no disagreements still has a
// valid, empty queue. A present-but-unparseable file is an error.
func ReadDisagreements(reviewDir string) (DisagreementsFile, error) {
	path := filepath.Join(reviewDir, reconciledSubdir, DisagreementsJSON)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DisagreementsFile{}, nil
		}
		return DisagreementsFile{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return DisagreementsFile{}, nil
	}
	var df DisagreementsFile
	if err := json.Unmarshal(data, &df); err != nil {
		return DisagreementsFile{}, fmt.Errorf("parsing %s: %w", DisagreementsJSON, err)
	}
	if df.SchemaVersion != "" {
		wantMajor := strings.SplitN(DisagreementsSchemaVersion, ".", 2)[0]
		gotMajor := strings.SplitN(df.SchemaVersion, ".", 2)[0]
		if gotMajor != wantMajor {
			return DisagreementsFile{}, fmt.Errorf("%s: schema version %q is incompatible with reader version %q", DisagreementsJSON, df.SchemaVersion, DisagreementsSchemaVersion)
		}
	}
	return df, nil
}

// RenderMarkdown writes the human report.md. It builds the disagreements radar
// and delegates to renderMarkdown; Emit passes a pre-built radar to avoid the
// redundant BuildDisagreements call on the reconcile path.
func RenderMarkdown(w io.Writer, r Result) error {
	return renderMarkdown(w, r, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
}

// renderMarkdown is the internal implementation. It accepts a pre-built
// DisagreementsFile so Emit can build the radar once and share it between
// report.md and disagreements.json without a second O(n log n) sort pass.
func renderMarkdown(w io.Writer, r Result, df DisagreementsFile) error {
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

	// Disagreement radar above the consensus findings (Epic 3.2). Nothing is
	// written when there is no tension, so report.md is byte-identical to the
	// pre-3.2 output for a review with no disagreements.
	writeRadarSection(&b, df)

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
		if m.PathWarning != "" {
			// Hallucinated path (Epic 5.0): the finding is kept, not dropped — the
			// path is flagged so a user can correct it. A candidate-index
			// suggestion (Epic 5.4), when present, points at the real file. esc()
			// neutralizes any markup in the reviewer-controlled path; the
			// suggestion comes from git ls-files but is esc'd for symmetry.
			label := "File not found"
			if m.PathWarning != stream.PathNotFoundWarning {
				label = esc(m.PathWarning)
			}
			if m.PathSuggestion != "" {
				fmt.Fprintf(b, "  - ⚠️ %s: %s (did you mean %s?)\n", label, esc(m.File), esc(m.PathSuggestion))
			} else {
				fmt.Fprintf(b, "  - ⚠️ %s: %s\n", label, esc(m.File))
			}
		}
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
// field cannot inject markdown structure), HTML metacharacters are escaped
// (so raw HTML never renders), and backticks are escaped so reviewer-controlled
// fields cannot open an inline code span inside a normal bullet.
func esc(s string) string {
	return strings.ReplaceAll(html.EscapeString(newlineFlattener.Replace(s)), "`", "&#96;")
}

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
