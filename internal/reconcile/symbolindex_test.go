package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/samestrin/atcr/internal/metrics"
	reclib "github.com/samestrin/atcr/reconcile"
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
		"docs/readme.md",           // read and token-scanned, never parsed
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
//
// Every over-cap fixture file is WRITTEN TO DISK, and the sub-cap companion below
// is the reason why. An earlier version of this test handed the index paths that
// were never created, so tier4Inconclusive, the zero parser calls and the +1
// unavailable counter all held via the readFiles==0 bail whether or not the cap
// fired — the assertions were guaranteed by the missing files, not by the cap.
// With the files present, removing the cap flips this test.
func TestSymbolIndex_FileCapDisablesTier4(t *testing.T) {
	// A cap small enough to write real fixtures for. The default 5000 is exercised
	// by symbolIndexFileCap's own assertions in the env-override test; what needs
	// covering HERE is the branch, and the branch reads the effective cap.
	t.Setenv(tier4IndexMaxFilesEnv, "4")
	require.Equal(t, 4, symbolIndexFileCap())

	root := t.TempDir()
	paths := make([]string, 0, 5)
	for i := range 5 { // 5 > cap 4
		paths = append(paths, fmt.Sprintf("internal/gen/f%d.go", i))
	}
	writeTracked(t, root, paths...) // ON DISK: the cap must be what disables Tier 4

	var calls int32
	lz := newLazySymbolIndex(root, paths)
	lz.newParser = newStubFactory(fileTree{"f0.go": file(fn("DialPeer", 7))}, &calls)

	before := metrics.Counter(tier4UnavailableMetric).Value()
	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome, "over the cap, Tier 4 reports 'could not check', not 'no match'")
	assert.Empty(t, got)
	assert.Zero(t, atomic.LoadInt32(&calls), "the cap is checked before any parsing work")
	assert.Equal(t, before+1, metrics.Counter(tier4UnavailableMetric).Value(),
		"tripping the cap is counted so an always-off Tier 4 is visible in CI")
}

// TestSymbolIndex_JustUnderFileCapStillResolves is the companion that makes the
// test above load-bearing. Same fixtures, same parser, one fewer file — under the
// cap the index builds and resolves. Disabling the cap branch turns the over-cap
// case into this one, so the pair fails in exactly the way a working cap forbids.
func TestSymbolIndex_JustUnderFileCapStillResolves(t *testing.T) {
	t.Setenv(tier4IndexMaxFilesEnv, "4")

	root := t.TempDir()
	paths := make([]string, 0, 4)
	for i := range 4 { // 4 == cap 4, not over it
		paths = append(paths, fmt.Sprintf("internal/gen/f%d.go", i))
	}
	writeTracked(t, root, paths...)

	var calls int32
	lz := newLazySymbolIndex(root, paths)
	lz.newParser = newStubFactory(fileTree{"f0.go": file(fn("DialPeer", 7))}, &calls)

	before := metrics.Counter(tier4UnavailableMetric).Value()
	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Resolved, outcome, "at the cap the index builds normally")
	assert.Equal(t, "internal/gen/f0.go", got)
	assert.NotZero(t, atomic.LoadInt32(&calls), "parsing happens when the cap is not tripped")
	assert.Equal(t, before, metrics.Counter(tier4UnavailableMetric).Value(),
		"a healthy build must not touch the unavailable counter")
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
	// The fixtures are written to disk for the same reason the default-cap test
	// writes its own: with the files absent, the readFiles==0 bail produces every
	// assertion below whether or not the cap fires.
	root := t.TempDir()
	paths := []string{"internal/gen/f0.go", "internal/gen/f1.go", "internal/gen/f2.go"}
	writeTracked(t, root, paths...)

	var calls int32
	lz := newLazySymbolIndex(root, paths)
	lz.newParser = newStubFactory(fileTree{"f0.go": file(fn("DialPeer", 7))}, &calls)

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
//
// All THREE lexical rejections are exercised — absolute path, leading slash, and
// `..` — because they are separate branches of escapesIndexRoot and only the
// `..` one was ever covered. The absolute/leading-slash half is the lexical part
// of the posture that keeps the Tier 4 index from becoming an arbitrary-file
// reader, so each case names a real file OUTSIDE root whose symbol must not be
// resolvable through the index.
func TestSymbolIndex_EscapingPathSkipped(t *testing.T) {
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "evil.go"),
		[]byte("package evil\n\nfunc leakedOutsideSymbol() {}\n"), 0o644))

	root := t.TempDir()
	writeTracked(t, root, "internal/net/pool.go")

	for _, tc := range []struct {
		name string
		rel  string
	}{
		{"relative dot-dot", "../outside/evil.go"},
		{"absolute path", filepath.Join(outside, "evil.go")},
		{"leading slash", "/etc/hosts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, escapesIndexRoot(filepath.ToSlash(tc.rel)),
				"%q must be rejected on its spelling alone", tc.rel)

			var calls int32
			lz := newLazySymbolIndex(root, []string{tc.rel, "internal/net/pool.go"})
			lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

			_, outcome := lz.resolve(context.Background(), []string{"leakedOutsideSymbol"}, nil)
			assert.NotEqual(t, tier4Resolved, outcome,
				"a symbol declared outside root must never be resolvable through the index")

			got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
			assert.Equal(t, tier4Resolved, outcome, "the contained file is still indexed")
			assert.Equal(t, "internal/net/pool.go", got)
		})
	}
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

// TestLazySymbolIndex_NilReceiverIsInconclusive pins the defensive nil-receiver
// guard: a nil *lazySymbolIndex resolves to tier4Inconclusive — never
// tier4NoMatch — so a nil index can never route a finding to the sidecar.
func TestLazySymbolIndex_NilReceiverIsInconclusive(t *testing.T) {
	var lz *lazySymbolIndex
	_, outcome := lz.resolve(context.Background(), []string{"Anything"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome)
}

// TestSymbolIndex_ZeroEligibleFilesIncrementsUnavailable pins that the
// zero-eligible-files early return increments atcr_tier4_index_unavailable_total
// like the four other Tier-4-disabling paths, so a tracked tree that yields no
// searchable file is distinguishable from a healthy run in the metrics.
//
// Eligibility is CONTAINMENT, not parser support: the fixture is a tracked set
// every member of which escapes root (or is blank). A tree of .md/.yaml files is
// deliberately NOT zero-eligible — see
// TestSymbolIndex_NonParserOnlyTreeIsStillSearchable.
func TestSymbolIndex_ZeroEligibleFilesIncrementsUnavailable(t *testing.T) {
	root := t.TempDir()
	writeTracked(t, root, "docs/readme.md")

	lz := newLazySymbolIndex(root, []string{"../outside/evil.go", "/etc/hosts", ""})
	// No newParser override: containment filtering drops every path before any
	// parser would be requested.

	before := metrics.Counter(tier4UnavailableMetric).Value()
	_, outcome := lz.resolve(context.Background(), []string{"Anything"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome)
	assert.Equal(t, before+1, metrics.Counter(tier4UnavailableMetric).Value(),
		"zero eligible files is an unavailable index, and must say so in the metrics")
}

// TestSymbolIndex_NonParserOnlyTreeIsStillSearchable pins the counterpart the
// test above used to contradict: a tracked tree of only non-parser-language
// files is a perfectly searchable tree. Its raw tokens feed `presentInSource`, so
// the index is available (no unavailable counter), a construct named in it is real
// code, and only an anchor absent from all of it is a no-match.
func TestSymbolIndex_NonParserOnlyTreeIsStillSearchable(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "infra"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "infra", "main.tf"),
		[]byte("resource \"aws_s3_bucket\" \"artifactStore\" {}\n"), 0o644))
	writeTracked(t, root, "docs/readme.md")

	lz := newLazySymbolIndex(root, []string{"infra/main.tf", "docs/readme.md"})

	before := metrics.Counter(tier4UnavailableMetric).Value()
	_, outcome := lz.resolve(context.Background(), []string{"artifactStore"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"a construct named in Terraform is real code, not a phantom")
	assert.Equal(t, before, metrics.Counter(tier4UnavailableMetric).Value(),
		"a parser-less tree is searchable, so Tier 4 is available and must not be counted unavailable")

	_, outcome = lz.resolve(context.Background(), []string{"NotAnywhereInThisTree"}, nil)
	assert.Equal(t, tier4NoMatch, outcome, "the tree was searched, and the anchor is genuinely absent")
}

