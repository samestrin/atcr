package registry

import (
	"net"
	"net/url"
	"strings"
)

// VisualReviewConfig is one tier's optional `visual_review` block (Epic
// 36.0.2), carried at the registry (~/.config/atcr/registry.yaml) and project
// (.atcr/config.yaml) tiers. Agent names the `agents:` entry that reviews
// screenshots; it must declare supports_vision, and its provider must be on a
// loopback host unless the run opts into a cloud model. Modelled on
// ThreatMapConfig.
type VisualReviewConfig struct {
	Agent string `yaml:"agent,omitempty"`
}

// VisualReview is the resolved `visual_review` setting. The zero value names
// no agent, and the visual stage then refuses to run rather than guessing one.
type VisualReview struct {
	Agent string
}

// overlayVisualReview applies one tier's block onto dst. An unset or blank
// agent falls through to the lower tier, like every string setting.
func overlayVisualReview(dst *VisualReview, cfg *VisualReviewConfig) {
	if cfg == nil {
		return
	}
	if v := strings.TrimSpace(cfg.Agent); v != "" {
		dst.Agent = v
	}
}

// IsLoopback reports whether the provider's base_url host is localhost, in
// 127.0.0.0/8 or ::1, so a request to it stays on this machine. An empty
// base_url means the client's built-in default endpoint, which is remote, so
// it is not loopback; so is a base_url that does not parse.
func (p Provider) IsLoopback() bool {
	if strings.TrimSpace(p.BaseURL) == "" {
		return false
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
