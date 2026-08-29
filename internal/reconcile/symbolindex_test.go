package reconcile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fileTree maps a source basename to the tree its "parser" should return, so a
// single stub factory can serve a multi-file index build.
type fileTree map[string]astgroup.Node

// newStubFactory builds a parser factory over trees keyed by file basename,
// counting how many times a parser was requested (the proxy for "the wazero
// runtime was touched").
func newStubFactory(trees fileTree, calls *int32) parserFactory {
	return func(lang string) (astgroup.Parser, error) {
		atomic.AddInt32(calls, 1)
		return parseByPath(trees), nil
	}
}

// parseByPath is a Parser that keys its answer off the source bytes, which the
// test fixtures set to the file's own basename.
type parseByPath fileTree

func (p parseByPath) Parse(src []byte) (astgroup.Node, error) {
	tree, ok := fileTree(p)[string(src)]
	if !ok {
		return astgroup.Node{}, errors.New("unparseable")
	}
	return tree, nil
}

// writeTracked writes each relpath under root with its own basename as content,
// which parseByPath uses to pick the right tree.
func writeTracked(t *testing.T, root string, relpaths ...string) {
	t.Helper()
	for _, rel := range relpaths {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(filepath.Base(rel)), 0o644))
	}
}

func fn(name string, line int) astgroup.Node {
	return astgroup.Node{Kind: "func", Name: name, StartLine: line, EndLine: line + 2}
}

func file(children ...astgroup.Node) astgroup.Node {
	return astgroup.Node{Kind: "file", StartLine: 1, EndLine: 500, Children: children}
}

// TestSymbolIndex_LazyUntilFirstResolve pins AC5: constructing the index costs
// nothing — no file is read and no parser is requested — until a Tier-4-eligible
// finding actually asks for a lookup.
func TestSymbolIndex_LazyUntilFirstResolve(t *testing.T) {
	root := t.TempDir()
	writeTracked(t, root, "internal/auth/session.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"internal/auth/session.go"})
	lz.newParser = newStubFactory(fileTree{"session.go": file(fn("RefreshToken", 12))}, &calls)

	assert.Zero(t, atomic.LoadInt32(&calls), "no parser requested before the first resolve")

	_, outcome := lz.resolve([]string{"RefreshToken"})
	assert.Equal(t, tier4Resolved, outcome)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "parser requested exactly once")

	_, _ = lz.resolve([]string{"RefreshToken"})
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "index is built once per run, not per lookup")
}

// TestSymbolIndex_ResolveOutcomes covers the three Tier 4 verdicts against one
// built index.
func TestSymbolIndex_ResolveOutcomes(t *testing.T) {
	root := t.TempDir()
	writeTracked(t, root, "internal/auth/session.go", "internal/net/pool.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"internal/auth/session.go", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{
		"session.go": file(fn("RefreshToken", 12), fn("Close", 40)),
		"pool.go":    file(fn("DialPeer", 7), fn("Close", 55)),
	}, &calls)

	t.Run("single declaring file resolves", func(t *testing.T) {
		got, outcome := lz.resolve([]string{"RefreshToken"})
		assert.Equal(t, tier4Resolved, outcome)
		assert.Equal(t, "internal/auth/session.go", got)
	})

	t.Run("declared in two files is ambiguous, never suggested", func(t *testing.T) {
		got, outcome := lz.resolve([]string{"Close"})
		assert.Equal(t, tier4Inconclusive, outcome, "AC7: multiple equally-plausible hits produce no suggestion")
		assert.Empty(t, got)
	})

	t.Run("unknown anchor matches nothing", func(t *testing.T) {
		got, outcome := lz.resolve([]string{"NeverDeclaredAnywhere"})
		assert.Equal(t, tier4NoMatch, outcome)
		assert.Empty(t, got)
	})

	t.Run("no anchors is not evidence of anything", func(t *testing.T) {
		_, outcome := lz.resolve(nil)
		assert.Equal(t, tier4Inconclusive, outcome,
			"zero anchors means 'could not check' — it must never be reported as 'checked and found nothing'")
	})

	t.Run("one precise anchor wins over a co-cited noisy one", func(t *testing.T) {
		got, outcome := lz.resolve([]string{"Close", "DialPeer"})
		assert.Equal(t, tier4Resolved, outcome)
		assert.Equal(t, "internal/net/pool.go", got)
	})

	t.Run("two precise anchors disagreeing is ambiguous", func(t *testing.T) {
		got, outcome := lz.resolve([]string{"RefreshToken", "DialPeer"})
		assert.Equal(t, tier4Inconclusive, outcome)
		assert.Empty(t, got)
	})

	t.Run("a matched anchor plus an unknown one still resolves", func(t *testing.T) {
		got, outcome := lz.resolve([]string{"DialPeer", "NeverDeclaredAnywhere"})
		assert.Equal(t, tier4Resolved, outcome)
		assert.Equal(t, "internal/net/pool.go", got)
	})
}

