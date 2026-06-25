package personas

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/registry"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

// UpgradeResult reports the outcome of an Upgrade for one persona.
type UpgradeResult struct {
	Name        string
	FromVersion string
	ToVersion   string
	Upgraded    bool // remote was newer (and written, unless dryRun)
	UpToDate    bool // remote not newer than local
}

// Upgrade re-fetches name from baseURL, compares its version to the installed
// copy, and overwrites the local file when the remote is newer. dryRun reports
// what would change without writing. The fetched content is validated before
// any write, so invalid remote content never overwrites a good local file.
func Upgrade(client HTTPClient, baseURL, personasDir, name string, dryRun bool) (UpgradeResult, error) {
	dest, err := personaPath(personasDir, name)
	if err != nil {
		return UpgradeResult{}, err
	}
	localData, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return UpgradeResult{}, fmt.Errorf("persona %q is not installed", name)
		}
		return UpgradeResult{}, fmt.Errorf("failed to read installed persona %q: %w", name, err)
	}
	remoteData, err := FetchPersonaYAML(client, baseURL, name)
	if err != nil {
		return UpgradeResult{}, err
	}
	if err := registry.ValidateAgentYAML(name, remoteData); err != nil {
		return UpgradeResult{}, fmt.Errorf("persona %q failed validation: %w", name, err)
	}

	localVersion, err := versionOf(localData)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("installed persona %q is unparseable; aborting upgrade: %w", name, err)
	}
	remoteVersion, err := versionOf(remoteData)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("remote persona %q is unparseable; aborting upgrade: %w", name, err)
	}
	res := UpgradeResult{Name: name, FromVersion: localVersion, ToVersion: remoteVersion}
	if !isNewer(res.FromVersion, res.ToVersion) {
		res.UpToDate = true
		return res, nil
	}
	res.Upgraded = true
	if dryRun {
		return res, nil
	}
	// Guard against TOCTOU symlink attacks: if dest is a symlink, writing
	// through it would follow it and write outside the personas directory.
	if fi, lerr := os.Lstat(dest); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return UpgradeResult{}, fmt.Errorf("refusing to write persona to symlink at %s", dest)
	}
	// Atomic replace: stage to a sibling temp file and rename into place so
	// readers never observe a partially-written persona.
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("failed to create persona temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(remoteData); err != nil {
		_ = tmp.Close()
		return UpgradeResult{}, fmt.Errorf("failed to write persona temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return UpgradeResult{}, fmt.Errorf("failed to set persona temp file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return UpgradeResult{}, fmt.Errorf("failed to close persona temp file: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return UpgradeResult{}, fmt.Errorf("failed to write persona to %s: %w", dest, err)
	}
	return res, nil
}

// versionOf extracts the version metadata field, or "-" when absent. A corrupt
// YAML payload surfaces as an error so callers do not silently treat it as a
// missing version and overwrite a customized local persona.
func versionOf(data []byte) (string, error) {
	var fm personaFileMeta
	if err := yaml.Unmarshal(data, &fm); err != nil {
		return "-", fmt.Errorf("failed to parse persona metadata: %w", err)
	}
	if strings.TrimSpace(fm.Version) == "" {
		return "-", nil
	}
	return fm.Version, nil
}

// isNewer reports whether remote is a newer version than local. Valid semver
// is compared structurally. When exactly one side is valid semver the versions
// are not comparable, so the local copy is treated as up-to-date to avoid
// silently overwriting a newer or customized local persona. Otherwise any
// difference is treated as an upgrade.
func isNewer(local, remote string) bool {
	lv := "v" + strings.TrimPrefix(local, "v")
	rv := "v" + strings.TrimPrefix(remote, "v")
	lValid := semver.IsValid(lv)
	rValid := semver.IsValid(rv)
	switch {
	case lValid && rValid:
		return semver.Compare(rv, lv) > 0
	case lValid || rValid:
		return false
	default:
		return local != remote
	}
}
