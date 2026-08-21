package reconcile

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// reviewFileName is the per-source narrative file each reviewer (pool agent
	// and host) writes alongside its findings.txt. It mirrors internal/fanout's
	// reviewFile; kept local to avoid an import cycle (fanout is a consumer, not a
	// dependency, of this package).
	reviewFileName = "review.md"
	// justificationMaxRunes caps an extracted narrative so a verbose review.md
	// section cannot bloat findings.json. The excerpt is the human-readable
	// pointer; SourceReport is the precise back-reference to the full detail.
	justificationMaxRunes = 1000
	// minAnchorTier is the weakest anchorTier accepted as a match. A bare file
	// mention (tier 1) is rejected: a file named only in an "Areas examined — no
	// issues found" line would otherwise attach a misleading no-issue narrative to
	// a real finding. A line-level reference (tier 2+) is required so the extracted
	// section is genuinely about the finding, per AC1's "relevant narrative" and
	// the plan's "match by file:line".
	minAnchorTier = 2
	// anchorLineProximity bounds a non-covering tier-2 match. A source's review.md
	// and its findings.txt are written in the same review run, so a legitimate
	// same-finding reference is exact (tier 3) or off by only the small divergence
	// cluster-merge introduces when it adopts a neighboring reviewer's line as the
	// merged line — the engine clusters findings by FILE:LINE ±3. A same-file
	// reference to a line farther than this is a DIFFERENT finding in that file,
	// not this one, so it must not attach its narrative here.
	anchorLineProximity = 3
	// maxReviewBytes caps a single source review.md read. Review narratives are
	// excerpts (justificationMaxRunes), so a multi-megabyte file under sources/
	// (an open extension point) is not worth fully materializing.
	maxReviewBytes = 1 << 20 // 1 MiB
)

// ElidedQuotePlaceholder is the exact line extractSection substitutes for each
// fenced region it drops from a justification excerpt (markers included). It is a
// named constant rather than an inline literal because the value is written into a
// PERSISTED, human- and LLM-read field: a consumer that needs to tell "atcr dropped
// a quote here" from "the reviewer wrote this" — cli/debt_resolve deciding whether a
// justification is real recorded rationale, a docs example, a test — has to match on
// something, and a literal spelled in five places cannot be matched reliably.
// Documented for readers of the field in docs/findings-format.md and
// skills/atcr/debt-resolve.md; changing it is a documented-surface change.
const ElidedQuotePlaceholder = "[quoted example elided]"

// reviewNarrative is one collected source review.md: its review-dir-relative path
// (the SourceReport.Path a consumer resolves against the review dir), the leaf
// directory name (host, or a pool agent name — used as a soft reviewer-match
// tiebreak), and the file split into lines.
type reviewNarrative struct {
	relPath string
	leaf    string
	lines   []string
}

// narrativeMatch is the best-effort correlation of a finding to a review.md
// section: the extracted (truncated) narrative plus the back-reference location.
type narrativeMatch struct {
	text    string
	relPath string
	line    int // 1-based line in the review.md where the file:line anchor matched
	section string
}

// stampJustifications correlates each reconciled finding to the narrative its
// originating source wrote in review.md (Epic 18.2) and stamps the extracted
// section onto Justification, so a downstream TD-resolution consumer inherits the
// reviewer's reasoning instead of re-deriving it from raw review.md files.
//
// The match is best-effort by file:line against every source review.md under
// reviewDir/sources; a finding with no matching narrative is left untouched
// (Justification stays empty), so findings.json is byte-identical to pre-18.2
// output whenever no review.md pairs with a finding. It mutates jf in place and
// is a no-op when reviewDir is empty, no review.md files exist, or jf is empty.
func stampJustifications(jf []JSONFinding, reviewDir string) {
	if reviewDir == "" || len(jf) == 0 {
		return
	}
	narratives := collectReviewNarratives(filepath.Join(reviewDir, sourcesSubdir), reviewDir)
	if len(narratives) == 0 {
		return
	}
	// Pre-index every narrative line once so each finding scans only the lines
	// that mention its file, instead of every line of every narrative (the
	// pre-18.2 hot path was O(findings x narrativeBytes)).
	index := buildAnchorIndex(narratives)
	matched := 0
	for i := range jf {
		m, ok := matchNarrative(narratives, index, jf[i].File, jf[i].Line, jf[i].Reviewers)
		if !ok {
			continue
		}
		jf[i].Justification = m.text
		jf[i].SourceReport = &SourceReport{Path: m.relPath, Line: m.line, Section: m.section}
		matched++
	}
	if matched == 0 {
		slog.Warn("justifications stamped", "matched", matched, "total", len(jf), "note", "review.md narratives exist but matched zero findings; possible format drift")
	} else {
		slog.Debug("justifications stamped", "matched", matched, "total", len(jf))
	}
}

