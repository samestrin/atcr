package reconcile

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/samestrin/atcr/internal/metrics"
	reclib "github.com/samestrin/atcr/reconcile"
)

// tier4UnavailableMetric counts reconcile runs where a Tier 4 index was needed
// but could not be built — over the file cap, or no parser would load.
//
// A slog.Warn alone is not enough here, for the same reason stream.BuildFileIndex
// counts its own git failure: when Tier 4 is silently off, every lookup reports
// "could not check", nothing is ever routed to the sidecar, and the run looks
// EXACTLY like a healthy one where no finding was fabricated. The counter is the
// only durable signal separating those two.
const tier4UnavailableMetric = "atcr_tier4_index_unavailable_total"

// tier4IncompleteMetric counts runs where the index was built but some eligible
// tracked file could not be read, so every no-match verdict was withheld. It is
// distinct from tier4UnavailableMetric because the causes and the fixes differ:
// unavailable means "no index at all" (cap, no parser), incomplete means "a hole
// in an otherwise working index" (a deleted-but-tracked file, a permission
// problem, a symlink pointing out of the repo).
const tier4IncompleteMetric = "atcr_tier4_index_incomplete_total"

// tier4Outcome is the verdict of a Tier 4 symbol lookup (Epic 35.16.6.5 T3).
// The three values are NOT interchangeable, and the distinction between the
// first two is the whole safety property of this epic: only tier4NoMatch is
// evidence that a finding may be fabricated.
type tier4Outcome int

const (
	// tier4NoMatch means the index WAS built and searched, the finding DID
	// supply anchors, and none of them is declared anywhere in the tracked tree.
	// This is the only outcome that makes a finding sidecar-eligible.
	tier4NoMatch tier4Outcome = iota
	// tier4Inconclusive means no judgment could be reached: the finding supplied
	// no anchors, the index could not be built (no parser, over the file cap), or
	// the anchors matched code in more than one file (AC7). The finding stays in
	// the primary stream with no suggestion. "Could not check" and "checked and
	// found nothing" are deliberately different answers — a wrong auto-drop is
	// worse than a flagged-but-wrong finding (the 5.0/5.4 preserve-never-discard
	// rule this epic inherits).
	tier4Inconclusive
	// tier4Resolved means the anchors localize to exactly one tracked file, which
	// is returned as the PathSuggestion candidate.
	tier4Resolved
)

// maxSymbolIndexFiles caps how many tracked files a Tier 4 index build will
// READ. The parse set is a subset of that — only files with a parser language
// are parsed — but the cap is measured on the read set, because reading and
// token-scanning is what the whole tracked tree now pays. Nothing else in this
// codebase touches the whole repository — astgroup.Grouper parses only the files
// findings actually cite, lazily, one at a time — so a repo-wide sweep is a
// genuinely new cost and is bounded here.
//
// Because the cap counts every contained tracked file rather than only the
// parser-language ones, a tree near the limit can trip it on its docs and
// fixtures. That direction is safe (Tier 4 goes fully inconclusive, nothing is
// routed), it is counted in tier4UnavailableMetric rather than silent, and
// tier4IndexMaxFilesEnv retunes it without a rebuild.
//
// Exceeding the cap DISABLES Tier 4 for the run (every lookup reports
// tier4Inconclusive) rather than indexing a prefix. A half-built index would
// silently mean "checked and found nothing" for every symbol living in the
// unindexed remainder, which is exactly the false-fabrication verdict the
// sidecar must never be reached on.
const maxSymbolIndexFiles = 5000

// tier4IndexMaxFilesEnv overrides maxSymbolIndexFiles for an operator whose
// tracked tree exceeds the default cap but who still wants Tier 4 — the only
// other escape today is ATCR_DISABLE_AST_GROUPING, which also turns off AST
// clustering. Parsed like astGroupingDisabled: an unparseable or non-positive
// value falls back to the default, because a silently-zero cap would disable
// Tier 4 with no signal distinguishable from an intentional opt-out.
const tier4IndexMaxFilesEnv = "ATCR_TIER4_INDEX_MAX_FILES"

// symbolIndexFileCap returns the effective Tier 4 index file cap: the
// tier4IndexMaxFilesEnv override when it parses to a positive integer, else
// maxSymbolIndexFiles.
func symbolIndexFileCap() int {
	if n, err := strconv.Atoi(os.Getenv(tier4IndexMaxFilesEnv)); err == nil && n > 0 {
		return n
	}
	return maxSymbolIndexFiles
}

