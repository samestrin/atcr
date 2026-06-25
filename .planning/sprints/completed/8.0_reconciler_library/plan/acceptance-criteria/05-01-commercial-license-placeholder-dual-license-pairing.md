# Acceptance Criteria: Commercial License Placeholder File with Dual-License Pairing

**Related User Story:** [05: Commercial License Placeholder for Proprietary Embedding](../user-stories/05-dual-licensing-path.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | License placeholder (Markdown) | `reconcile/LICENSE-COMMERCIAL.md` — documentation-only, not compiled or imported by any Go code |
| Test Framework | manual inspection + grep/CI checks | verifies presence, required phrases, and dual-license pairing |
| Key Dependencies | `reconcile/LICENSE` (Apache 2.0, Story 4) | the OSS half of the dual-license this file pairs with |
| License Scope | Commercial half of the dual-license only | placeholder for evaluation; no final license-grant language (that waits for a licensee) |

### Related Files (from codebase-discovery.json)
- `reconcile/LICENSE-COMMERCIAL.md` - create: states a commercial license is available for proprietary/closed-source embedding, paired with the Apache 2.0 `LICENSE`; documentation-only with no enforcement code.
- `reconcile/LICENSE` - create (delivered by Story 4): the Apache 2.0 OSS half; this AC verifies both files coexist at the module root so the dual-license is visible.
- `reconcile/go.mod` - read: module root; the license files sit alongside `go.mod` so a vendor cloning the module sees both, and `go list -m`/pkg.go.dev discover them.
- `reconcile/README.md` - create (Story 4 owns): cross-references both license files; AC 05-03 verifies the pointer resolves to this file.

## Happy Path Scenarios
**Scenario 1: LICENSE-COMMERCIAL.md exists at the module root**
- **Given** the `reconcile/` module has its own `go.mod` at the module root
- **When** a vendor clones or imports `github.com/samestrin/atcr/reconcile`
- **Then** `reconcile/LICENSE-COMMERCIAL.md` exists alongside `reconcile/LICENSE` and `reconcile/go.mod`, so both halves of the dual-license are visible without hunting

**Scenario 2: File states a commercial license is available for proprietary embedding**
- **Given** a proprietary devtools vendor is evaluating the reconciler for white-label/OEM embedding
- **When** the vendor reads `reconcile/LICENSE-COMMERCIAL.md`
- **Then** the file clearly states that a commercial license is available for proprietary/closed-source embedding, distinct from the Apache 2.0 OSS license

**Scenario 3: File is explicitly a placeholder, not a license grant**
- **Given** the actual commercial license text awaits a licensee and legal review
- **When** the file is inspected
- **Then** it is explicitly labeled as a placeholder for evaluation, and states that final commercial terms are negotiated on contact rather than granted by the file (no license-grant language is drafted)

**Scenario 4: Dual-license pairing is complete**
- **Given** the Apache 2.0 `reconcile/LICENSE` (Story 4) is the OSS half
- **When** both license files are checked
- **Then** both `reconcile/LICENSE` and `reconcile/LICENSE-COMMERCIAL.md` coexist at the module root, so the commercial placeholder does not ship as an orphan pointing at a missing OSS license

## Edge Cases
**Edge Case 1: Placeholder overstates commercial terms as a legal commitment**
- **Given** the risk that placeholder language creates a binding commitment before a real license exists
- **When** the file is reviewed
- **Then** the file avoids drafting actual license-grant language; it states terms are available on request and negotiated on contact, not granted by the file

**Edge Case 2: File is a SPDX pointer or one-liner only**
- **Given** some projects ship only a SPDX identifier or a single sentence
- **When** the file is inspected
- **Then** it contains enough substance for a vendor to understand the dual-license model and how to initiate a commercial conversation (not just `SPDX-License-Identifier: Apache-2.0`)

**Edge Case 3: Apache 2.0 LICENSE slips and the commercial placeholder is orphaned**
- **Given** the Apache 2.0 `LICENSE` lands in a separate story and slips
- **When** this story's AC verification runs
- **Then** a missing `reconcile/LICENSE` blocks this story's completion (the AC explicitly verifies both files coexist) rather than shipping an orphan commercial placeholder

## Error Conditions
**Error Scenario 1: LICENSE-COMMERCIAL.md is missing**
- Error message: "reconcile/LICENSE-COMMERCIAL.md: no such file or directory"
- HTTP status / error code: N/A — CI `test -f reconcile/LICENSE-COMMERCIAL.md` check fails the build

**Error Scenario 2: Companion Apache 2.0 LICENSE is missing**
- Error message: "reconcile/LICENSE missing: dual-license pairing incomplete (commercial placeholder is orphaned)"
- HTTP status / error code: N/A — CI check asserts both `reconcile/LICENSE` and `reconcile/LICENSE-COMMERCIAL.md` exist; failure blocks this story

**Error Scenario 3: File drafts binding license-grant language**
- Error message: "LICENSE-COMMERCIAL.md contains license-grant language (e.g., 'hereby grants', 'granted to', 'licensed to you')"
- HTTP status / error code: N/A — a grep denylist (`grep -Ei 'hereby grants|granted to|licensed to you' reconcile/LICENSE-COMMERCIAL.md` returns no matches) fails the build

## Performance Requirements
- **Response Time:** N/A — static Markdown documentation file.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public documentation file.
- **Input Validation:** N/A — no executable content; the file is not compiled, tested, or imported by any Go code.
- **Legal Integrity:** The file is explicitly a placeholder for evaluation; it must not draft actual license-grant language that could create a legal commitment before a real commercial license exists. Final commercial terms wait for a licensee and legal review. Copyright line reads `Copyright 2026 Sam Estrin` where attribution appears.
- **No Secrets:** The file must not contain credentials, API keys, private contact details, or non-public information (it is a public file).

## Test Implementation Guidance
**Test Type:** UNIT (file verification script)
**Test Data Requirements:** None beyond the file itself. A denylist of license-grant phrases to grep for (e.g., "hereby grants", "granted to", "licensed to you").
**Mock/Stub Requirements:** None. Implement a `make verify-commercial-license` target (or `.githooks` step) that: (1) asserts `reconcile/LICENSE-COMMERCIAL.md` exists; (2) asserts `reconcile/LICENSE` (Apache 2.0, Story 4) also exists so the pairing is complete; (3) greps for required phrases ("commercial", "proprietary", "embedding", "placeholder"); (4) greps the license-grant denylist and fails if matched.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`verify-commercial-license` target passes; `go test ./reconcile/...` still green)
- [x] No linting errors
- [x] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [x] `reconcile/LICENSE-COMMERCIAL.md` exists at the module root (alongside `reconcile/go.mod`)
- [x] File states a commercial license is available for proprietary/closed-source embedding
- [x] File is explicitly labeled a placeholder for evaluation (no binding license-grant language)
- [x] Both `reconcile/LICENSE` (Apache 2.0, Story 4) and `reconcile/LICENSE-COMMERCIAL.md` coexist (dual-license pairing complete)

**Manual Review:**
- [x] Code reviewed and approved
- [x] Placeholder language eyeballed to confirm it does not overstate commercial terms or create a legal commitment
