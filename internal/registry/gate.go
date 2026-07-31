package registry

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
// The tier walk itself lives in resolveFileTiered (shared_settings.go) so this
// resolver and the consensus resolver cannot drift apart; a caller that needs
// BOTH settings should use ResolveSharedSettings, which answers them from one
// load.
func ResolveGateThreshold(root, explicit string) (string, error) {
	return resolveFileTiered(root, explicit, projFailOn, regFailOn)
}
