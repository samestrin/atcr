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
// candidates by — is what tells the right file from a namesake, and returning OK=false
// for the rest is what keeps a backfill from rewriting a record out of an unrelated
// review. A caller with more than one surviving candidate must decline, not guess.
//
// The two negative outcomes are deliberately distinct: err is "the source is gone or
// unreadable" (prune the pointer or restore the file), OK=false is "this file is not
// the one, its section is pure quoted example, or the producer's file-level policy
// would never have stamped from it" (try another candidate). Collapsing them would
// make a pruned review dir indistinguishable from a mismatch.
//
// The result also carries the file-level policy verdict the call already had to
// reach (Replay.PolicyDeclined), so a caller asking WHY a candidate yielded nothing
// reads it here rather than paying for a second ReviewPolicyDeclinesFile evaluation
// of the same file.
func ReExtractJustification(path, file string, line, anchorLine int) (Replay, error) {
	// path is a review.md the caller located by walking a directory it chose; the
	// operator is deliberately replaying their own reviews, so there is no
	// untrusted-input step here to guard.
	// The producer's FILE-LEVEL refusal exclusions, applied at the replay too, through
	// the one definition ReviewPolicyDeclinesFile owns rather than a second copy here.
	// The replay set may not exceed the stamp set — a file collectReviewNarratives
	// would refuse is one the stamp cannot have come from, so it must not yield an
	// authoritative excerpt here either
	// (TD internal/reconcile/justification_backfill.go:38). Reading the arms from the
	// same place is what keeps the two from drifting: a policy arm added to one would
	// otherwise leave the other admitting a file the producer refuses.
	//
	// Not an error — a refused candidate is simply not one the stamp could have come
	// from, so the caller should try another.
	// And the draft-line exclusion, per chunk segment — the same set buildAnchorIndex
	// skips. A file:line the model wrote inside a reasoning run the findings parser
	// REFUSED is the model's discarded draft, and publishing it as a finding's
	// provenance is the damage draftLineSet exists to prevent. The SET is file-level,
	// so reviewPolicy hands it back rather than being recomputed here — it already had
	// to derive it to reach the desync verdict. Only its CONSUMPTION is per record,
	// against this call's anchor.
	declined, raw, excluded, err := reviewPolicy(path)
	if err != nil {
		return Replay{}, err
	}
	if declined {
		return Replay{PolicyDeclined: true}, nil
	}
	lines := strings.Split(raw, "\n")
	idx := anchorLine - 1 // SourceReport.Line is 1-based; extractSection indexes from 0
	if idx < 0 || idx >= len(lines) {
		// The file changed length since the stamp. Not an error — this candidate is
		// simply not the document the excerpt came from.
		return Replay{}, nil
	}
	if _, draft := excluded[idx]; draft {
		return Replay{}, nil
	}
	if anchorTier(lines[idx], file, line) < minAnchorTier {
		return Replay{}, nil
	}
	text, section := extractSection(lines, idx)
	if text == "" {
		// extractSection suppresses a block that is entirely fenced example text.
		// That is matchAllElided, not a match — and never a reason to blank a
		// stored justification.
		return Replay{}, nil
	}
	return Replay{Text: text, Section: section, OK: true}, nil
}

// Replay is what ReExtractJustification hands back for one candidate. A struct
// rather than more positional returns, so a future outcome is a new field and not a
// reshuffle of every call site.
type Replay struct {
	// Text and Section are the re-derived excerpt and its heading; both are empty
	// unless OK.
	Text, Section string
	// OK is true only when this candidate yields an authoritative excerpt.
	OK bool
	// PolicyDeclined is the FILE-LEVEL verdict ReviewPolicyDeclinesFile would report
	// for the same path, read from the one reviewPolicy evaluation the call already
	// made. It is a subset of !OK: the record-level refusals (a draft anchor line, a
	// namesake whose anchor does not match, an out-of-range line, an all-elided
	// section) leave it false, for the reason ReviewPolicyDeclinesFile excludes them.
	// It is never set alongside a non-nil error — "I could not look" is not a verdict.
	PolicyDeclined bool
}

