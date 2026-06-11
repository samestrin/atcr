# Configuration Reference

atcr reads configuration from two files plus embedded defaults, resolved by a strict precedence chain. Both files are strictly parsed: **unknown keys are load errors**, so configs stay typo-safe, and every validation failure surfaces in a second at load time — not after a 10-minute timeout.

| File | Scope | Holds |
|------|-------|-------|
| `~/.config/atcr/registry.yaml` | User | Providers, agents, and user-level defaults for the shared review settings. Personas live as `.md` files beside it. |
| `.atcr/config.yaml` | Project | The agent roster (which agents run, in which lane) and project defaults. Written by `atcr init`. |

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
fail_on: HIGH
```

| Field | Default | Notes |
|-------|---------|-------|
| `agents` | (required) | At least one. The parallel-lane roster. Every entry must exist in the registry. |
| `serial_agents` | `[]` | The serial-lane roster (sequential execution, for rate-limited providers). |
| `payload_mode` | `blocks` | One of `diff`, `blocks`, `files`. |
| `timeout_secs` | `600` | Global fan-out timeout. Must be positive and `≤ 86400`; an explicit `0` is rejected (not silently defaulted). |
| `fail_on` | `HIGH` (template only) | CI gate threshold (see [ci-integration.md](ci-integration.md)). The `HIGH` value is seeded into the config `atcr init` generates; the gate itself is opt-in — an unconfigured project does not gate. |

An agent may not appear twice, and may not appear in both `agents` and `serial_agents`.

## Precedence

The shared review settings (`payload_mode`, `timeout_secs`, `fail_on`) resolve **per field, independently**, in this order:

```
CLI flag  >  .atcr/config.yaml  >  registry.yaml  >  embedded default
```

A tier participates only where it explicitly sets a value; whitespace-only values count as unset, and a set-but-empty CLI flag is treated as unset rather than clobbering lower tiers. CLI values are validated at resolution time (they bypass the file-load checks), so an invalid `--payload` or out-of-range `--timeout` fails before any review work begins. Embedded defaults: `payload_mode=blocks`, `timeout_secs=600`. There is **no embedded default for `fail_on`**: the gate is opt-in, and `fail_on` resolution stops at the registry tier (`--fail-on` flag > project config > registry). The `fail_on: HIGH` line in a freshly generated config comes from the `atcr init` template, not from gate resolution.

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

## Reserved field names (rejected by the v1 parser)

The v1 schema is strict — unknown keys fail the load. The following names are reserved for later stages and will become valid keys when their stage lands; until then, including them is a load error.

| Field | Planned default | Activated by |
|-------|-----------------|--------------|
| `tools` | false | Stage 2 — tool-using reviewers |
| `max_turns` | 10 | Stage 2 |
| `tool_budget_bytes` | — | Stage 2 |
| `role` | reviewer | Stage 3/4 — `skeptic`, `judge` |
