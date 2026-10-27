package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/threatmap"
	"github.com/spf13/cobra"
)

// threatMapFlag is the opt-in threat-model mapping phase (Epic 37.0).
const threatMapFlag = "threat-map"

// threatMapAdapter runs threatmap.Map against one mapper agent, the shape
// fanout.ThreatMapper takes.
type threatMapAdapter struct{ completer threatmap.Completer }

func (a threatMapAdapter) Map(ctx context.Context, paths []string) ([]string, error) {
	return threatmap.Map(ctx, a.completer, paths)
}

// threatMapperFromFlags builds the mapper --threat-map asks for, or nil when
// the flag is not set. The agent comes from the resolved threat_map.agent
// setting; an unset or unknown agent is a usage error raised before any
// provider call, because the operator asked for a scoped review and running
// it unscoped would be a silent downgrade. Failures of the mapper CALL are
// different: they keep every file and warn (fanout's fail-open path).
func threatMapperFromFlags(ctx context.Context, cmd *cobra.Command, cfg *fanout.ReviewConfig) (fanout.ThreatMapper, error) {
	if !boolFlag(cmd, threatMapFlag) {
		return nil, nil
	}
	name := cfg.Settings.ThreatMap.Agent
	if name == "" {
		return nil, usageError(errors.New("--threat-map needs a mapper agent: set threat_map.agent in .atcr/config.yaml (or ~/.config/atcr/registry.yaml) to an agents: entry"))
	}
	agent, err := threatmap.NewAgent(name, cfg.Registry, cfg.Settings, cfg.PersonaDirs, newThreatMapLLM(ctx))
	if err != nil {
		return nil, usageError(fmt.Errorf("--threat-map: %w", err))
	}
	if agent.UsedLegacyKey() {
		log.FromContext(ctx).Warn(threatmap.LegacyKeyWarning)
	}
	return threatMapAdapter{completer: agent}, nil
}

// newThreatMapLLM is the mapper's provider client, observed like the
// reviewers' (newCompleter).
func newThreatMapLLM(ctx context.Context) threatmap.LLM {
	return hookobs.Wrap(ctx, llmclient.New())
}
