package fanout

import (
	"errors"
	"fmt"

	"github.com/samestrin/atcr/internal/registry"
)

// ErrVisionAgentUnset means no visual_review.agent is configured. It is a
// usage error: the operator asked for a visual review and the engine will not
// guess which agent may see the screenshots.
var ErrVisionAgentUnset = errors.New("the visual review needs a vision agent: set visual_review.agent in .atcr/config.yaml (or ~/.config/atcr/registry.yaml) to an agents: entry that declares supports_vision")

// ErrVisionAgentRefused means the configured agent may not receive
// screenshots: it is not in the registry, does not declare supports_vision, or
// sits on a non-loopback host without the cloud opt-in.
var ErrVisionAgentRefused = errors.New("vision agent refused")

// VisionAgent is the agent selected to review screenshots. Loopback is false
// only when allowCloud admitted a non-loopback provider, which means the
// screenshot pixels leave the machine.
type VisionAgent struct {
	Name     string
	Agent    registry.AgentConfig
	Provider registry.Provider
	Loopback bool
}

// SelectVisionAgent returns the visual_review.agent if it may receive
// screenshots: it must declare supports_vision, and its provider base_url must
// be a loopback host unless allowCloud is true. An empty base_url is the
// client's remote default, so it counts as non-loopback. An unset key returns
// ErrVisionAgentUnset; every refusal wraps ErrVisionAgentRefused. Both are
// raised before any provider call.
func SelectVisionAgent(reg *registry.Registry, settings registry.Settings, allowCloud bool) (VisionAgent, error) {
	name := settings.VisualReview.Agent
	if name == "" {
		return VisionAgent{}, ErrVisionAgentUnset
	}
	if reg == nil {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q: no registry", ErrVisionAgentRefused, name)
	}
	ac, ok := reg.Agents[name]
	if !ok {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q not found in registry", ErrVisionAgentRefused, name)
	}
	if !ac.SupportsVision {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q does not declare supports_vision", ErrVisionAgentRefused, name)
	}
	prov, ok := reg.Providers[ac.Provider]
	if !ok {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q: provider %q not found in registry", ErrVisionAgentRefused, name, ac.Provider)
	}
	loopback := prov.IsLoopback()
	if !loopback && !allowCloud {
		return VisionAgent{}, fmt.Errorf("%w: vision agent %q: provider %q is not on a loopback host, and screenshots go only to a local model unless --allow-cloud-vision is passed", ErrVisionAgentRefused, name, ac.Provider)
	}
	return VisionAgent{Name: name, Agent: ac, Provider: prov, Loopback: loopback}, nil
}
