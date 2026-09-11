package payload

import (
	"errors"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/astgroup"
)

// Context-aware pre-fetching (Epic 35.16.8).
//
// A reviewer receives the changed regions and nothing else, so a defect whose
// mechanism lives in a file the diff never touched is unreachable: the model
// cannot cite what it was never shown, and internal/fanout's grounding gate
// would drop the finding even if it guessed. This file retrieves that missing
// context by REFERENCE — the call sites that consume a changed symbol — rather
// than by resemblance, because a consumer is related to the code it calls by
// reference, not by looking like it.

const (
	// maxChangedSymbols caps how many distinct symbols one diff contributes to
	// the reference lookup. The cap bounds the `git grep` argv and, transitively,
	// the candidate file set every later stage parses — the AC4 latency budget is
	// spent per candidate file, so bounding the symbol set is what bounds the run.
	maxChangedSymbols = 40

	// maxScannedChangedLines bounds the per-file RESOLUTION WALKS. A whole-file
	// addition presents one range spanning every line, and without this a single
	// large generated file would walk hundreds of thousands of lines to yield the
	// handful of declarations the cap above already allows.
	//
	// Pass 1 spends roughly one walk per DECLARATION, not one per changed line:
	// once a line resolves to a declaration the cursor jumps to that
	// declaration's end, because every remaining line inside it resolves to the
	// same name and is deduped away. Spending the budget per line instead let one
	// huge changed function consume all 5000 walks to yield a single symbol, and
	// the labeled break below then dropped every later declaration in the file.
	// Pass 2 is still per line — it reads the line's text, so it has no choice.
	//
	// Hitting the budget can only UNDER-collect, which degrades to less context
	// rather than to a wrong answer.
	maxScannedChangedLines = 5000

	// minMockTokenLen is the shortest token the mock-cue scan will accept. It
	// discards the single-letter receivers and `t` handles that dominate test
	// lines without discarding any plausible symbol name.
	minMockTokenLen = 3
)

// mockCues are the substrings that mark a changed test line as replacing real
// behavior. The test is a lowercase substring match on the whole line, so it
// catches `mockReadStore`, `monkeypatch.Patch`, `gomock.NewController`, and a
// bare `// stub out the writer` alike.
//
// The scan deliberately OVER-collects: a token it wrongly admits is one the
// reference lookup then finds declared nowhere, so it contributes no snippet and
// costs one extra `git grep` pattern. Under-collecting is the expensive
// direction — it silently drops the AC6 case entirely.
var mockCues = []string{"mock", "patch", "stub", "fake", "spy", "double"}

// doubleNamePrefixes are the naming forms a test DOUBLE carries itself. A token
// starting with one of them names the substitute, not the thing substituted, so
// retrieving its definition would put the fake next to the fake.
//
// Matched as a PREFIX, and drawn from a narrower list than mockCues. Rejecting
// every token that merely CONTAINS a cue also rejects ordinary domain symbols —
// `ApplyPatchSet`, `DoubleBuffer`, `Inspector` — and losing one of those loses
// the AC6 snippet outright. Admitting a stray token costs one wasted `git grep`
// pattern, which is the cheaper direction, so the rejection is deliberately
// conservative. `spy` and `double` are absent for exactly this reason: both
// collide with common English inside longer identifiers.
var doubleNamePrefixes = []string{"mock", "fake", "stub"}

// doubleNameExact are whole tokens that name a double. They are matched exactly
// rather than by prefix because `patch` is a legitimate domain word — a prefix
// rule would swallow `PatchSet`, `PatchApplier` and every symbol in a package
// that is actually about patches.
var doubleNameExact = map[string]bool{"patch": true, "patched": true, "patches": true}

// mockTokenNoise are tokens that are never a mocked symbol: language keywords,
// builtins, and the test-framework verbs that appear on almost every test line.
// They are excluded so the `git grep` argv is spent on plausible symbols.
//
// It holds what is noise in EVERY language: Go's own keywords and builtins
// (this binary's tests are Go, and an unparseable file falls back to this set
// alone) plus the test-framework verbs every ecosystem shares.
//
// Entries are KEYWORDS and framework verbs only. Generic type names — set,
// list, dict, object, array, number, value, result — are deliberately absent
// even though every language has them: they are also ordinary symbol names, and
// the scan's stated bias is that admitting a stray token costs one grep pattern
// while dropping a real one loses the AC6 snippet outright. Matching is on the
// LOWERCASED token, so an entry here also rejects its capitalized form.
var mockTokenNoise = map[string]bool{
	// Go
	"func": true, "return": true, "nil": true, "err": true, "error": true,
	"byte": true, "string": true, "int": true, "int64": true, "bool": true,
	"var": true, "const": true, "type": true, "struct": true, "interface": true,
	"map": true, "chan": true, "range": true, "defer": true, "package": true,
	"import": true, "true": true, "false": true, "len": true, "cap": true,
	"make": true, "new": true, "append": true, "testing": true, "test": true,
	"for": true, "not": true, "the": true, "out": true,
	// Test frameworks, every language
	"expect": true, "describe": true, "jest": true, "spyon": true,
	"pytest": true, "unittest": true, "monkeypatch": true,
	"beforeeach": true, "aftereach": true,
}

// languageTokenNoise adds ONE language's keywords, keyed by the id
// astgroup.LanguageForExt returns for the file the cue line came from.
//
// Keyed per language rather than merged into mockTokenNoise because a great
// many of these words are ordinary exported identifiers in a DIFFERENT
// language: bash's `source`, `local` and `shift`, Rust's `use` and `some`,
// PHP's `echo`, Kotlin's `val`. Merged into one set, a Go symbol named Source
// would be silently dropped from the lookup — the failure direction this scan
// explicitly refuses, since a stray token costs one `git grep` pattern while a
// dropped real symbol costs the AC6 snippet outright.
//
// A language with no entry here (or a file with no parser, which resolves to
// "") contributes nothing, so the shared set above governs alone. Go is absent
// for that reason: mockTokenNoise already IS its keyword set.
var languageTokenNoise = map[string]map[string]bool{
	"python": {
		"def": true, "self": true, "cls": true, "elif": true, "pass": true,
		"raise": true, "except": true, "finally": true, "lambda": true,
		"yield": true, "assert": true, "none": true, "global": true,
		"nonlocal": true, "print": true, "super": true, "with": true,
		"from": true, "class": true, "try": true, "del": true, "while": true,
		"async": true, "await": true, "and": true, "import": true,
	},
	"ts": {
		"let": true, "function": true, "this": true, "async": true,
		"await": true, "export": true, "default": true, "undefined": true,
		"null": true, "typeof": true, "instanceof": true, "extends": true,
		"readonly": true, "void": true, "enum": true, "namespace": true,
		"static": true, "throw": true, "catch": true, "switch": true,
		"case": true, "break": true, "continue": true, "delete": true,
		"class": true, "implements": true, "public": true, "private": true,
		"protected": true, "abstract": true, "super": true, "yield": true,
	},
	"rust": {
		"impl": true, "mut": true, "pub": true, "crate": true, "trait": true,
		"dyn": true, "some": true, "unwrap": true, "use": true, "mod": true,
		"where": true, "loop": true, "match": true, "let": true, "enum": true,
		"async": true, "await": true, "self": true, "super": true,
	},
	"php": {
		"echo": true, "foreach": true, "endforeach": true, "elseif": true,
		"require": true, "include": true, "public": true, "private": true,
		"protected": true, "abstract": true, "implements": true,
		"namespace": true, "use": true, "this": true, "static": true,
		"function": true, "class": true, "extends": true, "null": true,
	},
	"java": {
		"final": true, "static": true, "void": true, "class": true,
		"public": true, "private": true, "protected": true, "extends": true,
		"implements": true, "throws": true, "synchronized": true,
		"this": true, "null": true, "abstract": true, "instanceof": true,
		"throw": true, "catch": true, "switch": true, "case": true,
	},
	"kotlin": {
		"val": true, "fun": true, "override": true, "suspend": true,
		"lateinit": true, "companion": true, "this": true, "class": true,
		"object": true, "null": true, "when": true, "internal": true,
		"private": true, "public": true, "protected": true, "abstract": true,
	},
	"cpp": {
		"define": true, "typedef": true, "unsigned": true, "signed": true,
		"template": true, "typename": true, "nullptr": true, "sizeof": true,
		"auto": true, "using": true, "virtual": true, "inline": true,
		"extern": true, "goto": true, "union": true, "include": true,
		"namespace": true, "class": true, "public": true, "private": true,
		"protected": true, "static": true, "void": true, "this": true,
		"delete": true, "throw": true, "catch": true, "switch": true,
	},
	"csharp": {
		"using": true, "namespace": true, "public": true, "private": true,
		"protected": true, "static": true, "void": true, "class": true,
		"override": true, "virtual": true, "async": true, "await": true,
		"this": true, "null": true, "sealed": true, "readonly": true,
		"throw": true, "catch": true, "switch": true, "case": true,
	},
	"bash": {
		"local": true, "then": true, "done": true, "esac": true,
		"shift": true, "unset": true, "source": true, "echo": true,
		"readonly": true, "export": true, "elif": true, "declare": true,
	},
}

