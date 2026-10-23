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
		if !recordedBin(s) {
			continue
		}
		dir, err := agentDirName(s.Primary.Name)
		if err != nil || seen[dir] {
			continue
		}
		seen[dir] = true
		rec, ok := trustedChunkRecord(filepath.Join(poolDir, poolRawAgentDir), dir, s.Primary.Name)
		if !ok {
			continue
		}
		if prior == nil {
			prior = map[string]*chunkRecord{}
		}
		prior[dir] = rec
	}
	return prior
}

// trustedChunkRecord reads one persona's chunks.json from rawDir/<dir>/ and reports
// whether it can be trusted as the record of that persona's bins. It cannot when
// it is missing, resolves outside rawDir (a symlink), is oversize, does not
// parse, has an unknown schema, names a different agent, plans no bins, or sits
// beside no findings file (see readPriorChunkRecords for why the last one). Both
// readers of the prior record — the chunk-aware pending set (Epic 35.16.13 T4)
// and the resume merge (T3) — go through it, so a record one of them refuses
// the other refuses too.
func trustedChunkRecord(rawDir, dir, agent string) (*chunkRecord, bool) {
	p := filepath.Join(rawDir, dir, chunksFile)
	if !pathWithin(rawDir, p) {
		return nil, false
	}
	data, err := readFileLimited(p, maxAgentFileBytes)
	if err != nil {
		return nil, false
	}
	var rec chunkRecord
	if json.Unmarshal(data, &rec) != nil || rec.SchemaVersion != chunksSchemaVersion || rec.Agent != agent || len(rec.Bins) == 0 {
		return nil, false
	}
	if _, err := stream.SelectFindingsFile(filepath.Join(rawDir, dir)); err != nil {
		return nil, false
	}
	return &rec, true
}

