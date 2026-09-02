// Package scorecard emits a normalized per-reviewer eval record alongside each
// reconcile run and accumulates those records into a local monthly JSONL store
// (~/.config/atcr/scorecard/YYYY-MM.jsonl). Each run appends one record per
// reviewer plus one aggregate record. The store is local and never committed;
// records are the data prerequisite for the public Model-Eval Leaderboard
// (Epic 10.0). Cost is computed at emit time from the per-model rate table so a
// rate correction re-prices historical records on read.
package scorecard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
)

// SchemaVersion is the scorecard record schema version. It is emitted as an
// integer on every record so Epic 10.0's public submission format can evolve
// independently; a future change increments this and old records stay readable.
const SchemaVersion = 1

// Record type discriminators (AC 01-05): one "reviewer" record per participating
// reviewer plus one "aggregate" record summarizing the whole run.
const (
	RecordTypeReviewer  = "reviewer"
	RecordTypeAggregate = "aggregate"
)

// Diagnostic message substrings emitted on the scorecard read/write paths,
// exported so the wiring tests in cmd/atcr and internal/mcp assert against the
// same literal the producers emit: a reword updates this one constant and every
// regression test follows it. Producers keep their richer surrounding format
// (path, error); these constants are the stable substrings the tests pin.
const (
	MsgMalformedSkip = "skipping malformed record"
	MsgWriteFailed   = "scorecard: write failed"
)

// defaultRole labels per-reviewer records produced from a reconcile run. Every
// reconcile finding originates from the review stage, whose agents are reviewers
// by definition (skeptics/judges run in later stages), so the role is constant
// here rather than threaded per agent.
const defaultRole = "reviewer"

// Record is one scorecard JSONL line. The first block is always present; the
// verification block (pointers + omitempty) is present only when a valid
// reconciled/verification.json drove the run (AC 01-03) — a nil pointer omits
// the key entirely, while a pointer to 0 still serializes (0 is a valid value).
type Record struct {
	SchemaVersion        int    `json:"schema_version"`
	RecordType           string `json:"record_type"`
	RunID                string `json:"run_id"`
	Reviewer             string `json:"reviewer"`
	Model                string `json:"model"`
	Role                 string `json:"role"`
	FindingsRaised       int    `json:"findings_raised"`
	FindingsCorroborated int    `json:"findings_corroborated"`
	FindingsSolo         int    `json:"findings_solo"`
	// FindingsDocShielded counts the routed findings this record's denominator
	// deliberately did NOT charge: those the Tier 4 check routed because their
	// subject was named only in a documentation-extension file (see
	// reconcile.UnresolvedReasonDocShield).
	//
	// It exists so the carve-out can never be silent. The exemption is driven by
	// the reviewer's own PROBLEM text — a reviewer who anchors a fabricated
	// finding on any identifier that appears in a tracked README or CHANGELOG
	// escapes the phantom charge — so a rate of 1.00 with a nonzero count here
	// means something very different from a rate of 1.00 without one. Recording
	// it lets a store tell those apart, and TrustPriors DOES tell them apart:
	// shielded counts join the trust rate's denominator (trustPriorsSince), so
	// the shield discounts the prior even though it escapes the scorecard
	// charge. Omitted when zero.
	FindingsDocShielded int     `json:"findings_doc_shielded,omitempty"`
	CorroborationRate   float64 `json:"corroboration_rate"`
	CostUSD             float64 `json:"cost_usd"`
	TokensIn            int     `json:"tokens_in"`
	TokensOut           int     `json:"tokens_out"`
	LatencyMS           int64   `json:"latency_ms"`

	// ConsensusLevel is the reconcile consensus level this run's counts were
	// measured under (epic 35.9.1). It matters because FindingsRaised and
	// FindingsCorroborated are computed from the POST-filter finding set, so the
	// same review yields a different CorroborationRate per level — and that rate
	// feeds TrustPriors, which drives demoteByTrust/trustExempt on later runs.
	// Recording it lets TrustPriors count only the strict runs its historical
	// semantics assume. Omitted when empty: a store written before 35.9.1 has no
	// level, and every one of those runs was strict by construction.
	ConsensusLevel string `json:"consensus_level,omitempty"`
	// RaisedIncludesUnresolved records that FindingsRaised counts the findings the
	// Epic 35.16.6.5 Tier 4 content check routed out of the primary stream. It is
	// the era discriminator for that denominator, and it exists for the same
	// reason ConsensusLevel above does: the number changed meaning, so a rate
	// summed across both meanings measures neither.
	//
	// Omitted when false, which is how a record written before the epic reads.
	// Unlike ConsensusLevel, an absent value here is NOT read as the current
	// definition — absent genuinely means the other one — so TrustPriors excludes
	// those records rather than blending them. See unresolvedEraRuns.
	//
	// It is a BOOL, and the denominator has now changed meaning twice, so it can
	// no longer carry the era on its own — see RaisedDenominator below, which is
	// what a new reader should use. This field stays because existing readers and
	// stores depend on it, and because "true" is still exactly right about the one
	// thing it claims: routed findings are in the denominator.
	RaisedIncludesUnresolved bool `json:"raised_includes_unresolved,omitempty"`
	// RaisedDenominator identifies WHICH definition of FindingsRaised this record
	// was computed under. It supersedes RaisedIncludesUnresolved, which is a bool
	// against a question that turned out to have more than two answers.
	//
	// See RaisedDenominatorCurrent for the values. An absent field means the
	// record predates this discriminator, and its era is then read from
	// RaisedIncludesUnresolved: true is denominator 2, absent is 1. That fallback
	// is what lets an existing store keep working rather than being blacked out.
	//
	// omitempty: a zero is not a version, and a record written before this field
	// existed must serialize as it always did.
	RaisedDenominator int `json:"raised_denominator,omitempty"`

	FindingsVerified    *int     `json:"findings_verified,omitempty"`
	FindingsRefuted     *int     `json:"findings_refuted,omitempty"`
	SurvivedSkepticRate *float64 `json:"survived_skeptic_rate,omitempty"`
}