// changedSymbol is one symbol the diff touched, with the shape a consumer would
// have to agree with.
//
// Signature carries the declaration header (from astgroup.FileSkeleton) rather
// than the name alone: AC5 is about a changed signature or return shape, and a
// reviewer handed only a name cannot tell whether a call site still agrees with
// it. It is empty when the file has no parser or the declaration could not be
// sliced — a graceful degradation, never an error.
//
// It is carried through to PrefetchSnippet.Signature and rendered by
// renderSnippetBlock. That is what makes the paragraph above a description of
// the shipped behavior rather than an argument for it: computed-but-unrendered,
// the value reached no provider and no reviewer, and the reasoning here was
// simply untrue of the payload.
//
// Mocked marks a symbol discovered in a changed TEST file on a line that mocks,
// patches, stubs or fakes something (AC6). Those symbols are retrieved so the
// REAL implementation sits next to the test that replaces it, which is what
// makes a mock that does not model the real behavior legible to a reader.
type changedSymbol struct {
	Name      string
	Signature string
	Mocked    bool
}

// extractChangedSymbols returns the symbol set the diff touched in one file.
//
// src is the file's HEAD text, ranges its head-side changed line ranges, root
// its parsed tree, and isTest whether it is a test file (which enables the
// mock-cue scan). A file with no parser passes a zero root: every line then
// falls outside root's span, so the declaration pass yields nothing and the file
// contributes only its mock cues.
//
// Order is deterministic — declaration order first, then cue order — because the
// retrieved context must be byte-identical for every agent in one fan-out (AC3),
// and the symbol order decides which snippets survive the cap.
func extractChangedSymbols(src string, ranges []LineRange, root astgroup.Node, isTest bool, lang string) []changedSymbol {
	if len(ranges) == 0 {
		return nil
	}
	skeleton := astgroup.FileSkeleton(root, src)

	declared := make(map[string]bool, len(skeleton))
	for _, e := range skeleton {
		if e.Name != "" {
			declared[e.Name] = true
		}
	}

	var out []changedSymbol
	seen := make(map[string]bool)

	// Pass 1 — the declarations the diff actually touched. EnclosingSymbolName
	// walks up past anonymous control-flow blocks, so an edit inside an `if` arm
	// resolves to the function that contains it rather than to the `if`.
	scanned := 0
	// Labeled break, NOT return: exhausting the declaration pass's line budget
	// must not skip the AC6 cue pass below. Returning here made the mock half of
	// the feature vanish on exactly the large changed test files it was written
	// for — the budget is spent walking declarations, and the cue scan never ran.
declPass:
	for _, r := range ranges {
		for line := r.Start; line <= r.End; line++ {
			if scanned >= maxScannedChangedLines || len(out) >= maxChangedSymbols {
				break declPass
			}
			scanned++
			name, ok := astgroup.EnclosingSymbolName(root, line)
			if !ok || name == "" {
				continue
			}
			if !seen[name] {
				seen[name] = true
				// Resolved on the ORIGINAL line, before the cursor moves below:
				// signatureFor's positional fallback is line-sensitive, and handing
				// it the declaration's end line could select a different entry.
				out = append(out, changedSymbol{Name: name, Signature: signatureFor(skeleton, name, line)})
			}
			// Skip to the end of the declaration this line resolved to. Every
			// remaining changed line inside it resolves to the SAME name and is
			// deduped away by seen, so walking them yields nothing while spending
			// the budget the later declarations in this file need.
			if end := declEnd(root, line); end > line {
				line = end
			}
		}
	}

	if !isTest {
		return out
	}

	// Split HERE rather than at the top of the function: the lines slice is read
	// ONLY by the cue scan below, which this guard makes unreachable for a
	// non-test file — the majority of any diff. Splitting above allocated a slice
	// over the whole source of every changed production file for nothing.
	lines := strings.Split(src, "\n")

	// Pass 2 — AC6. Only the CHANGED lines of a changed test file are scanned, so
	// an untouched mock elsewhere in the same file contributes nothing.
	//
	// It carries its OWN line budget rather than sharing pass 1's, so a large
	// declaration walk cannot starve it (and so this pass is itself bounded — it
	// previously had no line ceiling at all).
	cueScanned := 0
cuePass:
	for _, r := range ranges {
		for line := r.Start; line <= r.End; line++ {
			if cueScanned >= maxScannedChangedLines {
				break cuePass
			}
			cueScanned++
			if line < 1 || line > len(lines) {
				continue
			}
			text := lines[line-1]
			if !hasMockCue(text) {
				continue
			}
			for _, tok := range identifierTokens(text) {
				if len(out) >= maxChangedSymbols {
					return out
				}
				if !plausibleMockTarget(tok, lang) || seen[tok] || declared[tok] {
					continue
				}
				seen[tok] = true
				out = append(out, changedSymbol{Name: tok, Mocked: true})
			}
		}
	}
	return out
}

// declEnd returns the end line of the TOP-LEVEL declaration covering line, or 0
// when no child of root covers it.
//
// It reads root.Children directly rather than calling astgroup.CoveringBlock,
// which returns the DEEPEST covering block — the `if` arm inside a function, not
// the function — and would therefore advance the cursor only to the end of the
// innermost clause, leaving the rest of a huge declaration to be walked line by
// line. A zero root (no parser) has no children and yields 0, so the caller
// falls back to stepping one line at a time exactly as before.
func declEnd(root astgroup.Node, line int) int {
	for _, ch := range root.Children {
		if ch.StartLine <= line && line <= ch.EndLine {
			return ch.EndLine
		}
	}
	return 0
}

// signatureFor returns the declaration header for name, preferring an exact name
// match and falling back to the nearest declaration starting at or above line.
//
// The fallback exists because not every skeleton entry carries a name: the Go
// parser emits gendecl nodes (type, const, var) with an empty Name, so a change
// inside one is resolvable only by position. Matching by name first keeps two
// same-named declarations in one file from swapping headers by line proximity.
func signatureFor(entries []astgroup.SkeletonEntry, name string, line int) string {
	for _, e := range entries {
		if e.Name != "" && e.Name == name {
			return e.Header
		}
	}
	best, bestStart := "", 0
	for _, e := range entries {
		if e.StartLine <= line && e.StartLine >= bestStart {
			bestStart, best = e.StartLine, e.Header
		}
	}
	return best
}

