// Package reconcile merges findings from all sources deterministically:
// discovery, normalization, (FILE, LINE±3) clustering, Jaccard token-set
// dedupe, merge rules, confidence scoring, disagreement annotation, and the
// ambiguous.json sidecar.
//
// Tests in this package must NOT call t.Parallel: the Tier 4 constructor seam
// (newTier4Index in validate.go) is a mutable package-level var that tests
// swap and restore, so parallel tests would race on it.
package reconcile