// Denominator definitions for Record.FindingsRaised. Each value is a distinct
// rule for what counts, and a rate averaged across two of them measures neither
// — which is the whole reason the number is versioned rather than described.
//
//   - 1: routed findings are EXCLUDED. Everything written before Epic
//     35.16.6.5. Never stamped; it is what an absent discriminator means.
//   - 2: routed findings are INCLUDED (Epic 35.16.6.5). Stamped as
//     RaisedIncludesUnresolved=true, before RaisedDenominator existed.
//   - 3: routed findings are included EXCEPT those routed by the
//     documentation-extension heuristic (Epic 35.16.6.8). Those are counted in
//     FindingsDocShielded instead. This is the current definition.
const (
	raisedDenominatorPreEpic   = 1
	raisedDenominatorAllRouted = 2
	// RaisedDenominatorCurrent is the definition every record this package writes
	// is computed under. Bump it whenever the rule for FindingsRaised changes, and
	// the era filters separate the old records from the new ones automatically.
	//
	// RESERVED RANGE: production eras live in 1..99. RaisedDenominatorBenchmarkSuite
	// (100) shares this one un-namespaced int domain on the frozen public key, so a
	// future era bump must check the gap rather than assume it — an era that
	// reached 100 would be indistinguishable from a benchmark-suite row on the
	// board.
	RaisedDenominatorCurrent = 3
)

// RaisedDenominatorBenchmarkSuite marks a public row scored by the BENCHMARK
// SUITE rather than by a production reconcile. It is a different axis, not a
// newer era, and the numeric gap is there to make that obvious: never compare it
// ordinally with the values above.
//
// A benchmark row's numbers are not the production ones under an older rule —
// they are different quantities. benchmark.scoreOne puts CATEGORY RECALL in
// corroboration_rate and mean findings-per-case in findings_raised_avg; nothing
// there is routed, corroborated, or reconciled at all. Both producers publish
// onto the same board through the same frozen PublicRecord, so without this the
// board would be averaging recall against corroboration and calling the result
// one number.
//
// This is stamped rather than left absent because an absent value on a public row
// is the exact silence raised_denominator exists to remove. The envelope's own
// `source` field already separates the two producers; this makes each ROW
// self-describing too, which is what a board consumer reading the reviewers array
// actually has in hand.
const RaisedDenominatorBenchmarkSuite = 100

