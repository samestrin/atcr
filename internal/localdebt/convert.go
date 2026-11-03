package localdebt

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Conversion (Epic 92 T3) moves a store's month shards to the event layout
// (events.go), one way: every record of a month shard becomes its own event file,
// holding the shard line byte for byte, and the shard is removed. The store is
// opted in, so every later write is an event too.
//
// No record is lost, and that is checked rather than assumed: the run folds the
// store before, folds what the store would be after (the shards it keeps plus every
// event, the new ones included, read back from disk), and removes no shard unless
// the two folds are identical. Order is the one thing the event layout cannot keep:
// a month shard breaks a full tie (one id, one timestamp, one status) by line order,
// the event layout by file name, so a store holding such a tie can fold differently
// after conversion. Then the run refuses with ErrFoldChanged and changes nothing.
//
// A shard holding something this binary cannot carry into an event file is left
// whole: a corrupt or over-long line, a record from a newer atcr, or a record whose
// id or timestamp cannot name an event file. So is a .jsonl file that is not a
// month shard. Its records stay where they are and every read path keeps folding
// them (shard.go RecordFiles); the run reports each one and why.
//
// The run is staged so a crash cannot leave a record twice. New events are written
// to <store>/.convert/ (no read path looks there), the fold is checked over them,
// and only then is the COMMIT journal written, naming the shards to remove. Past
// that point the plan is verified and the run only finishes it: rename each staged
// event into items/, remove each named shard, remove .convert/. The next run finishes
// a COMMIT left by a crash (Resumed), and throws away staging that never got one.
const (
	convertSubdir  = ".convert"
	convertJournal = "COMMIT"
)

// ErrFoldChanged is returned when converting would make the store fold to different
// effective records. Nothing was changed.
var ErrFoldChanged = errors.New("converting would change how the store folds")

// convertAfterCommit is a test hook run after the COMMIT journal is written and
// before the plan is finished; an error stops the run there, as a crash would.
var convertAfterCommit func() error

// ConvertResult reports what a Convert or ConvertPlan call did, or would do.
type ConvertResult struct {
	// StoreFound is false when dir holds no record file at all.
	StoreFound bool
	// Records is the number of month-shard records moved into event files.
	Records int
	// Shards names the month shard files converted (and removed, unless planned).
	Shards []string
	// Kept lists the .jsonl files left in place, each with the reason.
	Kept []KeptShard
	// Items is the number of effective records, the same before and after.
	Items int
	// Resumed is true when the run first finished an interrupted conversion.
	Resumed bool
}

// KeptShard is one .jsonl file a conversion leaves in place.
type KeptShard struct {
	Name   string
	Reason string
}

// Convert converts the store at dir to the event layout; see the package comment
// above. It runs under the store lock, so no append lands mid-conversion.
func Convert(dir string, opts ReadOpts) (ConvertResult, error) {
	return convert(dir, opts, false)
}

// ConvertPlan reports what Convert would do, writing nothing to the store: the same
// plan and the same fold check, staged in a temporary directory outside it.
func ConvertPlan(dir string, opts ReadOpts) (ConvertResult, error) {
	return convert(dir, opts, true)
}

