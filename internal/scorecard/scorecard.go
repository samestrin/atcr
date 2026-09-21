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
	"strings"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
)

// SchemaVersion is the scorecard record schema version. It is emitted as an
// integer on every record so Epic 10.0's public submission format can evolve
// independently; a future change increments this and old records stay readable.
//
// Version 2 (sprint 36.0) is the store's FIRST-EVER bump. It DECLARES three
// additive fields together, in one increment rather than three:
//   - Record.Outcome — why this reviewer's counts look the way they do.
//     Written today.
//   - Record.CategoriesRaised — the distinct categories its findings raised.
//     Written today.
//   - Finding.Category — the per-finding value that fold reads. Threaded today.
//
// All three were declared by this single increment even though the latter two
// only started being WRITTEN a phase later: the schema changes ONCE for this
// body of work, so the follow-on phase added behaviour rather than another era.
//
// THAT LEAVES A V2 SUB-ERA, and it is worth knowing about: v2 records written
// before the threading landed omit both fields. Nothing discriminates them from
// a v2 record that measured an empty set, because omitempty makes the two
// byte-identical. They are not mis-scored — the opportunity link passes through
// any run whose union holds no discriminating category, which is exactly what
// such a record contributes — but the protection comes from that pass-through,
// not from an era marker. Do not add one retroactively; there is nothing in the
// bytes to key it on.
//
// All three are omitempty with era-safe absent meaning, so no migration shim is
// needed. That is a DECISION, not an omission: the read gate at store.go
// reserves this spot for "an explicit migration shim... when one appears", and
// this is the first bump to reach it. Both directions were checked, and only one
// of them is safe by construction:
//
//   - BACKWARD (old records, new binary) — safe. The gate skips records whose
//     version is STRICTLY GREATER than this constant, with no lower bound, so
//     every v1 record still decodes. Its absent fields read as "not measured"
//     and are excluded from trust scoring rather than inferred.
//   - FORWARD (new records, old binary) — a silent population change. Any atcr
//     build still compiled at SchemaVersion = 1 skips EVERY v2 record and
//     computes TrustPriors from the pre-bump subset alone, warning on stderr but
//     returning a normal map. A stale install, or a CI image lagging a local
//     build, therefore produces quietly different trust priors rather than an
//     error. No code change is available for that; it is recorded here so it is
//     not rediscovered as a surprise.
//
// Rollout note: excluding unclassified (v1) records from trust scoring is what
// trust.go's own era comments call blacking out an existing history. A fresh
// install has nothing to strand — the store is created on first reconcile — but
// an EXISTING store does, for roughly DefaultTrustMinRuns strict runs per lens.
//
// Absent from the priors map is not punitive (it is never read as a zero rate:
// reconcile/consensus.go's trustExempt and demoteByTrust both gate on the
// comma-ok), but it is NOT "neutral" either, and the difference is the thing to
// understand. Absence disables BOTH directions: a high-trust lens stops being
// exempted from the consensus filter, and a low-trust phantom-raiser stops being
// demoted to LOW. The second is a LOOSENING, visible in findings.json
// confidence. unresolvedEraRuns says the same thing about its own era gap.
//
// (Do not call this the 1/N baseline. 1/N is the per-run PageRank uniform
// authority in reconcile/pagerank.go — a different mechanism. Trust priors have
// no baseline value at all; they have presence or absence.)
const SchemaVersion = 2

