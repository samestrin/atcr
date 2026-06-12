package reconcile

import (
	"sort"
	"time"
)

// Options parameterizes a reconcile run. ReconciledAt stamps summary.json;
// Partial is true when at least one expected source was missing/unreadable while
// others succeeded (threaded from the caller, e.g. the fan-out manifest). Merges
// is the set of ambiguous cluster ids the Skill adjudicated as duplicates (from
// adjudication.json on a re-invocation); each is force-merged instead of left in
// the gray zone. Nil means no adjudication (the conservative default).
type Options struct {
	ReconciledAt time.Time
	Partial      bool
	Merges       map[string]bool
}

// Result is a completed reconciliation: the merged findings (sorted for
// deterministic output), the ambiguous sidecar, and the run summary.
type Result struct {
	Findings  []Merged
	Ambiguous []AmbiguousCluster
	Summary   Summary
}

// Summary is the run-stats record written to summary.json (AC 01-05 Scenario 6).
type Summary struct {
	SourcesScanned        []string       `json:"sources_scanned"`
	PerSourceCounts       map[string]int `json:"per_source_counts"`
	ClustersCollapsed     int            `json:"clusters_collapsed"`
	SeverityDisagreements int            `json:"severity_disagreements"`
	Partial               bool           `json:"partial"`
	// SkippedSources lists findings.txt paths Discover dropped on a read error
	// or bad header (TD-020): warn-and-continue degradation is recorded here
	// rather than exit-coded, mirroring the partial flag's contract.
	SkippedSources     []string `json:"skipped_sources"`
	SkippedSourceCount int      `json:"skipped_source_count"`
	// AmbiguousHash digests the emitted ambiguous.json bytes (TD-024); the
	// Skill copies it verbatim into adjudication.json as baseline_hash.
	AmbiguousHash string `json:"ambiguous_hash"`
	// OutOfScope counts findings annotated out-of-scope (AC 06-04): kept in
	// the artifacts but excluded from the severity gate.
	OutOfScope    int    `json:"out_of_scope"`
	TotalFindings int    `json:"total_findings"`
	ReconciledAt  string `json:"reconciled_at"`
}

// Reconcile runs the deterministic pipeline: cluster all findings by location,
// dedupe each cluster, merge duplicate groups, assign confidence, and collect
// the ambiguous sidecar and run summary. Output findings are sorted by severity
// (desc), then file, then line, so the same input always yields byte-identical
// artifacts.
func Reconcile(sources []Source, opts Options) Result {
	clusters := Cluster(AllFindings(sources))

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
	skipped := skippedSourceFiles(sources)
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
			SkippedSources:        skipped,
			SkippedSourceCount:    len(skipped),
			AmbiguousHash:         AmbiguousHash(ambiguous),
			OutOfScope:            outOfScope,
			TotalFindings:         len(merged),
			ReconciledAt:          opts.ReconciledAt.UTC().Format(time.RFC3339),
		},
	}
}

// sortMerged orders findings by severity (most severe first), then file, then
// line — a total order for deterministic emission.
func sortMerged(m []Merged) {
	sort.SliceStable(m, func(i, j int) bool {
		ri, rj := severityRank[m[i].Severity], severityRank[m[j].Severity]
		if ri != rj {
			return ri > rj
		}
		if m[i].File != m[j].File {
			return m[i].File < m[j].File
		}
		return m[i].Line < m[j].Line
	})
}

// sourceNames returns the discovered source names in sorted order.
func sourceNames(sources []Source) []string {
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}

// skippedSourceFiles flattens the per-source skipped findings.txt paths into a
// sorted list (always non-nil so summary.json serializes [] rather than null).
func skippedSourceFiles(sources []Source) []string {
	out := []string{}
	for _, s := range sources {
		out = append(out, s.SkippedFiles...)
	}
	sort.Strings(out)
	return out
}

// perSourceCounts maps each source name to its input finding count.
func perSourceCounts(sources []Source) map[string]int {
	counts := make(map[string]int, len(sources))
	for _, s := range sources {
		counts[s.Name] = len(s.Findings)
	}
	return counts
}
