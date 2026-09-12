package payload

import (
	"context"
	"sync/atomic"
)

// RangeBuilder computes every whole-range artifact a fan-out review needs — the
// per-mode payload entries and the grounding changed-lines map — from a single
// gitRunner, so the whole-range git processes (ref validation, --name-status,
// and the per-mode / zero-context diffs) run once for the range and are memoized
// across both the payload build and the grounding build. Construct one per review
// range via NewRangeBuilder and use it for both BuildEntries and
// BuildChangedLines; the review layer previously built each with its own
// throwaway gitRunner, re-spending validateRange + --name-status + --unified=0 in
// grounding (Epic 22.4).
//
// It is NOT safe for concurrent use: the gitRunner's range-state cache is
// single-writer, so BuildEntries and BuildChangedLines CAS-guard against
// concurrent use and panic (rather than silently corrupting the cache) if a
// caller shares one builder across goroutines. Callers use it sequentially
// (payload modes, then grounding).
type RangeBuilder struct {
	g          *gitRunner
	base, head string
	validated  bool
	// inUse is a CAS sentinel: 0 idle, 1 mid-build. It makes accidental concurrent
	// use fail loudly (panic) instead of corrupting the single-writer rangeState
	// cache. Uncontended sequential use pays one CompareAndSwap per build.
	inUse atomic.Int32
	// Memoized claim-ledger section for the range (Epic 35.16.7). Computed once
	// and reused across every mode this builder renders, which is what makes the
	// ledger byte-identical for every agent in a fan-out: buildPayloads builds
	// one payload per MODE from ONE RangeBuilder, so identical-across-modes is
	// what identical-across-agents reduces to.
	claims     string
	claimsDone bool
	// claimStatus records what the ledger read actually produced, so a run that
	// LOST its ledger stays distinguishable from a branch that asserted nothing.
	// Populated by claimLedger alongside claims, under the same memo.
	claimStatus ClaimLedgerStatus
	// Memoized pre-fetched context for the range (Epic 35.16.8), computed once and
	// reused across every mode. This is what makes the Context Definitions section
	// byte-identical for every agent in a fan-out (AC3), by the same argument the
	// claim ledger's memo carries: buildPayloads builds one payload per MODE from
	// ONE RangeBuilder, so identical-across-modes is what identical-across-agents
	// reduces to. It also bounds the cost — the `git grep` and the candidate
	// parses are paid once per range, not once per mode.
	prefetchSection string
	prefetchDone    bool
	// prefetchSpans maps a retrieved path to the head-line spans that were shown.
	// BuildChangedLines threads these into the grounding map so a finding on a
	// retrieved consumer survives the gate — scoped to these exact spans, so a
	// finding elsewhere in the same file is still dropped as ungrounded.
	prefetchSpans  map[string][]LineRange
	prefetchStatus PrefetchStatus
}

// ClaimLedgerStatus reports what the claim-ledger read produced for a range.
//
// It exists because the failure and the empty case were byte-for-byte
// indistinguishable in every persisted artifact: claimLedger degrades an
// unreadable `git log` to an EMPTY ledger with only a Warn line, and memoizes
// that failure for the whole run. Nothing downstream recorded that a ledger was
// expected and did not arrive, so a run that lost its ledger to a transient git
// failure looked exactly like a branch whose commits asserted nothing — and the
// Warn line cannot carry review_id (see claimLedger), so it is not even
// correlatable after the fact.
//
// Each field answers a question the artifacts previously could not:
//   - Present: did a ledger reach the payload at all?
//   - Claims: how many assertions were enumerated?
//   - Truncated: were claims shed at a cap, so the ledger is incomplete?
//   - Failed: did the read ERROR (as opposed to finding nothing)?
//   - Disabled: did the operator turn the feature off via max_claim_bytes: 0?
//
// Failed and Disabled are separate on purpose: "we could not read it" and "you
// told us not to" are opposite operational signals, and collapsing them into
// "absent" is the ambiguity this type exists to remove.
type ClaimLedgerStatus struct {
	Present   bool `json:"present"`
	Claims    int  `json:"claims"`
	Truncated bool `json:"truncated,omitempty"`
	Failed    bool `json:"failed,omitempty"`
	Disabled  bool `json:"disabled,omitempty"`
}

// ClaimLedgerStatus returns the range's claim-ledger outcome. Call it after a
// BuildEntries; before any build it reports the zero value (which reads as
// "absent", the honest answer when nothing has been attempted).
func (b *RangeBuilder) ClaimLedgerStatus() ClaimLedgerStatus {
	return b.claimStatus
}

