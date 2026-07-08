# Acceptance Criteria: README.md Quickstart Leads with Synthetic, Summarizes the 5-Tier Hierarchy

**Related User Story:** [07: Onboarding-Hierarchy Documentation Rewrite](../user-stories/07-onboarding-hierarchy-documentation.md)
**Design References:** [onboarding-hierarchy.md](../documentation/onboarding-hierarchy.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation edit (`README.md`, `## Quickstart` section) | Content-only change; no code |
| Test Framework | Manual review + `grep`-based acceptance checks (no markdown lint configured in this repo) | Deterministic string/order checks substitute for automated tests |
| Key Dependencies | None — pure prose edit; sources exact tier language from `documentation/onboarding-hierarchy.md` | Must not paraphrase the source tier wording |

### Related Files (from codebase-discovery.json)
- `README.md` — modify: rewrite the `## Quickstart` section so `atcr quickstart` (Synthetic) remains the one-command default step, and add a hierarchy summary covering DashScope, Chutes→Featherless, LiteLLM, and frontier/majors in that order with designated caveat language.
- `.planning/plans/active/19.6_community_registry_hub/documentation/onboarding-hierarchy.md` — reference: source of truth for tier order and exact caveat phrasing.


## Happy Path Scenarios
**Scenario 1: Quickstart section leads with `atcr quickstart` / Synthetic as the one-command default**
- **Given** the current README.md `## Quickstart` section already documents `atcr quickstart` at README.md:59 as the interactive step that scaffolds `.atcr/` and sets up the Synthetic provider
- **When** the section is rewritten per this AC
- **Then** `atcr quickstart` (Synthetic) remains the first onboarding action a reader encounters in `## Quickstart`, framed as "run this first" / the one-command default, before any other provider path is mentioned

**Scenario 2: All 5 hierarchy tiers appear in the correct order**
- **Given** the rewritten `## Quickstart` section
- **When** the section is scanned top to bottom
- **Then** the tiers appear in this exact order: (1) Synthetic via `atcr quickstart`, (2) DashScope as a secondary flat-rate option, (3) Chutes then Featherless as explore-only, (4) LiteLLM as an Advanced aggregation proxy, (5) frontier/majors as opt-in bring-your-own-key

**Scenario 3: Caveat language matches the source document verbatim**
- **Given** `documentation/onboarding-hierarchy.md`'s specified phrases ("explore, not default" for Chutes/Featherless, "Advanced" for LiteLLM, "bring your own key" for frontier/majors)
- **When** the README hierarchy summary is written
- **Then** each tier's caveat uses the exact phrase from the source document rather than an invented paraphrase

## Edge Cases
**Edge Case 1: Existing Quickstart bash block (steps 1-6) is preserved**
- **Given** the existing numbered `atcr init` → `atcr doctor` → `atcr review && atcr reconcile` → `atcr report` bash walkthrough in `## Quickstart`
- **When** the section is rewritten to add the hierarchy summary
- **Then** the existing bash walkthrough is preserved (edited in place per the story's constraint, not deleted or replaced with a new anchor), and the hierarchy summary is added as new prose/list content alongside it

**Edge Case 2: DashScope has no `quickstart` wiring**
- **Given** the story's explicit constraint that DashScope is docs-only this epic
- **When** the DashScope tier is summarized in README.md
- **Then** the text does not imply `atcr quickstart` supports DashScope directly (no flag or wizard step referenced) and instead points the reader to `docs/personas-install.md` for the manual registry snippet

**Edge Case 3: Section length stays a summary, not a full duplicate of `docs/personas-install.md`**
- **Given** `docs/personas-install.md` carries the full tier detail (registry snippets, caveats, discover-by-model flow) per AC 07-02
- **When** README.md's `## Quickstart` hierarchy summary is written
- **Then** it is a concise summary (tier name + one-line caveat + link) rather than a full restatement of `docs/personas-install.md`'s content, avoiding duplicated maintenance burden

## Error Conditions
**Error Scenario 1: Frontier/majors persona names leak into the Quickstart narrative**
- **Given** the story's constraint that frontier/majors personas must never appear inside the default `quickstart` funnel narrative
- **When** `README.md`'s `## Quickstart` section is reviewed after the rewrite
- **Then** `grep -iE "claude|gpt|gemini" README.md` restricted to the `## Quickstart` section's line range returns zero matches outside a clearly separated "opt-in" callout for frontier/majors — a match inside the funnel narrative itself is a failing condition, not a runtime error
- HTTP status / error code: N/A (documentation-only; failure mode is a content-review rejection, not a program error)

**Error Scenario 2: Tier order is scrambled or a tier is dropped**
- **Given** the required 5-tier order from `documentation/onboarding-hierarchy.md` — the EXACT locked order is: Synthetic (via `atcr quickstart`) > DashScope > Chutes then Featherless > LiteLLM (Advanced) > frontier/majors (opt-in bring-your-own-key)
- **When** the rewritten section is checked
- **Then** any missing tier, out-of-order tier, or deviation from that exact locked sequence fails this AC's Story-Specific Definition of Done item; this is a review-time failure with no runtime error code

**Error Scenario 3: Royal-we leaks into the rewritten Quickstart (singular-voice violation)**
- **Given** the project's hard singular-voice rule (no "we"/"our"/"us" in user-facing docs — the maintainer is a solo individual)
- **When** the rewritten `## Quickstart` section is checked
- **Then** `grep -inE "\b(we|our|us)\b"` restricted to the rewritten `## Quickstart` line range returns ZERO matches (case-insensitive; excludes unrelated substrings like "user"/"reuse" which the word-boundary `\b` already guards against) — any first-person-plural match is a failing condition, not a runtime error
- HTTP status / error code: N/A (documentation content-review gate)

## Performance Requirements
- **Response Time:** N/A — static Markdown content, no runtime execution.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — documentation-only change with no auth surface.
- **Input Validation:** N/A — no user input processed; any bash snippets shown are illustrative and copy-pasted by the reader, not executed by tooling as part of this AC.

## Test Implementation Guidance
**Test Type:** MANUAL (documentation content review) + scripted `grep` check for the frontier-provider-name assertion (Error Scenario 1)
**Test Data Requirements:** The rewritten `README.md` file; the source `documentation/onboarding-hierarchy.md` for phrase/order comparison
**Mock/Stub Requirements:** None — no code under test; verification is textual diffing against the source hierarchy document

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr quickstart` (Synthetic) is presented first in `## Quickstart` as the one-command default
- [ ] All 5 tiers appear in the EXACT locked order — Synthetic > DashScope > Chutes/Featherless > LiteLLM (Advanced) > frontier/majors (opt-in) — with the exact caveat phrasing from `documentation/onboarding-hierarchy.md`
- [ ] DashScope tier states no `quickstart` wiring and links to `docs/personas-install.md`
- [ ] `grep -iE "claude|gpt|gemini"` over the `## Quickstart` section returns zero matches outside a separated opt-in callout
- [ ] `grep -inE "\b(we|our|us)\b"` over the rewritten `## Quickstart` section returns zero matches (singular-voice hard rule — no royal-we)

**Manual Review:**
- [ ] Code reviewed and approved
