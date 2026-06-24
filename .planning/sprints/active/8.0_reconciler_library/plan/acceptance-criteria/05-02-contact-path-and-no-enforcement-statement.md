# Acceptance Criteria: Contact Path and No-Enforcement Statement

**Related User Story:** [05: Commercial License Placeholder for Proprietary Embedding](../user-stories/05-dual-licensing-path.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | License placeholder (Markdown) | contact path + no-enforcement statement inside `reconcile/LICENSE-COMMERCIAL.md` |
| Test Framework | manual inspection + grep/CI checks | verifies a reachable, Sam-owned contact path and the no-enforcement statement |
| Key Dependencies | GitHub-hosted contact channel (issue template / Discussions) | a path that survives contact with public repos and moves with the repository |
| Enforcement Model | None — documentation only | no license-check, no payment gating, no telemetry ships with the code |

### Related Files (from codebase-discovery.json)
- `reconcile/LICENSE-COMMERCIAL.md` - create (extends AC 05-01): names a contact path a vendor can use to initiate commercial licensing, and explicitly states no automated enforcement or payment gating ships with the code.
- `reconcile/LICENSE` - create (Story 4): the Apache 2.0 OSS half; the contact path initiates the commercial half distinct from the permissive OSS license.
- `.github/ISSUE_TEMPLATE/` (or GitHub Discussions) - read: the contact channel the file points to; must be a path Sam controls and can respond to.
- `reconcile/README.md` - create (Story 4 owns): the README licensing section surfaces the contact path; AC 05-03 verifies the README pointer.

## Happy Path Scenarios
**Scenario 1: File names a reachable contact path**
- **Given** a vendor wants to initiate a commercial licensing conversation
- **When** the vendor reads `reconcile/LICENSE-COMMERCIAL.md`
- **Then** the file names a contact path (a GitHub issue template or Discussions link preferred over a personal email) that the vendor can use to reach Sam

**Scenario 2: Contact path is GitHub-hosted and moves with the repo**
- **Given** the risk that a personal email rots or is abandoned before a vendor reaches out
- **When** the contact path is inspected
- **Then** it points to a GitHub-hosted channel (issue template or Discussions) that moves with the repository rather than a personal email, so it survives contact with public repos

**Scenario 3: File explicitly states no automated enforcement or payment gating**
- **Given** the epic explicitly defers automated license enforcement as out of scope
- **When** the file is inspected
- **Then** it plainly states that the project intentionally ships no enforcement, license-check, or payment-gating code, and that commercial compliance is a legal-wrapper concern, not a runtime one

**Scenario 4: Vendor is not confused by the absence of enforcement**
- **Given** a vendor might expect enforcement/telemetry that would make commercial detection automatic
- **When** the vendor reads the no-enforcement statement
- **Then** the absence of enforcement is explained as intentional (matching the epic's explicit out-of-scope), not a gap

## Edge Cases
**Edge Case 1: Contact path is a personal email that may rot**
- **Given** a personal email is fragile and may not survive between extraction and separate-repo publication
- **When** the contact path is reviewed
- **Then** a GitHub-hosted path (issue template or Discussions) is preferred; a personal email is a fallback only if no GitHub channel exists, and must be one Sam actively monitors

**Edge Case 2: Contact path resolves to a dead/abandoned channel**
- **Given** the contact path could rot before a vendor reaches out
- **When** the path is verified during the extraction sprint
- **Then** the path resolves to a live, Sam-owned channel (e.g., the issue template loads or Discussions is enabled)

**Edge Case 3: No-enforcement statement is ambiguous**
- **Given** a vague statement like "enforcement may be added later" could confuse a vendor
- **When** the statement is reviewed
- **Then** it is unambiguous: no enforcement, license-check, or payment-gating code is present or planned as part of this extraction

## Error Conditions
**Error Scenario 1: Contact path is missing or empty**
- Error message: "LICENSE-COMMERCIAL.md: no contact path found for commercial licensing"
- HTTP status / error code: N/A — CI grep check for a contact path (URL or `github.com/samestrin/atcr` reference) returns no matches and fails the build

**Error Scenario 2: Contact path does not resolve**
- Error message: "LICENSE-COMMERCIAL.md contact path does not resolve (dead link / abandoned channel)"
- HTTP status / error code: N/A — manual verification during the sprint flags a dead path as a blocker

**Error Scenario 3: No-enforcement statement is missing**
- Error message: "LICENSE-COMMERCIAL.md: missing no-enforcement/no-payment-gating statement"
- HTTP status / error code: N/A — CI grep check (`grep -Ei 'enforcement|payment.?gat|license.?check' reconcile/LICENSE-COMMERCIAL.md` returns matches confirming the statement) fails if absent

## Performance Requirements
- **Response Time:** N/A — static Markdown documentation file.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public documentation file; the contact path is a public channel.
- **Input Validation:** N/A — no executable content.
- **Contact Path Integrity:** The contact path must point to a channel Sam controls and can respond to; a GitHub-hosted path is preferred so it moves with the repository and does not rot. The path must not expose a private or unmonitored email.
- **No Secrets:** The file must not contain credentials, API keys, or private contact details.

## Test Implementation Guidance
**Test Type:** UNIT (file verification script) + MANUAL (contact path reachability)
**Test Data Requirements:** The expected contact path string (GitHub issue template URL or Discussions link).
**Mock/Stub Requirements:** None. Implement a `make verify-commercial-license` target (or extend the AC 05-01 target) that: (1) greps for a contact path (`grep -E 'github.com/samestrin/atcr|discussions|issue' reconcile/LICENSE-COMMERCIAL.md`); (2) greps for the no-enforcement statement (`grep -Ei 'no enforcement|no license-check|no payment' ...`); (3) fails if either is absent. The contact path reachability is a manual verification step during the sprint (confirm the issue template loads / Discussions is enabled).

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`verify-commercial-license` target passes; `go test ./reconcile/...` still green)
- [x] No linting errors
- [x] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [x] File names a contact path a vendor can use to initiate commercial licensing
- [x] Contact path is GitHub-hosted (issue template or Discussions) and moves with the repo
- [x] File explicitly states no automated enforcement, license-check, or payment-gating code ships with the module
- [x] Contact path verified to resolve during the extraction sprint

**Manual Review:**
- [x] Code reviewed and approved
- [x] Contact path clicked/loaded to confirm it reaches a live, Sam-owned channel