// TestSymbolIndex_UnparsedLanguageStillFeedsPresentInSource pins the polyglot
// hole: eligiblePaths used to drop every tracked file whose extension has no
// parser language BEFORE the index was built, so Ruby, Swift, Scala, Elixir, SQL,
// Terraform and YAML were structurally invisible to the raw-token set while
// `complete` stayed true. A finding whose construct genuinely lives in one of
// those files, but whose citation names a non-existent parser-language path, then
// reached tier4NoMatch and was routed out of findings.json as fabricated.
//
// Every tracked NON-DOCUMENTATION file now feeds the raw-token scan; only byName
// keeps the parser filter. Documentation is excluded on purpose and by a
// different rule (isDocExt) — prose names constructs it does not declare, which
// is what TestSymbolIndex_DocProseDoesNotSuppressNoMatch pins.
func TestSymbolIndex_UnparsedLanguageStillFeedsPresentInSource(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "config.rb"),
		[]byte("def parse_config(path)\n  YAML.load_file(path)\nend\n"), 0o644))
	writeTracked(t, root, "internal/net/pool.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"lib/config.rb", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	_, outcome := lz.resolve(context.Background(), []string{"parse_config"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"a construct declared in an unparseable language is real code — never a phantom")

	_, outcome = lz.resolve(context.Background(), []string{"NotAnywhereInThisTree"}, nil)
	assert.Equal(t, tier4NoMatch, outcome,
		"the tree is still searchable: a genuinely absent anchor is still a no-match")

	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Resolved, outcome, "byName keeps the parser filter and still localizes")
	assert.Equal(t, "internal/net/pool.go", got)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "the .rb file is read, never parsed")
}

// TestSymbolIndex_BinaryFileSkippedWithoutHole pins the bound on the scan above.
// Tracked binaries (this repo carries ~31MB of embedded .wasm parser plugins)
// must not be token-scanned: the cost is real and their byte noise would inflate
// `presentInSource` with tokens that no reviewer ever named. Skipping one is NOT
// a hole either — no source construct is declared in a binary — so `complete` stays
// true and the no-match verdict remains available.
func TestSymbolIndex_BinaryFileSkippedWithoutHole(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "plugins"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "plugins", "go.wasm"),
		append([]byte("\x00asm\x01\x00\x00\x00"), []byte("embeddedBinaryToken\x00\x00")...), 0o644))
	writeTracked(t, root, "internal/net/pool.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"plugins/go.wasm", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	_, outcome := lz.resolve(context.Background(), []string{"embeddedBinaryToken"}, nil)
	assert.Equal(t, tier4NoMatch, outcome,
		"a binary's byte noise must not enter `presentInSource`, and skipping it must not withhold the verdict")

	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Resolved, outcome)
	assert.Equal(t, "internal/net/pool.go", got)
}

// TestSymbolIndex_AllEligibleFilesUnreadable pins the readFiles==0 bail, which
// no test reached on its own merits: the file-cap tests used to hand the index
// paths that were never written, so they arrived here by accident while claiming
// to cover the cap. Both now write their fixtures, leaving this path uncovered
// unless a test aims at it.
//
// The bail is what stops an index that read NOTHING from answering "checked and
// found nothing" — the false-fabrication verdict this epic exists to prevent.
// Mutating it to `if false` must fail here.
func TestSymbolIndex_AllEligibleFilesUnreadable(t *testing.T) {
	root := t.TempDir()
	// Tracked, parser-language, root-contained — and absent from disk, so every
	// one fails its read.
	paths := []string{"internal/gen/gone0.go", "internal/gen/gone1.go"}

	var calls int32
	lz := newLazySymbolIndex(root, paths)
	lz.newParser = newStubFactory(fileTree{}, &calls)

	before := metrics.Counter(tier4UnavailableMetric).Value()
	got, outcome := lz.resolve(context.Background(), []string{"NotAnywhereInThisTree"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"an index that read nothing knows nothing — 'not in the tree' carries no information")
	assert.Empty(t, got)
	assert.Equal(t, before+1, metrics.Counter(tier4UnavailableMetric).Value(),
		"an index that read nothing is unavailable, and must say so in the metrics")
}

// TestSymbolIndex_UnreadableFileAdmitsHole covers the unreadable-file
// `complete = false` branch and the atcr_tier4_index_incomplete_total counter
// end to end. TestTier4Safety_IncompleteIndexIsNeverNoMatch asserts the VERDICT
// this branch produces but never the counter, and no test at all separated
// "read nothing" (unavailable) from "a hole in a working index" (incomplete) —
// the two counters exist precisely because their causes and fixes differ.
//
// Mutating the `complete = false` on the read-error path to a no-op must fail
// here: the index would then answer no-match for a symbol it never searched for.
func TestSymbolIndex_UnreadableFileAdmitsHole(t *testing.T) {
	root := t.TempDir()
	writeTracked(t, root, "internal/net/pool.go")

	// The fixture must be a file that RESOLVES but cannot be READ. An absent file
	// is not one: containedIndexPath's EvalSymlinks fails first, so it admits its
	// hole through the containment branch and never reaches the read at all —
	// which is why TestTier4Safety_IncompleteIndexIsNeverNoMatch, whose fixture is
	// a missing file, leaves this branch uncovered despite appearances.
	unreadable := filepath.Join(root, "internal", "net", "locked.go")
	require.NoError(t, os.WriteFile(unreadable, []byte("package net\n"), 0o644))
	require.NoError(t, os.Chmod(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) }) // let TempDir clean up
	if _, err := os.ReadFile(unreadable); err == nil {
		t.Skip("filesystem or privileges ignore mode 0000; cannot stage an unreadable file")
	}

	var calls int32
	lz := newLazySymbolIndex(root, []string{"internal/net/pool.go", "internal/net/locked.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	beforeIncomplete := metrics.Counter(tier4IncompleteMetric).Value()
	beforeUnavailable := metrics.Counter(tier4UnavailableMetric).Value()

	_, outcome := lz.resolve(context.Background(), []string{"GenuinelyAbsentSymbol"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"a region of the tree went unsearched, so 'not found' is unproven")

	assert.Equal(t, beforeIncomplete+1, metrics.Counter(tier4IncompleteMetric).Value(),
		"a hole in an otherwise working index is counted as incomplete")
	assert.Equal(t, beforeUnavailable, metrics.Counter(tier4UnavailableMetric).Value(),
		"an incomplete index is NOT an unavailable one — the two causes and fixes differ")

	// The index still works for what it did read: only the no-match direction is
	// withheld, never resolution.
	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Resolved, outcome)
	assert.Equal(t, "internal/net/pool.go", got)
}

// TestSymbolIndex_ParserLanguageSkipIsNotAHole covers the parserFailed language
// skip: once a language's parser cannot be obtained, every later file of that
// language is skipped for DECLARATIONS only. Its raw tokens were already
// harvested, so the skip costs resolution, never the no-match guard — and it must
// NOT be counted as a hole, or one broken parser would withhold every verdict.
func TestSymbolIndex_ParserLanguageSkipIsNotAHole(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "net"), 0o755))
	for i, name := range []string{"pool.go", "dial.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "net", name),
			[]byte(fmt.Sprintf("package net\n\nfunc dialPeer%d() error { return nil }\n", i)), 0o644))
	}

	var attempts int32
	lz := newLazySymbolIndex(root, []string{"internal/net/dial.go", "internal/net/pool.go"})
	lz.newParser = func(string) (astgroup.Parser, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, errors.New("wasm load failed")
	}

	beforeIncomplete := metrics.Counter(tier4IncompleteMetric).Value()

	_, outcome := lz.resolve(context.Background(), []string{"dialPeer0"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"the token scan ran, so a symbol plainly in the source is real code")

	_, outcome = lz.resolve(context.Background(), []string{"NotAnywhereInThisTree"}, nil)
	assert.Equal(t, tier4NoMatch, outcome,
		"a failed parser costs resolution, not the search: a genuinely absent symbol is still a no-match")

	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts),
		"parserFailed short-circuits the second file of the same language")
	assert.Equal(t, beforeIncomplete, metrics.Counter(tier4IncompleteMetric).Value(),
		"a language whose parser will not load is a resolution loss, not a hole in the search")
}

