package reconcile

import (
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/samestrin/atcr/internal/metrics"
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
// parse. Nothing else in this codebase parses the whole repository —
// astgroup.Grouper parses only the files findings actually cite, lazily, one at
// a time — so a repo-wide sweep is a genuinely new cost and is bounded here.
//
// Exceeding the cap DISABLES Tier 4 for the run (every lookup reports
// tier4Inconclusive) rather than indexing a prefix. A half-built index would
// silently mean "checked and found nothing" for every symbol living in the
// unindexed remainder, which is exactly the false-fabrication verdict the
// sidecar must never be reached on.
const maxSymbolIndexFiles = 5000

// symbolIndex maps a declared identifier to the DISTINCT tracked files that
// declare it. Only the file set matters: PathSuggestion is a path (the 5.4
// contract this epic extends), and the declaring line is not part of it.
//
// Each entry's file list is sorted and deduped at build time, so lookups are
// order-stable without re-sorting.
type symbolIndex struct {
	byName map[string][]string
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
func (x *symbolIndex) resolve(anchors []string) (string, tier4Outcome) {
	if x == nil {
		return "", tier4Inconclusive // index unavailable: could not check
	}
	if len(anchors) == 0 {
		return "", tier4Inconclusive // nothing to search for: could not check
	}
	precise := ""
	matchedSomewhere := false
	for _, a := range anchors {
		files := x.byName[a]
		if len(files) == 0 {
			continue
		}
		matchedSomewhere = true
		if len(files) > 1 {
			continue // too common to localize; a precise anchor may still decide
		}
		switch {
		case precise == "":
			precise = files[0]
		case precise != files[0]:
			return "", tier4Inconclusive // precise anchors disagree (AC7)
		}
	}
	switch {
	case precise != "":
		return precise, tier4Resolved
	case matchedSomewhere:
		return "", tier4Inconclusive // real code, just not localized (AC7)
	default:
		return "", tier4NoMatch // searched, found nothing: sidecar-eligible
	}
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
	// readFile is overridable for tests; nil means os.ReadFile.
	readFile func(string) ([]byte, error)

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
func (lz *lazySymbolIndex) resolve(anchors []string) (string, tier4Outcome) {
	if lz == nil {
		return "", tier4Inconclusive
	}
	lz.once.Do(lz.build)
	return lz.idx.resolve(anchors)
}

// build parses every eligible tracked file once and populates lz.idx, or leaves
// it nil when Tier 4 cannot run for this repo.
//
// Eligibility is "has a parser language" (astgroup.LanguageForExt) and "does not
// escape root". Per-file failures — an absent file, an unreadable one, an
// unparseable one — are SKIPPED, not fatal: a repo where one file fails to parse
// must still get Tier 4 for the rest. A failure to obtain the parser for a
// language at all is different in kind (every file of that language is now
// invisible), and if it leaves the index with nothing at all the index is
// discarded so lookups report "could not check".
func (lz *lazySymbolIndex) build() {
	eligible := lz.eligiblePaths()
	if len(eligible) == 0 {
		return
	}
	if len(eligible) > maxSymbolIndexFiles {
		slog.Warn("astgroup: tier-4 symbol index disabled, tracked file count over cap",
			"eligible", len(eligible), "cap", maxSymbolIndexFiles)
		metrics.Counter(tier4UnavailableMetric).Inc()
		return
	}

	readFile := lz.readFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	newParser := lz.newParser
	if newParser == nil {
		// The process-lifetime shared host, so the compiled-parser cache is the
		// same one clustering and symbol anchoring already warmed (T2).
		newParser = astgroup.SharedHost().Parser
	}

	sites := make(map[string]map[string]struct{})
	parsers := make(map[string]astgroup.Parser)
	parserFailed := make(map[string]bool)
	indexedFiles := 0

	for _, rel := range eligible {
		lang := astgroup.LanguageForExt(strings.ToLower(path.Ext(rel)))
		if parserFailed[lang] {
			continue
		}
		p, ok := parsers[lang]
		if !ok {
			var err error
			p, err = newParser(lang)
			if err != nil || p == nil {
				slog.Warn("astgroup: tier-4 parser unavailable, language skipped", "lang", lang, "err", err)
				parserFailed[lang] = true
				continue
			}
			parsers[lang] = p
		}
		abs, ok := containedIndexPath(lz.root, rel)
		if !ok {
			continue // symlink escaping root: refuse to read, skip
		}
		src, err := readFile(abs)
		if err != nil {
			continue // absent or unreadable tracked file: skip, never fatal
		}
		tree, err := p.Parse(src)
		if err != nil {
			continue // unparseable file: skip, never fatal
		}
		indexedFiles++
		for _, sym := range astgroup.NamedSymbols(tree) {
			if sites[sym.Name] == nil {
				sites[sym.Name] = make(map[string]struct{})
			}
			sites[sym.Name][rel] = struct{}{}
		}
	}

	if indexedFiles == 0 {
		// Nothing was indexed, so "not in the index" carries no information.
		// Leaving lz.idx nil makes every lookup inconclusive rather than a
		// false no-match.
		metrics.Counter(tier4UnavailableMetric).Inc()
		return
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
	lz.idx = &symbolIndex{byName: byName}
}

// eligiblePaths filters the tracked set to root-contained files whose extension
// has a parser, preserving a deterministic (sorted) order so a capped or
// partially-failing build is reproducible.
func (lz *lazySymbolIndex) eligiblePaths() []string {
	out := make([]string, 0, len(lz.paths))
	for _, rel := range lz.paths {
		rel = filepath.ToSlash(strings.ReplaceAll(rel, "\\", "/"))
		if rel == "" || escapesIndexRoot(rel) {
			continue
		}
		if astgroup.LanguageForExt(strings.ToLower(path.Ext(rel))) == "" {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
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
