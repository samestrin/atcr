package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/samestrin/atcr/internal/metrics"
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

	_, outcome := lz.resolve(context.Background(), []string{"RefreshToken"}, nil)
	assert.Equal(t, tier4Resolved, outcome)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "parser requested exactly once")

	_, _ = lz.resolve(context.Background(), []string{"RefreshToken"}, nil)
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
		got, outcome := lz.resolve(context.Background(), []string{"RefreshToken"}, nil)
		assert.Equal(t, tier4Resolved, outcome)
		assert.Equal(t, "internal/auth/session.go", got)
	})

	t.Run("declared in two files is ambiguous, never suggested", func(t *testing.T) {
		got, outcome := lz.resolve(context.Background(), []string{"Close"}, nil)
		assert.Equal(t, tier4Inconclusive, outcome, "AC7: multiple equally-plausible hits produce no suggestion")
		assert.Empty(t, got)
	})

	t.Run("unknown anchor matches nothing", func(t *testing.T) {
		got, outcome := lz.resolve(context.Background(), []string{"NeverDeclaredAnywhere"}, nil)
		assert.Equal(t, tier4NoMatch, outcome)
		assert.Empty(t, got)
	})

	t.Run("no anchors is not evidence of anything", func(t *testing.T) {
		_, outcome := lz.resolve(context.Background(), nil, nil)
		assert.Equal(t, tier4Inconclusive, outcome,
			"zero anchors means 'could not check' — it must never be reported as 'checked and found nothing'")
	})

	t.Run("one precise anchor wins over a co-cited noisy one", func(t *testing.T) {
		got, outcome := lz.resolve(context.Background(), []string{"Close", "DialPeer"}, nil)
		assert.Equal(t, tier4Resolved, outcome)
		assert.Equal(t, "internal/net/pool.go", got)
	})

	t.Run("two precise anchors disagreeing is ambiguous", func(t *testing.T) {
		got, outcome := lz.resolve(context.Background(), []string{"RefreshToken", "DialPeer"}, nil)
		assert.Equal(t, tier4Inconclusive, outcome)
		assert.Empty(t, got)
	})

	t.Run("a matched anchor plus an unknown one still resolves", func(t *testing.T) {
		got, outcome := lz.resolve(context.Background(), []string{"DialPeer", "NeverDeclaredAnywhere"}, nil)
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

	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	require.Equal(t, tier4Resolved, outcome, "one bad file must not abort the whole build")
	assert.Equal(t, "internal/net/pool.go", got)
}

// TestSymbolIndex_ParserFactoryFailureDegrades pins that a parser that cannot be
// obtained at all (a .wasm plugin that will not load) costs Tier 4 its
// RESOLUTION ability for that language without erroring — and, critically,
// without turning that loss into no-match "evidence": the file's raw tokens were
// harvested before the parse was attempted, so a symbol that is plainly in the
// source still reports "could not check".
func TestSymbolIndex_ParserFactoryFailureDegrades(t *testing.T) {
	root := t.TempDir()
	// Real source text here, not the basename placeholder writeTracked uses: this
	// test is precisely about the raw-token scan surviving a parser that never loads.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "net"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "net", "pool.go"),
		[]byte("package net\n\nfunc DialPeer(addr string) error { return nil }\n"), 0o644))

	lz := newLazySymbolIndex(root, []string{"internal/net/pool.go"})
	lz.newParser = func(string) (astgroup.Parser, error) { return nil, errors.New("wasm load failed") }

	before := metrics.Counter(tier4UnavailableMetric).Value()
	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome)
	assert.Empty(t, got)
	assert.Equal(t, before+1, metrics.Counter(tier4UnavailableMetric).Value(),
		"a silently-disabled Tier 4 must be observable, not indistinguishable from a clean run")

	_, outcome = lz.resolve(context.Background(), []string{"NotAnywhereInThatFile"}, nil)
	assert.Equal(t, tier4NoMatch, outcome,
		"the raw-token scan still ran, so a genuinely absent symbol is still a no-match")
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

	before := metrics.Counter(tier4UnavailableMetric).Value()
	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome, "over the cap, Tier 4 reports 'could not check', not 'no match'")
	assert.Empty(t, got)
	assert.Zero(t, atomic.LoadInt32(&calls), "the cap is checked before any parsing work")
	assert.Equal(t, before+1, metrics.Counter(tier4UnavailableMetric).Value(),
		"tripping the cap is counted so an always-off Tier 4 is visible in CI")
}

