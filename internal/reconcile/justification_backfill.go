package reconcile

import (
	"fmt"
	"os"
	"strings"
)

// ReExtractJustification re-derives the narrative excerpt one ALREADY-PERSISTED
// finding would be stamped with today. It is the replay primitive a one-off
// localdebt backfill needs, and it exists because the stored excerpt cannot be
// repaired from itself.
//
// Record.StampID hashes only file\x00line\x00problem, so a re-detected finding keeps
// its id, PersistForReconcile seeds seen[id] for every open and suppressing id, and
// the corrected record is never appended. Every justification written before a change
// to extractSection therefore keeps its original text permanently. Whether a block
// needed the synthetic dangling-fence opener is a property of where that block BEGAN
// in the source document — information the excerpt alone does not carry — so a repair
// must go back to the review.md.
//
// path is the review.md, file:line is the finding as persisted, and anchorLine is the
// 1-based SourceReport.Line the excerpt was taken at.
//
// The anchor is VERIFIED, not trusted: SourceReport.Path is review-dir-relative
// (`sources/pool/raw/agent/dax/review.md`) and every review directory holds the same
// relative paths, so a caller resolving it against a repo finds dozens of same-named
// candidates. Re-scoring the line with anchorTier — the same test matchNarrative ranks
// candidates by — is what tells the right file from a namesake, and returning ok=false
// for the rest is what keeps a backfill from rewriting a record out of an unrelated
// review. A caller with more than one surviving candidate must decline, not guess.
//
// The two negative outcomes are deliberately distinct: err is "the source is gone or
// unreadable" (prune the pointer or restore the file), ok=false is "this file is not
// the one, or its section is pure quoted example" (try another candidate). Collapsing
// them would make a pruned review dir indistinguishable from a mismatch.
func ReExtractJustification(path, file string, line, anchorLine int) (text, section string, ok bool, err error) {
	b, err := os.ReadFile(path) // #nosec G304 -- path comes from a persisted SourceReport the operator is replaying
	if err != nil {
		return "", "", false, fmt.Errorf("reading review narrative %s: %w", path, err)
	}
	lines := strings.Split(string(b), "\n")
	idx := anchorLine - 1 // SourceReport.Line is 1-based; extractSection indexes from 0
	if idx < 0 || idx >= len(lines) {
		// The file changed length since the stamp. Not an error — this candidate is
		// simply not the document the excerpt came from.
		return "", "", false, nil
	}
	if anchorTier(lines[idx], file, line) < minAnchorTier {
		return "", "", false, nil
	}
	text, section = extractSection(lines, idx)
	if text == "" {
		// extractSection suppresses a block that is entirely fenced example text.
		// That is matchAllElided, not a match — and never a reason to blank a
		// stored justification.
		return "", "", false, nil
	}
	return text, section, true, nil
}