// TestContainedIndexPath_ExitCoverage covers all four exits of the symlink
// containment resolver, two of which no test reached: the best-effort
// EvalSymlinks(root) fallback and the filepath.Rel error branch.
//
// Both are driven by an UNRESOLVABLE root, which is also the only way to reach
// the Rel branch on the supported platforms: Rel fails when exactly one of its
// arguments is absolute, and `resolved` is always absolute, so the failure needs
// a realRoot that stayed relative — which is precisely what the fallback leaves
// behind when EvalSymlinks(root) fails on a relative root.
//
// The resolver must answer ("", false) there rather than panicking or, worse,
// admitting the path: a root it cannot resolve is a root it cannot contain
// anything against.
func TestContainedIndexPath_ExitCoverage(t *testing.T) {
	real := t.TempDir()
	writeTracked(t, real, "internal/net/pool.go")

	t.Run("contained path resolves", func(t *testing.T) {
		got, ok := containedIndexPath(real, "internal/net/pool.go")
		require.True(t, ok)
		assert.True(t, filepath.IsAbs(got))
		assert.Equal(t, "pool.go", filepath.Base(got))
	})

	t.Run("absent target is treated as absent", func(t *testing.T) {
		got, ok := containedIndexPath(real, "internal/net/gone.go")
		assert.False(t, ok, "a path that cannot be resolved is skipped, exactly as the read would have")
		assert.Empty(t, got)
	})

	t.Run("unresolvable root falls back, then Rel rejects", func(t *testing.T) {
		// root "" cannot be EvalSymlinks'd, so realRoot keeps the unresolvable
		// value; the joined path still resolves (it is absolute), and Rel then
		// fails because realRoot is not.
		got, ok := containedIndexPath("", filepath.Join(real, "internal", "net", "pool.go"))
		assert.False(t, ok, "a root that cannot be resolved must contain nothing")
		assert.Empty(t, got)
	})

	t.Run("unresolvable root and unresolvable target", func(t *testing.T) {
		got, ok := containedIndexPath(filepath.Join(real, "no-such-root"), "internal/net/pool.go")
		assert.False(t, ok)
		assert.Empty(t, got)
	})
}

// TestIsBinaryContent_SniffWindow pins the bound on the binary sniff: only the
// first binarySniffBytes are inspected, so a NUL past that window is not seen.
// That is the deliberate trade — an unbounded scan of every tracked file is the
// cost this cap exists to avoid — and the miss is in the SAFE direction: the file
// is token-scanned, which over-collects into `presentInSource` and can only withhold a
// no-match verdict, never manufacture one.
func TestIsBinaryContent_SniffWindow(t *testing.T) {
	assert.False(t, isBinaryContent([]byte("package net\n\nfunc DialPeer() {}\n")),
		"ordinary source text is not binary")
	assert.True(t, isBinaryContent(append([]byte("\x00asm"), make([]byte, 64)...)),
		"a NUL inside the sniff window classifies the blob as binary")

	late := make([]byte, binarySniffBytes+16)
	for i := range late {
		late[i] = 'a'
	}
	late[binarySniffBytes+8] = 0
	assert.False(t, isBinaryContent(late),
		"a NUL past the sniff window is not seen — bounded on purpose, and safe: the file is merely scanned")
}

// TestSymbolIndex_OversizeFileSkippedWithoutHole pins the single-file read cap,
// in both of the halves it actually has.
//
// The index build reads EVERY contained tracked file, so one checked-in artifact
// — a dataset, a dump, a model weight — is otherwise pulled into memory whole on
// every build. Skipping it is not a hole for the same reason a binary is not: a
// reviewer does not name a source construct inside a multi-megabyte blob, so the
// no-match verdict stays available.
//
// The VERDICT half above is not enough on its own, and the open-count half below
// is why. readIndexSource refuses an over-cap file anyway, through its
// sniff-window read and io.LimitReader race guard, so deleting the build-side
// Lstat skip changes no verdict and every assertion here would still pass — a
// guard nothing can distinguish from its absence. What the skip actually buys is
// never OPENING the file: up to maxSourceFileBytes of read avoided per over-cap
// file, on a loop that walks the whole tracked tree. An open count is the only
// observable form of that, so the openIndexSource seam is swapped here to take
// one. That is also why this test must not call t.Parallel.
//
// The fixture is a .go file, not the .txt it used to be: .txt is a docExt, so
// its tokens never reach presentInSource whether the cap skips it or not, and
// the no-match assertion below would have held for a reason that has nothing to
// do with the cap.
func TestSymbolIndex_OversizeFileSkippedWithoutHole(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "data"), 0o755))
	// Text (no NUL, so the binary sniff does not catch it either) and over the cap
	// by one byte — the boundary itself must not be skipped, only what exceeds it.
	huge := make([]byte, maxSourceFileBytes+1)
	for i := range huge {
		huge[i] = 'a'
	}
	copy(huge, []byte("package data\n\nvar oversizeDatasetToken = 1\n"))
	require.NoError(t, os.WriteFile(filepath.Join(root, "data", "dump.go"), huge, 0o644))
	writeTracked(t, root, "internal/net/pool.go")

	// Keyed by suffix, not by the absolute path this test built: containedIndexPath
	// resolves symlinks, and a macOS t.TempDir() lives under /var -> /private/var.
	var mu sync.Mutex
	opens := map[string]int{}
	prevOpen := openIndexSource
	openIndexSource = func(abs string) (*os.File, error) {
		mu.Lock()
		opens[filepath.ToSlash(abs)]++
		mu.Unlock()
		return prevOpen(abs)
	}
	t.Cleanup(func() { openIndexSource = prevOpen })
	countFor := func(relsuffix string) int {
		mu.Lock()
		defer mu.Unlock()
		total := 0
		for abs, n := range opens {
			if strings.HasSuffix(abs, relsuffix) {
				total += n
			}
		}
		return total
	}

	var calls int32
	lz := newLazySymbolIndex(root, []string{"data/dump.go", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	_, outcome := lz.resolve(context.Background(), []string{"oversizeDatasetToken"}, nil)
	assert.Equal(t, tier4NoMatch, outcome,
		"an over-cap file must not enter `presentInSource`, and skipping it must not withhold the verdict")

	got, outcome := lz.resolve(context.Background(), []string{"DialPeer"}, nil)
	assert.Equal(t, tier4Resolved, outcome, "the rest of the tree must still resolve")
	assert.Equal(t, "internal/net/pool.go", got)

	mu.Lock()
	distinct := len(opens)
	mu.Unlock()
	require.Equal(t, 1, distinct,
		"exactly one path may be opened; asserting the SET keeps the suffix matching below unambiguous")
	assert.Zero(t, countFor("data/dump.go"),
		"an over-cap file must be skipped on its Lstat size, never opened: the skip is a read the build does not pay")
	assert.Equal(t, 1, countFor("internal/net/pool.go"),
		"the seam must be wired, and each eligible file read exactly once per build")
}

// TestSymbolIndex_AtCapSizeFileIsStillRead is the boundary companion that makes
// the over-cap assertion above mean what it says, mirroring the pairing the
// file-COUNT cap already has (TestSymbolIndex_FileCapDisablesTier4 at cap+1,
// TestSymbolIndex_JustUnderFileCapStillResolves at cap).
//
// Without it, only maxSourceFileBytes+1 is pinned, and mutating the comparison
// from `>` to `>=` would skip a file exactly AT the cap with nothing to catch it
// — a guard drifting by one byte, which is the same class of undetectable guard
// AC8 exists to remove. The cap is the largest size that is still read.
//
// Swaps the openIndexSource seam, so it must not call t.Parallel.
func TestSymbolIndex_AtCapSizeFileIsStillRead(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "data"), 0o755))
	atCap := make([]byte, maxSourceFileBytes)
	for i := range atCap {
		atCap[i] = 'a'
	}
	copy(atCap, []byte("package data\n\nvar atCapDatasetToken = 1\n"))
	require.NoError(t, os.WriteFile(filepath.Join(root, "data", "edge.go"), atCap, 0o644))

	var mu sync.Mutex
	opens := 0
	prevOpen := openIndexSource
	openIndexSource = func(abs string) (*os.File, error) {
		if strings.HasSuffix(filepath.ToSlash(abs), "data/edge.go") {
			mu.Lock()
			opens++
			mu.Unlock()
		}
		return prevOpen(abs)
	}
	t.Cleanup(func() { openIndexSource = prevOpen })

	var calls int32
	lz := newLazySymbolIndex(root, []string{"data/edge.go"})
	lz.newParser = newStubFactory(fileTree{}, &calls)

	_, outcome := lz.resolve(context.Background(), []string{"atCapDatasetToken"}, nil)

	mu.Lock()
	got := opens
	mu.Unlock()
	assert.Equal(t, 1, got,
		"a file exactly AT the cap is within it and must still be opened and read")
	assert.Equal(t, tier4Inconclusive, outcome,
		"its tokens reached presentInSource, so the construct is real code the index could not localize")
}