// hasMockCue reports whether a line replaces real behavior with a test double.
func hasMockCue(line string) bool {
	lower := strings.ToLower(line)
	for _, cue := range mockCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// plausibleMockTarget reports whether tok could name the REAL symbol a cue line
// replaces.
//
// A token naming the DOUBLE is rejected — `mockReadStore`, `fakeClock`,
// `patched` — because retrieving the substitute's own definition puts the
// substitute next to the substitute. The symbol worth showing beside a mock is
// the thing being mocked.
//
// The rejection matches a prefix (doubleNamePrefixes) or a whole token
// (doubleNameExact), never a bare substring: a substring rule also rejects
// `ApplyPatchSet` and `DoubleBuffer`, and a missed real symbol costs the AC6
// snippet while a stray one costs a single `git grep` pattern.
// lang is the file's resolved language id (astgroup.LanguageForExt), which
// selects the keyword set applied on top of the shared one. An empty lang — a
// file no parser reads — is answered by the shared set alone.
func plausibleMockTarget(tok, lang string) bool {
	if len(tok) < minMockTokenLen {
		return false
	}
	lower := strings.ToLower(tok)
	if mockTokenNoise[lower] || languageTokenNoise[lang][lower] || doubleNameExact[lower] {
		return false
	}
	for _, p := range doubleNamePrefixes {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}
	// A token that is all digits (or starts with one) is a literal, not a symbol.
	return tok[0] < '0' || tok[0] > '9'
}

const (
	// maxPrefetchSitesPerSymbol bounds how many candidate sites ONE symbol may
	// contribute to ADMISSION. Without it a very common name (Close, Run, New)
	// fills the whole candidate set before any other changed symbol is
	// represented, and the byte cap then sheds the symbols that actually needed
	// context.
	//
	// It is a bounded MULTIPLE of the emitted ceiling below rather than equal to
	// it, because admission is not emission: three later filters — the declOnly
	// declaration check, the maxAnalyzeFileBytes ceiling and overlapsEmitted — can
	// still reject an admitted hit. At parity a cue-derived symbol whose bare
	// mentions were all rejected spent its entire allowance and yielded nothing,
	// while a real consumer further down the match list was refused admission.
	maxPrefetchSitesPerSymbol = 6

	// maxEmittedSitesPerSymbol bounds how many snippets ONE symbol may actually
	// contribute. Fairness between symbols is a property of RESULTS, so it is
	// enforced where the snippet is emitted; latency is a property of CANDIDATES,
	// and maxPrefetchHits plus maxPrefetchFiles still bound that. One cap could
	// not be both without giving up one of the two.
	maxEmittedSitesPerSymbol = 3

	// maxPrefetchHits is the absolute ceiling on candidate sites from one lookup,
	// independent of how many symbols contributed them. Each surviving hit costs a
	// HEAD blob read and a parse downstream, which is where the AC4 latency budget
	// is actually spent, so this is the bound that keeps a very wide diff from
	// turning a ~20ms lookup into a repo-wide sweep by another name.
	maxPrefetchHits = 60

	// maxGrepMatchesPerFile bounds how many lines ONE file may contribute to the
	// `git grep` OUTPUT, via -m. Without it a common name (Run, New, Close) in a
	// large repository produces tens of megabytes that gitRunner.output buffers
	// whole, purely to select at most maxPrefetchHits records from it.
	//
	// It is deliberately larger than maxPrefetchSitesPerSymbol: -m is a per-FILE
	// ceiling across ALL patterns, so a file that legitimately consumes several
	// different changed symbols would otherwise lose the later ones before
	// parseGrepHits ever applied its own per-symbol cap.
	maxGrepMatchesPerFile = 10
)

// refHit is one `git grep` match: a candidate site that REFERENCES a changed
// symbol. Symbol records which changed symbol the match was attributed to, so a
// retrieved snippet can say what it was retrieved for.
type refHit struct {
	Path   string
	Line   int
	Symbol string
}

// parseGrepHits turns `git grep -n` output into candidate consumer sites.
//
// exclude drops files the diff already changed: those are in the payload
// verbatim, so retrieving a snippet of them spends the byte cap re-showing text
// the reviewer already has. maxPerSymbol bounds how many sites one symbol may
// contribute, so a single very common name cannot crowd out every other symbol.
// skip, when non-nil, drops a candidate path outright. It carries the ignore
// filter, and it is applied BEFORE the per-symbol cap on purpose: filtering
// afterwards would let three vendored hits consume a symbol's whole cap and
// leave a real consumer unretrieved.
// rev is the tree-ish `git grep` echoes back as a "<rev>:" prefix on every
// record. It is trimmed INLINE here, one line at a time, rather than by a
// separate pass: the output is unbounded in principle, and a Split plus a Join
// to strip the prefix followed by another Split to iterate materialized it three
// more times — roughly 4x peak of an already large blob — to select at most
// maxPrefetchHits records from it.
//
// maxHits is the absolute ceiling on returned hits. It is INJECTED rather than
// read from package scope so the function can be exercised at a different
// ceiling without editing a const — previously it took maxPerSymbol as a
// parameter but closed over maxPrefetchHits, which made it only half testable.
// It is guarded exactly like maxPerSymbol: a non-positive ceiling admits
// nothing, so "disabled" can never silently read as "unlimited".
func parseGrepHits(out, rev string, symbols []string, exclude map[string]bool, skip func(string) bool, maxPerSymbol, maxHits int) []refHit {
	if out == "" || len(symbols) == 0 || maxPerSymbol <= 0 || maxHits <= 0 {
		return nil
	}
	revPrefix := ""
	if rev != "" {
		revPrefix = rev + ":"
	}
	perSymbol := make(map[string]int, len(symbols))
	var hits []refHit
	for rest := out; rest != ""; {
		var line string
		line, rest, _ = strings.Cut(rest, "\n")
		// TrimPrefix with an empty prefix is a no-op, so an untagged output (no
		// rev, as in a direct unit call) flows through unchanged.
		line = strings.TrimPrefix(line, revPrefix)
		p, num, text, ok := splitGrepLine(line)
		if !ok || exclude[p] || (skip != nil && skip(p)) {
			continue
		}
		// A hit in a file no embedded parser can read cannot be expanded into a
		// snippet later, so admitting it here would spend a per-symbol cap slot on
		// a site that can never be rendered. `git grep -I` already skips binaries;
		// this additionally skips prose (README, CHANGELOG, docs/) — where an
		// identifier-shaped word is a mention, not a call site.
		if astgroup.LanguageForExt(strings.ToLower(path.Ext(p))) == "" {
			continue
		}
		sym, ok := attributeSymbol(text, symbols)
		if !ok {
			continue
		}
		if perSymbol[sym] >= maxPerSymbol {
			continue
		}
		perSymbol[sym]++
		hits = append(hits, refHit{Path: p, Line: num, Symbol: sym})
		if len(hits) >= maxHits {
			break
		}
	}
	return hits
}

// splitGrepLine splits one `git grep -n` record into path, line number and text.
//
// It splits on the FIRST two colons. A path containing a colon (legal on unix,
// vanishingly rare in a tracked tree) makes the second field unparseable as an
// integer and the record is skipped — under-collecting one candidate rather than
// attributing a snippet to the wrong file.
func splitGrepLine(line string) (p string, num int, text string, ok bool) {
	parts := strings.SplitN(line, ":", 3)
	if len(parts) < 3 || parts[0] == "" {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n <= 0 {
		return "", 0, "", false
	}
	return parts[0], n, parts[2], true
}

// attributeSymbol reports which changed symbol a matched line cites.
//
// The match is word-bounded, not a bare substring: `Store` is a substring of
// `ReadStore`, and attributing a ReadStore call site to Store would label the
// snippet with a symbol the reviewer never changed. Symbols are consulted in
// order, so attribution is deterministic (AC3).
func attributeSymbol(text string, symbols []string) (string, bool) {
	for _, s := range symbols {
		if s != "" && containsWord(text, s) {
			return s, true
		}
	}
	return "", false
}

// containsWord reports whether word occurs in text bounded by non-identifier
// characters on both sides.
func containsWord(text, word string) bool {
	for from := 0; ; {
		i := strings.Index(text[from:], word)
		if i < 0 {
			return false
		}
		i += from
		beforeOK := i == 0 || !isIdentByte(text[i-1])
		end := i + len(word)
		afterOK := end == len(text) || !isIdentByte(text[end])
		if beforeOK && afterOK {
			return true
		}
		from = i + 1
		if from >= len(text) {
			return false
		}
	}
}

func isIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// grepPatterns reduces the changed symbols to the deduped, argv-safe names the
// reference lookup will search for.
//
// validGrepSymbol is a SECURITY boundary as well as a noise filter: these names
// come from parsed source and flow into a subprocess argv, so a name beginning
// with `-` would be read as a flag and one containing a glob or a space would
// change what git searches. Restricting to identifier shape makes both
// impossible, and `-e` then guarantees the value is treated as a pattern.
func grepPatterns(symbols []changedSymbol) []string {
	seen := make(map[string]bool, len(symbols))
	var out []string
	for _, s := range symbols {
		if !validGrepSymbol(s.Name) || seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		out = append(out, s.Name)
		if len(out) >= maxChangedSymbols {
			break
		}
	}
	return out
}

// validGrepSymbol reports whether name is a plain identifier safe to pass as a
// fixed-string search pattern.
func validGrepSymbol(name string) bool {
	if len(name) < 2 {
		return false // a one-character name matches too much to be worth a slot
	}
	for i := 0; i < len(name); i++ {
		if !isIdentByte(name[i]) {
			return false
		}
	}
	return name[0] < '0' || name[0] > '9'
}

// referenceHits resolves every changed symbol to the sites that consume it, in
// ONE `git grep` process regardless of symbol count.
//
// It never returns an error. Pre-fetching is an ADDITIONAL input to a review, so
// a failed lookup degrades to empty context rather than failing the review — the
// same fail-open contract the claim ledger applies to an unreadable `git log`.
// failed reports that the lookup genuinely BROKE, as opposed to running fine and
// matching nothing. Both yield empty context, but they are opposite operational
// signals and only the first should set PrefetchStatus.Failed.
func (g *gitRunner) referenceHits(head string, symbols []changedSymbol, exclude map[string]bool) (hits []refHit, failed bool) {
	names := grepPatterns(symbols)
	if len(names) == 0 {
		// The laziness contract: a diff citing no resolvable symbol spawns no
		// process and reads no source file. Returning BEFORE g.output is what makes
		// that observable through execCount.
		return nil, false
	}
	// ONE process for every symbol. `-F` makes each pattern a fixed string (never
	// a regex), `-w` bounds it to whole words so `Store` does not match
	// `ReadStore`, and `-I` skips binaries. Each name is introduced by `-e`, so a
	// value can never be read as a flag.
	args := make([]string, 0, 7+2*len(names))
	args = append(args, "grep", "-n", "-I", "-F", "-w", "--no-color",
		"-m", strconv.Itoa(maxGrepMatchesPerFile))
	for _, n := range names {
		args = append(args, "-e", n)
	}
	// Search the REVIEWED REVISION, not the working tree.
	//
	// Without a tree-ish, `git grep` searches the checked-out files while
	// retrieveSnippets slices the same paths out of the `head` blob. On a dirty
	// worktree, or a range that is not checked out at all (`atcr review --base X
	// --head Y`), the hit line numbers then index DIFFERENT content than the
	// snippet is cut from — so the region shipped to providers, and the grounding
	// span derived from it, are both silently wrong. Passing head makes the search
	// and the slice read the same bytes.
	//
	// The trailing `--` is what makes the tree-ish UNAMBIGUOUS. `git grep` accepts
	// `<rev>... [--] [<pathspec>...]`, so without the separator a repository
	// containing a tracked file whose name equals the ref leaves git unable to
	// tell which was meant, and it fails with "ambiguous argument". The error arm
	// below reads any failure as "matched nothing", so that repository would lose
	// pre-fetching silently and permanently rather than loudly and once.
	args = append(args, head, "--")
	out, err := g.output(args...)
	if err != nil {
		// `git grep` exits 1 for NO MATCH and greater than 1 for a real failure.
		// gitRunner.output wraps the ExitError with %w (internal/payload/diff.go),
		// so errors.As recovers the code and the two ARE separable. An earlier
		// comment here asserted they were not, and that false premise is why
		// PrefetchStatus.Failed was never set for the single failure mode the type
		// exists to report — a permanently broken lookup was indistinguishable
		// from a clean no-match, forever.
		//
		// BOTH still degrade to empty context: pre-fetching is an ADDITIONAL
		// review input, so a broken lookup must never fail the review. Only what
		// is REPORTED differs.
		//
		// A non-ExitError (a cancelled context, a git binary that could not be
		// spawned) is a failure too, so anything that is not exactly exit 1
		// counts as broken.
		var exitErr *exec.ExitError
		broke := !errors.As(err, &exitErr) || exitErr.ExitCode() != 1
		if broke {
			g.log().Debug("payload: reference lookup FAILED; review proceeds without pre-fetched context",
				"symbols", len(names), "error", err)
		} else {
			g.log().Debug("payload: reference lookup matched nothing; review proceeds without pre-fetched context",
				"symbols", len(names))
		}
		return nil, broke
	}
	// The ignore filter must govern RETRIEVED context exactly as it governs the
	// diff. exclude is built from the already-filtered changed-file list, so an
	// ignored file is simply absent from it — and without this check a vendored,
	// generated or otherwise excluded file that merely REFERENCES a changed symbol
	// would be read and shipped to every reviewer, defeating the filter that kept
	// it out of the payload in the first place.
	var skip func(string) bool
	if m := g.matcher(); m.active() {
		skip = m.match
	}
	return parseGrepHits(string(out), head, names, exclude, skip, maxPrefetchSitesPerSymbol, maxPrefetchHits), false
}

const (
	// maxSnippetLines bounds one retrieved snippet. A consumer whose enclosing
	// function is enormous contributes the region AROUND the call site rather than
	// the whole function: the reviewer needs to see how the symbol is used, and
	// spending the byte cap on hundreds of unrelated lines starves every other
	// snippet.
	maxSnippetLines = 40

	// maxPrefetchFiles bounds how many DISTINCT candidate files are read and
	// parsed. This is the constant that actually holds AC4: every file past it
	// costs a `git show` plus a wasm parse, which is where the latency lives.
	maxPrefetchFiles = 25

	// maxPrefetchChangedFiles bounds how many CHANGED files the symbol-extraction
	// loop reads HEAD blobs for, alongside maxPrefetchFiles bounding the CANDIDATE
	// files retrieval reads. It exists because maxChangedSymbols cannot bound the
	// loop by itself: a file whose change yields no symbol (prose-only edits, a
	// comment-only change, or every symbol already seen) never moves the symbol
	// count, so an all-parseable pathological diff would read every changed blob.
	// 250 comfortably spans the 40-symbol budget without starving real diffs.
	maxPrefetchChangedFiles = 250

	// snippetFallbackRadius is the half-window used when a file cannot be parsed,
	// so an unparseable candidate degrades to a plain neighbourhood of the call
	// site instead of contributing nothing.
	snippetFallbackRadius = 8
)

// PrefetchSnippet is one retrieved region of a file the diff did not change.
//
// Start and End are 1-based inclusive HEAD line numbers. They are carried, not
// just the text, because the grounding gate is threaded with exactly this span:
// a finding inside it is groundable, and a finding elsewhere in the same file is
// still dropped as ungrounded.
type PrefetchSnippet struct {
	Path   string
	Symbol string
	Start  int
	End    int
	Body   string
	// Signature is the declaration header of the CHANGED symbol this snippet was
	// retrieved FOR — not of the code shown below it. It is rendered beside the
	// symbol name because AC5 is about a changed signature or return shape: a
	// reviewer handed only a name cannot judge whether the call site still agrees
	// with it. Empty when the changed symbol's own file had no parser or its
	// declaration could not be sliced, which renders exactly as it did before.
	Signature string
	// Tier ranks this snippet for shedding when the byte cap bites (AC7).
	Tier PrefetchTier
}

// snippetSpan expands a call-site line to the span worth showing around it.
//
// The span is the deepest AST block covering the line — the enclosing function
// or clause, so the reviewer sees a complete unit rather than a floating line —
// bounded by maxSnippetLines and re-centred on the hit when the block is larger
// than that. A zero root (no parser, or a parse that failed) degrades to a fixed
// window rather than returning nothing.
func snippetSpan(root astgroup.Node, line int) (start, end int) {
	if line < 1 {
		line = 1
	}
	lo, hi := line-snippetFallbackRadius, line+snippetFallbackRadius
	// Block bounds, retained so the re-centre below can be clamped to them. They
	// stay 0 when no block covered the line, which is what distinguishes the
	// parser-backed path from the fixed-window fallback.
	var blockStart, blockEnd int
	if block, _, ok := astgroup.CoveringBlock(root, line); ok && block.StartLine > 0 && block.EndLine >= block.StartLine {
		lo, hi = block.StartLine, block.EndLine
		blockStart, blockEnd = block.StartLine, block.EndLine
	}
	if lo < 1 {
		lo = 1
	}
	if hi-lo+1 > maxSnippetLines {
		// Re-CENTRE on the call site rather than truncating the block from its
		// top. Truncating would show the declaration header and none of the use
		// that motivated retrieving the file, which is the one thing the reviewer
		// needs to judge whether the call still agrees with the changed shape.
		lo = line - (maxSnippetLines-1)/2
		// Clamp the re-centred window to the BLOCK, not just to the file. A hit
		// within half a window of either edge of a large block otherwise produces
		// a span running into the neighbouring declaration — the snippet then
		// shows an unrelated function, and those lines are threaded into
		// prefetchSpans, which makes them groundable for a finding.
		//
		// Only when a block was actually found: the no-parser fallback window has
		// no declaration to stay inside and must remain unclamped.
		if blockStart > 0 && lo < blockStart {
			lo = blockStart
		}
		if lo < 1 {
			lo = 1
		}
		hi = lo + maxSnippetLines - 1
		if blockEnd > 0 && hi > blockEnd {
			hi = blockEnd
		}
	}
	return lo, hi
}

// sliceLines returns src's [start,end] 1-based inclusive line span, clamped to
// the text that actually exists.
//
// It returns the CLAMPED bounds alongside the body, and the caller records those
// rather than the requested ones: the span is what the grounding gate is
// threaded with, so a span claiming lines past the end of the file would mark
// non-existent lines groundable.
// splitSnippetLines splits src into its 1-based-indexable lines, once per
// file. retrieveSnippets slices EVERY hit out of the same immutable text, so
// splitting per hit — as sliceLines used to — allocated a full line slice of
// the file for each hit it contained (a candidate near the maxAnalyzeFileBytes
// ceiling with 10 hits paid ~10x the file size to extract at most 400 lines).
func splitSnippetLines(src string) []string {
	lines := strings.Split(src, "\n")
	// A trailing newline yields a final empty element that is not a real line.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i := range lines {
		// A CRLF checkout leaves a carriage return at the end of every split
		// element. Strip it per line, or the \r rides the rendered section into
		// provider prompts (inflating the byte count the cap adjudicates on) and
		// the same file's snippet body — and the span its grounding is threaded
		// with — would differ by line ending alone.
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

func sliceLines(lines []string, start, end int) (body string, s, e int, ok bool) {
	if len(lines) == 0 {
		return "", 0, 0, false
	}
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) || end < start {
		return "", 0, 0, false
	}
	return strings.Join(lines[start-1:end], "\n"), start, end, true
}

// parsePrefetchTree parses a candidate file with the parser its extension maps
// to, returning a ZERO node when no parser applies or the parse fails.
//
// A zero node is a working answer, not an error: snippetSpan degrades to a fixed
// window around the call site, so an unparseable candidate still contributes the
// neighbourhood of its reference.
func parsePrefetchTree(rel, src string) astgroup.Node {
	lang := astgroup.LanguageForExt(strings.ToLower(path.Ext(rel)))
	if lang == "" {
		return astgroup.Node{}
	}
	parser, err := astgroup.SharedHost().Parser(lang)
	if err != nil || parser == nil {
		return astgroup.Node{}
	}
	root, err := parser.Parse([]byte(src))
	if err != nil {
		return astgroup.Node{}
	}
	return root
}

// overlapsEmitted reports whether [start,end] intersects a span already emitted
// for the same file. Two call sites inside one function otherwise render the
// same body twice, paying the byte cap twice for one region.
func overlapsEmitted(emitted [][2]int, start, end int) bool {
	for _, sp := range emitted {
		if start <= sp[1] && sp[0] <= end {
			return true
		}
	}
	return false
}

// retrieveSnippets reads each candidate file's HEAD blob once and slices the
// region around every hit in it.
// declOnly names the symbols that entered the set ONLY through the mock-cue
// scan. Those are held to a stricter rule than an ordinary changed symbol: see
// the check in the per-hit loop below.
func (g *gitRunner) retrieveSnippets(base, head string, hits []refHit, declOnly map[string]bool) []PrefetchSnippet {
	if len(hits) == 0 {
		return nil
	}
	// Group by path in FIRST-APPEARANCE order, never by iterating a map: the
	// retrieved context must be byte-identical for every agent in one fan-out
	// (AC3), and map order would make it differ run to run.
	order := make([]string, 0, len(hits))
	byPath := make(map[string][]refHit, len(hits))
	for _, h := range hits {
		if _, seen := byPath[h.Path]; !seen {
			if len(order) >= maxPrefetchFiles {
				continue // over the file cap: drop this candidate entirely
			}
			order = append(order, h.Path)
		}
		byPath[h.Path] = append(byPath[h.Path], h)
	}

	var out []PrefetchSnippet
	// Counted across the WHOLE call, not per file: a symbol consumed from three
	// different files must still respect its emitted ceiling.
	perSymbolEmitted := make(map[string]int, len(hits))
	for _, rel := range order {
		// ReuseMemo, not Memo: these are files the diff did NOT change, read once
		// each, so populating the per-range blob cache would retain every
		// candidate's full text for the life of the range at a 0% hit rate — the
		// same reasoning the files-mode render documents.
		src, err := g.headContentReuseMemo(base, head, rel)
		if err != nil {
			// A candidate that vanished between the grep and the read (a concurrent
			// checkout, a submodule path) costs that one snippet, never the context.
			g.log().Debug("payload: pre-fetch candidate unreadable, skipped", "path", rel, "error", err)
			continue
		}
		if len(src) > maxAnalyzeFileBytes {
			continue // generated/oversized: not worth a parse, same ceiling as escalation
		}
		root := parsePrefetchTree(rel, src)
		lines := splitSnippetLines(src)
		emitted := make([][2]int, 0, len(byPath[rel]))
		for _, h := range byPath[rel] {
			// The per-symbol ceiling on EMITTED snippets. Checked before the span and
			// slice work below, which a hit over the ceiling would only discard.
			if perSymbolEmitted[h.Symbol] >= maxEmittedSitesPerSymbol {
				continue
			}
			// A cue-derived symbol must resolve to a DECLARATION of itself, not a
			// mere mention.
			//
			// The AC6 scan admits any identifier sitting on a changed test line that
			// mentions mock/patch/stub/fake — deliberately loose, because the symbol
			// being replaced is not syntactically marked. That looseness is fine while
			// it only costs a `git grep` pattern, but a matching token elsewhere in the
			// tree otherwise causes a 40-line region of an unrelated file to be read
			// and shipped to a third-party provider. Ordinary changed symbols keep
			// call-site retrieval (that is AC5, the whole point); only the guessed
			// ones must earn their snippet by being declared.
			if declOnly[h.Symbol] {
				if name, ok := astgroup.EnclosingSymbolName(root, h.Line); !ok || name != h.Symbol {
					continue
				}
			}
			start, end := snippetSpan(root, h.Line)
			body, s, e, ok := sliceLines(lines, start, end)
			if !ok || overlapsEmitted(emitted, s, e) {
				continue
			}
			emitted = append(emitted, [2]int{s, e})
			perSymbolEmitted[h.Symbol]++
			out = append(out, PrefetchSnippet{Path: rel, Symbol: h.Symbol, Start: s, End: e, Body: body})
		}
	}
	return out
}

// DefaultMaxPrefetchBytes is the default ceiling on the rendered Context
// Definitions section.
//
// It mirrors max_claim_bytes exactly (Epic 35.16.7, shipped immediately before
// this one): the feature is ON by default, and 0 DISABLES it outright rather
// than meaning "unlimited". The section's bytes are exempt from
// payload_byte_budget for the same reason the claim ledger's are — its value
// depends on reaching every reviewer identically — so this ceiling is the only
// thing bounding them, and an "unlimited" reading would be unbounded prompt text
// nothing downstream could shed.
//
// It is deliberately NOT named DefaultMaxContextBytes: in this package "context"
// already means the MODEL CONTEXT WINDOW (ResolveContextWindow,
// DefaultMaxContextLines), an unrelated quantity one letter away.
const DefaultMaxPrefetchBytes int64 = 16 * 1024

// PrefetchTier ranks a snippet's retrieval provenance for shedding. A LOWER tier
// is shed FIRST (AC7).
type PrefetchTier int

const (
	// PrefetchTierSimilarity is reserved for epic 35.16.12's embedding-similarity
	// retrieval, and is the lowest tier so similarity snippets shed before
	// reference ones. THIS EPIC EMITS NONE. It exists so 35.16.12 plugs into this
	// ledger rather than adding a second injection site — the contract AC7
	// describes when it says a lower-priority tier is always shed first.
	PrefetchTierSimilarity PrefetchTier = iota
	// PrefetchTierReference is a snippet reached by REFERENCE: a call site that
	// consumes a changed symbol, or the real implementation behind a mocked one.
	PrefetchTierReference
)

// PrefetchDrop records one snippet the byte cap shed.
//
// Every shed snippet produces one of these. AC7's requirement is that a drop is
// RECORDED rather than silent: a reviewer told nothing was retrieved cannot tell
// that apart from a reviewer whose context was silently discarded, and only the
// first is honest.
type PrefetchDrop struct {
	Path   string
	Symbol string
	Tier   PrefetchTier
	Bytes  int
}

// capPrefetchSnippets keeps as many snippets as fit within maxBytes, shedding
// the LOWEST tier first and the largest snippet first within a tier, and returns
// a ledger naming every snippet it shed.
//
// PRECONDITION: maxBytes must be positive. The operator's off switch lives one
// level up, in RangeBuilder.prefetch — it returns before any retrieval runs
// when maxPrefetchBytes <= 0 and reports PrefetchStatus.Disabled, so "turned
// off" and "retrieved nothing" stay distinguishable there. A not-positive
// argument here sheds everything through the general path, which is correct
// but is never the disabled signal.
func capPrefetchSnippets(snips []PrefetchSnippet, maxBytes int64) (kept []PrefetchSnippet, dropped []PrefetchDrop) {
	if len(snips) == 0 {
		return nil, nil
	}

	// Measure the RENDERED size, not len(Body): the emitted block carries a
	// per-snippet header and an "L<n>: " prefix on every line, so a body-only
	// budget lets the section overshoot max_prefetch_bytes by 15-40%.
	size := make([]int64, len(snips))
	var total int64
	for i, s := range snips {
		size[i] = int64(len(renderSnippetBlock(s)))
		total += size[i]
	}

	// The section overhead is billed INSIDE the budget: the start/end markers
	// and every drop-ledger line are emitted AFTER the snippet accounting, and
	// leaving them out let a 1024-byte cap emit a 1892-byte section. The ledger
	// bytes depend on WHICH snippets are shed, so the shed loop below
	// re-measures them at every step — and renders them through the same
	// function the section uses, because an accountant that can drift from the
	// emitter is the defect this reservation exists to close.
	markers := int64(len(prefetchSectionStart) + 1 + len(prefetchSectionEnd) + 1)
	if total+markers <= maxBytes {
		return append([]PrefetchSnippet(nil), snips...), nil
	}

	// Drop order: LOWEST tier first (AC7), then largest-first within a tier — the
	// same largest-first rule the payload byte budget uses, so shedding frees the
	// most room for the fewest lost snippets — then path and symbol as a stable
	// tie-break. Indices are sorted rather than the snippets themselves so two
	// snippets sharing a path are accounted for independently.
	idx := make([]int, len(snips))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		si, sj := snips[idx[a]], snips[idx[b]]
		if si.Tier != sj.Tier {
			return si.Tier < sj.Tier
		}
		if size[idx[a]] != size[idx[b]] {
			return size[idx[a]] > size[idx[b]]
		}
		if si.Path != sj.Path {
			return si.Path < sj.Path
		}
		return si.Symbol < sj.Symbol
	})

	drop := make([]bool, len(snips))
	used := total + markers
	for _, i := range idx {
		// Whole snippets only. A truncated snippet reads as a complete function to
		// the reviewer, which is worse than its absence: it invites a finding about
		// logic that was simply cut off.
		drop[i] = true
		used -= size[i]
		if used+int64(len(renderPrefetchDropLedger(dropsFor(snips, drop, size)))) <= maxBytes {
			break
		}
	}
	return splitPrefetchLedger(snips, drop, size)
}

