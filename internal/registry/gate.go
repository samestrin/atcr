package registry

import (
	"os"
	"strings"
)

// ResolveGateThreshold resolves the reconcile gate severity honoring the
// documented file-tier precedence: explicit value (--fail-on flag / fail_on
// tool argument) > project config (.atcr/config.yaml) > user-global registry
// (~/.config/atcr/registry.yaml). The embedded default is deliberately NOT
// applied — an unconfigured project stays opt-in (no gate) rather than
// spuriously failing on the default HIGH. The returned value is the raw
// configured string ("" = no gate configured); enum validation stays at the
// call sites so each layer phrases the failure for its own surface (CLI usage
// error vs MCP tool error).
//
// fail_on is intentionally absent from the project registry overlay
// (.atcr/registry.yaml): that file carries only providers and agents; shared
// settings including fail_on live in .atcr/config.yaml and
// ~/.config/atcr/registry.yaml.
//
// Error handling: a present-but-broken project config is an error (it is the
// repo's own config); a missing project config is skipped; a broken user-global
// registry is skipped best-effort so it never blocks a reconcile that does not
// otherwise need it.
func ResolveGateThreshold(root, explicit string) (string, error) {
	if v := strings.TrimSpace(explicit); v != "" {
		return v, nil
	}

	projPath := DefaultProjectConfigPath(root)
	if _, err := os.Stat(projPath); err == nil {
		proj, err := LoadProjectConfig(projPath)
		if err != nil {
			return "", err
		}
		if v := strings.TrimSpace(proj.FailOn); v != "" {
			return v, nil
		}
	}

	if regPath, err := DefaultRegistryPath(); err == nil {
		if _, serr := os.Stat(regPath); serr == nil {
			if reg, err := LoadRegistry(regPath); err == nil {
				if v := strings.TrimSpace(reg.FailOn); v != "" {
					return v, nil
				}
			}
		}
	}
	return "", nil
}