// RangeOption customizes the gitRunner a RangeBuilder wraps. It exists so review
// callers can opt out of ignore filtering (--no-ignore) without threading a bool
// through every payload entry point.
type RangeOption func(*gitRunner)

// WithoutIgnoreFilter disables the repo-root .gitignore/.atcrignore payload
// filter for this builder — the --no-ignore escape hatch, for when a caller
// deliberately wants an ignored file reviewed.
func WithoutIgnoreFilter() RangeOption {
	return func(g *gitRunner) { g.noIgnore = true }
}

// WithEscalation sets the per-file escalation thresholds for this builder
// (Epic 35.1). Callers pass the registry-resolved config; omitting the option
// leaves the built-in defaults in place. Pass a zero EscalationConfig to turn
// per-file escalation and skeleton injection off entirely.
func WithEscalation(c EscalationConfig) RangeOption {
	return func(g *gitRunner) { g.escalation = c }
}

// WithMaxClaimBytes sets the ceiling on the commit-message text the claim ledger
// reads for this builder (Epic 35.16.7). Callers pass the registry-resolved
// max_claim_bytes; omitting the option leaves DefaultMaxClaimBytes in place.
//
// **0 DISABLES the ledger entirely** — no `git log` runs, no commit text reaches
// a provider, and no ledger entry is prepended. That is the operator escape
// hatch the setting exists for, and it is why 0 is not the "unlimited" sentinel
// it is on payload_byte_budget and cache_max_bytes: the ledger entry carries
// Size 0 and is exempt from every byte budget, so an unbounded ledger would be
// unbounded prompt text nothing could see or shed. A negative value is treated
// as disabled too, so a mis-resolved setting fails safe rather than unbounded.
func WithMaxClaimBytes(n int64) RangeOption {
	return func(g *gitRunner) { g.maxClaimBytes = n }
}

// WithMaxPrefetchBytes sets the ceiling on the Context Definitions section this
// builder renders (Epic 35.16.8). Callers pass the registry-resolved
// max_prefetch_bytes; omitting the option leaves DefaultMaxPrefetchBytes.
//
// **0 DISABLES pre-fetching entirely** — no `git grep` runs, no repository
// source outside the diff reaches a provider, and no context entry is injected.
// It is not the "unlimited" sentinel it is on payload_byte_budget, for the same
// reason WithMaxClaimBytes is not: the section is exempt from every byte budget,
// so an unbounded setting would be unbounded prompt text nothing could shed. A
// negative value is treated as disabled, so a mis-resolved setting fails safe.
func WithMaxPrefetchBytes(n int64) RangeOption {
	return func(g *gitRunner) { g.maxPrefetchBytes = n }
}

// NewRangeBuilder returns a RangeBuilder for repo's base..head range, sharing one
// gitRunner (seeded with the context logger) across all its builds. Options
// customize the runner (e.g. WithoutIgnoreFilter).
func NewRangeBuilder(ctx context.Context, repo, base, head string, opts ...RangeOption) *RangeBuilder {
	g := newGitRunner(ctx, repo)
	for _, o := range opts {
		o(g)
	}
	return &RangeBuilder{g: g, base: base, head: head}
}

// validate runs validateRange once; subsequent builds on the same RangeBuilder
// skip the two rev-parse processes.
func (b *RangeBuilder) validate() error {
	if b.validated {
		return nil
	}
	if err := validateRange(b.g, b.base, b.head); err != nil {
		return err
	}
	b.validated = true
	return nil
}

// Range returns the base..head pair this builder was constructed for. A caller
// that grounds against a request's range (computeGroundingData) can assert the
// builder was built from that same range — a cheap in-memory check — so a future
// edit that threads a RangeBuilder whose range differs from the request it is
// grounding fails loudly instead of silently anchoring grounding to the wrong
// range. The invariant ("build rb from the same req.Range you later ground") is
// otherwise implicit: every current caller constructs rb and grounds in the same
// function, so they agree today, but nothing enforced the pairing. Construct one
// RangeBuilder per review range and use it for both BuildEntries and
// BuildChangedLines.
func (b *RangeBuilder) Range() (base, head string) {
	return b.base, b.head
}

// AllIgnored reports whether the range had changed files but the ignore filter
// removed every one — the ignore-stage analogue of Truncation.AllDropped. count
// is the number of changed files excluded. It lets the review layer emit a
// --no-ignore hint instead of a misleading "no changed files" error when a
// lockfile-only / vendored-only range is fully filtered. The signal is populated
// by the --name-status pass, so call it after a BuildEntries or
// BuildChangedLines has run on this builder; before any build it reports false.
func (b *RangeBuilder) AllIgnored() (all bool, count int) {
	s := b.g.forRange(b.base, b.head)
	return s.allIgnored, s.allIgnoredCount
}

