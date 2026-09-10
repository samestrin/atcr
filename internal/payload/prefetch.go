package payload

import (
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

	// maxScannedChangedLines bounds the per-file line walk. A whole-file addition
	// presents one range spanning every line, and the symbol resolution below is
	// per line; without this a single large generated file would walk hundreds of
	// thousands of lines to yield the handful of declarations the cap above
	// already allows. Hitting it can only UNDER-collect, which degrades to less
	// context rather than to a wrong answer.
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
// builtins, and the types that appear on almost every Go test line. They are
// excluded so the `git grep` argv is spent on plausible symbols.
var mockTokenNoise = map[string]bool{
	"func": true, "return": true, "nil": true, "err": true, "error": true,
	"byte": true, "string": true, "int": true, "int64": true, "bool": true,
	"var": true, "const": true, "type": true, "struct": true, "interface": true,
	"map": true, "chan": true, "range": true, "defer": true, "package": true,
	"import": true, "true": true, "false": true, "len": true, "cap": true,
	"make": true, "new": true, "append": true, "testing": true, "test": true,
	"for": true, "not": true, "the": true, "out": true,
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
func extractChangedSymbols(src string, ranges []LineRange, root astgroup.Node, isTest bool) []changedSymbol {
	if len(ranges) == 0 {
		return nil
	}
	lines := strings.Split(src, "\n")
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
			if !ok || name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, changedSymbol{Name: name, Signature: signatureFor(skeleton, name, line)})
		}
	}

	if !isTest {
		return out
	}

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
				if !plausibleMockTarget(tok) || seen[tok] || declared[tok] {
					continue
				}
				seen[tok] = true
				out = append(out, changedSymbol{Name: tok, Mocked: true})
			}
		}
	}
	return out
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
func plausibleMockTarget(tok string) bool {
	if len(tok) < minMockTokenLen {
		return false
	}
	lower := strings.ToLower(tok)
	if mockTokenNoise[lower] || doubleNameExact[lower] {
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
	// maxPrefetchSitesPerSymbol bounds how many consumer sites ONE symbol may
	// contribute. Without it a very common name (Close, Run, New) fills the whole
	// candidate set before any other changed symbol is represented, and the byte
	// cap then sheds the symbols that actually needed context.
	maxPrefetchSitesPerSymbol = 3

	// maxPrefetchHits is the absolute ceiling on candidate sites from one lookup,
	// independent of how many symbols contributed them. Each surviving hit costs a
	// HEAD blob read and a parse downstream, which is where the AC4 latency budget
	// is actually spent, so this is the bound that keeps a very wide diff from
	// turning a ~20ms lookup into a repo-wide sweep by another name.
	maxPrefetchHits = 60
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
func parseGrepHits(out string, symbols []string, exclude map[string]bool, skip func(string) bool, maxPerSymbol int) []refHit {
	if out == "" || len(symbols) == 0 || maxPerSymbol <= 0 {
		return nil
	}
	perSymbol := make(map[string]int, len(symbols))
	var hits []refHit
	for _, line := range strings.Split(out, "\n") {
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
		if len(hits) >= maxPrefetchHits {
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
// `git grep` also exits non-zero when it simply matched nothing, which
// gitRunner.output cannot distinguish from a real failure, so treating any error
// as "no context" is the only correct reading available here.
func (g *gitRunner) referenceHits(head string, symbols []changedSymbol, exclude map[string]bool) []refHit {
	names := grepPatterns(symbols)
	if len(names) == 0 {
		// The laziness contract: a diff citing no resolvable symbol spawns no
		// process and reads no source file. Returning BEFORE g.output is what makes
		// that observable through execCount.
		return nil
	}
	// ONE process for every symbol. `-F` makes each pattern a fixed string (never
	// a regex), `-w` bounds it to whole words so `Store` does not match
	// `ReadStore`, and `-I` skips binaries. Each name is introduced by `-e`, so a
	// value can never be read as a flag.
	args := make([]string, 0, 7+2*len(names))
	args = append(args, "grep", "-n", "-I", "-F", "-w", "--no-color")
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
	args = append(args, head)
	out, err := g.output(args...)
	if err != nil {
		// `git grep` exits non-zero on NO MATCH as well as on failure, and
		// gitRunner.output collapses both into one error, so the two are not
		// separable here. Both degrade to empty context: pre-fetching is an
		// additional review input, and failing a review because a lookup found
		// nothing would trade a complete review for none at all.
		g.log().Debug("payload: reference lookup matched nothing or failed; review proceeds without pre-fetched context",
			"symbols", len(names), "error", err)
		return nil
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
	return parseGrepHits(stripGrepRev(string(out), head), names, exclude, skip, maxPrefetchSitesPerSymbol)
}

// stripGrepRev removes the leading "<rev>:" field that `git grep <rev>` prefixes
// to every record, restoring the plain "path:line:text" shape splitGrepLine
// parses.
//
// The prefix is git echoing back the tree-ish argument verbatim, so trimming
// that exact string is exact rather than heuristic — unlike splitting on the
// third colon, which a path containing a colon would defeat.
func stripGrepRev(out, rev string) string {
	if rev == "" {
		return out
	}
	prefix := rev + ":"
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimPrefix(ln, prefix)
	}
	return strings.Join(lines, "\n")
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
	if block, _, ok := astgroup.CoveringBlock(root, line); ok && block.StartLine > 0 && block.EndLine >= block.StartLine {
		lo, hi = block.StartLine, block.EndLine
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
		if lo < 1 {
			lo = 1
		}
		hi = lo + maxSnippetLines - 1
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
func sliceLines(src string, start, end int) (body string, s, e int, ok bool) {
	if src == "" {
		return "", 0, 0, false
	}
	lines := strings.Split(src, "\n")
	// A trailing newline yields a final empty element that is not a real line.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
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
func (g *gitRunner) retrieveSnippets(base, head string, hits []refHit) []PrefetchSnippet {
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
		emitted := make([][2]int, 0, len(byPath[rel]))
		for _, h := range byPath[rel] {
			start, end := snippetSpan(root, h.Line)
			body, s, e, ok := sliceLines(src, start, end)
			if !ok || overlapsEmitted(emitted, s, e) {
				continue
			}
			emitted = append(emitted, [2]int{s, e})
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
// maxBytes <= 0 keeps nothing — the operator disabled the feature — and still
// records every drop, so "turned off" and "retrieved nothing" stay
// distinguishable in the artifacts.
func capPrefetchSnippets(snips []PrefetchSnippet, maxBytes int64) (kept []PrefetchSnippet, dropped []PrefetchDrop) {
	if len(snips) == 0 {
		return nil, nil
	}
	if maxBytes <= 0 {
		// Disabled by the operator. Keep nothing, but still record every snippet:
		// "you turned it off" and "retrieval found nothing" are opposite
		// operational signals, and an empty section reports them identically.
		return splitPrefetchLedger(snips, allTrue(len(snips)))
	}

	var total int64
	for _, s := range snips {
		total += int64(len(s.Body))
	}
	if total <= maxBytes {
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
		if len(si.Body) != len(sj.Body) {
			return len(si.Body) > len(sj.Body)
		}
		if si.Path != sj.Path {
			return si.Path < sj.Path
		}
		return si.Symbol < sj.Symbol
	})

	drop := make([]bool, len(snips))
	used := total
	for _, i := range idx {
		if used <= maxBytes {
			break
		}
		// Whole snippets only. A truncated snippet reads as a complete function to
		// the reviewer, which is worse than its absence: it invites a finding about
		// logic that was simply cut off.
		drop[i] = true
		used -= int64(len(snips[i].Body))
	}
	return splitPrefetchLedger(snips, drop)
}

// splitPrefetchLedger partitions snips by the drop mask, preserving the original
// order in BOTH results so the rendered section and its ledger are deterministic
// (AC3).
func splitPrefetchLedger(snips []PrefetchSnippet, drop []bool) (kept []PrefetchSnippet, dropped []PrefetchDrop) {
	for i, s := range snips {
		if drop[i] {
			dropped = append(dropped, PrefetchDrop{
				Path:   s.Path,
				Symbol: s.Symbol,
				Tier:   s.Tier,
				Bytes:  len(s.Body),
			})
			continue
		}
		kept = append(kept, s)
	}
	return kept, dropped
}

// allTrue returns an n-length mask with every position set.
func allTrue(n int) []bool {
	mask := make([]bool, n)
	for i := range mask {
		mask[i] = true
	}
	return mask
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
)

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
		b.WriteString(")\n")
		// Anchor every source line with its real HEAD line number. This is the
		// safety property as much as a convenience: because each content line
		// begins with "L<digits>: ", a retrieved body carrying a section marker
		// cannot open a spoofed file section.
		line := s.Start
		for _, src := range strings.Split(s.Body, "\n") {
			b.WriteByte('L')
			b.WriteString(strconv.Itoa(line))
			b.WriteString(": ")
			b.WriteString(src)
			b.WriteByte('\n')
			line++
		}
	}

	for _, d := range dropped {
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

	b.WriteString(prefetchSectionEnd)
	b.WriteByte('\n')
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
	return FileEntry{Path: PrefetchContextPath, Size: 0, Body: section, shedExempt: true}
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
// mock-cue scan for it. The forms cover the conventions of the languages
// astgroup embeds parsers for, not Go alone.
func looksLikeTestFile(rel string) bool {
	base := strings.ToLower(path.Base(rel))
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.py") ||
		strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") ||
		strings.Contains(base, "_spec.")
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
	seen := make(map[string]bool)
	for _, f := range files {
		if f.kind == kindDeleted {
			continue // nothing at HEAD to extract a symbol from
		}
		hunks := ranges[f.path]
		if len(hunks) == 0 {
			continue // binary or pure-deletion: no head lines to resolve
		}
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
		for _, s := range extractChangedSymbols(src, spanList, parsePrefetchTree(f.path, src), looksLikeTestFile(f.path)) {
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
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

	hits := g.referenceHits(head, symbols, changedPaths)
	if len(hits) == 0 {
		return "", nil, PrefetchStatus{}
	}
	snips := g.retrieveSnippets(base, head, hits)
	// Tier is stamped HERE rather than inside retrieveSnippets: retrieval is
	// tier-agnostic, and 35.16.12 adds a second producer feeding the same ledger.
	for i := range snips {
		snips[i].Tier = PrefetchTierReference
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