// TestSymbolIndex_DocProseDoesNotSuppressNoMatch pins that documentation prose
// cannot stand in for a declaration.
//
// The read set was once widened to every text file so unparsed LANGUAGES stayed
// searchable, which also pulled README/CHANGELOG/docs prose into the raw-token
// set. Those files carry camelCase and snake_case tokens that pass
// isIdentifierShaped, so a construct DELETED from the code but still named in a
// changelog scored "present" and the finding came back inconclusive instead of
// routed — the no-match detector losing sensitivity while still reporting
// state=applied. isDocExt now keeps documentation out of presentInSource, and
// this test is the guard on that.
func TestSymbolIndex_DocProseDoesNotSuppressNoMatch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"),
		[]byte("## 1.2.0\n\n- Removed `quantumFlux`, the retry handle helper.\n"), 0o644))
	writeTracked(t, root, "internal/net/pool.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"CHANGELOG.md", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	_, outcome := lz.resolve(context.Background(), []string{"quantumFlux"}, nil)
	assert.Equal(t, tier4NoMatch, outcome,
		"a construct named only in prose is declared nowhere in source: that is a no-match, not evidence it exists")

	// The widening this guard narrows must survive: a real construct in an
	// unparsed LANGUAGE still counts as present.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "helper.rb"), []byte("def ruby_only_helper\nend\n"), 0o644))
	lz2 := newLazySymbolIndex(root, []string{"lib/helper.rb", "internal/net/pool.go"})
	lz2.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)
	_, outcome = lz2.resolve(context.Background(), []string{"ruby_only_helper"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"a construct that genuinely lives in an unparsed language must still be shielded from no-match")
}

// TestSymbolIndex_DocProseDoesNotLicensePathSuggestion pins the other half of
// the presence asymmetry: prose may not LICENSE a suggestion either.
//
// The no-match shield reads presentInSource, so a construct named only in a
// changelog is correctly called absent there. The primaryMatched gate in
// resolve read the wide `present`, so the very same anchor satisfied it, let
// locate(secondary) run, and stamped a confident PathSuggestion (validate.go:133)
// for a subject the next branch would have called absent. Both reads have to
// consult the same set.
func TestSymbolIndex_DocProseDoesNotLicensePathSuggestion(t *testing.T) {
	newIndex := func(t *testing.T, root string, tracked ...string) *lazySymbolIndex {
		t.Helper()
		var calls int32
		lz := newLazySymbolIndex(root, tracked)
		lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("SessionPool", 12))}, &calls)
		return lz
	}

	// Every docExts entry, not only .md: the gate must read the same set the
	// shield does for all of them, and .txt is the entry the docExts doc block
	// itself calls "the one uncomfortable entry". Ranging the map directly —
	// rather than a hardcoded literal list — is what makes the claim true: a
	// seventh entry added to docExts is covered here automatically.
	exts := make([]string, 0, len(docExts))
	for ext := range docExts {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	for _, ext := range exts {
		doc := "NOTES" + ext
		t.Run("subject named only in "+ext+" licenses nothing", func(t *testing.T) {
			root := t.TempDir()
			// The export-leading line pins exportDeclaringExts membership in the
			// WIDENING direction: if this extension were ever added (or
			// declaresByExport widened to isDocExt), its export lines would feed
			// presentInSource and shieldedOnlyExport would license a suggestion —
			// exactly the prose-licensing failure the split exists to prevent.
			// .mdx legitimately declares by export, so it is exempt here. The guard
			// keys on the EXTENSION, not on declaresByExport — keying on the
			// function under test would let a widening mutation exempt the very
			// fixture that is supposed to catch it.
			content := "## 2.0.0\n\n- Removed `quantumFlux`, the retry handle helper.\n"
			if ext != ".mdx" {
				content += "\nexport const shieldedOnlyExport = 1\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(root, doc),
				[]byte(content), 0o644))
			writeTracked(t, root, "internal/session/pool.go")

			lz := newIndex(t, root, doc, "internal/session/pool.go")
			got, outcome := lz.resolve(context.Background(), []string{"quantumFlux"}, []string{"SessionPool"})

			assert.Equal(t, tier4NoMatch, outcome,
				"a subject that exists only in prose was searched for and not found: that is a no-match")
			assert.Empty(t, got,
				"no PathSuggestion may be stamped for a subject the source-presence shield calls absent")

			if ext != ".mdx" {
				got, outcome = lz.resolve(context.Background(), []string{"shieldedOnlyExport"}, []string{"SessionPool"})
				assert.Equal(t, tier4NoMatch, outcome,
					"an export-leading line in a %s file is prose, not a declaration — only exportDeclaringExts split", ext)
				assert.Empty(t, got,
					"a prose export line must not license a PathSuggestion either")
			}
		})
	}

	t.Run("an incomplete index withholds the no-match instead of routing", func(t *testing.T) {
		// The narrowed gate must not turn an unproven search into a routed
		// finding: a hole in the tree means "could not check", never "checked and
		// found nothing". The tracked-but-absent file is what makes complete false.
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"),
			[]byte("## 2.0.0\n\n- Removed `quantumFlux`, the retry handle helper.\n"), 0o644))
		writeTracked(t, root, "internal/session/pool.go")

		lz := newIndex(t, root, "CHANGELOG.md", "internal/session/pool.go", "internal/gone/deleted.go")
		got, outcome := lz.resolve(context.Background(), []string{"quantumFlux"}, []string{"SessionPool"})

		assert.Equal(t, tier4Inconclusive, outcome,
			"a search with a hole in it cannot ground a no-match, however the gate reads")
		assert.Empty(t, got)
	})

	t.Run("subject in real source still licenses the suggestion", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "lib"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "helper.rb"),
			[]byte("def ruby_only_helper\nend\n"), 0o644))
		writeTracked(t, root, "internal/session/pool.go")

		lz := newIndex(t, root, "lib/helper.rb", "internal/session/pool.go")
		got, outcome := lz.resolve(context.Background(), []string{"ruby_only_helper"}, []string{"SessionPool"})

		assert.Equal(t, tier4Resolved, outcome,
			"narrowing the gate must not stop a real construct in an unparsed language from localizing")
		assert.Equal(t, "internal/session/pool.go", got)
	})

	t.Run("subject in both prose and source still licenses the suggestion", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"),
			[]byte("## 2.0.0\n\n- Reworked `ruby_only_helper`.\n"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "lib"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "helper.rb"),
			[]byte("def ruby_only_helper\nend\n"), 0o644))
		writeTracked(t, root, "internal/session/pool.go")

		lz := newIndex(t, root, "CHANGELOG.md", "lib/helper.rb", "internal/session/pool.go")
		got, outcome := lz.resolve(context.Background(), []string{"ruby_only_helper"}, []string{"SessionPool"})

		assert.Equal(t, tier4Resolved, outcome,
			"prose naming a construct that also exists in source must not subtract from its presence")
		assert.Equal(t, "internal/session/pool.go", got)
	})
}

