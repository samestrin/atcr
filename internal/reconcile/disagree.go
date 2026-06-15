package reconcile

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
)

// DisagreementsSchemaVersion is the version of the reconciled/disagreements.json
// contract consumed by Epic 6.0 (Cross-Examination). Bump it on any
// backward-incompatible change to DisagreementsFile / DisagreementItem.
const DisagreementsSchemaVersion = "1.0"

// IndependenceModelReviewerCount documents the v1 independence proxy recorded in
// the handoff file. The independence factor in the score is the count of distinct
// reviewers on a finding; the data model carries no model-strength or
// near-duplicate-model signal (clarification 2026-06-14), so a richer metric is
// out of scope for this epic. Epic 6.0 reads this field to know which proxy
// produced the scores.
const IndependenceModelReviewerCount = "distinct-reviewer-count"

// Disagreement kinds — the tension classes the radar surfaces.
const (
	KindSeveritySplit            = "severity_split"
	KindSoloFinding              = "solo_finding"
	KindGrayZone                 = "gray_zone"
	KindVerificationDisagreement = "verification_disagreement"
)

// Position is one reviewer's stance on a contested location: the severity they
// assigned and (for gray-zone pairs) the problem text they wrote. It powers the
// "model positions side by side" view. For a merged severity split the per-
// reviewer severities are not recoverable (the merge collapsed them into a single
// record), so Positions is populated only for gray-zone clusters, whose member
// findings remain unmerged.
type Position struct {
	Reviewer string `json:"reviewer"`
	Severity string `json:"severity,omitempty"`
	Problem  string `json:"problem,omitempty"`
}

// DisagreementItem is one ranked tension spot in the radar and in the Epic 6.0
// handoff queue.
type DisagreementItem struct {
	Kind         string     `json:"kind"`
	File         string     `json:"file"`
	Line         int        `json:"line"`
	Severity     string     `json:"severity"`
	Problem      string     `json:"problem"`
	Score        float64    `json:"score"`
	Spread       int        `json:"spread"`
	Independence int        `json:"independence"`
	Reviewers    []string   `json:"reviewers,omitempty"`
	Disagreement string     `json:"disagreement,omitempty"`
	Skeptics     string     `json:"skeptics,omitempty"`
	Detail       string     `json:"detail,omitempty"`
	Positions    []Position `json:"positions,omitempty"`
}

// DisagreementsFile is the reconciled/disagreements.json document — the stable
// Epic 6.0 cross-exam handoff queue. Items are ranked highest-tension first.
//
// The file is written at reconcile time, so it carries the reconcile-time tension
// classes (severity splits, solo findings, gray-zone clusters). The
// verification_disagreement class exists only after the verify stage runs and is
// surfaced by the live radar (atcr report), not by this snapshot file. See
// docs/disagreement-radar.md (Snapshot semantics).
type DisagreementsFile struct {
	SchemaVersion     string             `json:"schemaVersion"`
	IndependenceModel string             `json:"independenceModel"`
	Items             []DisagreementItem `json:"items"`
}

