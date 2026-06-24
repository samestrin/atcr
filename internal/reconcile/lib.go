package reconcile

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/samestrin/atcr/internal/stream"
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

	MergeThreshold = reclib.MergeThreshold
	GrayLow        = reclib.GrayLow

	EvidenceSep          = reclib.EvidenceSep
	FixAttributionPrefix = reclib.FixAttributionPrefix
)

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

func Merge(group []Finding) Merged { return reclib.Merge(group) }

func Cluster(findings []Finding) [][]Finding { return reclib.Cluster(findings) }

func DedupeCluster(cluster []Finding) ([][]Finding, []AmbiguousCluster) {
	return reclib.DedupeCluster(cluster)
}

func AmbiguousID(file string, line int, problemA, problemB string) string {
	return reclib.AmbiguousID(file, line, problemA, problemB)
}

func AmbiguousHash(clusters []AmbiguousCluster) string { return reclib.AmbiguousHash(clusters) }

func HashBytes(data []byte) string { return reclib.HashBytes(data) }

func ConfidenceForVerdict(prior, verdict string) string {
	return reclib.ConfidenceForVerdict(prior, verdict)
}

func ConfidenceAtOrAbove(c, floor string) bool { return reclib.ConfidenceAtOrAbove(c, floor) }

func HasFixAttribution(evidence, name string) bool { return reclib.HasFixAttribution(evidence, name) }

func AppendFixAttribution(evidence, name string) string {
	return reclib.AppendFixAttribution(evidence, name)
}

// Result is ATCR's reconcile result. It mirrors reclib.Result (its fields are the
// library types via the aliases above) but is a distinct internal type so the
// findings.json-producing JSONFindings method can live in this package
// (emit.go). The Phase-3 consumer flip replaces it with reclib.Result directly.
type Result struct {
	Findings  []Merged
	Ambiguous []AmbiguousCluster
	Summary   Summary
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
		for j := range s.Findings {
			fs[j] = toLibFinding(s.Findings[j])
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

// toLibFinding converts an ATCR stream.Finding (per-source input) into the
// library Finding: the 9 wire fields plus the reviewer columns. Path-validation
// fields are ATCR-internal and are deliberately not carried (they are re-stamped
// onto the JSONFinding after reconcile, per Phase 2 Clarification Q1).
func toLibFinding(f stream.Finding) Finding {
	return Finding{
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

// fromLibFinding converts a reconciled library Finding back into an ATCR
// stream.Finding so the ATCR I/O layer can stamp path-validation fields onto it.
// The library finding's Disagreement and Verification have no stream.Finding home
// (they ride on the Merged/JSONFinding records); path fields come back zeroed and
// are stamped by validateFindingPaths.
func fromLibFinding(f Finding) stream.Finding {
	return stream.Finding{
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
