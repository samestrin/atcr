// Package doctor implements `atcr doctor`: a pre-flight self-test that invokes
// every configured model endpoint once with a trivial nonce prompt and reports
// which agents have a working invocation path, so misconfigured providers,
// models, keys, and base URLs are caught before a real review run.
package doctor

import (
	"fmt"
	"strconv"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
)

// Target is a distinct (provider, model, base_url, max_tokens) invocation target.
// The doctor invokes each target at most once; several roster agents may share one.
type Target struct {
	Provider  string
	Model     string
	BaseURL   string
	APIKeyEnv string
	// MaxTokens is the max_tokens declared by the agents sharing this target, or 0
	// when they declared none. Per-agent like ContextWindowTokens, but carried here
	// because probes run per-target: the probe budget has to be a property of the
	// thing being probed.
	//
	// It is part of the target's IDENTITY rather than a value merged across sharers.
	// An earlier revision took the LARGEST declaration among them, reasoning that
	// extra headroom cannot make a smaller declarer's marker emission fail — true of
	// false POSITIVES, and it does stop the "raise --max-tokens" hint being shown to
	// an agent that already raised it. But `atcr review` resolves the cap PER AGENT
	// (resolveMaxTokens), so a smaller declarer probed at a co-tenant's 32000 was
	// never probed at the invocation it will actually make: the marker-absent
	// ok_warning was suppressed and doctor exited 0 on an agent that truncates to
	// zero findings on the real run.
	//
	// A probe is only evidence about the invocation it reproduces, so distinct caps
	// are distinct probes. Sharers that agree still dedupe, which is the common case.
	MaxTokens int
}

// AgentTarget binds one effective-roster agent to the index of the Target it
// invokes. Serial marks agents drawn from the serial_agents lane. Source is the
// agent's definition tier (user | project) for provenance display.
type AgentTarget struct {
	Agent     string
	Serial    bool
	TargetIdx int
	Source    string
	// ContextWindowTokens is the agent's RESOLVED context window and
	// WindowSource the tier it resolved from. Per-agent, not per-target: two
	// agents can share one probe target while declaring different windows.
	ContextWindowTokens int
	WindowSource        string
	// DeclaredMaxTokens is the agent's OWN max_tokens declaration, or 0 when it
	// made none. Per-agent for exactly the reason ContextWindowTokens is: two
	// agents can share one probe target while declaring different caps.
	//
	// It is deliberately the RAW declaration and never the --max-tokens override.
	// Target.MaxTokens carries the override — it has to, because it decides which
	// invocation is probed — but the override is doctor's, and `atcr review`
	// resolves its cap independently (its own flag, then this declaration, then the
	// built-in default). Recovering what review will use therefore requires the
	// declaration to survive the override somewhere, and this is that somewhere.
	DeclaredMaxTokens int
}

// Resolution is the deduplicated invocation plan for a roster: the distinct
// targets to probe, every effective-roster agent (the directly-listed agents
// plus every agent reachable through fallback chains) bound to its target, and
// each listed agent's ordered invocation path used for the exit-code verdict.
type Resolution struct {
	Targets []Target
	Agents  []AgentTarget
	// Paths maps each directly-listed agent (agents + serial_agents) to its
	// ordered invocation path: the agent itself followed by its fallback chain.
	// A listed agent is healthy when any agent on its path has a working target.
	Paths map[string][]string
}

// Resolve walks the effective roster (project Agents + SerialAgents, plus every
// fallback-reachable agent) and returns the deduplicated invocation plan. Each
// distinct (provider, model, base_url, max_tokens) tuple becomes a single Target;
// results map back to every agent that uses it. The fallback graph is validated acyclic
// at registry load; a defensive seen-set guards against malformed input.
// Resolve is ResolveWithCap with no --max-tokens override.
func Resolve(reg *registry.Registry, proj *registry.ProjectConfig) (*Resolution, error) {
	return ResolveWithCap(reg, proj, 0)
}