// BuildDisagreements projects reconciled findings and gray-zone clusters into a
// ranked radar / handoff document. It is pure and deterministic: the same inputs
// always yield byte-identical output.
//
// Tension classes, by per-finding precedence (a single finding yields at most one
// item; gray-zone cluster members are excluded from the per-finding tiers so a
// pair is never double-surfaced):
//
//   - verification_disagreement: a skeptic-vote tie (verdict unverifiable with
//     2+ distinct skeptics). Present only when findings carry verification blocks
//     (Epic 3.0); absent otherwise.
//   - severity_split: reviewers assigned different severities (Disagreement set).
//   - solo_finding: a single reviewer raised it (MEDIUM confidence).
//   - gray_zone: an ambiguous.json pair (cluster-level item).
//
// Refuted findings are never surfaced (a skeptic rejected them — not a tension to
// action). Scoring: when a severity spread exists, score = spread × independence
// (distinct-reviewer count); otherwise score = the finding's severity rank, so a
// CRITICAL solo outranks a LOW-vs-MEDIUM split (clarification 2026-06-14).
func BuildDisagreements(findings []JSONFinding, clusters []AmbiguousCluster) DisagreementsFile {
	items := make([]DisagreementItem, 0, len(findings)+len(clusters))

	// Locations covered by a gray-zone cluster: their member findings surface as
	// the cluster item, never again as solo/split items. Keyed on file+line only
	// (not problem text): a gray-zone member may also be merged with a third
	// finding, replacing its problem text via longestField — the cluster's raw
	// member problem no longer matches the JSONFinding.Problem, but the location
	// identity is stable.
	grayKeys := map[string]bool{}
	for _, c := range clusters {
		for _, f := range c.Findings {
			grayKeys[locationKey(f.File, f.Line)] = true
		}
	}

	for _, f := range findings {
		if isRefutedJSON(f) {
			continue
		}
		// Out-of-scope findings are pre-existing issues outside the reviewed
		// change; they are annotated in their own report section and excluded from
		// the gate, so they are not change-tension and never enter the radar.
		if categoryIsOutOfScope(f.Category) {
			continue
		}
		if grayKeys[locationKey(f.File, f.Line)] {
			continue
		}
		switch {
		case isVerificationTie(f.Verification):
			items = append(items, verificationItem(f))
		case f.Disagreement != "":
			items = append(items, severitySplitItem(f))
		case len(f.Reviewers) == 0:
			// Malformed finding (no reviewer) — not a solo; skip.
			continue
		case len(f.Reviewers) == 1:
			items = append(items, soloItem(f))
		}
	}
	for _, c := range clusters {
		// Skip empty clusters (no members to surface) and groups where every
		// member is out-of-scope (fail-closed, matching modalCategory): an
		// in-scope member keeps the pair as real change-tension.
		if len(c.Findings) == 0 || allOutOfScope(c.Findings) {
			continue
		}
		items = append(items, grayZoneItem(c))
	}

	sortDisagreements(items)
	return DisagreementsFile{
		SchemaVersion:     DisagreementsSchemaVersion,
		IndependenceModel: IndependenceModelReviewerCount,
		Items:             items,
	}
}

// LoadDisagreements reads the ambiguous clusters from reviewDir and builds the
// disagreements radar file. A missing or corrupt ambiguous.json degrades to a
// findings-only radar (the read error is swallowed), matching the tolerant-read
// contract at the two call sites (cmd/atcr/report.go and internal/mcp/handlers.go).
// Both call sites previously inlined the ReadAmbiguousClusters + BuildDisagreements
// pair; this helper is the single shared entry point.
func LoadDisagreements(reviewDir string, findings []JSONFinding) DisagreementsFile {
	clusters, _ := ReadAmbiguousClusters(reviewDir)
	return BuildDisagreements(findings, clusters)
}

// categoryIsOutOfScope reports whether c matches the out-of-scope category
// using the same normalization the cluster-level allOutOfScope helper applies
// (case-insensitive, trimmed). A single helper prevents the per-finding and
// cluster-level checks from drifting apart.
func categoryIsOutOfScope(c string) bool {
	return strings.ToLower(strings.TrimSpace(c)) == CategoryOutOfScope
}

// allOutOfScope reports whether every finding in the group is tagged
// out-of-scope (an empty group is not).
func allOutOfScope(findings []stream.Finding) bool {
	if len(findings) == 0 {
		return false
	}
	for _, f := range findings {
		if !categoryIsOutOfScope(f.Category) {
			return false
		}
	}
	return true
}

// locationKey is the file+line identity used for gray-zone exclusion. It is
// intentionally coarser than findingKey: a cluster member may also be merged
// with a third finding, replacing its problem text via longestField, so the
// full findingKey would not match between the cluster's raw stream.Finding and
// the reconciled JSONFinding.
func locationKey(file string, line int) string {
	return file + "\x00" + strconv.Itoa(line)
}

