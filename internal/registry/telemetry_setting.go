package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	// Atomic replace (temp + rename) so a crash or full disk mid-write can never
	// truncate the whole config — roster and every other key — into a state the
	// strict loader rejects. Mirrors the trust-store write (trust.go).
	dir := filepath.Dir(path)
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s temp: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
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
			mapping.Content[i+1] = valNode
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
