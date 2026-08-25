package localdebt

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/reconcile"
)

// BackfillResult reports one backfill pass. The four non-Scanned counters partition
// the scanned set, and the three that are not Rewritten each mean something the
// operator may want to act on separately — a pruned review tree (Unresolved), a repo
// holding several reviews that anchor the same finding (Ambiguous), or a store that
// is already settled (Unchanged).
type BackfillResult struct {
	Scanned    int // effective records carrying both a source_report path and a justification
	Rewritten  int // replayed excerpt differed from the stored one and was written
	Unchanged  int // replayed excerpt was byte-identical
	Unresolved int // no surviving review.md anchored the finding
	Ambiguous  int // several surviving candidates disagreed, so none was written
}

// BackfillJustifications repairs the justifications ALREADY in the store by replaying
// each one from its originating review.md.
//
// It exists because the store's own identity rule makes the repair unreachable any
// other way: Record.StampID hashes file\x00line\x00problem and deliberately excludes
// Justification, so a re-detected finding hashes to the same id; PersistForReconcile
// then seeds seen[id] for every open and suppressing id and skips the append. A
// change to extractSection — the dangling-fence marker emission being the case this
// was written for — therefore governs only records first persisted after it, while
// every excerpt already on disk keeps its original text forever. Those stale excerpts
// are what cli/debt_resolve.go's wontfix gate reads, so the inconsistency is not
// cosmetic: a marker-free excerpt is accepted as a permanent dismissal's whole audit
// trail.
//
// It is a ONE-OFF, not a hook. Refreshing on every reconcile was considered and
// rejected: an unconditional enrichment append would re-add a record whenever a
// reviewer reworded its narrative, and store growth would stop being bounded by
// finding count — which is the whole reason PersistForReconcile dedupes.
//
// reviewRoot is searched for each record's SourceReport.Path. That search is
// necessary, not defensive: SourceReport.Path is review-dir-RELATIVE
// (`sources/pool/raw/agent/dax/review.md`) and every review directory holds the same
// relative paths, so the path alone selects dozens of namesakes. RunID cannot break
// the tie either — its suffix is filepath.Base(reviewDir), which is `multi-agent` for
// nearly every review. Only re-scoring the anchor line against the finding's own
// file:line distinguishes them, and a record whose candidates disagree is left ALONE.
// Guessing there would rewrite a permanent dismissal's audit trail out of an
// unrelated review, which is strictly worse than the stale text it replaces.
//
// A rewritten line is re-marshaled from a generic map, so a field this binary does
// not declare survives the edit; its key ORDER is not preserved (Go marshals map keys
// sorted), which is cosmetic in JSONL. Every line the pass does not change is copied
// through byte-for-byte and is not re-marshaled at all.
func BackfillJustifications(dir, reviewRoot string, dryRun bool) (BackfillResult, error) {
	var res BackfillResult
	err := withLock(dir, "backfill-justifications", func() error {
		recs, err := ReadAll(dir, ReadOpts{})
		if err != nil {
			return err
		}
		// Fold first: the gate reads the EFFECTIVE record, so that is the excerpt
		// whose staleness matters. An id's other rows are updated alongside it below,
		// because they carry the same stamp.
		want := map[string]string{} // id -> replayed excerpt, for ids that need one
		for _, r := range FoldRecords(recs) {
			if IsClosedStatus(r.Status) && !IsSuppressingStatus(r.Status) {
				// A resolved id is settled history; its excerpt gates nothing.
				continue
			}
			sr := r.SourceReport
			if sr == nil || sr.Path == "" || r.Justification == "" {
				continue
			}
			res.Scanned++
			texts, err := replayCandidates(reviewRoot, r)
			if err != nil {
				return err
			}
			switch {
			case len(texts) == 0:
				res.Unresolved++
			case len(texts) > 1:
				res.Ambiguous++
			case texts[0] == r.Justification:
				res.Unchanged++
			default:
				res.Rewritten++
				want[r.ID] = texts[0]
			}
		}
		if dryRun || len(want) == 0 {
			return nil
		}
		return rewriteJustifications(dir, want)
	})
	if err != nil {
		return BackfillResult{}, err
	}
	return res, nil
}

