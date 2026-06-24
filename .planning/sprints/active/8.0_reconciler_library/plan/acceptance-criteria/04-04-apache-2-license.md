# Acceptance Criteria: Apache 2.0 LICENSE at Module Root

**Related User Story:** [04: OSS Adoption Documentation and Apache 2.0 License](../user-stories/04-oss-adoption-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | License file (plain text) | `reconcile/LICENSE` — verbatim Apache 2.0 full text |
| Test Framework | manual inspection + `go-licenses` / diff against canonical Apache text | verifies verbatim text and copyright line |
| Key Dependencies | Apache 2.0 full text (apache.org) | the canonical source for byte-diff verification |
| License Scope | Apache 2.0 only — the permissive half of the dual-license path | `LICENSE-COMMERCIAL.md` is Story 5; this AC does not touch it |

### Related Files (from codebase-discovery.json)
- `reconcile/LICENSE` - create: verbatim Apache 2.0 full text with the copyright holder line populated (Copyright 2026 Sam Estrin); placed at the module root so `go list -m` and pkg.go.dev surface it automatically.
- `reconcile/go.mod` - read: module root — the `LICENSE` sits alongside `go.mod` so tooling discovers it.
- `reconcile/README.md` - create: the README's license pointer targets `LICENSE` (Apache 2.0) and the `LICENSE-COMMERCIAL.md` placeholder (Story 5).
- `reconcile/LICENSE-COMMERCIAL.md` - create (placeholder, delivered by Story 5): the README one-line pointer references it; this AC only verifies the pointer, not the commercial file.

## Happy Path Scenarios
**Scenario 1: LICENSE contains the verbatim Apache 2.0 full text**
- **Given** the canonical Apache 2.0 full text from apache.org/licenses/LICENSE-2.0.txt
- **When** `reconcile/LICENSE` is diffed against the canonical text
- **Then** the body matches verbatim (the Apache-2.0 boilerplate appendix is preserved), with only the copyright holder line filled in

**Scenario 2: Copyright holder line is populated**
- **Given** the Apache 2.0 boilerplate contains `Copyright [yyyy] [name of copyright owner]`
- **When** `reconcile/LICENSE` is inspected
- **Then** the line reads `Copyright 2026 Sam Estrin` (year and holder populated), and the placeholder tokens `[yyyy]` and `[name of copyright owner]` do not appear anywhere in the file

**Scenario 3: LICENSE is at the module root and discoverable**
- **Given** `reconcile/LICENSE` sits next to `reconcile/go.mod`
- **When** `go list -m -json github.com/samestrin/atcr/reconcile` (or `go-licenses`) inspects the module
- **Then** the license is detected as Apache-2.0 by tooling, and pkg.go.dev surfaces it on the module page

**Scenario 4: No enforcement code is added**
- **Given** this is a permissive license file, not a click-through or runtime license check
- **When** the `reconcile` package source is scanned
- **Then** no license-enforcement code (runtime checks, phone-home, feature gates tied to license) is introduced by this story

## Edge Cases
**Edge Case 1: LICENSE uses a truncated or SPDX-pointer-only text**
- **Given** some projects ship only `SPDX-License-Identifier: Apache-2.0`
- **When** `reconcile/LICENSE` is inspected
- **Then** it contains the full Apache 2.0 text, not a pointer or truncated summary (pkg.go.dev and `go-licenses` require the full text for accurate reporting)

**Edge Case 2: Year is stale or missing**
- **Given** the copyright line is templated
- **When** the file is reviewed
- **Then** the year is `2026` (the extraction sprint year) and is a literal, not `[yyyy]`

**Edge Case 3: LICENSE drifts from canonical Apache text during edits**
- **Given** a future edit reformats or rewraps the boilerplate
- **When** the file is re-diffed against apache.org's canonical text
- **Then** a CI diff check flags any non-copyright-line drift

## Error Conditions
**Error Scenario 1: LICENSE is missing**
- Error message: "reconcile/LICENSE: no such file or directory"
- HTTP status / error code: N/A — CI `test -f reconcile/LICENSE` check fails the build

**Error Scenario 2: LICENSE is not Apache 2.0**
- Error message: "reconcile/LICENSE license mismatch: expected Apache-2.0, got <detected>"
- HTTP status / error code: N/A — `go-licenses` (or a diff against the canonical Apache text) reports a mismatch and fails CI

**Error Scenario 3: Copyright holder line is unfilled**
- Error message: "reconcile/LICENSE still contains placeholder [name of copyright owner] or [yyyy]"
- HTTP status / error code: N/A — a grep check (`grep -E '\[yyyy\]|\[name of copyright owner\]' reconcile/LICENSE` returns no matches) fails the build

## Performance Requirements
- **Response Time:** N/A — static text file.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public license file.
- **Input Validation:** N/A — no executable content.
- **Legal Integrity:** The Apache 2.0 text must be the canonical full text from apache.org, not a paraphrase; the copyright holder name and year must be accurate (Sam Estrin, 2026). No additional license terms or restrictions beyond Apache 2.0 are added (the commercial path is a separate file, Story 5).
- **No Secrets:** The LICENSE file must not contain credentials, API keys, or private information (it is a public file).

## Test Implementation Guidance
**Test Type:** UNIT (file verification script)
**Test Data Requirements:** The canonical Apache 2.0 full text (fetched once and vendored as a fixture, or referenced from a known-good local copy) for byte-diff verification.
**Mock/Stub Requirements:** None. Implement a `make verify-license` target (or `.githooks` step) that: (1) asserts `reconcile/LICENSE` exists; (2) diffs the file against the canonical Apache 2.0 text, allowing only the copyright holder line to differ; (3) greps for unfilled `[yyyy]` / `[name of copyright owner]` placeholders and fails if found.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`verify-license` target passes; `go test ./reconcile/...` still green)
- [x] No linting errors
- [x] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [x] `reconcile/LICENSE` exists at the module root (alongside `reconcile/go.mod`)
- [x] LICENSE body matches the verbatim Apache 2.0 full text (only the copyright line differs from canonical)
- [x] Copyright line reads `Copyright 2026 Sam Estrin` with no `[yyyy]` / `[name of copyright owner]` placeholders remaining
- [x] `go-licenses` (or equivalent) detects the module license as Apache-2.0
- [x] No license-enforcement code is introduced by this story
- [x] `LICENSE-COMMERCIAL.md` is NOT created by this story (Story 5 owns it); only the README pointer to it is verified

**Manual Review:**
- [x] Code reviewed and approved
- [x] LICENSE eyeballed against apache.org canonical text and the copyright line confirmed
