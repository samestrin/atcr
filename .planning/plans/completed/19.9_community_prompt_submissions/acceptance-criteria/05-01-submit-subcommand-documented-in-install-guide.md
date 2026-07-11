# Acceptance Criteria: `atcr personas submit <name>` Documented as the Seventh Subcommand

**Related User Story:** [05: Documentation of the Submit Flow and Two-Tier Model](../user-stories/05-documentation-of-submit-flow-and-two-tier-model.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (no code) | Edits `docs/personas-install.md` only |
| Test Framework | None (docs-only); manual read-through against Themes 1-2's specified command behavior | No `go test` is added or run by this AC |
| Key Dependencies | None — pure content edit; sourced from Theme 1/2 ACs (`01-01`, `01-02`, `01-03`, `02-01`, `02-02`) and the actual `cmd/atcr/personas.go`/`internal/personas/submit.go` once those themes land | |

### Related Files (from codebase-discovery.json)
- `docs/personas-install.md` (modify) — change the heading at line 40 from `## The six subcommands` to `## The seven subcommands`; insert a new `### atcr personas submit <name>` subsection between the existing `test` subsection (lines 124-137) and the `upgrade` subsection (starting line 139), following the same one-line-description → example invocation/output → error-cases structure used by every other subcommand
- `.planning/plans/active/19.9_community_prompt_submissions/acceptance-criteria/02-02-fork-branch-push-and-pr-create.md` (reference only) — source of the exact PR-URL output format (`https://github.com/<owner>/<repo>/pull/<n>`) and fork/push/PR-create error strings this AC's example/error-cases text must match
- `.planning/plans/active/19.9_community_prompt_submissions/acceptance-criteria/01-02-missing-fixture-blocks-submission.md` (reference only) — source of the exact "no fixture defined" and fixture-failure error wording (deliberately distinct from `personas test`'s wording) this AC's error-cases text must match verbatim
- `internal/personas/submit.go` (reference only, not yet created as of this story's authoring; verify final wording against it once Themes 1-2 land, per the story's Implementation Notes) — the actual implementation is the source of truth for exact strings at doc-finalization time

## Design References
- [Personas Install & Authoring Doc Updates (AC4)](../documentation/personas-docs-updates.md) — the exact `docs/personas-install.md` edits required (heading change, subsection placement, example/error-case content)

## Happy Path Scenarios
**Scenario 1: A reader locates the seventh subcommand's heading and description**
- **Given** a reader opens `docs/personas-install.md` at the "## The seven subcommands" heading
- **When** they scan the subsection order
- **Then** they find `install`, `list`, `search`, `remove`, `test`, `submit`, `upgrade` in that order, with `submit` positioned immediately after `test` and before `upgrade`, matching the story's stated insertion point and rationale (fixture-related commands kept adjacent)

**Scenario 2: A reader learns the exact command and its successful output**
- **Given** a reader has locally tuned a persona and its fixture passes
- **When** they read the `### atcr personas submit <name>` subsection
- **Then** they find a fenced example (matching the style of every other subcommand's example, e.g. lines 46-49 for `install`) showing `atcr personas submit <namespace/name>` and the resulting PR URL printed to stdout in the form `https://github.com/<owner>/<repo>/pull/<n>`, with prose noting `gh` must be installed and authenticated first

## Edge Cases
**Edge Case 1: Reader has no committed fixture for their persona**
- **Given** the reader's persona has never had a fixture written
- **When** they read the new subsection's error-cases text
- **Then** it states that submission is blocked with a message equivalent to `cannot submit "<name>": no fixture defined — add a fixture before submitting`, and cross-references `atcr personas test <name>` (the existing adjacent subsection) as the way to check fixture status first

**Edge Case 2: Reader's fixture fails partially or completely**
- **Given** the reader's fixture exists but does not fully pass
- **When** they read the error-cases text
- **Then** it states the exact pass/fail-count message shape (e.g. `cannot submit "<name>": fixture failed (<passed>/<total> cases passed)`) and that no fork/PR is attempted in this case

**Edge Case 3: Reader lacks `gh` or an authenticated `gh` session**
- **Given** the reader has not installed the GitHub CLI or has not run `gh auth login`
- **When** they read the new subsection
- **Then** it states the precondition explicitly (gh on PATH + authenticated) before any fork/PR step, and names the actionable remediation (install `gh` / run `gh auth login`), matching Theme 2's precondition-check error wording

## Error Conditions
**Error Scenario 1: Documentation omits or misstates the fixture-gate error wording**
- Error message: N/A — this is a documentation-accuracy defect, not a runtime error; caught by a manual read-through comparing the doc's error-cases prose against `.planning/plans/active/19.9_community_prompt_submissions/acceptance-criteria/01-02-missing-fixture-blocks-submission.md` and, once shipped, the real `cmd/atcr/personas.go`/`internal/personas/submit.go` output
- HTTP status / error code: N/A (documentation, not code)

**Error Scenario 2: New subsection breaks markdown heading hierarchy or an existing anchor link**
- Error message: N/A — caught by rendering the file and confirming heading levels (`###`) match sibling subcommand sections and that no existing in-repo link to a heading in this file (e.g. `personas-authoring.md`'s links into `personas-install.md`) breaks
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation, no runtime path.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** The new subsection must state that `submit` rides the invoking user's own `gh auth login` session — no bot token, no separate credential — consistent with Theme 2's security posture; must not instruct the reader to embed a token in any config file.
- **Input Validation:** The new subsection's example must use a well-formed persona name (letters/digits/`_`/`-`/`/` only) and must not suggest or imply that `submit` accepts arbitrary shell input; if error cases mention invalid names, they must match Theme 1's exact validation error wording (e.g. "only letters, digits, '_', '-', and '/' are allowed").

## Test Implementation Guidance
**Test Type:** MANUAL (documentation review)
**Test Data Requirements:** The rendered `docs/personas-install.md`, reviewed end-to-end for heading-hierarchy correctness, subsection ordering, and wording match against the Theme 1/2 acceptance criteria (and, once those themes ship, the actual command output).
**Mock/Stub Requirements:** None — no code, mock, or automated test harness; verification is a documentation-accuracy read-through, re-checked once Themes 1-2 land so example output matches real output exactly (per the story's Implementation Notes).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (no test suite changes; pre-existing suite remains green)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Heading at `docs/personas-install.md:40` reads "## The seven subcommands"
- [ ] New `### atcr personas submit <name>` subsection is inserted between `test` and `upgrade`, matching the description → example → error-cases format of the surrounding subcommand sections
- [ ] Subsection documents the `gh` precondition, the fixture-gate blocking cases (no fixture / partial fail / complete fail) with wording matching Theme 1's acceptance criteria, and the success case's PR-URL output format
- [ ] No other subcommand section's content changed beyond the heading text and the insertion point

**Manual Review:**
- [ ] Code reviewed and approved
