package payload

import (
	"context"
	"sync/atomic"

	"github.com/samestrin/atcr/internal/log"
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

// NewRangeBuilder returns a RangeBuilder for repo's base..head range, sharing one
// gitRunner (seeded with the context logger) across all its builds. Options
// customize the runner (e.g. WithoutIgnoreFilter).
func NewRangeBuilder(ctx context.Context, repo, base, head string, opts ...RangeOption) *RangeBuilder {
	g := &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)}
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
	return b.g.buildEntriesValidated(mode, b.base, b.head)
}

// BuildChangedLines returns the grounding changed-lines map for the range,
// reusing the builder's memoized --name-status and zero-context diff. Reuse
// always elides validateRange and the --name-status process; the --unified=0
// zero-context diff is elided only when a zeroCtx-consuming payload mode (files)
// already ran on this builder — the default blocks mode does not populate the
// zero-context cache, so grounding after a blocks-mode build spawns one
// --unified=0 subprocess (validateRange + --name-status stay elided). Mirrors
// the package-level BuildChangedLines; the fail-open contract (a git error
// disables the grounding gate) lives at the fan-out caller.
func (b *RangeBuilder) BuildChangedLines() (ChangedLines, error) {
	if !b.inUse.CompareAndSwap(0, 1) {
		panic("payload.RangeBuilder used concurrently: it is not safe for concurrent use")
	}
	defer b.inUse.Store(0)
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b.g.changedLines(b.base, b.head)
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
// range-level state if needed, so a RangeBuilder stays reusable after release.
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
}
