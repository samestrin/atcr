package payload

import (
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
	"nil_": true, "for": true, "not": true, "the": true, "out": true,
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
// A token that itself contains a cue is rejected: `mockReadStore`, `patched` and
// `fakeClock` name the DOUBLE, and retrieving the double's own definition puts
// the substitute next to the substitute. The symbol worth showing beside a mock
// is the thing being mocked.
func plausibleMockTarget(tok string) bool {
	if len(tok) < minMockTokenLen {
		return false
	}
	lower := strings.ToLower(tok)
	if mockTokenNoise[lower] {
		return false
	}
	for _, cue := range mockCues {
		if strings.Contains(lower, cue) {
			return false
		}
	}
	// A token that is all digits (or starts with one) is a literal, not a symbol.
	return !(tok[0] >= '0' && tok[0] <= '9')
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
