# Acceptance Criteria: `docs/personas-install.md` Documents Tier Detail and the Discover-and-Install-by-Model Flow

**Related User Story:** [07: Onboarding-Hierarchy Documentation Rewrite](../user-stories/07-onboarding-hierarchy-documentation.md)
**Design References:** [onboarding-hierarchy.md](../documentation/onboarding-hierarchy.md), [cli-search-flags.md](../documentation/cli-search-flags.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation edit (`docs/personas-install.md`) | Content-only change; no code |
| Test Framework | Manual review + `grep`-based acceptance checks | No markdown lint configured in this repo |
| Key Dependencies | Depends on Theme 3 (AC2/AC6, `--model`/`--provider` search flags) for the discover-by-model bash example to be accurate; depends on Theme 4 (AC3) for real installed persona names in examples | Per story's stated Dependencies field |

### Related Files (from codebase-discovery.json)
- `docs/personas-install.md` — modify: add DashScope, Chutes→Featherless, LiteLLM, and frontier/majors subsections with required caveat framing, plus a discover-and-install-by-model walkthrough (`personas search` → `install` → `list` → `test`).

> **Documentation-baseline premise corrected (accuracy).** DashScope, Chutes, and Featherless are NOT currently documented anywhere in this repo — these three subsections are NET-NEW content to be AUTHORED, sourced from `documentation/onboarding-hierarchy.md` (tier order + exact caveat phrasing). Only LiteLLM pre-exists in the docs (README.md:189 + `docs/providers.md`), and frontier/majors provider usage is likewise already established. Therefore: the "must not weaken / must preserve existing docs" constraint applies to LiteLLM and frontier/majors (align the new subsection with, and do not contradict, what `docs/providers.md`/README already say); for DashScope/Chutes/Featherless there is no pre-existing doc to preserve — the requirement is faithful authoring from the source hierarchy, not non-regression.
- `.planning/plans/active/19.6_community_registry_hub/documentation/onboarding-hierarchy.md` — reference: source of truth for the bash sequence and tier caveat language.
- `internal/personas/search.go` (`Search`, `--model`/`--provider` flags) — reference: confirms the discover-by-model example cites existing CLI behavior.


## Happy Path Scenarios
**Scenario 1: DashScope documented as secondary flat-rate option with manual snippet**
- **Given** `docs/personas-install.md`'s existing structure (registry URL config, six subcommands, quick walkthrough)
- **When** a "DashScope" subsection is added
- **Then** it includes a manual `registry.yaml` snippet for wiring a DashScope provider and a link to DashScope's own docs, explicitly stating no `atcr quickstart` wiring exists for it this epic

**Scenario 2: Chutes then Featherless documented as explore-only with caveats**
- **Given** the required tier order (Chutes before Featherless)
- **When** the explore-only subsection is added
- **Then** Chutes is introduced first, Featherless second, and both carry caveats for slower inference, tighter context windows, and concurrency limits, using the "explore, not default" framing

**Scenario 3: LiteLLM documented as an Advanced aggregation-proxy note**
- **Given** the required "Advanced" framing for LiteLLM, which already pre-exists in the docs (README.md:189 + `docs/providers.md`)
- **When** the LiteLLM subsection is added
- **Then** it is placed under or labeled "Advanced" and describes LiteLLM as an OpenAI-compatible proxy for aggregating multiple providers behind one endpoint, without recommending it as a first-run path, and WITHOUT weakening or contradicting the existing LiteLLM documentation in `docs/providers.md`/README (this is the "must not weaken existing docs" case, distinct from the net-new DashScope/Chutes/Featherless subsections)

**Scenario 4: Frontier/majors documented as opt-in bring-your-own-key**
- **Given** the required opt-in framing for frontier/majors personas
- **When** the frontier/majors subsection is added
- **Then** it explains that Claude/GPT/Gemini-tuned personas are installed deliberately via `atcr personas search`/`install` by users who already hold an API key for that provider, and is not referenced from the default quickstart funnel

**Scenario 5: Discover-and-install-by-model flow documented end to end**
- **Given** `documentation/onboarding-hierarchy.md`'s specified bash sequence (`personas search deepseek` / `--provider`/`--model` filter → `personas install community/deepseek` → `personas list` → `personas test community/deepseek`)
- **When** the flow is added to `docs/personas-install.md`
- **Then** the four-step sequence (search → install → list → test) appears verbatim-equivalent to the source sequence, using real command names already documented in the file's "six subcommands" section (README.md:98,68,40,122)

## Edge Cases
**Edge Case 1: Discover-by-model example matches actual CLI flag behavior once Theme 3 lands**
- **Given** the story's Risk that the example might be written before `--model`/`--provider` flags exist
- **When** this AC is finalized (after Theme 3 merges per the story's Dependencies)
- **Then** the documented `atcr personas search --provider deepseek --model deepseek-chat` example is verified against a real CLI invocation before the doc is considered done, per the story's Mitigation note

**Edge Case 2: Installed persona name cited in examples must be real**
- **Given** the story's Dependency on Theme 4 (AC3) for real installed persona names
- **When** `community/deepseek` (or an equivalent real persona) is cited in the discover-by-model example
- **Then** the cited persona actually exists in the community index at doc-finalization time, not a placeholder name

**Edge Case 3: Existing six-subcommand reference content is preserved**
- **Given** `docs/personas-install.md`'s existing `install`/`list`/`search`/`remove`/`test`/`upgrade` subcommand reference sections (docs/personas-install.md:38-155)
- **When** the tier subsections and discover-by-model flow are added
- **Then** the existing subcommand reference sections and "Quick walkthrough" section remain intact and unmodified in meaning — new content is additive, not a replacement

## Error Conditions
**Error Scenario 1: Hierarchy caveat wording diverges from README.md's wording**
- **Given** the story's Risk that inconsistent caveat phrasing across README.md and `docs/personas-install.md` confuses cross-referencing readers
- **When** both files are compared after this AC and AC 07-01 are both applied
- **Then** the caveat phrases ("explore, not default", "Advanced", "bring your own key") match verbatim between the two files — a mismatch is a review-time failure, not a runtime error
- HTTP status / error code: N/A (documentation-only)

**Error Scenario 2: Frontier/majors subsection implies default/first-run usage**
- **Given** the constraint that frontier personas must remain opt-in only
- **When** the frontier/majors subsection is reviewed
- **Then** no sentence in that subsection frames Claude/GPT/Gemini personas as a default or recommended first step; any such framing fails this AC

**Error Scenario 3: Royal-we leaks into the authored tier subsections (singular-voice violation)**
- **Given** the project's hard singular-voice rule (no "we"/"our"/"us" in user-facing docs — the maintainer is a solo individual)
- **When** the newly authored/edited subsections and discover-by-model walkthrough in `docs/personas-install.md` are checked
- **Then** `grep -inE "\b(we|our|us)\b"` restricted to the added/edited line ranges returns ZERO matches — any first-person-plural match is a failing condition, not a runtime error
- HTTP status / error code: N/A (documentation content-review gate)

## Performance Requirements
- **Response Time:** N/A — static Markdown content, no runtime execution.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — documentation-only; the DashScope/registry snippet documents where a user places their own API key locally, consistent with existing `docs/personas-install.md` conventions (keys are never written into `atcr`'s own tracked config).
- **Input Validation:** N/A — no user input processed by this AC; bash examples are illustrative, copy-pasted by the reader.

## Test Implementation Guidance
**Test Type:** MANUAL (documentation content review) + scripted verification of the discover-by-model bash example against a live `atcr personas search --model/--provider` invocation once Theme 3 lands
**Test Data Requirements:** The rewritten `docs/personas-install.md`; `documentation/onboarding-hierarchy.md` for phrase/sequence comparison; a real community index entry to substitute for the `community/deepseek` placeholder
**Mock/Stub Requirements:** None for the doc content itself; the CLI-behavior verification step (Edge Case 1) exercises the real `atcr personas` binary against the live or a test community index, not a mock

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] DashScope, Chutes→Featherless, and Featherless subsections are AUTHORED net-new from `documentation/onboarding-hierarchy.md` (not pre-existing anywhere in the repo); LiteLLM and frontier/majors subsections align with — and do not weaken/contradict — the existing `docs/providers.md`/README content
- [ ] All four tier groupings (DashScope, Chutes→Featherless, LiteLLM, frontier/majors) are present with their required caveat framing
- [ ] The four-step discover-and-install-by-model bash sequence matches `documentation/onboarding-hierarchy.md` verbatim-equivalent
- [ ] Caveat phrasing matches README.md's hierarchy summary (AC 07-01) verbatim
- [ ] Existing subcommand reference and Quick walkthrough sections remain unmodified in meaning
- [ ] `grep -inE "\b(we|our|us)\b"` over the added/edited `docs/personas-install.md` line ranges returns zero matches (singular-voice hard rule — no royal-we)

**Manual Review:**
- [ ] Code reviewed and approved