func convert(dir string, opts ReadOpts, dryRun bool) (ConvertResult, error) {
	var res ConvertResult
	// Sampled before the lock, which creates the store dir (as Compact does).
	if !hasShardFiles(dir) {
		return res, nil
	}
	err := withLock(dir, "convert", func() error {
		res.StoreFound = true
		staging := filepath.Join(dir, convertSubdir)
		if _, err := os.Stat(filepath.Join(staging, convertJournal)); err == nil {
			if dryRun {
				return errors.New("an interrupted conversion is pending; run `atcr debt convert` without --dry-run to finish it")
			}
			if err := finishConversion(dir); err != nil {
				return err
			}
			res.Resumed = true
		} else if !dryRun {
			if err := os.RemoveAll(staging); err != nil {
				return fmt.Errorf("removing unfinished localdebt conversion: %w", basePathErr(err))
			}
		}

		before, err := ReadAll(dir, opts)
		if err != nil {
			return err
		}
		res.Items = len(FoldRecords(before))
		plan, err := planConversion(dir)
		if err != nil {
			return err
		}
		res.Shards, res.Kept, res.Records = plan.shards, plan.kept, len(plan.events)
		if len(plan.shards) == 0 {
			if dryRun {
				return nil
			}
			return enableEventLayoutLocked(dir)
		}

		stageDir := staging
		if dryRun {
			if stageDir, err = os.MkdirTemp("", "atcr-debt-convert-*"); err != nil {
				return fmt.Errorf("staging localdebt conversion: %w", err)
			}
			defer func() { _ = os.RemoveAll(stageDir) }()
		} else if err := os.MkdirAll(staging, 0o700); err != nil {
			return fmt.Errorf("staging localdebt conversion: %w", basePathErr(err))
		}
		discard := func(err error) error {
			if !dryRun {
				_ = os.RemoveAll(staging)
			}
			return err
		}
		for _, ev := range plan.events {
			if err := os.WriteFile(filepath.Join(stageDir, ev.name), ev.line, 0o600); err != nil {
				return discard(fmt.Errorf("staging localdebt event: %w", basePathErr(err)))
			}
		}

		files, err := convertedFiles(dir, plan.shards, stageDir)
		if err != nil {
			return discard(err)
		}
		after, err := readRecordFiles(files, ReadOpts{Writer: io.Discard})
		if err != nil {
			return discard(err)
		}
		if ids := foldDiff(before, after); len(ids) > 0 {
			return discard(foldChangedError(ids))
		}
		if dryRun {
			return nil
		}

		if err := enableEventLayoutLocked(dir); err != nil {
			return discard(err)
		}
		if err := writeConvertJournal(staging, plan.shards); err != nil {
			return discard(err)
		}
		if convertAfterCommit != nil {
			if err := convertAfterCommit(); err != nil {
				return err
			}
		}
		if err := finishConversion(dir); err != nil {
			return fmt.Errorf("%w; the conversion is verified and committed, run `atcr debt convert` again to finish it", err)
		}
		return nil
	})
	if err != nil {
		return ConvertResult{}, err
	}
	return res, nil
}

// conversionPlan is what a conversion would move: the month shards it converts, the
// event file each of their records becomes, and the .jsonl files it keeps.
type conversionPlan struct {
	shards []string
	events []stagedEvent
	kept   []KeptShard
}

type stagedEvent struct {
	name string
	line []byte
}

// planConversion classifies every .jsonl file in the store. A month shard converts
// only when every line in it decodes and names an event file; anything less and the
// whole shard is kept, so a conversion never removes a line it did not carry.
func planConversion(dir string) (conversionPlan, error) {
	var plan conversionPlan
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return plan, nil
		}
		return plan, fmt.Errorf("reading localdebt dir: %w", basePathErr(err))
	}
	for _, e := range entries {
		if !IsShardEntry(e) {
			continue
		}
		name := e.Name()
		keep := func(reason string) { plan.kept = append(plan.kept, KeptShard{Name: name, Reason: reason}) }
		if !monthRe.MatchString(strings.TrimSuffix(name, shardExt)) {
			keep("not a month shard")
			continue
		}
		var newer int
		var lines [][]byte
		recs, health, err := scanShard(filepath.Join(dir, name), ReadOpts{Writer: io.Discard},
			func([]byte) { newer++ },
			func(line []byte) { lines = append(lines, line) })
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return plan, basePathErr(err)
		}
		switch {
		case health.unrepresentable:
			keep("holds a line too long to read")
			continue
		case health.malformed:
			keep("holds a line that is not a valid record")
			continue
		case newer > 0:
			keep("holds a record written by a newer atcr")
			continue
		}
		events := make([]stagedEvent, 0, len(recs))
		for i, r := range recs {
			line := append(lines[i], '\n')
			evName, err := eventFileName(r, line)
			if err != nil {
				break
			}
			events = append(events, stagedEvent{name: evName, line: line})
		}
		if len(events) < len(recs) {
			keep("holds a record whose id or timestamp cannot name an event file")
			continue
		}
		plan.shards = append(plan.shards, name)
		plan.events = append(plan.events, events...)
	}
	return plan, nil
}

