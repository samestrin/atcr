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
// The write is atomic-on-failure: all shards are marshaled in memory and staged
// to dir/.shards-staging before any existing shards are removed, so a mid-write
// error leaves dir intact (the README remains authoritative for re-running migrate).
func WriteShards(dir string, shards []Shard) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	// Marshal all shards in memory first; a failure here leaves dir untouched.
	used := map[string]bool{}
	type pending struct {
		name string
		data []byte
	}
	todo := make([]pending, 0, len(shards))
	for _, s := range shards {
		if !shardDateFormat.MatchString(s.Date) {
			return nil, fmt.Errorf("shard %q: date %q does not match YYYY-MM-DD format", s.Label, s.Date)
		}
		data, err := MarshalShard(s)
		if err != nil {
			return nil, fmt.Errorf("marshal shard %s/%s: %w", s.Date, s.Label, err)
		}
		todo = append(todo, pending{ShardFilename(s, used), data})
	}

	// Prepare a staging directory inside dir (same filesystem → rename is atomic).
	// A leftover staging dir from a previous crash is safe to remove; a file at
	// that path indicates an unexpected collision and is treated as an error.
	staging := filepath.Join(dir, ".shards-staging")
	if info, err := os.Stat(staging); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("staging path %s exists but is not a directory", staging)
		}
		if err := os.RemoveAll(staging); err != nil {
			return nil, err
		}
	}
	if err := os.Mkdir(staging, 0o755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)

	for _, p := range todo {
		if err := os.WriteFile(filepath.Join(staging, p.name), p.data, 0o644); err != nil {
			return nil, err
		}
	}

	// All staging writes succeeded — prune old shards, then move new ones into place.
	existing, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	for _, f := range existing {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		if !bytes.Contains(data, []byte("date:")) {
			continue
		}
		if err := os.Remove(f); err != nil {
			return nil, err
		}
	}
	for _, p := range todo {
		if err := os.Rename(filepath.Join(staging, p.name), filepath.Join(dir, p.name)); err != nil {
			return nil, err
		}
	}

	var written []string
	for _, p := range todo {
		written = append(written, p.name)
	}
	sort.Strings(written)
	return written, nil
}

// LoadShards strict-loads every *.yaml shard in dir (unknown fields rejected,
// schema validated) and returns them sorted by filename for determinism.
func LoadShards(dir string) ([]Shard, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("load shards: %w", err)
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
