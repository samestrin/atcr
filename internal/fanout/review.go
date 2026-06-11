package fanout

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
)

// byteBudget is the per-payload byte budget. v1 ships unlimited (0): no config
// knob exposes truncation yet, and the budget machinery (payload.ApplyByteBudget)
// stays wired so a future setting needs no plumbing change.
const byteBudget int64 = 0

// ReviewConfig bundles the loaded configuration a review needs. Built by
// LoadReviewConfig so both the CLI and the MCP server discover config the same way.
type ReviewConfig struct {
	Registry    *registry.Registry
	Project     *registry.ProjectConfig
	Settings    registry.Settings
	PersonaDirs registry.PersonaDirs
}

// ReviewRange is the resolved git range as plain provenance fields. The engine
// package cannot import gitrange (package-boundary rule), so the caller resolves
// the range and passes the result in.
type ReviewRange struct {
	Base          string
	Head          string
	DetectionMode string
	DefaultBranch string
	CommitCount   int
}

// ReviewRequest is everything RunReview needs beyond the config: the repo/range,
// the branch + date used to derive the review id, the collision suffix, the run
// start time, and an optional id override.
type ReviewRequest struct {
	Repo       string // git work tree to diff
	Root       string // where .atcr lives (usually == Repo)
	Range      ReviewRange
	Branch     string
	Date       string // YYYY-MM-DD
	TimeSuffix string // HHMMSS collision suffix
	StartedAt  time.Time
	IDOverride string
}

// ReviewResult is the outcome of a completed review run.
type ReviewResult struct {
	ID      string
	Dir     string
	Summary Summary
}

// LoadReviewConfig loads the registry and project config under root, validates
// the roster against the registry, resolves shared settings with the CLI
// overlays, and computes the persona search dirs.
func LoadReviewConfig(root string, cli registry.CLIOverrides) (*ReviewConfig, error) {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return nil, err
	}
	reg, err := registry.LoadRegistry(regPath)
	if err != nil {
		return nil, err
	}
	proj, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath(root))
	if err != nil {
		return nil, err
	}
	if err := proj.ValidateAgainst(reg); err != nil {
		return nil, err
	}
	settings, err := registry.ResolveSettings(cli, proj, reg)
	if err != nil {
		return nil, err
	}
	return &ReviewConfig{
		Registry: reg,
		Project:  proj,
		Settings: settings,
		PersonaDirs: registry.PersonaDirs{
			Project:  filepath.Join(root, ".atcr", "personas"),
			Registry: filepath.Join(filepath.Dir(regPath), "personas"),
		},
	}, nil
}

// RunReview is the full review flow: build per-mode payloads, assemble the
// roster into parallel/serial slots (with fallback chains), scaffold the review
// directory, fan out under the global timeout, then write per-agent artifacts,
// the merged pool, the manifest, and the latest pointer. The completer is
// injected so the CLI uses the real HTTP client and tests use a fake/httptest.
//
// Artifacts are always persisted, even when every agent fails; in that case the
// populated *ReviewResult is still returned alongside the wrapped
// ErrAllAgentsFailed so the caller can map it to exit 1 while the on-disk review
// remains for inspection.
func RunReview(ctx context.Context, completer Completer, cfg *ReviewConfig, req ReviewRequest) (*ReviewResult, error) {
	// Reject an empty roster before scaffolding so a no-op run never creates a
	// review directory or repoints .atcr/latest. (LoadReviewConfig also rejects
	// this earlier; RunReview is defended for direct/MCP callers.)
	if len(rosterNames(cfg.Project)) == 0 {
		return nil, ErrEmptyRoster
	}
	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
	if err != nil {
		return nil, err
	}
	slots, perAgentMode, err := buildSlots(cfg, payloads, req.Range)
	if err != nil {
		return nil, err
	}

	id, err := ReviewID(req.IDOverride, req.Branch, req.Date, req.TimeSuffix,
		func(candidate string) bool { return ReviewExists(req.Root, candidate) })
	if err != nil {
		return nil, err
	}
	dir, err := ScaffoldReviewDir(req.Root, id)
	if err != nil {
		return nil, err
	}
	if err := writePayloadArtifacts(dir, payloads); err != nil {
		return nil, err
	}

	runCtx := ctx
	if cfg.Settings.TimeoutSecs > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(cfg.Settings.TimeoutSecs)*time.Second)
		defer cancel()
	}
	results := NewEngine(completer).Run(runCtx, slots)

	sum, err := WritePool(filepath.Join(dir, "sources", "pool"), results)
	if err != nil {
		return nil, err
	}

	m := &payload.Manifest{
		Base:            req.Range.Base,
		Head:            req.Range.Head,
		DetectionMode:   req.Range.DetectionMode,
		DefaultBranch:   req.Range.DefaultBranch,
		CommitCount:     req.Range.CommitCount,
		PayloadMode:     cfg.Settings.PayloadMode,
		PerAgentPayload: perAgentMode,
		Roster:          rosterNames(cfg.Project),
		StartedAt:       req.StartedAt,
		Partial:         sum.Partial,
	}
	if err := WriteManifest(dir, m); err != nil {
		return nil, err
	}
	if err := WriteLatest(req.Root, id); err != nil {
		return nil, err
	}

	res := &ReviewResult{ID: id, Dir: dir, Summary: sum}
	// The all-agents-failed gate runs after artifacts are on disk; the result is
	// returned regardless so the caller knows where to look.
	if _, outErr := Outcome(results); outErr != nil {
		return res, outErr
	}
	return res, nil
}

