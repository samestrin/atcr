package registry

// LoadQualitySignalSetting resolves the persisted quality-signal opt-IN from
// .atcr/config.yaml under root, WITHOUT requiring a valid roster the way the
// strict LoadProjectConfig does — so the opt-in gate can read it on every command
// (including reconcile, which loads no project config) at negligible cost. It
// delegates to the key-agnostic loadConfigBool with the quality_signal key — the
// identical code path as LoadTelemetrySetting (the opt-IN vs opt-OUT semantics
// live in the caller's combining function, not here). It returns:
//
//   - (nil, nil) when the config file is absent OR present without a
//     quality_signal key: the setting is unset (neutral), contributing nothing to
//     the gate;
//   - (&value, nil) for an explicit quality_signal: true/false;
//   - (nil, err) when the file is unreadable or the quality_signal value is
//     malformed (e.g. quality_signal: maybe) — a corrupt value must surface, never
//     be silently coerced to enabled.
func LoadQualitySignalSetting(root string) (*bool, error) {
	return loadConfigBool(root, "quality_signal")
}

// SetQualitySignalSetting persists enabled to the quality_signal key of the
// existing .atcr/config.yaml under root, mutating ONLY that key via a yaml.Node
// edit so every other key (and its comments) survives untouched. It delegates to
// the key-agnostic setConfigBool with the quality_signal key and the
// set-quality-signal lock session — the identical hardened write path (mkdir
// lock, atomic temp+fsync+rename, in-lock symlink rejection) as
// SetTelemetrySetting, so the two setters can no longer drift. The config file
// must already exist — a missing file is returned as a wrapped I/O error (an
// environment failure, not a usage mistake); this never creates the file.
func SetQualitySignalSetting(root string, enabled bool) error {
	return setConfigBool(root, "set-quality-signal", "quality_signal", enabled)
}
