package tdmigrate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DecodeShardStrict decodes a single YAML shard document with strict-load
// semantics: unknown fields are rejected (KnownFields), tabs in indentation and
// other malformed YAML surface as decode errors, and the decoded shard is
// schema-validated. This converts "silent bad YAML" into a loud error.
//
// A shard file is required to contain exactly one YAML document: a trailing
// `---`-separated document is rejected rather than silently discarded, since
// yaml.v3's Decode only ever reads the first document and would otherwise
// truncate a malformed multi-document shard without error.
func DecodeShardStrict(data []byte) (Shard, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var s Shard
	if err := dec.Decode(&s); err != nil {
		return Shard{}, fmt.Errorf("yaml decode: %w", err)
	}
	if err := s.Validate(); err != nil {
		return Shard{}, fmt.Errorf("schema: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Shard{}, fmt.Errorf("yaml decode: shard file contains more than one YAML document")
		}
		return Shard{}, fmt.Errorf("yaml decode: %w", err)
	}
	return s, nil
}

// ValidationError aggregates per-file validation failures so the gate reports
// every bad shard in one pass rather than stopping at the first.
type ValidationError struct {
	Failures []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%d invalid shard(s):\n  %s",
		len(e.Failures), strings.Join(e.Failures, "\n  "))
}

// ValidateDir strict-loads and schema-checks every *.yaml shard in dir. It
// returns a *ValidationError listing each failure, or nil if all shards pass.
func ValidateDir(dir string) (int, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return 0, err
	}
	sort.Strings(files)
	var failures []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(f), err))
			continue
		}
		if _, err := DecodeShardStrict(data); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(f), err))
		}
	}
	if len(failures) > 0 {
		return len(files), &ValidationError{Failures: failures}
	}
	return len(files), nil
}
