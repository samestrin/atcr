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
	nonSlugChars = regexp.MustCompile(`[^a-z0-9._-]+`)
	dashRuns     = regexp.MustCompile(`-{2,}`)
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
func WriteShards(dir string, shards []Shard) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
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
			// Not a shard file — leave it alone.
			continue
		}
		if err := os.Remove(f); err != nil {
			return nil, err
		}
	}
	used := map[string]bool{}
	var written []string
	for _, s := range shards {
		data, err := MarshalShard(s)
		if err != nil {
			return nil, fmt.Errorf("marshal shard %s/%s: %w", s.Date, s.Label, err)
		}
		name := ShardFilename(s, used)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, err
		}
		written = append(written, name)
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