// dropsFor returns the dropped subset of snips in ORIGINAL order, mirroring
// splitPrefetchLedger — the ledger lists drops in snippet order even though
// the shed order is tier-then-size. Bytes is the RENDERED size: the ledger
// line prints it, so the accounting must measure the same number it would
// emit, digit-for-digit.
func dropsFor(snips []PrefetchSnippet, drop []bool, size []int64) []PrefetchDrop {
	var out []PrefetchDrop
	for i, s := range snips {
		if drop[i] {
			out = append(out, PrefetchDrop{Path: s.Path, Symbol: s.Symbol, Tier: s.Tier, Bytes: int(size[i])})
		}
	}
	return out
}

// splitPrefetchLedger partitions snips by the drop mask, preserving the original
// order in BOTH results so the rendered section and its ledger are deterministic
// (AC3).
// size carries each snippet's RENDERED byte count so the ledger reports the
// bytes a drop actually freed, which is the number the cap adjudicated on.
func splitPrefetchLedger(snips []PrefetchSnippet, drop []bool, size []int64) (kept []PrefetchSnippet, dropped []PrefetchDrop) {
	for i, s := range snips {
		if drop[i] {
			dropped = append(dropped, PrefetchDrop{
				Path:   s.Path,
				Symbol: s.Symbol,
				Tier:   s.Tier,
				Bytes:  int(size[i]),
			})
			continue
		}
		kept = append(kept, s)
	}
	return kept, dropped
}

