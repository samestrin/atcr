package payload

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A RangeBuilder shares one gitRunner across the payload build and the grounding
// build for the same range, so computing grounding after building the payload
// spawns NO additional git subprocesses: everything grounding needs
// (ref validation, --name-status, and the --unified=0 diff) is already memoized
// by the files-mode payload build. Before this reuse, grounding ran its own
// gitRunner and re-spent validateRange + --name-status + --unified=0.
func TestRangeBuilder_GroundingReusesPayloadGitProcesses(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	write(t, dir, "b.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "a.go", goFileV2)
	write(t, dir, "b.go", goFileV2)
	head := commitAll(t, dir, "v2")

	rb := NewRangeBuilder(context.Background(), dir, base, head)

	// Files mode exercises the --unified=0 range split (via changedHeadRanges),
	// so after this build every whole-range diff grounding needs is memoized.
	_, err := rb.BuildEntries(ModeFiles)
	require.NoError(t, err)
	afterPayload := rb.g.execCount

	cl, err := rb.BuildChangedLines()
	require.NoError(t, err)
	require.Len(t, cl, 2, "both changed files present in grounding data")

	assert.Equal(t, afterPayload, rb.g.execCount,
		"grounding after a files-mode payload build must add zero git subprocesses (reused memoized range caches)")
}

// The RangeBuilder-scoped grounding data must be byte-for-byte identical to the
// standalone package-level BuildChangedLines — this is a pure performance reuse,
// not a semantics change.
func TestRangeBuilder_ChangedLinesMatchesStandalone(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "mod.go", goFileV1)
	write(t, dir, "old.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "mod.go", goFileV2)
	gitCmd(t, dir, "mv", "old.go", "new.go")
	write(t, dir, "new.go", goFileV2)
	write(t, dir, "added.go", "package p\n\nfunc Added() string { return \"added\" }\n")
	head := commitAll(t, dir, "v2")
	ctx := context.Background()

	standalone, err := BuildChangedLines(ctx, dir, base, head)
	require.NoError(t, err)

	rb := NewRangeBuilder(ctx, dir, base, head)
	reused, err := rb.BuildChangedLines()
	require.NoError(t, err)

	assert.Equal(t, standalone, reused,
		"RangeBuilder grounding must match standalone BuildChangedLines exactly")
}

// The zero-context diff is computed once per range even when both the files-mode
// range split and the grounding text parse consume it: rangeChunks and the
// grounding builder share the memoized --unified=0 chunks.
func TestRangeBuilder_ZeroContextDiffRunsOnce(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "a.go", goFileV2)
	head := commitAll(t, dir, "v2")

	g := &gitRunner{ctx: context.Background(), dir: dir}
	// First consumer: the range-split parse (files-mode line ranges).
	_, err := g.rangeChunks(base, head)
	require.NoError(t, err)
	afterRanges := g.execCount

	// Second consumer: grounding changed-text parse. Must reuse the memoized
	// zero-context chunks, adding no git process.
	_, err = g.changedLines(base, head)
	require.NoError(t, err)

	assert.Equal(t, afterRanges, g.execCount,
		"grounding changed-lines parse must reuse the memoized --unified=0 chunks (no extra git process)")
}
