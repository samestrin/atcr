# Acceptance Criteria: GitHub Code Scanning Upload Example

**Related User Story:** [04: SARIF CI Integration Documentation](../user-stories/04-sarif-ci-integration-docs.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation | GitHub Flavored Markdown, fenced `yaml` code block |
| Test Framework | YAML lint / manual review | `yamllint` or `yq eval` against the extracted fenced block; `markdown-link-check` for the relative link |
| Key Dependencies | `github/codeql-action/upload-sarif@v3` | External GitHub Action, referenced by tag only — not vendored or pinned to a SHA in this doc |

## Related Files
- `docs/ci-integration.md` - modify: add a new subsection (e.g. "SARIF Upload for GitHub Code Scanning") directly beneath the existing "Maintained PR Action" subsection, containing the GitHub Actions YAML snippet and the distinction sentence linking to `docs/github-action.md`
- `docs/github-action.md` - reference only (no edits): link target proving the new subsection correctly points readers to the separate `atcr github` PR check/inline-comment flow instead of duplicating it
- `.planning/plans/active/25.0_sarif_output_integration/documentation/github-code-scanning-integration.md` - reference: source of the verbatim, already-validated GitHub Actions snippet ("Code Examples" section) and the GitHub SARIF ingestion constraints (rules[]/ruleId linkage, size/count ceilings) that inform any caveat text added

## Happy Path Scenarios
**Scenario 1: Reader finds a copy-pasteable GitHub Actions snippet**
- **Given** a reader is viewing `docs/ci-integration.md`
- **When** they reach the new subsection following "Maintained PR Action"
- **Then** they see a fenced `yaml` code block containing `actions/checkout@v4` with `fetch-depth: 0`, a step running `atcr review && atcr reconcile && atcr report --format=sarif > results.sarif`, and a `github/codeql-action/upload-sarif@v3` step with `sarif_file: results.sarif`

**Scenario 2: Reader distinguishes the SARIF path from the `atcr github` flow**
- **Given** the reader reads the lead-in sentence near the top of the new subsection
- **When** they compare it against the existing "Maintained PR Action" subsection
- **Then** they see an explicit statement that the SARIF-upload path is separate from, and does not replace, `atcr github`'s already-shipped PR check/inline-comment flow, with a markdown link to `docs/github-action.md`

## Edge Cases
**Edge Case 1: Reader unfamiliar with GitHub Advanced Security**
- **Given** the reader has never used GitHub Code Scanning before
- **When** they follow the snippet as written, with no other context
- **Then** the snippet remains self-contained (checkout, run, upload-sarif) and does not silently omit a required detail (e.g., `security-events: write` permission) at a level of detail consistent with the existing "Maintained PR Action" example's permissions callout

**Edge Case 2: Reader already runs the "Maintained PR Action" workflow**
- **Given** a repo already runs the composite `samestrin/atcr@v1` action for PR checks
- **When** they add the new SARIF-upload workflow alongside it
- **Then** the distinction sentence makes clear both can coexist — one feeds PR checks/inline comments, the other feeds the Security tab — without implying redundancy or conflict

## Error Conditions
**Error Scenario 1: YAML snippet fails to parse**
- Error message: a YAML syntax error (e.g. "mapping values are not allowed in this context") reported by `yamllint`/`yq` when validating the extracted fenced block
- HTTP status / error code: N/A (static documentation, no runtime surface) — treated as a lint failure blocking merge

**Error Scenario 2: Broken relative link to `docs/github-action.md`**
- Error message: `markdown-link-check` style "404 relative path not found: github-action.md"
- HTTP status / error code: N/A — link-check failure blocking merge

**Error Scenario 3: Missing or buried distinction sentence**
- Error message: manual review comment "subsection does not clearly state this is separate from `atcr github`"
- HTTP status / error code: N/A — review-flagged doc gap corresponding to the story's Risk #1

## Performance Requirements
- **Response Time:** N/A (static documentation, no runtime); the subsection should be scannable and copy-pasteable in well under a minute by a reader already familiar with the "Maintained PR Action" pattern
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** the snippet must not hardcode secrets; any API key referenced follows the `${{ secrets.* }}` pattern already used in the "Maintained PR Action" example
- **Input Validation:** N/A (documentation only, no user input); the YAML itself must not model an unsafe workflow pattern (e.g. no `pull_request_target` misuse, no over-broad token scoping beyond what `upload-sarif` requires)

## Test Implementation Guidance
**Test Type:** MANUAL / STATIC (YAML lint + manual documentation review)
**Test Data Requirements:** none — validation runs directly against the fenced snippet text extracted from the rendered markdown
**Mock/Stub Requirements:** none (no runtime component to mock)

## Definition of Done
**Auto-Verified:**
- [ ] Markdown renders without lint errors (markdownlint)
- [ ] Fenced YAML block parses cleanly (yamllint / yq)
- [ ] No broken relative links (markdown-link-check against `docs/github-action.md`)

**Story-Specific:**
- [ ] New subsection (e.g. "SARIF Upload for GitHub Code Scanning") added directly beneath "Maintained PR Action" in `docs/ci-integration.md`
- [ ] YAML snippet matches the verbatim example in `documentation/github-code-scanning-integration.md` (checkout with `fetch-depth: 0`, atcr pipeline, `upload-sarif@v3`)
- [ ] Distinction sentence present near the top of the subsection, linking to `docs/github-action.md`
- [ ] No edits made to `docs/github-action.md` content (link-only reference)

**Manual Review:**
- [ ] Doc reviewed and approved by a maintainer familiar with `docs/github-action.md`
