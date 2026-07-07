# Acceptance Criteria: OpenAI GPT Persona Content

**Related User Story:** [01: Author Model-Tuned Persona Content](../user-stories/01-author-model-tuned-persona-content.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | YAML persona file + Markdown prompt template | External repo `atcr/personas` |
| Test Framework | Persona fixture test harness (`atcr personas test`, per `docs/personas-authoring.md`) | Manual/external execution — no in-repo CI surface |
| Key Dependencies | `docs/personas-authoring.md` schema; `personas/_base.md` canonical structure; OpenAI's official GPT prompting guide (system/developer-message framing, explicit step-by-step instructions, delimiters) |

## Related Files
- `atcr/personas/gpt-reviewer.yaml` - create: persona-file metadata + agent binding (`provider: openai`, flagship-primary `model`, e.g. `gpt-4-turbo`, plus `persona: gpt-reviewer`)
- `atcr/personas/gpt-reviewer.md` - create: prompt template phrased per OpenAI's official prompting guide conventions
- `docs/personas-authoring.md` - reference only: schema and canonical section structure this file must mirror (no change)
- `personas/_base.md` - reference only: canonical built-in template this structure is modeled on (no change)

## Happy Path Scenarios
**Scenario 1: Persona YAML declares a flagship-primary + lighter-tier same-family fallback**
- **Given** the contributor is authoring `atcr/personas/gpt-reviewer.yaml`
- **When** the file sets `provider: openai` and `model: gpt-4-turbo` as the primary binding, with a same-family lighter-tier fallback (e.g. a `gpt-reviewer-backup` persona or documented fallback field set to a lighter GPT-family model such as `gpt-4o-mini`), mirroring `registry.yaml`'s `bruce`/`bruce-backup` primary/backup convention
- **Then** both the primary and fallback are real, non-placeholder GPT-family model ids, and neither is a stub value

**Scenario 2: Prompt phrasing follows OpenAI's official prompting guide structure**
- **Given** the `gpt-reviewer.md` prompt template is being authored
- **When** the `## Role` and instructional sections use explicit, numbered step-by-step instructions and clear task framing consistent with OpenAI's documented guidance for GPT models (direct instructions, delimited sections, explicit output-format contract)
- **Then** the phrasing choices are attributable to OpenAI's own guide (not a generic template rephrased) — e.g. numbered instruction lists, explicit "do X, then Y" ordering, and unambiguous formatting rules

## Edge Cases
**Edge Case 1: Fallback model omitted or same as primary**
- **Given** the YAML is reviewed before merge
- **When** the fallback binding is missing entirely, or the fallback `model` value is identical to the primary
- **Then** the persona fails the contribution checklist review — a same-family lighter-tier fallback distinct from the primary is required

**Edge Case 2: Cross-family fallback**
- **Given** the primary model is a GPT model
- **When** the fallback is set to a non-OpenAI model (e.g. Claude or Gemini)
- **Then** the persona fails review — the fallback must be same-family (another GPT-family model tier), not a cross-provider substitute

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
**Mock/Stub Requirements:** N/A — verification is `atcr personas install gpt-reviewer` or `atcr personas test gpt-reviewer` run manually against the published `atcr/personas` repo once available, not a mocked in-repo test

## Definition of Done
**Auto-Verified:**
- [ ] N/A in this codebase — no automated test target exists here for external-repo content (documented per story's Constraints)
- [ ] No linting errors (N/A — external repo)
- [ ] Build succeeds (N/A — external repo)

**Story-Specific:**
- [ ] `gpt-reviewer.yaml` sets `provider: openai` with a real flagship-primary `model` and a real same-family lighter-tier fallback, mirroring `bruce`/`bruce-backup`
- [ ] `gpt-reviewer.md` prompt phrasing reflects OpenAI's own official prompting guide conventions (system/developer-message framing, numbered step-by-step instructions)
- [ ] No placeholder values (`TODO`, `changeme`, or example ids) remain in the shipped YAML

**Manual Review:**
- [ ] Code reviewed and approved (external repo maintainer review against the contribution checklist in `docs/personas-authoring.md`)
