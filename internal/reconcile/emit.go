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
	reclib "github.com/samestrin/atcr/reconcile"
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

// Verification is ATCR's compatibility alias for the library Verification type.
// The canonical definition was extracted to github.com/samestrin/atcr/reconcile
// (Epic 8.0); this alias keeps internal/reconcile and every existing consumer
// compiling unchanged until they flip to the library import in Phase 3. Because
// it is a type alias, the type is identical — JSON serialization and the
// *Verification pointer carried on Merged are byte-for-byte unchanged.
//
// Epic 3.0 contract (unchanged): when populating this block, the writing stage
// MUST validate Verdict against the allowed enum values before persisting; an
// empty Verdict is a contract violation. Readers do not re-validate the enum.
type Verification = reclib.Verification

// Verdict enum values for Verification.Verdict (Epic 3.0), re-exported from the
// extracted library so internal lookups and external callers keep a stable
// symbol. The verify stage validates skeptic output against this set before
// persisting; the gate reads these to exclude refuted findings and, under
// requireVerified, to count only confirmed ones.
const (
	VerdictConfirmed    = reclib.VerdictConfirmed
	VerdictRefuted      = reclib.VerdictRefuted
	VerdictUnverifiable = reclib.VerdictUnverifiable
)

// JSONFinding is the findings.json record schema (AC 01-06). It is the stable,
// re-readable structured contract the report command renders views over.
// The schema is additively extended by later epics (path-validation 5.x,
// cross-examination 6.x); all such fields are omitempty for backward
// byte-compatibility with pre-extension findings.json.
//
// Verification carries the adversarial-verification block (Epic 3.0). It is
// populated by the verify stage and omitted from JSON output when nil
// (omitempty); reconcile-time producers leave it empty.
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
	Verification *Verification `json:"verification,omitempty"` // populated by verify stage; omitted when nil
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
	// stamped alongside ClusterMerged by the cross-examination apply path
	// (internal/debate/cluster.go applyOneClusterMerge) and read by the debate
	// radar (internal/debate/cluster.go filterMergedClusters), which keys merge
	// idempotency on cluster identity rather than File+Line alone — so a second
	// DISTINCT cluster co-located at the same canonical File+Line is no longer
	// over-suppressed once the first is merged. The cross-package symbols are
	// named (not line-numbered) so the reference survives line shifts; grep them
	// if either path moves. This field is intended ONLY for that debate inline-
	// merge stamping: reconcile-time producers MUST leave it empty, and omitempty
	// keeps every non-merged record byte-identical to pre-6.2 findings.json.
	ClusterID string `json:"cluster_id,omitempty"`
	// FixWarning records a non-fatal fix-generation warning (Epic 7.0): when the
	// executor errors or returns an empty completion for a finding it was eligible
	// to fix — or when the generated fix fails the local syntax guard (Epic 7.1),
	// stamped as "invalid_syntax: <parser error>" while the attempted fix stays in
	// Fix — the warning is stamped here instead of failing the run, so a downstream
	// consumer (the report, the Epic 7.3 PR action) can see the fix was attempted
	// and why it is absent or flagged. omitempty keeps every finding without a
	// fix-generation warning byte-identical to pre-7.0 findings.json; reconcile-time
	// producers MUST leave it empty (it is set only by the verify fix phase).
	FixWarning string `json:"fix_warning,omitempty"`
	// EvidenceExec carries the execution-reproduction block (Epic 11.0): the
	// command a repro/skeptic agent ran in the sandbox, its exit code, and a
	// truncated output excerpt. It is set ONLY by the repro write-back (a
	// reproduced finding also gets Verification.Verdict=confirmed, skeptic="repro"),
	// never by any reconcile-time path. omitempty keeps every non-reproduced record
	// byte-identical to pre-11.0 findings.json.
	EvidenceExec *EvidenceExec `json:"evidence_exec,omitempty"`
	// Justification is the narrative context extracted at reconcile time from the
	// originating source's review.md (Epic 18.2): the human-readable explanation a
	// reviewer wrote alongside its terse findings.txt row, matched best-effort by
	// file:line and carried forward so a downstream TD-resolution consumer inherits
	// the reasoning instead of re-deriving it from raw review.md files. Set ONLY by
	// stampJustifications on the reconcile path; distinct from Verification.Notes
	// (skeptic/judge reasoning, a different concept). No match leaves it empty, and
	// omitempty keeps a finding with no matched narrative byte-identical to pre-18.2
	// findings.json. It lands ONLY here — never in the TD README table's Problem
	// cell, whose column structure Epic 18.1 freezes.
	Justification string `json:"justification,omitempty"`
	// SourceReport is the back-reference to the review.md section the Justification
	// was extracted from (Epic 18.2): path (review-dir-relative) plus the anchor
	// line and nearest heading, so a consumer can navigate to full detail without
	// re-deriving the mapping. Set alongside Justification by stampJustifications;
	// nil (omitempty) when no narrative matched, keeping output byte-identical.
	SourceReport *SourceReport `json:"source_report,omitempty"`
}

