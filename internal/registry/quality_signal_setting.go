package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadQualitySignalSetting resolves the persisted quality-signal opt-IN from
// .atcr/config.yaml under root, WITHOUT requiring a valid roster the way the
// strict LoadProjectConfig does — so the opt-in gate can read it on every command
// (including reconcile, which loads no project config) at negligible cost. It is
// the exact structural mirror of LoadTelemetrySetting; only the key it probes
// differs (the opt-IN vs opt-OUT semantics live in the caller's combining
// function, not here). It returns:
//
//   - (nil, nil) when the config file is absent OR present without a
//     quality_signal key: the setting is unset (neutral), contributing nothing to
//     the gate;
//   - (&value, nil) for an explicit quality_signal: true/false;
//   - (nil, err) when the file is unreadable or the quality_signal value is
//     malformed (e.g. quality_signal: maybe) — a corrupt value must surface, never
//     be silently coerced to enabled.
func LoadQualitySignalSetting(root string) (*bool, error) {
	path := DefaultProjectConfigPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// Permissive decode: only the quality_signal field is read, unknown sibling
	// keys (agents, telemetry, payload_mode, …) are ignored, so a roster-less or
	// partial config still resolves. A non-boolean value fails here, by design.
	var probe struct {
		QualitySignal *bool `yaml:"quality_signal"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse %s quality_signal setting: %w", filepath.Base(path), err)
	}
	return probe.QualitySignal, nil
}
