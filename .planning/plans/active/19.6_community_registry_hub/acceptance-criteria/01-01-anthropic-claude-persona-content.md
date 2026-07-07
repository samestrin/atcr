# Acceptance Criteria: Anthropic Claude Persona Content

**Related User Story:** [01: Author Model-Tuned Persona Content](../user-stories/01-author-model-tuned-persona-content.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | YAML persona file + Markdown prompt template | External repo `atcr/personas` |
| Test Framework | Persona fixture test harness (`atcr personas test`, per `docs/personas-authoring.md`) | Manual/external execution — no in-repo CI surface |
| Key Dependencies | `docs/personas-authoring.md` schema; `personas/_base.md` canonical structure; Anthropic's official Claude prompting guide (role prompting, XML-tag structuring, explicit imperative instructions) |

## Related Files
- `atcr/personas/claude-reviewer.yaml` - create: persona-file metadata + agent binding (`provider: anthropic`, flagship-primary `model`, e.g. `claude-opus-4-8`, plus `persona: claude-reviewer`)
- `atcr/personas/claude-reviewer.md` - create: prompt template phrased per Anthropic's official prompting guide conventions
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
**Scenario 1: Persona YAML declares a flagship-primary + same-family fallback**
- **Given** the contributor is authoring `atcr/personas/claude-reviewer.yaml`
- **When** the file sets `provider: anthropic` and `model: claude-opus-4-8` as the primary binding, with a same-family fallback entry (e.g. a `claude-reviewer-backup` persona or documented fallback field set to `claude-sonnet-4-6`), mirroring `registry.yaml`'s `bruce`/`bruce-backup` primary/backup convention
- **Then** both the primary and fallback are real, non-placeholder Claude model ids from the same family (Opus primary, Sonnet fallback), and neither value is a stub like `TODO` or `changeme`

**Scenario 2: Prompt phrasing follows Anthropic's official prompting guide structure**
- **Given** the `claude-reviewer.md` prompt template is being authored
- **When** the `## Role` section uses explicit role framing (e.g. "You are {{.AgentName}}, ...") and the body favors clear, direct imperative instructions and structured delimiters (XML-tag-style or clearly labeled sections) consistent with Anthropic's documented guidance for Claude
- **Then** the phrasing choices are attributable to Anthropic's own guide (not a generic template rephrased) — e.g. explicit "no flattery" framing, direct step ordering, and structured example blocks

## Edge Cases
**Edge Case 1: Fallback model omitted or same as primary**
- **Given** the YAML is reviewed before merge
- **When** the fallback binding is missing entirely, or the fallback `model` value is identical to the primary
- **Then** the persona fails the contribution checklist review — a same-family fallback distinct from the primary is required, mirroring the `bruce`/`bruce-backup` convention

**Edge Case 2: Cross-family fallback**
- **Given** the primary model is a Claude model
- **When** the fallback is set to a non-Anthropic model (e.g. GPT or Gemini)
- **Then** the persona fails review — the fallback must be same-family (another Claude model tier), not a cross-provider substitute

## Error Conditions
**Error Scenario 1: Unknown or malformed agent field**
- Error message: registry load error surfaced by the existing schema validator (per `docs/personas-authoring.md`'s "unknown *agent* field or an out-of-range value is rejected before the persona is ever written to disk")
- HTTP status / error code: N/A (CLI/load-time validation error, not an HTTP path)

**Error Scenario 2: Placeholder model value shipped**
- Error message: N/A — caught in manual contribution review, not a runtime error; flagged as "flagship-primary model, not a placeholder" per the story's Success Criteria
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
**Mock/Stub Requirements:** N/A — verification is `atcr personas install claude-reviewer` or `atcr personas test claude-reviewer` run manually against the published `atcr/personas` repo once available, not a mocked in-repo test

## Definition of Done
**Auto-Verified:**
- [ ] N/A in this codebase — no automated test target exists here for external-repo content (documented per story's Constraints)
- [ ] No linting errors (N/A — external repo)
- [ ] Build succeeds (N/A — external repo)

**Story-Specific:**
- [ ] `claude-reviewer.yaml` sets `provider: anthropic` with a real flagship-primary `model` and a real same-family fallback, mirroring `bruce`/`bruce-backup`
- [ ] `claude-reviewer.md` prompt phrasing reflects Anthropic's own official prompting guide conventions (explicit role framing, direct imperative instructions, structured sections)
- [ ] `claude-reviewer.md` contains every canonical section and required template variable listed in AC 01-05, with no unrendered `{{ }}` actions
- [ ] No placeholder values (`TODO`, `changeme`, or example ids) remain in the shipped YAML

**Manual Review:**
- [ ] Code reviewed and approved (external repo maintainer review against the contribution checklist in `docs/personas-authoring.md`)
