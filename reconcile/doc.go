// Package reconcile is the standalone, embeddable implementation of ATCR's
// deterministic finding reconciler: (FILE, LINE±3) clustering, Jaccard
// token-set dedupe, merge rules, confidence scoring, ambiguity sidecar, and
// inline disagreement annotation.
//
// The package is stdlib-only in non-test files. ATCR consumes it through a
// boundary adapter; external tools embed it directly via
// Reconcile(sources []Source, opts Options) Result.
//
// This file is the Phase-1 scaffold. The reconcile pipeline (Reconcile,
// Options, Result, Summary, Merged, and the clustering/dedupe/merge logic) is
// moved in during Phase 2; Phase 1 establishes the module, the root replace
// directive, and the public types Phase 2 populates (Verification, the Verdict
// constants, Source, and Finding).
package reconcile