// Rendered Context Definitions markers. Like the skeleton markers, they avoid
// every prefix the rendered-payload splitter treats as the start of a new file
// section (isRenderedEntryStart) and every prefix the diff scanner reads, so the
// block folds into the payload instead of opening a spoofed section.
const (
	prefetchSectionStart = ">>> CONTEXT DEFINITIONS <<<"
	prefetchSectionEnd   = ">>> END CONTEXT DEFINITIONS <<<"
	// prefetchNotePrefix leads every non-source line inside the block.
	prefetchNotePrefix = "[context] "

	// maxPrefetchDropLines bounds the rendered drop ledger. Without it a tiny cap
	// produced a section that was almost entirely ledger: every shed snippet
	// contributed a line, and those lines were themselves outside the byte
	// accounting. The remainder is disclosed as a count, so a bounded ledger is
	// still never a silent one.
	maxPrefetchDropLines = 10
)

// renderSnippetBlock renders ONE snippet exactly as it appears in the payload.
//
// The cap and the renderer share this function deliberately. Budgeting on
// len(Body) while emitting a header plus an "L<n>: " prefix on every line
// under-measured the block by 15-40%, so a section sized to fit
// max_prefetch_bytes routinely exceeded it. An estimator that can drift from the
// emitter is the defect; one function cannot drift from itself.
func renderSnippetBlock(s PrefetchSnippet) string {
	var b strings.Builder
	b.WriteString(prefetchNotePrefix)
	b.WriteString(s.Path)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(s.Start))
	b.WriteByte('-')
	b.WriteString(strconv.Itoa(s.End))
	b.WriteString(" (")
	b.WriteString(s.Tier.String())
	b.WriteString(" to ")
	b.WriteString(s.Symbol)
	if sig := flattenSignature(s.Signature); sig != "" {
		b.WriteString(": ")
		b.WriteString(sig)
	}
	b.WriteString(")\n")
	// Anchor every source line with its real HEAD line number. This is the safety
	// property as much as a convenience: because each content line begins with
	// "L<digits>: ", a retrieved body carrying a section marker cannot open a
	// spoofed file section.
	line := s.Start
	for _, src := range strings.Split(s.Body, "\n") {
		b.WriteByte('L')
		b.WriteString(strconv.Itoa(line))
		b.WriteString(": ")
		b.WriteString(src)
		b.WriteByte('\n')
		line++
	}
	return b.String()
}