// symbolIndex maps a declared identifier to the DISTINCT tracked files that
// declare it. Only the file set matters: PathSuggestion is a path (the 5.4
// contract this epic extends), and the declaring line is not part of it.
//
// Each entry's file list is sorted and deduped at build time, so lookups are
// order-stable without re-sorting.
type symbolIndex struct {
	byName map[string][]string
	// present holds every identifier-shaped token seen in the RAW SOURCE TEXT of
	// the indexed files, not just the ones the parser named. It exists solely to
	// make the no-match verdict safe.
	//
	// The declaration index is only as complete as the parser's naming rules, and
	// those vary sharply by language. The embedded go.wasm parser, for instance,
	// names *ast.FuncDecl and nothing else — so every Go type, interface, const
	// and var is absent from byName while being plainly present in the tree. Left
	// unguarded, a perfectly real finding ("`FileIndex` is not concurrency-safe")
	// resolves to "declared nowhere" and is routed out of the report as
	// fabricated. That failure is Go-specific, silent, and would fire on atcr's
	// own reviews.
	//
	// So the two directions are held to different standards: a RESOLUTION needs a
	// byName hit (a declaration site is what PathSuggestion points at), while a
	// NO-MATCH additionally requires the anchor to be absent from present — absent
	// from the source text entirely, by any parser's reckoning.
	present map[string]struct{}
	// complete reports that every eligible tracked file was actually read. When
	// false, some region of the tree was never searched, so "not found" is
	// unproven and no-match downgrades to inconclusive — the same reasoning the
	// file-cap branch applies, reached through a different door.
	complete bool
}

// resolve applies the Tier 4 decision procedure to one finding's anchors.
//
// Each anchor is scored independently and the PRECISE ones win: an anchor
// declared in exactly one file localizes the finding, while an anchor declared
// in many (Close, Run, New) is too common to localize and is ignored as long as
// some other anchor is precise. Only if no anchor is precise does the outcome
// fall back to the coarse signal — matched-somewhere is inconclusive, matched
// nowhere is a no-match.
//
// Disagreement between two precise anchors is inconclusive, not a coin flip: a
// wrong Tier 4 guess that suggests the wrong file is worse than no suggestion
// (the suggest-never-auto-correct constraint inherited from 5.4).
// resolve applies the Tier 4 decision procedure to one finding's anchors.
//
// primary holds the PROBLEM anchors and secondary the FIX anchors. Both may
// produce a resolution, but ONLY primary may produce a no-match: a FIX names the
// construct the reviewer wants created, so its absence from the tree is expected
// rather than incriminating. primary is consulted first so a resolution is
// attributed to the finding's subject rather than to whichever anchor happens to
// be unique — a FIX-named collaborator must not out-rank the thing the PROBLEM
// is actually about. For the same reason a secondary resolution is accepted
// only when at least one primary anchor matched something in the tree: a FIX
// naming an existing collaborator must not localize a finding whose actual
// subject is absent.
//
// Within a tier, PRECISE anchors win: an anchor declared in exactly one file
// localizes the finding, while an anchor declared in many (Close, Run, New) is
// too common to localize and is ignored as long as some other anchor is precise.
// Disagreement between two precise anchors is inconclusive, not a coin flip: a
// wrong Tier 4 guess that suggests the wrong file is worse than no suggestion
// (the suggest-never-auto-correct constraint inherited from 5.4).
func (x *symbolIndex) resolve(primary, secondary []string) (string, tier4Outcome) {
	if x == nil {
		return "", tier4Inconclusive // index unavailable: could not check
	}
	if file, ok := x.locate(primary); ok {
		return file, tier4Resolved
	}
	// The secondary set may only LOCALIZE, never substitute for the subject:
	// with no primary anchor present anywhere in the tree, a FIX-derived hit
	// would render "did you mean X?" for a finding whose subject is fabricated
	// — the exact verdict inversion the no-match direction exists to catch.
	primaryMatched := false
	for _, a := range primary {
		if _, seen := x.present[a]; seen || len(x.byName[a]) > 0 {
			primaryMatched = true
			break
		}
	}
	if primaryMatched {
		if file, ok := x.locate(secondary); ok {
			return file, tier4Resolved
		}
	}
	if len(primary) == 0 {
		// The PROBLEM named no construct, so nothing was ever searched for on the
		// only evidence that counts. A FIX-only anchor set cannot stand in.
		return "", tier4Inconclusive
	}
	if !x.complete {
		return "", tier4Inconclusive // the search had holes: "not found" is unproven
	}
	for _, a := range primary {
		if _, seen := x.present[a]; seen {
			// Named somewhere in the source text even though the parser did not
			// declare it (a Go type, a const, a struct field). Real code, just not
			// localizable — never a phantom.
			return "", tier4Inconclusive
		}
		if len(x.byName[a]) > 0 {
			return "", tier4Inconclusive // declared, just not in one place (AC7)
		}
	}
	return "", tier4NoMatch // searched the whole tree, found nothing: sidecar-eligible
}

