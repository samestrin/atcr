package registry

import "strings"

// ThreatMapConfig is one tier's optional `threat_map` block (Epic 37.0),
// carried at the registry (~/.config/atcr/registry.yaml) and project
// (.atcr/config.yaml) tiers. Agent names the normal `agents:` entry the
// --threat-map phase calls; that entry's persona is the mapper prompt
// (nora when it sets none; the legacy key threat_mapper is an alias). The block is inert without --threat-map.
type ThreatMapConfig struct {
	Agent string `yaml:"agent,omitempty"`
}

// ThreatMap is the resolved `threat_map` setting. The zero value names no
// agent, and --threat-map then refuses to run rather than guessing one.
type ThreatMap struct {
	Agent string
}

// overlayThreatMap applies one tier's block onto dst. An unset or blank agent
// falls through to the lower tier, like every string setting.
func overlayThreatMap(dst *ThreatMap, cfg *ThreatMapConfig) {
	if cfg == nil {
		return
	}
	if v := strings.TrimSpace(cfg.Agent); v != "" {
		dst.Agent = v
	}
}
