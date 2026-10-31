package reconcile

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"
)

// This file is ATCR's compatibility surface over the extracted reconcile library
// (github.com/samestrin/atcr/reconcile, Epic 8.0). The deterministic core —
// clustering, dedupe, merge, confidence, severity, attribution, ambiguity
// content-addressing — now lives in the library; internal/reconcile re-exports
// the moved types/constants/functions so every existing ATCR consumer keeps
// compiling unchanged until it flips to the library import in Phase 3.
//
// What STAYS ATCR-internal (not in the library, by Phase-2 design): the
// findings.json JSONFinding schema and its path-validation fields, the
// MergeJSONFindings gray-zone merge family, the disagreement radar
// (BuildDisagreements / DisagreementsFile), the gate, path validation, source
// discovery, adjudication, and all file I/O.

// Re-exported types (aliases — identical types, so a Phase-3 import flip is a
// pure path change with no behavioral difference).
type (
	// Finding is the library's per-finding wire record.
	Finding = reclib.Finding
	// Merged is one reconciled finding.
	Merged = reclib.Merged
	// SourceCategory is one source finding's own category in a merged cluster.
	SourceCategory = reclib.SourceCategory
	// AmbiguousCluster is a gray-zone pair left unmerged.
	AmbiguousCluster = reclib.AmbiguousCluster
	// Options parameterizes a reconcile run.
	Options = reclib.Options
	// Summary is the run-stats record.
	Summary = reclib.Summary
)

// Re-exported severity/confidence/category constants (canonical values now owned
// by the library).
const (
	SevCritical = reclib.SevCritical
	SevHigh     = reclib.SevHigh
	SevMedium   = reclib.SevMedium
	SevLow      = reclib.SevLow

	ConfHigh   = reclib.ConfHigh
	ConfMedium = reclib.ConfMedium
	ConfLow    = reclib.ConfLow

	ConfidenceVerified = reclib.ConfidenceVerified

	CategoryOutOfScope = reclib.CategoryOutOfScope
)

// UnresolvedReasonDocShield marks a finding routed because its PROBLEM anchors
// were named only in a documentation-extension file, re-exported from the
// library (it is a wire value in reconciled/unresolved.json).
// TestDocShieldLocalCopy_MatchesInTree pins it to the in-tree declaration and
// the wire value "doc_shield".
const UnresolvedReasonDocShield = reclib.UnresolvedReasonDocShield

// SeverityRank is the canonical severity rubric, re-exported from the library
// (single source of truth — no second copy). It is copied defensively so an
// ATCR-internal mutation cannot accidentally corrupt the shared library map.
var SeverityRank = func() map[string]int {
	cp := make(map[string]int, len(reclib.SeverityRank))
	for k, v := range reclib.SeverityRank {
		cp[k] = v
	}
	return cp
}()

// Re-exported pure functions. These delegate to the library so there is exactly
// one implementation of each.
func NormalizeSeverity(s string) string { return reclib.NormalizeSeverity(s) }

func AmbiguousID(file string, line int, problemA, problemB string) string {
	return reclib.AmbiguousID(file, line, problemA, problemB)
}

func AmbiguousHash(clusters []AmbiguousCluster) string { return reclib.AmbiguousHash(clusters) }

func HashBytes(data []byte) string { return reclib.HashBytes(data) }

func ConfidenceFor(reviewerCount int) string { return reclib.ConfidenceFor(reviewerCount) }

func ConfidenceForCategory(reviewerCount int, category string, independent bool) string {
	return reclib.ConfidenceForCategory(reviewerCount, category, independent)
}

func IsPanelWide(group []Finding) bool { return reclib.IsPanelWide(group) }

// PanelWideReason is the JSONFinding.ConfidenceDiscount value for the
// panel-wide-instruction discount (Epic 35.16.17), re-exported from the library.
const PanelWideReason = reclib.PanelWideReason

// discountedConfidence is reclib.DiscountedConfidence over a record's quotes:
// the confidence for reviewerCount voices and the reason to record on
// JSONFinding.ConfidenceDiscount, with independence decided by
// reclib.IsPanelWideQuotes. It is the rule reclib.Merge applies, so the
// cluster merge and the fallback recompute record a discount exactly when the
// library would, and take the reason from the same source.
func discountedConfidence(reviewerCount int, category string, quotes []reclib.SourceQuote) (confidence, discount string) {
	return reclib.DiscountedConfidence(reviewerCount, category, !reclib.IsPanelWideQuotes(quotes))
}

// memberQuotes returns a findings.json record's per-reviewer quotes for the
// independence test: its SourceQuotes when it carries them (a record merged
// from two or more findings), else one quote per reviewer of its own file,
// category and evidence. The fallback is exact for a one-finding record, whose
// evidence is its only quote. A record with no reviewers (hand-built, or an old
// findings.json) still yields one quote with an empty Reviewer: it adds no
// voice to the distinct-reviewer count, but its file and evidence are tested,
// so it cannot drop out of the test. A multi-reviewer record written before
// SourceQuotes existed is read with its joined evidence as every reviewer's
// quote, which rarely matches a quote, so a union with it reads as independent:
// the same accepted false negative reclib.IsPanelWide documents.
func memberQuotes(f JSONFinding) []reclib.SourceQuote {
	if len(f.SourceQuotes) > 0 {
		return f.SourceQuotes
	}
	reviewers := f.Reviewers
	if len(reviewers) == 0 {
		reviewers = []string{""}
	}
	out := make([]reclib.SourceQuote, 0, len(reviewers))
	for _, r := range reviewers {
		out = append(out, reclib.SourceQuote{
			Reviewer: r,
			File:     f.File,
			Category: strings.ToLower(strings.TrimSpace(f.Category)),
			Evidence: f.Evidence,
		})
	}
	return out
}