// categoriesRaisedSinceSchema is the schema version that INTRODUCED
// Record.CategoriesRaised. The opportunity-set link gates on this, never on
// SchemaVersion itself.
//
// The difference is the whole point and it is a landmine, not a style choice.
// SchemaVersion MOVES — and the fact that it has NOT moved for Phase 4a is a
// decision, not an accident. TD-030 originally put a v3 bump on that phase's
// table; C15 closed it the other way, with an additive omitempty field plus its
// own era marker (Record.PairEra), exactly as D8 closes WeightedCredit. The
// "schema changes ONCE for this body of work" sentence above therefore still
// holds. A future field may yet move the constant, which is the point below. A
// guard written as `r.SchemaVersion < SchemaVersion` reads "pre-schema-2" only
// while the constant happens to be 2; the day it becomes 3, every v2 record —
// each carrying a genuinely MEASURED category set — is silently reclassified as
// unmeasured, the runs lose their category evidence, and opportunity scoping
// switches itself off for the entire back-catalogue. No test would catch it,
// because a test that builds its fixture with `SchemaVersion: SchemaVersion`
// moves with the constant.
//
// So: one named constant per field era, pinned by a test that hardcodes the
// literal 2. A later field gets its own constant; it does not reuse this one.
const categoriesRaisedSinceSchema = 2

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

	// Outcome records WHY this reviewer's counts look the way they do: it is the
	// nine-value vocabulary defined in internal/benchmark/outcome.go, carrying
	// the distinction that file's header exists for — a reviewer that read the
	// diff and correctly found nothing, one that emitted prose no parser could
	// use, and one whose call failed outright all report zero findings and
	// otherwise score identically.
	//
	// It is what lets trustPriorsSince score only the runs a lens got a fair
	// attempt at (see eligibleOutcomeRuns), so an infrastructure fault —
	// a LiteLLM timeout, a silently capped prompt, a billing-cap auth failure —
	// never durably demotes a lens for its hosting rather than its judgment.
	//
	// The VALUE is a plain string, not a benchmark.Outcome* constant, and that
	// is forced rather than chosen: internal/benchmark imports this package, so
	// importing it back would close a cycle. internal/fanout is the safe leaf
	// that owns the classifier (fanout.ReviewerOutcome) and its validator
	// (fanout.ValidReviewerOutcome); a drift test in cli/ — a legal importer of
	// both — pins those literals to internal/benchmark's constants.
	//
	// omitempty: absent means benchmark.OutcomeUnknown (""), which is what every
	// record written before schema 2 reads as. Absent is NOT inferred as clean —
	// that would assert "reviewed successfully and found nothing" about a run
	// nobody classified — it is excluded from the trust tally instead.
	Outcome string `json:"outcome,omitempty"`
	// CategoriesRaised holds the distinct reconcile.Categories() values attached
	// to the findings this reviewer participated in. The field was declared by
	// the v2 bump so the schema changes once; it is WRITTEN on every reviewer
	// record this emitter produces.
	//
	// Three streams feed it, and the third is not obvious: the surviving
	// findings, the Tier-4-routed ones, and EmitInput.AmbiguousFindings — the
	// clusters reconcile set aside. See that field's comment for the starvation
	// loop the third one breaks. The ambiguous stream contributes categories
	// only; it touches no count.
	//
	// READ THE WORDING CAREFULLY, because the obvious reading is wrong and the
	// Phase 2 gate caught it: these are NOT per-reviewer categories. The only
	// Category reachable where the record is built is reconcile.Merged.Category,
	// which Merge sets to ModalCategory(group) — the cluster's modal value. In a
	// cluster where one lens raised `security` and two raised `performance`, the
	// merged category is `performance` and the first lens's own category is
	// unrecoverable from res.Findings. The cluster-modal meaning is therefore
	// ADOPTED EXPLICITLY, which is sound for the only consumer — opportunity-set
	// membership asks "was this topic in play on the case", which the modal value
	// answers faithfully. Nothing may read it as a per-reviewer claim. Filed as
	// TD-022.
	//
	// It is the per-record input to opportunity-set scoping: a lens
	// will be scored on a case only when some reviewer raised a category inside
	// that lens's remit, so a specialist that is correctly silent on an
	// out-of-remit diff is neither credited nor penalised.
	//
	// omitempty: absent means "not measured" (a pre-schema-2 record), which is
	// excluded rather than read as either in-remit or out-of-remit.
	CategoriesRaised []string `json:"categories_raised,omitempty"`

	// PairSignals records how this reviewer related to each co-reviewer it
	// shared a finding with on this run: agreed on the defect, or split on its
	// severity. It is the durable half of the penny test — "if the two never
	// disagree, drop one" — and it exists because nothing else on this record
	// carries co-reviewer identity at all (TD-030). See PairSignal.
	//
	// omitempty: absent means the run produced no pair. PairEra, not this
	// field, is what says the run was MEASURED.
	PairSignals []PairSignal `json:"pair_signals,omitempty"`
	// PairEra is the pair-signal measurement era marker (C15, closing TD-030
	// the way D8 closes WeightedCredit). It is stamped on every reviewer record
	// this emitter writes, including one with no pairs, because an absent
	// PairSignals is byte-identical on a measured-empty run and a pre-4a record
	// — and scoring the pre-4a back-catalogue as "these lenses never
	// co-occurred" is the drop-candidate verdict applied to the whole store.
	//
	// This is an ADDITIVE omitempty field plus an era marker, so it does NOT
	// increment SchemaVersion. See PairEraCurrent.
	PairEra int `json:"pair_era,omitempty"`

	// WeightedCredit is the ISOLATION half of the disagreement-weighted credit
	// (D8, C18): the sum, over the findings this reviewer participated in, of
	// 1/distinctCount(Reviewers). A finding raised alone contributes the full
	// isolatedFindingWeight; one raised alongside four others contributes 0.2.
	//
	// It is a NEW field rather than a redefinition of FindingsCorroborated,
	// which is a persisted int with consumers past trust priors (Aggregate, the
	// export/leaderboard path) and would truncate every partial credit to
	// nothing. FindingsRaised, FindingsCorroborated and FindingsSolo keep their
	// existing integer meanings, so no non-trust consumer changes.
	//
	// THE GROUND-TRUTH HALF IS NOT IN THIS NUMBER, and that split is C18's
	// decision rather than an omission. At emit time a finding has just been
	// raised and has no TD status yet, so "did it prove real" cannot be known
	// here; at read time the per-finding reviewer count no longer exists
	// (scorecard.Finding is never persisted). The isolation half is therefore
	// persisted here and the confirmation half applied per persona in the trust
	// fold. See C19 for what that costs.
	//
	// omitempty: absent is indistinguishable from a measured 0.0, so CreditEra —
	// not this field — is what says the record was measured.
	WeightedCredit float64 `json:"weighted_credit,omitempty"`
	// CreditEra is the weighted-credit measurement era marker (D8), stamped
	// unconditionally on every reviewer record this emitter writes for exactly
	// the reason PairEra is: a pre-weighting record and a genuinely-zero one
	// serialize identically, and averaging the pre-weighting back-catalogue in
	// as measured zeroes would drive every lens's weighted rate toward zero.
	//
	// This is an ADDITIVE omitempty field plus an era marker, so it does NOT
	// increment SchemaVersion. See CreditEraCurrent.
	CreditEra int `json:"credit_era,omitempty"`

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
	// raisedDenominatorRoutedExShield names era 3 in its own right, so code that
	// means "era 3" can say so without saying "whatever is current". The two are
	// equal today and must not be assumed to stay equal: mergeRoutedEras folds
	// era 3 into era 2 on a proof that holds for THAT PAIR ONLY, so it keys on
	// this name. Keyed on RaisedDenominatorCurrent instead, a bump to 4 would
	// silently re-point the fold at an era whose equivalence nobody proved.
	raisedDenominatorRoutedExShield = 3
	// RaisedDenominatorCurrent is the definition every record this package writes
	// is computed under. Bump it whenever the rule for FindingsRaised changes, and
	// the era filters separate the old records from the new ones automatically.
	//
	// RESERVED RANGE: production eras live in 1..99. RaisedDenominatorBenchmarkSuite
	// (100) shares this one un-namespaced int domain on the frozen public key, so a
	// future era bump must check the gap rather than assume it — an era that
	// reached 100 would be indistinguishable from a benchmark-suite row on the
	// board.
	RaisedDenominatorCurrent = raisedDenominatorRoutedExShield
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
	// Clamped, not trusted — but the clamp is now only a backstop. Both callers
	// exclude above-current records before asking (unresolvedEraRuns' two loops,
	// and reviewerAcc.add behind ExportSelected's own era pass), so no in-tree
	// path reaches this branch; it holds the contract for a future caller that
	// asks directly. For those, an out-of-range value still reads as the current
	// definition rather than defining a cohort of its own.
	// Pinned by TestRaisedDenominatorOf_ClampsAboveCurrent, which is the only
	// thing that can fail when the branch is deleted.
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
	// Category carries the finding's reconcile.Categories() value verbatim from
	// reconcile.Finding.Category, for Emit to fold into Record.CategoriesRaised —
	// this struct is never persisted, so that fold is where the value becomes
	// durable. EmitForReconcile threads it at all three construction sites
	// (res.Findings, res.Unresolved, res.Ambiguous).
	//
	// Carried verbatim, judged in Emit: reviewerCategories applies the
	// reconcile.Categories() vocabulary gate, so the construction sites cannot
	// disagree with each other about which values are durable.
	Category string
	// Severity and Disagreement carry reconcile.Finding's own values verbatim,
	// threaded at the EmitForReconcile construction sites exactly as Category
	// was. This struct is never persisted, so both are free: no schema change,
	// no store migration, no era marker (C16).
	//
	// THE PAIR SIGNAL NEEDS BOTH, and Disagreement is the load-bearing one.
	// reconcile.Merge sets Severity to the cluster MAX, so a merged finding's
	// severity cannot by itself reveal that its members disagreed; Merge
	// records that fact separately, in Disagreement ("<lo> vs <hi>"), and
	// BuildDisagreements keys KindSeveritySplit off exactly that field.
	// Carrying Severity alone would let Emit see the outcome of a split and
	// never the split.
	//
	// Same cluster-modal caveat as Category, and for the same reason: after a
	// merge the per-reviewer severities are unrecoverable (reconcile.Position
	// says so in its own comment, which is why Positions is populated only for
	// gray-zone clusters). These are the CLUSTER's values. Nothing may read
	// them as a per-reviewer claim.
	Severity     string
	Disagreement string
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
	// Outcome is this reviewer's classified run outcome, stamped onto
	// Record.Outcome by Emit. It rides ReviewerMeta because that is already the
	// carrier between EmitForReconcile (which holds the fanout.AgentStatus the
	// classification is derived from) and Emit (which builds the Record), so no
	// new parameter or parallel map is introduced.
	//
	// Three sources produce a non-empty value, and they agree by construction
	// because all three describe what the reviewer actually did:
	//   - outcomeFor, applied to an AgentStatus from this run's pool summary.
	//     This is the witnessed case and it WINS when it exists, so a reviewer
	//     recorded as failed stays failed even if res.Findings also names it.
	//   - outcomeFindings, for a reviewer with no AgentStatus that is named on
	//     a finding which survived reconcile. Being named there means it raised
	//     one, which is exactly what the shared classifier means by findings.
	//   - outcomeFindings again, for a reviewer reached only through
	//     res.Unresolved: the Tier 4 check routed its findings, which is why
	//     they are not in res.Findings. The phantom is charged through
	//     FindingsRaised, not through this field.
	//
	// The zero value survives in exactly two cases: an AgentStatus that is not
	// internally coherent (see outcomeFor), and a direct Emit caller that does
	// not populate the field.
	//
	// It is benchmark.OutcomeUnknown (""), which excludes the record from trust
	// scoring and from nothing else — the record is still written and still
	// carries its counts, its rate and its leaderboard row.
	Outcome string
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
	// AmbiguousFindings holds reconcile.Result.Ambiguous flattened to its member
	// findings. It feeds Record.CategoriesRaised and NOTHING ELSE — never
	// FindingsRaised, never FindingsCorroborated, and it never mints a reviewer
	// record for a name that has none. It is evidence about the CASE, not a count
	// against a lens.
	//
	// IT CLOSES A FEEDBACK LOOP, which is why it is worth a field. Under strict
	// consensus, reconcile routes an uncorroborated singleton into Ambiguous
	// unless trustExempt spares it — and trustExempt is OFF for exactly the lenses
	// with no prior yet. So a new or narrow lens's solo finding is filtered out of
	// res.Findings, its category never reaches CategoriesRaised, the run reads
	// out-of-remit, the opportunity filter deletes the record, its run count stays
	// under DefaultTrustMinRuns, and it never earns the prior that would have
	// spared the finding. The lens is starved by the very filter its missing prior
	// caused — against AC 1 and AC 6 both. Reading Ambiguous for categories breaks
	// the cycle without giving a filtered finding any scoring credit.
	//
	// It is also simply truer to the field's name: a consensus-filtered finding
	// WAS raised. Only its survival was denied.
	AmbiguousFindings []Finding
	VerificationPath  string
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
		// credit comes from in.Findings ONLY, and the two calls below deliberately
		// discard theirs. A routed phantom cites a file the patch does not
		// contain, so paying credit for one would pay MOST for a phantom nobody
		// else raised — isolation credit for a finding whose isolation is the
		// evidence against it. Both still charge the denominator.
		raised, corroborated, credit := reviewerCounts(name, in.Findings)
		// Routed phantoms add to the denominator only — see UnresolvedFindings.
		routedRaised, _, _ := reviewerCounts(name, chargeableUnresolved)
		raised += routedRaised
		shielded, _, _ := reviewerCounts(name, docShielded)
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
			Outcome:                  meta.Outcome,
			// in.AmbiguousFindings is a category stream ONLY — it is absent from
			// every reviewerCounts call above, so it moves no numerator and no
			// denominator. See the field's comment for the loop it breaks.
			CategoriesRaised: reviewerCategories(name, in.Findings, chargeableUnresolved, in.AmbiguousFindings),
			// Pair signals come from in.Findings ONLY. A routed phantom has no
			// co-reviewer to relate to (routing is what removed it from the
			// merged set), and the ambiguous stream is documented as
			// category-only — feeding it here would move a count it is
			// deliberately kept out of.
			PairSignals: reviewerPairSignals(name, in.Findings),
			// Stamped unconditionally, including on a run with no pairs at all:
			// the marker, not the slice, is what records that this run was
			// measured. See PairEraCurrent.
			PairEra: PairEraCurrent,
			// Stamped unconditionally alongside the value, including when the
			// value is 0.0 — see CreditEraCurrent for why the marker rather than
			// the value is what records that this run was measured.
			WeightedCredit:    credit,
			CreditEra:         CreditEraCurrent,
			CorroborationRate: ratio(corroborated, raised),
			CostUSD:           llmclient.ComputeCostUSD(meta.Model, meta.TokensIn, meta.TokensOut),
			TokensIn:          meta.TokensIn,
			TokensOut:         meta.TokensOut,
			LatencyMS:         meta.LatencyMS,
		}
		if hasVerification {
			v, r := verified[name], refuted[name]
			rec.FindingsVerified = &v
			rec.FindingsRefuted = &r
			// The RATE is gated on a countable verdict surviving, not merely on a
			// verification.json being present. With v+r == 0 — every verdict
			// truncated, or this reviewer's findings drew none — ratio(0,0) is 0,
			// and a published 0.0 is indistinguishable from a reviewer whose
			// findings were ALL refuted: the strongest negative signal the metric
			// carries, applied to a reviewer that was never measured. The counts
			// above still ship; they say "nothing was countable", which is true.
			//
			// internal/scorecard/export.go states this rule at its own gate and
			// cannot enforce it: a stored 0.0 satisfies its len(storedRates) > 0
			// branch. The gate has to live here, where the degenerate ratio is
			// produced.
			if v+r > 0 {
				rate := ratio(v, v+r)
				rec.SurvivedSkepticRate = &rate
			}
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
		// Same gate as the per-reviewer record above: an aggregate over zero
		// countable verdicts has no rate, and a 0.0 there misreports the whole run.
		if aggVerified+aggRefuted > 0 {
			rate := ratio(aggVerified, aggVerified+aggRefuted)
			agg.SurvivedSkepticRate = &rate
		}
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

// reviewerCategories returns the distinct CATEGORY values the findings name
// raised, deduped and sorted, across every stream passed in.
//
// It is the durable half of opportunity-set scoping: the per-finding Category
// lives on Finding, which is never persisted, so this fold is where the value
// becomes readable 180 days from now.
//
// Three rules, each of which has a way of going wrong quietly:
//
//   - The VOCABULARY GATE is here, not at the wire boundary. A value that is not
//     a literal reclib.Categories() member is dropped from the SET while the
//     finding itself still counts toward FindingsRaised. Dropping the finding
//     too would let a reviewer shrink its own denominator by emitting a junk
//     category. Failing the emit outright would breach EmitForReconcile's
//     contract that scorecard emission never fails the caller's reconcile. So it
//     fails NEUTRAL — recorded as absent, never trusted into the opportunity
//     gate — matching coerceOutcome's stance on the same path.
//
//   - DOC-SHIELDED findings never reach here, because the caller passes the
//     chargeable split rather than in.UnresolvedFindings. That matches the
//     carve-out Record.FindingsRaised applies, and it is what AC 03-02 Edge Case
//     2 requires: a shielded finding in the category set would put its reviewer
//     in-remit on a case that record deliberately did not charge it for.
//
//     DO NOT read that as "the two carve-outs agree everywhere" — an earlier
//     version of this comment did, and it was wrong about the only denominator
//     this field feeds. mergeRoutedEras folds FindingsDocShielded BACK into
//     FindingsRaised before the opportunity link runs, precisely so a reviewer
//     cannot launder phantoms out of its prior by anchoring them on doc-named
//     tokens. So in the trust tally the shielded finding IS charged while its
//     category is still withheld, and a lens whose only in-remit evidence on a
//     run was doc-shielded contributes nothing to that run's union — its record,
//     re-folded charge and all, is then dropped whenever another reviewer raised
//     a discriminating out-of-remit category. That is a real residual escape
//     from the anti-laundering property, filed as TD-036. The code here is
//     correct against its AC; only the old rationale was.
//
//   - The result is SORTED, not map-ordered. Two byte-identical runs must
//     serialize byte-identically, or a diff of the store reports churn that is
//     really just Go's map iteration.
//
// An empty result returns nil, so omitempty omits the key entirely and a
// measured-empty record is byte-identical to a pre-schema-2 one.
//
// BE PRECISE ABOUT WHAT RESOLVES THAT COLLISION, because the obvious answer is
// wrong. opportunitySetRuns' SchemaVersion guard separates v1 from v2 and
// nothing else — and this branch shipped SchemaVersion 2 one phase EARLY, with
// CategoriesRaised declared and never written, so there is a v2 era whose empty
// set is unmeasured and which no discriminator distinguishes. What actually
// protects those records is the same guard that protects a genuinely clean run:
// a run with no discriminating category is passed through un-scoped rather than
// judged. That is a behavioural safety net, not an era marker, and it would stop
// covering them if that pass-through were ever narrowed.
func reviewerCategories(name string, streams ...[]Finding) []string {
	seen := map[string]struct{}{}
	for _, findings := range streams {
		for _, f := range findings {
			if !contains(f.Reviewers, name) {
				continue
			}
			if !inVocabulary(f.Category) {
				continue
			}
			seen[f.Category] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// reviewerCounts returns how many findings name raised and how many of those were
// corroborated (the finding carried 2+ distinct reviewers). Solo is the
// difference, computed by the caller. The O(reviewers x findings) scan (one pass
// per reviewer, recomputing distinctCount per match) is intentional: emission is
// a once-per-reconcile, best-effort path over a handful of reviewers and a
// diff-bounded finding set, so a single-pass precompute buys no observable speed.
// credit is the THIRD return value and it is a new quantity, not a restatement
// of corroborated: the disagreement-weighted credit this reviewer earned, summed
// over the same findings. Per-finding it is 1/distinctCount(Reviewers), so a
// finding nobody else raised is worth a full point and one the whole panel
// raised is worth a fraction of one — the inverse of what `corroborated` counts,
// which is the point (see the epic's "credit disagreement, not agreement").
//
// It GROWS the signature rather than changing the meaning of the second return
// value, per D8. FindingsCorroborated is a persisted int with consumers past
// trust priors, and a fractional credit summed into it would truncate to
// nothing.
//
// ONLY the confirmed-real half is missing from this number, and deliberately
// (C18): at emit time no finding has a TD status yet. The trust fold applies
// that factor per persona at read time.
func reviewerCounts(name string, findings []Finding) (raised, corroborated int, credit float64) {
	for _, f := range findings {
		if !contains(f.Reviewers, name) {
			continue
		}
		raised++
		n := distinctCount(f.Reviewers)
		if n >= 2 {
			corroborated++
			credit += 1.0 / float64(n)
			continue
		}
		// n is 0 or 1 here. Zero is reachable — contains() matched on a name that
		// distinctCount then normalised away — and dividing by it would put an
		// +Inf into a persisted field and from there into reconcile's priors map.
		// Both cases are the same judgment anyway: nobody corroborated this
		// finding, so it earns the isolated weight.
		credit += isolatedFindingWeight
	}
	return raised, corroborated, credit
}

// CreditEraCurrent is the weighted-credit measurement era this binary writes.
//
// It exists for the same reason PairEraCurrent does and is stamped the same way:
// unconditionally, on every reviewer record this emitter writes, including one
// that earned 0.0. An absent weighted_credit key is byte-identical on a
// pre-weighting record and on a genuinely-zero one, and reading the whole
// pre-weighting back-catalogue as measured zeroes would drag every lens's
// weighted rate toward zero on upgrade — the blackout strictRuns and
// unresolvedEraRuns both refuse to cause.
//
// ABOVE-CURRENT IS EXCLUDED, not clamped, exactly as PairEra and
// RaisedDenominator handle it: a record stamped era 2 was measured under a rule
// this binary does not implement, and it still carries schema_version 2, so
// nothing but this check stops it blending with era-1 evidence.
//
// See Record.CreditEra.
const CreditEraCurrent = 1

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
		// A verdict reached from a truncated read leaves the precision ratio
		// entirely — neither numerator nor denominator. It is NOT counted against
		// the reviewer: the point is that a partial read is not evidence about the
		// reviewer in either direction, and survived_skeptic_rate is durable.
		//
		// The signal is read from verification.json, deliberately, and NOT from the
		// truncated flag on findings.json's verification block that report.md
		// renders. The two carry the same fact, but only this one survives to the
		// moment this code runs: EmitForReconcile is called after RunReconcile,
		// which rebuilds findings.json from sources/ and strips every verification
		// block in the process, while verification.json is never recomputed. Keying
		// on findings.json would read an artifact that is empty by then — a gate
		// that silently never fires. internal/debate keeps this entry honest when a
		// judge ruling clears the caveat (see syncVerificationTruncation).
		if truncatedRead(vfind.TrippedBudgets) {
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
		// TrippedBudgets names every per-finding budget that halted the skeptic
		// run. It is parsed for ONE purpose: a tool_budget_bytes trip riding a
		// confirmed or refuted verdict means the answer stands but was reached
		// from a shortened read (internal/verify's tripsVoidTheVerdict exempts a
		// window-DERIVED ceiling), and such a verdict must not move a durable
		// per-reviewer precision score. Dropping it at unmarshal is what made that
		// impossible to act on.
		TrippedBudgets []string `json:"trippedBudgets"`
	} `json:"findings"`
}

// budgetToolBytes is the tripped-budget marker internal/verify records for the
// tool-output ceiling. It is restated here rather than imported: this package
// deliberately has no dependency on verify (see verificationFile above), and the
// string is part of verification.json's on-disk shape, which is the contract
// both sides actually share.
const budgetToolBytes = "tool_budget_bytes"

// truncatedRead reports whether a verdict was reached from a shortened tool
// read. A DECLARED ceiling's trip voids the verdict to unverifiable, which the
// tally never counts, so on a confirmed/refuted record this marker can only mean
// the exempted derived-ceiling case.
func truncatedRead(trippedBudgets []string) bool {
	for _, b := range trippedBudgets {
		if b == budgetToolBytes {
			return true
		}
	}
	return false
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
		// TrimSpace, not just != "": a whitespace-only name is dropped
		// everywhere else (EmitForReconcile's trimmedReviewers, the pool loop,
		// NewCloudSyncRecord), so counting one here would let a reviewer that
		// leaves no record of its own act as a distinct corroborator.
		if name := strings.TrimSpace(x); name != "" {
			// Key on the TRIMMED name. Filtering on the trimmed value while
			// keying on the raw one would let " bruce" and "bruce" count as two
			// distinct corroborators of the same finding — a reviewer
			// corroborating itself. Unreachable through EmitForReconcile, which
			// pre-trims, but Emit is exported.
			seen[name] = true
		}
	}
	return len(seen)
}