// locate returns the single file declaring one of anchors, if exactly one such
// file is agreed on by every precise anchor in the set.
func (x *symbolIndex) locate(anchors []string) (string, bool) {
	precise := ""
	for _, a := range anchors {
		files := x.byName[a]
		if len(files) != 1 {
			continue // absent, or too common to localize
		}
		switch {
		case precise == "":
			precise = files[0]
		case precise != files[0]:
			return "", false // precise anchors disagree (AC7)
		}
	}
	return precise, precise != ""
}

// parserFactory obtains a parser for a language id. It is the seam that lets
// index-build behavior be tested without standing up the wazero runtime; the
// production value is astgroup.SharedHost().Parser, so the index reuses the
// process-lifetime host rather than instantiating a second one (T2).
type parserFactory func(lang string) (astgroup.Parser, error)

// lazySymbolIndex builds a symbolIndex at most once, on the first resolve.
//
// The laziness is AC5 and mirrors the lazyGrouper discipline in astgrouping.go:
// a reconcile run whose findings all cite real files has no Tier-4-eligible
// finding, never calls resolve, and therefore never reads a source file or
// touches the wazero runtime.
type lazySymbolIndex struct {
	root  string
	paths []string

	// newParser is overridable for tests; nil means "use the shared host".
	newParser parserFactory

	once sync.Once
	idx  *symbolIndex // nil after build => Tier 4 unavailable this run
}

// newLazySymbolIndex prepares (but does not build) a Tier 4 index over paths,
// which are the repo-root-relative tracked files from 5.4's candidate index
// (stream.FileIndex.Paths()). No second `git ls-files` pass is made.
func newLazySymbolIndex(root string, paths []string) *lazySymbolIndex {
	return &lazySymbolIndex{root: root, paths: paths}
}

// resolve builds the index on first call and applies the Tier 4 decision
// procedure. A build that could not run at all yields tier4Inconclusive for
// every lookup — never tier4NoMatch — so nothing is routed to the sidecar on
// the strength of an index that does not exist.
func (lz *lazySymbolIndex) resolve(ctx context.Context, primary, secondary []string) (string, tier4Outcome) {
	if lz == nil {
		return "", tier4Inconclusive
	}
	lz.once.Do(func() { lz.build(ctx) })
	return lz.idx.resolve(primary, secondary)
}

// state reports what the build actually achieved, for Summary.UnresolvedState.
//
// It is meaningful only AFTER a resolve has forced the build — before that the
// index is deliberately unbuilt (AC5) and there is nothing to report. The caller
// (validateFindingPaths) only asks once it has consulted the resolver, so the
// lazy contract is preserved: asking for the state never triggers a build.
//
// The three answers map 1:1 onto the reasons a lookup can fail to reach a
// verdict, which is exactly what makes them worth reporting separately: a nil
// index means the build could not run at all (cap, no parser, nothing readable,
// no contained file), an incomplete one means a region of the tree went
// unsearched so every no-match was withheld, and anything else means the check
// was fully in force.
func (lz *lazySymbolIndex) state() string {
	switch {
	case lz == nil || lz.idx == nil:
		return reclib.UnresolvedStateUnavailable
	case !lz.idx.complete:
		return reclib.UnresolvedStateIncomplete
	default:
		return reclib.UnresolvedStateApplied
	}
}

