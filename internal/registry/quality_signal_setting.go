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

// SetQualitySignalSetting persists enabled to the quality_signal key of the
// existing .atcr/config.yaml under root, mutating ONLY that key via a yaml.Node
// edit so every other key (and its comments) survives untouched. It is the exact
// structural mirror of SetTelemetrySetting — same mkdir-lock, atomic
// temp+fsync+rename write path, and symlink rejection — reusing withConfigLock,
// configMapping, and setMappingBool verbatim (all key-agnostic). The config file
// must already exist — a missing file is returned as a wrapped I/O error (an
// environment failure, not a usage mistake); this never creates the file.
func SetQualitySignalSetting(root string, enabled bool) error {
	path := DefaultProjectConfigPath(root)
	dir := filepath.Dir(path)
	return withConfigLock(dir, "set-quality-signal", func() error {
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
		setMappingBool(mapping, "quality_signal", enabled)

		out, err := yaml.Marshal(&doc)
		if err != nil {
			return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
		}
		// Atomic replace (temp + fsync + rename + dir fsync), identical to
		// SetTelemetrySetting: the temp is fully written and fsync'd, then the rename
		// flips the name atomically, then the parent dir is fsync'd so the rename is
		// durable across a crash.
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
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("sync %s temp: %w", filepath.Base(path), err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("close %s temp: %w", filepath.Base(path), err)
		}
		// Re-check for a symlink INSIDE the lock, immediately before the atomic
		// rename, to close the TOCTOU: Stat/ReadFile above follow a link, but Rename
		// replaces the link itself with a regular file, writing to the wrong logical
		// location. A non-ErrNotExist Lstat error is a hard failure.
		if li, lerr := os.Lstat(path); lerr != nil {
			if !errors.Is(lerr, os.ErrNotExist) {
				return fmt.Errorf("stat %s before replace: %w", path, lerr)
			}
		} else if li.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("config %s: symlinked configs are unsupported — rename would sever the link; use a regular file", path)
		}
		if err := os.Rename(tmpName, path); err != nil {
			return fmt.Errorf("replace %s: %w", path, err)
		}
		if err := syncDir(dir); err != nil {
			return fmt.Errorf("sync %s dir: %w", filepath.Base(path), err)
		}
		return nil
	})
}
