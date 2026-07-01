package payload

import "context"

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
// (Epic 14.1), which drops findings whose FILE:LINE is not within the patch.
func BuildChangedLines(ctx context.Context, repo, base, head string) (ChangedLines, error) {
	return nil, nil // STUB (RED)
}

// parseFileChange parses one zero-context per-file diff chunk into its changed
// head ranges and changed-line texts.
func parseFileChange(chunk string) FileChange {
	return FileChange{} // STUB (RED)
}
