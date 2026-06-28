package reconcile

import (
	"sort"
	"time"
)

// Options parameterizes a reconcile run. ReconciledAt stamps the summary; Partial
// is true when at least one expected source was missing/unreadable while others
// succeeded (threaded from the caller). Merges is the set of ambiguous cluster
// ids a host adjudicated as duplicates; each is force-merged instead of left in
// the gray zone. Nil means no adjudication (the conservative default).
type Options struct {
	ReconciledAt time.Time
	Partial      bool
	Merges       map[string]bool
	// Root is the base directory file-existence validation resolves finding paths
	// against. It is carried for embedders that layer their own path validation on
	// the result; the core reconcile pipeline does not read it. Empty disables that
	// downstream concern.
	Root string
	// Grouper, when non-nil, supplies the primary clustering signal (AST
	// isomorphism): findings whose GroupKey matches cluster together regardless of
	// line distance, and findings with an empty key fall back to line proximity.
	// Nil keeps the legacy line-proximity-only behavior. The interface is
	// stdlib-only so wiring a wazero-backed grouper adds no dependency here.
	Grouper Grouper
}

// Result is a completed reconciliation: the merged findings (sorted for
// deterministic output), the ambiguous sidecar, and the run summary.
type Result struct {
	Findings  []Merged
	Ambiguous []AmbiguousCluster
	Summary   Summary
}

// Summary is the run-stats record.
type Summary struct {
	SourcesScanned        []string       `json:"sources_scanned"`
	PerSourceCounts       map[string]int `json:"per_source_counts"`
	ClustersCollapsed     int            `json:"clusters_collapsed"`
	SeverityDisagreements int            `json:"severity_disagreements"`
	Partial               bool           `json:"partial"`
	// SkippedSources lists source paths an embedder dropped on a read error or bad
	// header: warn-and-continue degradation is recorded here rather than
	// exit-coded, mirroring the Partial flag's contract. The core library leaves
	// this empty (it reconciles in-memory findings, not files); an embedding I/O
	// layer that discovers sources stamps it after Reconcile returns.
	SkippedSources     []string `json:"skipped_sources"`
	SkippedSourceCount int      `json:"skipped_source_count"`
	// AmbiguousHash digests the emitted ambiguous sidecar bytes; a host copies it
	// verbatim into an adjudication file as the baseline hash.
	AmbiguousHash string `json:"ambiguous_hash"`
	// OutOfScope counts findings annotated out-of-scope: kept in the artifacts but
	// excluded from a severity gate.
	OutOfScope    int    `json:"out_of_scope"`
	TotalFindings int    `json:"total_findings"`
	ReconciledAt  string `json:"reconciled_at"`
}

// Reconcile runs the deterministic pipeline: cluster all findings by location,
// dedupe each cluster, merge duplicate groups, assign confidence, and collect the
// ambiguous sidecar and run summary. Output findings are sorted by severity
// (desc), then file, then line, so the same input always yields byte-identical
// artifacts.
//
// When opts.Grouper is set (AST-isomorphism grouping), clustering additionally
// depends on the inputs that grouper reads — for the astgroup grouper, the source
// tree it parses. Reconcile stays deterministic for a fixed (findings, source
// tree) pair; reproducibility therefore holds per checkout, since a review runs
// against a fixed working tree. A nil Grouper keeps clustering a pure function of
// the findings.
func Reconcile(sources []Source, opts Options) Result {
	clusters := ClusterWith(AllFindings(sources), opts.Grouper)

	var merged []Merged
	ambiguous := []AmbiguousCluster{}
	clustersCollapsed, disagreements := 0, 0

	for _, cl := range clusters {
		groups, amb := dedupeCluster(cl, opts.Merges)
		ambiguous = append(ambiguous, amb...)
		for _, g := range groups {
			m := Merge(g)
			if len(g) >= 2 {
				clustersCollapsed++
			}
			if m.Disagreement != "" {
				disagreements++
			}
			merged = append(merged, m)
		}
	}
	sortMerged(merged)
	outOfScope := 0
	for _, m := range merged {
		if m.Category == CategoryOutOfScope {
			outOfScope++
		}
	}

	return Result{
		Findings:  merged,
		Ambiguous: ambiguous,
		Summary: Summary{
			SourcesScanned:        sourceNames(sources),
			PerSourceCounts:       perSourceCounts(sources),
			ClustersCollapsed:     clustersCollapsed,
			SeverityDisagreements: disagreements,
			Partial:               opts.Partial,
			SkippedSources:        []string{},
			SkippedSourceCount:    0,
			AmbiguousHash:         AmbiguousHash(ambiguous),
			OutOfScope:            outOfScope,
			TotalFindings:         len(merged),
			ReconciledAt:          opts.ReconciledAt.UTC().Format(time.RFC3339),
		},
	}
}

// sortMerged orders findings by severity (most severe first), then file, then
// line, then Problem — a strict total order independent of input permutation.
func sortMerged(m []Merged) {
	sort.SliceStable(m, func(i, j int) bool {
		ri, rj := SeverityRank[NormalizeSeverity(m[i].Severity)], SeverityRank[NormalizeSeverity(m[j].Severity)]
		if ri != rj {
			return ri > rj
		}
		if m[i].File != m[j].File {
			return m[i].File < m[j].File
		}
		if m[i].Line != m[j].Line {
			return m[i].Line < m[j].Line
		}
		return m[i].Problem < m[j].Problem
	})
}

// AllFindings flattens the findings across sources in source order.
func AllFindings(sources []Source) []Finding {
	var out []Finding
	for _, s := range sources {
		out = append(out, s.Findings...)
	}
	return out
}

// sourceNames returns the source names in sorted order.
func sourceNames(sources []Source) []string {
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}

// perSourceCounts maps each source name to its input finding count.
func perSourceCounts(sources []Source) map[string]int {
	counts := make(map[string]int, len(sources))
	for _, s := range sources {
		counts[s.Name] = len(s.Findings)
	}
	return counts
}
