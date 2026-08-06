package localdebt

import (
	"strings"

	"github.com/samestrin/atcr/internal/history"
)

// SchemaVersion is the local TD record schema version. It is emitted as an integer
// on every record so the format can evolve independently; a future
// backward-incompatible change increments this and older records stay readable
// while forward-incompatible (newer) records are skipped on read.
//
// v1 -> v2 (Sprint 30.0): adds the optional Model attribution field. The bump is
// backward-compatible on read — a v1 record has no "model" key and decodes with
// Model == "" — and forward-compatible in the established sense: this reader
// accepts v1 and v2 (r.SchemaVersion <= SchemaVersion), while v3+ records stay
// forward-incompatible and are skipped with a warning (store.go decodeRecord).
//
// Downgrade visibility (accepted loss): a pre-30.0 binary (SchemaVersion == 1)
// reads v2 records as forward-incompatible and skips them, so v2 findings are
// intentionally invisible to older binaries. An old reader would structurally
// decode a v2 record and ignore the unknown model key, but the
// forward-incompat-skip model treats every newer version uniformly rather than
// special-casing additive bumps.
//
// v2 -> v3 (Plan 35.13): adds the optional Origin provenance field, the
// Occurrences / FirstSeen pair, and ModelReviewers, as .atcr/debt/ becomes the
// single system of record for every atcr debt subcommand — a manual `atcr debt
// add` will write here alongside reconcile once T2 rewires it. The bump is backward-compatible on read —
// a v1/v2 record has none of those keys and decodes with the Go zero values, which
// EffectiveOrigin reports as "review" without mutating the record — and
// forward-compatible in the same established sense: this reader accepts v1, v2 and
// v3, while v4+ records stay forward-incompatible and are skipped with a warning.
//
// Downgrade visibility (accepted loss): a pre-35.13 binary (SchemaVersion == 2)
// reads v3 records as forward-incompatible and skips them, so findings written
// after the bump are intentionally invisible to older binaries. This is the same
// accepted loss the v1 -> v2 bump documented; the uniform forward-incompat-skip
// model is preserved rather than special-casing additive bumps.
//
// Downgrade is invisibility only, never destruction. Skipping a record on read
// would otherwise not be read-only in its effects: Compact rebuilds each shard from
// what the read path returned and removes shards left with no live records, so an
// older binary running `atcr debt compact` would permanently delete every newer
// record rather than merely failing to show it. Compact therefore carries
// forward-incompatible lines through a rewrite verbatim and never removes a shard
// that still holds one (store.go). That guard is what makes this uniform
// forward-incompat-skip model safe to downgrade into, and it is a prerequisite for
// T5's automatic compaction, which removes the "a human chose to run compact" step
// that used to bound the exposure.
const SchemaVersion = 3

// Diagnostic message substrings emitted on the read path, exported so tests assert
// against the same literal the producer emits: a reword updates this one constant
// and every regression test follows it.
const (
	// MsgMalformedSkip is the substring logged when a JSONL line fails to parse.
	MsgMalformedSkip = "skipping malformed record"

	// msgOverLongLine is the full format string logged when a line exceeds
	// maxLineBytes. Both read paths (store.go's scanShard and streaming.go's
	// streamSummaryFile) emit it from this one constant, so the two cannot drift
	// apart on the one gate whose warning the tests compare only by substring.
	msgOverLongLine = "localdebt: skipping over-long line (> %d bytes) in %s\n"
)

// Terminal status values, spelled once here so no call site writes a bare
// literal — the same contract the Origin constants above establish. Every
// status predicate below routes through normalizeStatus over these, so adding
// or renaming a status fails the exhaustiveness test rather than silently
// ranking 0 (indistinguishable from open) in ClosedStatusRank.
const (
	// StatusResolved marks a finding that was fixed.
	StatusResolved = "resolved"
	// StatusDeferred marks a finding consciously put off: "not now", not "never".
	StatusDeferred = "deferred"
	// StatusWontfix marks a finding dismissed as a false positive; it is the
	// only status that survives re-detection (see IsSuppressingStatus).
	StatusWontfix = "wontfix"
)

// normalizeStatus is the ONE normalization applied before any status
// comparison — trim surrounding whitespace and lowercase — so the predicates
// below cannot drift apart on input hygiene.
func normalizeStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

// Origin values are the closed set a v3 record's origin field may carry. They are
// spelled once here so no call site writes a bare literal. An empty Origin on a
// v1/v2 record means OriginReview: reconcile was the only writer before v3 (see
// EffectiveOrigin).
const (
	// OriginReview marks a record produced by an `atcr reconcile` run
	// (cli/reconcile.go's persistence hook).
	OriginReview = "review"
	// OriginManual marks a record filed by hand through `atcr debt add`, which has
	// no reconcile run behind it and so carries a synthetic ManualRunID.
	OriginManual = "manual"
)

