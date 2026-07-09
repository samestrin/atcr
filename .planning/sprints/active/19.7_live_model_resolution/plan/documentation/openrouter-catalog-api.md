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

---

## Epic 19.7 Spike Findings — Independently Confirmed (recorded 2026-07-08)

This section records the outcome of Epic 19.7's own authenticated spike (Phase 1, tasks 1.1 + 1.2),
superseding the earlier "not yet independently confirmed" caveat above. Stories 2 (family/channel
binding) and 3 (hybrid resolver) cite these findings directly as their design basis. The API key was
read inline from the environment for both calls and never printed, committed, or echoed.

### Task 1.1 — `~…-latest` alias routability: **CONFIRMED ROUTABLE** (AC 01-01)

One authenticated `POST https://openrouter.ai/api/v1/chat/completions` was made with
`{"model": "~openai/gpt-latest", "messages": [{"role":"user","content":"Reply with the single word: pong"}], "max_tokens": 16}`.

**Verbatim outcome:**
- HTTP status: **`200 OK`**
- The `~openai/gpt-latest` alias **resolved server-side to a concrete model**, echoed back in the
  response `model` field: **`openai/gpt-5.5-20260423`** (`"provider": "OpenAI"`).
- Response object: `"object": "chat.completion"`, `id` `gen-1783568516-…`, one `choices[0]` entry.
- `usage`: `prompt_tokens: 13`, `completion_tokens: 16`, `total_tokens: 29`, `cost: 0.000545`
  (`reasoning_tokens: 11`).
- `finish_reason: "length"` (`native_finish_reason: "max_output_tokens"`); `choices[0].message.content`
  was `null` **because the 16-token budget was fully consumed by the reasoning trace** — a
  token-budget artifact of the minimal spike request, **not** a routability signal. The request was
  accepted, routed to a concrete flagship model, and billed.

**Verdict:** `~`-prefixed `-latest` aliases are **completion-routable in a live call**, not just
catalog-browsing artifacts. This confirms the open question the earlier docs-cited evidence could only
suggest. Design consequence: Story 3's alias-bind path for the 7 alias-covered personas may pass the
`~…-latest` alias straight through as the resolved binding — the provider owns resolution server-side.

### Task 1.2 — `@stable` exclusion heuristic + vendor-prefix pins (AC 01-02)

Derived from one authenticated `GET https://openrouter.ai/api/v1/models` → **`200 OK`, 344 models**.

**`@stable` exclusion heuristic (definition for Story 3):** a model is excluded from `@stable` if
**EITHER** condition holds:

1. **Preview/pre-release token** present as a **hyphen-delimited segment** in `id` or `canonical_slug`
   (see the critical matching rule below). Tokens **actually observed** in the live catalog:
   - **`-preview`** — 16 models today (e.g. `google/gemini-3.1-pro-preview`, `qwen/qwen3.6-max-preview`,
     `openai/gpt-4-turbo-preview`, `google/gemini-2.5-flash-lite-preview-09-2025`).
   - **`-exp`** — 1 model today (`deepseek/deepseek-v3.2-exp`).
   - Forward-looking watch tokens with **zero** matches today but retained as exclusion rules so a
     future reader does not treat `expiration_date` as the only signal: `-beta`, `-alpha`, `-rc`,
     `-experimental`, `-nightly`, `-snapshot`, dated `-preview-MM-YYYY` forms.
2. **Deprecation:** `expiration_date` is **non-null**, regardless of the `id`/`canonical_slug` text.
   8 of 344 models carry a non-null `expiration_date` today:
   `tencent/hy3:free` (2026-07-21), `poolside/laguna-xs.2:free` + `poolside/laguna-xs.2` (2026-07-09),
   `z-ai/glm-5v-turbo` (2098-12-31), `openai/gpt-5.2-chat` (2026-08-10), `arcee-ai/trinity-mini`
   (2026-07-10), `google/gemini-2.5-flash-lite-preview-09-2025` (2026-07-09), `z-ai/glm-4.5` (2026-12-31).

**`@latest` vs `@stable` (cross-ref AC 03-05):** `@latest` bypasses **only** the preview-token
exclusion (condition 1); the deprecation exclusion (condition 2, non-null `expiration_date`) is
**always** applied and fails closed to the next-newest non-expiring member.

**Overlap edge case (feeds 3.11.A):** `google/gemini-2.5-flash-lite-preview-09-2025` satisfies **both**
conditions (preview-tagged AND `expiration_date` non-null). Under `@stable` it is excluded by either
rule; under `@latest` it is still excluded by the deprecation rule.

**CRITICAL matching rule — segment match, NOT bare substring (Edge Case 3, false-positive risk).**
A naive `strings.Contains` over-excludes stable models on substring collisions actually present in the
live catalog:
- `test` collides with **every `~…-latest` alias** ("la**test**" contains "test") — 11 hits, all aliases
  we must KEEP.
- `rc` collides with `sea**rc**h` / `resea**rc**h` / `a**rc**ee-ai` / `pe**rc**eptron` / `me**rc**ury` — 18 hits, none preview.
- `dev` collides with `mistralai/**dev**stral-2512` — a product name, not a channel.
- `:free` (25) and `:thinking` (11) are **variant-syntax suffixes**, orthogonal to stability channels —
  NOT `@stable` exclusions.
The resolver MUST match preview tokens as hyphen-delimited path segments (segment equals / suffix
`-token` / interior `-token-`), never as a bare substring.