// collectReviewNarratives walks sourcesDir for every review.md and returns them
// sorted by review-dir-relative path (deterministic ordering so findings.json is
// reproducible run to run). Unreadable files/subtrees are skipped, not fatal —
// sources/ is an open extension point, same resilience stance as Discover.
func collectReviewNarratives(sourcesDir, reviewDir string) []reviewNarrative {
	var out []reviewNarrative
	_ = filepath.WalkDir(sourcesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// IsRegular() excludes symlinks/FIFOs/devices named review.md, matching
		// leafFindingsFiles' refusal to read a non-regular findings.txt.
		if !d.Type().IsRegular() || d.Name() != reviewFileName {
			return nil
		}
		if fi, serr := os.Stat(path); serr == nil && fi.Size() > maxReviewBytes {
			slog.Debug("skipping oversized review.md", "path", path, "size", fi.Size())
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(reviewDir, path)
		if rerr != nil {
			// Cannot express the path relative to the review dir (e.g. different
			// volume). Skip it rather than leak an absolute filesystem path into
			// source_report.path, whose documented contract is review-dir-relative.
			return nil
		}
		out = append(out, reviewNarrative{
			relPath: filepath.ToSlash(rel),
			leaf:    filepath.Base(filepath.Dir(path)),
			lines:   strings.Split(string(data), "\n"),
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].relPath < out[j].relPath })
	return out
}

// anchorRef locates one review.md line that carries a file:line anchor: the
// narrative index and the line index within that narrative's lines.
type anchorRef struct {
	ni int
	li int
}

// anchorIndex maps a referenced file path to every review.md line that mentions
// it with a line anchor (`<path>:<digit>`). The key is the MAXIMAL path token
// preceding the colon, which mirrors anchorTier's suffix-path rejection: a line
// referencing `internal/x/y.go:42` is indexed only under `internal/x/y.go`, so a
// finding for `y.go` finds no candidate — identical to the full-scan behavior.
type anchorIndex map[string][]anchorRef

// buildAnchorIndex scans every narrative line once, recording under each
// referenced file path the (narrative, line) positions that mention it. Built
// once per stampJustifications run and shared across all findings, replacing the
// pre-18.2 per-finding full rescan (O(findings x narrativeBytes)). anchorTier
// still scores the exact tier per finding on the real line, so match results are
// unchanged — this only prunes the candidate set each finding considers.
func buildAnchorIndex(narratives []reviewNarrative) anchorIndex {
	idx := make(anchorIndex)
	for ni := range narratives {
		for li, lt := range narratives[ni].lines {
			indexLineFiles(idx, lt, ni, li)
		}
	}
	return idx
}

// indexLineFiles records, for line lt at position (ni,li), each distinct file
// path appearing as `<path>:<digit>` — the maximal run of path chars ending at a
// colon that is followed by a digit. Deduped per line so a file mentioned twice
// on one line yields a single ref (anchorTier already scans every occurrence on
// the line for the best tier).
func indexLineFiles(idx anchorIndex, lt string, ni, li int) {
	var seen []string // files already recorded for this line; a line rarely holds
	// more than one anchor, so a nil-until-needed slice with a linear dedup scan
	// avoids allocating a map for the common 0-1 anchor case.
	for c := 0; c < len(lt); c++ {
		if lt[c] != ':' {
			continue
		}
		// A parseable line reference requires a digit right after the colon.
		if c+1 >= len(lt) || lt[c+1] < '0' || lt[c+1] > '9' {
			continue
		}
		// Walk left over path chars to recover the maximal path token.
		start := c
		for start > 0 && isPathChar(lt[start-1]) {
			start--
		}
		if start == c {
			continue // colon with no path token before it
		}
		file := lt[start:c]
		if containsString(seen, file) {
			continue
		}
		seen = append(seen, file)
		idx[file] = append(idx[file], anchorRef{ni: ni, li: li})
	}
}

// containsString reports whether s appears in xs. Used for per-line anchor dedup
// where xs is at most a couple of entries, so a linear scan beats a map.
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// matchNarrative finds the review.md section that best references file:line and
// returns its extracted narrative + back-reference. Selection is deterministic:
// highest anchor tier wins (a line/range covering the finding's exact line beats
// a same-file-other-line reference beats a bare file mention); ties break toward
// a narrative whose leaf dir is one of the finding's reviewers, then toward the
// earliest narrative (sorted), then the earliest line. Returns ok=false when no
// review.md mentions the finding's file at all.
func matchNarrative(narratives []reviewNarrative, index anchorIndex, file string, line int, reviewers []string) (narrativeMatch, bool) {
	if file == "" {
		return narrativeMatch{}, false
	}
	refs := index[file]
	if len(refs) == 0 {
		return narrativeMatch{}, false
	}
	revSet := toSet(reviewers)
	bestTier, bestRev, bestNarr, bestLine := 0, false, -1, -1
	// refs are already in (ni, li) order (buildAnchorIndex scans in that order),
	// and anchorTier re-scores each candidate line for this finding's line, so the
	// winner is identical to the full-scan version — this only prunes the lines
	// considered from all lines to the handful that mention file.
	for _, r := range refs {
		tier := anchorTier(narratives[r.ni].lines[r.li], file, line)
		if tier < minAnchorTier {
			continue
		}
		revPref := revSet[narratives[r.ni].leaf]
		if beatsMatch(tier, revPref, r.ni, r.li, bestTier, bestRev, bestNarr, bestLine) {
			bestTier, bestRev, bestNarr, bestLine = tier, revPref, r.ni, r.li
		}
	}
	if bestNarr < 0 {
		return narrativeMatch{}, false
	}
	text, section := extractSection(narratives[bestNarr].lines, bestLine)
	if text == "" {
		return narrativeMatch{}, false
	}
	return narrativeMatch{
		text:    text,
		relPath: narratives[bestNarr].relPath,
		line:    bestLine + 1, // 1-based
		section: section,
	}, true
}

// beatsMatch reports whether a candidate anchor (tier/reviewer-preference/
// narrative-index/line-index) is strictly better than the current best, applying
// the deterministic tiebreak order documented on matchNarrative.
func beatsMatch(tier int, revPref bool, ni, li, bTier int, bRev bool, bNi, bLi int) bool {
	if bNi < 0 {
		return true
	}
	if tier != bTier {
		return tier > bTier
	}
	if revPref != bRev {
		return revPref
	}
	if ni != bNi {
		return ni < bNi
	}
	return li < bLi
}

// anchorTier scores how strongly a single review.md line references file:line:
//
//	3 — a `file:N` or `file:A-B` reference whose N (or range A..B) covers line
//	2 — a `file:N` reference to the same file within anchorLineProximity of line
//	0 — no usable line reference (bare file mention is intentionally ignored
//	    because minAnchorTier is 2; returning 1 would be treated the same as 0).
//
// The `file:` scan handles both a bare `internal/x.go:42` anchor and a range
// `internal/x.go:65-102`; the char after the number is not required to be a
// non-digit because parseLineRange consumes the full leading integer run. Each
// `file:` occurrence is rejected when it is a suffix of a longer path token (the
// char to its left is a path/identifier char), so a finding for "y.go" does not
// falsely match a line referencing "internal/x/y.go:42".
func anchorTier(s, file string, line int) int {
	if line <= 0 {
		// File-level finding with no specific line: proximity/covering matching makes
		// no sense and would attach arbitrary early-line narratives.
		return 0
	}
	best := 0
	needle := file + ":"
	from := 0
	for {
		p := strings.Index(s[from:], needle)
		if p < 0 {
			break
		}
		abs := from + p
		from = abs + len(needle)
		if abs > 0 && isPathChar(s[abs-1]) {
			continue // suffix of a longer path token — not this file
		}
		lo, hi, ok := parseLineRange(s[abs+len(needle):])
		if !ok {
			continue
		}
		if line >= lo && line <= hi {
			return 3 // covering reference — best possible, stop early
		}
		// Non-covering same-file reference: a weaker match only within the small
		// proximity window (beyond it, a different finding in the same file).
		if best < 2 && lineDistance(line, lo, hi) <= anchorLineProximity {
			best = 2
		}
	}
	return best
}

// lineDistance returns how far line lies outside the inclusive [lo,hi] interval
// (0 when inside).
func lineDistance(line, lo, hi int) int {
	switch {
	case line < lo:
		return lo - line
	case line > hi:
		return line - hi
	default:
		return 0
	}
}

// isPathChar reports whether b can be part of a path/identifier token, used to
// detect when a matched file path is actually the tail of a longer path.
func isPathChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '/' || b == '.' || b == '_' || b == '-':
		return true
	}
	return false
}