// flattenSignature collapses a declaration header to ONE line.
//
// The header is repository-controlled text sliced straight out of source, and
// every safety property of the rendered block rests on each emitted line
// beginning with "[context] " or "L<digits>: ". A header carrying a newline
// would emit a bare repository-controlled line that could open a spoofed file
// section — the exact injection renderPrefetchSection's anchors exist to block.
func flattenSignature(sig string) string {
	if !strings.ContainsAny(sig, "\r\n") {
		return strings.TrimSpace(sig)
	}
	flat := strings.ReplaceAll(sig, "\r\n", " ")
	flat = strings.ReplaceAll(flat, "\n", " ")
	flat = strings.ReplaceAll(flat, "\r", " ")
	return strings.TrimSpace(flat)
}

// String names a tier for the rendered drop ledger.
func (t PrefetchTier) String() string {
	switch t {
	case PrefetchTierSimilarity:
		return "similarity"
	case PrefetchTierReference:
		return "reference"
	default:
		return "unknown"
	}
}

// renderPrefetchSection formats the retrieved snippets as a payload block, or
// returns "" when there is nothing to say at all.
//
// Every source line is emitted with an "L<n>: " anchor. The anchor is the safety
// property, exactly as in renderSkeleton: because each content line begins with
// "L<digits>: ", no rendered line can start with a payload section marker even
// if the retrieved source did — so a repository cannot inject a spoofed file
// section through a snippet body.
//
// A non-empty drop ledger is rendered even when NOTHING was kept. AC7 asks that
// a drop be recorded rather than silent, and a reviewer shown no section cannot
// tell "nothing was retrieved" from "everything retrieved was shed".
func renderPrefetchSection(kept []PrefetchSnippet, dropped []PrefetchDrop) string {
	if len(kept) == 0 && len(dropped) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(prefetchSectionStart)
	b.WriteByte('\n')

	for _, s := range kept {
		b.WriteString(renderSnippetBlock(s))
	}

	b.WriteString(renderPrefetchDropLedger(dropped))

	b.WriteString(prefetchSectionEnd)
	b.WriteByte('\n')
	return b.String()
}

