package payload

import (
	"path"
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
	for _, r := range ranges {
		for line := r.Start; line <= r.End; line++ {
			if scanned >= maxScannedChangedLines || len(out) >= maxChangedSymbols {
				return out
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
	for _, r := range ranges {
		for line := r.Start; line <= r.End; line++ {
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
	return !(tok[0] >= '0' && tok[0] <= '9')
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
func parseGrepHits(out string, symbols []string, exclude map[string]bool, maxPerSymbol int) []refHit {
	if out == "" || len(symbols) == 0 || maxPerSymbol <= 0 {
		return nil
	}
	perSymbol := make(map[string]int, len(symbols))
	var hits []refHit
	for _, line := range strings.Split(out, "\n") {
		p, num, text, ok := splitGrepLine(line)
		if !ok || exclude[p] {
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
	return !(name[0] >= '0' && name[0] <= '9')
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
func (g *gitRunner) referenceHits(symbols []changedSymbol, exclude map[string]bool) []refHit {
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
	args := make([]string, 0, 6+2*len(names))
	args = append(args, "grep", "-n", "-I", "-F", "-w", "--no-color")
	for _, n := range names {
		args = append(args, "-e", n)
	}
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
	return parseGrepHits(string(out), names, exclude, maxPrefetchSitesPerSymbol)
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