// TestSymbolIndex_MDXDeclarationIsSourceNotProse pins AC2: a documentation
// EXTENSION is not the same thing as prose.
//
// MDX is executable JS — `export function Callout() {}` in docs/guide.mdx
// genuinely DECLARES Callout — but .mdx was classified by name alongside .md, so
// its tokens were kept out of presentInSource. A finding naming such a construct,
// cited against a path that does not exist, reached tier4NoMatch and was routed
// out of the primary report: out of the gate exit code, out of report.md, out of
// the skeptic pipeline's findings.json, and durably charged to the reviewer as a
// phantom. The construct was real the whole time.
//
// The file is SPLIT rather than reclassified: its export lines feed
// presentInSource, everything else in it feeds only presentInDocs. A blanket
// re-admission would not have been safe — presentInSource is read by the
// primaryMatched gate as well as the no-match shield, so an extra token there
// also licenses a confident PathSuggestion, which is the failure
// TestSymbolIndex_DocProseDoesNotLicensePathSuggestion forbids.
func TestSymbolIndex_MDXDeclarationIsSourceNotProse(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "guide.mdx"),
		[]byte("import {Note} from './ui'\n\nexport function Callout() { return null }\n\nexport {NamedReExport} from './x'\n\nThe `quantumFlux` helper was removed in 2.0.\n\nexported_retryHandle is gone\n\nexports.quantumFlux = 1\n\n```jsx\nexport const fencedOnlyExport = 1\n```\n"), 0o644))
	writeTracked(t, root, "internal/net/pool.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"docs/guide.mdx", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	_, outcome := lz.resolve(context.Background(), []string{"Callout"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"a construct an MDX file declares is real code that Tier 4 cannot localize, not a phantom")

	// The prose in the SAME file gets none of that credit. This is the half a
	// blanket "drop .mdx from docExts" would have lost: an anchor mentioned in an
	// MDX paragraph would have satisfied primaryMatched and licensed a confident
	// PathSuggestion off the FIX anchor — the exact inversion
	// TestSymbolIndex_DocProseDoesNotLicensePathSuggestion forbids.
	got, outcome := lz.resolve(context.Background(), []string{"quantumFlux"}, []string{"DialPeer"})
	assert.Equal(t, tier4NoMatch, outcome,
		"a construct named only in MDX PROSE is named in prose, whatever the file also declares")
	assert.Empty(t, got, "MDX prose must not license a path suggestion")

	// A code EXAMPLE in a fence is illustrating an export, not declaring one, and
	// must get none of the declaration credit either — otherwise any docs page
	// showing `export const metadata = ...` would license suggestions for it.
	_, outcome = lz.resolve(context.Background(), []string{"fencedOnlyExport"}, nil)
	assert.Equal(t, tier4NoMatch, outcome,
		"an export inside a fenced code block is a code sample, not a declaration")

	// The shield itself is unchanged: a name that appears nowhere at all in the
	// tree is still a no-match, MDX or no MDX.
	_, outcome = lz.resolve(context.Background(), []string{"NotAnywhereInThisTree"}, nil)
	assert.Equal(t, tier4NoMatch, outcome,
		"widening the source set must not blunt the verdict for a genuinely absent construct")

	// The word-boundary guard: a line that merely STARTS WITH the letters "export"
	// (`exported ...`, `exports.foo = ...`) is prose, not a declaration. Deleting
	// the guard admits both tokens into presentInSource — suppressing the no-match
	// AND licensing a suggestion.
	for _, tok := range []string{"exported_retryHandle", "quantumFlux"} {
		got, outcome := lz.resolve(context.Background(), []string{tok}, []string{"DialPeer"})
		assert.Equal(t, tier4NoMatch, outcome,
			"%s appears only on a line that starts with the letters export — that is not an export declaration", tok)
		assert.Empty(t, got, "%s must not license a PathSuggestion from prose", tok)
	}

	// The `{` allowance is pinned positively: `export {NamedReExport} from './x'`
	// IS a declaration, so the token reaches presentInSource and shields the
	// no-match.
	_, outcome = lz.resolve(context.Background(), []string{"NamedReExport"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"a brace re-export declares — deleting the { allowance would misroute a real construct")
}

// TestSymbolIndex_NamedInDocsExplainsTheRouting pins the index half of the
// durable-accounting fix: presentInDocs must hold what isDocExt kept out of
// presentInSource, and nothing else.
//
// namedInDocs is asked only after resolve has already returned a no-match, so a
// true answer means "the doc-extension heuristic is what routed this", not "the
// construct exists". It must never influence a verdict — that is the
// prose-suppression hole the parent epic closed.
func TestSymbolIndex_NamedInDocsExplainsTheRouting(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"),
		[]byte("## 2.0.0\n\n- Removed `quantumFlux`, the retry handle helper.\n"+
			"- Reworked `ruby_only_helper`, which still exists.\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "helper.rb"),
		[]byte("def ruby_only_helper\nend\n"), 0o644))
	writeTracked(t, root, "internal/net/pool.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"CHANGELOG.md", "lib/helper.rb", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	assert.False(t, lz.namedInDocs([]string{"quantumFlux"}),
		"asking must not build the index — an index that adjudicated nothing explains nothing")

	_, outcome := lz.resolve(context.Background(), []string{"quantumFlux"}, nil)
	require.Equal(t, tier4NoMatch, outcome, "the verdict is unchanged by the explanation")

	assert.True(t, lz.namedInDocs([]string{"quantumFlux"}),
		"the subject IS in the tree, in a file classified as prose by its extension")
	assert.False(t, lz.namedInDocs([]string{"ruby_only_helper"}),
		"a construct named in the changelog AND declared in source is not doc-shielded: "+
			"the doc mention explains nothing, because the source one already keeps it out of no-match")
	assert.False(t, lz.namedInDocs([]string{"NotAnywhereInThisTree"}),
		"a genuinely absent construct has no doc-shield excuse")
}

// TestSymbolIndex_NamedInDocsCoversMDXProse pins the presentInDocs half of the
// .mdx split — the one extension the split was built for. An MDX file's PROSE
// tokens feed presentInDocs only, so a subject named in an MDX paragraph and
// nowhere in source is doc-shielded exactly like a changelog mention; its export
// lines feed BOTH maps, so a declared construct is not. Restructuring build's
// else-branch (mdx prose skipped from presentInDocs) leaves the first assertion
// red while the rest of the package stays green — and silently charges the
// reviewer's denominator instead.
func TestSymbolIndex_NamedInDocsCoversMDXProse(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "guide.mdx"),
		[]byte("export function Callout() { return null }\n\nThe `quantumFlux` helper was removed in 2.0.\n"), 0o644))
	writeTracked(t, root, "internal/net/pool.go")

	var calls int32
	lz := newLazySymbolIndex(root, []string{"docs/guide.mdx", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	_, outcome := lz.resolve(context.Background(), []string{"quantumFlux"}, nil)
	require.Equal(t, tier4NoMatch, outcome, "an MDX-prose-only subject is searched for and not found in source")

	assert.True(t, lz.namedInDocs([]string{"quantumFlux"}),
		"an MDX paragraph is prose: the subject is named in docs and nowhere in source")
	assert.False(t, lz.namedInDocs([]string{"Callout"}),
		"Callout is declared on an export line, so it is in BOTH maps — not doc-only, no shield")
}

// TestSymbolIndex_StateUnavailableWhenEveryParserFailed pins state() against the
// metric it must agree with.
//
// When every parser fails to load, the files are still READ (readFiles > 0) and
// `complete` stays true, so the index is built with an empty byName: locate() can
// never resolve and Tier 4's resolution half is dead. state() nonetheless reported
// "applied" while tier4UnavailableMetric was incremented for the same run — one
// surface calling it healthy, the other calling it degraded.
//
// The companion assertion below is why the condition is a CONJUNCTION rather than
// the disjunction "byName empty OR a parser failed": a tree with no
// parser-language file at all also has an empty byName, and it is healthy — the
// raw-token scan searched it and the no-match verdict is available.
func TestSymbolIndex_StateUnavailableWhenEveryParserFailed(t *testing.T) {
	t.Run("every parser failed", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "net"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "net", "pool.go"),
			[]byte("package net\n\nfunc DialPeer(addr string) error { return nil }\n"), 0o644))

		lz := newLazySymbolIndex(root, []string{"internal/net/pool.go"})
		lz.newParser = func(string) (astgroup.Parser, error) { return nil, errors.New("wasm load failed") }

		_, _ = lz.resolve(context.Background(), []string{"DialPeer"}, nil)
		assert.Equal(t, reclib.UnresolvedStateUnavailable, lz.state(),
			"an index that can resolve nothing must not report itself as applied")
	})

	t.Run("no parser-language file in the tree is still applied", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "infra"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "infra", "main.tf"),
			[]byte("resource \"aws_s3_bucket\" \"artifactStore\" {}\n"), 0o644))

		lz := newLazySymbolIndex(root, []string{"infra/main.tf"})
		_, _ = lz.resolve(context.Background(), []string{"artifactStore"}, nil)
		assert.Equal(t, reclib.UnresolvedStateApplied, lz.state(),
			"a parser-less tree was searched in full; the check was in force")
	})
}

