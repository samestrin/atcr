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
	// AuthorityPromoted counts findings PageRank authority promotion (epic 13.3,
	// promoteByAuthority) raised from MEDIUM to HIGH confidence in this run. It is
	// observability only — promotion behavior is unchanged — surfacing a misfiring
	// promotion that would otherwise be derivable only indirectly as a "HIGH with a
	// single reviewer."
	AuthorityPromoted int  `json:"authority_promoted"`
	Partial           bool `json:"partial"`
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
	// AmbiguousCount is the total number of entries in the ambiguous sidecar.
	AmbiguousCount int `json:"ambiguous_count"`
	// NoiseCount is the number of single-finding ambiguous entries isolated as
	// DBSCAN noise (as opposed to multi-finding gray pairs).
	NoiseCount int `json:"noise_count"`
	// ConsensusFiltered is the number of uncorroborated singletons the epic-14.2
	// consensus filter routed to the ambiguous sidecar (single-reviewer, MEDIUM
	// confidence, not exempt) when the panel had at least consensusMinSources
	// sources. Zero when the panel was too small for the filter to run or nothing
	// was dropped. Observability only — the dropped findings live in the sidecar.
	ConsensusFiltered int `json:"consensus_filtered"`
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

	// First pass: dedupe every cluster into merge groups. The groups are collected
	// across ALL clusters before any confidence is assigned because authority
	// (epic 13.3) is a run-global property — a model's PageRank depends on every
	// agreement it took part in, not just the ones inside one location cluster.
	allGroups := make([][]Finding, 0, len(clusters))
	ambiguous := []AmbiguousCluster{}
	for _, cl := range clusters {
		groups, amb := dedupeCluster(cl, clusterKeys(cl, opts.Grouper), opts.Merges)
		ambiguous = append(ambiguous, amb...)
		allGroups = append(allGroups, groups...)
	}

	// Build the run-global model-agreement graph and compute per-model authority.
	// An empty result (no cross-model agreement) disables promotion, keeping
	// confidence byte-identical to the pre-13.3 vote-count behavior.
	authority := modelAuthority(allGroups)
	var baseline float64
	if len(authority) > 0 {
		baseline = 1.0 / float64(len(authority))
	}

	// Second pass: merge each group and assign authority-aware confidence.
	var merged []Merged
	clustersCollapsed, disagreements, authorityPromoted := 0, 0, 0
	for _, g := range allGroups {
		base := Merge(g)
		m := promoteByAuthority(base, authority, baseline)
		// Count an actual authority-driven flip: the merged group was MEDIUM by the
		// vote-count rule and promotion raised it to HIGH. Comparing before/after
		// keeps the counter exact even if promoteByAuthority's predicate evolves.
		if base.Confidence == ConfMedium && m.Confidence == ConfHigh {
			authorityPromoted++
		}
		if len(g) >= 2 {
			clustersCollapsed++
		}
		if m.Disagreement != "" {
			disagreements++
		}
		merged = append(merged, m)
	}
	sortMerged(merged)

	// NoiseCount reflects DBSCAN-isolated singletons only, so capture it before the
	// consensus filter appends its own single-finding clusters to the sidecar below.
	noiseCount := 0
	for _, c := range ambiguous {
		if len(c.Findings) == 1 {
			noiseCount++
		}
	}

	// Consensus filter (epic 14.2): once the panel is large enough that a real issue
	// is likely to be caught by more than one reviewer (>= consensusMinSources), an
	// uncorroborated singleton is more plausibly a hallucination than a rare true
	// positive, so route it to the ambiguous sidecar instead of promoting it to
	// findings.json — UNLESS a false negative would be too costly (consensusExempt).
	// This runs after DBSCAN clustering (first pass) and the merge/authority passes,
	// so consensusSingleton sees each finding's final confidence (authority-promoted
	// singletons are HIGH and never dropped). The panel-size gate preserves the
	// documented single-API-key workflow (host + 1 pool agent = 2 sources), where
	// nearly every finding is a singleton. Filtered findings stay sorted-order-stable
	// (kept preserves order) and recoverable from the sidecar for adjudication.
	consensusFiltered := 0
	if len(sources) >= consensusMinSources {
		kept := merged[:0]
		for _, m := range merged {
			if consensusSingleton(m) && !consensusExempt(m.Finding) {
				ambiguous = append(ambiguous, consensusNoiseCluster(m.Finding))
				consensusFiltered++
				continue
			}
			kept = append(kept, m)
		}
		merged = kept
	}

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
			AuthorityPromoted:     authorityPromoted,
			Partial:               opts.Partial,
			SkippedSources:        []string{},
			SkippedSourceCount:    0,
			AmbiguousHash:         AmbiguousHash(ambiguous),
			AmbiguousCount:        len(ambiguous),
			NoiseCount:            noiseCount,
			ConsensusFiltered:     consensusFiltered,
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

// clusterKeys returns the AST group key for each finding in a cluster (empty
// strings when g is nil or supplies no key), aligned by index with cluster. The
// keys feed the composite edge-weight distance: two findings sharing a non-empty
// key are structurally isomorphic (13.1) and matched at distance 0.
func clusterKeys(cluster []Finding, g Grouper) []string {
	keys := make([]string, len(cluster))
	if g == nil {
		return keys
	}
	for i, f := range cluster {
		keys[i] = g.GroupKey(f)
	}
	return keys
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
