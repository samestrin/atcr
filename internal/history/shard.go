package history

import (
	"io"
	"path/filepath"
	"time"

	"github.com/samestrin/atcr/internal/stream"
)

// shardMonthLayout is the year-month stem of a monthly history shard file.
const shardMonthLayout = "2006-01"

// ShardPath returns the monthly shard file for a run at ts under dir, e.g.
// <dir>/2026-07.jsonl. The month is taken in UTC so shard names are
// deterministic regardless of the caller's local zone, and every record from a
// single run (all stamped with the same ts) lands in exactly one shard.
//
// Because the file name is derived from ts, a run in a new month writes to a new
// file and never reopens a prior month's shard — that is what stops old shards
// from churning fresh git blobs once the month rolls over (Epic 19.4 AC3).
func ShardPath(dir string, ts time.Time) string {
	return filepath.Join(dir, ts.UTC().Format(shardMonthLayout)+".jsonl")
}

// LoadShards reads every monthly shard (*.jsonl) under dir and returns the
// merged records across all months, ordered by shard file name — which, for the
// YYYY-MM naming, is chronological. A missing or empty dir is a valid empty
// history, not an error (mirroring Load), so a query spans whatever shards exist
// without the caller naming one (Epic 19.4 AC2). Malformed lines inside a shard
// are skipped by Load; an unreadable shard is skipped with a warning to diag
// (default os.Stderr).
//
// This is the unwindowed read — every shard, whatever its month. `atcr history`
// uses LoadShardsSince instead, which skips out-of-window shards by filename;
// both run through the same loadShardsWhere implementation so they cannot drift.
func LoadShards(dir string, diag ...io.Writer) ([]Record, error) {
	return loadShardsWhere(dir, func(string) bool { return true }, diag...)
}

// LoadAll returns the full queryable history: every monthly shard under shardDir
// (LoadShards) merged with the legacy flat ledger at legacyPath, if it still
// exists. Both locations live under .atcr/ (Epic 35.14), so a standalone user
// with no .planning/ directory gets the whole ledger. The legacy file — the
// pre-19.4 .atcr/findings-history.jsonl — is read in place and read-only; it is
// never moved or rewritten (Epic 19.4: "no complex write-migrations"), so a
// project that accrued history before sharding keeps that data visible alongside
// new shards. An absent shard dir or legacy file is simply empty history, not an
// error.
//
// The union is deduplicated by dedupeOccurrences; see that function for why the
// key is (Timestamp, ID). Legacy records come first, so ordering stays
// chronological across the two sources: the flat ledger only ever holds pre-19.4
// runs, which all predate the oldest shard.
//
// NOTE: this is a TEST-FACING PRIMITIVE, kept deliberately — it has had no
// production caller since Epic 35.14 pointed cli/history.go at LoadAllSince. It
// stays because it is the unwindowed union, which is exactly what the AC2
// union+dedup tests want to assert: expressing them through LoadAllSince would
// mean inventing a "wide enough" duration and a now, making tests of the MERGE
// depend on clock arithmetic that has nothing to do with what they cover. The
// windowing logic is tested separately against LoadAllSince. Both share
// unionWithLegacy, so the two entry points cannot drift on legacy handling or
// dedupe, and this wrapper cannot rot independently of the one in production use.
func LoadAll(shardDir, legacyPath string, diag ...io.Writer) ([]Record, error) {
	shards, err := LoadShards(shardDir, diag...)
	if err != nil {
		return nil, err
	}
	return unionWithLegacy(shards, legacyPath, time.Time{})
}

// unionWithLegacy is the single union implementation behind LoadAll and
// LoadAllSince — one merge so the two entry points cannot drift on legacy
// handling or dedupe (the same argument loadShardsWhere makes one level
// down). It reads the legacy flat ledger and deduplicates the union on
// (Timestamp, ID); legacy records come first, so ordering stays
// chronological across the two sources (the flat ledger only ever holds
// pre-19.4 runs, which all predate the oldest shard).
//
// A non-zero cutoff drops legacy records strictly before it BEFORE the
// dedupe pass: the dedupe map and output slice are sized by what they hold,
// so the windowed caller's cost stays proportional to the window rather than
// to the entire pre-19.4 ledger. The cutoff can never split a duplicate
// group — a (Timestamp, ID) group shares its Timestamp by construction.
func unionWithLegacy(shards []Record, legacyPath string, cutoff time.Time) ([]Record, error) {
	legacy, err := Load(legacyPath)
	if err != nil {
		return nil, err
	}
	if !cutoff.IsZero() {
		inWindow := make([]Record, 0, len(legacy))
		for _, r := range legacy {
			if !r.Timestamp.Before(cutoff) {
				inWindow = append(inWindow, r)
			}
		}
		legacy = inWindow
	}
	return dedupeOccurrences(append(legacy, shards...)), nil
}

