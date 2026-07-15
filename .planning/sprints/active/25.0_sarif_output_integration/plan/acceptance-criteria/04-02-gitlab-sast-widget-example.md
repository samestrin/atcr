# Acceptance Criteria: GitLab CI SAST Widget Example

**Related User Story:** [04: SARIF CI Integration Documentation](../user-stories/04-sarif-ci-integration-docs.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation | GitHub Flavored Markdown, fenced `yaml` code block modeling a `.gitlab-ci.yml` job |
| Test Framework | YAML lint / manual review | `yamllint` against the extracted fenced block; manual cross-check against GitLab's documented `artifacts:reports:sast` schema |
| Key Dependencies | GitLab CI native SAST report artifact ingestion (`artifacts: reports: sast:`) | External GitLab CI mechanism, referenced not vendored — atcr has no existing GitLab integration to model it on |

### Related Files (from codebase-discovery.json)

- [`docs/ci-integration.md`](../../../../../docs/ci-integration.md) — modify: add a fenced `.gitlab-ci.yml` snippet in the same subsection as (or immediately adjacent to) the GitHub Actions example, showing the GitLab job that produces `results.sarif` and wires it via `artifacts: reports: sast:`.
- [`docs/github-action.md`](../../../../../docs/github-action.md) — reference only (no edits): ensures the GitLab-facing text does not conflate `atcr github` (a GitHub-only, direct-API flow) with the GitLab SAST widget path.
- [`.planning/plans/active/25.0_sarif_output_integration/documentation/github-code-scanning-integration.md](../documentation/github-code-scanning-integration.md) — reference: grounds the "this is not `atcr github`" framing reused for the GitLab audience, and documents the shared `atcr review && atcr reconcile && atcr report --format=sarif` command the GitLab job also runs.

### Technical References

- [GitHub Code Scanning SARIF Integration Constraints](../documentation/github-code-scanning-integration.md)

## Happy Path Scenarios
**Scenario 1: Reader finds a GitLab CI job producing a SARIF SAST report artifact**
- **Given** a reader viewing the SARIF subsection of `docs/ci-integration.md`
- **When** they scroll to the GitLab CI snippet
- **Then** they see a fenced `yaml` block resembling a `.gitlab-ci.yml` job with a `script:` step running `atcr review && atcr reconcile && atcr report --format=sarif > results.sarif` and an `artifacts: reports: sast: results.sarif` block wiring it into GitLab's native SAST report widget

**Scenario 2: GitLab-only reader confirms this isn't `atcr github`**
- **Given** `atcr github` (`cmd/atcr/github.go`) is a GitHub-only, direct-API flow with no GitLab equivalent
- **When** a GitLab-only reader reads the subsection
- **Then** no GitHub-only terminology (PR checks, inline comments) is presented as if it applies to GitLab pipelines; the GitLab snippet stands on its own as CI job + artifact wiring

## Edge Cases
**Edge Case 1: Reader assumes GitLab has an upload-action equivalent to `codeql-action/upload-sarif`**
- **Given** a reader is anchored on the adjacent GitHub example (`github/codeql-action/upload-sarif@v3`)
- **When** they read the GitLab snippet
- **Then** the doc clearly uses GitLab's own artifact-based mechanism (`artifacts: reports: sast:`) rather than implying an equivalent "upload action" step exists, avoiding a copy-paste failure

**Edge Case 2: GitLab MR widget vs. Security Dashboard tier differences**
- **Given** GitLab surfaces SAST results in both the MR widget and a project-level Security Dashboard depending on subscription tier
- **When** the doc describes what the artifact produces
- **Then** the claim stays scoped to "GitLab's native SAST report artifact mechanism" as grounded in the story's Technical Considerations, without overclaiming which specific GitLab tier/surface renders the widget

## Error Conditions
**Error Scenario 1: `.gitlab-ci.yml` snippet fails YAML parse**
- Error message: a YAML syntax error reported by `yamllint` on the extracted fenced block
- HTTP status / error code: N/A (static documentation) — lint failure blocking merge

**Error Scenario 2: Incorrect artifact report key**
- Error message: manual review comment "`artifacts:reports:sast` key does not match GitLab's documented schema" (e.g. singular `report:` typo'd for `reports:`)
- HTTP status / error code: N/A — review-flagged inaccuracy corresponding to the story's Risk #2

**Error Scenario 3: GitHub-only concepts bleed into the GitLab example**
- Error message: manual review comment "GitHub-specific terminology (PR check / inline comment) appears in the GitLab-facing snippet or its lead-in text"
- HTTP status / error code: N/A — review-flagged doc gap

## Performance Requirements
- **Response Time:** N/A (static documentation, no runtime); per the story's risk mitigation the snippet stays minimal — job, script, artifact wiring only, no speculative advanced GitLab features
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** the snippet uses GitLab CI/CD variables (e.g. `$OPENROUTER_API_KEY`) rather than a hardcoded secret, mirroring the `${{ secrets.* }}` pattern used in the GitHub example
- **Input Validation:** N/A (documentation only, no user input); the snippet should not model disabling GitLab's masked/protected variable handling

## Test Implementation Guidance
**Test Type:** MANUAL / STATIC (YAML lint + manual review against GitLab's documented `artifacts:reports:sast` mechanism)
**Test Data Requirements:** none — validation runs directly against the fenced snippet text
**Mock/Stub Requirements:** none (no runtime component to mock; atcr has no existing GitLab integration test surface to extend)

## Definition of Done
**Auto-Verified:**
- [x] Markdown renders without lint errors (markdownlint)
- [x] Fenced YAML block parses cleanly (yamllint)
- [x] No broken links elsewhere in the subsection

**Story-Specific:**
- [x] `.gitlab-ci.yml`-style fenced snippet added to `docs/ci-integration.md` alongside the GitHub Actions snippet
- [x] Snippet includes `atcr review && atcr reconcile && atcr report --format=sarif > results.sarif` and `artifacts: reports: sast: results.sarif`
- [x] Snippet stays minimal — no speculative or unverified GitLab features
- [x] Terminology stays GitLab-native — no bleed of GitHub-only "PR check"/"inline comment" language

**Manual Review:**
- [x] Doc reviewed and approved, cross-checked against GitLab's documented `artifacts:reports:sast` mechanism
