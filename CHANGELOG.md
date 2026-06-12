## [1.4.0] - 2026-06-12

### Added

- `max_parallel` setting: bounds the fan-out engine's parallel lane with a buffered semaphore so a large roster cannot burst every provider call at once. Resolved through the usual `CLI --max-parallel > .atcr/config.yaml > registry.yaml > embedded default` precedence; `0` = unbounded escape hatch, negative is rejected as a usage error (exit 2). The serial lane is unaffected
- `--max-parallel N` flag on `atcr review`, a `max_parallel` key in the `atcr init` config template, and documentation in `docs/registry.md`

### Changed

- The embedded default for `max_parallel` is `10`, matching the prior effective fan-out for typical (≤10-agent) rosters. A roster larger than 10 agents that previously fired every call at once is now throttled to 10 concurrent provider calls by default — raise the cap or set `max_parallel: 0` to restore the old unbounded behavior

*Shipped via /execute-epic (epic 1.4)*

## [1.3.0] - 2026-06-11

### Added

- Project registry overlay: a repo can ship `.atcr/registry.yaml` defining its own providers and agents, merged over the user-level `~/.config/atcr/registry.yaml` so a clone is self-contained — no contributor has to mirror agent definitions by hand. Project entries shadow same-named user entries whole (no field-level merge); new names are added. Strictly parsed like every other config file
- `atcr trust` command to authorize project-defined providers: lists project providers and their status (`atcr trust`), pins one by the sha256 of its `(base_url, api_key_env)` pair (`atcr trust <name>` / `--all`) in `~/.config/atcr/trusted_providers.yaml`. Only the env var name is ever stored — never the key value
- Trust gate: a project-defined provider cannot receive a key until trusted, so a cloned repo cannot silently redirect a key to an arbitrary endpoint; `atcr review` and `atcr doctor` fail fast (exit 2) naming the provider and the `atcr trust` remedy. A project agent that references an existing user-defined provider needs no trust prompt
- Loud first-use banner naming each active project provider's `base_url` and key env on stderr
- `SOURCE` (user/project) provenance column in the `atcr doctor` table and a `source` field in its `--json` output, so overlay shadowing is visible rather than silent

### Changed

- Registry validation (roster references, fallback dangling/cycle checks, range checks) now runs over the merged user+project view; cross-tier fallback chains are supported, and every load error names the file that defined the offending entry (`registry.yaml` vs `.atcr/registry.yaml`)
- `docs/registry.md` documents the overlay, whole-entry merge semantics, the trust model, and a unified `CLI > project > user > embedded` precedence diagram now uniform across settings, personas, and definitions

*Shipped via /execute-epic (epic 1.3)*

## [1.2.0] - 2026-06-11

### Added

- `atcr doctor` command: self-tests every configured model endpoint with a trivial nonce prompt and a generous default token budget (2048, override with `--max-tokens`), so misconfigured providers, models, API keys, and base URLs are caught before a real review run burns time and tokens
- Per-agent doctor report (human table or `--json`) classifying each endpoint as `ok`, `ok_warning` (HTTP 200 but the marker is absent/empty — raise `--max-tokens`), `auth_failed`, `not_found`, `rate_limited`, `provider_error`, `network_error`, `timeout`, `missing_key`, or `invalid_config`; the key and base_url pre-flight checks run with no network call
- `atcr doctor` resolves the effective roster (agents + serial_agents, including fallback chains), deduplicates to distinct (provider, model, base_url) targets, and invokes each target at most once
- `--agents` flag to self-test a subset of the roster and `--timeout` for a per-call deadline independent of the review timeout
- Exit-code contract for `atcr doctor`: `0` when every agent has a working invocation path (primary or fallback), `1` when any agent has none, `2` for usage/configuration errors

### Changed

- `llmclient` now surfaces a structured `*HTTPStatusError` (HTTP status + bounded, secret-redacted body snippet) so callers can classify provider failures by status via `errors.As`, and exposes a per-call `max_tokens` request option (previously absent)

*Shipped via /execute-epic (epic 1.2)*

## [1.1.0] - 2026-06-11

### Added

- Reserved (parsed-and-validated, inert in 1.x) agent fields in the registry schema for the future agentic stages: `tools`, `max_turns`, `tool_budget_bytes`, `role`
- Reserved optional `verification` block (`{verdict, skeptic, notes}`) in `reconciled/findings.json` for the future adversarial-verification stage; absent in 1.x and tolerated on read
- `stages` array in `manifest.json`, recording `["review"]` in 1.x
- Reserved per-agent `turns`, `tool_calls`, `tool_bytes` counters in `status.json` (absent in 1.x)

### Changed

- Registry v1 parser now accepts the reserved agent fields (previously documented as rejected as unknown keys); they load and type-validate but remain inert, so a config can target a future stage without forcing a format-version break
- Documented every reserved field with its owning future epic in `docs/registry.md` and `docs/findings-format.md`

*Shipped via /execute-epic (epic 1.1)*

## [1.0.0] - 2026-06-11

### Added

- Go CLI binary with `review`, `reconcile`, `report`, `init`, `serve`, and `status` commands
- Fanout engine that dispatches review prompts to heterogeneous LLM reviewer personas
- Git range resolver supporting base/head refs, merge-commit SHA, and `.atcr/latest` pointer
- Reconciliation pipeline with dedup, cluster-merge, ambiguous detection, and confidence scoring
- MCP stdio server with tool schemas for review/reconcile/report/status integration
- Payload builders with diff extraction, persona templates, byte-budget truncation, and manifest generation
- Registry/config system with project overlay, persona resolution, precedence chain, and gate configuration
- Six embedded reviewer personas (bruce, greta, kai, mira, dax, otto) with shared base template
- Agent Skill definition for host-model review contribution and orchestration
- CI workflow with gofmt verification, golangci-lint, and race-enabled test suite
