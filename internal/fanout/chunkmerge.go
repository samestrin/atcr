package fanout

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
)

// Resume merge for chunked personas (Epic 35.16.13 T3).
//
// writeResumedAgents rewrites a re-run persona's artifacts wholesale, which is
// right when the resume re-dispatched the persona's whole payload (today's
// behaviour) and wrong once a resume re-dispatches only the bins that never
// finished: brad's 60 re-run bins would erase the findings of the 4 he already
// reviewed. So a resumed persona's record becomes the UNION of its bins — the
// prior run's reviewed bins replayed, the resumed bins fresh — and its
// findings.txt / status.json are rebuilt from that union.
//
// A prior bin is replayed only when it finished ok AND none of its files is in
// any bin the resume dispatched for the persona. A full-persona re-run covers
// every file of the original partition, so nothing is replayed and the persona
// takes today's wholesale path unchanged. A persona with no readable prior
// chunks.json likewise replays nothing.

// readPriorChunkRecords loads the chunks.json the interrupted run left for each
// persona the resume is about to re-dispatch as baseline bins, keyed by the
// persona's directory name. It must run BEFORE the engine: the resume's own
// ledger rewrites that file at plan time. A record that is missing, oversize,
// unparseable, of an unknown schema, or names a different agent is skipped, so
// that persona resumes exactly as it did before chunks.json existed.
//
// So is a record whose persona has no findings file on disk. chunks.json is
// rewritten as each bin resolves, but the persona's findings are written only
// when the run finishes, so a run killed in between leaves bins recorded ok
// whose findings exist nowhere. Replaying those bins would claim them reviewed
// with nothing to show for it; skipping the record keeps the wholesale path,
// which re-runs the persona instead.
func readPriorChunkRecords(poolDir string, slots []Slot) map[string]*chunkRecord {
	var prior map[string]*chunkRecord
	seen := map[string]bool{}
	for _, s := range slots {
		if s.Primary.ChunkTotal <= 1 || len(s.Primary.chunkFiles) == 0 {
			continue
		}
		dir, err := agentDirName(s.Primary.Name)
		if err != nil || seen[dir] {
			continue
		}
		seen[dir] = true
		data, err := readFileLimited(filepath.Join(poolDir, poolRawAgentDir, dir, chunksFile), maxAgentFileBytes)
		if err != nil {
			continue
		}
		var rec chunkRecord
		if json.Unmarshal(data, &rec) != nil || rec.SchemaVersion != chunksSchemaVersion || rec.Agent != s.Primary.Name {
			continue
		}
		if _, err := stream.SelectFindingsFile(filepath.Join(poolDir, poolRawAgentDir, dir)); err != nil {
			continue
		}
		if prior == nil {
			prior = map[string]*chunkRecord{}
		}
		prior[dir] = &rec
	}
	return prior
}

// carryReviewed prepends to each planned persona record the prior bins that
// finished ok and share no file with any bin this run dispatches for that
// persona, then renumbers the bins so the union is one contiguous list. It must
// run before writePlan so the first record on disk is already the union: a
// resume killed mid-fan-out must not leave a chunks.json that has forgotten the
// bins reviewed before it.
//
// Paths read back from the prior record are only compared as strings against
// the fresh bins' files and the persona's own findings; nothing is opened or
// dispatched from them.
func (l *chunkLedger) carryReviewed(prior map[string]*chunkRecord) {
	if len(prior) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, dir := range l.order {
		p, ok := prior[dir]
		if !ok {
			continue
		}
		rec := l.records[dir]
		fresh := map[string]bool{}
		for _, b := range rec.Bins {
			for _, f := range b.Files {
				fresh[f] = true
			}
		}
		var kept []chunkRecordBin
		for _, b := range p.Bins {
			if b.Outcome != StatusOK || len(b.Files) == 0 || anyIn(b.Files, fresh) {
				continue
			}
			kept = append(kept, chunkRecordBin{Files: append([]string(nil), b.Files...), Outcome: StatusOK})
		}
		if len(kept) == 0 {
			continue
		}
		files := map[string]bool{}
		for _, b := range kept {
			for _, f := range b.Files {
				files[path.Clean(f)] = true
			}
		}
		if l.carried == nil {
			l.carried = map[string]map[string]bool{}
		}
		l.carried[dir] = files
		rec.Bins = append(kept, rec.Bins...)
		for i := range rec.Bins {
			rec.Bins[i].Index = i
		}
		for i, ref := range l.bySlot {
			if ref.dir == dir {
				ref.bin += len(kept)
				l.bySlot[i] = ref
			}
		}
	}
}