// dedupeOccurrences collapses records that describe the SAME occurrence of a
// finding — the same finding id recorded by the same run — keeping the first one
// seen. Duplicates arise when a repo's two read locations both hold a run's
// records, which is what a cutover between storage locations produces; counting
// such a record twice would inflate the per-severity table.
//
// The key is (Timestamp, ID), and both halves are load-bearing:
//
//   - ID alone would fold every recurrence of a finding into one row. A finding
//     that survives six monthly reviews IS the trend this ledger exists to
//     report (Epic 35.14 Design Decision 2 — "history's value is the history"),
//     so each run's occurrence must survive. RecordReview dedupes by id alone
//     (capture.go) only because every record in one run shares a Timestamp; that
//     precondition does not hold across the union.
//   - Timestamp alone would fold every distinct finding from a single run into
//     one row, since one run stamps all of its records identically.
//
// Timestamp is compared as an instant rather than as a time.Time value: RFC3339
// round-trips preserve the zone offset, so the same instant read from two files
// written in different zones is one occurrence, not two. The instant is keyed as
// (seconds, nanoseconds) rather than UnixNano because UnixNano is undefined
// outside 1678-2262 — a zero-time record decodes to year 1, the canonical
// out-of-range case (such records are now carved out of keying entirely, below,
// but any other pre-1678 or post-2262 date wraps the same way).
//
// Two rules keep the collapse from losing information:
//
//   - A record with an EMPTY id is never deduplicated. An id-less record (a
//     malformed line that still parsed as JSON) has no identity to match on, so
//     keying it as (ts, "") would collapse every id-less record from one run into
//     a single row — silent loss the plain concatenation this replaces did not
//     have.
//   - A record with a ZERO Timestamp is never deduplicated either: a missing or
//     malformed "ts" decodes to the zero time, which is UNKNOWN time, not the
//     SAME time — folding two such records would collapse occurrences from
//     different runs into one row, the same silent loss by the other half of
//     the key.
//   - A duplicate resolves to the HIGHEST severity seen, matching RecordReview's
//     within-run rule (capture.go). Duplicates reach the union from the
//     storage-location cutover — a run's records present in BOTH the legacy
//     ledger and a shard — where the two copies are normally identical;
//     highest-severity-wins is the deterministic rule for the defensive case
//     where they disagree (a hand-edited file, or a severity re-settled between
//     the two writes). Only Severity is promoted; every other field keeps the
//     first copy read, so file order never decides the reported severity.
func dedupeOccurrences(recs []Record) []Record {
	if len(recs) < 2 {
		return recs
	}
	type occurrence struct {
		sec  int64
		nsec int
		id   string
	}
	out := make([]Record, 0, len(recs))
	seen := make(map[occurrence]int, len(recs)) // occurrence -> index in out
	for _, r := range recs {
		// A record missing EITHER half of the key has no occurrence identity:
		// an empty id has no finding to match on, and a zero Timestamp (a line
		// whose "ts" was missing or unparseable) is UNKNOWN time, not the SAME
		// time — keying two ts-less occurrences of one finding as equal would
		// fold records from different runs into one row.
		if r.ID == "" || r.Timestamp.IsZero() {
			out = append(out, r)
			continue
		}
		key := occurrence{sec: r.Timestamp.Unix(), nsec: r.Timestamp.Nanosecond(), id: r.ID}
		if idx, dup := seen[key]; dup {
			if stream.SeverityRank[stream.NormalizeSeverity(r.Severity)] >
				stream.SeverityRank[stream.NormalizeSeverity(out[idx].Severity)] {
				out[idx].Severity = r.Severity
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, r)
	}
	return out
}