// TestSymbolIndex_FileCapEnvOverride pins the operator override:
// ATCR_TIER4_INDEX_MAX_FILES retunes the Tier 4 index cap without a rebuild,
// and an unset, unparseable, or non-positive value falls back to
// maxSymbolIndexFiles — a silently-zero cap would disable Tier 4 with no
// signal distinguishable from an intentional opt-out.
func TestSymbolIndex_FileCapEnvOverride(t *testing.T) {
	assert.Equal(t, maxSymbolIndexFiles, symbolIndexFileCap(), "unset env keeps the default cap")

	t.Setenv(tier4IndexMaxFilesEnv, "2")
	assert.Equal(t, 2, symbolIndexFileCap())

	// A tree over the override disables Tier 4 exactly as over the default does.
	root := t.TempDir()
	writeTracked(t, root, "internal/net/pool.go")

	paths := []string{"internal/gen/f0.go", "internal/gen/f1.go", "internal/gen/f2.go"}

	var calls int32
	lz := newLazySymbolIndex(root, paths)
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	before := metrics.Counter(tier4UnavailableMetric).Value()
	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome, "over the override cap, Tier 4 reports 'could not check', not 'no match'")
	assert.Empty(t, got)
	assert.Zero(t, atomic.LoadInt32(&calls), "the cap is checked before any parsing work")
	assert.Equal(t, before+1, metrics.Counter(tier4UnavailableMetric).Value())

	for _, bad := range []string{"abc", "0", "-5"} {
		t.Setenv(tier4IndexMaxFilesEnv, bad)
		assert.Equal(t, maxSymbolIndexFiles, symbolIndexFileCap(), "value %q must fall back to the default cap", bad)
	}
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

	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
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

	file, outcome := lz.resolve(context.Background(), []string{"LeakedSymbol"}, nil)
	assert.NotEqual(t, tier4Resolved, outcome, "a symlinked-out file must not appear in the index")
	assert.Empty(t, file)
	assert.Equal(t, tier4Inconclusive, outcome,
		"refusing to read it also left a hole in the search, so no-match is withheld too")

	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Resolved, outcome, "the contained file is still indexed and resolvable")
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

	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	require.Equal(t, tier4Resolved, outcome, "the real embedded go.wasm parser must find a top-level func")
	assert.Equal(t, "internal/net/pool.go", got)

	_, outcome = lz.resolve(context.Background(), []string{"NotInThisFile"}, nil)
	assert.Equal(t, tier4NoMatch, outcome)
}

// benchGoSource returns a realistic small Go source file for the index-build
// benchmark: a package clause, imports, a struct and several top-level funcs,
// so each parsed file yields a handful of declarations rather than one.
func benchGoSource(i int) string {
	return fmt.Sprintf(`package pkg%03d

import (
	"errors"
	"fmt"
)

type Conn%03d struct {
	addr string
	open bool
}

func DialPeer%03d(addr string) (*Conn%03d, error) {
	if addr == "" {
		return nil, errors.New("empty addr")
	}
	return &Conn%03d{addr: addr, open: true}, nil
}

func (c *Conn%03d) Close() error {
	c.open = false
	return nil
}

func (c *Conn%03d) String() string {
	return fmt.Sprintf("conn(%%s)", c.addr)
}

func closeIdle%03d(conns []*Conn%03d) int {
	n := 0
	for _, c := range conns {
		if !c.open {
			n++
		}
	}
	return n
}
`, i, i, i, i, i, i, i, i, i)
}

// BenchmarkSymbolIndexBuild_RealGoParser measures the wall-clock cost of the
// SERIAL Tier 4 index build against the real embedded go.wasm parser.
//
// It exists to answer a specific question before any code changes: the build
// loop in lazySymbolIndex.build parses eligible files one at a time, and the
// standing proposal is to parallelize it. internal/astgroup/host.go already
// commits this codebase to measuring before adding parser instances ("If
// profiling later shows real same-language contention, the remedy is a
// per-language pool of instances sized to GOMAXPROCS ... deferred until
// measurement justifies the extra instances"). A wasm module instance
// serializes its own Parse calls behind a mutex, so goroutines around one
// parser cannot speed this loop up at all — only additional instances could,
// and this benchmark is what decides whether that is worth their memory.
//
// Sized in files rather than run at the 5000-file cap so one iteration fits a
// normal `go test -bench` budget. The build loop is linear in file count, so
// per-file cost extrapolates: cap_cost = 5000 * (ns_per_op / files).
//
// The shared wasm host is warmed before the timer starts. That matches
// production: the Tier 4 index reuses the process-lifetime host that AST
// clustering and symbol anchoring have already compiled (T2), so one-time
// module compilation is not part of the per-run build cost this row is about.
func BenchmarkSymbolIndexBuild_RealGoParser(b *testing.B) {
	for _, fileCount := range []int{50, 200} {
		b.Run(fmt.Sprintf("files=%d", fileCount), func(b *testing.B) {
			root := b.TempDir()
			paths := make([]string, 0, fileCount)
			for i := range fileCount {
				rel := fmt.Sprintf("internal/pkg%03d/file.go", i)
				abs := filepath.Join(root, filepath.FromSlash(rel))
				require.NoError(b, os.MkdirAll(filepath.Dir(abs), 0o755))
				require.NoError(b, os.WriteFile(abs, []byte(benchGoSource(i)), 0o644))
				paths = append(paths, rel)
			}

			anchor := fmt.Sprintf("DialPeer%03d", 0)

			// Warm the shared host, and prove the build actually resolves before
			// timing anything — a benchmark over an index that silently failed to
			// build would report a fast, meaningless number.
			warm := newLazySymbolIndex(root, paths)
			if _, outcome := warm.resolve(context.Background(), []string{anchor}, nil); outcome != tier4Resolved {
				b.Fatalf("index build did not resolve %s (outcome %v) — benchmark would measure nothing", anchor, outcome)
			}

			b.ResetTimer()
			for range b.N {
				lz := newLazySymbolIndex(root, paths)
				lz.resolve(context.Background(), []string{anchor}, nil)
			}
		})
	}
}

