package reconcile

import (
	"encoding/json"
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
	// TrustPriors maps a reviewer's lowercase name to its historical
	// corroboration rate (findings corroborated / findings raised), the shape
	// scorecard.TrustPriors (epic 35.8) returns. It exempts a high-trust
	// reviewer's singleton from the consensus filter (trustExempt) and demotes
	// a low-trust reviewer's singleton to ConfLow (demoteByTrust) — pure data
	// in, no I/O inside the library, the same injection shape as Grouper. Nil
	// or empty is a complete no-op: confidence and filtering stay
	// byte-identical to pre-35.9 behavior, mirroring epic 13.3's
	// empty-authority-map guarantee.
	TrustPriors map[string]float64
	// Consensus selects the consensus filter's corroboration bar (epic 35.9.1):
	// ConsensusStrict (the default, and what "" means), ConsensusLenient, or
	// ConsensusOff. It moves ONLY that bar — the consensusMinReviewers panel
	// floor and the consensusExempt/trustExempt exemptions are identical at
	// every level, and demoteByTrust runs ahead of the filter unaffected by it.
	// An empty or unrecognized value resolves to strict (consensusFloor fails
	// safe), so a caller that never sets this field, or one that bypassed
	// boundary validation, keeps byte-identical pre-35.9.1 behavior.
	Consensus string
}

// Result is a completed reconciliation: the merged findings (sorted for
// deterministic output), the ambiguous sidecar, and the run summary.
type Result struct {
	Findings  []Merged
	Ambiguous []AmbiguousCluster
	Summary   Summary
}