// modePayload is one payload mode's built content shared by every agent using it.
type modePayload struct {
	Text       string
	FileCount  int
	Truncation payload.Truncation
}

// buildPayloads builds each distinct payload mode the roster uses exactly once.
func buildPayloads(ctx context.Context, cfg *ReviewConfig, repo, base, head string) (map[string]modePayload, error) {
	out := map[string]modePayload{}
	for _, mode := range neededModes(cfg) {
		entries, err := payload.BuildEntries(ctx, payload.PayloadMode(mode), repo, base, head)
		if err != nil {
			return nil, fmt.Errorf("building %s payload: %w", mode, err)
		}
		kept, trunc := payload.ApplyByteBudget(entries, byteBudget)
		var b strings.Builder
		for _, e := range kept {
			b.WriteString(e.Body)
		}
		// FileCount reflects what the reviewer actually saw (post-truncation), not
		// the pre-budget total — the dropped files are recorded in trunc.
		out[mode] = modePayload{Text: b.String(), FileCount: len(kept), Truncation: trunc}
	}
	return out, nil
}

// neededModes returns the distinct payload modes across the whole roster.
func neededModes(cfg *ReviewConfig) []string {
	seen := map[string]bool{}
	var modes []string
	for _, name := range rosterNames(cfg.Project) {
		if ac, ok := cfg.Registry.Agents[name]; ok {
			m := ac.EffectivePayloadMode(cfg.Settings)
			if !seen[m] {
				seen[m] = true
				modes = append(modes, m)
			}
		}
	}
	return modes
}

// buildSlots assembles the roster into ordered slots (parallel lane first, then
// serial) and returns the per-agent payload-mode map for the manifest. A
// build-time failure (unknown agent/provider, persona resolution, prompt render)
// aborts the whole run fail-fast: these are configuration errors the user must
// fix, not transient per-agent outcomes, so there is nothing useful to preserve
// — unlike the all-agents-failed runtime path, which keeps artifacts on disk.
func buildSlots(cfg *ReviewConfig, payloads map[string]modePayload, rng ReviewRange) ([]Slot, map[string]string, error) {
	perAgentMode := map[string]string{}
	var slots []Slot

	add := func(name string, serial bool) error {
		primary, mode, err := buildAgent(cfg, name, payloads, rng)
		if err != nil {
			return err
		}
		perAgentMode[name] = mode

		var fbs []Agent
		seen := map[string]bool{name: true}
		for fb := cfg.Registry.Agents[name].Fallback; fb != ""; fb = cfg.Registry.Agents[fb].Fallback {
			if seen[fb] {
				break // registry validation guarantees acyclic; defensive stop
			}
			seen[fb] = true
			agent, err := buildFallbackAgent(cfg, primary, fb)
			if err != nil {
				return err
			}
			fbs = append(fbs, agent)
		}
		slots = append(slots, Slot{Primary: primary, Fallbacks: fbs, Serial: serial})
		return nil
	}

	for _, name := range cfg.Project.Agents {
		if err := add(name, false); err != nil {
			return nil, nil, err
		}
	}
	for _, name := range cfg.Project.SerialAgents {
		if err := add(name, true); err != nil {
			return nil, nil, err
		}
	}
	return slots, perAgentMode, nil
}