// BuildEntries returns the per-file payload contributions for mode, reusing the
// builder's memoized range caches. Mirrors the package-level BuildEntries.
func (b *RangeBuilder) BuildEntries(mode PayloadMode) ([]FileEntry, error) {
	if !b.inUse.CompareAndSwap(0, 1) {
		panic("payload.RangeBuilder used concurrently: it is not safe for concurrent use")
	}
	defer b.inUse.Store(0)
	if err := validatePayloadMode(mode); err != nil {
		return nil, err
	}
	if err := b.validate(); err != nil {
		return nil, err
	}
	entries, err := b.g.buildEntriesValidated(mode, b.base, b.head)
	if err != nil {
		return nil, err
	}
	return b.withPrefetchSection(b.withClaimLedger(entries)), nil
}

// PrefetchStatus returns the range's pre-fetch outcome. Call it after a
// BuildEntries; before any build it reports the zero value.
func (b *RangeBuilder) PrefetchStatus() PrefetchStatus {
	return b.prefetchStatus
}

// withPrefetchSection inserts the Context Definitions entry AFTER the claim
// ledger, never before it.
//
// The position is load-bearing, not cosmetic: "the ledger leads the payload" is
// asserted in three places (claims_ledger_test.go) and the ledger's own
// consequence list is written against it sitting above the first column-0 diff
// marker. Prepending here would silently move the ledger and break that contract.
//
// A range with no entries gets no section, for the reason withClaimLedger gives:
// an empty entry set is how the review layer detects "nothing to review", and
// injecting here would convert that into a payload carrying context and no code.
//
// Accepted consequence, inherited from the claim ledger's list (claims.go,
// consequence 5): the entry sits ABOVE the first column-0 diff marker, and
// EntriesFromRenderedPayload deliberately discards everything before that
// marker, so up to DefaultMaxPrefetchBytes of retrieved repository source —
// the content the grounding widening now trusts — is ABSENT from every
// model-invocation audit record. An auditor sees the code and not the context
// that shaped the verdict. Closing it means touching the audit seam itself
// (surface the pre-marker prefix as an unattributed entry), which is tracked
// as its own technical-debt row, not fixed here.
func (b *RangeBuilder) withPrefetchSection(entries []FileEntry) []FileEntry {
	if len(entries) == 0 {
		return entries
	}
	section := b.prefetch()
	if section == "" {
		return entries
	}
	at := 0
	if entries[0].shedExempt && entries[0].Path == ClaimLedgerPath {
		at = 1
	}
	out := make([]FileEntry, 0, len(entries)+1)
	out = append(out, entries[:at]...)
	out = append(out, newPrefetchEntry(section))
	return append(out, entries[at:]...)
}

// prefetch returns the memoized Context Definitions section for this range,
// running the lookup at most once per builder.
//
// A failed pass yields an empty section, never an error, and the failure is
// memoized like a success: every mode must carry the SAME section (AC3), and a
// per-mode retry could succeed on the second mode and hand two agents different
// context.
func (b *RangeBuilder) prefetch() string {
	if b.prefetchDone {
		return b.prefetchSection
	}
	b.prefetchDone = true
	if b.g.maxPrefetchBytes <= 0 {
		// The operator disabled the feature: return before any `git grep` runs, so
		// no repository source outside the diff is read at all.
		b.prefetchStatus = PrefetchStatus{Disabled: true}
		return ""
	}
	b.prefetchSection, b.prefetchSpans, b.prefetchStatus = b.g.buildPrefetch(b.base, b.head)
	return b.prefetchSection
}

// withClaimLedger prepends the range's claim-ledger entry to entries, so the
// author's assertions lead the payload and every reviewer adjudicates them
// against the diff that follows.
//
// A range with NO changed files gets no ledger. An empty entry set is how the
// review layer detects "nothing to review"; injecting a ledger there would
// convert that condition into a one-entry payload carrying claims and no code.
//
// The entry is deliberately shaped to be inert everywhere it is not wanted:
// Size 0 keeps it out of byte-budget accounting (so it never displaces diff
// content), and an empty Mode keeps it out of the escalated-file bookkeeping
// that reads FileEntry.Mode.
func (b *RangeBuilder) withClaimLedger(entries []FileEntry) []FileEntry {
	if len(entries) == 0 {
		return entries
	}
	section := b.claimLedger()
	if section == "" {
		return entries
	}
	out := make([]FileEntry, 0, len(entries)+1)
	out = append(out, newClaimLedgerEntry(section))
	return append(out, entries...)
}

