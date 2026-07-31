package registry

import (
	"errors"
	"os"
	"strings"
)

// sharedTiers holds the two file tiers every shared setting reads — the project
// config (.atcr/config.yaml) and the user-global registry
// (~/.config/atcr/registry.yaml) — loaded at most once.
//
// Either field is nil when its tier is absent or (for the registry only) broken:
// a missing project config is skipped, a present-but-broken one is an error
// returned by loadSharedTiers, and a broken user-global registry is skipped
// best-effort so it never blocks a run that does not otherwise need it. That
// asymmetry is the documented contract of both resolvers below; loading the
// tiers once is what keeps them from drifting apart.
type sharedTiers struct {
	proj *ProjectConfig
	reg  *Registry
}

// loadSharedTiers reads both file tiers under root a single time.
func loadSharedTiers(root string) (*sharedTiers, error) {
	var t sharedTiers

	// No os.Stat pre-check: it cannot distinguish "absent" (skip this tier) from
	// "unreachable" (a permission-denied .atcr/ would silently drop the project
	// tier and hand control to the weaker user-global one), and it races the read
	// — a config deleted in between would flip a benign skip into a fatal "no
	// roster found". LoadProjectConfig wraps os.ErrNotExist, so absence is the
	// only condition skipped here; every other error, permission denied included,
	// is fatal.
	proj, err := LoadProjectConfig(DefaultProjectConfigPath(root))
	switch {
	case err == nil:
		t.proj = proj
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}

	if regPath, err := DefaultRegistryPath(); err == nil {
		if _, serr := os.Stat(regPath); serr == nil {
			if reg, err := LoadRegistry(regPath); err == nil {
				t.reg = reg
			}
		}
	}
	return &t, nil
}

// pick applies the documented tier precedence to one setting: project config >
// user-global registry > "" (nothing configured). A whitespace-only value is not
// a decision and falls through to the next tier.
func (t *sharedTiers) pick(pickProj func(*ProjectConfig) string, pickReg func(*Registry) string) string {
	if t.proj != nil {
		if v := strings.TrimSpace(pickProj(t.proj)); v != "" {
			return v
		}
	}
	if t.reg != nil {
		if v := strings.TrimSpace(pickReg(t.reg)); v != "" {
			return v
		}
	}
	return ""
}

// resolveFileTiered resolves one shared setting through explicit value > project
// config > user-global registry. An explicit value short-circuits before any
// file is touched, so a fully-explicit call costs no I/O at all.
//
// Enum validation is deliberately NOT applied here: the raw configured string is
// returned ("" = nothing configured) and each surface phrases the failure for
// itself (CLI usage error vs MCP tool error).
func resolveFileTiered(root, explicit string, pickProj func(*ProjectConfig) string, pickReg func(*Registry) string) (string, error) {
	if v := strings.TrimSpace(explicit); v != "" {
		return v, nil
	}
	tiers, err := loadSharedTiers(root)
	if err != nil {
		return "", err
	}
	return tiers.pick(pickProj, pickReg), nil
}

func projFailOn(p *ProjectConfig) string { return p.FailOn }
func regFailOn(r *Registry) string       { return r.FailOn }

func projConsensus(p *ProjectConfig) string { return p.Consensus }
func regConsensus(r *Registry) string       { return r.Consensus }

// ResolveSharedSettings resolves fail_on and consensus from a SINGLE load of the
// shared file tiers, honoring the same precedence the standalone resolvers
// document (explicit > project config > user-global registry).
//
// Entry points that need both — every reconcile write path does — should call
// this rather than the two resolvers back to back. Two independent resolvers
// parse .atcr/config.yaml twice and, under ATCR_REGISTRY_URL, issue two separate
// HTTP GETs; because the registry tier is swallowed best-effort, one fetch can
// succeed while the other fails, leaving the gate and the consensus level
// resolved from different tiers within one run. One load makes that impossible.
//
// The tiers are loaded only for the settings not given explicitly, so a call
// with both values explicit touches no file.
func ResolveSharedSettings(root, explicitFailOn, explicitConsensus string) (failOn, consensus string, err error) {
	failOn = strings.TrimSpace(explicitFailOn)
	consensus = strings.TrimSpace(explicitConsensus)
	if failOn != "" && consensus != "" {
		return failOn, consensus, nil
	}

	tiers, err := loadSharedTiers(root)
	if err != nil {
		return "", "", err
	}
	if failOn == "" {
		failOn = tiers.pick(projFailOn, regFailOn)
	}
	if consensus == "" {
		consensus = tiers.pick(projConsensus, regConsensus)
	}
	return failOn, consensus, nil
}