// renderPrefetchDropLedger renders the bounded drop ledger (no section
// markers). capPrefetchSnippets measures its output INSIDE the byte budget, so
// this is the single emitter for both the render and the accounting — a
// ledger estimator that could drift from the emitted text would reopen the
// over-cap section defect the reservation closed.
func renderPrefetchDropLedger(dropped []PrefetchDrop) string {
	var b strings.Builder
	for i, d := range dropped {
		if i >= maxPrefetchDropLines {
			b.WriteString(prefetchNotePrefix)
			b.WriteString("... and ")
			b.WriteString(strconv.Itoa(len(dropped) - i))
			b.WriteString(" more snippet(s) dropped over the pre-fetch byte cap\n")
			break
		}
		b.WriteString(prefetchNotePrefix)
		b.WriteString("dropped ")
		b.WriteString(d.Path)
		b.WriteString(" (")
		b.WriteString(d.Symbol)
		b.WriteString(", tier ")
		b.WriteString(d.Tier.String())
		b.WriteString(", ")
		b.WriteString(strconv.Itoa(d.Bytes))
		b.WriteString(" bytes) - over the pre-fetch byte cap\n")
	}
	return b.String()
}

// PrefetchContextPath is the sentinel Path carried by the Context Definitions
// FileEntry, mirroring ClaimLedgerPath. Like that one it is exported so callers
// can RECOGNIZE the entry, and like that one it is deliberately NOT what the
// byte-budget exemption keys on — a reviewed repository can legitimately contain
// a file named "<context>", and that file IS reviewable.
const PrefetchContextPath = "<context>"

// newPrefetchEntry builds the Context Definitions FileEntry.
//
// Size 0 and shedExempt mirror the claim ledger exactly, and for the same
// reason: the section's value depends on every reviewer in a fan-out seeing the
// SAME context, so a section some agents received and others did not would be
// worse than none. Its bytes are bounded by max_prefetch_bytes instead of by
// payload_byte_budget.
//
// The accepted consequences of carrying a synthetic section as a FileEntry are
// enumerated on ClaimLedgerPath and apply here too — with one PARTIALLY
// mitigated: buildPayloads derives its reported file count from ReviewableCount
// rather than len(kept), so a second synthetic entry does not inflate the count
// the manifest and the persona-visible {{.FileCount}} report for the range. The
// PER-AGENT re-derivations in internal/fanout's buildSlots still count synthetic
// entries and remain inflated; that half is tracked as technical debt rather
// than fixed here.
func newPrefetchEntry(section string) FileEntry {
	// exemptRank 0 is BELOW the claim ledger's 1, stated explicitly rather than
	// left to the zero value: when a budget cannot fund both synthetic sections,
	// retrieved context is the one that goes. Context is supporting material for
	// judging the diff; the ledger is the set of assertions being judged, and a
	// ledger some agents received and others did not is worse than absent.
	return FileEntry{Path: PrefetchContextPath, Size: 0, Body: section, shedExempt: true, exemptRank: 0}
}