// canonVerdict normalizes a verdict the same way the report layer and gate do.
func canonVerdict(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// isRefutedJSON reports whether a skeptic refuted the finding.
func isRefutedJSON(f JSONFinding) bool {
	return f.Verification != nil && canonVerdict(f.Verification.Verdict) == VerdictRefuted
}

// isVerificationTie reports whether v is an unverifiable verdict reached with
// multiple skeptics — the radar's verification-disagreement signal. The verify
// stage records every voter in Skeptic (comma-joined) and yields unverifiable on
// a tie; a single-skeptic unverifiable (could-not-verify) or the empty-verdict
// "no_skeptic_verdicts" case is not a disagreement.
//
// v1 heuristic limitation: the persisted verdict block does not carry per-skeptic
// verdicts, so a genuine confirmed-vs-refuted tie cannot be distinguished from a
// unanimous "unverifiable" (both collapse to verdict=unverifiable with all voters
// named). This therefore over-includes the unanimous-unverifiable case; precise
// tie detection needs per-verdict counts from the verify stage (tracked as TD).
func isVerificationTie(v *Verification) bool {
	if v == nil || canonVerdict(v.Verdict) != VerdictUnverifiable {
		return false
	}
	return len(splitNames(v.Skeptic)) >= 2
}

// splitNames splits a comma-joined name list, trimming blanks.
func splitNames(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// spreadFromDisagreement returns the tier distance encoded in a "<lo> vs <hi>"
// annotation, or 0 if it cannot be parsed. mergeSeverity guarantees lo is the
// minimum and hi the maximum, so the distance is never negative.
func spreadFromDisagreement(d string) int {
	parts := strings.SplitN(d, " vs ", 2)
	if len(parts) != 2 {
		return 0
	}
	lo := severityRank[strings.TrimSpace(parts[0])]
	hi := severityRank[strings.TrimSpace(parts[1])]
	if hi < lo {
		return 0
	}
	return hi - lo
}

// scoreFor is the single scoring rule: a severity spread (reviewers disagree on
// severity) scores spread × independence; otherwise the finding's own severity
// rank, so a CRITICAL solo (4) outranks a LOW-vs-MEDIUM split (1×independence).
func scoreFor(spread, independence, sevRank int) float64 {
	if spread > 0 {
		return float64(spread) * float64(independence)
	}
	return float64(sevRank)
}

func atLeastOne(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func severitySplitItem(f JSONFinding) DisagreementItem {
	spread := spreadFromDisagreement(f.Disagreement)
	indep := atLeastOne(len(f.Reviewers))
	return DisagreementItem{
		Kind:         KindSeveritySplit,
		File:         f.File,
		Line:         f.Line,
		Severity:     f.Severity,
		Problem:      f.Problem,
		Score:        scoreFor(spread, indep, severityRank[f.Severity]),
		Spread:       spread,
		Independence: indep,
		Reviewers:    f.Reviewers,
		Disagreement: f.Disagreement,
	}
}

func soloItem(f JSONFinding) DisagreementItem {
	indep := atLeastOne(len(f.Reviewers))
	return DisagreementItem{
		Kind:         KindSoloFinding,
		File:         f.File,
		Line:         f.Line,
		Severity:     f.Severity,
		Problem:      f.Problem,
		Score:        scoreFor(0, indep, severityRank[f.Severity]),
		Spread:       0,
		Independence: indep,
		Reviewers:    f.Reviewers,
	}
}

func verificationItem(f JSONFinding) DisagreementItem {
	// A tie finding may also be a severity split; keep the stronger spread-based
	// score when present so labeling it a verification disagreement never demotes
	// a high-spread split.
	spread := spreadFromDisagreement(f.Disagreement)
	indep := atLeastOne(len(f.Reviewers))
	var skeptic, notes string
	if f.Verification != nil {
		skeptic = f.Verification.Skeptic
		notes = f.Verification.Notes
	}
	return DisagreementItem{
		Kind:         KindVerificationDisagreement,
		File:         f.File,
		Line:         f.Line,
		Severity:     f.Severity,
		Problem:      f.Problem,
		Score:        scoreFor(spread, indep, severityRank[f.Severity]),
		Spread:       spread,
		Independence: indep,
		Reviewers:    f.Reviewers,
		Disagreement: f.Disagreement,
		Skeptics:     skeptic,
		Detail:       notes,
	}
}

func grayZoneItem(c AmbiguousCluster) DisagreementItem {
	maxRank, minRank := 0, 1<<31
	var maxSev string
	revSet := map[string]bool{}
	positions := make([]Position, 0, len(c.Findings))
	for _, f := range c.Findings {
		if r, ok := severityRank[f.Severity]; ok {
			if r > maxRank {
				maxRank, maxSev = r, f.Severity
			}
			if r < minRank {
				minRank = r
			}
		}
		if f.Reviewer != "" {
			revSet[f.Reviewer] = true
		}
		positions = append(positions, Position{Reviewer: f.Reviewer, Severity: f.Severity, Problem: f.Problem})
	}
	spread := 0
	if maxRank > 0 && minRank <= maxRank {
		spread = maxRank - minRank
	}
	if maxSev == "" && len(c.Findings) > 0 {
		maxSev = c.Findings[0].Severity
	}
	reviewers := sortedKeys(revSet)
	indep := atLeastOne(len(reviewers))
	score := scoreFor(spread, indep, severityRank[maxSev])
	// Floor: a real gray-zone cluster (2+ findings, distinct reviewers) must
	// never sort below a LOW solo (rank 1). When all members carry unknown or
	// blank severities, severityRank[maxSev] is 0 and spread is 0, so scoreFor
	// returns 0 — the cluster would sort dead last despite being real tension.
	if score == 0 && len(c.Findings) > 0 {
		score = 1
	}
	return DisagreementItem{
		Kind:         KindGrayZone,
		File:         c.File,
		Line:         c.Line,
		Severity:     maxSev,
		Problem:      longestProblem(c.Findings),
		Score:        score,
		Spread:       spread,
		Independence: indep,
		Reviewers:    reviewers,
		Detail:       "similarity " + strconv.FormatFloat(c.Similarity, 'f', 2, 64),
		Positions:    positions,
	}
}

// longestProblem returns the longest PROBLEM in the cluster (a stable, content-
// based representative of the contested location).
func longestProblem(findings []stream.Finding) string {
	best := ""
	for _, f := range findings {
		if len(f.Problem) > len(best) {
			best = f.Problem
		}
	}
	return best
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// writeRadarSection appends the "Disagreements" section to the reconciled
// report.md, above the consensus findings. It writes nothing when there are no
// items, so a review with no disagreements yields byte-identical report output.
// Free text is routed through esc/codeSpan (emit.go), the same injection defenses
// the rest of the report uses.
func writeRadarSection(b *bytes.Buffer, df DisagreementsFile) {
	if len(df.Items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))
	for i, it := range df.Items {
		fmt.Fprintf(b, "\n### %d. %s — %s (%s) · score %s\n",
			i+1, esc(it.Kind), codeSpan(it.File, it.Line), esc(it.Severity), formatScore(it.Score))
		if it.Disagreement != "" {
			fmt.Fprintf(b, "- Severity disagreement: %s\n", esc(it.Disagreement))
		}
		if it.Skeptics != "" {
			fmt.Fprintf(b, "- Skeptics split: %s\n", esc(it.Skeptics))
		}
		if len(it.Reviewers) > 0 {
			fmt.Fprintf(b, "- Reviewers: %s (independence %d)\n", esc(joinOrNone(it.Reviewers)), it.Independence)
		}
		if it.Problem != "" {
			fmt.Fprintf(b, "- Problem: %s\n", esc(it.Problem))
		}
		if it.Detail != "" {
			fmt.Fprintf(b, "- Detail: %s\n", esc(it.Detail))
		}
		if len(it.Positions) > 0 {
			b.WriteString("- Positions:\n")
			for _, p := range it.Positions {
				name := p.Reviewer
				if name == "" {
					name = "(unknown)"
				}
				fmt.Fprintf(b, "  - %s — %s: %s\n", esc(name), esc(p.Severity), esc(p.Problem))
			}
		}
	}
}

// formatScore renders the ranking score compactly: an integer-valued score drops
// the decimal (6, not 6.0); a fractional score keeps two places.
func formatScore(s float64) string {
	if s == float64(int64(s)) {
		return strconv.FormatInt(int64(s), 10)
	}
	return strconv.FormatFloat(s, 'f', 2, 64)
}

// sortDisagreements orders items highest-tension first with a total order so the
// ranking is deterministic (same input → same order): score desc, then severity
// rank desc, file asc, line asc, kind asc, problem asc.
func sortDisagreements(items []DisagreementItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if ra, rb := severityRank[a.Severity], severityRank[b.Severity]; ra != rb {
			return ra > rb
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Problem < b.Problem
	})
}