// Record is one persisted technical-debt finding occurrence. This struct and the
// SchemaVersion doc block above are the authoritative specification of the JSONL
// record. docs/technical-debt.md documents the .atcr/debt/ store's command surface
// as of T2 but not yet its wire schema; T7 adds that section, at which point the
// two must be kept in step. The schema is v3 as of Plan 35.13, which added the
// optional Origin, Occurrences, FirstSeen and ModelReviewers fields on top of
// v2's Model. The
// required block is always present; the optional block
// (omitempty) is present only when the reconciled finding carried the
// corresponding enrichment (Epic 18.3 justification/source_report), a resolved
// model attribution, or a recorded resolution.
type Record struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	RunID         string   `json:"run_id"`
	Timestamp     string   `json:"ts"`
	Severity      string   `json:"severity"`
	File          string   `json:"file"`
	Line          int      `json:"line"`
	Problem       string   `json:"problem"`
	Fix           string   `json:"fix"`
	Category      string   `json:"category"`
	EstMinutes    int      `json:"est_minutes"`
	Evidence      string   `json:"evidence"`
	Reviewers     []string `json:"reviewers"`
	Confidence    string   `json:"confidence"`

	// Model is the provider+model slug (e.g. "claude-sonnet-4-6") that produced
	// this finding, added in schema v2 for per-(persona, model) quality-signal
	// aggregation (Sprint 30.0). It is populated at write time by persistLocalDebt
	// from the fan-out pool summary's AgentStatus.Model. omitempty keeps it absent
	// from v1 records and from v2 records whose model attribution could not be
	// resolved; such attribution-incomplete records are excluded from per-model
	// aggregation rows rather than bucketed under an empty model. A model slug is a
	// non-PII, publicly-known catalog identifier (see internal/scorecard/telemetry.go),
	// never code/path/finding content.
	Model string `json:"model,omitempty"`

	// ModelReviewers is the subset of Reviewers whose recorded pool model IS
	// Model — added to v3 alongside Origin/Occurrences/FirstSeen (Plan 35.13,
	// still unreleased, so no bump) when Reviewers stopped being narrowed at
	// write time. The record now keeps the finding's FULL reviewer list (the
	// store is the only persistent copy — narrowing destroyed who else reported
	// the finding, and with it their resolve-time credit), while this field
	// carries the model-attributable subset AggregateQualitySignal credits under
	// Model. omitempty keeps it absent whenever Model is "" (attribution-
	// incomplete) and on records written before the field existed; readers
	// falling back to Reviewers for those get exactly the pre-field behavior,
	// because a pre-field record's Reviewers were already the narrowed,
	// attributable set.
	ModelReviewers []string `json:"model_reviewers,omitempty"`

	// Origin is the provenance of this record — OriginReview for a finding produced
	// by an `atcr reconcile` run, OriginManual for one filed by `atcr debt add` —
	// added in schema v3 (Plan 35.13) when .atcr/debt/ became the single store for
	// every debt subcommand and a manual entry stopped being distinguishable from a
	// review finding. omitempty keeps it absent whenever no origin was recorded —
	// on every v1/v2 record, and on any v3 record whose writer left it unset — and
	// an absent origin means OriginReview on ANY schema version. Reconcile was the
	// only writer before v3; since T2, `atcr debt add` stamps OriginManual
	// explicitly, while the reconcile hook still leaves the field unset and relies
	// on this default. Read it through EffectiveOrigin rather than defaulting at
	// each call site.
	Origin string `json:"origin,omitempty"`

	// Occurrences is how many times this id has been observed across the store's
	// history, added in schema v3 (Plan 35.13). FoldRecords aggregates it across an
	// id's whole record group and stamps it on the effective record, so the
	// regression signal that re-openable resolution creates survives compaction at
	// O(1) size instead of O(history). Regression count is Occurrences-1. See
	// aggregateCounters (store.go) for the counting rule; it is idempotent, so
	// re-compacting an already-compacted store never inflates or decays the value.
	//
	// A record carrying a non-zero value is a CARRIER, and CountedThrough says
	// exactly which sightings it accounts for. Every writer appends with this field
	// zero. Three sites must do so EXPLICITLY, across the two paths that copy an
	// existing record wholesale — cli/debt_resolve.go's resolution, and
	// retainForCompaction's resolution trail and model donor — since a second
	// carrier in one group would let the fold count part of the history twice.
	//
	// omitempty keeps it absent from records no fold has aggregated yet; readers
	// treat an absent/zero value as one occurrence ("not yet aggregated").
	Occurrences int `json:"occurrences,omitempty"`

	// CountedThrough is the newest sighting timestamp this record's Occurrences
	// value accounts for. It is what makes the count idempotent under repeated
	// compaction, and it must be persisted rather than inferred.
	//
	// The reason is that compaction does not reach every record. A shard holding a
	// line over maxLineBytes is left whole, and a non-month .jsonl file is read but
	// never rewritten, so sightings in them survive every compaction as zero-count
	// records. Deciding "already counted?" from a record's shape — that it still
	// carries a zero count, or that it predates the carrier's own Timestamp — is
	// wrong for exactly those survivors: the first re-counts them every fold, and
	// the second re-counts any survivor NEWER than the carrier, which happens
	// whenever a suppressing (wontfix) record wins the fold and is therefore not the
	// group's newest. Both drift upward by one per compaction, without bound.
	// CountedThrough records the boundary as a fact rather than deriving it.
	//
	// Same UTC-normalized RFC3339 invariant as Timestamp and FirstSeen, but compared
	// LEXICOGRAPHICALLY like Timestamp, NOT parsed like FirstSeen. That is
	// deliberate: this value is only ever compared against Timestamp values, so it
	// has to use Timestamp's comparison to stay consistent with them — parsing one
	// side of `r.Timestamp > boundary` and not the other would be worse than
	// parsing neither. It inherits Timestamp's exposure to an offset-bearing
	// producer, which every writer in this repo rules out by normalizing at the
	// write site.
	//
	// omitempty keeps it absent from records no fold has aggregated yet, where an
	// absent value falls back to the carrier's own Timestamp. That fallback is a
	// defensive default for a schema state no released binary ever produced — the
	// released SchemaVersion is 2, which has no occurrences field at all. It is
	// exactly right under fold rule 2, where the carrier IS the group's newest
	// record; under rule 1 a suppressing carrier can be older, so the fallback costs
	// one extra count per surviving sighting newer than that carrier — on the first
	// fold only, after which CountedThrough is persisted and the value is stable.
	CountedThrough string `json:"counted_through,omitempty"`

	// FirstSeen is the RFC3339 timestamp of the earliest record observed for this
	// id, added in schema v3 (Plan 35.13). FoldRecords carries it forward with
	// Occurrences, so the original sighting is not lost when superseded records are
	// dropped. omitempty keeps it absent from records no fold has aggregated yet,
	// where the record's own Timestamp is the earliest known sighting.
	//
	// Invariant, shared with Timestamp: UTC-normalized RFC3339
	// (time.Time.UTC().Format(time.RFC3339)). An offset-bearing value is a silent
	// ordering bug — "2026-01-01T01:00:00+05:00" is really 2025-12-31T20:00:00Z yet
	// sorts after "2026-01-01T00:00:00Z" — so normalize at the write site.
	//
	// The two fields are compared differently, deliberately. FoldRecords selects the
	// effective record by comparing Timestamp strings LEXICOGRAPHICALLY (store.go,
	// latestItem), which is sound because every producer normalizes. FirstSeen is
	// durable — carried forward across every future compaction — so a single
	// offset-bearing record would corrupt it permanently rather than for one fold;
	// earlierTimestamp (store.go) therefore PARSES both operands and falls back to a
	// byte comparison only when a value does not parse.
	//
	// Unlike Origin, this field and Occurrences have no Effective accessor yet:
	// their only consumer (aggregateCounters) works at the group level with its
	// own guards. Add one following EffectiveOrigin's pattern when a single-record
	// caller appears — do not re-implement the defaults inline (an empty
	// FirstSeen sorts below every real timestamp).
	FirstSeen string `json:"first_seen,omitempty"`

	Justification string        `json:"justification,omitempty"`
	SourceReport  *SourceReport `json:"source_report,omitempty"`
	Status        string        `json:"status,omitempty"`
	ResolvedAt    string        `json:"resolved_at,omitempty"`
}

