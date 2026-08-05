package localdebt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Summary is the minimal projection of a Record that the two hot read paths need:
// the write-time dedup seed in cli/reconcile.go's persistLocalDebt, and the
// open-backlog selection in cli/debt_resolve.go's selectOpenDebt. Neither needs
// Problem, Fix, Evidence, Justification, Reviewers or SourceReport — the bulk of a
// record's ~500 bytes — so decoding into this struct instead of a full Record cuts
// both churned and retained memory by roughly an order of magnitude at the scale
// the store is sized for (store.go's maxLineBytes block; Plan 35.13 Decision 5).
//
// HasFile records only WHETHER the record had a File, not the path: the resolve
// worklist skips records with no location (nothing to display or act on), and
// keeping the boolean avoids retaining one string per record for a question that
// is answered once.
//
// This is a projection for SELECTION, never for display. A caller that needs to
// render a record hydrates it through ReadRecords/ReadAll for the selected ids
// (see cli/debt_resolve.go's two-pass select-then-hydrate).
type Summary struct {
	ID        string
	Status    string
	Severity  string
	Timestamp string
	HasFile   bool

	// The selection fields `atcr debt list` and `atcr debt dashboard` filter and
	// sort on: File and Line for the --component filter and sortDebt's location
	// tiebreak, Category for --category, EstMinutes for --sort=est. They are the
	// difference between selecting on summaries and reading every record in full,
	// and they are still a fraction of a record: the bulk (problem, fix, evidence,
	// justification, reviewers, source_report) stays unprojected.
	//
	// HasFile is kept alongside File rather than derived at each use: it is the
	// resolve worklist's question ("is there anything to act on"), asked once per
	// record on a path that predates these fields.
	File       string
	Line       int
	Category   string
	EstMinutes int
}

// summaryLine is the on-the-wire decode target. Its JSON tags MUST stay identical
// to the corresponding Record fields (record.go) — ts for Timestamp, run_id for
// RunID — or a record would decode differently depending on which path read it,
// which is exactly the drift that corrupts dedup.
//
// # Why the unprojected numeric fields are declared
//
// encoding/json type-checks only the fields the TARGET struct declares; it ignores
// unknown keys entirely. So a field Record declares and summaryLine omits is
// type-checked by decodeRecord and NOT by decodeSummary — and the resulting
// asymmetry is the dangerous direction: a line the full path rejects but the
// summary path accepts puts an id in the dedup seed that no rendering path can
// display, so a re-detection of that finding is skipped and it becomes
// permanently invisible.
//
// Every NUMERIC field Record declares is therefore declared here too and left
// unread — including SourceReport's nested Line. They cost nothing (ints are
// inline, and the nested struct is a value field, so neither allocates) and they
// cover the realistic corruption: a number written as a string, e.g. "line": "12".
//
// Residual, accepted and tracked as TD-016: a wrong JSON TYPE in one of Record's
// string, array or object fields (e.g. "problem": 123, "reviewers": "claude") is
// still accepted here and rejected by decodeRecord. Closing it needs either the
// per-record string allocations this whole path exists to avoid, or a bespoke
// zero-size validating unmarshaler per field kind. No atcr writer can produce such
// a line — Append always marshals a Record — so, like the other hand-corruption
// cases in this package, it is filed rather than fixed here.
type summaryLine struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	RunID         string `json:"run_id"`
	Status        string `json:"status"`
	Severity      string `json:"severity"`
	Timestamp     string `json:"ts"`
	File          string `json:"file"`
	Category      string `json:"category"`
	Line          int    `json:"line"`
	EstMinutes    int    `json:"est_minutes"`

	// Declared for type-check parity only; never read. See the doc block above. The
	// nested block is a VALUE, not a pointer, so it stays inline and allocation-free
	// while still type-checking source_report.line the way decodeRecord does.
	// (Line and EstMinutes were in this group until the list/dashboard selection
	// path started reading them; they still type-check exactly as before.)
	Occurrences  int `json:"occurrences"`
	SourceReport struct {
		Line int `json:"line"`
	} `json:"source_report"`
}

