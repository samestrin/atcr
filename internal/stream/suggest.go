package stream

import (
	"path"
	"strings"
)

// tier2SimilarityThreshold is the minimum stem similarity for a Tier 2 typo
// suggestion. The 5.0 plan and this epic's Open Question 1 recommended ~0.85 on
// the assumption that the canonical example (validator -> validate) was edit
// distance 1; it is actually distance 2 (shared prefix "validat", then "or" vs
// "e"), giving stem similarity 1 - 2/9 ≈ 0.78. The epic's binding directive is
// to tune "against the 5.0 examples rather than a round number", so the
// threshold is set to admit that example with margin while still rejecting
// clearly-different names (e.g. config/cfg ≈ 0.50).
const tier2SimilarityThreshold = 0.75

// CaseCorrection implements Tier 3 (case-only difference). It reports whether
// citedRel differs from a tracked file only by case, and — when exactly one
// such file exists — the correctly-cased path to suggest.
//
//   - byte-exact tracked citation       -> ("", false)  (correctly cited)
//   - single case-folded match, no exact -> (realPath, true)
//   - multiple case-folded matches        -> ("", true)   (mismatch, ambiguous)
//   - no case-folded match                -> ("", false)  (genuine miss; see MissingSuggestion)
//
// mismatch is independent of the filesystem: it fires even on case-insensitive
// filesystems where os.Stat reports the cited path as present (AC3), so the
// caller must consult it before trusting an existence check.
func (x *FileIndex) CaseCorrection(citedRel string) (suggestion string, mismatch bool) {
	if x == nil {
		return "", false
	}
	rel := toSlashKeys(citedRel)
	if x.Has(rel) {
		return "", false // correctly cited, byte-exact
	}
	folded := x.ByFold(rel)
	switch len(folded) {
	case 0:
		return "", false // not a case issue
	case 1:
		return folded[0], true
	default:
		return "", true // ambiguous: mismatch but no single winner
	}
}

// MissingSuggestion implements Tier 1 then Tier 2 for a path absent from the
// index. It returns the single best correctly-spelled candidate, or "" when
// none is confident enough:
//
//   - Tier 1 (exact basename elsewhere): the cited basename matches tracked
//     files in other directories. One match -> suggest it (no edit distance). On
//     a tie, rank by shared path segments; a unique winner is suggested, else "".
//   - Tier 2 (basename typo, same directory): the cited directory tracks files
//     but not this basename. Compare the cited stem against each same-directory
//     file's stem (matching extension only) by similarity; the single closest
//     above tier2SimilarityThreshold is suggested.
//
// Tier 1 is tried first because an exact-basename match is near-certain and
// needs no threshold; Tier 2 only runs when Tier 1 finds nothing.
//
// Trade-off: a lone Tier 1 match with zero path-segment overlap (e.g., a very
// common basename such as handler.go appearing in an unrelated directory) will
// be suggested confidently even though it may be the wrong file. This is
// mandated by AC2, which requires a lone exact-basename match to be suggested
// without an overlap threshold. Callers that need stricter filtering may
// post-filter suggestions by directory similarity or require a minimum segment
// overlap before presenting the result.
func (x *FileIndex) MissingSuggestion(citedRel string) string {
	if x == nil {
		return ""
	}
	rel := toSlashKeys(citedRel)
	base := path.Base(rel)
	dir := path.Dir(rel)

	if s := x.tier1(rel, base, dir); s != "" {
		return s
	}
	return x.tier2(rel, base, dir)
}

// tier1 ranks exact-basename matches in other directories by path-segment
// overlap with the cited path, returning a unique winner or "".
//
// For a lone match (len(candidates) == 1) the result is returned regardless of
// overlap, per AC2. See MissingSuggestion for the documented trade-off.
func (x *FileIndex) tier1(rel, base, dir string) string {
	candidates := x.ByBasename(base)
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) == 1 {
		// Only meaningful when it is not the cited path itself; an absent path
		// is never in the tracked set, so a lone match is always elsewhere.
		if candidates[0] != rel {
			return candidates[0]
		}
		return ""
	}
	bestScore, best, tie := -1, "", false
	selfTracked := false
	for _, c := range candidates {
		if c == rel {
			selfTracked = true
			continue
		}
		score := segOverlap(dir, path.Dir(c))
		switch {
		case score > bestScore:
			bestScore, best, tie = score, c, false
		case score == bestScore:
			tie = true
		}
	}
	if tie || best == "" {
		return "" // ambiguous: a wrong guess is worse than none
	}
	if selfTracked {
		// The cited path is itself tracked; a tracked path is never a hallucination
		// to correct, regardless of on-disk state.
		return ""
	}
	return best
}

// tier2 finds the closest same-directory file by stem similarity (matching
// extension), above the threshold and with a unique winner.
func (x *FileIndex) tier2(rel, base, dir string) string {
	if !x.HasDir(dir) {
		return ""
	}
	citedStem, citedExt := splitStem(base)
	bestScore, best, tie := tier2SimilarityThreshold, "", false
	for _, cand := range x.DirBasenames(dir) {
		if cand == base {
			continue // never suggest the cited path back to itself
		}
		candStem, candExt := splitStem(cand)
		if candExt != citedExt {
			continue // Tier 2 stays within a file type
		}
		if prefixDerivation(citedStem, candStem) {
			// One stem is a strict prefix of the other: a pluralization or
			// derivation (user/users, handler/handlers, parse/parser), which are
			// commonly distinct coexisting files, not a typo. Similarity cannot
			// separate these from a real typo — they often score HIGHER than the
			// canonical validator/validate (0.78) — so guard structurally and
			// emit no suggestion rather than a confident wrong one.
			continue
		}
		score := similarity(citedStem, candStem)
		switch {
		case score > bestScore:
			bestScore, best, tie = score, cand, false
		case score == bestScore:
			tie = true
		}
	}
	if tie || best == "" {
		return ""
	}
	if dir == "." {
		return best
	}
	return dir + "/" + best
}

// segOverlap counts directory path segments shared (as a set) between two
// slash-separated directory paths. "." (no directory) contributes nothing.
func segOverlap(a, b string) int {
	set := make(map[string]struct{})
	for _, s := range strings.Split(a, "/") {
		if s != "" && s != "." {
			set[s] = struct{}{}
		}
	}
	n := 0
	for _, s := range strings.Split(b, "/") {
		if s == "" || s == "." {
			continue
		}
		if _, ok := set[s]; ok {
			n++
		}
	}
	return n
}

// prefixDerivation reports whether one stem is a strict prefix of the other —
// the signature of a pluralization or derivation (user->users, parse->parser)
// rather than a typo. It deliberately does NOT fire for validator/validate,
// which share a prefix but where neither contains the other.
func prefixDerivation(a, b string) bool {
	if a == b {
		return false
	}
	short, long := a, b
	if len(b) < len(a) {
		short, long = b, a
	}
	return strings.HasPrefix(long, short)
}

// splitStem splits a basename into its stem and extension (including the dot),
// e.g. "validate.go" -> ("validate", ".go"). A dotfile with no extension keeps
// the whole name as the stem.
func splitStem(base string) (stem, ext string) {
	ext = path.Ext(base)
	return strings.TrimSuffix(base, ext), ext
}