// convertedFiles lists the record files the store would hold after the conversion,
// in RecordFiles order: the month shards it keeps, then every event file, existing
// and staged, by name. A staged event whose name already exists in items/ is the
// same bytes (the name hashes them), so it is listed once.
func convertedFiles(dir string, converted []string, stageDir string) ([]string, error) {
	gone := map[string]bool{}
	for _, s := range converted {
		gone[s] = true
	}
	shards, err := shardFilePaths(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range shards {
		if !gone[filepath.Base(p)] {
			files = append(files, p)
		}
	}
	events, err := eventFilePaths(dir)
	if err != nil {
		return nil, err
	}
	byName := map[string]string{}
	for _, p := range events {
		byName[filepath.Base(p)] = p
	}
	staged, err := os.ReadDir(stageDir)
	if err != nil {
		return nil, fmt.Errorf("reading localdebt conversion staging: %w", basePathErr(err))
	}
	for _, e := range staged {
		if _, ok := byName[e.Name()]; !ok && IsEventEntry(e) {
			byName[e.Name()] = filepath.Join(stageDir, e.Name())
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		files = append(files, byName[n])
	}
	return files, nil
}

// foldDiff returns, sorted, the ids whose effective record differs between the two
// record sets, including an id present in only one of them.
func foldDiff(before, after []Record) []string {
	sig := func(recs []Record) map[string]string {
		m := map[string]string{}
		for _, r := range FoldRecords(recs) {
			b, err := json.Marshal(r)
			if err != nil {
				m[r.ID] = "unencodable: " + err.Error()
				continue
			}
			m[r.ID] = string(b)
		}
		return m
	}
	b, a := sig(before), sig(after)
	var ids []string
	for id, v := range b {
		if a[id] != v {
			ids = append(ids, id)
		}
	}
	for id := range a {
		if _, ok := b[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func foldChangedError(ids []string) error {
	shown := ids
	if len(shown) > 5 {
		shown = shown[:5]
	}
	more := ""
	if len(ids) > len(shown) {
		more = fmt.Sprintf(" and %d more", len(ids)-len(shown))
	}
	return fmt.Errorf("%w for %d item(s): %s%s; nothing was changed. Records of one item with the same "+
		"timestamp and status keep their line order in a month shard but not in the event layout",
		ErrFoldChanged, len(ids), strings.Join(shown, ", "), more)
}

// writeConvertJournal commits a verified plan: the COMMIT file naming the shards to
// remove, written to a temp name and renamed so it is whole or absent.
func writeConvertJournal(staging string, shards []string) error {
	b, err := json.Marshal(shards)
	if err != nil {
		return fmt.Errorf("writing localdebt conversion journal: %w", err)
	}
	tmp := filepath.Join(staging, "."+convertJournal+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("writing localdebt conversion journal: %w", basePathErr(err))
	}
	if err := os.Rename(tmp, filepath.Join(staging, convertJournal)); err != nil {
		return fmt.Errorf("writing localdebt conversion journal: %w", basePathErr(err))
	}
	return nil
}

// finishConversion carries out a committed plan. Every step is safe to repeat, so a
// run that dies here is finished by the next one: publish the staged events, then
// remove the named shards, then the staging directory and its journal.
func finishConversion(dir string) error {
	staging := filepath.Join(dir, convertSubdir)
	b, err := os.ReadFile(filepath.Join(staging, convertJournal))
	if err != nil {
		return fmt.Errorf("reading localdebt conversion journal: %w", basePathErr(err))
	}
	var shards []string
	if err := json.Unmarshal(b, &shards); err != nil {
		return fmt.Errorf("reading localdebt conversion journal: %w", err)
	}
	// The journal sits in the store, which may be a shared repository: only a bare
	// month shard name is ever removed, never a path.
	for _, s := range shards {
		if s != filepath.Base(s) || !monthRe.MatchString(strings.TrimSuffix(s, shardExt)) || !strings.HasSuffix(s, shardExt) {
			return fmt.Errorf("localdebt conversion journal names %q, which is not a month shard", s)
		}
	}
	if err := enableEventLayoutLocked(dir); err != nil {
		return err
	}
	staged, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("reading localdebt conversion staging: %w", basePathErr(err))
	}
	for _, e := range staged {
		if !IsEventEntry(e) {
			continue
		}
		if err := os.Rename(filepath.Join(staging, e.Name()), filepath.Join(ItemsDir(dir), e.Name())); err != nil {
			return fmt.Errorf("publishing localdebt event: %w", basePathErr(err))
		}
	}
	for _, s := range shards {
		if err := os.Remove(filepath.Join(dir, s)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing converted localdebt shard: %w", basePathErr(err))
		}
	}
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("removing localdebt conversion staging: %w", basePathErr(err))
	}
	return nil
}