// decodeSummary parses one trimmed JSONL line into a Summary, applying the SAME
// three gates as decodeRecord (store.go) in the same order: malformed JSON, a
// schema_version newer than this binary understands, and a missing required field.
// ok is false when the line was skipped, in which case a warning has already been
// emitted to w.
//
// Gate parity is a correctness requirement, not a stylistic one, and it is the
// SKIP DECISION that must match — a line visible to one decoder and invisible to
// the other corrupts dedup in one direction or hides a finding in the other.
//
// The warning TEXT is byte-identical for the two explicit gates (schema version,
// missing required field) and for a JSON syntax error. It necessarily differs for
// a type error, where encoding/json names the target struct in its message
// ("...Go struct field summaryLine.line of type int" vs "...Record.line..."). The
// tests assert equality where it holds and the shared skip decision where it does
// not. See summaryLine for the one type-error class that is not detected here.
//
// Unlike decodeRecord, this path does not distinguish a forward-incompatible line
// from a malformed one: only Compact needs that distinction (to carry unreadable
// lines through a rewrite rather than destroy them), and Compact reads full
// records.
func decodeSummary(line []byte, path string, w io.Writer) (Summary, bool) {
	var s summaryLine
	if err := json.Unmarshal(line, &s); err != nil {
		_, _ = fmt.Fprintf(w, "localdebt: "+MsgMalformedSkip+" in %s: %v\n", path, err)
		return Summary{}, false
	}
	if s.SchemaVersion > SchemaVersion {
		_, _ = fmt.Fprintf(w, "localdebt: skipping record with unsupported schema_version %d (> %d) in %s\n", s.SchemaVersion, SchemaVersion, path)
		return Summary{}, false
	}
	if s.RunID == "" || s.ID == "" {
		_, _ = fmt.Fprintf(w, "localdebt: "+MsgMalformedSkip+" in %s: missing required field (run_id or id)\n", path)
		return Summary{}, false
	}
	return Summary{
		ID:         s.ID,
		Status:     s.Status,
		Severity:   s.Severity,
		Timestamp:  s.Timestamp,
		HasFile:    s.File != "",
		File:       s.File,
		Line:       s.Line,
		Category:   s.Category,
		EstMinutes: s.EstMinutes,
	}, true
}

// streamSummaryFile scans one month shard and hands every decodable line to fn,
// accumulating nothing. The reader shape is ReadRecords' verbatim (store.go
// scanShard): a bufio.Reader sized to maxLineBytes rather than a bufio.Scanner, so
// a single over-long line is drained and skipped instead of terminating the read —
// bufio.Scanner's ErrTooLong is terminal and would abort the whole shard, and via
// the directory walk every later month with it.
//
// An error returned by fn aborts the scan and is returned wrapped in callbackErr,
// so StreamSummaries can tell a caller's own error apart from a read failure.
//
// br is supplied and RESET by the caller rather than allocated here: the buffer is
// sized to maxLineBytes (1 MiB), so allocating one per shard would cost a MiB per
// month of history on a walk whose entire purpose is to stay memory-lean. The walk
// is single-goroutine, so one buffer reused across shards is safe.
func streamSummaryFile(path string, br *bufio.Reader, opts ReadOpts, fn func(Summary) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := diagWriter(opts.Writer)

	br.Reset(f)
	for {
		frag, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			_, _ = fmt.Fprintf(w, msgOverLongLine, maxLineBytes, path)
			if derr := drainLine(br); derr != nil {
				if derr == io.EOF {
					break
				}
				return fmt.Errorf("reading localdebt file: %w", derr)
			}
			continue
		}
		if line := bytes.TrimSpace(frag); len(line) > 0 {
			if s, ok := decodeSummary(line, path, w); ok {
				if ferr := fn(s); ferr != nil {
					return callbackErr{ferr}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("reading localdebt file: %w", err)
		}
	}
	return nil
}

// StreamSummaries walks every *.jsonl month shard under dir and calls fn once per
// decodable record, in ReadAll's exact order: shards in os.ReadDir (lexical) order,
// lines in file order. That ordering is load-bearing, not incidental — the fold's
// full-tie rule (equal timestamp, equal status rank) is "last append wins", which
// is positional, so a map-ordered or concurrent walk would make FoldSummaries
// non-deterministic for same-second records and let it disagree with FoldRecords.
//
// A missing directory is empty (nil), not an error — the "no backlog yet" state,
// matching ReadAll. Non-.jsonl entries are ignored. Errors are redacted through
// basePathErr per the package's path-disclosure contract (store.go SECURITY
// block). An error returned by fn aborts the walk and is propagated as-is.
//
// Nothing is accumulated here: a caller that only needs a set of ids retains
// nothing but that set. Use ReadSummaries when the whole set is genuinely needed
// at once (the fold), and ReadAll when full records are needed (rendering).
func StreamSummaries(dir string, opts ReadOpts, fn func(Summary) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading localdebt dir: %w", basePathErr(err))
	}
	// One maxLineBytes buffer for the whole walk, reset per shard (see
	// streamSummaryFile): a fresh 1 MiB buffer per month would dominate this path's
	// allocation on a store with any real history. Allocated lazily, on the first
	// entry that is actually a shard, so the "no backlog yet" and non-shard-entries
	// cases pay nothing.
	var br *bufio.Reader
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if br == nil {
			br = bufio.NewReaderSize(bytes.NewReader(nil), maxLineBytes)
		}
		if err := streamSummaryFile(filepath.Join(dir, e.Name()), br, opts, fn); err != nil {
			// A caller's own error is neither a missing shard nor a path to redact:
			// unwrap it and return it verbatim so it stays usable as an early-exit
			// signal. Testing it with os.IsNotExist/basePathErr first would let a
			// sentinel that happens to wrap fs.ErrNotExist be swallowed as a
			// disappeared shard, silently reporting success on an aborted walk.
			var cb callbackErr
			if errors.As(err, &cb) {
				return cb.err
			}
			if os.IsNotExist(err) {
				continue
			}
			return basePathErr(err)
		}
	}
	return nil
}