// ResolveWithCap is Resolve given the run's --max-tokens override (0 = unset).
//
// The override belongs HERE, at identity, and not only at probe time. Target identity
// includes the cap because a probe is only evidence about the invocation it reproduces
// — but the cap that will actually be sent is the RESOLVED one, and an explicit
// --max-tokens overrides every declaration. Keying on the declaration while probing at
// the override made agents whose calls are byte-identical (same base_url, model,
// resolved cap, nonce) into separate targets, so doctor paid N times for one piece of
// evidence. Against a quota-limited upstream the tail of those duplicates returns 429,
// which classifies as rate_limited rather than healthy and exits 1 — doctor reporting
// a broken roster it broke itself.
func ResolveWithCap(reg *registry.Registry, proj *registry.ProjectConfig, override int) (*Resolution, error) {
	res := &Resolution{Paths: map[string][]string{}}
	targetIdx := map[string]int{}
	agentSeen := map[string]bool{}

	addTarget := func(ac registry.AgentConfig) (int, error) {
		prov, ok := reg.Providers[ac.Provider]
		if !ok {
			return 0, fmt.Errorf("references unknown provider %q", ac.Provider)
		}
		// The cap this agent will actually be probed at: the override wins, exactly as
		// probe() resolves it. Keeping the two in step is what makes the key honest.
		declared := 0
		if ac.MaxTokens != nil {
			declared = *ac.MaxTokens
		}
		if override > 0 {
			declared = override
		}
		// NUL separates fields so no model/base_url value can forge a collision. The
		// declared cap joins the key because it changes the invocation being probed —
		// see Target.MaxTokens for why merging sharers onto one cap made the probe
		// evidence about a call no agent makes.
		key := ac.Provider + "\x00" + ac.Model + "\x00" + prov.BaseURL + "\x00" + strconv.Itoa(declared)
		if idx, ok := targetIdx[key]; ok {
			return idx, nil
		}
		idx := len(res.Targets)
		res.Targets = append(res.Targets, Target{
			Provider:  ac.Provider,
			Model:     ac.Model,
			BaseURL:   prov.BaseURL,
			APIKeyEnv: prov.APIKeyEnv,
			MaxTokens: declared,
		})
		targetIdx[key] = idx
		return idx, nil
	}

	addAgent := func(name string, serial bool) error {
		ac, ok := reg.Agents[name]
		if !ok {
			return fmt.Errorf("agent %q not found in registry", name)
		}
		idx, err := addTarget(ac)
		if err != nil {
			return fmt.Errorf("agent %q %w", name, err)
		}
		if !agentSeen[name] {
			agentSeen[name] = true
			window, windowSrc := payload.ResolveContextWindow(ac.Model, ac.ContextWindowTokens)
			declaredCap := 0
			if ac.MaxTokens != nil {
				declaredCap = *ac.MaxTokens
			}
			res.Agents = append(res.Agents, AgentTarget{
				Agent:               name,
				Serial:              serial,
				TargetIdx:           idx,
				Source:              reg.AgentTier(name),
				ContextWindowTokens: window,
				WindowSource:        windowSrc,
				DeclaredMaxTokens:   declaredCap,
			})
		}
		return nil
	}

	// walk follows a listed agent's primary + fallback chain, registering each
	// node as an effective-roster row and recording the ordered path.
	walk := func(start string, serial bool) error {
		seen := map[string]bool{}
		var path []string
		for node := start; node != "" && !seen[node]; node = reg.Agents[node].Fallback {
			seen[node] = true
			path = append(path, node)
			// Only the listed head carries the lane marker; fallback steps are
			// invoked identically (once per target) regardless of lane.
			if err := addAgent(node, serial && node == start); err != nil {
				return err
			}
		}
		res.Paths[start] = path
		return nil
	}

	for _, name := range proj.Agents {
		if err := walk(name, false); err != nil {
			return nil, err
		}
	}
	for _, name := range proj.SerialAgents {
		if err := walk(name, true); err != nil {
			return nil, err
		}
	}
	return res, nil
}
