package tdmigrate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	nonSlugChars    = regexp.MustCompile(`[^a-z0-9._-]+`)
	dashRuns        = regexp.MustCompile(`-{2,}`)
	shardDateFormat = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// sanitizeLabel turns a free-text section label into a filesystem-safe slug.
// The original label is preserved verbatim in the shard's `label` field; this
// only affects the filename.
func sanitizeLabel(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = dashRuns.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "section"
	}
	return s
}

// ShardFilename returns a deterministic, collision-free filename for a shard.
// used tracks names already chosen in this run so two same-date/same-label
// sections never clobber one another.
func ShardFilename(s Shard, used map[string]bool) string {
	base := fmt.Sprintf("%s_%s", s.Date, sanitizeLabel(s.Label))
	name := base + ".yaml"
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s-%d.yaml", base, i)
	}
	used[name] = true
	return name
}

// MarshalShard renders a shard as a YAML document. yaml.v3 emits valid,
// correctly-quoted YAML by construction, which is the first line of the
// YAML-safety plan (generate, do not hand-author the risky parts).
func MarshalShard(s Shard) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteShards writes one YAML file per shard into dir. It is idempotent: it
// prunes its own prior output (every *.yaml already in dir) before writing, so
// re-running migrate never leaves orphaned shards from a previous run. dir is
// owned entirely by this tool, so the prune is safe.
//
// The write is staged-then-swapped so a mid-run failure cannot half-wipe items/:
// every shard is marshaled and written to a sibling .yaml.tmp file first, and
// only once all shards are staged successfully does it prune the prior *.yaml set
// and rename the staged files into place. A marshal or staging-write failure
// aborts with the existing shards untouched (any partial .tmp output is removed).
func WriteShards(dir string, shards []Shard) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	// Clear any .tmp files left by a previously interrupted run so stale staging
	// output never lingers or collides with this run's renames.
	if stale, err := filepath.Glob(filepath.Join(dir, "*.yaml.tmp")); err == nil {
		for _, f := range stale {
			_ = os.Remove(f)
		}
	}

	type staged struct{ tmp, final string }
	var stages []staged
	cleanup := func() {
		for _, s := range stages {
			_ = os.Remove(s.tmp)
		}
	}

	used := map[string]bool{}
	var written []string
	for _, s := range shards {
		if !shardDateFormat.MatchString(s.Date) {
			cleanup()
			return nil, fmt.Errorf("shard %q: date %q does not match YYYY-MM-DD format", s.Label, s.Date)
		}
		data, err := MarshalShard(s)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("marshal shard %s/%s: %w", s.Date, s.Label, err)
		}
		name := ShardFilename(s, used)
		final := filepath.Join(dir, name)
		tmp := final + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			cleanup()
			return nil, err
		}
		stages = append(stages, staged{tmp: tmp, final: final})
		written = append(written, name)
	}

	// All shards staged. Prune the prior output, then swap staged files into
	// place. Renames are within dir (atomic, near-instant), so the destructive
	// window is minimal and only opens after every shard is known-good on disk.
	existing, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		cleanup()
		return nil, err
	}
	for _, f := range existing {
		if err := os.Remove(f); err != nil {
			cleanup()
			return nil, err
		}
	}
	for _, s := range stages {
		if err := os.Rename(s.tmp, s.final); err != nil {
			return nil, err
		}
	}

	sort.Strings(written)
	return written, nil
}

// LoadShards strict-loads every *.yaml shard in dir (unknown fields rejected,
// schema validated) and returns them sorted by filename for determinism.
func LoadShards(dir string) ([]Shard, error) {
	if info, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("shard directory %s: %w", dir, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("shard directory %s: not a directory", dir)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var shards []Shard
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		s, err := DecodeShardStrict(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		shards = append(shards, s)
	}
	return shards, nil
}
