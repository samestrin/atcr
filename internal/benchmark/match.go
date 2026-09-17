package benchmark

import "sort"

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
	// ReportedIndex identifies WHICH report settled it, as an index into the
	// reported slice, or -1 when nothing did. Naming the report rather than
	// counting matches is what lets a spot-check confirm a hit was earned by the
	// citation an author expected.
	ReportedIndex int
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
//     line is not itself an added line of the case's own diff;
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
		out[i] = FindingMatch{Expected: e, ReportedIndex: -1}
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
			if r.File != e.File {
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
			if e.IsOutsideDiff() && lm.IsAddedLine(r.File, r.Line) {
				continue
			}
			candidates = append(candidates, candidate{
				expIdx:      ei,
				repIdx:      ri,
				midDistance: abs(2*r.Line - (e.LineStart + e.LineEnd)),
				expID:       e.ID,
				repFile:     r.File,
				repLine:     r.Line,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.midDistance != b.midDistance {
			return a.midDistance < b.midDistance
		}
		if a.expID != b.expID {
			return a.expID < b.expID
		}
		// Below here the two candidates name the SAME expectation at the same
		// distance, so they differ only in which report settles it. The remaining
		// keys make that choice deterministic; they cannot change the match SET,
		// because two reports tied this far are interchangeable for scoring.
		if a.repFile != b.repFile {
			return a.repFile < b.repFile
		}
		if a.repLine != b.repLine {
			return a.repLine < b.repLine
		}
		return a.repIdx < b.repIdx
	})

	usedReport := make([]bool, len(reported))
	for _, c := range candidates {
		if out[c.expIdx].Matched || usedReport[c.repIdx] {
			continue
		}
		out[c.expIdx].Matched = true
		out[c.expIdx].ReportedIndex = c.repIdx
		usedReport[c.repIdx] = true
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
