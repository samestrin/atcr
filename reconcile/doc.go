// Package reconcile is the standalone, embeddable implementation of ATCR's
// deterministic finding reconciler: (FILE, LINE±3) clustering, Jaccard
// token-set dedupe, merge rules, confidence scoring, ambiguity sidecar, and
// inline disagreement annotation.
//
// The package is stdlib-only in non-test files (its tests use only the standard
// `testing` package, so `go mod tidy` yields an empty require block). ATCR
// consumes it through a boundary adapter; external tools embed it directly via
// Reconcile(sources []Source, opts Options) Result.
//
// Public surface: Reconcile, Options, Result, Summary, Merged, Source, Finding,
// AmbiguousCluster, Verification and the Verdict* constants; the merge/cluster/
// dedupe/confidence/attribution building blocks; and the severity rubric
// (SeverityRank, NormalizeSeverity). Path-validation fields, the findings.json
// schema, the disagreement radar, and adjudication stay ATCR-internal by design.
package reconcile