func anyIn(files []string, set map[string]bool) bool {
	for _, f := range files {
		if set[f] {
			return true
		}
	}
	return false
}

// carriedFiles returns the files of the replayed bins for one persona, or nil
// when nothing was replayed for it (nil-safe on the ledger).
func (l *chunkLedger) carriedFiles(dir string) map[string]bool {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.carried[dir]
}

// binCounts returns how many of a persona's union bins there are and how many
// of them finished ok.
func (l *chunkLedger) binCounts(dir string) (total, ok int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, b := range l.records[dir].Bins {
		total++
		if b.Outcome == StatusOK {
			ok++
		}
	}
	return total, ok
}

// writeMergedAgent rebuilds one persona's artifacts from the union of its
// replayed and resumed bins: the prior findings that belong to a replayed bin's
// files, plus the resumed bins' fresh findings, with the persona's per-source
// constraints applied over the union exactly as a fresh run applies them over
// all of its bins. The prior review.md is kept ahead of the fresh output so the
// replayed findings keep their justification text. Status and
// unreviewed_chunks are recomputed from the union record, not taken from the
// resumed bins alone. Must run before the prior artifacts are overwritten.
func writeMergedAgent(poolDir, dir string, r Result, changed payload.ChangedLines, l *chunkLedger, carried map[string]bool) error {
	agentDir := filepath.Join(poolDir, poolRawAgentDir, dir)
	prior, err := readPriorFindings(agentDir)
	if err != nil {
		return fmt.Errorf("replaying reviewed bins for '%s': %w", r.Agent, err)
	}
	var union []stream.Finding
	for _, f := range prior {
		if carried[path.Clean(f.File)] {
			union = append(union, f)
		}
	}
	fresh, ungrounded := groundedFindings(r, changed)
	union = append(union, fresh...)
	findings, dropped, truncated := enforceConstraints(union, r.Agent, r.MinSeverity, r.MaxFindings)
	fr := findingsResult{Findings: findings, Dropped: dropped, Truncated: truncated, Ungrounded: ungrounded}

	content := agentReviewContent(r)
	if old, err := readFileLimited(filepath.Join(agentDir, reviewFile), maxAgentFileBytes); err == nil && len(old) > 0 {
		if content == "" {
			content = string(old)
		} else {
			content = string(old) + "\n" + chunkBoundaryLine + "\n" + content
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replaying review.md for '%s': %w", r.Agent, err)
	}

	st := statusFor(r, fr)
	total, ok := l.binCounts(dir)
	st.ChunkCount = total
	st.UnreviewedChunks = 0
	if ok > 0 {
		st.Status = StatusOK
		st.Error = ""
		st.UnreviewedChunks = total - ok
	}
	return writeAgentFiles(poolDir, dir, r.Agent, content, fr.Findings, &st)
}

// readPriorFindings reads a persona's findings as the interrupted run left them
// (findings.toon when present, else findings.txt — the RebuildPool rule). It
// fails closed: a missing, oversize or unparseable file is an error, because
// silently replaying nothing would erase the reviewed bins' findings.
// readPriorChunkRecords already refuses a record whose persona has no findings
// file, so a missing file here means it vanished during the resume.
func readPriorFindings(agentDir string) ([]stream.Finding, error) {
	fpath, err := stream.SelectFindingsFile(agentDir)
	if err != nil {
		return nil, err
	}
	data, err := readFileLimited(fpath, maxAgentFileBytes)
	if err != nil {
		return nil, err
	}
	pr, err := stream.ParseFindingsFile(fpath, data)
	if err != nil {
		return nil, err
	}
	return pr.Findings, nil
}
