package payload

import (
	"context"
	"fmt"

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

// BuildEntries returns the per-file payload contributions for mode, reusing the
// builder's memoized range caches. Mirrors the package-level BuildEntries.
func (b *RangeBuilder) BuildEntries(mode PayloadMode) ([]FileEntry, error) {
	if mode != ModeDiff && mode != ModeBlocks && mode != ModeFiles {
		return nil, fmt.Errorf("unknown payload mode '%s': must be one of diff, blocks, files", mode)
	}
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b.g.buildEntriesValidated(mode, b.base, b.head)
}

// BuildChangedLines returns the grounding changed-lines map for the range,
// reusing the builder's memoized --name-status and zero-context diff. When a
// payload build already ran for this range on the same builder, this adds no git
// subprocess. Mirrors the package-level BuildChangedLines; the fail-open contract
// (a git error disables the grounding gate) lives at the fan-out caller.
func (b *RangeBuilder) BuildChangedLines() (ChangedLines, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b.g.changedLines(b.base, b.head)
}
