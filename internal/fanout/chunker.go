package fanout

import "strings"

// reviewStrategyChunked is the review_strategy value (Epic 14.3) that enables
// per-persona diff bin-packing. Kept as a local constant so the fan-out compares
// against the resolved Settings string without importing the registry enum.
const reviewStrategyChunked = "chunked"

// diffFileMarker is the column-0 boundary git writes before each file's hunk in
// a unified diff (`diff --git a/<old> b/<new>`). Epic 14.3's chunker splits on
// it directly rather than importing internal/payload (a package-private split),
// keeping this a self-contained ~20-line utility and avoiding cross-package
// coupling (epic Technical Constraints).
const diffFileMarker = "diff --git a/"

// countLines returns the diff line count of s, measured by newline count
// (strings.Count per the epic constraint). It is the unit the bin-packer budgets
// against; a trailing partial line is not counted, which is fine — the budget is
// a soft attention-window guard, not a byte-exact accountant.
func countLines(s string) int { return strings.Count(s, "\n") }

// countDiffFiles reports how many per-file segments a diff contains, i.e. the
// number of column-0 `diff --git a/` markers. Used to stamp a chunk's FileCount
// in the rendered prompt.
func countDiffFiles(diff string) int {
	if diff == "" {
		return 0
	}
	n := 0
	for _, ln := range strings.SplitAfter(diff, "\n") {
		if strings.HasPrefix(ln, diffFileMarker) {
			n++
		}
	}
	return n
}

// splitDiffFiles splits a unified diff into per-file segments on column-0
// `diff --git a/` boundaries. Any preamble before the first marker (rare) is
// attached to the first segment so no bytes are lost, and a diff carrying no
// marker at all comes back as a single segment. Concatenating the segments
// reproduces the input exactly (SplitAfter preserves newlines). Returns nil for
// empty input.
func splitDiffFiles(diff string) []string {
	if diff == "" {
		return nil
	}
	var segments []string
	var cur strings.Builder
	started := false
	for _, ln := range strings.SplitAfter(diff, "\n") {
		if strings.HasPrefix(ln, diffFileMarker) && started {
			segments = append(segments, cur.String())
			cur.Reset()
		}
		if strings.HasPrefix(ln, diffFileMarker) {
			started = true
		}
		cur.WriteString(ln)
	}
	if cur.Len() > 0 {
		segments = append(segments, cur.String())
	}
	return segments
}

// chunkDiff bin-packs a unified diff into chunks whose line counts do not exceed
// maxLines, splitting only on file boundaries — a single file is never split
// across chunks (epic Technical Constraint). Packing is greedy next-fit in
// original file order: files accumulate into the current chunk until adding the
// next one would exceed maxLines, then a new chunk opens. This groups multiple
// files per request (fewer API calls than naive per-file chunking, the Cost
// Efficiency NFR) while keeping presentation order stable. A single file larger
// than maxLines becomes its own oversized chunk — the caller may warn, but it is
// preserved whole. maxLines <= 0 disables chunking and returns the whole diff as
// one chunk. Returns nil for empty input.
func chunkDiff(diff string, maxLines int) []string {
	if diff == "" {
		return nil
	}
	files := splitDiffFiles(diff)
	if maxLines <= 0 || len(files) <= 1 {
		return []string{diff}
	}
	var chunks []string
	var cur strings.Builder
	curLines := 0
	for _, f := range files {
		fl := countLines(f)
		// Close the current chunk before adding a file that would overflow it, but
		// only when the chunk already holds something — an empty chunk must accept
		// even an oversized file (it cannot be split) so it lands in its own chunk.
		if cur.Len() > 0 && curLines+fl > maxLines {
			chunks = append(chunks, cur.String())
			cur.Reset()
			curLines = 0
		}
		cur.WriteString(f)
		curLines += fl
	}
	if cur.Len() > 0 {
		chunks = append(chunks, cur.String())
	}
	return chunks
}

