# Acceptance Criteria: Google Gemini Persona Content

**Related User Story:** [01: Author Model-Tuned Persona Content](../user-stories/01-author-model-tuned-persona-content.md)

> **Scope Note:** This AC covers the Google Gemini persona, which is additive scope beyond the original request's illustrative 1–2 persona examples (Claude, GPT-4). It is included because the refined plan expanded the default pack to a 3-provider frontier-model-diverse set (Anthropic, OpenAI, Google).

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | YAML persona file + Markdown prompt template | External repo `atcr/personas` |
| Test Framework | Persona fixture test harness (`atcr personas test`, per `docs/personas-authoring.md`) | Manual/external execution — no in-repo CI surface |
| Key Dependencies | `docs/personas-authoring.md` schema; `personas/_base.md` canonical structure; Google's official Gemini prompting guide (specific/unambiguous instructions, task decomposition, structured formatting) |

## Related Files
- `atcr/personas/gemini-reviewer.yaml` - create: persona-file metadata + agent binding (`provider: google`, flagship-primary `model`, e.g. `gemini-pro`, plus `persona: gemini-reviewer`)
- `atcr/personas/gemini-reviewer.md` - create: prompt template phrased per Google's official prompting guide conventions
- `docs/personas-authoring.md` - reference only: schema and canonical section structure this file must mirror (no change)
- `personas/_base.md` - reference only: canonical built-in template this structure is modeled on (no change)

### Related Files (from codebase-discovery.json)

- `docs/personas-authoring.md` — schema, canonical prompt section structure, required template variables, and contribution checklist this persona must satisfy
- `personas/_base.md` — canonical built-in prompt template structure the community-persona template mirrors
- `docs/personas-install.md` — documents the community registry URL and `atcr personas install/search/test` commands that will distribute and validate this persona
- `internal/personas/client.go` — fetches persona YAML from `<ATCR_PERSONAS_URL>/<name>.yaml` and the community `index.json`
- `internal/personas/install.go` — validates and writes the installed persona YAML to the local config directory
- `cmd/atcr/personas.go` — registers the `atcr personas install/search/list/upgrade/remove/test` CLI surface

## Happy Path Scenarios
**Scenario 1: Persona YAML declares a flagship-primary + lighter-tier same-family fallback**
- **Given** the contributor is authoring `atcr/personas/gemini-reviewer.yaml`
- **When** the file sets `provider: google` and `model: gemini-pro` as the primary binding, with a same-family lighter-tier fallback (e.g. a `gemini-reviewer-backup` persona or documented fallback field set to a lighter Gemini-family model such as `gemini-flash`), mirroring `registry.yaml`'s `bruce`/`bruce-backup` primary/backup convention
- **Then** both the primary and fallback are real, non-placeholder Gemini-family model ids, and neither is a stub value

**Scenario 2: Prompt phrasing follows Google's official prompting guide structure**
- **Given** the `gemini-reviewer.md` prompt template is being authored
- **When** the `## Role` and instructional sections use specific, unambiguous task framing with explicit output constraints and decomposed sub-tasks, consistent with Google's documented guidance for Gemini models
- **Then** the phrasing choices are attributable to Google's own guide (not a generic template rephrased) — e.g. explicit "be specific" instruction framing, task decomposition into discrete focus areas, and clearly bounded output constraints

## Edge Cases
**Edge Case 1: Fallback model omitted or same as primary**
- **Given** the YAML is reviewed before merge
- **When** the fallback binding is missing entirely, or the fallback `model` value is identical to the primary
- **Then** the persona fails the contribution checklist review — a same-family lighter-tier fallback distinct from the primary is required

**Edge Case 2: Cross-family fallback**
- **Given** the primary model is a Gemini model
- **When** the fallback is set to a non-Google model (e.g. Claude or GPT)
- **Then** the persona fails review — the fallback must be same-family (another Gemini-family model tier), not a cross-provider substitute

## Error Conditions
**Error Scenario 1: Unknown or malformed agent field**
- Error message: registry load error surfaced by the existing schema validator (per `docs/personas-authoring.md`'s "unknown *agent* field or an out-of-range value is rejected before the persona is ever written to disk")
- HTTP status / error code: N/A (CLI/load-time validation error, not an HTTP path)

**Error Scenario 2: Placeholder model value shipped**
- Error message: N/A — caught in manual contribution review, not a runtime error
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static content authoring, no runtime performance surface
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — no credentials or network calls are embedded in the persona per `docs/personas-authoring.md`'s security note
- **Input Validation:** Persona YAML must validate strictly against the registry agent schema for `provider`/`model` fields; the prompt template must not contain secrets, tokens, or instructions to make external network calls

## Test Implementation Guidance
**Test Type:** MANUAL (external-observation-based; this codebase's CI does not execute against the `atcr/personas` repo)
**Test Data Requirements:** N/A for this AC — schema/fixture validation covered in AC 01-04 and 01-05
**Mock/Stub Requirements:** N/A — verification is `atcr personas install gemini-reviewer` or `atcr personas test gemini-reviewer` run manually against the published `atcr/personas` repo once available, not a mocked in-repo test

## Definition of Done
**Auto-Verified:**
- [ ] N/A in this codebase — no automated test target exists here for external-repo content (documented per story's Constraints)
- [ ] No linting errors (N/A — external repo)
- [ ] Build succeeds (N/A — external repo)

**Story-Specific:**
- [ ] `gemini-reviewer.yaml` sets `provider: google` with a real flagship-primary `model` and a real same-family lighter-tier fallback, mirroring `bruce`/`bruce-backup`
- [ ] `gemini-reviewer.md` prompt phrasing reflects Google's own official prompting guide conventions (specific/unambiguous instructions, task decomposition)
- [ ] `gemini-reviewer.md` contains every canonical section and required template variable listed in AC 01-05, with no unrendered `{{ }}` actions
- [ ] No placeholder values (`TODO`, `changeme`, or example ids) remain in the shipped YAML

**Manual Review:**
- [ ] Code reviewed and approved (external repo maintainer review against the contribution checklist in `docs/personas-authoring.md`)