// TestSymbolIndex_SkipsUnusableFiles pins T2's success criterion: an
// unsupported-language file is never parsed, an unparseable one is skipped, and
// a missing one is skipped — none of them is fatal to the build.
func TestSymbolIndex_SkipsUnusableFiles(t *testing.T) {
	root := t.TempDir()
	writeTracked(t, root, "docs/readme.md", "internal/net/pool.go", "internal/bad/broken.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{
		"docs/readme.md",           // no parser language
		"internal/net/pool.go",     // parses
		"internal/bad/broken.go",   // parser returns an error
		"internal/gone/deleted.go", // tracked but absent on disk
	})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	got, outcome := lz.resolve([]string{"DialPeer"})
	require.Equal(t, tier4Resolved, outcome, "one bad file must not abort the whole build")
	assert.Equal(t, "internal/net/pool.go", got)
}

// TestSymbolIndex_ParserFactoryFailureDegrades pins that a parser that cannot be
// obtained at all (a .wasm plugin that will not load) disables Tier 4 for the
// run rather than erroring: every lookup reports "could not check", so nothing
// is routed to the sidecar on the strength of an index that was never built.
func TestSymbolIndex_ParserFactoryFailureDegrades(t *testing.T) {
	root := t.TempDir()
	writeTracked(t, root, "internal/net/pool.go")

	lz := newLazySymbolIndex(root, []string{"internal/net/pool.go"})
	lz.newParser = func(string) (astgroup.Parser, error) { return nil, errors.New("wasm load failed") }

	got, outcome := lz.resolve([]string{"DialPeer"})
	assert.Equal(t, tier4Inconclusive, outcome)
	assert.Empty(t, got)
}

// TestSymbolIndex_FileCapDisablesTier4 pins the clarified cost cap: a tree with
// more eligible files than the cap disables Tier 4 for that run instead of
// half-indexing, so no finding is judged against a partial view of the repo.
func TestSymbolIndex_FileCapDisablesTier4(t *testing.T) {
	root := t.TempDir()
	writeTracked(t, root, "internal/net/pool.go")

	paths := make([]string, 0, maxSymbolIndexFiles+1)
	for i := 0; i <= maxSymbolIndexFiles; i++ {
		paths = append(paths, fmt.Sprintf("internal/gen/f%d.go", i))
	}

	var calls int32
	lz := newLazySymbolIndex(root, paths)
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	got, outcome := lz.resolve([]string{"DialPeer"})
	assert.Equal(t, tier4Inconclusive, outcome, "over the cap, Tier 4 reports 'could not check', not 'no match'")
	assert.Empty(t, got)
	assert.Zero(t, atomic.LoadInt32(&calls), "the cap is checked before any parsing work")
}

// TestSymbolIndex_EscapingPathSkipped pins that a tracked path which would
// escape root is never read, mirroring the containment discipline in
// stream.ValidatePath and astgroup.Grouper.canonicalPath.
func TestSymbolIndex_EscapingPathSkipped(t *testing.T) {
	root := t.TempDir()
	writeTracked(t, root, "internal/net/pool.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"../outside/evil.go", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	got, outcome := lz.resolve([]string{"DialPeer"})
	assert.Equal(t, tier4Resolved, outcome)
	assert.Equal(t, "internal/net/pool.go", got)
}

// TestSymbolIndex_SymlinkEscapeSkipped pins the containment fix: a tracked path
// whose spelling is contained but which resolves, via a symlink, to a file
// outside root is never read or indexed. Git can track such a symlink, so the
// lexical check alone is not enough.
func TestSymbolIndex_SymlinkEscapeSkipped(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.go")
	require.NoError(t, os.WriteFile(secret, []byte("secret.go"), 0o644))

	root := t.TempDir()
	writeTracked(t, root, "internal/net/pool.go")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.Symlink(secret, filepath.Join(root, "pkg", "linked.go")))

	var calls int32
	lz := newLazySymbolIndex(root, []string{"pkg/linked.go", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{
		"pool.go":   file(fn("DialPeer", 7)),
		"secret.go": file(fn("LeakedSymbol", 3)),
	}, &calls)

	_, outcome := lz.resolve([]string{"LeakedSymbol"})
	assert.Equal(t, tier4NoMatch, outcome, "a symlinked-out file must not appear in the index")

	got, outcome := lz.resolve([]string{"DialPeer"})
	assert.Equal(t, tier4Resolved, outcome, "the contained file is still indexed")
	assert.Equal(t, "internal/net/pool.go", got)
}

// TestSymbolIndex_RealGoParser exercises the PRODUCTION path end to end: the
// default parser factory (astgroup.SharedHost().Parser) against the real
// embedded Go .wasm plugin, on real source. Every other test in this file stubs
// the parser, so without this one the wiring that actually ships is untested.
func TestSymbolIndex_RealGoParser(t *testing.T) {
	root := t.TempDir()
	src := "package pool\n\nfunc DialPeer(addr string) error {\n\treturn nil\n}\n\nfunc closeIdle() {}\n"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "net"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "net", "pool.go"), []byte(src), 0o644))

	lz := newLazySymbolIndex(root, []string{"internal/net/pool.go"})

	got, outcome := lz.resolve([]string{"DialPeer"})
	require.Equal(t, tier4Resolved, outcome, "the real embedded go.wasm parser must find a top-level func")
	assert.Equal(t, "internal/net/pool.go", got)

	_, outcome = lz.resolve([]string{"NotInThisFile"})
	assert.Equal(t, tier4NoMatch, outcome)
}