// TestReadIndexSource_BeyondTheSniffWindow covers the multi-chunk path: the
// sniff-first read returns the head alone for a small file, so a file LARGER than
// binarySniffBytes is the only thing that exercises the second read, the cap race
// guard, and the concatenation.
func TestReadIndexSource_BeyondTheSniffWindow(t *testing.T) {
	t.Run("identifier past the sniff window is still harvested", func(t *testing.T) {
		root := t.TempDir()
		body := append(make([]byte, 0, binarySniffBytes*2),
			[]byte("package big\n\n// "+strings.Repeat("padding ", binarySniffBytes/8)+"\n")...)
		body = append(body, []byte("\nfunc tailOfTheFileSymbol() {}\n")...)
		require.Greater(t, len(body), binarySniffBytes, "the fixture must exceed the sniff window")
		p := filepath.Join(root, "big.go")
		require.NoError(t, os.WriteFile(p, body, 0o644))

		src, skip, err := readIndexSource(p)
		require.NoError(t, err)
		require.False(t, skip)
		assert.Equal(t, len(body), len(src), "the whole file must be returned, not just the sniff window")
		assert.Contains(t, string(src), "tailOfTheFileSymbol")
	})

	t.Run("a file that grew past the cap after the caller's stat is skipped", func(t *testing.T) {
		root := t.TempDir()
		p := filepath.Join(root, "grown.txt")
		require.NoError(t, os.WriteFile(p, make([]byte, maxSourceFileBytes+64), 0o644))
		// NUL-free so the binary sniff does not catch it first.
		big := make([]byte, maxSourceFileBytes+64)
		for i := range big {
			big[i] = 'a'
		}
		require.NoError(t, os.WriteFile(p, big, 0o644))

		// Called directly, bypassing the caller's Lstat cap check — which is
		// exactly the race the LimitReader bound exists to catch.
		src, skip, err := readIndexSource(p)
		require.NoError(t, err)
		assert.True(t, skip, "an over-cap file must be refused here too, not read whole")
		assert.Nil(t, src)
	})

	t.Run("a read failure after a successful open is a hole, not a skip", func(t *testing.T) {
		// os.Open on a directory succeeds on Unix; the read then fails. Production
		// never reaches this (build's Lstat skips non-regular entries first), so
		// this is the only way to pin that a read error propagates as an error
		// rather than being swallowed as a binary skip.
		_, skip, err := readIndexSource(t.TempDir())
		require.Error(t, err)
		assert.False(t, skip, "a read failure must not masquerade as a binary skip")
	})
}

// TestCollectExportedIdentifiers_ProseCannotLicenseTokens pins the three prose
// paths that defeat the narrowness collectExportedIdentifiers claims — every
// admitted token lands in presentInSource, which the primaryMatched gate reads,
// so prose must not reach it:
//
//  1. a 4-space-indented Markdown code block is not protected by the fence
//     check at all (TrimLeft strips the indentation before the fence test);
//  2. an ordinary English sentence beginning with the lowercase verb "export"
//     contributes every identifier-shaped token on the line;
//  3. a ~~~ line inside a ```-opened fence flips the inFence bool and the
//     remainder of that real code block is harvested.
//
// Real ESM export forms must keep flowing through.
func TestCollectExportedIdentifiers_ProseCannotLicenseTokens(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantAbsent  []string
		wantPresent []string
	}{
		{
			name:       "indented 4-space shell sample is a code block, not a declaration",
			src:        "Set the cap first.\n\n    export ATCR_TIER4_INDEX_MAX_FILES=5000\n",
			wantAbsent: []string{"ATCR_TIER4_INDEX_MAX_FILES"},
		},
		{
			name:       "english sentence opening with the verb export",
			src:        "Before shipping, export your apiKey and call the remoteEndpoint once.\n",
			wantAbsent: []string{"apiKey", "remoteEndpoint"},
		},
		{
			name:       "tilde line inside a backtick fence does not close it",
			src:        "```\n~~~\nexport const fencedInner = 1\n~~~\nexport const stillFenced = 2\n```\n",
			wantAbsent: []string{"fencedInner", "stillFenced"},
		},
		{
			// Path 2's sibling, and the one the ESM-form check actually answers.
			// The existing "Before shipping, export ..." case never reaches
			// isESMExportBody at all — its line does not START with `export`, so
			// the HasPrefix test rejects it first. Only a sentence whose FIRST word
			// is the verb exercises the reject arm the godoc credits to this test.
			name:       "english sentence whose first word is the verb export",
			src:        "export your apiKey before running the seedScript\n",
			wantAbsent: []string{"apiKey", "seedScript"},
		},
		{
			name:        "column-0 ESM export declares",
			src:         "export function Callout() { return null }\n",
			wantPresent: []string{"Callout"},
		},
		{
			name:        "export up to 3 leading spaces (list item) still declares",
			src:         "   export const listItemExport = 1\n",
			wantPresent: []string{"listItemExport"},
		},
		{
			name:        "export brace re-export declares",
			src:         "export {NamedReExport} from './x'\n",
			wantPresent: []string{"NamedReExport"},
		},
		{
			// The .mdx fixture the TD row asks for: an MDX page's TypeScript
			// declarations must reach presentInSource, or a finding anchored on
			// one becomes eligible for a doc_shield carve-out the page's own
			// declaration disproves.
			name: "mdx TypeScript declarations declare",
			src: "# Guide\n\n" +
				"export type CalloutKind = 'note' | 'warn'\n" +
				"export interface CalloutProps {}\n" +
				"export enum CalloutTone {}\n" +
				"export declare const calloutDefault: CalloutProps\n" +
				"export abstract class CalloutBase {}\n",
			wantPresent: []string{"CalloutKind", "CalloutProps", "CalloutTone", "calloutDefault", "CalloutBase"},
		},
		{
			name:       "prose beginning with one of the TypeScript keywords declares nothing",
			src:        "export types of finding are documented at reportEndpoint\n",
			wantAbsent: []string{"reportEndpoint"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := make(map[string]uint8)
			collectExportedIdentifiers([]byte(c.src), out)
			for _, tok := range c.wantAbsent {
				assert.Zero(t, out[tok]&presenceSource, "%s reached the source side of present from prose or a sample — it can license a confident PathSuggestion", tok)
			}
			for _, tok := range c.wantPresent {
				assert.NotZero(t, out[tok]&presenceSource, "%s is a real declaration and must carry the source bit", tok)
			}
		})
	}
}