// raisedDenominatorOf reports which definition r's FindingsRaised was computed
// under, reading the modern field when present and falling back to the bool that
// preceded it. Never returns 0: a record always belongs to some era, and treating
// "unmarked" as its own class would strand every pre-existing store.
func raisedDenominatorOf(r Record) int {
	// Clamped, not trusted — but the clamp is now only a backstop for callers
	// that BYPASS the era pass: unresolvedEraRuns excludes above-current records
	// outright, so this branch fires only for a direct Aggregate/ExportSelected
	// consumer that skipped it. For those, an out-of-range value still reads as
	// the current definition rather than defining a cohort of its own.
	if r.RaisedDenominator > RaisedDenominatorCurrent {
		return RaisedDenominatorCurrent
	}
	if r.RaisedDenominator > 0 {
		return r.RaisedDenominator
	}
	if r.RaisedIncludesUnresolved {
		return raisedDenominatorAllRouted
	}
	return raisedDenominatorPreEpic
}

// Finding is the minimal per-finding input the emitter needs to compute
// per-reviewer corroboration metrics and (when verification is present) attribute
// skeptic verdicts to the reviewers that raised the finding.
type Finding struct {
	File      string
	Line      int
	Problem   string
	Reviewers []string
	// UnresolvedReason is set only on records in EmitInput.UnresolvedFindings and
	// carries the Tier 4 routing reason verbatim from
	// reconcile.JSONFinding.UnresolvedReason. Empty means the ordinary no-match:
	// the anchors appear nowhere in the tracked tree.
	UnresolvedReason string
}

// ReviewerMeta carries the per-reviewer identity/usage sourced from the fan-out's
// persisted status.json (model + token usage + latency). reconcile runs as a
// separate process from the review, so this data must come from disk, not the
// in-memory fan-out Result. Cost is NOT carried here — it is derived at emit time
// from Model + tokens via llmclient.ComputeCostUSD.
type ReviewerMeta struct {
	Model     string
	TokensIn  int
	TokensOut int
	LatencyMS int64
}

// EmitOpts controls emission side-effects. NoScorecard suppresses all I/O (the
// --no-scorecard gate; checked first, before any directory creation). Dir
// overrides the store root (tests pin a temp dir); empty means the default user
// config dir. Diag is the sink for operational diagnostics (write failures,
// verification read/parse failures, orphan verdicts); a nil Diag defaults to
// os.Stderr so existing callers keep their prior behavior (Epic 3.4). Diag must
// be safe for the caller's concurrency model; the package does not synchronize
// writes to it. SECURITY: diagnostics may embed absolute store paths (which can
// contain a username via ~/.config/atcr/...) and raw %v error strings, so the
// sink is assumed local and trusted. Before routing Diag to any non-local sink
// (a leaderboard submission or a remote-facing MCP response), scrub absolute
// paths (use base names) and avoid echoing raw error strings.
//
// NAMING: the read-path equivalent of this sink is ReadOpts.Writer (store.go).
// The divergent field names — emit-path Diag vs read-path Writer — are
// intentional and retained for caller-API stability; both denote the same
// "operational diagnostics sink, default os.Stderr" concept.
type EmitOpts struct {
	NoScorecard bool
	Dir         string
	Diag        io.Writer
}

