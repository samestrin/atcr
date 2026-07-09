# OpenRouter Catalog & Completions API

**Priority:** [CRITICAL]

## Overview

OpenRouter exposes a model catalog at `/api/v1/models` that, as of the 2026-07-08 spike against 344 models, returns entries with `id`, `canonical_slug`, `created`, and `expiration_date` fields, alongside the fully documented schema of `id`, `canonical_slug`, `name`, `created`, `description`, `context_length`, `architecture`, `pricing`, `top_provider`, `per_request_limits`, `supported_parameters`, `default_parameters`, `expiration_date`, and `benchmarks`. There is no explicit stability/GA/preview flag anywhere in this schema, and the catalog spike confirms this gap in practice: only 8 of the 344 models carry a non-null `expiration_date`, so deprecation is signaled but "is this stable" is not.

Completions run through a single endpoint, `https://openrouter.ai/api/v1/chat/completions` (base URL `https://openrouter.ai/api/v1`), authenticated with a Bearer token in the `Authorization` header. The request body's `model` field accepts a plain string, and OpenRouter's own quickstart example populates it with an alias-style identifier — `"~openai/gpt-latest"` — rather than a concrete slug, which is the strongest available evidence that `~`-prefixed aliases are routable in a real completion call, not just catalog-browsing artifacts.

Aliases resolve automatically on OpenRouter's side: the docs describe both plain redirects (e.g. `anthropic/claude-3-5-sonnet` resolving to `anthropic/claude-3.5-sonnet`) and "latest" aliases such as `~openai/gpt-latest`, described as always resolving to "the newest OpenAI flagship model — so your code keeps using the freshest version without redeploying." The catalog spike found this pattern covers 7 of 10 atcr personas (Anthropic, OpenAI, Google, Moonshot), leaving DeepSeek, Qwen, and GLM (delia/quinn/glenna) without a vendor-provided `-latest` alias, so those three need either a `created`-timestamp "newest-in-vendor-prefix" resolver or an explicit pin. Slug suffixes like `:free` and `:thinking` are mentioned as supported variant syntax but are not documented as part of the JSON schema itself.

## Key Concepts

**Model catalog schema fields.** The documented Model object contains `id`, `canonical_slug`, `name`, `created`, `description`, `context_length`, `architecture`, `pricing`, `top_provider`, `per_request_limits`, `supported_parameters`, `default_parameters`, `expiration_date`, and `benchmarks`.
> Source: [https://openrouter.ai/docs/guides/overview/models]

The catalog spike independently observed `id`, `canonical_slug`, `created`, and `expiration_date` as the fields relevant to resolution, across all 344 models returned by `/api/v1/models`.
> Source: [original-requirements.md > Catalog spike]

**No explicit stable/GA/preview flag — a heuristic is required.** Neither the documented schema nor the live catalog exposes a stability field, so "the stable channel" needs a heuristic (exclude preview/beta/exp tokens + honor `expiration_date`) or light curation rather than a schema read.
> Source: [https://openrouter.ai/docs/guides/overview/models]
> Source: [original-requirements.md > Catalog spike]

**`expiration_date` as the deprecation signal.** `expiration_date` is a `string | null` representing the "Deprecation date for the model endpoint (null if not deprecated)" — the only structured signal for a model's lifecycle state. In the live catalog only 8 of 344 models carry a non-null value today.
> Source: [https://openrouter.ai/docs/guides/overview/models]
> Source: [original-requirements.md > Catalog spike]

**`-latest` aliases exist for 7 of 10 personas.** Anthropic, OpenAI, Google, and Moonshot each expose a `~`-prefixed `-latest` alias (e.g. `~anthropic/claude-opus-latest`, `~openai/gpt-latest`), meaning the provider owns resolution for those personas with near-zero ongoing maintenance. DeepSeek, Qwen, and GLM have no such alias and need a separate resolver or pin.
> Source: [original-requirements.md > Catalog spike]

**`~`-prefixed alias resolution, confirmed in principle by the quickstart's own example.** The quickstart's example request body uses `"model": "~openai/gpt-latest"` directly, and the docs state this "always resolves to the newest OpenAI flagship model" server-side. This is strong evidence the `~` alias form is routable in a live completion call — but it is evidence toward confirming the catalog spike's open question (AC1), not a substitute for the epic's own planned authenticated completion-call spike, since the quickstart excerpt does not itself demonstrate a live response.
> Source: [https://openrouter.ai/docs/quickstart#using-the-openrouter-api]
> Source: [original-requirements.md > Catalog spike]

**Aliases also resolve without the `~` marker.** The docs separately describe non-`~` redirects (`anthropic/claude-3-5-sonnet` → `anthropic/claude-3.5-sonnet`) as resolving automatically, distinct from the `~`-marked "latest" aliases.
> Source: [https://openrouter.ai/docs/guides/overview/models]

**Variant suffixes (`:free`, `:thinking`) are slug syntax, not schema fields.** The docs note these suffixes are supported by appending them to a slug, but no schema field documents how they are stored or represented in a catalog response.
> Source: [https://openrouter.ai/docs/guides/overview/models]

**All 10 of Epic 19.6's pinned slugs still resolve verbatim today** — this epic is additive, not a breaking migration.
> Source: [original-requirements.md > Catalog spike]

## Code Examples

Request body `model` field using an alias (from the quickstart example):

```
"model": "~openai/gpt-latest"
```
> Source: [https://openrouter.ai/docs/quickstart#using-the-openrouter-api]

Authentication header:

```
"Authorization": "Bearer <OPENROUTER_API_KEY>"
```
> Source: [https://openrouter.ai/docs/quickstart#using-the-openrouter-api]

Example alias forms observed in the catalog spike (not full request/response bodies, just the slug forms):

```
~anthropic/claude-opus-latest
~anthropic/claude-sonnet-latest
~openai/gpt-latest
~openai/gpt-mini-latest
~openai/gpt-chat-latest
~google/gemini-pro-latest
~google/gemini-flash-latest
~moonshotai/kimi-latest
```
> Source: [original-requirements.md > Catalog spike]

## Quick Reference

| Item | Value |
|---|---|
| Base URL | `https://openrouter.ai/api/v1` |
| Completions endpoint | `https://openrouter.ai/api/v1/chat/completions` |
| Catalog endpoint | `/api/v1/models` |
| Auth header | `Authorization: Bearer <OPENROUTER_API_KEY>` |
| Request `model` field | plain string; accepts concrete slug or `~`-prefixed alias (e.g. `~openai/gpt-latest`) |
| Documented schema fields | `id`, `canonical_slug`, `name`, `created`, `description`, `context_length`, `architecture`, `pricing`, `top_provider`, `per_request_limits`, `supported_parameters`, `default_parameters`, `expiration_date`, `benchmarks` |
| Stability/GA/preview flag | Not present in schema — requires heuristic or curation |
| Deprecation signal | `expiration_date` (`string \| null`; only 8/344 models set today) |
| Alias resolution | Server-side automatic; both plain redirects and `~`-prefixed "latest" aliases; `~` routability strongly suggested by quickstart example but not yet independently confirmed by the epic's own authenticated spike |
| `-latest` alias coverage | 7 of 10 personas (Anthropic, OpenAI, Google, Moonshot); DeepSeek/Qwen/GLM need a `created`-timestamp resolver or explicit pin |
| Variant suffixes | `:free`, `:thinking` — slug syntax only, not a schema field |

## Related Documentation

- Original epic requirements: ../original-requirements.md (Catalog spike section)
- https://openrouter.ai/docs/guides/overview/models
- https://openrouter.ai/docs/quickstart#using-the-openrouter-api