// SourceReport is a back-reference to the review.md section a justification was
// extracted from (Epic 18.3). Path is always present when the object is; Line and
// Section are best-effort.
type SourceReport struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Section string `json:"section,omitempty"`
}

// StampID sets ID to the stable content hash of the finding's location and problem
// text, reusing history.FindingID verbatim (SHA-256 over file\x00line\x00problem,
// first 8 bytes hex-encoded). Severity is deliberately excluded so a re-settled
// severity keeps the same ID across runs. StampID is a pure setter with no error
// path: FindingID always yields a deterministic digest and never panics, even on an
// empty problem string.
func (r *Record) StampID() {
	r.ID = history.FindingID(r.File, r.Line, r.Problem)
}

// EffectiveOrigin reports the record's provenance, resolving a v1/v2 record's
// absent origin to OriginReview — reconcile was the only writer before schema v3.
// The value is trimmed and lowercased, and anything outside the closed set — a
// whitespace-only origin, or an UNRECOGNIZED one — is treated as absent rather
// than as a distinct value: decodeRecord validates schema_version, run_id and id
// only, so a hand-edited or corrupted "origin":"bogus" would otherwise propagate
// to every caller that switches on the two constants.
//
// The default is applied here, at the call site, and deliberately NOT on the read
// path: Compact re-marshals exactly what ReadAll returned, so backfilling Origin
// during decode would rewrite a v1/v2 record with an origin key while its
// schema_version still reads 1 or 2 — an on-disk record matching no schema
// version. EffectiveOrigin takes a value receiver and never mutates r.
func (r Record) EffectiveOrigin() string {
	return effectiveOrigin(r.Origin)
}

