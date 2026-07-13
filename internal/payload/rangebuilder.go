package payload

import (
	"context"

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
// single-writer. Callers use it sequentially (payload modes, then grounding).
type RangeBuilder struct {
	g          *gitRunner
	base, head string
	validated  bool
}

// NewRangeBuilder returns a RangeBuilder for repo's base..head range, sharing one
// gitRunner (seeded with the context logger) across all its builds.
func NewRangeBuilder(ctx context.Context, repo, base, head string) *RangeBuilder {
	return &RangeBuilder{
		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
		base: base,
		head: head,
	}
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

// BuildEntries returns the per-file payload contributions for mode, reusing the
// builder's memoized range caches. Mirrors the package-level BuildEntries.
func (b *RangeBuilder) BuildEntries(mode PayloadMode) ([]FileEntry, error) {
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
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b.g.changedLines(b.base, b.head)
}