// ReviewPolicyDeclinesFile reports whether the producer's FILE-LEVEL policy would
// refuse to stamp any excerpt from this review.md at all — it is not a regular file
// (a symlink, FIFO or device), it is over the size cap, it is a wholly salvaged reply
// (promoted chain-of-thought, which no lane reads findings from), or its bin list
// names segments the document does not contain.
//
// It answers a different question from ReExtractJustification's OK=false, and the
// difference is the one an operator acts on. OK=false also covers "this candidate is
// simply not the one" — a namesake whose anchor does not match, a file that changed
// length, a section that is pure quoted example. Those say nothing about whether the
// record's own review.md survives, so a caller that treats them as a policy refusal
// tells the operator not to bother restoring a file that restoring would fix
// (TD internal/localdebt/backfill.go:389).
//
// The four arms here are FILE-level and therefore PATH-INDEPENDENT, which is what
// lets a review-dir-unscoped walk use the answer at all: a file the policy refuses is
// refused wherever it sits, so the caller need not establish which review directory
// the candidate belongs to — the question SourceReport.Path cannot answer (see the
// anchor note on ReExtractJustification above).
//
// Path-independence is a property of the VERDICT, not of its attribution, and a caller
// must not read more into it than that. An over-cap namesake in an unrelated review is
// genuinely policy-refused, yet says nothing about the record whose relative path it
// shares — so a caller aggregating this per record still over-attributes on that
// shape. It is a far narrower residue than "any candidate was present", which is what
// it replaced, and closing it needs the record's own review dir, which the record does
// not carry.
//
// A caller that also needs the excerpt should read Replay.PolicyDeclined from
// ReExtractJustification instead of calling this as well: both read the same
// reviewPolicy verdict, and calling both evaluates the file twice.
//
// The record-level arms are deliberately NOT included. A draft anchor line is
// unrepairable too, but it is a property of one record's anchor rather than of the
// file, so reporting it would need the anchor and would re-introduce the scoping
// question this predicate exists to avoid.
//
// A read error is reported rather than swallowed: "I could not look" is not evidence
// of a policy refusal, and the caller must not record one on it.
func ReviewPolicyDeclinesFile(path string) (bool, error) {
	declined, _, _, err := reviewPolicy(path)
	return declined, err
}

// reviewPolicy is the single definition of the FILE-LEVEL policy — the non-regular,
// size-cap, salvaged and desynced arms all live here and nowhere else — serving both
// the exported predicate and ReExtractJustification's own gate, so a policy arm added
// here reaches both callers at once. Neither caller, nor replayCandidates above them,
// re-checks any of the four.
//
// The salvaged and desynced arms hand back the content they already read even though
// they decline. Nothing downstream needs it — it is there so the caller's `declined`
// early return is the ONLY thing standing between a declined file and anchorTier. An
// empty raw would refuse on its own (the anchor line is out of range), masking a
// deleted guard; with the content in hand, removing that return lets a declined file
// through, and the exclusion tests catch it. The non-regular and over-cap arms return
// before the read and stay empty — there is no content to hand back.
//
// It also returns what it already had to derive: the raw content and the excluded
// draft-anchor set, so ReExtractJustification pays neither a second stat+read nor a
// second excludedAnchorLines pass. That is the whole reason this is a separate helper
// rather than the exported predicate being called directly — and it leaves one call
// site per computation, so neither can drift from the policy verdict built on it.
// ReExtractJustification surfaces the verdict as Replay.PolicyDeclined, so a caller
// evaluating a candidate pays for this once, not once per question it asks.
func reviewPolicy(path string) (declined bool, raw string, excluded map[int]struct{}, err error) {
	// Lstat, not Stat: the size cap and the read below must measure the candidate
	// itself, and Stat would follow a link to its target.
	fi, serr := os.Lstat(path)
	if serr != nil {
		return false, "", nil, fmt.Errorf("stat review narrative %s: %w", path, serr)
	}
	// The producer stamps only from a REGULAR file: collectReviewNarratives skips a
	// symlink, FIFO or device named review.md, so each is refused here too. Checked
	// before the read, which would otherwise follow a link or block on a FIFO.
	if !fi.Mode().IsRegular() {
		return true, "", nil, nil
	}
	// The producer's size cap: collectReviewNarratives skips any review.md over
	// maxReviewBytes, so a file it would never have stamped from must not yield an
	// authoritative excerpt either.
	if fi.Size() > maxReviewBytes {
		return true, "", nil, nil
	}
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		return false, "", nil, fmt.Errorf("reading review narrative %s: %w", path, rerr)
	}
	// A SALVAGED reply is not a narrative — it is promoted chain-of-thought, and no
	// lane reads findings from it. Withheld whole only when no bin index narrows it,
	// exactly as the producer decides: a bin list means the untouched segments are
	// real prose.
	salvaged, bins := sourceSalvage(path)
	if salvaged && len(bins) == 0 {
		return true, string(b), nil, nil
	}
	if !salvaged {
		bins = nil
	}
	// A DESYNCED bin list names no segment in this review.md, so nothing says which
	// lines were refused. Withhold, mirroring the producer.
	lines, desynced := excludedAnchorLines(string(b), bins)
	if desynced {
		return true, string(b), nil, nil
	}
	return false, string(b), lines, nil
}