// effectiveOrigin is the single normalization behind Record.EffectiveOrigin and
// Summary.EffectiveOrigin, so the full-record and summary projections can never
// disagree on a record's provenance.
func effectiveOrigin(origin string) string {
	switch o := strings.ToLower(strings.TrimSpace(origin)); o {
	case OriginReview, OriginManual:
		return o
	default:
		return OriginReview
	}
}

// IsClosedStatus reports whether a record CARRIES a terminal status marker:
// resolved, deferred, or wontfix. It classifies the record, not the id — a
// terminal record does not mean the finding is closed forever, because a later
// re-detection can supersede it (see IsSuppressingStatus and FoldRecords). Ask
// this question of the record you are looking at; ask whether an ITEM is closed
// of the record FoldRecords selected for its id.
func IsClosedStatus(status string) bool {
	switch normalizeStatus(status) {
	case StatusResolved, StatusDeferred, StatusWontfix:
		return true
	default:
		return false
	}
}

// IsSuppressingStatus reports whether a terminal status SURVIVES a later
// re-detection of the same id. Only wontfix does.
//
// It is deliberately separate from IsClosedStatus because the two answer
// different questions, and conflating them is the defect this predicate exists
// to fix. Closed classifies a record as terminal — the fold, the quality signal,
// and the resolve guard all legitimately need that. Suppressing decides whether
// that terminal state outlives a re-detection, and the answer differs by status
// because the identity function decides it:
//
//	FindingID = SHA-256(file \x00 line \x00 problem)[:8] — LINE is part of the id.
//
// A genuine fix shifts surrounding lines, so the id changes and the finding
// returns as a fresh item regardless of any suppression rule. The only case a
// terminal `resolved` actually suppresses is same file, same line, same problem
// text — which after a real fix means a REGRESSION, or a fix that never landed.
// That is precisely the case most worth surfacing, so `resolved` must not
// suppress. `wontfix` is the mirror image: a false positive is stable at its
// location with stable text, so its id is stable and permanent suppression is
// the entire point of the flag (Epic 24.0). `deferred` means "not now", which is
// not "never".
func IsSuppressingStatus(status string) bool {
	return normalizeStatus(status) == StatusWontfix
}

// IsSettledStatus reports whether an item needs no further action: it was fixed
// (resolved) or dismissed (wontfix). It is the third of the three status
// predicates, and the distinction from IsClosedStatus is `deferred`.
//
// `deferred` carries a terminal status marker, so IsClosedStatus is true for it —
// but "not now" is not "done". A deferred item is still live debt: it counts in
// the backlog, it appears in the rendered views, and crucially it must remain
// CLOSEABLE, because deciding to fix or dismiss it later is the whole point of
// deferring. Gating closability on IsClosedStatus instead would make a deferred
// item permanently unactionable — refused by `debt resolve` as already closed,
// while every other view still showed it as outstanding work.
//
// Use IsSettledStatus for "is this item done?", IsClosedStatus for "does this
// record carry a terminal marker?", and IsSuppressingStatus for "does this
// terminal state survive re-detection?".
func IsSettledStatus(status string) bool {
	switch normalizeStatus(status) {
	case StatusResolved, StatusWontfix:
		return true
	default:
		return false
	}
}

// ClosedStatusRank orders terminal statuses so a deterministic effective status can
// be chosen when divergent terminal records exist for one id.
func ClosedStatusRank(status string) int {
	switch normalizeStatus(status) {
	case StatusWontfix:
		return 3
	case StatusResolved:
		return 2
	case StatusDeferred:
		return 1
	default:
		return 0
	}
}

// HigherClosedStatus returns whichever of the two terminal statuses ranks higher.
func HigherClosedStatus(current, candidate string) string {
	if ClosedStatusRank(candidate) > ClosedStatusRank(current) {
		return candidate
	}
	return current
}
