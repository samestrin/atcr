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
// files is a perfectly searchable tree. Its raw tokens feed `present`, so the
// index is available (no unavailable counter), a construct named in it is real
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

// TestSymbolIndex_UnparsedLanguageStillFeedsPresent pins the polyglot hole:
// eligiblePaths used to drop every tracked file whose extension has no parser
// language BEFORE the index was built, so Ruby, Swift, Scala, Elixir, SQL,
// Terraform, YAML and Markdown were structurally invisible to `present` while
// `complete` stayed true. A finding whose construct genuinely lives in one of
// those files, but whose citation names a non-existent parser-language path, then
// reached tier4NoMatch and was routed out of findings.json as fabricated.
//
// Every tracked file now feeds the raw-token scan; only byName keeps the parser
// filter.
func TestSymbolIndex_UnparsedLanguageStillFeedsPresent(t *testing.T) {
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
// `present` with tokens that no reviewer ever named. Skipping one is NOT a hole
// either — no source construct is declared in a binary — so `complete` stays
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
		"a binary's byte noise must not enter `present`, and skipping it must not withhold the verdict")

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
// is token-scanned, which over-collects into `present` and can only withhold a
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
