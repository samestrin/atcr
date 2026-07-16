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
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: not a valid config mapping", filepath.Base(path))
	}
	setMappingBool(doc.Content[0], "telemetry", enabled)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
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