// claimLedger returns the memoized claim-ledger section for this range, reading
// the commit messages at most once per builder.
//
// An unreadable range yields an empty ledger, never an error: the claim ledger
// is an additional input to a review, and failing a whole review because git
// could not produce a log would trade a complete review for none at all. The
// failure is logged so it is diagnosable rather than silent.
//
// A failed read is memoized like a successful one — deliberately. Every mode
// this builder renders must carry the SAME ledger (AC3), and a per-mode retry
// could succeed on the second mode and hand two agents different payloads. The
// cost is that one transient git failure disables the ledger for the whole run
// rather than just one mode; the Warn line is what makes that visible.
//
// There is deliberately NO happy-path log line here, though one would be
// useful: an empty ledger is otherwise indistinguishable in production from an
// absent one. The payload build runs BEFORE the review id is minted
// (cli/review.go builds the review, then correlates the context logger), so a
// line emitted from this stage cannot carry review_id — and the correlation
// requirement (sprint 4.0_structured_logging, AC9; user-facing contract in
// docs/logging.md, "Request correlation") is that EVERY log line emitted during
// a review carries it. Correlating the payload stage, or surfacing the ledger's
// presence some other way, is tracked as technical debt against
// rangebuilder.go:183 (the manifest-field option); until then the observability
// gap is the honest cost of not breaking that correlation rule on every debug
// run.
//
// "AC9" here is sprint 4.0's, NOT epic 35.16.7's — that epic defines AC1–AC7
// only, so an unqualified "AC9" in this file reads as a reference to something
// that does not exist.
func (b *RangeBuilder) claimLedger() string {
	if b.claimsDone {
		return b.claims
	}
	b.claimsDone = true
	// 0 (or a negative, mis-resolved value) means the operator disabled the
	// feature: return before the git process runs, so no commit text is read at
	// all — not merely trimmed to nothing. This is the ONE place the setting's
	// "0 = disabled" meaning is translated into commitMessages' own "<= 0 =
	// unlimited" parameter convention; passing the setting straight through would
	// invert it into an UNBOUNDED read, the exact opposite of what was asked for.
	if b.g.maxClaimBytes <= 0 {
		b.claimStatus = ClaimLedgerStatus{Disabled: true}
		return b.claims
	}
	msgs, truncated, err := b.g.commitMessages(b.base, b.head, b.g.maxClaimBytes, DefaultMaxClaimCommits)
	if err != nil {
		b.g.log().Warn("payload: commit messages unreadable; review proceeds without a claim ledger",
			"base", b.base, "head", b.head, "error", err)
		b.claimStatus = ClaimLedgerStatus{Failed: true}
		return b.claims
	}
	claims, fenceSuppressed := splitClaims(msgs)
	b.claims = claimLedgerSection(claims, truncated, fenceSuppressed)
	b.claimStatus = ClaimLedgerStatus{
		// Present tracks the RENDERED section, not the claim count: a zero-claim
		// ledger renders nothing at all (claimLedgerSection returns ""), so there is
		// no entry to report and "present" would be a false claim.
		Present: b.claims != "",
		Claims:  len(claims),
		// A fence that swallowed body text is claim loss exactly like a byte-cap
		// shed, and the section discloses both the same way — so the status records
		// both under one flag rather than inventing a distinction the payload text
		// does not make.
		Truncated: truncated != claimsComplete || fenceSuppressed,
	}
	return b.claims
}

// BuildChangedLines returns the grounding changed-lines map for the range,
// reusing the builder's memoized --name-status and zero-context diff. Reuse
// always elides validateRange and the --name-status process. Files mode
// consumes the zero-context diff directly, and with escalation enabled (the
// default) a diff/blocks-mode build populates the zero-context cache as a side
// effect of hunk-range measurement — so grounding after those builds elides
// the --unified=0 process entirely (pinned by
// TestRangeBuilder_BlocksModeGroundingReusesZeroContext). The residual +1
// --unified=0 subprocess returns only when escalation is disabled, pinned by
// TestRangeBuilder_BlocksModeGroundingSpawnsOneDiffWithoutEscalation.
// (validateRange + --name-status stay elided either way.) Mirrors the
// package-level BuildChangedLines; the fail-open contract (a git error
// disables the grounding gate) lives at the fan-out caller.
func (b *RangeBuilder) BuildChangedLines() (ChangedLines, error) {
	if !b.inUse.CompareAndSwap(0, 1) {
		panic("payload.RangeBuilder used concurrently: it is not safe for concurrent use")
	}
	defer b.inUse.Store(0)
	if err := b.validate(); err != nil {
		return nil, err
	}
	cl, err := b.g.changedLines(b.base, b.head)
	if err != nil {
		return nil, err
	}
	return b.withPrefetchedSpans(cl), nil
}

