# Registry: Providers, Personas, Agents

> Status: draft — bones for Epic 1.0 tasks 2/5; authoritative schema lands with implementation.

User-level config at `~/.config/atcr/registry.yaml`; project-level roster and defaults at `.atcr/config.yaml`. Precedence: CLI flag > project config > registry > embedded default.

## Three concepts, deliberately decoupled

- **Provider** — an OpenAI-compatible endpoint + key env var. See [providers.md](providers.md).
- **Persona** — a named prompt: lens, personality, severity rubric. atcr ships six (bruce/generalist+security, greta/algorithmic, kai/architecture, mira/production, dax/tests, otto/style); `atcr init` writes editable copies.
- **Agent** — a provider+model binding that references a persona. Fallback agents reference the *same persona* — a fallback is by construction the same lens on a different model, never duplicated prompt text.

```yaml
agents:
  bruce:
    persona: bruce
    provider: local
    model: qwen-3.7-plus
    temperature: 0.3
    fallback: bruce-backup
  bruce-backup:
    persona: bruce
    provider: local
    model: qwen-3.6-plus
```

## Agent fields

| Field | Default | Notes |
|-------|---------|-------|
| `provider` | (required) | must name a provider |
| `model` | (required) | model id at that provider |
| `persona` | agent name | persona resolution: `persona:` ref > `<agent>.md` in registry dir > `_base.md` > embedded default; `--task-message` overrides all |
| `temperature` | 0.7 | |
| `timeout_secs` | 600 | covers the whole invocation |
| `rate_limited` | false | true → serial lane |
| `fallback` | — | another agent; chains validated at load (dangling refs and cycles fail fast) |
| `payload` | project default | per-agent payload mode override, see [payload-modes.md](payload-modes.md) |

## Reserved field names (NOT accepted by the v1 parser)

The v1 schema is strict: unknown keys are load errors, so configs stay typo-safe. The following names are reserved for later stages and will become valid keys when their stage lands — until then, including them fails the load.

| Field | Planned default | Activated by |
|-------|-----------------|--------------|
| `tools` | false | Stage 2 — tool-using reviewers |
| `max_turns` | 10 | Stage 2 |
| `tool_budget_bytes` | — | Stage 2 |
| `role` | reviewer | Stage 3/4 — `skeptic`, `judge` |

## Validation at load, not at invoke

Dangling fallback names, fallback cycles, unknown providers, unknown fields, and type errors all fail when the registry loads — in a second, not after a 10-minute timeout.
