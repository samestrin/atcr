package benchmark

import (
	"sort"
	"strings"
)

// ReportedFinding is one finding a reviewer raised, LOCATED. It is the projection
// of stream.Finding this package needs — file, line, category — and nothing else:
// internal/benchmark carries no dependency on the stream parser, and the scorer
// has no use for a reviewer's prose.
type ReportedFinding struct {
	File     string
	Line     int
	Category string
}

// FindingMatch is one expected finding's outcome against a reviewer's report.
// There is exactly one per expected finding, in the case's own order, so a caller
// reports per-finding outcomes without re-joining on id.
type FindingMatch struct {
	Expected ExpectedFinding
	Matched  bool
}

// candidate is one legal (report, expectation) pairing, carrying the two sort keys
// the tie-break rules are defined in terms of.
type candidate struct {
	expIdx      int
	repIdx      int
	midDistance int // |2*line - (start+end)|, doubled to stay an integer
	expID       string
	repFile     string
	repLine     int
}

// MatchFindings pairs reported findings with expected ones POSITIONALLY: by file
// and line range, not by category. That is the whole reason this tier exists —
// standard-v1's category-only scoring cannot tell "found the silent deletion in
// the reconcile loop" from "raised any correctness nit anywhere in the diff", and
// both produce the same number.
//
// The rule is FORMAT.md's, implemented in full (AC3 and AC3b):
//
//   - the reported file equals `file` exactly, as a repository-relative POSIX path;
//   - the reported line falls in [line_start-tolerance, line_end+tolerance], with
//     the tolerance taken PER FINDING from case.json rather than hardcoded;
//   - an `outside_diff: true` expectation additionally requires that the reported
//     line is not itself an added OR REMOVED line of the case's own diff (FORMAT.md,
//     "The matching rule", condition 3);
//   - a reported finding settles at most ONE expectation, and an expectation is
//     settled at most once — so N reports of one defect score as one hit;
//   - among several legal pairings the nearest range MIDPOINT wins, and an exact
//     tie breaks alphabetically by expected id.
//
// Categories are deliberately not consulted. The expected category is recorded for
// authors and for the run-result, but making it a match condition would mean a
// reviewer who found the right defect and filed it under a defensible neighbouring
// word scored zero — the failure mode internal/benchmark/equivalence.go already
// exists to avoid on the category-recall side.
//
// DETERMINISM: the outcome depends only on the CONTENT of the two slices, never on
// their order. Every sort key below is data, and the final key pair (file, line)
// distinguishes any two reports that are not interchangeable — two that ARE
// interchangeable produce the same match set whichever is consumed.
func MatchFindings(expected []ExpectedFinding, reported []ReportedFinding, lm DiffLineMap) []FindingMatch {
	if len(expected) == 0 {
		return nil
	}
	out := make([]FindingMatch, len(expected))
	for i, e := range expected {
		out[i] = FindingMatch{Expected: e}
	}

	// Enumerate every LEGAL pairing first, then assign. A greedy single pass over
	// reports would let an early report claim an expectation that a nearer one
	// settles better, making the outcome depend on report order.
	var candidates []candidate
	for ri, r := range reported {
		// A non-positive line carries no position. Crediting it would let one
		// file-level finding satisfy every expectation in that file.
		if r.Line <= 0 {
			continue
		}
		for ei, e := range expected {
			if !pathMatches(r.File, e.File) {
				continue
			}
			tol := e.Tolerance()
			if r.Line < e.LineStart-tol || r.Line > e.LineEnd+tol {
				continue
			}
			// Condition 3. Checked when ENUMERATING rather than when assigning, so a
			// blocked pairing never becomes a candidate at all — which is what keeps
			// the report available to another expectation. Consuming it here would
			// let one mis-aimed citation silently cost a second, correct finding.
			//
			// Looked up under the EXPECTATION's spelling, not the report's: the line
			// map is keyed by head-side repository paths, which is the space e.File is
			// validated to live in, while r.File is whatever the reviewer typed. The
			// removed side is keyed base-side (the only space a removed line has a
			// number in) and the two numberings coincide before the first hunk, which
			// is why the removed check is load-bearing and not decoration.
			if e.IsOutsideDiff() && (lm.IsAddedLine(e.File, r.Line) || lm.IsRemovedLine(e.File, r.Line)) {
				continue
			}
			candidates = append(candidates, candidate{
				expIdx:      ei,
				repIdx:      ri,
				midDistance: abs(2*r.Line - (e.LineStart + e.LineEnd)),
				expID:       e.ID,
				// The REPORT's own spelling, not e.File. Assigning the expectation's
				// file made this key constant across every candidate for one
				// expectation, so the sort fell through to repIdx — pure input order —
				// and the DETERMINISM contract above was false whenever two reports
				// tied on expID and line. pathMatches accepts several spellings of one
				// path, so that tie is reachable, not theoretical.
				repFile: r.File,
				repLine: r.Line,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidateLess(candidates[i], candidates[j])
	})

	usedReport := make([]bool, len(reported))
	for _, c := range candidates {
		if out[c.expIdx].Matched || usedReport[c.repIdx] {
			continue
		}
		out[c.expIdx].Matched = true
		usedReport[c.repIdx] = true
	}
	return out
}

// candidateLess is the total order MatchFindings sorts its candidates by. It is a
// named function rather than an inline closure so the key sequence is reachable from
// an in-package test: the last key is invisible through MatchFindings' return value
// (see below), so pinning it at the call site is the only place it can be pinned at
// all.
func candidateLess(a, b candidate) bool {
	if a.midDistance != b.midDistance {
		return a.midDistance < b.midDistance
	}
	if a.expID != b.expID {
		return a.expID < b.expID
	}
	// Below here the two candidates name the SAME expectation at the same distance,
	// so they differ only in which report settles it. The remaining keys make that
	// choice deterministic; they cannot change the match SET, because two reports
	// tied this far are interchangeable for scoring.
	//
	// THE FINAL repIdx TIE-BREAK IS NOT OBSERVABLE THROUGH MatchFindings' RETURN
	// VALUE, and that is a property of FindingMatch rather than an oversight.
	// Candidates tied through repLine share an expID, hence an expIdx, so they
	// compete for ONE expectation; FindingMatch carries only Expected and Matched,
	// never the report that settled it; and the losing reports stay available and —
	// being at the same file and line — are interchangeable candidates everywhere
	// else. Dropping the key therefore leaves every MatchFindings test green.
	//
	// It is kept because the sort is sort.Slice, NOT sort.SliceStable: without a
	// total order the candidate SLICE order is arbitrary between runs. Nothing
	// downstream reads that order today, which is exactly why the mutation is silent
	// through the return value — but a future consumer that does (an artifact naming
	// which report settled each expectation is the obvious one) would inherit
	// reproducibility for free rather than discovering it was never there.
	//
	// That is what this function's own test pins directly, in both argument orders.
	// Order-independence of the RESULT is pinned separately in match_test.go.
	if a.repFile != b.repFile {
		return a.repFile < b.repFile
	}
	if a.repLine != b.repLine {
		return a.repLine < b.repLine
	}
	return a.repIdx < b.repIdx
}

// pathMatches reports whether a reviewer's cited path names the expectation's
// file, tolerating the diff-artifact prefixes a citation may carry: a leading `./`
// or `/`, or an `a/`/`b/` copied out of the diff header.
//
// It exists because the grounding gate ALREADY accepts those spellings.
// fanout.normalizeFindingPath strips them for its own changed-map lookup but never
// rewrites the finding, so a citation of `b/pkg/a.py` clears the gate, reaches the
// pool with its prefix intact, and would score zero here against an expected
// `pkg/a.py`. That is the tier's headline metric turning on a cosmetic path habit
// rather than on whether the reviewer found the defect.
//
// The `a/`/`b/` strip is CONDITIONAL, exactly as the grounding gate's is: it is
// tried only after the unstripped path has failed. A real repository path may
// legitimately begin `a/` — `a/b.py` is a valid file — and stripping it
// unconditionally would make this function fail to match the very path it was
// handed verbatim.
//
// Deliberately NOT filepath.Clean: these are POSIX repository paths on every host,
// and a case's expected `file` is validated as one, so cleaning with the host
// separator would make matching differ on Windows for identical bytes.
func pathMatches(reported, expected string) bool {
	p := strings.TrimSpace(reported)
	if p == expected {
		return true
	}
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == expected {
		return true
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:] == expected
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
