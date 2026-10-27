package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/verify"
	"github.com/spf13/cobra"
)

// allowCloudVisionFlag names `atcr review --allow-cloud-vision` (Epic
// 36.0.2): the opt-in that lets the visual review send screenshots to a vision
// agent whose provider is not on a loopback host.
const allowCloudVisionFlag = "allow-cloud-vision"

// visualStageFromFlags builds the visual review stage --preview-url asks for,
// or nil when there is nothing to run: no --preview-url (the review then skips
// felix with a warning), or felix not in the roster (warned here). With
// --preview-url and felix in the roster every missing piece is a usage error
// raised before any provider call: a baseline scan, no usable vision agent,
// felix's own agent or persona, and the sandbox the browser runs the
// model-written spec in.
func visualStageFromFlags(ctx context.Context, cmd *cobra.Command, cfg *fanout.ReviewConfig, baseline bool) (fanout.VisualReviewer, error) {
	previewURL, _ := cmd.Flags().GetString(previewURLFlag)
	allowCloud := boolFlag(cmd, allowCloudVisionFlag)
	if strings.TrimSpace(previewURL) == "" {
		if allowCloud {
			return nil, usageError(errors.New("--allow-cloud-vision only applies with --preview-url"))
		}
		return nil, nil
	}
	if !slices.Contains(cfg.Project.Agents, fanout.VisualPersonaName) && !slices.Contains(cfg.Project.SerialAgents, fanout.VisualPersonaName) {
		log.FromContext(ctx).Warn("--preview-url is set but felix is not in the roster, so no visual review runs; add felix to agents: in .atcr/config.yaml")
		return nil, nil
	}
	if baseline {
		return nil, usageError(errors.New("--preview-url applies to git-range reviews only; it cannot be combined with --all or --scope"))
	}
	agent, err := fanout.SelectVisionAgent(cfg.Registry, cfg.Settings, allowCloud)
	if err != nil {
		return nil, usageError(fmt.Errorf("--preview-url: %w", err))
	}
	if cfg.Project.Sandbox == nil {
		return nil, usageError(errors.New("--preview-url needs a sandbox: block in .atcr/config.yaml; the browser runs the model-written spec only inside a sandbox"))
	}
	author, err := newFelixAuthor(ctx, cfg)
	if err != nil {
		return nil, usageError(fmt.Errorf("--preview-url: %w", err))
	}
	sb, _, _, err := verify.ResolveExecBackend(ctx, true, cfg.Project.Sandbox)
	if err != nil {
		return nil, usageError(fmt.Errorf("--preview-url: sandbox: %w", err))
	}
	browser := fanout.NewPlaywrightBackend(sb)
	if err := browser.Preflight(ctx); err != nil {
		return nil, usageError(fmt.Errorf("--preview-url: browser: %w", err))
	}
	return &fanout.VisualStage{
		Author:     author,
		Browser:    browser,
		Vision:     hookobs.Wrap(ctx, llmclient.New()),
		Agent:      agent,
		Settings:   cfg.Settings,
		PreviewURL: previewURL,
	}, nil
}

// felixAuthor is call 1's SpecAuthor: felix's own agents: entry and persona.
type felixAuthor struct {
	persona string
	inv     llmclient.Invocation
	timeout time.Duration
	llm     hookobs.Client
}

// newFelixAuthor resolves felix's agent, provider and persona from the
// registry, the way the reviewers' are.
func newFelixAuthor(ctx context.Context, cfg *fanout.ReviewConfig) (*felixAuthor, error) {
	name := fanout.VisualPersonaName
	ac, ok := cfg.Registry.Agents[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found in registry", name)
	}
	prov, ok := cfg.Registry.Providers[ac.Provider]
	if !ok {
		return nil, fmt.Errorf("agent %q: provider %q not found in registry", name, ac.Provider)
	}
	persona, err := registry.ResolvePersona(name, ac.Persona, nil, cfg.PersonaDirs)
	if err != nil {
		return nil, fmt.Errorf("agent %q: %w", name, err)
	}
	timeoutSecs := ac.EffectiveTimeoutSecs(cfg.Settings)
	if timeoutSecs <= 0 {
		timeoutSecs = registry.DefaultTimeoutSecs
	}
	maxTokens := registry.DefaultMaxTokens
	switch {
	case cfg.Settings.MaxTokens > 0:
		maxTokens = cfg.Settings.MaxTokens
	case ac.MaxTokens != nil && *ac.MaxTokens > 0:
		maxTokens = *ac.MaxTokens
	}
	return &felixAuthor{
		persona: persona.Text,
		inv: llmclient.Invocation{
			BaseURL:          prov.BaseURL,
			APIKeyEnv:        prov.APIKeyEnv,
			Model:            ac.Model,
			Temperature:      ac.Temperature,
			MaxTokens:        &maxTokens,
			Thinking:         ac.Thinking,
			ThinkingLevel:    ac.ThinkingLevel,
			ThinkingStyle:    ac.ThinkingStyle,
			PreserveThinking: ac.PreserveThinking,
		},
		timeout: time.Duration(timeoutSecs) * time.Second,
		llm:     hookobs.Wrap(ctx, llmclient.New()),
	}, nil
}

func (a *felixAuthor) Persona() string { return a.persona }

// Complete sends prompt to felix's model under its own timeout.
func (a *felixAuthor) Complete(ctx context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	inv := a.inv
	inv.Prompt = prompt
	return a.llm.Complete(ctx, inv)
}

var _ fanout.SpecAuthor = (*felixAuthor)(nil)
