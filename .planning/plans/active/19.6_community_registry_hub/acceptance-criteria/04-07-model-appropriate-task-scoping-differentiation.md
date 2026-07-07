# Acceptance Criteria: Model-Appropriate Task-Scoping Differentiation

**Related User Story:** [04: Model-Indexed Persona Library Authoring](../user-stories/04-model-indexed-persona-library-authoring.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Persona content (Markdown prompts) + content-review checklist | Cross-persona comparison, not a runtime code path |
| Test Framework | Go `testing` (structural distinctness assertions) + manual review checklist | Automated checks catch duplication; genuine grounding is a manual review gate |
| Key Dependencies | None new — reuses the community persona set from AC 04-01/04-02 | |

## Related Files
- `personas/community/*.md` - reference: all 10 authored persona prompt templates, the subject of cross-persona comparison
- `personas/community_test.go` - modify/create: a structural-distinctness test asserting no two personas' `## Focus` sections are near-identical text
- `.planning/plans/active/19.6_community_registry_hub/documentation/` - reference: any vendor-guidance citation notes captured during authoring, used by the manual review checklist

## Happy Path Scenarios
**Scenario 1: Each persona's `## Focus` section is scoped to a distinct review lens**
- **Given** the 10 authored community personas
- **When** each persona's `## Focus` section is extracted and compared pairwise
- **Then** no two personas share a near-identical `## Focus` list (e.g. via a normalized-text similarity check), evidencing genuine per-model task scoping rather than a single generic list restated 10 times

**Scenario 2: A reasoning-heavy model's persona is scoped toward architecture/logic review**
- **Given** a frontier flagship persona bound to a reasoning-strong model
- **When** its `## Role`/`## Focus` are read
- **Then** the lens named (e.g. architecture, logic, design-level correctness) plausibly matches that model's documented reasoning strength, per the story's assumption

**Scenario 3: A fast/cheap model's persona is scoped toward a narrower or higher-volume lens**
- **Given** a fallback or flat-rate persona bound to a lower-cost/faster model
- **When** its `## Role`/`## Focus` are read
- **Then** the lens named is narrower or throughput-oriented (e.g. style/lint-level findings, a single target category) rather than the same broad architecture-review scope as the flagship personas

## Edge Cases
**Edge Case 1: Two personas legitimately share a target category but differ in framing**
- **Given** two personas that both review for, e.g., security issues
- **When** their `## Focus` sections are compared
- **Then** the structural-distinctness check tolerates a shared category word while still failing on near-identical full-section text — the test compares normalized section text, not individual keyword overlap

**Edge Case 2: Vendor-guidance grounding is undocumented for a given persona**
- **Given** a persona whose authoring notes do not cite the specific vendor guidance consulted
- **When** the manual review checklist step runs
- **Then** the review flags the gap and requires the citation to be added before the AC is considered satisfied, per the story's risk-mitigation requirement to cite grounding sources during review

## Error Conditions
**Error Scenario 1: Structural-distinctness check finds near-duplicate `## Focus` sections**
- **Given** two personas whose `## Focus` sections normalize to near-identical text
- **When** the distinctness test runs
- **Then** the test fails, naming both offending personas so the content can be revised before merge

**Error Scenario 2: A persona's task scope cannot be traced to any documented model strength**
- **Given** a persona's `## Role`/`## Focus` making a claim about the bound model's capability
- **When** the manual review checklist cross-checks that claim against the model's official documentation/model card
- **Then** an unsupported claim is flagged and must be revised or grounded before the checklist item is marked done

## Performance Requirements
- **Response Time:** Pairwise text-similarity comparison across 10 personas' `## Focus` sections completes in well under 1 second in the test suite.
- **Throughput:** N/A (test-time/content-review only).

## Security Considerations
- **Authentication/Authorization:** N/A — this AC is a content-quality/differentiation check with no auth surface.
- **Input Validation:** N/A — no external input is processed; the check operates purely over committed static template text.

## Test Implementation Guidance
**Test Type:** UNIT (automated structural-distinctness check) + MANUAL (vendor-guidance grounding review checklist, since genuine task-appropriateness cannot be fully automated)
**Test Data Requirements:** The 10 committed persona Markdown templates
**Mock/Stub Requirements:** None — pure static-text comparison, no network or LLM call required

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] No two personas' `## Focus` sections are near-identical (automated distinctness check passes)
- [ ] Reasoning-heavy-model personas are scoped toward architecture/logic-level review
- [ ] Fast/cheap-model personas are scoped toward narrower or higher-volume lenses
- [ ] Manual review confirms each persona's task-scope claim is traceable to documented vendor/model guidance

**Manual Review:**
- [ ] Code reviewed and approved
