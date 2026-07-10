# User Story 1: Catalog Routability Spike & Stable-Channel Heuristic

**Plan:** [19.7: Live Model Resolution, Lockfile & Drift Detection](../plan.md)

## User Story

**As an** atcr maintainer designing the hybrid model resolver
**I want** one authenticated completion call against a `~…-latest` alias to confirm (or refute) real routability, and a `@stable` preview/beta/exp-exclusion heuristic defined against OpenRouter's live `/api/v1/models` schema
**So that** the resolver's alias-bind design (covering 7 of 10 personas) commits to a proven mechanism instead of an assumption, and the remaining 3 personas' `created`-timestamp fallback keys on the correct `z-ai/` vendor prefix instead of a nonexistent `glm/` namespace

## Story Context

- **Background:** Desk research already enumerated OpenRouter's `/api/v1/models` catalog (344 models) and documented its schema (`id`, `canonical_slug`, `created`, `expiration_date`, no explicit stability flag) plus a list of 8 candidate `~`-prefixed `-latest` aliases across Anthropic, OpenAI, Google, and Moonshot. OpenRouter's own quickstart example shows `"model": "~openai/gpt-latest"` in a request body and describes it as resolving server-side to "the newest OpenAI flagship model" — strong supporting evidence, but the plan's own documentation (`documentation/openrouter-catalog-api.md`) explicitly flags this as not yet "independently confirmed by the epic's own authenticated spike." AC1 requires a real completion call, not a documentation citation. This is the epic's first task: the hybrid resolver's alias-bind path (Theme 3) and the family/channel binding schema (Theme 2) both depend on knowing whether `~` aliases actually complete, and on having a `@stable` heuristic defined before code is written against it.
- **Assumptions:** A valid `OPENROUTER_API_KEY` is available in the maintainer's environment to make the one authenticated call. The existing `internal/personas/client.go` `fetch()` helper (retry/backoff/injectable `HTTPClient`) is reusable for both the catalog listing call and the spike completion call, but this story's call itself is a one-off design action, not a new production code path or a permanent test — it is not required to run in CI. `personas/community/index.json`'s existing vendor-prefixed slugs (`delia → deepseek/deepseek-v4-pro`, `quinn → qwen/qwen3-coder-plus`, `glenna → z-ai/glm-5.2`) are the ground truth for vendor-prefix pinning.
- **Constraints:** The call must not touch the review hot path or alter any existing persona resolution behavior — this is pure investigation feeding a downstream design decision, per 19.6's C3 reproducibility guardrail (resolution/lookups happen only at explicit maintainer/upgrade time, never silently). CI must remain zero-live-network (Objective 11); this spike's live call is a one-time, manually-invoked, and recorded action, not a new automated test fixture. The finding must be recorded in the plan's documentation so Stories 2 and 3 (family/channel binding, hybrid resolver) can cite it as their design basis.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** One authenticated `POST` to `https://openrouter.ai/api/v1/chat/completions` using a `~`-prefixed `-latest` alias (e.g. `~openai/gpt-latest`) as the `model` field returns a successful completion (or a definitive, recorded failure), and the `@stable` heuristic (token-exclusion list + `expiration_date` handling) is written down against the live schema's actual field values, including the confirmed `z-ai/` vendor prefix for glenna.
- **Measurable:** Exactly one live completion call is made and its HTTP status/response outcome is recorded verbatim; the `@stable` heuristic names the specific preview/beta/exp substring patterns observed in the live catalog's `id`/`canonical_slug` values (not a hypothetical list); the vendor-prefix finding for delia/quinn/glenna is cross-checked against `personas/community/index.json`.
- **Achievable:** The call reuses the same HTTP client conventions already proven in `internal/personas/client.go`; no new infrastructure, dependency, or long-running process is required — this is a single request plus a written finding.
- **Relevant:** The finding is the explicit go/no-go input the hybrid resolver (Theme 3) and family/channel binding schema (Theme 2) need before either is designed in code; per the plan's Implementation Strategy, this task is sequenced first for exactly this reason.
- **Time-bound:** Completed before any resolver or binding-schema code (Stories 2+) is written, as the first task in the epic's execution order.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-latest-alias-routability-confirmed.md) | `~…-latest` Alias Routability Confirmed via Authenticated Completion Call | Manual |
| [01-02](../acceptance-criteria/01-02-stable-channel-heuristic-z-ai-prefix.md) | `@stable` Channel Heuristic Defined Against Live Schema, `z-ai/` Prefix Pinned | Manual |

## Original Criteria Overview

1. A real authenticated completion call against a `~…-latest` alias is made and its outcome (success or failure, with response detail) is recorded as the answer to "are `~`-prefixed aliases completion-routable."
2. The `@stable` channel heuristic is defined and recorded: which preview/beta/exp token patterns to exclude from `id`/`canonical_slug`, and how a non-null `expiration_date` is treated, grounded in the live catalog's actual field values rather than assumption.
3. The `z-ai/` vendor prefix (not `glm/`) is confirmed and recorded as the pinned resolver key for glenna, cross-referenced against `personas/community/index.json`'s existing GLM/DeepSeek/Qwen slugs.

## Technical Considerations

- **Implementation Notes:** Reuse `internal/personas/client.go`'s `fetch()` retry/backoff/`HTTPClient`-injection pattern (or a minimal throwaway script following the same conventions) to issue the single completion call and, if needed, re-list `/api/v1/models` to confirm live field values before writing the heuristic. This story is investigation-and-documentation output, not a new resolver code path — production resolver code lands in later stories (Theme 2/3) that cite this finding.
- **Integration Points:** OpenRouter `/api/v1/chat/completions` (completion call) and `/api/v1/models` (catalog confirmation), both authenticated with `Authorization: Bearer <OPENROUTER_API_KEY>`. Downstream integration point: the finding feeds the family/channel binding schema and hybrid resolver design (Stories 2 and 3), and the `z-ai/` prefix finding feeds the `created`-timestamp newest-in-vendor-prefix resolver for glenna specifically.
- **Data Requirements:** No schema or persisted-data change in this story. The only artifact is the recorded finding (routability outcome, `@stable` heuristic definition, `z-ai/` prefix confirmation), which downstream stories treat as their design input — the plan's `documentation/openrouter-catalog-api.md` already carries the desk-research half of this and should be updated with the live-call confirmation.

### References

- [OpenRouter Catalog & Completions API](../documentation/openrouter-catalog-api.md) — existing desk research on the model schema, alias forms, and the open question this spike closes.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| The `~`-prefixed alias is not actually completion-routable (a 4xx/error response), contradicting the quickstart's implication | Medium | The hybrid resolver design (Theme 3) already includes a `created`-timestamp / explicit-pin fallback independent of this outcome — a negative result changes which personas use which resolver path, but does not block the epic (per plan Risk Mitigation and `HAS_GATED_WORK: false`). |
| The `@stable` heuristic under- or over-excludes models because token patterns are guessed rather than observed | Medium | Derive the exclusion list directly from substrings actually present in the live `/api/v1/models` listing's `id`/`canonical_slug` values, not from a generic assumption. |
| The `z-ai/` vs `glm/` prefix confusion recurs if not pinned explicitly before resolver code is written | Low | Cross-check directly against `personas/community/index.json`'s existing `glenna → z-ai/glm-5.2` entry and record the confirmed prefix in this story's finding so Theme 3's resolver keys on it correctly from the start. |

---

**Created:** July 08, 2026 06:01:13PM
**Status:** Draft - Awaiting Acceptance Criteria
