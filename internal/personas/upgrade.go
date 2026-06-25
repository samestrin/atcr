package personas

import (
	"fmt"
	"os"
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
	if err := os.WriteFile(dest, remoteData, 0o644); err != nil {
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

// isNewer reports whether remote is a newer version than local. Valid semver is
// compared structurally; otherwise any difference is treated as an upgrade.
func isNewer(local, remote string) bool {
	lv := "v" + strings.TrimPrefix(local, "v")
	rv := "v" + strings.TrimPrefix(remote, "v")
	if semver.IsValid(lv) && semver.IsValid(rv) {
		return semver.Compare(rv, lv) > 0
	}
	return local != remote
}