// PrefetchStatus reports what the pre-fetch pass produced for a range.
//
// It exists for the reason ClaimLedgerStatus does: an absent section, a disabled
// feature and a failed lookup are byte-for-byte identical in every artifact
// otherwise, and they are opposite operational signals.
type PrefetchStatus struct {
	Present   bool `json:"present"`
	Snippets  int  `json:"snippets"`
	Dropped   int  `json:"dropped,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
	Disabled  bool `json:"disabled,omitempty"`
	Failed    bool `json:"failed,omitempty"`
}

// looksLikeTestFile reports whether rel is a test file, enabling the AC6
// mock-cue scan for it. The forms cover the FILENAME conventions of the
// languages astgroup embeds parsers for, not Go alone. Rust's inline #[cfg(test)]
// modules are out of scope: they live inside the production source file, so no
// filename form can identify them.
func looksLikeTestFile(rel string) bool {
	base := path.Base(rel)
	// The CamelCase conventions (PHPUnit UserTest.php, JUnit FooTest.java /
	// FooTests.java) are matched against the ORIGINAL name, case-significantly:
	// a case-folded suffix check would also admit ordinary files ending in the
	// letters t-e-s-t, like latest.php or attest.java.
	stem, _, _ := strings.Cut(base, ".")
	if strings.HasSuffix(stem, "Test") || strings.HasSuffix(stem, "Tests") {
		return true
	}
	lower := strings.ToLower(base)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasPrefix(lower, "test_") ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasSuffix(lower, "_test.rb") ||
		strings.Contains(lower, ".test.") ||
		strings.Contains(lower, ".spec.") ||
		strings.Contains(lower, "_spec.")
}

// buildPrefetch runs the whole pre-fetch pass for a range: extract the changed
// symbols, resolve them to consuming call sites, retrieve snippets, cap them,
// and render the section.
//
// It returns the rendered section, the retrieved spans keyed by path (for the
// grounding threading), and a status. It never returns an error: pre-fetching is
// an additional review input, so every failure degrades to empty context.
func (g *gitRunner) buildPrefetch(base, head string) (section string, spans map[string][]LineRange, status PrefetchStatus) {
	files, err := g.changedFilesMemo(base, head)
	if err != nil {
		g.log().Debug("payload: pre-fetch skipped, changed files unreadable", "error", err)
		return "", nil, PrefetchStatus{Failed: true}
	}
	if len(files) == 0 {
		return "", nil, PrefetchStatus{}
	}
	// The memoized whole-range zero-context split, which grounding and files-mode
	// sizing already consume — so the changed-line ranges cost NO additional git
	// process here.
	ranges, err := g.rangeChunks(base, head)
	if err != nil {
		g.log().Debug("payload: pre-fetch skipped, changed ranges unreadable", "error", err)
		return "", nil, PrefetchStatus{Failed: true}
	}

	// Both sides of a rename are excluded: the payload already carries the file,
	// and a snippet of it would re-show text the reviewer has.
	changedPaths := make(map[string]bool, len(files))
	for _, f := range files {
		changedPaths[f.path] = true
		if f.oldPath != "" {
			changedPaths[f.oldPath] = true
		}
	}

	var symbols []changedSymbol
	// seenIdx records each name's slot in symbols, not mere presence: a name
	// first collected from a changed TEST file (Mocked) must NOT eclipse the
	// same symbol the diff genuinely changed in production code — that record
	// REPLACES the mock-cue one in place, or AC5 call-site retrieval silently
	// narrows to declaration sites depending on which file the iteration
	// visited first.
	seenIdx := make(map[string]int)
	readFiles := 0
	for _, f := range files {
		if f.kind == kindDeleted {
			continue // nothing at HEAD to extract a symbol from
		}
		hunks := ranges[f.path]
		if len(hunks) == 0 {
			continue // binary or pure-deletion: no head lines to resolve
		}
		// The cheap guards run ABOVE the blob read: a file with no parser and no
		// test-file shape can contribute neither a declaration signature nor a
		// mock cue, so there is nothing to extract from it. Left below the read,
		// a 3000-file JSON/YAML/Markdown diff paid one `git show` per file — on a
		// cold escalation cache, 3000 real subprocesses — to learn nothing, and
		// the maxChangedSymbols ceiling never fires for files that yield no
		// symbols.
		lang := astgroup.LanguageForExt(strings.ToLower(path.Ext(f.path)))
		if lang == "" && !looksLikeTestFile(f.path) {
			continue
		}
		if readFiles >= maxPrefetchChangedFiles {
			// Same shape as the maxFetcherHits ceiling: a pathological all-parseable
			// diff whose files yield few or duplicate symbols would otherwise read
			// every changed blob. 250 files comfortably span the 40-symbol budget.
			g.log().Debug("payload: pre-fetch symbol extraction stopped at the changed-file ceiling",
				"ceiling", maxPrefetchChangedFiles, "symbols", len(symbols))
			break
		}
		readFiles++
		// ReuseMemo, never Memo: memoizing here would RETAIN the blob of a file the
		// escalation pass deliberately refused to read, defeating the
		// maxAnalyzeFileBytes ceiling that exists to stop a change set of
		// multi-megabyte generated files being held in memory for the life of the
		// range. When escalation already cached the blob this is free; otherwise it
		// reads without retaining.
		src, err := g.headContentReuseMemo(base, head, f.path)
		if err != nil || len(src) > maxAnalyzeFileBytes {
			continue
		}
		spanList := make([]LineRange, 0, len(hunks))
		for _, h := range hunks {
			spanList = append(spanList, LineRange{Start: h.start, End: h.end})
		}
		for _, s := range extractChangedSymbols(src, spanList, parsePrefetchTree(f.path, src), looksLikeTestFile(f.path), lang) {
			if idx, dup := seenIdx[s.Name]; dup {
				if s.Mocked || !symbols[idx].Mocked {
					continue
				}
				symbols[idx] = s // the diff-changed record wins over the mock-cue guess
				continue
			}
			seenIdx[s.Name] = len(symbols)
			symbols = append(symbols, s)
			if len(symbols) >= maxChangedSymbols {
				break
			}
		}
		if len(symbols) >= maxChangedSymbols {
			break
		}
	}
	// Laziness (AC4): a diff citing no resolvable symbol returns here, before any
	// `git grep` runs and before a single file outside the diff is read.
	if len(symbols) == 0 {
		return "", nil, PrefetchStatus{}
	}

	hits, lookupFailed := g.referenceHits(head, symbols, changedPaths)
	if lookupFailed {
		// A lookup that BROKE, not one that matched nothing. Both ship empty
		// context, but only this one is a malfunction worth reporting.
		return "", nil, PrefetchStatus{Failed: true}
	}
	if len(hits) == 0 {
		return "", nil, PrefetchStatus{}
	}
	// Symbols the cue scan guessed are retrieved only from their declaration
	// sites; symbols the diff genuinely changed keep full call-site retrieval.
	declOnly := make(map[string]bool, len(symbols))
	for _, s := range symbols {
		if s.Mocked {
			declOnly[s.Name] = true
		}
	}
	snips := g.retrieveSnippets(base, head, hits, declOnly)
	// Tier is stamped HERE rather than inside retrieveSnippets: retrieval is
	// tier-agnostic, and 35.16.12 adds a second producer feeding the same ledger.
	//
	// The signature is stamped in the same pass and for the same reason:
	// retrieval resolves a hit to a REGION, while the header belongs to the
	// changed symbol the region was retrieved for, which only this scope holds.
	sigByName := make(map[string]string, len(symbols))
	for _, s := range symbols {
		if s.Signature != "" {
			sigByName[s.Name] = s.Signature
		}
	}
	for i := range snips {
		snips[i].Tier = PrefetchTierReference
		snips[i].Signature = sigByName[snips[i].Symbol]
	}

	kept, dropped := capPrefetchSnippets(snips, g.maxPrefetchBytes)
	section = renderPrefetchSection(kept, dropped)
	if section == "" {
		return "", nil, PrefetchStatus{}
	}

	// Only KEPT snippets become groundable. A shed snippet was never shown, so
	// marking its lines groundable would let a finding cite code no reviewer saw.
	spans = make(map[string][]LineRange, len(kept))
	for _, s := range kept {
		spans[s.Path] = append(spans[s.Path], LineRange{Start: s.Start, End: s.End})
	}
	return section, spans, PrefetchStatus{
		Present:   true,
		Snippets:  len(kept),
		Dropped:   len(dropped),
		Truncated: len(dropped) > 0,
	}
}

// identifierTokens splits line into identifier-shaped runs, in source order.
//
// It is a lexer-free scan for the same reason internal/reconcile's own token
// harvest is: it must work for every language whose parser this binary embeds,
// and for the ones it does not. Duplicates are left in; the caller dedupes
// against the symbols it has already collected.
func identifierTokens(line string) []string {
	var out []string
	start := -1
	for i := 0; i <= len(line); i++ {
		word := i < len(line) && (line[i] == '_' ||
			(line[i] >= 'a' && line[i] <= 'z') ||
			(line[i] >= 'A' && line[i] <= 'Z') ||
			(line[i] >= '0' && line[i] <= '9'))
		switch {
		case word && start < 0:
			start = i
		case !word && start >= 0:
			out = append(out, line[start:i])
			start = -1
		}
	}
	return out
}
