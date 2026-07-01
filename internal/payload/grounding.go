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
// text), which the gate treats as "present but no changed lines"; a file absent
// from the range is simply absent from the map (the gate fails open for it).
func BuildChangedLines(ctx context.Context, repo, base, head string) (ChangedLines, error) {
	g := &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)}
	if err := validateRange(g, base, head); err != nil {
		return nil, err
	}
	chunks, err := g.diffChunks(base, head, "--unified=0")
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
// head ranges (from the @@ hunk headers, reusing parseHeadRanges) and the
// trimmed text of every added ('+') and removed ('-') line. The ---/+++ file
// headers are excluded; blank changed lines are skipped so they never match
// arbitrary evidence.
func parseFileChange(chunk string) FileChange {
	var fc FileChange
	for _, r := range parseHeadRanges(chunk) {
		fc.Ranges = append(fc.Ranges, LineRange{Start: r.start, End: r.end})
	}
	for _, line := range strings.Split(chunk, "\n") {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+':
			if strings.HasPrefix(line, "+++") {
				continue
			}
		case '-':
			if strings.HasPrefix(line, "---") {
				continue
			}
		default:
			continue
		}
		if t := strings.TrimSpace(line[1:]); t != "" {
			fc.ChangedText = append(fc.ChangedText, t)
		}
	}
	return fc
}
