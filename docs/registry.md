# Configuration Reference

atcr reads configuration from a user file, optional project files, and embedded defaults, resolved by a strict precedence chain. Every file is strictly parsed: **unknown keys are load errors**, so configs stay typo-safe, and every validation failure surfaces in a second at load time — not after a 10-minute timeout.

| File | Scope | Holds |
|------|-------|-------|
| `~/.config/atcr/registry.yaml` | User | Providers, agents, and user-level defaults for the shared review settings. Personas live as `.md` files beside it. |
| `.atcr/config.yaml` | Project | The agent roster (which agents run, in which lane) and project defaults. Written by `atcr init`. |
| `.atcr/registry.yaml` | Project (optional) | Project-defined providers and agents, merged over the user registry (project entries win by name). Lets a repo ship a self-contained review setup. See [Project registry overlay](#project-registry-overlay). |
| `~/.config/atcr/trusted_providers.yaml` | User | Allow list pinning which project-defined providers may receive a key. Managed by `atcr trust`. |

## Three concepts, deliberately decoupled

- **Provider** — an OpenAI-compatible endpoint + key environment variable. See [providers.md](providers.md).
- **Persona** — a named prompt: lens, personality, severity rubric. atcr ships six (bruce/generalist+correctness, greta/algorithmic, kai/architecture, mira/production, dax/tests+error-paths, otto/style+idiom); `atcr init` writes editable copies into `.atcr/personas/`.
- **Agent** — a provider+model binding that references a persona. Fallback agents reference the *same persona* — a fallback is by construction the same lens on a different model, never duplicated prompt text.

## `registry.yaml` (user level)

```yaml
providers:
  openrouter:
    api_key_env: OPENROUTER_API_KEY
    base_url: https://openrouter.ai/api/v1

agents:
  bruce:
    persona: bruce
    provider: openrouter
    model: anthropic/claude-3.7-sonnet
    temperature: 0.3
    timeout_secs: 600
    fallback: bruce-backup
  bruce-backup:
    persona: bruce
    provider: openrouter
    model: anthropic/claude-3.5-haiku

# Optional user-level defaults (the tier between project config and embedded defaults):
payload_mode: blocks
timeout_secs: 600
payload_byte_budget: 524288
max_parallel: 10
fail_on: HIGH
```

### Provider fields

| Field | Required | Notes |
|-------|----------|-------|
| `api_key_env` | yes | Name of the environment variable holding the key. Must be a valid POSIX env var name (`^[A-Za-z_][A-Za-z0-9_]*$`). The key is **never stored in config** — it is resolved from the environment at invoke time. |
| `base_url` | no | An `http`/`https` URL. Must not embed credentials (userinfo). |

### Agent fields

| Field | Default | Notes |
|-------|---------|-------|
| `provider` | (required) | Must name a provider defined above. |
| `model` | (required) | Model id at that provider. |
| `persona` | agent name | Selects the prompt; resolution chain below. |
| `temperature` | 0.7 | Must be within `[0, 2]`. |
| `timeout_secs` | inherited | Covers the whole invocation. When unset, the agent inherits the resolved shared timeout (precedence chain); set it to override per agent. Must be within `1..86400`. |
| `rate_limited` | false | `true` places the agent in the serial lane. |
| `fallback` | — | Another agent name, tried when this one fails. Chains are validated at load (dangling refs and cycles fail fast). |
| `payload` | inherited | Per-agent payload mode override (`diff`, `blocks`, `files`). When unset, inherits the resolved shared payload mode. See [payload-modes.md](payload-modes.md). |

## `.atcr/config.yaml` (project level)

```yaml
# Roster entries must match agent names in ~/.config/atcr/registry.yaml.
agents:
  - bruce
  - greta
  - kai
serial_agents: []
payload_mode: blocks
timeout_secs: 600
payload_byte_budget: 524288
max_parallel: 10
fail_on: HIGH
```

| Field | Default | Notes |
|-------|---------|-------|
| `agents` | (required†) | The parallel-lane roster. Every entry must exist in the registry. †May be empty when `serial_agents` is non-empty — only a roster empty in both lanes is rejected. |
| `serial_agents` | `[]` | The serial-lane roster (sequential execution, for rate-limited providers). |
| `payload_mode` | `blocks` | One of `diff`, `blocks`, `files`. |
| `timeout_secs` | `600` | Global fan-out timeout. Must be positive and `≤ 86400`; an explicit `0` is rejected (not silently defaulted). |
| `payload_byte_budget` | `524288` | Per-payload byte budget (512 KiB ≈ 128k tokens). Files are dropped largest-first when a payload exceeds it, recorded per agent in `status.json`. `0` = unlimited; negative is rejected. CLI override: `atcr review --byte-budget N`. |
| `max_parallel` | `10` | Cap on concurrent parallel-lane agent calls. Bounds the fan-out so a large roster cannot burst every provider call at once. When `serial_agents` is non-empty, the serial lane runs concurrently with the parallel lane in its own goroutine — peak provider concurrency is therefore `max_parallel + 1`, not `max_parallel`. `0` = unbounded; negative is rejected. CLI override: `atcr review --max-parallel N`. |
| `fail_on` | `HIGH` (template only) | CI gate threshold (see [ci-integration.md](ci-integration.md)). The `HIGH` value is seeded into the config `atcr init` generates; the gate itself is opt-in — an unconfigured project does not gate. |

An agent may not appear twice, and may not appear in both `agents` and `serial_agents`.

## Project registry overlay

A repository can ship its own providers and agents in **`.atcr/registry.yaml`**, so a clone is self-contained — no contributor has to mirror agent definitions into `~/.config/atcr/registry.yaml` by hand. The overlay reuses the exact `providers:` / `agents:` shapes documented above (including the reserved fields) and is strictly parsed like every other config file. It carries **definitions only** — shared settings such as `payload_mode` belong in `.atcr/config.yaml`, so a settings key here is an unknown-field load error.

```yaml
# .atcr/registry.yaml — project-defined providers and agents (optional).
providers:
  team-llm:
    base_url: https://llm.team.example/v1
    api_key_env: TEAM_LLM_KEY
agents:
  team-reviewer:
    provider: team-llm        # a project provider…
    model: team-model
    fallback: bruce           # …may fall back to a user-defined agent, and vice versa
```

**Merge semantics — whole-entry shadowing, project wins.** The effective registry is the user registry with project entries merged in by name: a project provider or agent with the same name as a user one **replaces it entirely** (no field-level deep merge — drop the project entry to restore the user definition), and a new name is simply added. Validation — roster references, fallback dangling/cycle checks, persona refs, and range checks — all run over the **merged** view, so a chain may span tiers; any error names the file that defined the offending entry (`registry.yaml` vs `.atcr/registry.yaml`).

### Trust model for project-defined providers

A project provider names a `base_url` and an `api_key_env`, so a cloned repo could otherwise direct one of your keys to an arbitrary endpoint. atcr therefore **refuses to send a key to a project-defined provider until you explicitly trust it**:

```bash
atcr trust                 # list project providers and their trust status
atcr trust team-llm        # authorize one (prints its base_url + key env first)
atcr trust --all           # authorize every project provider
```

`atcr trust` pins the provider's `(base_url, api_key_env)` pair by sha256 in `~/.config/atcr/trusted_providers.yaml`. Change either field and trust must be re-granted — this is what stops a repo from silently re-pointing a trusted key at a new host. **Only `api_key_env` (the variable name) is ever stored; the key value never enters any file.** A project **agent** that references an existing **user-defined** provider needs no trust prompt — only project-defined *providers* are gated. Until a project provider is trusted, `atcr review` and `atcr doctor` fail fast (exit 2) naming the provider and the `atcr trust` remedy. On a run that does use trusted project providers, a one-time banner names each provider's `base_url` and key env on stderr (confirmation, not the gate).

## Precedence

The shared review settings (`payload_mode`, `timeout_secs`, `payload_byte_budget`, `max_parallel`, `fail_on`) resolve **per field, independently**, in this order:

```
CLI flag  >  .atcr/config.yaml  >  registry.yaml  >  embedded default
```

A tier participates only where it explicitly sets a value; whitespace-only values count as unset, and a set-but-empty CLI flag is treated as unset rather than clobbering lower tiers. CLI values are validated at resolution time (they bypass the file-load checks), so an invalid `--payload`, out-of-range `--timeout`, or negative `--max-parallel` fails before any review work begins. Embedded defaults: `payload_mode=blocks`, `timeout_secs=600`, `payload_byte_budget=524288`, `max_parallel=10`. There is **no embedded default for `fail_on`**: the gate is opt-in, and `fail_on` resolution stops at the registry tier (`--fail-on` flag > project config > registry). The `fail_on: HIGH` line in a freshly generated config comes from the `atcr init` template, not from gate resolution.

The same **project-over-user** rule now applies uniformly across all three kinds of configuration — settings, personas, and definitions:

```
CLI flag  >  .atcr/*  (project)  >  ~/.config/atcr/*  (user)  >  embedded default
```

Settings resolve per field (above); personas resolve per file (`.atcr/personas/` shadows `~/.config/atcr/personas/`, chain below); and provider/agent **definitions** merge whole-entry (`.atcr/registry.yaml` shadows `~/.config/atcr/registry.yaml`, [overlay](#project-registry-overlay) above). There is no embedded tier for definitions — providers and agents must be defined in at least one registry file.

**Generated configs shadow registry defaults.** `atcr init` bakes explicit `payload_mode`, `timeout_secs`, `payload_byte_budget`, `max_parallel`, and `fail_on` values into `.atcr/config.yaml` so every knob is visible and editable. Because the project tier outranks the registry tier, those baked lines shadow any user-global defaults set in `registry.yaml` — an initialized project never inherits registry-tier values for them. To inherit a registry-tier value, delete the corresponding line from the project config (for `payload_mode` and `fail_on`, a whitespace-only value also counts as unset).

## Persona resolution chain

For each agent, the prompt is resolved by walking six levels in order, first hit wins:

1. **`--task-message` flag** — if provided it wins outright (even when empty: an explicit "no system prompt").
2. **`<persona>.md` in the project personas dir** (`.atcr/personas/`).
3. **`<persona>.md` in the registry personas dir** (`~/.config/atcr/personas/`).
4. **`_base.md`** in the project dir, then the registry dir.
5. **embedded `<agentName>.md`**, then
6. **embedded `_base.md`**.

When `persona` names a value other than the agent name (an explicit ref) and no file exists at level 2 or 3, resolution **fails** with "persona not found" — an explicit ref never silently falls through to a base or embedded default. Empty or whitespace-only persona files are treated as missing (with a stderr warning). Symlinked persona files are refused for safety (persona text is fed verbatim into prompts). Persona and agent names are sanitized against path traversal: no path separators, no `..`, no leading dot, and `_base` is reserved for the shared base template.

## Fallback-chain validation

Fallback references are validated when the registry loads:

- A `fallback` pointing at an agent that does not exist → **dangling fallback** error.
- A cycle (`a → b → a`, or any longer loop) → **fallback cycle** error, reported with the offending path.

Both fail the load immediately, so a misconfigured chain can never surface mid-run.

## Reserved fields (parsed and validated, inert in 1.x)

The v1 schema is strict — unknown keys fail the load — but the following agent fields are **reserved for later stages and already accepted**. A 1.x registry may set them; the loader parses and type-validates them, yet **no v1 code path acts on them** and **no load-time default is applied** — an omitted field stays at its zero/unset value until its stage lands. This lets a config target a future stage today without forcing a format-version break.

| Field | Type | Planned default | Validated at load | Activated by |
|-------|------|-----------------|-------------------|--------------|
| `tools` | bool | false | type only | Stage 2 — tool-using reviewers |
| `max_turns` | int | 10 | must be `> 0` | Stage 2 |
| `tool_budget_bytes` | int | — (unset) | must be `>= 0` | Stage 2 |
| `role` | string | reviewer | one of `reviewer`, `skeptic`, `judge` | Stage 3/4 |

The **planned default** column documents the value each field's owning stage will assume when it activates; 1.x applies none of these. An out-of-range value (e.g. `max_turns: 0`, an unknown `role`) is a load error even though the field is otherwise inert, so a config targeting a future stage is still caught early.

## Verifying the configuration (`atcr doctor`)

`atcr doctor` is the recommended check to run right after `atcr init` and after any registry edit. It resolves the **effective roster** — every agent in `agents` and `serial_agents`, plus every agent reachable through `fallback` chains — deduplicates it to the distinct `(provider, model, base_url)` targets, and invokes each target **once** with a trivial nonce prompt. Success is verified by the marker appearing in the response content, not merely by HTTP 200.

```bash
atcr doctor                      # human-readable table
atcr doctor --json               # machine-readable, stable schema
atcr doctor --agents bruce,kai   # test only these listed agents (their fallback chains are still probed)
atcr doctor --max-tokens 4096    # raise the completion budget for thinking models
atcr doctor --timeout 30         # per-call timeout in seconds
```

Flags: `--max-tokens` (default `2048`), `--timeout` (default `60`s), `--json`, `--agents <a,b,...>` (restrict to a subset of the directly-listed agents — each selected agent's fallback chain is still probed so its health verdict stays accurate). The token budget defaults high on purpose: reasoning/thinking models spend completion tokens on internal reasoning, so a small budget can exhaust before the marker is emitted.

### Status classes

| Status | Meaning | Typical fix |
|--------|---------|-------------|
| `ok` | Marker returned in visible content | — |
| `ok_warning` | HTTP 200 but the marker was absent/empty (often a thinking model that spent the whole budget reasoning) | Raise `--max-tokens` |
| `auth_failed` | 401/403 | Check the API key in the provider's `api_key_env` |
| `not_found` | 404 | Check the `model` name and the provider `base_url` |
| `rate_limited` | 429 | Retry later, or test a smaller `--agents` subset |
| `provider_error` | 5xx or other non-classified HTTP error | Inspect the bounded error-body snippet in the row |
| `network_error` | Transport-level failure (DNS, connection, TLS) | Check connectivity and `base_url` host |
| `timeout` | Per-call deadline exceeded | Raise `--timeout` |
| `missing_key` | The provider's `api_key_env` variable is unset — reported **without any network call** | `export` the named variable |
| `invalid_config` | `base_url` empty or malformed — reported **without any network call** | Set a valid `http(s)` `base_url` |

A failure class always references the **environment variable name**, never its value: no secret is ever logged.

### Exit codes

- **0** — every directly-listed agent has at least one working invocation path (its primary target *or* any fallback in its chain). A `fallback` that works covers a failing primary.
- **1** — at least one listed agent has no working path.
- **2** — usage/configuration error (missing or invalid registry / project config, bad flag value, unknown `--agents` name).

### JSON schema (`--json`)

A stable top-level object with an `agents` array; one entry per effective-roster agent (fallback agents included):

```json
{
  "agents": [
    {
      "agent": "bruce",
      "serial": false,
      "provider": "openrouter",
      "model": "anthropic/claude-3.5-sonnet",
      "status": "ok",
      "latency_ms": 412,
      "hint": "",
      "detail": "",
      "source": "user"
    }
  ]
}
```

`hint` (actionable next step) and `detail` (a bounded, secret-redacted provider error snippet) are present only when relevant and omitted when empty. `source` is the agent's definition tier — `user` or `project` — surfaced so overlay shadowing is visible rather than silent; the human-readable table shows it as a `SOURCE` column. The doctor invokes each distinct target at most once, so several agents that share a `(provider, model, base_url)` report the same latency and status.
