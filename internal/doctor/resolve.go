// Package doctor implements `atcr doctor`: a pre-flight self-test that invokes
// every configured model endpoint once with a trivial nonce prompt and reports
// which agents have a working invocation path, so misconfigured providers,
// models, keys, and base URLs are caught before a real review run.
package doctor

import (
	"fmt"

	"github.com/samestrin/atcr/internal/registry"
)

// Target is a distinct (provider, model, base_url) invocation target. The
// doctor invokes each target at most once; several roster agents may share one.
type Target struct {
	Provider  string
	Model     string
	BaseURL   string
	APIKeyEnv string
}

// AgentTarget binds one effective-roster agent to the index of the Target it
// invokes. Serial marks agents drawn from the serial_agents lane.
type AgentTarget struct {
	Agent     string
	Serial    bool
	TargetIdx int
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
// distinct (provider, model, base_url) tuple becomes a single Target; results
// map back to every agent that uses it. The fallback graph is validated acyclic
// at registry load; a defensive seen-set guards against malformed input.
func Resolve(reg *registry.Registry, proj *registry.ProjectConfig) (*Resolution, error) {
	res := &Resolution{Paths: map[string][]string{}}
	targetIdx := map[string]int{}
	agentSeen := map[string]bool{}

	addTarget := func(ac registry.AgentConfig) (int, error) {
		prov, ok := reg.Providers[ac.Provider]
		if !ok {
			return 0, fmt.Errorf("references unknown provider %q", ac.Provider)
		}
		// NUL separates fields so no model/base_url value can forge a collision.
		key := ac.Provider + "\x00" + ac.Model + "\x00" + prov.BaseURL
		if idx, ok := targetIdx[key]; ok {
			return idx, nil
		}
		idx := len(res.Targets)
		res.Targets = append(res.Targets, Target{
			Provider:  ac.Provider,
			Model:     ac.Model,
			BaseURL:   prov.BaseURL,
			APIKeyEnv: prov.APIKeyEnv,
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
			res.Agents = append(res.Agents, AgentTarget{Agent: name, Serial: serial, TargetIdx: idx})
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