// SourceReport is the back-reference (Epic 18.2) from a reconciled finding to the
// review.md narrative section its Justification was extracted from. Path is
// relative to the review directory (e.g. "sources/host/review.md") so a consumer
// can resolve it against the same review dir that holds reconciled/findings.json.
// Line is the 1-based line in that review.md where the file:line match anchored;
// Section is the nearest enclosing Markdown heading (both omitempty when absent).
type SourceReport struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Section string `json:"section,omitempty"`
}

// EvidenceExec is the executable-evidence block attached to a finding that was
// reproduced by running code in the sandbox (Epic 11.0). It is the categorical
// upgrade from asserted to demonstrated: a finding carrying it cannot be a
// hallucination, and it hands the resolver a failing command to start from.
type EvidenceExec struct {
	// Command is the human-readable command that was executed.
	Command string `json:"command"`
	// ExitCode is the command's exit status (non-zero == reproduced failure).
	ExitCode int `json:"exit_code"`
	// OutputExcerpt is the captured combined output, truncated to a budget.
	OutputExcerpt string `json:"output_excerpt"`
}

// JSONFindings converts the merged findings to their JSON schema records.
//
// Path-validation fields (PathValid/PathWarning/PathSuggestion) are NOT carried
// here: the extracted library Merged no longer holds them (Epic 8.0 Phase 2
// Clarification Q1). When this Result was produced by RunReconcile, the
// path-stamped records were cached on it (after validateFindingPaths ran over the
// JSONFinding layer), so this returns those — every consumer sees identical,
// path-validated records. A Result built directly (no path validation) derives
// fresh path-less records from the merged findings.
func (r Result) JSONFindings() []JSONFinding {
	// Return cached path-stamped records when available; otherwise derive fresh
	// path-less records from the merged findings. The cache-vs-derivation split
	// is intentional: RunReconcile validates paths once and reuses the result.
	if r.jsonFindings != nil {
		return r.jsonFindings
	}
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
			// Verification pointer identity is intentionally preserved (not deep-copied)
			// because gate.go IsFailing and debate cross-examination mutate the same
			// block referenced by the merged finding.
			Verification: m.Verification,
			// FixWarning intentionally not copied: it is set by the verify fix phase
			// (executor.go generateFixes) after reconcile, so the reconcile path owns
			// only the pre-fix merged state.
			// EvidenceExec intentionally not copied: the extracted library Finding does
			// not carry this Epic 11.0 field, and derivation-time producers never hold
			// it. It is preserved only on the cached path-stamped records produced by
			// RunReconcile (which already stamped it via repro write-back before caching).
			// If this derived path is ever used for a stamped finding, carry it here.
		})
	}
	return out
}

// ambiguousWire is the on-disk shape of an ambiguous.json cluster. Its Findings
// are ATCR stream.Finding values (no json tags → PascalCase keys, every field
// present including the zero-valued path-validation fields), reproducing the
// pre-extraction byte layout exactly. The extracted library AmbiguousCluster
// carries the stdlib-only reconcile.Finding (lowercase tags, omitempty, no path
// fields), which serializes differently; ATCR converts to this wire type so
// ambiguous.json stays byte-identical across the Epic 8.0 extraction (AC 01-05).
type ambiguousWire struct {
	ID         string           `json:"id"`
	File       string           `json:"file"`
	Line       int              `json:"line"`
	Similarity float64          `json:"similarity"`
	Findings   []stream.Finding `json:"findings"`
}