// replayCandidates returns the DISTINCT excerpts every surviving review.md under
// reviewRoot yields for rec's anchor. Distinct rather than one-per-file: two copies
// of the same review (a re-run, a backup) agree, and treating that as ambiguity would
// decline a repair that has only one answer.
func replayCandidates(reviewRoot string, rec Record) ([]string, error) {
	rel := filepath.FromSlash(rec.SourceReport.Path)
	var out []string
	seen := map[string]bool{}
	err := filepath.WalkDir(reviewRoot, func(p string, d fs.DirEntry, walkErr error) error {
		// An unreadable file or subtree is SKIPPED, not fatal: reviewRoot is an open
		// tree — the same resilience stance collectReviewNarratives takes over
		// sources/ — and one bad directory must not abort a repair pass over the
		// whole store. Propagating walkErr here would do exactly that.
		if walkErr != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, rel) {
			return nil
		}
		text, _, ok, rerr := reconcile.ReExtractJustification(p, rec.File, rec.Line, rec.SourceReport.Line)
		if rerr != nil || !ok {
			// rerr here is "this candidate is unreadable", not "the backfill
			// failed" — another candidate may still resolve the record.
			return nil
		}
		if !seen[text] {
			seen[text] = true
			out = append(out, text)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("searching %s for review narratives: %w", reviewRoot, err)
	}
	return out, nil
}

// rewriteJustifications edits the justification field of every line whose id is in
// want, in place, shard by shard.
//
// It works at the JSON-object level rather than through Record + stageShard on
// purpose: stageShard re-marshals a decoded Record, which cannot round-trip a field
// this binary does not declare — the exact loss Compact's preserved-lines mechanism
// exists to prevent. Editing a map touches one key and carries the rest.
//
// Each shard is staged to a temp beside itself and renamed, so a failure mid-pass
// leaves whole shards, never a half-written one.
func rewriteJustifications(dir string, want map[string]string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading localdebt dir for backfill: %w", basePathErr(err))
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		// path is dir + an entry name os.ReadDir just returned, inside the store
		// directory this pass already holds the lock on — not caller input.
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return fmt.Errorf("reading shard for backfill: %w", basePathErr(rerr))
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		changed := false
		for i, l := range lines {
			if strings.TrimSpace(l) == "" {
				continue
			}
			var m map[string]any
			if json.Unmarshal([]byte(l), &m) != nil {
				continue // a forward-incompatible or corrupt line is carried through untouched
			}
			id, _ := m["id"].(string)
			text, wanted := want[id]
			if !wanted {
				continue
			}
			if cur, ok := m["justification"].(string); !ok || cur == "" || cur == text {
				continue
			}
			m["justification"] = text
			enc, merr := json.Marshal(m)
			if merr != nil {
				return fmt.Errorf("re-encoding backfilled record %s: %w", id, merr)
			}
			lines[i] = string(enc)
			changed = true
		}
		if !changed {
			continue
		}
		tmp, terr := os.CreateTemp(dir, "."+e.Name()+".tmp-*")
		if terr != nil {
			return fmt.Errorf("creating temp file for backfill: %w", basePathErr(terr))
		}
		_, werr := tmp.WriteString(strings.Join(lines, "\n") + "\n")
		if cerr := tmp.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(tmp.Name())
			return fmt.Errorf("writing backfilled shard: %w", basePathErr(werr))
		}
		if rnerr := os.Rename(tmp.Name(), path); rnerr != nil {
			_ = os.Remove(tmp.Name())
			return fmt.Errorf("publishing backfilled shard: %w", basePathErr(rnerr))
		}
	}
	return nil
}
