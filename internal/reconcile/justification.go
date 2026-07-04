package reconcile

import (
	"log/slog"
	"os"
	"path/filepath"
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
)

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
	matched := 0
	for i := range jf {
		m, ok := matchNarrative(narratives, jf[i].File, jf[i].Line, jf[i].Reviewers)
		if !ok {
			continue
		}
		jf[i].Justification = m.text
		jf[i].SourceReport = &SourceReport{Path: m.relPath, Line: m.line, Section: m.section}
		matched++
	}
	slog.Debug("justifications stamped", "matched", matched, "total", len(jf))
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
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(reviewDir, path)
		if rerr != nil {
			rel = path
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

// matchNarrative finds the review.md section that best references file:line and
// returns its extracted narrative + back-reference. Selection is deterministic:
// highest anchor tier wins (a line/range covering the finding's exact line beats
// a same-file-other-line reference beats a bare file mention); ties break toward
// a narrative whose leaf dir is one of the finding's reviewers, then toward the
// earliest narrative (sorted), then the earliest line. Returns ok=false when no
// review.md mentions the finding's file at all.
func matchNarrative(narratives []reviewNarrative, file string, line int, reviewers []string) (narrativeMatch, bool) {
	if file == "" {
		return narrativeMatch{}, false
	}
	revSet := toSet(reviewers)
	bestTier, bestRev, bestNarr, bestLine := 0, false, -1, -1
	for ni := range narratives {
		revPref := revSet[narratives[ni].leaf]
		for li, lt := range narratives[ni].lines {
			tier := anchorTier(lt, file, line)
			if tier < minAnchorTier {
				continue
			}
			if beatsMatch(tier, revPref, ni, li, bestTier, bestRev, bestNarr, bestLine) {
				bestTier, bestRev, bestNarr, bestLine = tier, revPref, ni, li
			}
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
//	2 — a `file:N` reference to the same file but a different line
//	1 — the file path appears with no adjacent line number
//	0 — the file path does not appear on this line
//
// The `file:` scan handles both a bare `internal/x.go:42` anchor and a range
// `internal/x.go:65-102`; the char after the number is not required to be a
// non-digit because parseLineRange consumes the full leading integer run. Each
// `file:` occurrence is rejected when it is a suffix of a longer path token (the
// char to its left is a path/identifier char), so a finding for "y.go" does not
// falsely match a line referencing "internal/x/y.go:42".
func anchorTier(s, file string, line int) int {
	best := 0
	if strings.Contains(s, file) {
		best = 1 // file present, no covering line reference (yet)
	}
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
		if best < 2 {
			best = 2
		}
	}
	return best
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
// nearest enclosing Markdown heading. The block is the maximal run of contiguous
// non-blank, non-heading lines around idx (a heading may start the block but is
// never crossed), so a per-finding list item or paragraph is captured without
// bleeding into the next section. The result is trimmed and truncated to
// justificationMaxRunes.
func extractSection(lines []string, idx int) (text, section string) {
	if idx < 0 || idx >= len(lines) {
		return "", ""
	}
	for j := idx; j >= 0; j-- {
		if h, ok := headingText(lines[j]); ok {
			section = h
			break
		}
	}
	start := idx
	for start > 0 && !isHeadingLine(lines[start]) &&
		strings.TrimSpace(lines[start-1]) != "" && !isHeadingLine(lines[start-1]) {
		start--
	}
	end := idx
	for end < len(lines)-1 &&
		strings.TrimSpace(lines[end+1]) != "" && !isHeadingLine(lines[end+1]) {
		end++
	}
	var b strings.Builder
	for j := start; j <= end; j++ {
		if j > start {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(lines[j], "\r"))
	}
	return truncateRunes(strings.TrimSpace(b.String()), justificationMaxRunes), section
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
