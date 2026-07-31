package registry

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
// The tier walk itself lives in resolveFileTiered (shared_settings.go) so this
// resolver and the gate resolver cannot drift apart; a caller that needs BOTH
// settings should use ResolveSharedSettings, which answers them from one load.
func ResolveConsensus(root, explicit string) (string, error) {
	return resolveFileTiered(root, explicit, projConsensus, regConsensus)
}
