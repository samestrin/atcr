package fanout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// maxChunksBytes bounds one chunks.json read. A record holds one repo-relative
// path per planned file (the measured 1875-file run is ~150 KB), so 16 MiB is
// generous while still refusing to slurp an unexpectedly huge file.
const maxChunksBytes = 16 << 20

// Coverage is how much of a baseline run's planned work was actually reviewed,
// read back from every persona's chunks.json (Epic 35.16.13 T5). It is counted
// two ways because the two quantities disagree: bins are per persona, while a
// file counts as covered once ANY persona reviewed it (the same union the
// file-hash index uses in uncoveredBaselineFiles).
//
// "Reviewed" is the index's predicate: a bin whose outcome is ok and that did
// not contribute nothing (a lane that refused its content never read the
// files). A re-packed bin covers only the files its server kept. pending,
// failed, timeout and deferred bins are planned but not reviewed.
type Coverage struct {
	BinsReviewed int
	BinsPlanned  int
	FilesCovered int
	FilesInScope int
	// BinsOverWindow counts the planned bins left unreviewed because they hold
	// one file larger than their reviewer's context window (Epic 35.16.14 fix
	// AC1b), so the coverage line can name that reason for them.
	BinsOverWindow int
}

// FilePercent is FilesCovered as a percentage of FilesInScope.
func (c Coverage) FilePercent() float64 {
	if c.FilesInScope == 0 {
		return 0
	}
	return float64(c.FilesCovered) * 100 / float64(c.FilesInScope)
}

// ReadCoverage sums the chunks.json records under reviewDir's pool. ok is false
// when no persona wrote one (a bulk or diff review: nothing was binned, so there
// is no bin-level coverage to report). An unreadable, oversized, corrupt or
// unknown-schema record is an error rather than a skip: dropping one persona's
// planned bins would overstate coverage.
//
// A persona that ran as a single bin writes no record, so its reviewed files are
// not counted here; on a run that mixes the two the figure can only understate.
func ReadCoverage(reviewDir string) (cov Coverage, ok bool, err error) {
	paths, err := chunkRecordPaths(reviewDir)
	if err != nil {
		return Coverage{}, false, err
	}
	inScope := map[string]struct{}{}
	covered := map[string]struct{}{}
	for _, p := range paths {
		rec, err := loadChunkRecord(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // raced with a rewrite's rename; nothing to count
			}
			return Coverage{}, false, err
		}
		ok = true
		for _, b := range rec.Bins {
			cov.BinsPlanned++
			for _, f := range b.Files {
				inScope[f] = struct{}{}
			}
			if !b.reviewed() {
				if b.OverWindow {
					cov.BinsOverWindow++
				}
				continue
			}
			cov.BinsReviewed++
			for _, f := range b.coveredFiles() {
				covered[f] = struct{}{}
			}
		}
	}
	// A served file outside the planned set cannot be "in scope covered".
	for f := range covered {
		if _, in := inScope[f]; in {
			cov.FilesCovered++
		}
	}
	cov.FilesInScope = len(inScope)
	return cov, ok, nil
}

// chunkRecordPaths lists the candidate chunks.json paths under reviewDir's pool,
// one per agent directory, sorted. It lists the directory rather than globbing,
// because reviewDir is user-supplied: a path holding [, * or ? would be read as
// a pattern, match nothing, and turn a binned run into an unmeasured one. A
// missing agent directory is no records; a path without a record is skipped by
// the caller as a raced rename is.
func chunkRecordPaths(reviewDir string) ([]string, error) {
	rawDir := filepath.Join(reviewDir, "sources", "pool", poolRawAgentDir)
	entries, err := os.ReadDir(rawDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		dir := filepath.Join(rawDir, e.Name())
		if !e.IsDir() {
			// Follow a symlinked agent directory as the glob did.
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				continue
			}
		}
		paths = append(paths, filepath.Join(dir, chunksFile))
	}
	sort.Strings(paths)
	return paths, nil
}

func loadChunkRecord(path string) (*chunkRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxChunksBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) > maxChunksBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, maxChunksBytes)
	}
	var rec chunkRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("%s is corrupt: %w", path, err)
	}
	if rec.SchemaVersion != chunksSchemaVersion {
		return nil, fmt.Errorf("%s has schema_version %d, want %d", path, rec.SchemaVersion, chunksSchemaVersion)
	}
	return &rec, nil
}