// build parses every eligible tracked file once and populates lz.idx, or leaves
// it nil when Tier 4 cannot run for this repo.
//
// Eligibility is "does not escape root" — nothing more. EVERY contained tracked
// file is read and token-scanned into `present`; the parser language
// (astgroup.LanguageForExt) gates only byName, the declaration half. Filtering
// eligibility by parser language instead is what once made whole languages
// invisible to the no-match verdict (see eligiblePaths).
//
// Per-file failures — an absent file, an unreadable one, an unparseable one —
// are SKIPPED, not fatal: a repo where one file fails to parse must still get
// Tier 4 for the rest. A failure to obtain the parser for a language at all is
// different in kind (every file of that language loses its declarations), and if
// it leaves the index with nothing at all the index is discarded so lookups
// report "could not check".
func (lz *lazySymbolIndex) build(ctx context.Context) {
	eligible := lz.eligiblePaths()
	if len(eligible) == 0 {
		// A tracked tree with no root-contained file disables Tier 4 exactly like
		// the cap, the missing-parser and the aborted-build paths below — count it
		// the same way or it is indistinguishable from a healthy run.
		metrics.Counter(tier4UnavailableMetric).Inc()
		return
	}
	if maxFiles := symbolIndexFileCap(); len(eligible) > maxFiles {
		slog.Warn("astgroup: tier-4 symbol index disabled, tracked file count over cap",
			"eligible", len(eligible), "cap", maxFiles)
		metrics.Counter(tier4UnavailableMetric).Inc()
		return
	}

	newParser := lz.newParser
	if newParser == nil {
		// The process-lifetime shared host, so the compiled-parser cache is the
		// same one clustering and symbol anchoring already warmed (T2).
		newParser = astgroup.SharedHost().Parser
	}

	sites := make(map[string]map[string]struct{})
	present := make(map[string]struct{})
	parsers := make(map[string]astgroup.Parser)
	parserFailed := make(map[string]bool)
	readFiles := 0
	complete := true

	for _, rel := range eligible {
		if ctx != nil && ctx.Err() != nil {
			// A cancelled or timed-out reconcile must be able to interrupt the
			// repo-wide sweep. An aborted build is an incomplete search, so it is
			// discarded outright rather than answered from.
			slog.Warn("astgroup: tier-4 symbol index aborted", "err", ctx.Err())
			metrics.Counter(tier4UnavailableMetric).Inc()
			return
		}
		// Read BEFORE parsing, and harvest the raw token set from the bytes
		// regardless of whether a parser is available or the parse succeeds. That
		// ordering is what keeps `present` complete when `byName` is not: a file
		// whose language has no working parser still proves which identifiers exist
		// in the tree, which is all the no-match verdict needs from it.
		abs, ok := containedIndexPath(lz.root, rel)
		if !ok {
			complete = false // symlink escaping root: refuse to read, and admit the hole
			continue
		}
		if fi, lerr := os.Lstat(abs); lerr == nil {
			if !fi.Mode().IsRegular() {
				// A tracked entry that is not a regular file. `git ls-files` emits a
				// git SUBMODULE as one gitlink row naming its directory, and since
				// eligiblePaths stopped filtering by parser language that row now
				// reaches this loop, where a read fails with "is a directory".
				// Admitting that as a hole below would clear `complete` and withhold
				// EVERY no-match verdict, so a single submodule silently disabled
				// Tier 4 for the whole repo. No declaration lives in a non-file, so
				// this is a resolution limit, not a search hole: `complete` stays
				// true, exactly as for the binary and over-cap skips below.
				continue
			}
			if fi.Size() > maxSourceFileBytes {
				continue // over-cap artifact: skipped, not a hole. See maxSourceFileBytes.
			}
		}
		src, skip, err := readIndexSource(abs)
		if err != nil {
			complete = false // absent or unreadable: a region of the tree went unsearched
			continue
		}
		if skip {
			// Not a hole: no declaration lives in a compiled blob. See isBinaryContent.
			continue
		}
		readFiles++
		collectSourceIdentifiers(src, present)

		lang := astgroup.LanguageForExt(strings.ToLower(path.Ext(rel)))
		if lang == "" {
			// No parser for this extension, so it contributes no DECLARATIONS. Its
			// raw tokens were harvested above, which is all the no-match verdict
			// needs from it — so this is a resolution limit, not a search hole, and
			// `complete` deliberately stays true.
			continue
		}
		if parserFailed[lang] {
			continue
		}
		p, ok := parsers[lang]
		if !ok {
			var err error
			p, err = newParser(lang)
			if err != nil || p == nil {
				// Every file of this language loses its DECLARATIONS, so Tier 4 can
				// no longer resolve anything in it. Its raw tokens were already
				// harvested above, so the no-match guard stays sound — but the lost
				// resolution capability is a real degradation and is counted, not
				// just logged to a sink that may be discarded.
				slog.Warn("astgroup: tier-4 parser unavailable, language skipped", "lang", lang, "err", err)
				metrics.Counter(tier4UnavailableMetric).Inc()
				parserFailed[lang] = true
				continue
			}
			parsers[lang] = p
		}
		tree, err := p.Parse(src)
		if err != nil {
			continue // unparseable: no declarations, but its tokens still counted above
		}
		for _, name := range astgroup.NamedSymbols(tree) {
			if sites[name] == nil {
				sites[name] = make(map[string]struct{})
			}
			sites[name][rel] = struct{}{}
		}
	}

	if readFiles == 0 {
		// Nothing was read, so "not in the tree" carries no information at all.
		// Leaving lz.idx nil makes every lookup inconclusive rather than a
		// false no-match.
		metrics.Counter(tier4UnavailableMetric).Inc()
		return
	}
	if !complete {
		metrics.Counter(tier4IncompleteMetric).Inc()
	}

	byName := make(map[string][]string, len(sites))
	for name, fileSet := range sites {
		files := make([]string, 0, len(fileSet))
		for f := range fileSet {
			files = append(files, f)
		}
		sort.Strings(files) // stable output regardless of map iteration order
		byName[name] = files
	}
	lz.idx = &symbolIndex{byName: byName, present: present, complete: complete}
}