// EmitInput bundles everything Emit needs for one run. Reviewers is keyed by
// reviewer name and defines the set of per-reviewer records (the reviewers that
// actually ran); Findings drives the corroboration counts. VerificationPath, when
// non-empty and pointing at a readable, well-formed verification.json, adds the
// conditional skeptic fields.
type EmitInput struct {
	RunID     string
	Findings  []Finding
	Reviewers map[string]ReviewerMeta
	// ConsensusLevel is the reconcile consensus level Findings were measured
	// under; it is stamped onto every emitted record (see Record.ConsensusLevel).
	// Empty means "not recorded" and is read as strict downstream, matching a
	// pre-35.9.1 store.
	ConsensusLevel string
	// UnresolvedFindings holds the findings the Epic 35.16.6.5 Tier 4 content
	// check routed OUT of the primary stream (reconcile.Result.Unresolved): their
	// cited file does not exist and the constructs their prose names are declared
	// nowhere in the tracked tree. They are counted in FindingsRaised and NEVER in
	// FindingsCorroborated — with one carve-out, below.
	//
	// THE CARVE-OUT: a record whose UnresolvedReason is
	// reconcile.UnresolvedReasonDocShield is NOT counted in FindingsRaised. Its
	// subject WAS named in the tree, in a file the doc-extension heuristic
	// classified as prose, so being routed is not by itself fabrication evidence.
	// Every other consumer of a routed record recovers from a heuristic misfire by
	// reading unresolved.json back; this store never does, so a wrong charge here
	// stands for the 180-day window. Those records are counted separately in
	// Record.FindingsDocShielded, and their reviewers are still registered — see
	// the filter in Emit.
	//
	// They must be counted, and they must be counted this way. Routing removes
	// them from Findings before this emitter runs, so leaving them out would
	// delete exactly the highest-signal fabrication evidence from the denominator
	// the corroboration rate divides by — a reviewer raising six corroborated
	// findings and four phantoms would report 1.00 instead of 0.60, cross
	// trustHighThreshold, and earn the consensus-filter exemption its phantoms
	// argue against. And they are never corroborated even when two reviewers
	// agreed on one: agreement on a construct that exists nowhere in the tree is
	// not corroboration, and treating it as such would restore the same inflation
	// through a narrower door.
	UnresolvedFindings []Finding
	VerificationPath   string
}

