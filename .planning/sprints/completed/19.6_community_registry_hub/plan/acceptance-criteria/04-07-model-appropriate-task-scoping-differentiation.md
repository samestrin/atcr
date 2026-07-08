# Acceptance Criteria: Model-Appropriate Task-Scoping Differentiation

**Related User Story:** [04: Model-Indexed Persona Library Authoring](../user-stories/04-model-indexed-persona-library-authoring.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Persona content (Markdown prompts) + content-review checklist | Cross-persona comparison, not a runtime code path |
| Test Framework | Go `testing` (structural distinctness assertions) + manual review checklist | Automated checks catch duplication; genuine grounding is a manual review gate |
| Key Dependencies | None new — reuses the community persona set from AC 04-01/04-02 | |

### Related Files (from codebase-discovery.json)
- `personas/community/*.md` — reference: all 10+ authored persona prompt templates, the subject of cross-persona comparison.
- `personas/community_test.go` — create: a structural-distinctness test asserting no two personas' combined `## Role`+`## Focus` sections exceed the locked similarity threshold, plus a vendor-guidance citation-presence check.
- `docs/personas-authoring.md` — reference: authoring contract and expected prompt structure.

### Similarity metric (LOCKED — deterministic pass/fail)
Differentiation is measured as **token-set Jaccard similarity** over the combined `## Role`+`## Focus` text of each persona pair, computed as: lowercase → split on non-alphanumeric → drop a small stopword set → deduplicate into a token set per persona → `J(A,B) = |A ∩ B| / |A ∪ B|`. **A pair with `J > 0.85` FAILS** (too near-identical). The check runs over all C(10,2)=45 pairs; any pair above threshold names both personas and fails the test. `0.85` is the locked threshold so pass/fail is deterministic and reproducible.

### Vendor-guidance citation (machine-checkable, consistent with AC 04-03)
Each persona `.md` MUST carry the same `<!-- vendor-guidance: <url-or-section> -->` citation defined in AC 04-03; its presence is asserted automatically here too. The citation is the machine-checkable proxy; whether the task-scope claim is genuinely *supported by* the cited guidance remains a MANUAL review gate.


## Happy Path Scenarios
**Scenario 1: Each persona's `## Role`+`## Focus` is scoped to a distinct review lens**
- **Given** the 10 authored community personas
- **When** each persona's combined `## Role`+`## Focus` text is tokenized and compared pairwise across all 45 pairs
- **Then** every pair has token-set Jaccard similarity `J ≤ 0.85` (the locked threshold), evidencing genuine per-model task scoping rather than a single generic list restated 10 times

**Scenario 2: A reasoning-heavy model's persona is scoped toward architecture/logic review**
- **Given** a frontier flagship persona bound to a reasoning-strong model
- **When** its `## Role`/`## Focus` are read
- **Then** the lens named (e.g. architecture, logic, design-level correctness) plausibly matches that model's documented reasoning strength, per the story's assumption

**Scenario 3: A lower-cost/faster model's persona is scoped toward a narrower or higher-volume lens**
- **Given** a fallback or flat-rate persona bound to a lower-cost/faster model
- **When** its `## Role`/`## Focus` are read
- **Then** the lens named is narrower or throughput-oriented (e.g. style/lint-level findings, a single target category) rather than the same broad architecture-review scope as the flagship personas

## Edge Cases
**Edge Case 1: Two personas legitimately share a target category but differ in framing**
- **Given** two personas that both review for, e.g., security issues
- **When** their `## Focus` sections are compared
- **Then** the structural-distinctness check tolerates a shared category word while still failing only when the pair's token-set Jaccard over combined `## Role`+`## Focus` exceeds `0.85` — a shared category token alone cannot push two otherwise-distinct sections above threshold

**Edge Case 2: Vendor-guidance citation missing for a given persona**
- **Given** a persona `.md` with no `<!-- vendor-guidance: <url-or-section> -->` marker
- **When** the automated citation-presence check runs (consistent with AC 04-03)
- **Then** that persona's subtest fails on the missing marker — the machine-checkable citation is a hard gate; whether the cited guidance actually supports the task-scope claim remains a separate MANUAL review step

## Error Conditions
**Error Scenario 1: Structural-distinctness check finds near-duplicate sections**
- **Given** two personas whose combined `## Role`+`## Focus` token-set Jaccard `J > 0.85`
- **When** the distinctness test runs
- **Then** the test fails, naming both offending personas and the computed `J` value so the content can be revised before merge

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
- [ ] No persona pair's combined `## Role`+`## Focus` token-set Jaccard exceeds `0.85` (automated distinctness check passes over all 45 pairs)
- [ ] Every persona `.md` carries a `<!-- vendor-guidance: ... -->` citation (automated presence check, consistent with AC 04-03)
- [ ] Reasoning-heavy-model personas are scoped toward architecture/logic-level review
- [ ] Fast/cheap-model personas are scoped toward narrower or higher-volume lenses
- [ ] Manual review confirms each persona's task-scope claim is traceable to the cited vendor/model guidance

**Manual Review:**
- [ ] Code reviewed and approved
