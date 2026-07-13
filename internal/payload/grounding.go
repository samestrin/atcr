package payload

import (
	"context"
	"strings"

	"github.com/samestrin/atcr/internal/log"
)

// LineRange is an inclusive, 1-based line span in the head file — the exported
// view of the internal lineRange used by the diff splitter.
type LineRange struct {
	Start int
	End   int
}

// FileChange holds the grounding data for one changed file: the head-side
// changed line ranges (added or modified lines) and the trimmed text of every
// changed line (added or removed) for evidence-based matching.
type FileChange struct {
	Ranges      []LineRange
	ChangedText []string
}

// ChangedLines maps each changed head-side path to its FileChange grounding data.
type ChangedLines map[string]FileChange

// BuildChangedLines returns the per-file grounding data for base..head: the
// head-side changed line ranges and the changed-line texts, parsed from a single
// zero-context git diff. It is the data source for the fan-out grounding gate
// (Epic 14.1), which drops findings whose FILE:LINE is not within the patch. A
// binary or pure-mode-change file yields an empty FileChange (no ranges, no
// text), which the gate treats as "changed but no lines to check" and keeps (fail
// open); a file absent from the map was not changed by the patch, so the gate
// drops findings that cite it (the fabricated-file hallucination class).
func BuildChangedLines(ctx context.Context, repo, base, head string) (ChangedLines, error) {
	g := &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)}
	if err := validateRange(g, base, head); err != nil {
		return nil, err
	}
	return g.changedLines(base, head)
}

// changedLines builds the grounding data from the memoized whole-range
// zero-context diff. It is the runner-bound core shared by the standalone
// BuildChangedLines and RangeBuilder.BuildChangedLines: when the range's payload
// was already built on the same gitRunner, the --unified=0 chunks (and the
// --name-status underlying them) are served from cache, so grounding adds no git
// subprocess. Callers must validate the range before the first cache-populating
// call (BuildChangedLines and RangeBuilder both do).
func (g *gitRunner) changedLines(base, head string) (ChangedLines, error) {
	chunks, err := g.zeroCtxChunks(base, head)
	if err != nil {
		return nil, err
	}
	out := make(ChangedLines, len(chunks))
	for path, chunk := range chunks {
		out[path] = parseFileChange(chunk)
	}
	return out, nil
}

// parseFileChange parses one zero-context per-file diff chunk into its changed
// head ranges (from the @@ hunk headers, reusing parseHeadRanges) and the trimmed
// text of every added ('+') and removed ('-') line. Only lines AFTER the first @@
// hunk header are collected as content, so the per-file diff headers (diff --git,
// index, --- a/…, +++ b/…) are excluded structurally — not by a ---/+++ prefix
// test, which would wrongly discard genuine content whose text begins with "--"
// or "++" (a removed SQL/Lua "-- comment" renders as a "--- comment" diff line).
// Blank changed lines are skipped so they never match arbitrary evidence.
func parseFileChange(chunk string) FileChange {
	var fc FileChange
	for _, r := range parseHeadRanges(chunk) {
		fc.Ranges = append(fc.Ranges, LineRange{Start: r.start, End: r.end})
	}
	inHunk := false
	for _, line := range strings.Split(chunk, "\n") {
		if hunkHeaderRe.MatchString(line) {
			inHunk = true
			continue
		}
		if !inHunk || len(line) == 0 {
			continue
		}
		if line[0] != '+' && line[0] != '-' {
			continue
		}
		if t := strings.TrimSpace(line[1:]); t != "" {
			fc.ChangedText = append(fc.ChangedText, t)
		}
	}
	return fc
}
