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
	paths, err := filepath.Glob(filepath.Join(reviewDir, "sources", "pool", poolRawAgentDir, "*", chunksFile))
	if err != nil {
		return Coverage{}, false, err
	}
	sort.Strings(paths)
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
			if b.Outcome != StatusOK || b.ContributedNothing {
				continue
			}
			cov.BinsReviewed++
			served := b.Files
			if b.RePacked {
				served = b.ServedFiles
			}
			for _, f := range served {
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
