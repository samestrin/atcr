package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// Load reads the JSONL ledger at path and returns every record in file order.
// An absent ledger returns (nil, nil): a project that has never run a review is
// a valid empty audit trail, not an error. Blank lines and malformed (non-JSON)
// lines are skipped rather than failing the whole read: the ledger is
// append-only and the CLI offers no repair path, so a single torn write or stray
// byte must not permanently brick `atcr audit-report`. Only an IO/scan failure
// is an error.
func Load(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening audit ledger: %w", err)
	}
	defer func() { _ = f.Close() }()

	var records []Record
	sc := bufio.NewScanner(f)
	// Records are small, but raise the max token to 1MiB so a long line is never
	// silently truncated into a parse error.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue // skip a malformed line so the rest of the ledger stays queryable
		}
		records = append(records, rec)
	}
	if err := sc.Err(); err != nil {
		if !errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("reading audit ledger: %w", err)
		}
		// The scanner stopped at an oversized line. Continue with a reader that
		// tolerates arbitrarily long lines so the rest of the ledger is still
		// queryable; any unparseable fragment is skipped like other malformed data.
		r := bufio.NewReader(f)
		for {
			line, rerr := r.ReadString('\n')
			if len(line) > 0 {
				raw := bytes.TrimSpace([]byte(line))
				if len(raw) != 0 {
					var rec Record
					if jerr := json.Unmarshal(raw, &rec); jerr == nil {
						records = append(records, rec)
					}
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					break
				}
				return nil, fmt.Errorf("reading audit ledger: %w", rerr)
			}
		}
	}
	return records, nil
}
