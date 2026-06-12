# Epic Plan 1.2: Model Endpoint Self-Test (`atcr doctor`)

**Estimated Durations**: 1 week

## Objective

Give users a single command that self-tests every configured model endpoint with a simple prompt — using a token budget generous enough that thinking/reasoning models produce visible output — so misconfigured providers, models, keys, and base URLs are caught before a real review run burns time and tokens.

## Context

A review run fans out to 6+ agents across multiple providers. Today the first signal that an endpoint is misconfigured (wrong base_url, missing/invalid API key env, bad model name, provider quota) arrives mid-review as a per-agent failure or an all-agents-failed exit — after payload construction and possibly after other agents have spent tokens. Code review of Epic 1.0 also confirmed the llmclient currently discards provider error bodies, so the failure reason (invalid model vs quota vs auth) is invisible. A pre-flight self-test closes this gap cheaply.

## Problem Statement

There is no way to verify that the configured roster can actually be invoked. Users discover configuration errors only by running a full review. Thinking/reasoning models add a trap: with a small max_tokens they exhaust the budget on internal reasoning and return empty visible content, which is indistinguishable from a broken endpoint unless the self-test budgets for it.

## Proposed Solution

A new `atcr doctor` subcommand (name open — see Clarifications) that:

1. **Loads the registry + project config** through the existing loader (strict validation already applies) and resolves the effective roster: every agent in `agents` and `serial_agents`, plus every agent reachable via `fallback` chains.
2. **Deduplicates invocation targets** to distinct (provider, model, base_url) tuples so a provider/model shared by several agents is called once, with results mapped back to every agent that uses it.
3. **Pre-flight checks without network:** API key env var set and non-empty; base_url well-formed. Report these as distinct failure classes before any HTTP call.
4. **Invokes each target once** through the existing internal/llmclient with:
   - A trivial echo prompt containing a per-run nonce (e.g. "Reply with exactly: ATCR-OK-<nonce>"), so success is verified by content, not just HTTP 200.
   - `max_tokens` high enough for thinking models to finish reasoning and still emit the marker (default 2048, configurable via flag), since reasoning tokens count against the completion budget on OpenAI-compatible endpoints.
   - A short per-call timeout (default 60s, flag-overridable) independent of the review global timeout.
5. **Classifies each result:** ok (marker found), ok-with-warning (HTTP 200 but marker absent / empty content — likely token budget or model behavior), auth_failed (401/403), not_found (404 model/base_url), rate_limited (429), provider_error (5xx), network_error, timeout, missing_key, invalid_config.
6. **Reports** a per-agent table to stdout (agent, provider, model, status, latency, hint) and supports `--json` for machine consumption. Exit 0 when every roster agent has at least one working invocation path (primary or fallback), exit 1 when any agent has no working path, exit 2 for usage/config errors — consistent with the documented exit contract.
7. **Never logs secrets:** errors reference the env var name, never the value, matching the existing redaction discipline.

## Success Criteria

- [ ] `atcr doctor` tests all distinct (provider, model, base_url) targets from the effective roster, including fallback agents, with each target invoked at most once.
- [ ] Missing API key env is reported per agent as `missing_key` without any network call.
- [ ] A thinking model configured correctly passes: the default token budget is large enough that the marker appears in visible content (verified against at least one reasoning-style mock in tests).
- [ ] Empty visible content on HTTP 200 is reported as a warning with a hint to raise `--max-tokens`, not as a hard failure.
- [ ] Failure classes are distinguished (auth vs not-found vs rate-limit vs network vs timeout) and include a bounded snippet of the provider error body when available.
- [ ] `--json` output is stable and documented; human table goes to stdout, logs to stderr.
- [ ] Exit codes: 0 all agents have a working path, 1 otherwise, 2 usage/config — covered by tests via httptest fake providers.
- [ ] README and docs/registry.md document the command as the recommended post-`atcr init` verification step.

## Task Breakdown

1. Roster/target resolution + dedup (registry walk incl. fallback chains) + unit tests.
2. llmclient: bounded error-body capture surfaced to callers (shared fix with TD "error bodies discarded", internal/llmclient/client.go:186) + per-call max_tokens/timeout options if not already exposed.
3. `atcr doctor` command: pre-flight checks, concurrent invocation (reuse fan-out lanes or a simple bounded pool), classification, table + `--json` renderers, exit-code mapping + tests.
4. Docs: README quick-start step, docs/registry.md troubleshooting section, flag help.

## Out of Scope

- Validating persona files or payload construction (covered by `atcr init`/config load and review itself).
- Cost estimation or token accounting beyond the single self-test call.
- Continuous/scheduled health checks or MCP exposure (`atcr_doctor` tool can ride a later epic if needed).
- Auto-fixing configuration.

## Dependencies

- Epic 1.0 core (registry, llmclient, CLI scaffolding) — complete, in code review.
- Synergy: TD finding "provider error bodies discarded" (internal/llmclient/client.go:186) — task 2 resolves it for both doctor and review paths.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Thinking models still return empty content at default budget | Medium | Low | Warning class (not failure) + `--max-tokens` flag + hint text |
| Self-test spends real tokens on paid endpoints | High | Low | One call per distinct target, tiny prompt, document expected cost; `--agents <name,...>` filter to test a subset |
| Provider quirks (non-OpenAI-compatible response shapes) misclassified | Low | Medium | Classification falls back to provider_error with raw bounded body snippet |

## Clarifications

- **Q: Command name?** Proposed `atcr doctor` (familiar from `brew doctor`/`flutter doctor`); alternatives: `atcr selftest`, `atcr check`. Decide at /init-plan time. (2026-06-11)
- **Q: New epic vs existing?** No existing epic covers configuration diagnostics (1.1 is schema-only; 2.0–5.0 are agentic stages). Numbered 1.2 to ride behind the 1.x core line as a small utility epic. (2026-06-11)
