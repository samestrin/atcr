package registry

import (
	"os"
	"strings"
)

// ResolveConsensus resolves the reconcile consensus filter level honoring the
// same file-tier precedence ResolveGateThreshold documents: explicit value
// (--consensus flag / consensus tool argument) > project config
// (.atcr/config.yaml) > user-global registry (~/.config/atcr/registry.yaml).
// The embedded DefaultConsensus is deliberately NOT applied here — the returned
// value is the raw configured string ("" = nothing configured) and the call site
// maps "" to strict, exactly as the gate resolver leaves its own default to its
// callers. Enum validation likewise stays at the call sites so each surface
// phrases the failure for itself (CLI usage error vs MCP tool error).
//
// consensus is intentionally absent from the project registry overlay
// (.atcr/registry.yaml): that file carries only providers and agents; shared
// settings including consensus live in .atcr/config.yaml and
// ~/.config/atcr/registry.yaml (see sharedSettingsKeys for the misplaced-key
// hint that enforces this).
//
// Error handling mirrors the gate resolver: a present-but-broken project config
// is an error (it is the repo's own config); a missing project config is
// skipped; a broken user-global registry is skipped best-effort so it never
// blocks a reconcile that does not otherwise need it.
func ResolveConsensus(root, explicit string) (string, error) {
	if v := strings.TrimSpace(explicit); v != "" {
		return v, nil
	}

	projPath := DefaultProjectConfigPath(root)
	if _, err := os.Stat(projPath); err == nil {
		proj, err := LoadProjectConfig(projPath)
		if err != nil {
			return "", err
		}
		if v := strings.TrimSpace(proj.Consensus); v != "" {
			return v, nil
		}
	}

	if regPath, err := DefaultRegistryPath(); err == nil {
		if _, serr := os.Stat(regPath); serr == nil {
			if reg, err := LoadRegistry(regPath); err == nil {
				if v := strings.TrimSpace(reg.Consensus); v != "" {
					return v, nil
				}
			}
		}
	}
	return "", nil
}