// buildAgent resolves an agent's persona, renders its prompt against the payload
// it sees, and assembles the invocation. It returns the agent and its mode.
func buildAgent(cfg *ReviewConfig, name string, payloads map[string]modePayload, rng ReviewRange) (Agent, string, error) {
	ac, ok := cfg.Registry.Agents[name]
	if !ok {
		return Agent{}, "", fmt.Errorf("agent %q not found in registry", name)
	}
	mode := ac.EffectivePayloadMode(cfg.Settings)
	mp := payloads[mode]

	persona, err := registry.ResolvePersona(name, ac.Persona, nil, cfg.PersonaDirs)
	if err != nil {
		return Agent{}, "", err
	}
	prompt, err := payload.RenderPrompt(persona.Text, payload.PayloadContext{
		AgentName:   name,
		BaseRef:     rng.Base,
		HeadRef:     rng.Head,
		PayloadMode: mode,
		FileCount:   mp.FileCount,
		Payload:     mp.Text,
		ScopeRule:   payload.ScopeRule(payload.PayloadMode(mode)),
	})
	if err != nil {
		return Agent{}, "", fmt.Errorf("agent %q: %w", name, err)
	}
	prov, ok := cfg.Registry.Providers[ac.Provider]
	if !ok {
		return Agent{}, "", fmt.Errorf("agent %q references unknown provider %q", name, ac.Provider)
	}
	return Agent{
		Name:        name,
		Prompt:      prompt,
		PayloadMode: mode,
		Truncation:  mp.Truncation,
		TimeoutSecs: ac.EffectiveTimeoutSecs(cfg.Settings),
		Invocation: llmclient.Invocation{
			BaseURL:     prov.BaseURL,
			APIKeyEnv:   prov.APIKeyEnv,
			Model:       ac.Model,
			Temperature: ac.Temperature,
			Prompt:      prompt,
		},
	}, mode, nil
}

// buildFallbackAgent builds a fallback that reviews the SAME persona prompt and
// payload as the primary (AC 01-04: "fallback agent tried (same persona)"), only
// the provider/model/temperature/timeout differ.
func buildFallbackAgent(cfg *ReviewConfig, primary Agent, name string) (Agent, error) {
	ac, ok := cfg.Registry.Agents[name]
	if !ok {
		return Agent{}, fmt.Errorf("fallback agent %q not found in registry", name)
	}
	prov, ok := cfg.Registry.Providers[ac.Provider]
	if !ok {
		return Agent{}, fmt.Errorf("fallback agent %q references unknown provider %q", name, ac.Provider)
	}
	return Agent{
		Name:        name,
		Prompt:      primary.Prompt,
		PayloadMode: primary.PayloadMode,
		Truncation:  primary.Truncation,
		TimeoutSecs: ac.EffectiveTimeoutSecs(cfg.Settings),
		Invocation: llmclient.Invocation{
			BaseURL:     prov.BaseURL,
			APIKeyEnv:   prov.APIKeyEnv,
			Model:       ac.Model,
			Temperature: ac.Temperature,
			Prompt:      primary.Prompt,
		},
	}, nil
}

// writePayloadArtifacts persists each distinct payload under payload/<mode>.txt
// so the manifest's provenance is backed by what reviewers actually saw.
func writePayloadArtifacts(dir string, payloads map[string]modePayload) error {
	for mode, mp := range payloads {
		path := filepath.Join(dir, "payload", mode+".txt")
		if err := atomicWriteFile(path, []byte(mp.Text)); err != nil {
			return fmt.Errorf("writing payload %s: %w", mode, err)
		}
	}
	return nil
}

// rosterNames returns the full roster (parallel lane then serial lane).
func rosterNames(p *registry.ProjectConfig) []string {
	names := make([]string, 0, len(p.Agents)+len(p.SerialAgents))
	names = append(names, p.Agents...)
	names = append(names, p.SerialAgents...)
	return names
}