// parseLineRange parses a leading integer, optionally followed by "-integer",
// from s and returns the inclusive [lo,hi] plus ok. "166" → (166,166); "65-102"
// → (65,102). A non-numeric prefix returns ok=false.
func parseLineRange(s string) (lo, hi int, ok bool) {
	lo, n := leadingInt(s)
	if n == 0 {
		return 0, 0, false
	}
	hi = lo
	if rest := s[n:]; strings.HasPrefix(rest, "-") {
		if h, n2 := leadingInt(rest[1:]); n2 > 0 {
			hi = h
		}
	}
	if hi < lo {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

// leadingInt returns the integer value of the leading ASCII-digit run of s and
// its byte width (0 width when s does not start with a digit).
func leadingInt(s string) (val, width int) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, 0
	}
	v, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, 0
	}
	return v, i
}

// extractSection returns the narrative block containing anchor line idx and the
// nearest enclosing Markdown heading. The block is the run of contiguous
// non-blank lines around idx bounded by a blank line, a heading, or a new list
// item — so a per-finding list item or paragraph is captured without bleeding
// into a sibling item (a tight, blank-line-free findings list) or the next
// section. A heading or list-item marker may START the block but is never
// crossed. None of the three boundary shapes counts inside a fenced block, and
// neither does a heading when resolving the enclosing section — fenced content is
// quoted example text, not structure. The result is trimmed and truncated to
// justificationMaxRunes.
func extractSection(lines []string, idx int) (text, section string) {
	if idx < 0 || idx >= len(lines) {
		return "", ""
	}
	// Anything inside a fenced block is an EXAMPLE, not structure — the parser skips
	// fenced content, so treating it as a boundary would split a narrative on the
	// very counter-example the fence exists to quote. Resolved per line index rather
	// than per string because fence membership is a property of position, not text.
	//
	// The predicates do NOT all read the same mask. recordAt reads the STRICT view —
	// its contract is byte-exact parser parity, and a record-shaped line in the tail
	// of an unterminated fence was never emitted by the parser, so it is a boundary
	// to nothing here either. headingAt, itemAt and the section walk-up read the
	// RELEASED view, which un-masks that tail: a model quoting an example that
	// contains a `# heading` or a `- bullet` — the documented reason this parser
	// tracks fences at all — otherwise still truncated the narrative mid-sentence
	// with no marker, the section walk-up could still attribute the excerpt to a
	// heading that only exists inside a quote, and a dangling opener would take the
	// whole rest of the document with it. The loss is permanent either way:
	// localdebt persists Justification into an append-only store whose id excludes
	// it.
	strict, released, balanced := fenceMask(lines)
	headingAt := func(j int) bool { return !released[j] && isHeadingLine(lines[j]) }
	itemAt := func(j int) bool { return !released[j] && isItemStart(lines[j]) }
	recordAt := func(j int) bool { return !strict[j] && isFindingRecordStart(lines[j]) }

	for j := idx; j >= 0; j-- {
		if released[j] {
			continue
		}
		if h, ok := headingText(lines[j]); ok {
			section = h
			break
		}
	}

	// Walk up until the current line begins the block (heading or list item) or
	// the line above is a blank / heading / new list item. If the anchor landed on
	// a continuation line, absorb the list-item marker line above it so the finding
	// headline is included in the excerpt.
	start := idx
	for start > 0 && !headingAt(start) && !itemAt(start) && !recordAt(start) {
		if strings.TrimSpace(lines[start-1]) == "" || headingAt(start-1) {
			break
		}
		// A finding record begins its OWN block and is never absorbed — unlike a list
		// marker, it is not a headline for the line below it. Without this the walk
		// swallowed the preceding record(s) and two findings anchored at different
		// lines received byte-identical justification text.
		if recordAt(start - 1) {
			break
		}
		if itemAt(start - 1) {
			start-- // absorb the marker line, then stop
			break
		}
		start--
	}
	// Walk down until the next line starts a new item/section/record or is blank.
	end := idx
	for end < len(lines)-1 {
		if strings.TrimSpace(lines[end+1]) == "" || headingAt(end+1) || itemAt(end+1) || recordAt(end+1) {
			break
		}
		end++
	}
	var b strings.Builder
	wroteAny := false
	// wroteProse tracks whether any NON-elided line reached the builder. An excerpt
	// made of nothing but placeholders carries zero reviewer content, and the
	// caller's `text == ""` guard is what turns that into the documented "no
	// narrative" state instead of a Justification (and a SourceReport pointing
	// inside a quoted example) that no later reconcile can replace.
	wroteProse := false
	inElidedFence := false
	for j := start; j <= end; j++ {
		// Fenced content is quoted example text, not the reviewer's prose. The walk
		// crosses it deliberately (headingAt/itemAt are masked, so a quoted
		// `- line` or `# heading` no longer ends the block), but absorbing it whole
		// would spend the justificationMaxRunes budget on quoted code and push the
		// actual prose past the truncation ellipsis. Elide each fenced region —
		// markers included — to ONE placeholder line, so the budget buys prose on
		// both sides of the fence.
		// A marker belongs to the elision when it is half of a TERMINATED pair —
		// keyed on the pairing fenceMask already resolved rather than on whether a
		// neighbouring line happens to be masked, so an EMPTY fence (whose markers
		// have no masked neighbour in either direction) is absorbed like any other
		// quote instead of rendering two bare ``` lines as reviewer prose. A
		// DANGLING opener's marker stays: it is in no pair, its tail is released
		// (treated as prose), and the marker is the only sign a quote was opened.
		elidedLine := released[j] || balanced[j]
		if elidedLine {
			if !inElidedFence {
				if wroteAny {
					b.WriteByte('\n')
				}
				b.WriteString(ElidedQuotePlaceholder)
				wroteAny = true
				inElidedFence = true
			}
			continue
		}
		inElidedFence = false
		if wroteAny {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(lines[j], "\r"))
		wroteAny = true
		wroteProse = true
		// Bound block growth: we only keep justificationMaxRunes runes, so stop
		// accumulating once we are clearly over the limit. truncateRunes cleans up
		// the exact boundary.
		if b.Len() >= justificationMaxRunes {
			break
		}
	}
	if !wroteProse {
		return "", section
	}
	return truncateRunes(strings.TrimSpace(b.String()), justificationMaxRunes), section
}

