# User Story 2: Agent Configuration

**Plan:** [1.0: atcr Core - Review Engine, Reconciler, and Skill](../plan.md)

## User Story

**As a** developer setting up atcr for my project
**I want** to configure which agents review my code and how they connect to LLM providers
**So that** I can customize the review panel to match my team's needs and available API keys

## Story Context

- **Background:** atcr supports multiple LLM providers (OpenAI, Anthropic, local endpoints) and six embedded personas (bruce, greta, kai, mira, dax, otto). Developers need to configure which agents to use, set API keys, and optionally override default settings.
- **Assumptions:** Developer has access to at least one OpenAI-compatible LLM API. Developer wants to customize the default roster or use local endpoints.
- **Constraints:** Configuration must be strict (typos caught at load time). Fallback chains must be validated (no dangling refs, no cycles). API keys resolved from environment variables at invoke time, not load time.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Scaffold (task 1) |

## Success Criteria (SMART Format)

- **Specific:** Developer can run `atcr init` to generate config files, edit them to select agents and configure providers, then run a review with the custom roster
- **Measurable:** `atcr init` produces valid .atcr/config.yaml and six editable persona files; custom roster is used in subsequent `atcr review`
- **Achievable:** Uses yaml.v3 with KnownFields(true) strict mode, embedded defaults, precedence chain
- **Relevant:** Enables BYO-keys, multi-provider support — key differentiator from single-vendor tools
- **Time-bound:** Implemented in task 2 (config + init)

## Acceptance Criteria Overview

1. `atcr init` writes .atcr/config.yaml with default roster (all six personas), payload mode (blocks), timeouts, fail_on
2. `atcr init` writes six editable persona files to .atcr/personas/{bruce,greta,kai,mira,dax,otto}.md
3. Developer can edit .atcr/config.yaml to select subset of agents (e.g., only bruce, greta, kai)
4. Developer can configure providers in ~/.config/atcr/registry.yaml (name, api_key_env, base_url)
5. Developer can configure agents in registry.yaml (provider, model, temperature, timeout_secs, rate_limited, fallback, payload, persona)
6. Precedence chain works: CLI flag > project config > registry > embedded default
7. Fallback chain validated at load time: dangling reference = error, cycle = error
8. Strict parsing: unknown fields in YAML produce error (KnownFields(true))
9. Persona resolution: agent's persona ref > <agent>.md in registry dir > _base.md > embedded default
10. `--task-message` CLI flag overrides all persona resolution

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/1.0_atcr_core/`_

## Technical Considerations

- **Implementation Notes:** 
  - Config loader: internal/registry/config.go — two-tier loader with precedence
  - YAML parsing: yaml.v3 Decoder with KnownFields(true) for strict mode
  - Embedded defaults: use Go embed for personas/ and default config templates
  - Fallback validation: build dependency graph, detect cycles and dangling refs at load time
  - Persona resolution: text/template with payload vars ({{.Payload}}, {{.PayloadMode}}, {{.FileCount}}, {{.BaseRef}}, {{.HeadRef}}, {{.AgentName}})

- **Integration Points:** 
  - Filesystem: ~/.config/atcr/registry.yaml, .atcr/config.yaml, .atcr/personas/*.md
  - Environment variables: API keys resolved at invoke time (e.g., OPENAI_API_KEY, ANTHROPIC_API_KEY)
  - LLM client: uses provider config from registry

- **Data Requirements:** 
  - registry.yaml schema:
    ```yaml
    providers:
      <name>: { api_key_env: <ENV_VAR>, base_url: <openai-compatible endpoint> }
    agents:
      <name>:
        provider: <provider>        # required
        model: <model-id>           # required
        persona: <persona>          # optional, defaults to agent name
        temperature: 0.7            # optional
        timeout_secs: 600           # optional
        rate_limited: false         # optional → serial lane
        fallback: <agent>           # optional; validated at load
        payload: blocks             # optional per-agent override
    ```
  - .atcr/config.yaml schema:
    ```yaml
    agents: [bruce, greta, kai]     # roster
    serial_agents: []               # rate-limited agents
    payload_mode: blocks            # default
    timeout_secs: 600               # global
    fail_on: HIGH                   # CI gate threshold
    ```

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Typos in config YAML silently ignored | High | KnownFields(true) strict mode catches unknown fields |
| Fallback chain creates infinite loop | High | Cycle detection at load time (graph traversal) |
| Dangling fallback reference | Medium | Validation at load time: all agent refs must resolve |
| API key not set in environment | Medium | Clear error at invoke time: "API key env var OPENAI_API_KEY not set" |
| Persona file missing | Low | Resolution chain falls back to _base.md, then embedded default |
| Config precedence confusion | Low | Document precedence clearly in docs/registry.md |

---

**Created:** June 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
