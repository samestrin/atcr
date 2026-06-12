// Package reconcile merges findings from all sources deterministically:
// discovery, normalization, (FILE, LINE±3) clustering, Jaccard token-set
// dedupe, merge rules, confidence scoring, disagreement annotation, and the
// ambiguous.json sidecar.
package reconcile