// recordRe mirrors internal/stream's severityRe (parser.go:53) — the regexp that
// decides which review.md lines actually become findings. Restated rather than
// imported because it is unexported there; TestIsFindingRecordStart_AgreesWithThe
// ProducingParser pins the two together against stream.ParseModelOutput itself, so
// the restatement cannot drift silently.
var recordRe = regexp.MustCompile(`^(CRITICAL|HIGH|MEDIUM|LOW)\|`)

// isFindingRecordStart reports whether s begins a raw pipe-delimited finding record —
// SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST|EVIDENCE, the shape reviewer review.md
// narratives carry alongside prose. Such records sit on consecutive lines with no
// blank separator, so without treating them as block boundaries extractSection folds
// several into one excerpt and stamps one finding's reasoning onto its neighbour.
//
// It matches the PRODUCING PARSER EXACTLY, and the exactness is the whole contract.
// fanout writes review.md as a byte-identical copy of the content it hands to
// stream.ParseModelOutput, so a line the parser calls prose IS prose — and ending a
// block on it truncates a reviewer's narrative with no marker to show it happened.
// That loss is permanent: localdebt persists Justification into an append-only store
// whose id excludes it, so the first reconcile is the only one that can be right.
//
// Hence all three of the parser's conditions, not just the first:
//   - column-0 anchored, uppercase, pipe adjacent (recordRe, mirroring parser.go:162);
//   - at least three fields with a non-empty location (mirroring parser.go:168, which
//     drops degenerate severity-prefixed noise like a bare "HIGH|").
//
// An EARLIER revision keyed on SeverityRank/NormalizeSeverity after trimming, and
// claimed that made drift impossible. It did the opposite: NormalizeSeverity accepts
// any case and TrimSpace erased the anchoring, so `  high|`, `   High | ...` (an
// ordinary markdown table row) and `HIGH |...` were all read as records. Fence state
// is handled by the caller — see fenceMask.
func isFindingRecordStart(s string) bool {
	if !recordRe.MatchString(s) {
		return false
	}
	fields := strings.Split(s, "|")
	return len(fields) >= 3 && strings.TrimSpace(fields[1]) != ""
}