// Emit computes per-reviewer metrics, builds one record per reviewer plus one
// aggregate record, and appends them to the monthly JSONL store. It is
// best-effort: a write failure for one record is logged and the run continues, so
// scorecard emission never fails the caller's reconcile. The NoScorecard gate is
// the first check — when set, Emit returns immediately with zero I/O (no directory
// creation, no file open).
func Emit(in EmitInput, opts EmitOpts) error {
	// Suppression gate — intentionally the FIRST statement, before resolveDir or
	// any file I/O, so --no-scorecard creates no directory and opens no file.
	if opts.NoScorecard {
		return nil
	}
	w := diagWriter(opts.Diag)

	dir, err := resolveDir(opts.Dir)
	if err != nil {
		_, _ = fmt.Fprintf(w, MsgWriteFailed+": %v\n", err)
		return err
	}

	verified, refuted, hasVerification := verdictTallies(in, w)

	// Deterministic reviewer order so the JSONL line order is stable.
	names := make([]string, 0, len(in.Reviewers))
	for name := range in.Reviewers {
		names = append(names, name)
	}
	sort.Strings(names)

	records := make([]Record, 0, len(names)+1)
	var agg Record
	agg.SchemaVersion = SchemaVersion
	agg.RecordType = RecordTypeAggregate
	agg.RunID = in.RunID
	agg.ConsensusLevel = in.ConsensusLevel
	// Stamped for the same reason, and by the same rule, as the per-reviewer
	// records below: the flag describes the DENOMINATOR on the record carrying
	// it, and agg.FindingsRaised is the sum of per-reviewer denominators, every
	// one of which was computed under the current definition. Omitting it
	// labelled every aggregate line pre-epic while it carried post-epic numbers.
	// Latent today — ApplyFilters and Aggregate both drop RecordTypeAggregate —
	// but the JSONL is read by things this package does not control.
	agg.RaisedIncludesUnresolved = true
	agg.RaisedDenominator = RaisedDenominatorCurrent
	var aggVerified, aggRefuted int

	// A routed finding is charged to its reviewer's denominator because being
	// routed IS the fabrication evidence. That holds only where the routing
	// itself is evidence. A doc-shield routing is not: it says the subject was
	// named in the tree, in a file classified as prose by its EXTENSION — a
	// heuristic, and the one this sprint had to correct for .mdx. Every other
	// consumer recovers from a misfire by reading unresolved.json back; the
	// scorecard never reads it back, so a wrong charge here is permanent and
	// moves trustExempt/demoteByTrust on unrelated runs for 180 days.
	//
	// Excluded from the COUNT, not from the input: EmitForReconcile registers
	// every routed record's reviewers into in.Reviewers before calling here (see
	// reconcile.go), so a reviewer whose every finding was doc-shield-routed still
	// gets a record rather than vanishing. Emit itself iterates in.Reviewers only,
	// so a direct caller that supplies UnresolvedFindings without populating
	// Reviewers gets no record for them — that is the caller's contract, not a
	// guarantee this filter makes.
	chargeableUnresolved := make([]Finding, 0, len(in.UnresolvedFindings))
	docShielded := make([]Finding, 0)
	for _, u := range in.UnresolvedFindings {
		if u.UnresolvedReason == reconcile.UnresolvedReasonDocShield {
			docShielded = append(docShielded, u)
			continue
		}
		chargeableUnresolved = append(chargeableUnresolved, u)
	}

	for _, name := range names {
		meta := in.Reviewers[name]
		raised, corroborated := reviewerCounts(name, in.Findings)
		// Routed phantoms add to the denominator only — see UnresolvedFindings.
		routedRaised, _ := reviewerCounts(name, chargeableUnresolved)
		raised += routedRaised
		shielded, _ := reviewerCounts(name, docShielded)
		rec := Record{
			SchemaVersion: SchemaVersion,
			// Stamped unconditionally, not only when UnresolvedFindings is
			// non-empty: the flag records which DEFINITION this record's
			// denominator was computed under, and every record this emitter writes
			// uses the current one. Stamping it only when routing happened would
			// make an ordinary clean run indistinguishable from a pre-epic record.
			RaisedIncludesUnresolved: true,
			RaisedDenominator:        RaisedDenominatorCurrent,
			RecordType:               RecordTypeReviewer,
			RunID:                    in.RunID,
			ConsensusLevel:           in.ConsensusLevel,
			Reviewer:                 name,
			Model:                    meta.Model,
			Role:                     defaultRole,
			FindingsRaised:           raised,
			FindingsCorroborated:     corroborated,
			FindingsSolo:             raised - corroborated,
			FindingsDocShielded:      shielded,
			CorroborationRate:        ratio(corroborated, raised),
			CostUSD:                  llmclient.ComputeCostUSD(meta.Model, meta.TokensIn, meta.TokensOut),
			TokensIn:                 meta.TokensIn,
			TokensOut:                meta.TokensOut,
			LatencyMS:                meta.LatencyMS,
		}
		if hasVerification {
			v, r := verified[name], refuted[name]
			rate := ratio(v, v+r)
			rec.FindingsVerified = &v
			rec.FindingsRefuted = &r
			rec.SurvivedSkepticRate = &rate
			aggVerified += v
			aggRefuted += r
		}

		agg.FindingsRaised += rec.FindingsRaised
		agg.FindingsDocShielded += rec.FindingsDocShielded
		agg.FindingsCorroborated += rec.FindingsCorroborated
		agg.FindingsSolo += rec.FindingsSolo
		agg.CostUSD += rec.CostUSD
		agg.TokensIn += rec.TokensIn
		agg.TokensOut += rec.TokensOut
		if rec.LatencyMS > agg.LatencyMS {
			agg.LatencyMS = rec.LatencyMS // run latency ~ slowest reviewer (parallel)
		}

		records = append(records, rec)
	}

	// Aggregate corroboration rate is computed from totals, not an average of
	// per-reviewer rates (AC 01-05 EC3).
	agg.CorroborationRate = ratio(agg.FindingsCorroborated, agg.FindingsRaised)
	if hasVerification {
		agg.FindingsVerified = &aggVerified
		agg.FindingsRefuted = &aggRefuted
		rate := ratio(aggVerified, aggVerified+aggRefuted)
		agg.SurvivedSkepticRate = &rate
	}
	// Aggregate is appended LAST so it is the final line of the run's batch.
	records = append(records, agg)

	var firstErr error
	for _, rec := range records {
		if err := Append(dir, rec); err != nil {
			_, _ = fmt.Fprintf(w, MsgWriteFailed+": %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// reviewerCounts returns how many findings name raised and how many of those were
// corroborated (the finding carried 2+ distinct reviewers). Solo is the
// difference, computed by the caller. The O(reviewers x findings) scan (one pass
// per reviewer, recomputing distinctCount per match) is intentional: emission is
// a once-per-reconcile, best-effort path over a handful of reviewers and a
// diff-bounded finding set, so a single-pass precompute buys no observable speed.
func reviewerCounts(name string, findings []Finding) (raised, corroborated int) {
	for _, f := range findings {
		if !contains(f.Reviewers, name) {
			continue
		}
		raised++
		if distinctCount(f.Reviewers) >= 2 {
			corroborated++
		}
	}
	return raised, corroborated
}

// verdictTallies reads VerificationPath and attributes each finding's skeptic
// verdict to the reviewers that raised that finding (matched by file+line+problem
// against in.Findings). It returns per-reviewer confirmed/refuted counts and
// whether a valid verification.json was present. An absent, unreadable, or
// malformed file degrades to no verification (fields omitted), per AC 01-03.
func verdictTallies(in EmitInput, w io.Writer) (verified, refuted map[string]int, present bool) {
	if in.VerificationPath == "" {
		return nil, nil, false
	}
	data, err := os.ReadFile(in.VerificationPath)
	if err != nil {
		if !os.IsNotExist(err) {
			_, _ = fmt.Fprintf(w, "scorecard: verification read failed: %v\n", err)
		}
		return nil, nil, false
	}
	var vf verificationFile
	if err := json.Unmarshal(data, &vf); err != nil {
		_, _ = fmt.Fprintf(w, "scorecard: verification parse failed: %v\n", err)
		return nil, nil, false
	}

	// Map finding location -> reviewers so a verdict credits the right reviewers.
	// Two findings can share one (file,line,problem) key with different reviewers;
	// union (deduped) rather than overwrite so a verdict on that location credits
	// every reviewer that raised it, not just the last one seen.
	reviewersByKey := make(map[string][]string, len(in.Findings))
	for _, f := range in.Findings {
		k := findingKey(f.File, f.Line, f.Problem)
		for _, rev := range f.Reviewers {
			if !contains(reviewersByKey[k], rev) {
				reviewersByKey[k] = append(reviewersByKey[k], rev)
			}
		}
	}

	verified = map[string]int{}
	refuted = map[string]int{}
	for _, vfind := range vf.Findings {
		revs, ok := reviewersByKey[findingKey(vfind.File, vfind.Line, vfind.Problem)]
		if !ok {
			// Orphan verdict: a verification finding with no matching raised finding.
			// The exact (file,line,problem) key is canonical across the pipeline
			// (findings.json and verification.json derive from the same reconciled
			// objects), so a miss means real under-counting — warn rather than drop it
			// silently (mirrors verify's orphan_verdict diagnostic).
			_, _ = fmt.Fprintf(w, "scorecard: verification finding %s:%d has no matching raised finding; verdict attribution skipped\n", vfind.File, vfind.Line)
			continue
		}
		switch normalizeVerdict(vfind.Verdict) {
		case verdictConfirmed:
			for _, r := range revs {
				verified[r]++
			}
		case verdictRefuted:
			for _, r := range revs {
				refuted[r]++
			}
		}
	}
	return verified, refuted, true
}

// verificationFile is the minimal subset of reconciled/verification.json the
// emitter parses: each finding's location plus its skeptic verdict. It mirrors
// internal/verify.VerificationFile but stays local so the scorecard package has
// no dependency on the verify package.
type verificationFile struct {
	Findings []struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Problem string `json:"problem"`
		Verdict string `json:"verdict"`
	} `json:"findings"`
}

// Verdict values (lower-cased) matching internal/verify's enum.
const (
	verdictConfirmed = "confirmed"
	verdictRefuted   = "refuted"
)

func normalizeVerdict(v string) string {
	out := make([]rune, 0, len(v))
	for _, r := range v {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			// drop ALL whitespace (internal included, not just surrounding), so a
			// reformatted verdict like " Con firmed " still normalizes to "confirmed"
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func findingKey(file string, line int, problem string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", file, line, problem)
}

// ratio returns num/den as a float, or 0.0 when den == 0 (never NaN/Inf).
func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// distinctCount counts distinct non-empty reviewer names in a finding's reviewer
// list (the list is deduped upstream, but the emitter does not rely on that).
func distinctCount(xs []string) int {
	seen := make(map[string]bool, len(xs))
	for _, x := range xs {
		if x != "" {
			seen[x] = true
		}
	}
	return len(seen)
}