**Vendor-prefix pins for the 3 alias-less personas (confirmed present in live catalog):**
- **`z-ai/`** — 12 models (**not `glm/`, which has 0 models**). `glenna → z-ai/glm-5.2` (index.json),
  and `z-ai/glm-5.2` is the newest `z-ai/` member by `created` (1781631930) that is non-expiring →
  the `created`-timestamp newest-in-prefix resolver for glenna MUST key on `z-ai/`.
- **`deepseek/`** — 11 models. `delia → deepseek/deepseek-v4-pro` (index.json) — present.
- **`qwen/`** — 49 models. `quinn → qwen/qwen3-coder-plus` (index.json) — present. **Note:** although
  `qwen/` is a `created`-timestamp-eligible prefix, quinn is **reclassified to explicit-pin** in the
  per-persona table below — its `coder` specialization is not the newest `qwen/` member, so
  newest-in-prefix would mis-resolve it. The created-timestamp resolver applies to delia + glenna only.
All three bindings above were cross-checked against `personas/community/index.json` and the live catalog.

### Per-persona resolution strategy — resolver-output validated against all 10 pins (Phase 3 input)

The three resolver strategies were validated against the **actual** live-catalog output for every one of
the 10 personas (roster + 19.6 pins from `personas/community/index.json`). Two personas the earlier "7
alias-covered / 3 created-timestamp" framing implied — **quinn** and **celeste** — turn out to be
**specialized variants** whose correct strategy is the **explicit-pin escape hatch** (AC 03-03), because
neither the general `-latest` alias nor the newest-in-vendor-prefix scan preserves their specialization.
This is an additive refinement (the aliases/prefixes still exist as described); it does not contradict
19.6, and the explicit-pin path is already a first-class Story-3 strategy. Final split: **6 alias-bind +
2 created-timestamp + 2 explicit-pin**.

| Persona | 19.6 pin (lock seed) | Strategy | Resolved target (live catalog, 2026-07-08) | Notes |
|---|---|---|---|---|
| anthony | `anthropic/claude-opus-4.8` | alias-bind | `~anthropic/claude-opus-latest` | opus family alias present |
| sonny | `anthropic/claude-sonnet-5` | alias-bind | `~anthropic/claude-sonnet-latest` | sonnet family alias present |
| gene | `openai/gpt-5.5` | alias-bind | `~openai/gpt-latest` | flagship alias |
| milo | `openai/gpt-5.4-mini` | alias-bind | `~openai/gpt-mini-latest` | mini-family alias (NOT `~openai/gpt-chat-latest`, which is unmapped) |
| gia | `google/gemini-2.5-pro` | alias-bind | `~google/gemini-pro-latest` | pro alias present |
| flint | `google/gemini-2.5-flash` | alias-bind | `~google/gemini-flash-latest` | flash alias present |
| delia | `deepseek/deepseek-v4-pro` | created-timestamp | `deepseek/deepseek-v4-pro` (created 1777000679) | newest non-expiring `deepseek/` member **equals** the pin ✓ |
| glenna | `z-ai/glm-5.2` | created-timestamp | `z-ai/glm-5.2` (created 1781631930) | newest non-expiring `z-ai/` member **equals** the pin ✓ (keyed on `z-ai/`, never `glm/`) |
| quinn | `qwen/qwen3-coder-plus` | **explicit-pin** | `qwen/qwen3-coder-plus` (verbatim) | newest non-expiring `qwen/` member is `qwen/qwen3.7-plus` (created 1780491783) — a **general** model; newest-in-prefix would silently drop the **coder** specialization, so quinn pins explicitly |
| celeste | `moonshotai/kimi-k2.7-code` | **explicit-pin** | `moonshotai/kimi-k2.7-code` (verbatim) | only moonshot alias is `~moonshotai/kimi-latest` (**general** flagship); alias-bind would drop the **code** specialization, so celeste pins explicitly |

**Design consequences for Phase 3 (Story 3):**
- Alias-bind covers exactly 6 personas (anthony, sonny, gene, milo, gia, flint); the resolver passes the
  mapped `~…-latest` alias through unchanged (Task 1.1 confirmed such aliases are completion-routable).
- Created-timestamp resolution selects the newest-by-`created` member **that passes the active channel
  filter** — NOT merely "non-expiring". Under `@stable` that means excluding preview-token members
  (condition 1) AND non-null `expiration_date` (condition 2) before taking the max `created`; under
  `@latest`, excluding only condition 2. This matters because `deepseek/` contains a non-expiring
  preview member, `deepseek/deepseek-v3.2-exp` (`created 1759150481`), which must be skipped under
  `@stable`. It happens to be older than the pin today, so delia is safe either way, but the resolver
  must apply the channel filter so a future newest `-exp`/`-preview` member never floats delia.
- With the channel filter applied, delia (`deepseek/`) resolves to `deepseek/deepseek-v4-pro`
  (`created 1777000679`, the max qualifying member) and glenna (`z-ai/`) to `z-ai/glm-5.2`
  (`created 1781631930`) — both **equal the current pin**; tests (AC 03-02) should assert those exact
  targets and include a newer-but-preview decoy to prove the channel filter is applied.
- quinn and celeste MUST use explicit-pin (AC 03-03) — a specialized variant must never be silently
  advanced to a general newest member. AC 03-03's "explicit pin never floats" test should cover both.

> Source: Epic 19.7 Phase 1 authenticated spike, 2026-07-08 (LLM_OPENROUTER_API_KEY, inline; value never recorded).