// Result is ATCR's reconcile result. It mirrors reclib.Result (its fields are the
// library types via the aliases above) but is a distinct internal type so the
// findings.json-producing JSONFindings method can live in this package
// (emit.go). The Phase-3 consumer flip replaces it with reclib.Result directly.
type Result struct {
	Findings  []Merged
	Ambiguous []AmbiguousCluster
	// Unresolved holds the findings the Epic 35.16.6.5 Tier 4 content check
	// routed OUT of the primary stream: their cited file does not exist, no
	// filename-level tier produced a candidate, and the constructs their prose
	// names are declared nowhere in the tracked tree. They are written to
	// unresolved.json and are absent from Findings/JSONFindings — which is how
	// they stay out of report.md and out of the findings.json subset
	// internal/verify reads, with no change to internal/verify itself (AC3).
	// Preserved, never deleted (AC4). Empty on every path but RunReconcile,
	// which is the only one with a checked-out tree to resolve against.
	Unresolved []JSONFinding
	// Suppressed holds the findings a .atcr/ignore.yaml rule (Epic 40.0)
	// matched: dropped from Findings/JSONFindings, so they stay out of
	// findings.json, report.md and the gate, and kept here for the
	// suppressed.json sidecar and the scorecard. Internal only: the published
	// reconcile module and its Summary carry no suppressed field. Empty on
	// every path but RunReconcile, the only one with a checkout to read
	// rules from.
	Suppressed []JSONFinding
	Summary    Summary
	// ReviewerRoles maps a reviewer name to its persona role, so report.md can
	// label it `name (role)` (Epic 83.0 T5). Display only: no JSON artifact
	// carries it. Nil leaves every reviewer bare. RunReconcile sets it from the
	// review dir's reviewer_roles.json sidecar (ReadReviewerRoles).
	ReviewerRoles map[string]string
	// jsonFindings caches the path-validated findings.json records the ATCR I/O
	// layer stamps after reconcile (Phase 2 Clarification Q1): the library Merged
	// no longer carries PathValid/PathWarning/PathSuggestion, so path validation
	// runs on these records instead. When set (by RunReconcile after
	// validateFindingPaths), JSONFindings returns it so every downstream
	// consumer — the report, the gate's inline failing list — sees the same
	// path-stamped records. When unset (a Result built directly, e.g. a test with
	// no path validation), JSONFindings derives path-less records from Findings.
	jsonFindings []JSONFinding
	// ambiguousBytes caches the rendered ambiguous.json bytes (emit.go wire shape)
	// so Reconcile, ambiguousHash, and Emit share one marshal instead of two.
	// When unset, Emit and ambiguousHash fall back to rendering from Ambiguous.
	ambiguousBytes []byte
}

// Reconcile is the ATCR entry point. It bridges ATCR's discovery Source (which
// carries file-I/O bookkeeping the public library Source omits) to the library
// Source, runs the library pipeline, and stamps the skipped-source bookkeeping
// back onto the summary — the one piece the stdlib-only library cannot produce
// because it reconciles in-memory findings, not files (Phase 2 Clarification Q3).
func Reconcile(sources []Source, opts Options) Result {
	lib := make([]reclib.Source, len(sources))
	skipped := []string{}
	for i, s := range sources {
		fs := make([]Finding, len(s.Findings))
		for j, f := range s.Findings {
			// Inline the stream.Finding -> reconcile.Finding field map. The canonical
			// implementation lives in the boundary adapter (internal/reconcile/adapter),
			// but this package cannot import adapter without an import cycle (adapter
			// imports this package), so this copy stays inline by design — resolved
			// as of Epic 8.0 Clarification Q8/Q9 (2026-06-23): the duplication is
			// permanent under the current package layout, not a pending collapse.
			fs[j] = Finding{
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
		lib[i] = reclib.Source{Name: s.Name, Findings: fs}
		skipped = append(skipped, s.SkippedFiles...)
	}
	lr := reclib.Reconcile(lib, opts)
	sort.Strings(skipped)
	lr.Summary.SkippedSources = skipped
	lr.Summary.SkippedSourceCount = len(skipped)
	// The library hashes its own stdlib-only AmbiguousCluster serialization; ATCR
	// emits ambiguous.json via a wire shape that reproduces the pre-extraction
	// bytes (emit.go toAmbiguousWire), so re-bind ambiguous_hash to those exact
	// bytes to keep summary.json byte-identical (Epic 8.0 AC 01-05). Render once
	// and cache the bytes so Emit can reuse them without a second marshal.
	res := Result{Findings: lr.Findings, Ambiguous: lr.Ambiguous, Summary: lr.Summary}
	var ambBuf bytes.Buffer
	if err := renderIndentedJSON(&ambBuf, toAmbiguousWire(res.Ambiguous)); err != nil {
		panic(fmt.Sprintf("atcr: Reconcile: unreachable ambiguous JSON render error: %v", err))
	}
	res.ambiguousBytes = ambBuf.Bytes()
	res.Summary.AmbiguousHash = HashBytes(res.ambiguousBytes)
	return res
}
