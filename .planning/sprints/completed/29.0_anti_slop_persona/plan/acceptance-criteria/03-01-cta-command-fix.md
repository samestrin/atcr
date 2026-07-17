# Acceptance Criteria: CTA Command Fix

**Related User Story:** [03: Verify and Refresh the Blog Post Outline](../user-stories/03-verify-and-refresh-the-blog-post-outline.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown content review/edit | Single-file corrective edit, no code or schema changes |
| Test Framework | Manual review + `grep`-based verification | Matches the story's Measurable success criteria (grep for `--persona simon`, `atcr personas install simon`, `atcr personas test simon`) |
| Key Dependencies | None (no new packages); relies on the already-shipped `atcr personas` command surface | `cmd/atcr/personas.go` defines `install`/`test` subcommands used by the corrected CTA |

## Related Files
- `.planning/product/content/blog/slopfix-ai-code-bloat.md` - modify: replace the invalid section 5 CTA line (`Install ATCR today and run \`atcr review --persona simon\` on your next PR.`) with a verified invocation.
- `cmd/atcr/review.go` - reference: confirms `atcr review` has no `--persona` flag (see the `Flags()` registrations at lines 59-80), proving the current CTA is invalid.
- `cmd/atcr/personas.go` - reference: `newPersonasInstallCmd` (line 162) and `newPersonasTestCmd` (line 343) define the real `atcr personas install <name>` and `atcr personas test <name>` command surface the corrected CTA must cite.
- `docs/personas-install.md` - reference: canonical worked examples (`atcr personas install delia`, `atcr personas test delia`) the corrected CTA should mirror in form.

### Related Files (from codebase-discovery.json)
- `.planning/product/content/blog/slopfix-ai-code-bloat.md:38` - modify (`files_to_modify`, minor scope; `integration_points` doc-refresh): replace the invalid `atcr review --persona simon` CTA with the real flow — `atcr personas install simon` (+ reviewer config) and/or `atcr personas test simon` for the zero-setup fixture demo
- `docs/personas-install.md` - reference only (`related_files`, medium relevance): install/CLI reference whose worked examples the corrected CTA mirrors
- `docs/registry.md` - reference only (`related_files`, medium relevance): § Persona resolution chain — the authoritative reference for how an installed persona reaches a reviewer at `atcr review` time (the production flow the CTA points at)

## Happy Path Scenarios
**Scenario 1: Invalid CTA replaced with a verified production-install command**
- **Given** section 5 of `slopfix-ai-code-bloat.md` currently reads `atcr review --persona simon`
- **When** the CTA line is corrected
- **Then** it reads `atcr personas install simon` (or equivalent verified phrasing) with no `--persona` flag anywhere in the sentence

**Scenario 2: Zero-setup demo path cited alongside the install command**
- **Given** the corrected CTA
- **When** the outline also wants to offer a no-API-key way to see `simon` in action
- **Then** it cites `atcr personas test simon`, mirroring the documented `atcr personas test delia` pattern in `docs/personas-install.md`

**Scenario 3: Post-edit grep validation passes**
- **Given** the corrected outline file
- **When** a reviewer runs `grep -n -- '--persona simon' .planning/product/content/blog/slopfix-ai-code-bloat.md` and `grep -n 'atcr personas install simon\|atcr personas test simon' .planning/product/content/blog/slopfix-ai-code-bloat.md`
- **Then** the first grep returns zero matches and the second returns at least one match

## Edge Cases
**Edge Case 1: Generic `atcr review` mentions elsewhere in the outline**
- **Given** the outline may mention `atcr review` in a non-persona-specific context (e.g., describing CI integration generally)
- **When** the CTA fix is applied
- **Then** only the invalid `--persona` invocation is corrected; unrelated, valid mentions of `atcr review` are left untouched

**Edge Case 2: Dual command citation reads as complementary, not contradictory**
- **Given** both `atcr personas install simon` (production) and `atcr personas test simon` (zero-setup demo) are cited
- **When** a reader scans the CTA
- **Then** the phrasing makes clear these are two entry points (adopt vs. try-before-adopting), not conflicting instructions

## Error Conditions
**Error Scenario 1: Corrected CTA still contains the invalid flag**
- Error message (validation failure, not a runtime error): "grep for `--persona simon` returned N matches after edit; CTA fix incomplete"
- HTTP status / error code: N/A (content-review failure, not a running system)

**Error Scenario 2: A reader copies the pre-fix CTA into a shell**
- Error message (illustrates why the fix matters — the pre-fix state, not the desired post-fix state): `Error: unknown flag: --persona` (cobra flag-parsing error from `atcr review`)
- HTTP status / error code: N/A (CLI exit code 1)

## Performance Requirements
- **Response Time:** N/A (static content edit; no runtime component)
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — no code or auth surface touched
- **Input Validation:** The cited command (`atcr personas install simon`) must match the actual name-validation rules described in `docs/personas-install.md` (letters, digits, `_`, `-`, `/`); the CTA must not imply an unsafe or unsupported invocation pattern

## Test Implementation Guidance
**Test Type:** MANUAL (content review) + scripted grep check
**Test Data Requirements:** The edited `slopfix-ai-code-bloat.md` file itself; no fixtures or external data needed
**Mock/Stub Requirements:** None. Optional manual smoke-check: run `atcr personas install simon` and `atcr personas test simon` locally against the Story 1/2 shipped persona to confirm the cited commands actually succeed (depends on Stories 1-2 being complete)

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (N/A for this content-only story — no Go tests are added or modified; `go test ./personas/...` from Stories 1-2 remains green and untouched)
- [x] No linting errors (markdown renders correctly; no broken code-fence syntax)
- [x] Build succeeds (no build step applies to a Markdown-only change)

**Story-Specific:**
- [x] `grep -- '--persona simon'` (and `review --persona`) over `slopfix-ai-code-bloat.md` returns zero matches
- [x] `grep 'atcr personas install simon\|atcr personas test simon'` over `slopfix-ai-code-bloat.md` returns at least one match
- [x] The corrected command form matches the pattern used in `docs/personas-install.md`'s worked examples
- [x] No other CTA content (link to GitHub repo, Quickstart guide reference) was altered

**Manual Review:**
- [ ] Code reviewed and approved
