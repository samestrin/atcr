package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LoadTelemetrySetting resolves the persisted telemetry opt-out from
// .atcr/config.yaml under root, WITHOUT requiring a valid roster the way the
// strict LoadProjectConfig does — so the opt-out gate can read it on every
// command (including reconcile, which loads no project config) at negligible
// cost. It returns:
//
//   - (nil, nil) when the config file is absent OR present without a telemetry
//     key: the setting is unset (neutral), contributing nothing to the gate;
//   - (&value, nil) for an explicit telemetry: true/false;
//   - (nil, err) when the file is unreadable or the telemetry value is malformed
//     (e.g. telemetry: maybe) — a corrupt value must surface, never silently
//     fall open to enabled.
func LoadTelemetrySetting(root string) (*bool, error) {
	path := DefaultProjectConfigPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// Permissive decode: only the telemetry field is read, unknown sibling keys
	// (agents, payload_mode, …) are ignored, so a roster-less or partial config
	// still resolves. A non-boolean telemetry value fails here, by design.
	var probe struct {
		Telemetry *bool `yaml:"telemetry"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse %s telemetry setting: %w", filepath.Base(path), err)
	}
	return probe.Telemetry, nil
}

// SetTelemetrySetting persists enabled to the telemetry key of the existing
// .atcr/config.yaml under root, mutating ONLY that key via a yaml.Node edit so
// every other key (and its comments) survives untouched. The config file must
// already exist — a missing file is returned as a wrapped I/O error (an
// environment failure, not a usage mistake); this never creates the file.
func SetTelemetrySetting(root string, enabled bool) error {
	path := DefaultProjectConfigPath(root)
	// A symlinked config would be silently severed: Stat/ReadFile follow the
	// link while the atomic Rename replaces the link itself with a regular file,
	// writing to the wrong logical location. Reject up front.
	if li, err := os.Lstat(path); err == nil && li.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("config %s: symlinked configs are unsupported — rename would sever the link; use a regular file", path)
	}

	dir := filepath.Dir(path)
	return withConfigLock(dir, "set-telemetry", func() error {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}
		mapping, err := configMapping(&doc, filepath.Base(path))
		if err != nil {
			return err
		}
		setMappingBool(mapping, "telemetry", enabled)

		out, err := yaml.Marshal(&doc)
		if err != nil {
			return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
		}
		// Atomic replace (temp + rename) so a rename-swap never leaves a half-written
		// file at the live path: the temp is fully written and fsync'd, then the rename
		// flips the name atomically. Rename alone only atomizes the name swap — fsync
		// of the temp's data and the parent dir is what makes the write durable across
		// a crash; without it a crash can leave the renamed file's blocks un-persisted.
		// This write hardens beyond the trust-store write (trust.go Save), which is
		// Chmod/Write/Close/Rename only — no temp fsync, no dir fsync.
		tmp, err := os.CreateTemp(dir, ".config-*.tmp")
		if err != nil {
			return fmt.Errorf("create %s temp: %w", filepath.Base(path), err)
		}
		tmpName := tmp.Name()
		defer func() { _ = os.Remove(tmpName) }() // no-op once renamed
		if err := tmp.Chmod(info.Mode().Perm()); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chmod %s temp: %w", filepath.Base(path), err)
		}
		if _, err := tmp.Write(out); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("write %s temp: %w", filepath.Base(path), err)
		}
		// fsync the temp's data before the rename so the renamed file's blocks are
		// persisted, not just the name swap.
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("sync %s temp: %w", filepath.Base(path), err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("close %s temp: %w", filepath.Base(path), err)
		}
		if err := os.Rename(tmpName, path); err != nil {
			return fmt.Errorf("replace %s: %w", path, err)
		}
		// fsync the parent directory so the rename (a directory-entry update) is
		// durable too; otherwise a crash can roll the live path back to the old file.
		if err := syncDir(dir); err != nil {
			return fmt.Errorf("sync %s dir: %w", filepath.Base(path), err)
		}
		return nil
	})
}

// withConfigLock acquires a mkdir-based advisory lock under the config directory
// to serialize concurrent reads-modify-writes to config.yaml.
func withConfigLock(dir, session string, fn func() error) error {
	lockDir := filepath.Join(dir, "config.lock")
	ownerFile := filepath.Join(lockDir, "owner.txt")

	// Ensure the parent directory (.atcr/) exists.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config lock parent directories: %w", err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		err := os.Mkdir(lockDir, 0o755)
		if err == nil {
			// Acquired. Write owner metadata and ensure cleanup.
			epoch := time.Now().Unix()
			_ = os.WriteFile(ownerFile, []byte(fmt.Sprintf("session=%s|epoch=%d", session, epoch)), 0o644)
			defer func() { _ = os.RemoveAll(lockDir) }()
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("acquire config lock: %w", err)
		}

		// Lock is held. Check for stale owner before waiting.
		if data, err := os.ReadFile(ownerFile); err == nil {
			if e := parseConfigOwnerEpoch(data); e > 0 {
				if time.Since(time.Unix(e, 0)) > 300*time.Second {
					_ = os.RemoveAll(lockDir)
					continue
				}
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout acquiring config lock at %s", lockDir)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func parseConfigOwnerEpoch(data []byte) int64 {
	s := strings.TrimSpace(string(data))
	const prefix = "epoch="
	i := strings.Index(s, prefix)
	if i < 0 {
		return 0
	}
	n, err := strconv.ParseInt(s[i+len(prefix):], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// configMapping returns the top-level mapping node to mutate, tolerating an
// empty/whitespace-only config file by synthesizing an empty document + mapping
// in place (so `config set` can record an opt-out on a stub config). A document
// whose root is a non-mapping (e.g. a YAML list) is rejected — a key cannot be
// set on it.
func configMapping(doc *yaml.Node, name string) (*yaml.Node, error) {
	if doc.Kind == 0 || len(doc.Content) == 0 {
		// Empty document: build `{}` so the key can be appended.
		mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{mapping}
		return mapping, nil
	}
	if doc.Kind != yaml.DocumentNode || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: not a valid config mapping", name)
	}
	return doc.Content[0], nil
}

// setMappingBool sets key to a boolean value on a YAML mapping node, updating an
// existing key in place (preserving its position/comments) or appending a new
// key/value pair when absent. A mapping node stores content as alternating
// key,value scalar pairs.
func setMappingBool(mapping *yaml.Node, key string, val bool) {
	valNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: boolLiteral(val)}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			// Mutate the existing value node in place so its LineComment/FootComment
			// survive (a wholesale node swap would discard them), honoring the doc
			// comment's "preserving its position/comments" promise.
			existing := mapping.Content[i+1]
			existing.Kind = yaml.ScalarNode
			existing.Tag = "!!bool"
			existing.Value = boolLiteral(val)
			existing.Content = nil
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	mapping.Content = append(mapping.Content, keyNode, valNode)
}

func boolLiteral(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// syncDir fsyncs the directory holding the config so the rename that swapped in
// the new config is durable across a crash — the rename updates a directory
// entry, which the filesystem must persist separately from the file's data.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