// nopParser is a Parser that returns an empty file node without inspecting the
// source. It isolates everything the build loop does APART from parsing.
type nopParser struct{}

func (nopParser) Parse([]byte) (astgroup.Node, error) { return file(), nil }

// BenchmarkSymbolIndexBuild_NoParseBaseline is the companion measurement to
// BenchmarkSymbolIndexBuild_RealGoParser: the same build over the same corpus
// with the wasm Parse call replaced by a no-op, so it times only the parts of
// the loop that are free to run concurrently — os.ReadFile, path containment,
// and collectSourceIdentifiers.
//
// The gap between the two benchmarks is the parse cost, and parse is the part
// a single wasm instance serializes internally. That gap is what decides the
// standing "parallelize the index build" question: if it dominates, goroutines
// around one parser buy nothing and only a pool of instances could help.
func BenchmarkSymbolIndexBuild_NoParseBaseline(b *testing.B) {
	for _, fileCount := range []int{50, 200} {
		b.Run(fmt.Sprintf("files=%d", fileCount), func(b *testing.B) {
			root := b.TempDir()
			paths := make([]string, 0, fileCount)
			for i := range fileCount {
				rel := fmt.Sprintf("internal/pkg%03d/file.go", i)
				abs := filepath.Join(root, filepath.FromSlash(rel))
				require.NoError(b, os.MkdirAll(filepath.Dir(abs), 0o755))
				require.NoError(b, os.WriteFile(abs, []byte(benchGoSource(i)), 0o644))
				paths = append(paths, rel)
			}

			b.ResetTimer()
			for range b.N {
				lz := newLazySymbolIndex(root, paths)
				lz.newParser = func(string) (astgroup.Parser, error) { return nopParser{}, nil }
				lz.resolve(context.Background(), []string{"DialPeer000"}, nil)
			}
		})
	}
}

// TestSymbolIndex_SecondaryResolutionRequiresPrimaryMatch pins the rule that a
// FIX-derived suggestion is only meaningful when the finding's subject
// demonstrably exists in the tree. When no PROBLEM anchor matched anything,
// locate(secondary) must not fire: a FIX naming an existing collaborator
// ("route this through `parseConfigFile`") otherwise localizes a finding whose
// actual subject is absent, rendering a bogus "did you mean X?" suggestion
// instead of the no-match verdict the absent subject warrants.
func TestSymbolIndex_SecondaryResolutionRequiresPrimaryMatch(t *testing.T) {
	root := t.TempDir()
	writeTracked(t, root, "internal/config/load.go", "internal/config/dump.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"internal/config/load.go", "internal/config/dump.go"})
	lz.newParser = newStubFactory(fileTree{
		"load.go": file(fn("parseConfigFile", 5), fn("Close", 20)),
		"dump.go": file(fn("Close", 9)),
	}, &calls)

	t.Run("unmatched primary rejects the FIX-derived suggestion", func(t *testing.T) {
		got, outcome := lz.resolve(context.Background(), []string{"AbsentSubject"}, []string{"parseConfigFile"})
		assert.Equal(t, tier4NoMatch, outcome,
			"subject absent from the whole tree: sidecar-eligible, not resolved by its FIX")
		assert.Empty(t, got)
	})

	t.Run("empty primary cannot stand in via FIX anchors", func(t *testing.T) {
		got, outcome := lz.resolve(context.Background(), nil, []string{"parseConfigFile"})
		assert.Equal(t, tier4Inconclusive, outcome,
			"a FIX-only anchor set cannot stand in")
		assert.Empty(t, got)
	})

	t.Run("primary present in source text keeps the FIX-derived suggestion", func(t *testing.T) {
		// "load" is not parser-declared but appears in the raw source text of
		// load.go (the fixture content is the basename), so the subject is real
		// and a FIX-named collaborator may still localize the finding.
		got, outcome := lz.resolve(context.Background(), []string{"load"}, []string{"parseConfigFile"})
		assert.Equal(t, tier4Resolved, outcome)
		assert.Equal(t, "internal/config/load.go", got)
	})

	t.Run("imprecise primary declaration keeps the FIX-derived suggestion", func(t *testing.T) {
		// "Close" is declared in both files — real code, not localizable — so
		// the secondary resolution remains available.
		got, outcome := lz.resolve(context.Background(), []string{"Close"}, []string{"parseConfigFile"})
		assert.Equal(t, tier4Resolved, outcome)
		assert.Equal(t, "internal/config/load.go", got)
	})
}