// mergeChunkResults collapses results that share an Agent name into one result
// per persona (Epic 14.3, AC4). The chunked strategy emits one slot per
// bin-packed chunk, all under the persona's name, so Run returns N results for a
// single persona; merging them before the artifact-write step yields one
// raw/agent/<persona>/ dir with Reviewer=<persona> — satisfying writePool's
// duplicate-dir guard and keeping the already-merged 14.2 consensus filter
// (which counts distinct Reviewer values) treating the persona as ONE voter. In
// bulk strategy every Agent name is unique, so every group has size one and this
// is a no-op. First-appearance order is preserved so the slot ordering the
// manifest and summary observe is stable.
func mergeChunkResults(results []Result) []Result {
	order := make([]string, 0, len(results))
	groups := make(map[string][]Result, len(results))
	for _, r := range results {
		if _, seen := groups[r.Agent]; !seen {
			order = append(order, r.Agent)
		}
		groups[r.Agent] = append(groups[r.Agent], r)
	}
	merged := make([]Result, 0, len(order))
	for _, name := range order {
		g := groups[name]
		if len(g) == 1 {
			merged = append(merged, g[0])
			continue
		}
		merged = append(merged, mergeResultGroup(g))
	}
	return merged
}

// mergeResultGroup folds N chunk results for one persona into a single result.
// Content is the newline-joined non-empty chunk outputs — ParseModelOutput is
// line-based, so this is exactly the union of every chunk's findings. Status is
// OK when ANY chunk succeeded (the persona produced findings from at least one
// bin — a partial-success the reviewer legitimately contributes); otherwise it
// is Timeout when any chunk timed out, else Failed, carrying the first error.
// Token/turn usage and per-call telemetry accumulate across chunks so cost and
// call-count metrics reflect every request the chunked fan-out actually made.
func mergeResultGroup(g []Result) Result {
	out := g[0] // inherit stable per-slot identity (Agent, Model, PayloadMode, constraints)
	out.Err = nil
	out.DurationMS = 0
	out.TokensIn, out.TokensOut = 0, 0
	out.Turns, out.ToolCalls, out.ToolBytes = 0, 0, 0
	out.CallRecords = nil
	out.TrippedBudgets = nil
	out.FallbackUsed = false
	out.FallbackFrom = ""

	var contents []string
	var firstErr error
	anyOK, sawTimeout, allCacheHit := false, false, true
	for _, r := range g {
		if strings.TrimSpace(r.Content) != "" {
			contents = append(contents, r.Content)
		}
		out.TokensIn += r.TokensIn
		out.TokensOut += r.TokensOut
		out.Turns += r.Turns
		out.ToolCalls += r.ToolCalls
		out.ToolBytes += r.ToolBytes
		out.CallRecords = append(out.CallRecords, r.CallRecords...)
		out.TrippedBudgets = append(out.TrippedBudgets, r.TrippedBudgets...)
		if r.DurationMS > out.DurationMS {
			// Chunks run as independent parallel-lane slots, so the persona's
			// wall-clock is the slowest chunk, not the sum.
			out.DurationMS = r.DurationMS
		}
		if r.FallbackUsed {
			out.FallbackUsed = true
			out.FallbackFrom = r.FallbackFrom
		}
		out.Tools = out.Tools || r.Tools
		out.ToolsRequested = out.ToolsRequested || r.ToolsRequested
		out.ToolsDegraded = out.ToolsDegraded || r.ToolsDegraded
		allCacheHit = allCacheHit && r.CacheHit
		switch r.Status {
		case StatusOK:
			anyOK = true
		case StatusTimeout:
			sawTimeout = true
			if firstErr == nil {
				firstErr = r.Err
			}
		default:
			if firstErr == nil {
				firstErr = r.Err
			}
		}
	}
	out.Content = strings.Join(contents, "\n")
	out.CacheHit = allCacheHit
	switch {
	case anyOK:
		out.Status = StatusOK
	case sawTimeout:
		out.Status = StatusTimeout
		out.Err = firstErr
	default:
		out.Status = StatusFailed
		out.Err = firstErr
	}
	return out
}