// toAmbiguousWire converts library AmbiguousClusters to the ATCR on-disk wire
// shape, mapping each library finding back to a stream.Finding (path fields zero,
// as they were at reconcile time — the ambiguous sidecar predates path validation).
func toAmbiguousWire(clusters []AmbiguousCluster) []ambiguousWire {
	out := make([]ambiguousWire, len(clusters))
	for i, c := range clusters {
		fs := make([]stream.Finding, len(c.Findings))
		for j, f := range c.Findings {
			// Inline the library Finding -> stream.Finding field map (TD-006 inlined).
			fs[j] = stream.Finding{
				Severity:   f.Severity,
				File:       f.File,
				Line:       f.Line,
				Problem:    f.Problem,
				Fix:        f.Fix,
				Category:   f.Category,
				EstMinutes: f.EstMinutes,
				Evidence:   f.Evidence,
				Reviewer:   f.Reviewer,
				Reviewers:  f.Reviewers,
				Confidence: f.Confidence,
			}
		}
		out[i] = ambiguousWire{ID: c.ID, File: c.File, Line: c.Line, Similarity: c.Similarity, Findings: fs}
	}
	return out
}

// ambiguousHash digests the exact bytes Emit writes for ambiguous.json (the wire
// shape), recorded in summary.json as ambiguous_hash so a host copies it verbatim
// into adjudication.json. It must hash the SAME bytes ambiguous.json carries, so
// it serializes the wire type — not the library AmbiguousCluster (whose stdlib-only
// serialization differs).
func ambiguousHash(r Result) string {
	if len(r.ambiguousBytes) > 0 {
		return HashBytes(r.ambiguousBytes)
	}
	var buf bytes.Buffer
	if err := renderIndentedJSON(&buf, toAmbiguousWire(r.Ambiguous)); err != nil {
		panic(fmt.Sprintf("atcr: ambiguousHash: unreachable JSON render error: %v", err))
	}
	return HashBytes(buf.Bytes())
}

