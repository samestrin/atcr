# Acceptance Criteria: Category Word & Framing Alignment

**Related User Story:** [03: Verify and Refresh the Blog Post Outline](../user-stories/03-verify-and-refresh-the-blog-post-outline.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown content review/edit | Cross-references outline prose against the shipped persona prompt |
| Test Framework | Manual review + `grep`-based word-match verification | Story's Measurable criterion: category word in the outline must match `simon.md`'s `## Focus` section exactly |
| Key Dependencies | Story 1 (`personas/community/simon.yaml`/`simon.md`) and Story 2 (roster/index registration) must be complete first | This AC reads those shipped artifacts as source of truth; it does not derive the category word independently |

## Related Files
- `.planning/product/content/blog/slopfix-ai-code-bloat.md` - modify: reconcile any category-word or persona-behavior phrasing in sections 1, 3, and 4 against the shipped persona; leave sections 2 and 5 (cost narrative, CTA scope) and the already-accurate hook/pitch/example structure unchanged.
- `personas/community/simon.md` - reference: source of truth for the exact category word and `## Focus` framing (Story 1 deliverable; anticipated word is `bloat` per `plan.md`, but the shipped file governs).
- `personas/community/simon.yaml` - reference: confirms the persona's shipped metadata (`name`/`provider`/`model`). Note: the `Category` value does NOT live in the YAML — it lives in the `communityPersonas` roster row (`personas/community_test.go:117`, registered by Story 2) and must match the word used verbatim in `simon.md`'s prompt text per `TestCommunityPersonas_FixtureAndPromptCategory` (`personas/community_test.go:202`).
- `.planning/plans/active/29.0_anti_slop_persona/plan.md` - reference: documents the anticipated category word (`bloat`) and the list of already-claimed category words it must stay distinct from.

### Related Files (from codebase-discovery.json)
- `.planning/product/content/blog/slopfix-ai-code-bloat.md` - modify (`files_to_modify`, minor scope): refresh only what has drifted — align the category word/framing with the shipped persona (the CTA fix itself is AC 03-01's scope)
- `personas/community/simon.md` - reference (`files_to_create`, Story 1): source of truth for the shipped category word in the `## Focus` prose
- `personas/community/simon.yaml` - reference (`files_to_create`, Story 1): shipped persona metadata
- `personas/community_test.go:117` - reference (`files_to_modify`, Story 2): the roster row holding the formal `Category` value that `simon.md`'s prompt text must contain

## Happy Path Scenarios
**Scenario 1: Category word matches the shipped persona exactly**
- **Given** `simon.md`'s `## Focus` section uses a specific category word (e.g. `bloat`)
- **When** sections 1, 3, and 4 of the outline are reviewed for category-word language
- **Then** every instance of that framing word in the outline matches the shipped word verbatim (case-sensitive match on the word itself, not requiring identical sentence structure)

**Scenario 2: Already-accurate sections are left untouched**
- **Given** the outline's Slopfix hook (section 1's news reference and cost framing), the cost narrative (section 2), and the before/after code-example structure (section 4's example scaffold) already accurately reflect the shipped persona
- **When** the corrective pass is applied
- **Then** the diff is confined to the CTA fix (AC 03-01) and any confirmed word-level drift only — no wholesale rewrite of unrelated prose

**Scenario 3: Persona-behavior description matches shipped `## Focus` bullets**
- **Given** section 3's description of what `simon` does (`Doesn't complain about business logic; only flags verbosity, useless comments, and over-engineering`)
- **When** compared against `simon.md`'s shipped `## Focus` bullets
- **Then** the outline's description is consistent with (not contradictory to) the shipped persona's actual scope

## Edge Cases
**Edge Case 1: Shipped category word differs from the anticipated `bloat`**
- **Given** Story 1 ships `simon.md` with a category word other than the plan's anticipated `bloat` (e.g. renamed during authoring to avoid a Jaccard-similarity collision)
- **When** this story runs after Stories 1-2 are complete
- **Then** the outline is updated to the actual shipped word, not the word anticipated in `plan.md`

**Edge Case 2: Outline already uses consistent generic terms ("slop", "bloat") interchangeably**
- **Given** the outline's narrative language ("slop", "AI-generated code bloat") is marketing framing distinct from the persona's formal category-word field
- **When** reviewed against `simon.md`
- **Then** narrative/marketing language is not forced to literally match the category word if it is not presented as a direct persona-behavior claim — only claims that assert what the category/field value is must match exactly

## Error Conditions
**Error Scenario 1: Outline's category-word claim contradicts the shipped persona**
- Error message (validation failure): "outline section 3 states persona category as '<X>'; `simon.md` `## Focus` uses '<Y>' — drift not corrected"
- HTTP status / error code: N/A (content-review failure)

**Error Scenario 2: Corrective edit exceeds the story's diff-scope constraint**
- Error message (validation failure): "diff touches section 2 (cost narrative) or section 4's example scaffold beyond the CTA/word-drift fix — out of scope per Story 3's constraints"
- HTTP status / error code: N/A (content-review failure; requires reverting unrelated changes)

## Performance Requirements
- **Response Time:** N/A (static content edit; no runtime component)
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — no code or auth surface touched
- **Input Validation:** N/A — no user input; this is a static prose-consistency check between two Markdown files

## Test Implementation Guidance
**Test Type:** MANUAL (content review) + scripted grep/diff check
**Test Data Requirements:** The final shipped `personas/community/simon.md` and `simon.yaml` (from Story 1) as the comparison baseline; the outline file before and after edit for diff-scope verification
**Mock/Stub Requirements:** None. This AC is blocked on Stories 1-2 landing (`go test ./personas/...` green with `simon` registered) before it can be executed, per the story's stated dependency order

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (N/A directly — this story adds no Go tests; confirms `go test ./personas/...` from Stories 1-2 is already green before this story starts)
- [ ] No linting errors (markdown renders correctly; no broken code-fence syntax)
- [ ] Build succeeds (no build step applies to a Markdown-only change)

**Story-Specific:**
- [ ] The category word used in outline sections 1, 3, and 4 matches `simon.md`'s shipped `## Focus` section verbatim
- [ ] Section 3's description of `simon`'s behavior does not contradict the shipped `## Focus`/`## Scope` content
- [ ] A diff of the outline file shows changes confined to the CTA (AC 03-01) and any confirmed word-level drift — sections 1's hook, 2, and 4's example scaffold are otherwise unchanged
- [ ] Stories 1 and 2 are confirmed complete (persona files exist, roster/index registration green) before this AC is executed

**Manual Review:**
- [ ] Code reviewed and approved