// withPrefetchedSpans adds each retrieved snippet's span to the grounding map.
//
// This is what makes the epic's motivating defect reportable: a bug whose
// mechanism lives in a file the diff never touched is dropped by isGrounded
// ("file not in the patch: ungrounded") no matter how good the reviewer is, so
// retrieving the file without threading it here would show the consumer and then
// discard every finding about it.
//
// The widening is deliberately NARROW. Only the spans actually rendered become
// groundable, so a finding elsewhere in the same retrieved file is still dropped
// exactly as today. A path the diff DID change is left alone: its own changed
// ranges govern, and overwriting them with a snippet span would shrink the
// groundable region of a genuinely changed file. (That case is unreachable by
// construction today — parseGrepHits drops every excluded path at
// internal/payload/prefetch.go's candidate filter, and referenceHits is only
// ever called with changedPaths — so this guard is deliberate defense-in-depth
// against a future producer that feeds spans without that exclusion.)
func (b *RangeBuilder) withPrefetchedSpans(cl ChangedLines) ChangedLines {
	// Consume the memo READ-ONLY: grounding widens the gate, so it may only
	// cover a section a build actually rendered. Invoking prefetch() here made
	// GROUNDING a trigger for the whole `git grep` + blob-read pass — on a path
	// that never shipped the section — and, past ReleaseModeCaches, re-spawned
	// one `git show` per changed file while making unshown lines groundable.
	if len(b.prefetchSpans) == 0 {
		return cl
	}
	if cl == nil {
		cl = ChangedLines{}
	}
	for p, spans := range b.prefetchSpans {
		if _, changed := cl[p]; changed {
			continue
		}
		// PrefetchOnly is what keeps the widening as narrow as it is described:
		// without it the gate's file-level arm (Line <= 0) would keep ANY finding
		// against a merely-referenced file, which is broader than "only the exact
		// retrieved spans" and would let fabricated file-level findings through.
		fc := FileChange{PrefetchOnly: true}
		fc.Ranges = append(fc.Ranges, spans...)
		cl[p] = fc
	}
	return cl
}

// ReleaseModeCaches drops the per-mode diff chunk caches (function-context,
// plain -U10, and raw) and the parsed line-range cache, retaining only the
// zero-context diff and the --name-status list that grounding needs. Call it
// once every payload mode's entries are materialized (e.g. after buildPayloads):
// the per-mode caches are dead weight once the entries are copied out, and
// releasing them lowers peak heap during the subsequent grounding build for
// large multi-mode diffs without re-spawning any git process — grounding reads
// the retained zero-context cache and the retained --name-status list. A later
// BuildEntries call re-populates the per-mode caches from the retained
// range-level state if needed, so a RangeBuilder stays reusable after release —
// with one exception: headSrc (the full HEAD blobs) is NOT rebuildable from
// retained state, so a later files-mode or escalating build after release
// re-spawns one `git show` per changed file. Only call ReleaseModeCaches once
// every mode's entries are materialized.
func (b *RangeBuilder) ReleaseModeCaches() {
	if !b.inUse.CompareAndSwap(0, 1) {
		panic("payload.RangeBuilder used concurrently: it is not safe for concurrent use")
	}
	defer b.inUse.Store(0)
	s := b.g.forRange(b.base, b.head)
	s.fc = nil
	s.plain = nil
	s.raw = nil
	s.lineRanges = nil
	// headSrc holds a full HEAD blob per analyzed file — the largest per-mode
	// cache by far. Grounding does not read it, so it is released with the rest.
	s.headSrc = nil
	// fileCtx holds the memoized per-file escalation analysis (parse/skeleton).
	// It is dead weight once every mode's entries are materialized, and like
	// headSrc it is re-derivable by a later BuildEntries, so release it too.
	s.fileCtx = nil
}

// EscalationDegraded reports whether the change set exceeded the escalation
// file cap, so the per-file escalation and skeleton passes were skipped for the
// whole run. Call it after a BuildEntries; before any build it reports false.
// The review layer records it in the manifest so a reader can tell "nothing was
// complex enough to escalate" apart from "escalation never ran".
func (b *RangeBuilder) EscalationDegraded() bool {
	return b.g.forRange(b.base, b.head).escalationDegraded
}