// TestIsESMExportBody_RejectPaths pins the two arms of isESMExportBody that
// answer "no", tested directly because neither is observable through
// collectExportedIdentifiers.
//
// The empty-body guard is the clearer case: an empty body produces no tokens
// whether it is admitted or rejected, so an out-map assertion cannot tell the two
// apart. What the guard actually buys is that `body[0]` on the next line does not
// panic on a line that is nothing but `export` and trailing whitespace. Asserting
// the false directly catches both mutations — flipping the return, and deleting
// the guard (which panics).
//
// The trailing return is the fall-through every non-ESM body reaches. It is the
// arm that stops an English sentence's tokens from landing in presentInSource,
// where a token can LICENSE a confident PathSuggestion rather than merely
// withhold a routing.
func TestIsESMExportBody_RejectPaths(t *testing.T) {
	t.Run("empty body is not an export form", func(t *testing.T) {
		assert.False(t, isESMExportBody(nil), "a nil body declares nothing")
		assert.False(t, isESMExportBody([]byte("")), "`export` followed by only whitespace declares nothing")
	})

	t.Run("prose body falls through to the reject", func(t *testing.T) {
		for _, body := range []string{
			"your apiKey before running",
			"the seedScript once",
			"exports.foo = 1",
			"defaults to five",   // a keyword PREFIX is not the keyword
			"classicMode = true", // ditto
		} {
			assert.False(t, isESMExportBody([]byte(body)), "%q is prose, not an ESM export form", body)
		}
	})

	t.Run("real ESM forms are still admitted", func(t *testing.T) {
		for _, body := range []string{"{Named} from './x'", "* from './x'", "const a = 1", "default fn", "function f()", "class C {}", "async function f()", "let a", "var a"} {
			assert.True(t, isESMExportBody([]byte(body)), "%q is a real ESM export form", body)
		}
	})

	// The surface this guard actually runs over is MDX, and MDX carries
	// TypeScript. `export type Foo = ...` at column 0 of an .mdx file IS a
	// declaration, so a keyword list built from the JavaScript forms alone leaves
	// Foo out of presentInSource — and a finding anchored on Foo then becomes
	// eligible for the doc_shield carve-out it should not get, because the doc
	// genuinely declares it.
	t.Run("TypeScript declaration forms are ESM export forms too", func(t *testing.T) {
		for _, body := range []string{
			"type Foo = string",
			"interface Bar {}",
			"enum Color {}",
			"declare const x: number",
			"abstract class Base {}",
		} {
			assert.True(t, isESMExportBody([]byte(body)), "%q is a TypeScript export declaration", body)
		}
	})

	// Each new keyword is also a real English word, so the prefix rule that
	// protects `defaults`/`classicMode` has to protect these too.
	t.Run("prose that merely starts with a new keyword is still rejected", func(t *testing.T) {
		for _, body := range []string{
			"types of findings are listed below",
			"interfaces you should implement",
			"enumerate the results first",
			"declares nothing by itself",
			"abstractions leak here",
		} {
			assert.False(t, isESMExportBody([]byte(body)), "%q is prose, not an export declaration", body)
		}
	})

	// The prefix rule above only protects prose whose FIRST word merely STARTS
	// with a keyword (`types`, `declares`). It does nothing for prose whose first
	// word IS the keyword, because the boundary test admits a keyword followed by
	// a space — and every one of these words is an ordinary English word that is
	// normally followed by a space. `export type definitions for MyWidget are
	// generated at build time` is a sentence a real .mdx page can contain, and it
	// put MyWidget into presentInSource, where a fabricated anchor on MyWidget
	// reads as "named in the source" (symbolindex.go tier4Inconclusive) and
	// licenses a confident PathSuggestion via primaryMatched.
	//
	// A declaration is not "keyword + words". It is keyword + a NAME, then the
	// grammar's own punctuation: `= < ( { : ; ,`, end of line, or an
	// `extends`/`implements` clause. Prose almost never reaches that shape.
	t.Run("keyword followed by an English sentence is not a declaration", func(t *testing.T) {
		for _, body := range []string{
			"type definitions for MyWidget are generated at build time",
			"interface changes affect PublicThing downstream",
			"declare your intent before touching SecretHelper",
			"abstract concepts like QuantumFlux are not real",
			"const values are frozen once exported",
			"class names should be PascalCase in MyModule",
			"function calls are cheap in HotPath",
			"enum members must be unique across ColorSet",
			"let us assume RetryPolicy is configured",
			"var names are legacy in OldModule",
			"namespace collisions are common in BigApp",
			"module boundaries are enforced by BuildTool",
			"global state is shared across MyWorker",
		} {
			assert.False(t, isESMExportBody([]byte(body)),
				"%q is an English sentence, not a declaration — admitting it lets doc prose license a source presence", body)
		}
	})

	// The tightening must not cost a single real form. These are the shapes the
	// keyword+NAME+punctuation rule has to keep admitting, including the ones
	// whose name is NOT the next token (`declare`/`abstract` are modifiers) and
	// the ones with no punctuation at all (`let a`).
	t.Run("every real declaration shape survives the tightening", func(t *testing.T) {
		for _, body := range []string{
			"const a = 1",
			"let a",
			"var a;",
			"function f()",
			"function f<T>(x: T)",
			"function* gen()",
			"async function f()",
			"class C {}",
			"class C extends Base {}",
			"class C implements Iface {}",
			"type T = string",
			"type T<A> = A[]",
			"interface I {}",
			"interface I extends J {}",
			"enum Color {}",
			"declare const x: number",
			"declare function f(): void",
			"abstract class Base {}",
			"const { a, b } = obj",
			"const [a, b] = arr",
			// TypeScript forms the modifier recursion must not lose. `const enum`
			// is one keyword pair, not `const` binding a name `enum`; and the old
			// flat list took `declare namespace` on the strength of `declare`
			// alone, so recursing only preserves it if the inner form is known.
			"const enum Direction {}",
			"declare namespace N {}",
			"declare module 'x' {}",
			"declare global {}",
			"namespace N {}",
			"type { Foo } from './x'",
			"default fn",
			"{Named} from './x'",
			"* from './x'",
		} {
			assert.True(t, isESMExportBody([]byte(body)), "%q is a real export declaration", body)
		}
	})

	// A declared name is not required to be ASCII. JavaScript and TypeScript both
	// take the full Unicode identifier set, and an .mdx page written for a
	// non-English audience uses it — `export const café`, `export let 日本語`.
	//
	// These are the shapes the grammar rule silently dropped, and the drop runs in
	// the DANGEROUS direction: a real declaration missing from presentInSource
	// leaves a finding anchored on that name eligible for the doc_shield carve-out
	// its own declaration disproves (namedInDocs, symbolindex.go), so the finding
	// is routed out of findings.json, out of the gate exit code, and out of
	// report.md — and trust.go still charges it to the reviewer.
	//
	// The name scan must therefore run PAST a non-ASCII byte and let the
	// punctuation test decide, which is what isDeclNameByte's docstring always
	// claimed and what this subtest pins.
	t.Run("a non-ASCII declared name is still a declaration", func(t *testing.T) {
		for _, body := range []string{
			"const café = 1",
			"let 日本語 = 1",
			"var Ünicode;",
			"function ünwrap()",
			"class Ωmega {}",
			"type Ünion = string",
			"interface Ílink extends J {}",
			"enum Café {}",
			"const café",
			// The name is only PARTLY non-ASCII — the scan must not stop at the
			// first multi-byte rune and read the ASCII remainder as a second word.
			"const caféMenu = () => 1",
			"const 日本Language = 1",
		} {
			assert.True(t, isESMExportBody([]byte(body)),
				"%q declares a name — dropping it hides a real symbol from presentInSource", body)
		}
	})

	// declaresName's "no name at all" reject path, pinned by mutation rather than
	// by coverage alone. It and isDeclNameRune's leading-digit clause were the ONLY
	// added lines of the grammar rewrite that no test executed, and BOTH survived
	// deletion with the whole package green — `if n == 0 { return true }` and
	// `return !first` → `return true` each left `go test ./internal/reconcile/`
	// passing.
	//
	// They are not decoration. Between them they are the entire defence against
	// prose whose second token is typographic punctuation or a digit, and every
	// line below is a sentence an .mdx page can genuinely carry. Admitting one
	// harvests EVERY token on it into presentInSource, where — per
	// collectExportedIdentifiers and exportDeclaringExts — a prose token can
	// license a confident PathSuggestion that nothing downstream can undo.
	//
	// Each case is chosen so exactly one mutant flips it:
	//   n == 0 → true      : the first group (no name is ever scanned)
	//   !first → true      : the second group (a digit becomes a legal first rune,
	//                        and the punctuator right after it then says "yes")
	t.Run("a keyword followed by punctuation declares no name", func(t *testing.T) {
		for _, body := range []string{
			"type — see the guide",        // em-dash
			"const – note the dash",       // en-dash
			"class “Widget” is styled",    // curly double quotes
			"interface … described below", // ellipsis
			"type ‘Foo’ is exported",      // curly single quotes
			"function ✓ verified",         // symbol
			"type (see the appendix) is described",
			"enum = the enumeration concept",
			"namespace / module boundaries",
		} {
			assert.False(t, isESMExportBody([]byte(body)),
				"%q names nothing — admitting it lets prose license a source presence", body)
		}
	})

	t.Run("a digit cannot start a declared name", func(t *testing.T) {
		// The punctuator AFTER the digit is what makes these discriminating: with
		// the leading-digit clause relaxed, `2` scans as the name and the `=` then
		// satisfies the grammar. Prose whose digit is followed by a bare word is
		// rejected either way and would prove nothing.
		for _, body := range []string{
			"type 2 = deprecated",
			"enum 2 = the second kind",
			"const 3 = three",
			"interface 4 : the fourth",
		} {
			assert.False(t, isESMExportBody([]byte(body)),
				"%q starts with a digit, so it declares no name", body)
		}
		// The other half of the same clause: a digit that is NOT first is a
		// perfectly ordinary name rune, and rejecting it would lose real symbols.
		for _, body := range []string{
			"const a2 = 1",
			"let x1",
			"class Base64 {}",
			"type UTF8 = string",
		} {
			assert.True(t, isESMExportBody([]byte(body)),
				"%q declares a name containing a digit", body)
		}
	})

	// `namespace`, `module` and `global` were added to the binding list to
	// recover `declare namespace N {}` and friends, which the recursion would
	// otherwise have lost. Adding them also handed ordinary prose two ways
	// through declaresName that the old flat list never had, because all three
	// are common English nouns that a sentence follows with a colon or a quote:
	//
	//	export module 'quantumFlux' is discussed below
	//	export global state: shared across MyWorker
	//	export namespace collisions: see QuantumFlux
	//
	// This is the over-admission direction, which is the dangerous one. A prose
	// token in presentInSource unlocks resolve's primaryMatched gate, and the
	// secondary branch then localizes on a FIX anchor and returns tier4Resolved —
	// so validate.go stamps a confident PathSuggestion pointing at a file that has
	// nothing to do with the finding's (nonexistent) subject. Nothing downstream
	// can undo it; see the docstrings on collectExportedIdentifiers and
	// exportDeclaringExts, which say exactly this.
	//
	// Two rules close it, and each is pinned in both directions below:
	//   - a colon after the name is a TYPE ANNOTATION, which none of the three
	//     can carry, so it is prose punctuation for them and a declaration
	//     punctuator for everything else
	//   - a quoted name is an AMBIENT MODULE name, which only `module` takes —
	//     and it still has to be followed by the grammar's own punctuation, since
	//     `'react' {}` is a declaration and `'quantumFlux' is discussed` is not
	t.Run("namespace module and global do not admit prose", func(t *testing.T) {
		for _, body := range []string{
			"module 'quantumFlux' is discussed below",
			"module \"someName\" is described here",
			"global state: shared across MyWorker",
			"namespace collisions: see QuantumFlux",
			"namespace 'quoted' is not a module",
			"global 'state' is shared",
			// An unterminated quote is not a declaration either.
			"module 'unterminated {}",
			// The adversarial sweep found the same colon hole at five keywords
			// the finding did not name — a predicate fixed only where it was
			// pointed. None of these can carry a `name:` type annotation either:
			// a type alias is `type T = X`, and an interface, class, enum or
			// function puts `{` or `(` where the colon is.
			"type definitions: see the guide",
			"interface changes: see below",
			"enum members: Red, Green, Blue",
			"class hierarchy: see the diagram",
			"function signatures: documented below",
		} {
			assert.False(t, isESMExportBody([]byte(body)),
				"%q is prose — admitting it lets a doc sentence license a confident PathSuggestion", body)
		}
	})

	t.Run("the real namespace module and global forms still pass", func(t *testing.T) {
		for _, body := range []string{
			"namespace N {}",
			"module M {}",
			"global {}",
			"declare namespace N {}",
			"declare module 'x' {}",
			"declare module \"y\" {}",
			"declare global {}",
			// The colon rule must stay OPEN for every keyword that really does
			// take a type annotation, or tightening it costs real declarations.
			"const x: number = 1",
			"let y: string",
			"var z: boolean",
			"declare const w: T",
			// The colon closes for every OTHER keyword, so these prove the
			// tightening cost nothing: each puts `{`, `(` or `=` where the
			// annotation would have been.
			"function f(): void",
			"function f<T>(x: T): T",
			"class C { x: number }",
			"type T = { a: string }",
			"interface I { a: b }",
			"enum E { A = 1 }",
		} {
			assert.True(t, isESMExportBody([]byte(body)), "%q is a real export declaration", body)
		}
	})

	// The quoted-name arm belongs to `module` and to nothing else, and the gate
	// that says so — `shape&declQuotedName == 0` in declaresName — was an added
	// restriction that no test executed: deleting it left the whole package
	// green while nine bodies flipped from rejected to accepted.
	//
	// The quoted fixtures in the subtest above cannot pin it. `namespace
	// 'quoted' is not a module` and `global 'state' is shared` are rejected by
	// followsDeclaredName's bare-word arm whether the gate is present or not,
	// so they kill no mutant. Each body here puts a DECLARATION punctuator
	// immediately after the closing quote, which leaves the gate as the only
	// thing still saying no — and a quoted PHRASE is exactly the prose shape
	// that would otherwise harvest its whole line into presentInSource.
	t.Run("a quoted name belongs to module and to nothing else", func(t *testing.T) {
		for _, body := range []string{
			"namespace 'foo' {}",
			"type 'Foo' = string",
			"interface 'I' {}",
			"class 'C' {}",
			"global 'g' {}",
			"enum 'E' {}",
			"function 'f'()",
			"let 'x' = 1",
			"var 'y' = 2",
		} {
			assert.False(t, isESMExportBody([]byte(body)),
				"%q quotes a name after a non-module keyword — only an ambient module names a string", body)
		}
		// The other direction, or the gate would be free to reject everything:
		// the one keyword that DOES carry a quoted name must keep carrying it.
		for _, body := range []string{
			"module 'react' {}",
			"module \"react\" {}",
		} {
			assert.True(t, isESMExportBody([]byte(body)), "%q is an ambient module declaration", body)
		}
	})

	// `const enum` is ONE keyword pair, and the pair binds no variable, so it
	// takes no type annotation — `const enum E: number` is not a TypeScript
	// form. Passing declTypeAnnotation to its declaresName call instead of 0
	// also left the package green, so the 0 was untested; with the annotation
	// admitted, that sentence-shaped body reaches the `:` arm and licenses a
	// source presence.
	t.Run("const enum takes no type annotation", func(t *testing.T) {
		assert.False(t, isESMExportBody([]byte("const enum E: number")),
			"`const enum E: number` binds no variable, so the colon is prose punctuation")
		assert.True(t, isESMExportBody([]byte("const enum Direction {}")),
			"`const enum Direction {}` is the real form and must survive")
	})
}

