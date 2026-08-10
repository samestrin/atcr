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
// dedupe/confidence/attribution building blocks; the severity rubric
// (SeverityRank, NormalizeSeverity); the consensus-filter levels (ConsensusOff,
// ConsensusLenient, ConsensusStrict, ConsensusLevels, NormalizeConsensus and
// InvalidConsensusError); and the closed reviewer CATEGORY vocabulary
// (Categories, CategoryMerges, plus the Category* constants). The building-block
// group is an umbrella over the exported cluster/merge/confidence/attribution
// helpers and their tuning constants (MergeThreshold, LineProximity,
// EvidenceSep, FixAttributionPrefix); every other exported group above is named
// in full. Path-validation fields, the findings.json schema, the disagreement
// radar, and adjudication stay ATCR-internal by design.
//
// This paragraph is the contract external embedders read, so a group that names
// its members must name all of them: a silently omitted symbol in an otherwise
// complete enumeration is worse than no enumeration at all.
//
// Casing conventions: severity tiers (Severity*, Sev*) and confidence tiers
// (Conf*, ConfidenceVerified) are canonical UPPER-CASE strings. Verdict enum
// values (VerdictConfirmed, VerdictRefuted, VerdictUnverifiable), consensus
// levels (Consensus*) and CATEGORY values (Category*) are canonical lower-case
// strings. Consumers normalize via strings.ToUpper/ToLower where
// case-insensitive comparison is required.
package reconcile