// carryReviewed prepends to each planned persona record the prior bins that
// finished ok and share no file with any bin this run dispatches for that
// persona, then renumbers the bins so the union is one contiguous list. Prior
// bins the ceiling deferred (T2) that this run neither dispatches nor defers
// again are appended still deferred: they were never reviewed, so they stay in
// the union and count toward unreviewed_chunks instead of dropping out of the
// record. Failed and pending prior bins are not carried. It must
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
		var kept, deferred []chunkRecordBin
		for _, b := range p.Bins {
			if len(b.Files) == 0 || anyIn(b.Files, fresh) {
				continue
			}
			// The bin is carried whole: T5's coverage reads contributed_nothing and
			// re_packed / served_files, so a replayed bin must not claim more of its
			// files reviewed than the interrupted run did.
			b.Files = append([]string(nil), b.Files...)
			b.ServedFiles = append([]string(nil), b.ServedFiles...)
			switch b.Outcome {
			case StatusOK:
				kept = append(kept, b)
			case chunkOutcomeDeferred:
				deferred = append(deferred, b)
			}
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
		if g := carriedGap(kept, deferred); g.unreviewed > 0 {
			if l.carriedGaps == nil {
				l.carriedGaps = map[string]fileGap{}
			}
			l.carriedGaps[dir] = g
		}
		rec.Bins = append(append(kept, rec.Bins...), deferred...)
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

// carriedGap is T6's per-persona file gap over the bins a resume carries
// instead of dispatching, read by T5's per-bin rule (ReadCoverage): a carried
// deferred bin's files are unreviewed and deferred; an ok bin whose lane
// refused its content leaves all its files unreviewed, and a re-packed one
// those it did not serve. Carried bins share no file with the resumed bins, so
// the result adds to the resumed slots' gap without double counting.
func carriedGap(kept, deferred []chunkRecordBin) fileGap {
	unreviewed := map[string]bool{}
	var g fileGap
	for _, b := range deferred {
		for _, f := range b.Files {
			unreviewed[f] = true
			g.deferred++
		}
	}
	for _, b := range kept {
		if !b.open() {
			continue
		}
		served := map[string]bool{}
		for _, f := range b.coveredFiles() {
			served[f] = true
		}
		for _, f := range b.Files {
			if !served[f] {
				unreviewed[f] = true
			}
		}
	}
	g.unreviewed = len(unreviewed)
	return g
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

// markMergedEarly records that runEngine's per-persona hook already merged
// dir's replayed bins with its resumed ones (Epic 35.16.14.1 T2).
func (l *chunkLedger) markMergedEarly(dir string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.mergedEarlyDirs == nil {
		l.mergedEarlyDirs = make(map[string]bool)
	}
	l.mergedEarlyDirs[dir] = true
}

// mergedEarly reports whether markMergedEarly recorded dir.
func (l *chunkLedger) mergedEarly(dir string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.mergedEarlyDirs[dir]
}

// carriedGapFor returns the never-reviewed gap of one persona's carried bins.
func (l *chunkLedger) carriedGapFor(dir string) fileGap {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.carriedGaps[dir]
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
// resumed bins alone. The first call for dir must run before the prior
// artifacts are overwritten; a later call (an early write that failed partway,
// retried by writeResumedAgents) rebuilds from the same prior artifacts, so it
// never merges the first call's output a second time (Epic 78 T4).
func writeMergedAgent(poolDir, dir string, r Result, changed payload.ChangedLines, l *chunkLedger, carried map[string]bool) error {
	agentDir := filepath.Join(poolDir, poolRawAgentDir, dir)
	prior, err := l.priorFor(dir, agentDir, r.Agent)
	if err != nil {
		return err
	}
	var union []stream.Finding
	for _, f := range prior.findings {
		if carried[path.Clean(f.File)] {
			union = append(union, f)
		}
	}
	fresh, ungrounded := groundedFindings(r, changed)
	union = append(union, fresh...)
	findings, dropped, truncated := enforceConstraints(union, r.Agent, r.MinSeverity, r.MaxFindings)
	fr := findingsResult{Findings: findings, Dropped: dropped, Truncated: truncated, Ungrounded: ungrounded}

	content := agentReviewContent(r)
	if old := prior.review; old != "" {
		if content == "" {
			content = old
		} else {
			content = old + "\n" + chunkBoundaryLine + "\n" + content
		}
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
	// T6's unreviewed_files is likewise the union's: the resumed slots' gap
	// (already on r) plus the carried bins that were never reviewed.
	g := l.carriedGapFor(dir)
	st.UnreviewedFiles += g.unreviewed
	st.DeferredFiles += g.deferred
	return writeAgentFiles(poolDir, dir, r.Agent, content, fr.Findings, &st)
}

// priorArtifacts is one persona's findings and review.md as the interrupted run
// left them.
type priorArtifacts struct {
	findings []stream.Finding
	review   string
}

// priorFor returns dir's prior artifacts, read from agentDir by the first call
// and kept on the ledger for every later one: by then a merge may already have
// rewritten the files, and re-reading them would replay that merge's fresh
// output as prior work.
func (l *chunkLedger) priorFor(dir, agentDir, agent string) (priorArtifacts, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if p, ok := l.priors[dir]; ok {
		return p, nil
	}
	findings, err := readPriorFindings(agentDir)
	if err != nil {
		return priorArtifacts{}, fmt.Errorf("replaying reviewed bins for '%s': %w", agent, err)
	}
	p := priorArtifacts{findings: findings}
	if old, err := readFileLimited(filepath.Join(agentDir, reviewFile), maxAgentFileBytes); err == nil {
		p.review = string(old)
	} else if !errors.Is(err, os.ErrNotExist) {
		return priorArtifacts{}, fmt.Errorf("replaying review.md for '%s': %w", agent, err)
	}
	if l.priors == nil {
		l.priors = make(map[string]priorArtifacts)
	}
	l.priors[dir] = p
	return p, nil
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