// collectSourceIdentifiers adds every identifier-shaped token in src to out.
//
// This is a lexer-free scan on purpose: it must work for a language whose parser
// failed to load, and it must not depend on any parser's naming rules — that
// dependence is exactly what makes byName an unsafe basis for a no-match
// verdict. Over-collection is harmless and in fact desirable here: a token
// appearing in a comment or a string literal still proves the finding's subject
// is not a total phantom, and the only consequence of a false positive is a
// finding kept in the primary report.
func collectSourceIdentifiers(src []byte, out map[string]struct{}) {
	start := -1
	for i := 0; i <= len(src); i++ {
		var b byte
		if i < len(src) {
			b = src[i]
		}
		isWord := i < len(src) && (b == '_' ||
			(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9') || b >= utf8.RuneSelf)
		switch {
		case isWord && start < 0:
			start = i
		case !isWord && start >= 0:
			if tok := string(src[start:i]); isIdentifierShaped(tok) {
				out[tok] = struct{}{}
			}
			start = -1
		}
	}
}

// eligiblePaths filters the tracked set to root-contained files, preserving a
// deterministic (sorted) order so a capped or partially-failing build is
// reproducible.
//
// It deliberately does NOT filter by parser language. astgroup.LanguageForExt
// covers only go/py/ts-js/php/rust/bash/java/kotlin/c-cpp/csharp, so filtering
// here made Ruby, Swift, Scala, Elixir, SQL, Terraform, proto, YAML and Markdown
// structurally invisible to `present` while `complete` stayed true — the largest
// hole the incomplete-index downgrade could have, and the one it never saw. A
// finding whose construct genuinely lives in a .rb file, cited against a
// non-existent .go path, then reached tier4NoMatch and was routed out of
// findings.json as fabricated.
//
// The parser filter still applies, one level down in build: it gates byName
// (which needs declarations) and nothing else. Reading a file and harvesting its
// raw tokens needs no parser at all.
func (lz *lazySymbolIndex) eligiblePaths() []string {
	out := make([]string, 0, len(lz.paths))
	for _, rel := range lz.paths {
		rel = filepath.ToSlash(strings.ReplaceAll(rel, "\\", "/"))
		if rel == "" || escapesIndexRoot(rel) {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// maxSourceFileBytes caps how much of a SINGLE tracked file the index build will
// read. The whole-tree sweep reads every contained tracked file, so without this
// one checked-in artifact — a dataset, a database dump, a model weight — is
// pulled into memory in full on every build.
//
// Exceeding it SKIPS the file and is NOT a hole, for the same reason a binary
// skip is not: a reviewer does not name a source construct inside a
// multi-megabyte blob, so `complete` stays true and the no-match verdict stays
// available. maxSymbolIndexFiles bounds how many files are read; this bounds how
// large any one of them may be.
//
// KNOWN GAP, in the unsafe direction, and it is the same one isBinaryContent
// carries: a genuine SOURCE file over the cap (a generated or minified bundle)
// is skipped, so its identifiers never reach `present` and a finding about one
// of them could reach a no-match verdict. `complete` deliberately stays true —
// clearing it would let one large artifact withhold every verdict for the whole
// repo, which is the failure the non-regular-file skip above exists to prevent.
// Widen this by RAISING the cap, never by clearing `complete` here.
const maxSourceFileBytes = 4 << 20 // 4MiB

// binarySniffBytes is how much of a file is inspected for a NUL byte before the
// content is accepted as text. A NUL in the first chunk is the same heuristic
// git itself uses to classify a blob as binary.
const binarySniffBytes = 8000

// isBinaryContent reports whether src looks like a binary blob rather than
// source text.
//
// Tracked binaries must not be token-scanned. This repository alone carries
// ~31MB of embedded .wasm parser plugins, so scanning them would dominate the
// index build cost, and their byte noise would inflate `present` with tokens no
// reviewer ever named — weakening the very set the no-match verdict depends on.
//
// Skipping one is NOT a hole in the search, so it does not clear `complete`: a
// source construct is declared in source text, never in a compiled blob. That
// distinction is what keeps the no-match verdict available on a normal
// repository instead of being withheld by every checked-in image and plugin.
//
// KNOWN GAP, in the unsafe direction: a UTF-16/UTF-32 encoded SOURCE file
// carries NULs and is classified binary here, so its identifiers never reach
// `present` and a finding about one of them could reach a no-match verdict. The
// exposure is small — git itself treats such a blob as binary, and toolchains
// normalize to UTF-8 — but it is real. Widen this by DECODING those encodings,
// never by dropping the NUL test: without it the ~31MB of embedded .wasm in this
// repo alone would be scanned on every build.
func isBinaryContent(src []byte) bool {
	if len(src) > binarySniffBytes {
		src = src[:binarySniffBytes]
	}
	return bytes.IndexByte(src, 0) >= 0
}

// readIndexSource reads one tracked file for the index build, sniffing for
// binary content BEFORE the whole file is in memory.
//
// isBinaryContent only ever inspects the first binarySniffBytes, so reading the
// file whole and then sniffing paid the full cost of every blob it was about to
// discard — ~31MB of embedded .wasm in this repo alone, on every build. Reading
// the sniff window first and returning early is the same decision made for the
// same reason, minus the allocation.
//
// skip reports "this is not source text" (the binary verdict), which the caller
// treats as a resolution limit rather than a search hole; err reports a genuine
// read failure, which is a hole.
func readIndexSource(abs string) (src []byte, skip bool, err error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	head := make([]byte, binarySniffBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, false, err
	}
	head = head[:n]
	if isBinaryContent(head) {
		return nil, true, nil
	}
	if n < binarySniffBytes {
		return head, false, nil // the whole file fit in the sniff window
	}

	// Bounded by maxSourceFileBytes+1 so a file that grew past the cap between
	// the caller's Lstat and this read is still refused rather than read whole.
	rest, err := io.ReadAll(io.LimitReader(f, maxSourceFileBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(head))+int64(len(rest)) > maxSourceFileBytes {
		return nil, true, nil // raced past the cap: skip, exactly as the caller would have
	}
	return append(head, rest...), false, nil
}

// escapesIndexRoot reports whether a tracked relpath would read outside the
// repository root by its spelling alone. `git ls-files` never emits such a path,
// but the index must not become an arbitrary-file reader if it is ever handed a
// set from elsewhere — the same containment posture stream.ValidatePath and
// astgroup.Grouper.canonicalPath already take. This lexical check is the cheap
// first pass; containedIndexPath catches what spelling cannot.
func escapesIndexRoot(rel string) bool {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return true
	}
	cleaned := path.Clean(rel)
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

// containedIndexPath resolves rel against root through every symlink and
// returns the on-disk path only if it still lives under root.
//
// The lexical guard above cannot see this case: git tracks symlinks, so a repo
// may carry `pkg/x.go -> /etc/hosts`, whose spelling is perfectly contained. A
// bare filepath.Join + os.ReadFile would follow it and index a file outside the
// reviewed tree, then surface its identifiers as PathSuggestion candidates. The
// resolve-then-contain shape is copied from stream.existsContained and
// astgroup.Grouper.canonicalPath so all three agree on what "inside the repo"
// means. A path that cannot be resolved at all is treated as absent (skipped),
// which is also what the subsequent read would have done.
func containedIndexPath(root, rel string) (string, bool) {
	joined := filepath.Join(root, filepath.FromSlash(rel))
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root // best-effort, matching stream.existsContained
	}
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", false
	}
	r, err := filepath.Rel(realRoot, resolved)
	if err != nil {
		return "", false
	}
	if escapesIndexRoot(filepath.ToSlash(r)) {
		return "", false
	}
	return resolved, true
}
