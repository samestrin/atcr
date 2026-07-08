# Sprint 19.6: Community Registry Hub

**Status:** Ready for Execution
**Branch:** `feature/19.6_community_registry_hub`
**Complexity:** 11/12 (VERY COMPLEX) · **Timeline:** 15 days · **Phases:** 7
**Mode:** Strict TDD 🔒 · Adversarial 🎯 (inline: CRITICAL/HIGH) · Gated 🚧

---

## Overview

Make the in-repo community-persona channel (fetched from `samestrin/atcr`, not compiled into the binary) the **canonical** source of reviewer personas; add structured `provider`/`model` metadata so a user can discover a persona **by the model they already have**; ship a human-named, model-indexed persona library (frontier + flat-rate open models); and lead onboarding with the monetizing Synthetic path while keeping frontier personas opt-in.

## Timeline

| Phase | Focus | Days |
|-------|-------|------|
| 1 | Research & Spike — Resolution Chain Design | 1 |
| 2 | Foundation — Schema Extension + Registry Repoint | 1.5 |
| 3 | Core Resolution — Fetch-and-Pin + ResolvePersona Chain | 3.5 |
| 4 | Discovery — Model-Aware Search | 1.5 |
| 5 | Content Authoring — Persona Library + Human-Names Migration | 5 |
| 6 | Contract Enforcement + Onboarding Docs | 1.5 |
| 7 | Integration & Validation | 1 |

**Pattern:** Research & Spike → Foundation → Core Resolution → Discovery → Content Authoring → Contract & Docs → Integration & Validation

## Expected Outcomes

- Default fetch URL points at `samestrin/atcr`; fetch-and-pin, `--offline` stub, backward compatibility verified against a mock registry (AC1, AC6).
- `atcr personas search` finds a persona by its bound model from structured `index.json` data, not free-text (AC2).
- A single deterministic `ResolvePersona` precedence chain resolves self-contained persona units with untrusted-input guardrails — length cap + hard fixture gate (C1/C2/C3).
- A model-indexed, human-named persona library with passing fixtures; no role-based names remain anywhere in the active set (AC3, AC4).
- `go test ./...` passes with the authoring contract enforced by the fixture test (AC7).
- `README.md` and persona docs lead with the monetizing Synthetic path and position frontier personas as opt-in (AC5, AC8).

## Risk Summary (Top 3)

1. **Live-URL untestable until public** — the real `samestrin/atcr` fetch can't be exercised until the repo goes public; every AC1/AC2/AC6 test uses `httptest.NewServer` + `ATCR_PERSONAS_URL` (existing pattern). AC6 explicitly scopes E2E to a mock/local registry.
2. **Fetched custom prompts are untrusted input** — prompt-injection / oversized-prompt DoS / leftover `{{ }}` template injection. Mitigated by a length cap (mirroring `MaxExecutorSystemPromptLen`), a hard fixture gate before ship/resolve, and strict YAML decode on the persona-load path (Phase 3).
3. **Content authoring is large & judgment-heavy** — 8+ genuinely model-tuned personas (real vendor-guidance research) could stall a linear TDD sprint. Isolated into Phase 5, separate from schema/network code, with manual per-persona verification that the category word is authored into the prompt (not leaked from the injected diff).

## Sprint Assets

| Asset | Path |
|-------|------|
| Sprint plan (executable) | [sprint-plan.md](sprint-plan.md) |
| Metadata & tracking | [metadata.md](metadata.md) |
| Knowledge manifest | [sprint-knowledge.yaml](sprint-knowledge.yaml) |
| Sprint design | [plan/sprint-design.md](plan/sprint-design.md) |
| Original requirements (source of truth) | [plan/original-requirements.md](plan/original-requirements.md) |
| User stories (7) | [plan/user-stories/](plan/user-stories/) |
| Acceptance criteria (30) | [plan/acceptance-criteria/](plan/acceptance-criteria/) |
| Documentation references | [plan/documentation/](plan/documentation/) |

---

**Next:** `/refine-sprint @.planning/sprints/active/19.6_community_registry_hub/`
