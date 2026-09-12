package payload

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// changedSymbolRepo builds a range where n DISTINCT symbols each change in their
// own file, and each has exactly one consumer that the diff never touches.
//
// One symbol per changed file, one consumer per symbol, is what makes the
// snippet count track the symbol-extraction loop one-for-one: if the loop reads
// k changed files it collects k symbols and retrieves k consumers. A shared
// consumer would let two symbols land in overlapping spans and collapse, and a
// repeated symbol would be clamped by maxEmittedSitesPerSymbol long before the
// changed-file ceiling could bind — either shape would make the assertion below
// pass for the wrong reason.
func changedSymbolRepo(t *testing.T, n int) (dir, base, head string) {
	t.Helper()
	dir = initRepo(t)

	decl := func(i int, ret string) string {
		return fmt.Sprintf("package store\n\nfunc Alpha%d(p string) (%s, error) {\n\t_ = p\n\treturn %s, nil\n}\n",
			i, ret, map[string]string{"string": `""`, "int": "0"}[ret])
	}

	for i := 0; i < n; i++ {
		write(t, dir, fmt.Sprintf("sym%d.go", i), decl(i, "string"))
		// The consumer is committed at BASE and never touched again, so it reaches
		// a reviewer only by reference retrieval.
		write(t, dir, fmt.Sprintf("use%d.go", i),
			fmt.Sprintf("package store\n\nfunc UseAlpha%d(p string) {\n\t_, _ = Alpha%d(p)\n}\n", i, i))
	}
	base = commitAll(t, dir, "seed symbols and their consumers")

	// Every declaring file changes; no consumer does.
	for i := 0; i < n; i++ {
		write(t, dir, fmt.Sprintf("sym%d.go", i), decl(i, "int"))
	}
	head = commitAll(t, dir, "change every symbol's return shape")
	return dir, base, head
}

// maxPrefetchChangedFiles is one of the AC4 latency bounds: it stops the
// symbol-extraction loop reading the HEAD blob of an unbounded number of changed
// files. It shipped with no test, and an unexercised ceiling is how a latency
// bound ships broken — silently, since a large diff simply costs unbounded blob
// reads and wasm parses before any `git grep` runs.
//
// The ceiling is injected through gitRunner.changedFileCeiling rather than read
// from the const, so this binds at 2 instead of needing a 251-changed-file
// fixture. Its sibling maxPrefetchFiles is 25 and stays a const, because 30 real
// files exercise it directly (TestRetrieveSnippets_StopsAtTheCandidateFileCap).
func TestBuildPrefetch_StopsAtTheChangedFileCeiling(t *testing.T) {
	dir, base, head := changedSymbolRepo(t, 3)

	t.Run("the default ceiling retrieves every changed symbol", func(t *testing.T) {
		// The control. Without it, a ceiling that bound at ZERO — or extraction
		// that broke for an unrelated reason — would satisfy the capped assertion
		// below just as well.
		g := newGitRunner(context.Background(), dir)
		_, _, st := g.buildPrefetch(base, head)

		require.True(t, st.Present, "three changed symbols each have an untouched consumer")
		require.Equal(t, 3, st.Snippets,
			"PRECONDITION: all three symbols must be retrievable, or the capped case below proves nothing")
	})

	t.Run("a bound ceiling stops the loop early", func(t *testing.T) {
		g := newGitRunner(context.Background(), dir)
		g.changedFileCeiling = 2

		_, _, st := g.buildPrefetch(base, head)

		require.Equal(t, 2, st.Snippets,
			"the loop must stop reading changed blobs at the ceiling; retrieving all three means the ceiling was ignored and the latency bound is not enforced")
	})

	t.Run("a non-positive ceiling falls back to the default, never to zero", func(t *testing.T) {
		// A gitRunner built as a literal rather than through newGitRunner carries a
		// zero ceiling. Reading that literally would disable symbol extraction for
		// the whole range — a silent, total loss of the feature, which is a worse
		// failure than the unbounded reads the ceiling exists to prevent.
		g := newGitRunner(context.Background(), dir)
		g.changedFileCeiling = 0

		_, _, st := g.buildPrefetch(base, head)

		require.Equal(t, 3, st.Snippets,
			"a zero ceiling must mean 'use the package default', not 'read no changed files at all'")
	})
}