// Emit writes the four reconciled artifacts plus the ambiguous.json sidecar into
// reconciledDir (created if absent). Each file is written atomically.
func Emit(reconciledDir string, r Result) error {
	if err := os.MkdirAll(reconciledDir, 0o755); err != nil {
		return fmt.Errorf("creating reconciled dir: %w", err)
	}
	jf := r.JSONFindings()
	df := BuildDisagreements(jf, r.Ambiguous)
	writers := []struct {
		name   string
		render func(io.Writer) error
	}{
		{FindingsTxt, func(w io.Writer) error { return RenderText(w, r) }},
		{FindingsJSON, func(w io.Writer) error { return RenderJSON(w, r) }},
		{ReportMD, func(w io.Writer) error { return renderMarkdown(w, r.Summary, jf, df) }},
		{SummaryJSON, func(w io.Writer) error { return renderIndentedJSON(w, r.Summary) }},
		{AmbiguousJSON, func(w io.Writer) error {
			if len(r.ambiguousBytes) > 0 {
				_, err := w.Write(r.ambiguousBytes)
				return err
			}
			return renderIndentedJSON(w, toAmbiguousWire(r.Ambiguous))
		}},
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
	records := r.JSONFindings()
	findings := make([]stream.Finding, 0, len(records))
	for _, rec := range records {
		f := stream.Finding{
			Severity:   rec.Severity,
			File:       rec.File,
			Line:       rec.Line,
			Problem:    rec.Problem,
			Fix:        rec.Fix,
			Category:   rec.Category,
			EstMinutes: rec.EstMinutes,
			Evidence:   rec.Evidence,
			Reviewers:  rec.Reviewers,
			Confidence: rec.Confidence,
		}
		if rec.Disagreement != "" {
			if f.Evidence != "" {
				f.Evidence += " "
			}
			f.Evidence += "(disagreement: " + rec.Disagreement + ")"
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
	jf := r.JSONFindings()
	return renderMarkdown(w, r.Summary, jf, BuildDisagreements(jf, r.Ambiguous))
}

// renderMarkdown is the internal implementation. It renders from the path-stamped
// JSONFinding records (the library Merged no longer carries path-validation
// fields, Phase 2 Clarification Q1) and accepts a pre-built DisagreementsFile so
// Emit can build the radar once and share it between report.md and
// disagreements.json without a second O(n log n) sort pass.
func renderMarkdown(w io.Writer, summary Summary, findings []JSONFinding, df DisagreementsFile) error {
	inScope := make([]JSONFinding, 0, len(findings))
	var outOfScope []JSONFinding
	for _, m := range findings {
		if m.Category == CategoryOutOfScope {
			outOfScope = append(outOfScope, m)
		} else {
			inScope = append(inScope, m)
		}
	}

	var b bytes.Buffer
	b.WriteString("# atcr Reconciled Review\n\n")

	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- Total findings: %d\n", summary.TotalFindings)
	fmt.Fprintf(&b, "- Sources: %s\n", esc(joinOrNone(summary.SourcesScanned)))
	fmt.Fprintf(&b, "- Clusters collapsed: %d\n", summary.ClustersCollapsed)
	fmt.Fprintf(&b, "- Severity disagreements: %d\n", summary.SeverityDisagreements)
	if summary.AuthorityPromoted > 0 {
		// Surface PageRank authority promotions (Epic 13.5) to report-only readers
		// so a misfiring promotion is visible without reading summary.json. Rendered
		// only when nonzero, keeping report.md byte-identical on the common path.
		fmt.Fprintf(&b, "- Authority promoted: %d\n", summary.AuthorityPromoted)
	}
	if summary.ConsensusFiltered > 0 {
		// Surface epic-14.2 consensus filtering to report-only readers: uncorroborated
		// singletons routed to the ambiguous sidecar. Rendered only when nonzero so
		// report.md stays byte-identical on the common (small-panel) path.
		fmt.Fprintf(&b, "- Consensus filtered: %d (uncorroborated singletons routed to the ambiguous sidecar)\n", summary.ConsensusFiltered)
	}
	if len(outOfScope) > 0 {
		fmt.Fprintf(&b, "- Out-of-scope findings: %d (annotated, excluded from the gate)\n", len(outOfScope))
	}
	if summary.Partial {
		b.WriteString("- Partial: yes (a source was missing or unreadable)\n")
	}
	b.WriteString("\n")
	writeSeverityConfidenceTable(&b, inScope)

	// Disagreement radar above the consensus findings (Epic 3.2). Nothing is
	// written when there is no tension, so report.md is byte-identical to the
	// pre-3.2 output for a review with no disagreements. The reconciled report
	// uses esc (verbatim/archival) for free-text body fields; the display view
	// in internal/report passes escTrunc through the same shared renderer.
	WriteRadarSection(&b, df, esc)

	if len(findings) == 0 {
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
// given (the caller passes sortMerged-ordered slices). It renders from the
// path-stamped JSONFinding records so the hallucinated-path warning (Epic 5.0)
// survives the library Merged dropping its path-validation fields (Phase 2
// Clarification Q1).
func writeFindingsList(b *bytes.Buffer, findings []JSONFinding) {
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
func writeSeverityConfidenceTable(b *bytes.Buffer, findings []JSONFinding) {
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

// Esc makes a free-text field safe in markdown: newlines are flattened (so a
// field cannot inject markdown structure), HTML metacharacters are escaped
// (so raw HTML never renders), and backticks are escaped so reviewer-controlled
// fields cannot open an inline code span inside a normal bullet. It is the
// single source of truth for this escaping contract; report/render.go's esc
// delegates here so the two packages cannot drift apart.
func Esc(s string) string {
	return strings.ReplaceAll(html.EscapeString(newlineFlattener.Replace(s)), "`", "&#96;")
}

// esc is the package-local alias for Esc, kept so existing callers in this
// package continue to compile unchanged.
func esc(s string) string { return Esc(s) }

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