// fenceMask reports, per line, whether it sits INSIDE a fenced code block, in TWO
// views. strict matches the toggle-then-continue order in stream/parser.go:152-158
// byte-for-byte: fence markers are OUTSIDE, and an UNTERMINATED fence masks to EOF,
// exactly as the parser's bare `inFence = !inFence` skips every line below a
// dangling opener. released is identical except that the run below a dangling
// opener is un-masked.
//
// balanced is a third, disjoint signal: it marks the MARKER lines of every
// TERMINATED pair (both views leave markers themselves unmasked, so no mask can
// answer this). A dangling opener is in no pair and is therefore not marked. It
// exists so the excerpt builder can absorb a fence's markers into the elision by
// their PAIRING rather than by whether a neighbouring line happens to be masked —
// an empty fence has no masked neighbour in either direction, and keying on that
// rendered its two bare ``` lines as reviewer prose.
//
// Callers pick the view per predicate. recordAt reads strict: its contract is
// byte-exact parser parity (isFindingRecordStart), and a record-shaped line in that
// tail is a line the producing parser never emitted, so it must not act as a block
// boundary here either. headingAt, itemAt and the section walk-up read released:
// left masked, a model that opens a fence and forgets to close it takes the whole
// rest of the document with it, and every finding below the fence collapses to one
// byte-identical excerpt attributed to the last heading ABOVE it. Reading the tail
// as ordinary structure can at worst end a block on a boundary that was meant to be
// quoted; reading it as one fenced blob loses every boundary there is. The damage
// is also permanent — Justification is persisted append-only and StampID does not
// cover it — so the failure directions are not symmetric and this split is chosen
// deliberately.
//
// It exists because a model quoting an example row while explaining the format is the
// documented reason the parser tracks fences at all — that row carries a real severity
// prefix at column 0 and is otherwise indistinguishable from a record. Without the
// same state here, the boundary detector splits a narrative on the parser's own
// counter-example.
func fenceMask(lines []string) (strict, released, balanced []bool) {
	strict = make([]bool, len(lines))
	balanced = make([]bool, len(lines))
	inFence := false
	openedAt := -1
	for i, l := range lines {
		if isFenceMarker(l) {
			if inFence {
				inFence = false
				// A TERMINATED pair: both markers delimit a quoted example, whether
				// or not there is anything between them.
				balanced[openedAt], balanced[i] = true, true
			} else {
				inFence, openedAt = true, i
			}
			continue
		}
		strict[i] = inFence
	}
	// An UNTERMINATED fence releases the tail in the released view only. Every
	// earlier balanced pair keeps its mask in both views; the strict view keeps the
	// tail masked because the parser emits nothing below the dangling opener.
	released = strict
	if inFence {
		released = make([]bool, len(lines))
		copy(released, strict)
		for i := openedAt; i < len(lines); i++ {
			released[i] = false
		}
	}
	return strict, released, balanced
}