// The canonical Summary.UnresolvedState values. See that field's doc for why a
// bare UnresolvedFiltered count cannot stand in for them.
const (
	// UnresolvedStateApplied means the Tier 4 content check was IN FORCE for this
	// run: every verdict it could reach was available to it. A 0
	// UnresolvedFiltered here is the healthy case — nothing was judged fabricated.
	//
	// It does NOT assert that an index was built, and on the most common path
	// none is: when every finding cites a file that exists, no finding is
	// Tier-4-eligible, so the index is never constructed and not one file is read
	// (the AC5 laziness). "In force with nothing to adjudicate" and "in force and
	// found nothing to route" are both applied, and both are healthy.
	UnresolvedStateApplied = "applied"
	// UnresolvedStateDisabled means Tier 4 never ran: the AST opt-out was set, or
	// there was no tracked file index to search (the root is not a git
	// repository, or git was unavailable).
	UnresolvedStateDisabled = "disabled"
	// UnresolvedStateUnavailable means Tier 4 was in force but could not reach a
	// usable index — over the file cap, nothing in the tracked tree readable, no
	// root-contained file, or a parser failure that left the declaration set
	// empty. Nothing was resolved: with no declarations there is no file to point
	// a suggestion at.
	//
	// It does NOT mean nothing was routed. In the parser-failure case an index
	// WAS built, so the raw-token search still ran and findings naming constructs
	// absent from the tree are still routed to the sidecar. A nonzero routed
	// count alongside this state is expected, not a contradiction. Only the
	// no-index cases route nothing, and they are not distinguishable from this
	// value alone.
	UnresolvedStateUnavailable = "unavailable"
	// UnresolvedStateIncomplete means the index was built but some eligible
	// tracked file could not be read, so a region of the tree went unsearched and
	// every no-match verdict was withheld. No finding was routed under this
	// state, and none could be.
	//
	// Resolutions were still available — the resolution branches run before the
	// completeness gate — UNLESS the same run also lost its declaration set to a
	// parser failure. That combination reports incomplete rather than
	// unavailable, because the unread region is what withheld the verdicts and is
	// the condition an operator can act on; it also increments BOTH the
	// unavailable and the incomplete counter.
	UnresolvedStateIncomplete = "incomplete"
)

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
	// consensus filter routed to the ambiguous sidecar (single-reviewer, below the
	// confidence bar, not exempt) when the panel had at least consensusMinReviewers
	// distinct reviewers. The bar is opts.Consensus-dependent (see the
	// Options.Consensus godoc): below-HIGH under strict, below-MEDIUM under
	// lenient. Zero when the panel was too small for the filter to run, when
	// nothing was dropped, or when the filter was turned off entirely
	// (ConsensusOff) — a 0 therefore cannot distinguish "off" from
	// "strict with nothing to filter". Observability only — the dropped findings
	// live in the sidecar.
	ConsensusFiltered int `json:"consensus_filtered"`
	// UnresolvedFiltered is the number of findings the epic-35.16.6.5 Tier 4
	// content check routed OUT of the primary stream into the unresolved sidecar:
	// their cited file does not exist, no filename-level tier (5.4 Tiers 1-3)
	// produced a candidate, and the constructs their prose names are declared
	// nowhere in the tracked tree.
	//
	// Like ConsensusFiltered this is observability only — the routed findings are
	// preserved on disk, never deleted — and, like SkippedSources, it is NOT
	// produced by the library pipeline: content resolution needs a checked-out
	// tree and a parser, so the ATCR I/O layer computes it and stamps it here
	// after Reconcile returns. It is therefore always 0 for a pure in-memory
	// embedder.
	// A caller comparing counters across a routed run should note that
	// TotalFindings and OutOfScope describe the POST-routing set (what
	// findings.json holds) while the merge diagnostics above —
	// ClustersCollapsed, SeverityDisagreements, NoiseCount, PerSourceCounts,
	// AmbiguousCount — describe the pre-routing reconcile pass, which is a
	// record of work performed rather than a property of the surviving set.
	UnresolvedFiltered int `json:"unresolved_filtered"`
	// UnresolvedState records what the Tier 4 content check actually DID this
	// run, which UnresolvedFiltered alone cannot convey. A count of 0 is produced
	// by at least six conditions and five of them mean Tier 4 never adjudicated
	// anything: no tracked file index, the AST opt-out set, an eligible file set
	// over the index cap, a tracked tree with no readable file, and an incomplete
	// index that withheld every no-match verdict. Only the sixth — the check ran
	// and routed nothing — is a healthy run, and without this field a report
	// cannot tell it from a silently-disabled one.
	//
	// It is the exact counterpart of ConsensusLevel beside ConsensusFiltered, and
	// is stamped and rendered on the same terms: unconditionally, so the record of
	// which configuration produced these artifacts survives in report.md.
	//
	// Always one of UnresolvedStateApplied / UnresolvedStateDisabled /
	// UnresolvedStateUnavailable / UnresolvedStateIncomplete, or EMPTY. Empty
	// means "not recorded": like UnresolvedFiltered this is stamped by the ATCR
	// I/O layer after Reconcile returns (content resolution needs a checked-out
	// tree and a parser), so a pure in-memory embedder always sees empty.
	UnresolvedState string `json:"unresolved_state,omitempty"`
	// ConsensusLevel records the level the filter actually ran at, which
	// ConsensusFiltered alone cannot convey (0 is ambiguous between "off" and
	// "strict with nothing to filter"). Always one of the canonical levels: an
	// unset or unrecognized Options.Consensus is recorded as the strict the
	// filter failed safe to, never echoed back raw.
	ConsensusLevel string `json:"consensus_level"`
	// TrustPriorsResolved is the number of reviewers whose trust prior the caller
	// resolved and attached via Options.TrustPriors — len(opts.TrustPriors), not a
	// count of findings the priors affected. It exists because that map is read
	// through a WINDOWED store read (scorecard.ResolveTrustPriors, epic 35.11):
	// a reviewer with no runs in the window silently drops out of the map and
	// loses trust exemption/demotion, and nothing else surfaces that. The
	// scorecard read discards its own diagnostics (io.Discard) and
	// `atcr personas list --scores` still reads all history, so it reports that
	// reviewer as healthy — this count is the only place the divergence is
	// observable without an all-history read. A drop between runs is the signal;
	// an absolute value is not meaningful on its own. KNOWN LIMITATION: it is a
	// cardinality, not a roster, so one reviewer aging out while another crosses
	// the min-runs floor in the same interval leaves the count flat and the
	// churn invisible. That is the accepted cost of a signal that needs no
	// second read; a per-reviewer diff would need the all-history set this
	// exists to avoid. Observability only: it
	// changes no finding, no confidence, and no exit code. 0 means the caller
	// attached no priors (a fresh install, an unresolvable config dir, or an
	// unreadable store — all of which degrade to a nil map by design).
	TrustPriorsResolved int `json:"trust_priors_resolved"`
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
	clusters := ClusterWith(collapseIdentical(AllFindings(sources)), opts.Grouper)

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

	// Trust-prior demotion (epic 35.9): before the consensus filter runs, demote
	// a ConfMedium singleton to ConfLow when its sole reviewer's historical
	// corroboration rate is at or below trustLowThreshold — making ConfLow
	// reachable at reconcile time for the first time. Runs after the authority
	// pass above so a PageRank-promoted ConfHigh finding is never touched (see
	// demoteByTrust's guard), and a nil/empty opts.TrustPriors is a no-op. Gated
	// on the same panel-size floor as the consensus filter below: the epic's own
	// rationale frames demotion's effect as tied to the (>= consensusMinReviewers)
	// consensus-filter regime, so a 1-2-reviewer panel (the documented
	// single-API-key host + 1 pool persona workflow, the common case) must not
	// have its findings.json Confidence silently downgraded by reviewer history
	// the consensus filter itself would never have engaged for.
	//
	// Demotion is deliberately INDEPENDENT of opts.Consensus (epic 35.9.1): only
	// the panel floor gates it, so a low-trust singleton is ConfLow at every
	// level. What the level changes is whether that ConfLow finding is then
	// sidecarred — under ConsensusOff the filter is inert and the demoted finding
	// reaches findings.json still carrying Confidence == LOW, which is the only
	// configuration in which the demotion is observable end-to-end (epic 35.9
	// AC2).
	// The panel size gates both this demotion pass and the consensus filter
	// below; compute it once — sources are not mutated in between.
	panel := panelReviewers(sources)
	if panel >= consensusMinReviewers {
		for i := range merged {
			merged[i] = demoteByTrust(merged[i], opts.TrustPriors)
		}
	}

	// NoiseCount reflects DBSCAN-isolated singletons only, so capture it before the
	// consensus filter appends its own single-finding clusters to the sidecar below.
	noiseCount := 0
	for _, c := range ambiguous {
		if len(c.Findings) == 1 {
			noiseCount++
		}
	}

	// Consensus filter (epic 14.2): once the panel is large enough that a real issue
	// is likely to be caught by more than one reviewer (>= consensusMinReviewers
	// DISTINCT reviewers — see panelReviewers, NOT len(sources), because discovery
	// flattens every pool persona into one "pool" source), an uncorroborated singleton
	// is more plausibly a hallucination than a rare true positive, so route it to the
	// ambiguous sidecar instead of promoting it to findings.json — UNLESS a false
	// negative would be too costly (consensusExempt). This runs after DBSCAN clustering
	// (first pass) and the merge/authority passes, so consensusSingletonAt sees each
	// finding's final confidence (authority-promoted singletons are HIGH and never
	// dropped). The reviewer-count gate preserves the documented single-API-key
	// workflow (host + 1 pool persona = 2 reviewers), where nearly every finding is a
	// singleton. Filtered findings stay sorted-order-stable (kept preserves order) and
	// recoverable from the sidecar for adjudication.
	//
	// Epic 35.9.1 makes only the corroboration bar configurable (opts.Consensus):
	// consensusFloor maps the level to the confidence floor and reports whether the
	// filter runs at all (off -> inert, ConsensusFiltered stays 0). Everything else is
	// deliberately level-independent — the consensusMinReviewers gate above, and BOTH
	// exemption terms below. Keeping !trustExempt(...) in the predicate at every level
	// is what preserves epic 35.9's high-trust-singleton escape hatch under strict;
	// dropping it while restructuring this block is the regression this epic guards
	// against explicitly (AC5).
	consensusFiltered := 0
	consensusLevel := effectiveConsensus(opts.Consensus)
	floor, filterEnabled := consensusFloor(consensusLevel)
	if filterEnabled && panel >= consensusMinReviewers {
		kept := merged[:0]
		for _, m := range merged {
			if consensusSingletonAt(m, floor) && !consensusExempt(m.Finding) && !trustExempt(m.Finding, opts.TrustPriors) {
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
			ConsensusLevel:        consensusLevel,
			TrustPriorsResolved:   len(opts.TrustPriors),
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

// collapseIdentical drops byte-identical duplicate findings — every field equal,
// reviewer included — keeping the first occurrence, as a pre-pass before
// clustering. A single source can emit the exact same row twice (e.g. a model that
// quoted the same example line twice), and such a perfect duplicate is noise, not
// corroboration: left in, it survives as a separate row in findings.txt,
// findings.json, and report.md. This runs on the flattened findings before
// ClusterWith so the one insertion point feeds all three output artifacts.
//
// It is deliberately conservative — only findings identical in ALL fields are
// collapsed. A same-location finding that differs in text, or one from a different
// reviewer, is legitimate independent signal and is left untouched (that merge is
// dedupeCluster's job, which relies on the distinct rows surviving to here).
func collapseIdentical(findings []Finding) []Finding {
	if len(findings) < 2 {
		return findings
	}
	seen := make(map[string]struct{}, len(findings))
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		key, err := json.Marshal(f)
		if err != nil {
			// A finding that will not marshal cannot be keyed; keep it rather than
			// risk dropping a distinct finding on a serialization edge case.
			out = append(out, f)
			continue
		}
		if _, dup := seen[string(key)]; dup {
			continue
		}
		seen[string(key)] = struct{}{}
		out = append(out, f)
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