// callbackErr distinguishes an error returned by a StreamSummaries callback from
// an error raised by the read itself, so the two are never confused for each other
// in the walk's error handling.
type callbackErr struct{ err error }

func (e callbackErr) Error() string { return e.err.Error() }
func (e callbackErr) Unwrap() error { return e.err }

// ReadSummaries collects StreamSummaries into a slice, for the one caller that
// genuinely needs every summary at once: the fold cannot decide an id's effective
// status until it has seen all of that id's records.
//
// On a mid-walk error it returns the summaries decoded SO FAR alongside the error,
// not a nil slice, so a caller can salvage what was readable.
//
// WARNING: a partial result is NOT safe to FOLD. FoldSummaries decides an id's
// effective state from that id's complete record history, so a truncated read
// silently produces wrong effective statuses — an id whose resolution lived in the
// unread shard folds to `open`. cli/reconcile.go's dedup seed discards the partial
// result for exactly this reason (see doc.go's Deduplication contract). Use it only
// for questions that do not depend on the fold.
func ReadSummaries(dir string, opts ReadOpts) ([]Summary, error) {
	var out []Summary
	err := StreamSummaries(dir, opts, func(s Summary) error {
		out = append(out, s)
		return nil
	})
	return out, err
}

// foldID, foldStatus and foldTimestamp implement the foldable interface for
// Summary, so FoldSummaries and FoldRecords are two instantiations of ONE
// precedence rule rather than two copies that drift apart the next time the status
// semantics change.
func (s Summary) foldID() string        { return s.ID }
func (s Summary) foldStatus() string    { return s.Status }
func (s Summary) foldTimestamp() string { return s.Timestamp }

// FoldSummaries folds summaries by id to their effective state, applying exactly
// the precedence rule FoldRecords applies (see its doc comment for the rule and
// why it is what it is). It exists so the write-time dedup seed can decide an id's
// effective status without materializing full records.
//
// Because both functions call the same foldByID helper, a change to the fold
// automatically reaches both; TestFoldSummaries_MatchesFoldRecords pins that they
// agree on id ordering and on the winning record per id.
func FoldSummaries(sums []Summary) []Summary {
	folded, _ := foldByID(sums)
	return folded
}