// isFenceMarker reports whether a line opens or closes a fenced block: its first
// non-space content is a run of >=3 backticks. Mirrors stream/parser.go:194.
func isFenceMarker(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "```")
}

// isItemStart reports whether s begins a Markdown list item: an unordered bullet
// ("- ", "* ", "+ ") or an ordered marker ("N." / "N)" optionally followed by a
// space), after optional leading spaces. Used as a block boundary so one finding's
// list item does not absorb its siblings when items are not blank-separated.
func isItemStart(s string) bool {
	s = strings.TrimLeft(s, " ")
	if s == "" {
		return false
	}
	if (s[0] == '-' || s[0] == '*' || s[0] == '+') && len(s) > 1 && s[1] == ' ' {
		return true
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && (s[i] == '.' || s[i] == ')') {
		return i+1 == len(s) || s[i+1] == ' '
	}
	return false
}

// isHeadingLine reports whether s is a Markdown ATX heading (1–6 leading '#'
// followed by a space, after optional leading spaces).
func isHeadingLine(s string) bool {
	s = strings.TrimLeft(s, " ")
	i := 0
	for i < len(s) && s[i] == '#' {
		i++
	}
	return i >= 1 && i <= 6 && i < len(s) && s[i] == ' '
}

// headingText returns the heading's text (leading '#'s and surrounding space
// stripped) and whether s was a heading.
func headingText(s string) (string, bool) {
	if !isHeadingLine(s) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimLeft(strings.TrimLeft(s, " "), "#")), true
}

// truncateRunes returns s unchanged when it is at most max runes, else the first
// max runes (re-trimmed) with a horizontal ellipsis appended.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}