// TestSymbolIndex_OnePresenceMap pins the memory-shape decision: the index carries
// ONE presence map keyed by token with a per-token origin flag, not parallel
// presentInSource/presentInDocs maps that each hold every identifier-shaped token
// of their half of the tree (roughly doubling index memory for the doc-shield
// explanation path). Behaviour — which verdict reads which origin — is pinned by
// the verdict tests above; this guards the structure they read.
func TestSymbolIndex_OnePresenceMap(t *testing.T) {
	tp := reflect.TypeOf(symbolIndex{})
	presenceMaps := 0
	for i := 0; i < tp.NumField(); i++ {
		f := tp.Field(i)
		if f.Type.Kind() == reflect.Map && f.Name != "byName" {
			presenceMaps++
		}
	}
	assert.Equal(t, 1, presenceMaps,
		"symbolIndex must carry exactly one presence map (origin-tagged), not one per source class")
	assert.NotNil(t, tp, "symbolIndex must exist")
}

// TestSymbolIndex_NamedInDocsRequiresEveryAnchorAccounted pins the quantifier the
// doc-shield carve-out rests on: EVERY anchor must be accounted for in the tree
// before the shield may explain the routing.
//
// The shield exempts a finding from the scorecard's FindingsRaised denominator
// (scorecard.go, the UnresolvedReasonDocShield carve-out). Answering on the FIRST
// doc-only anchor let a finding that also names genuinely invented constructs buy
// its whole exemption with one token that happened to survive in a changelog —
// the normal fate of a symbol a release removed. The record it produced was
// NEUTRAL rather than rate-depressing, so it still counted as a run toward the
// trust floor on a thinner base of real measurements.
//
// The doc token set is built with the LOOSE filter (isIdentifierShaped over raw
// prose bytes) while anchors come from the STRICT one, so an ordinary English
// word in any tracked .md can be that one token. Requiring every anchor closes
// the gap without touching either filter.
func TestSymbolIndex_NamedInDocsRequiresEveryAnchorAccounted(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"),
		[]byte("## 2.0.0\n\n- Removed `quantumFlux`, the retry handle helper.\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "net"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "net", "pool.go"),
		[]byte("package net\n\nfunc DialPeer() error { return nil }\n"), 0o644))

	var calls int32
	lz := newLazySymbolIndex(root, []string{"CHANGELOG.md", "internal/net/pool.go"})
	lz.newParser = newStubFactory(fileTree{"pool.go": file(fn("DialPeer", 7))}, &calls)

	_, outcome := lz.resolve(context.Background(), []string{"quantumFlux", "phantomWidget"}, nil)
	require.Equal(t, tier4NoMatch, outcome, "neither anchor is declared in source")

	assert.True(t, lz.namedInDocs([]string{"quantumFlux"}),
		"the single-anchor doc-only case is unchanged")
	assert.False(t, lz.namedInDocs([]string{"quantumFlux", "phantomWidget"}),
		"phantomWidget is in neither set — it was invented, so the doc mention of its "+
			"co-anchor explains only part of the finding and must not shield the whole of it")
	assert.False(t, lz.namedInDocs([]string{"phantomWidget", "quantumFlux"}),
		"the verdict must not depend on which anchor is examined first")
	assert.True(t, lz.namedInDocs([]string{"quantumFlux", "DialPeer"}),
		"a co-anchor declared in SOURCE was not invented, so it does not defeat the shield")
}
