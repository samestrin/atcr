// Package registry loads and validates the two-tier configuration:
// ~/.config/atcr/registry.yaml (providers + agents) and .atcr/config.yaml
// (roster, payload mode, timeouts, fail-on), with strict YAML parsing,
// precedence resolution, and fallback-chain validation.
package registry
