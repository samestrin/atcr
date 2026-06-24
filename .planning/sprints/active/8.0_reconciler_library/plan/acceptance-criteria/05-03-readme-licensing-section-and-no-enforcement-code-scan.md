# Acceptance Criteria: README Licensing Section and No-Enforcement Code Scan

**Related User Story:** [05: Commercial License Placeholder for Proprietary Embedding](../user-stories/05-dual-licensing-path.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | README licensing section (Markdown) + repo code scan | `reconcile/README.md` short licensing section cross-references both license files |
| Test Framework | grep/CI checks + `go build` | verifies README pointers resolve and no enforcement code exists in the module |
| Key Dependencies | `reconcile/LICENSE` (Story 4) and `reconcile/LICENSE-COMMERCIAL.md` (AC 05-01) | both files the README cross-references |
| Enforcement Model | None — scan confirms no license-check/payment-gating code anywhere in `./reconcile/` | documentation + a negative code scan |

### Related Files (from codebase-discovery.json)
- `reconcile/README.md` - create (Story 4 owns the README; this AC verifies/extends the licensing section): includes a short licensing section cross-referencing both `LICENSE` and `LICENSE-COMMERCIAL.md` so a vendor discovers the commercial path without hunting.
- `reconcile/LICENSE` - create (Story 4): the Apache 2.0 OSS half the README points to.
- `reconcile/LICENSE-COMMERCIAL.md` - create (AC 05-01): the commercial placeholder the README's commercial pointer must resolve to.
- `reconcile/*.go` - scan: the module source is scanned to confirm no license-check or payment-gating code exists.

## Happy Path Scenarios
**Scenario 1: README includes a short licensing section**
- **Given** a vendor is evaluating the module via `reconcile/README.md`
- **When** the vendor reads the README
- **Then** a short licensing section is present and cross-references both `LICENSE` (Apache 2.0) and `LICENSE-COMMERCIAL.md` (commercial)

**Scenario 2: Commercial path is discoverable without hunting**
- **Given** the commercial license path is the value-add for proprietary vendors
- **When** a vendor scans the README
- **Then** the licensing section surfaces the commercial path (a one-line pointer to `LICENSE-COMMERCIAL.md`) so the vendor discovers it without reading the file tree

**Scenario 3: README pointers resolve to real files**
- **Given** the README cross-references both license files
- **When** the pointers are resolved against the filesystem
- **Then** both `reconcile/LICENSE` and `reconcile/LICENSE-COMMERCIAL.md` exist at the paths the README names (no dangling pointers)

**Scenario 4: Module contains no license-check or payment-gating code**
- **Given** the epic explicitly defers automated license enforcement as out of scope
- **When** the `./reconcile/` module source is scanned
- **Then** no license-check, license-validation, payment-gating, or telemetry code exists anywhere in the module (the licensing layer is documentation plus a future legal wrapper, not runtime code)

## Edge Cases
**Edge Case 1: README licensing section duplicates the commercial file's content**
- **Given** the README should surface, not duplicate, the commercial path
- **When** the licensing section is reviewed
- **Then** it is a short cross-reference (one-line pointer + Apache 2.0 notice), not a copy of `LICENSE-COMMERCIAL.md`'s content

**Edge Case 2: A Go file imports a license-checking package**
- **Given** a third-party license-check library could slip in during extraction
- **When** `go.mod` and the source are scanned
- **Then** no license-enforcement dependency appears in `reconcile/go.mod` and no `import` references a license-check package

**Edge Case 3: Enforcement code is deferred but stubbed**
- **Given** a stub or TODO could imply future enforcement
- **When** the source is scanned
- **Then** no license-enforcement stub, TODO, or build-tagged gate exists (the no-enforcement stance is absolute for this extraction)

## Error Conditions
**Error Scenario 1: README licensing section is missing**
- Error message: "reconcile/README.md: missing licensing section cross-referencing LICENSE and LICENSE-COMMERCIAL.md"
- HTTP status / error code: N/A — CI grep check (`grep -E 'LICENSE-COMMERCIAL|commercial' reconcile/README.md` returns matches) fails if absent

**Error Scenario 2: README commercial pointer dangles**
- Error message: "reconcile/README.md: commercial-license pointer does not resolve (LICENSE-COMMERCIAL.md missing)"
- HTTP status / error code: N/A — a CI script resolves the README's license pointers against the filesystem and fails on a dangling pointer

**Error Scenario 3: License-check or payment-gating code is found**
- Error message: "reconcile/: license-enforcement code detected (file:line)"
- HTTP status / error code: N/A — a CI scan (`grep -rEi 'license.?check|license.?valid|payment.?gat|phone.?home' reconcile/ --include='*.go'` returns no matches) fails the build if enforcement code is present

## Performance Requirements
- **Response Time:** N/A — static Markdown + a grep scan over the small `./reconcile/` module (sub-second).
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public docs and a negative code scan.
- **Input Validation:** N/A — no executable content added.
- **No Telemetry:** The scan must confirm no phone-home, telemetry, or license-reporting code exists in the module — the licensing layer is documentation only, matching the epic's explicit out-of-scope for enforcement.
- **No Secrets:** N/A — the scan and README add no secrets.

## Test Implementation Guidance
**Test Type:** UNIT (docs cross-check + code scan script)
**Test Data Requirements:** The set of license filenames the README should reference (`LICENSE`, `LICENSE-COMMERCIAL.md`); a denylist of enforcement-code patterns.
**Mock/Stub Requirements:** None. Implement a `make verify-licensing` target (or extend the existing verify targets) that: (1) greps `reconcile/README.md` for references to both `LICENSE` and `LICENSE-COMMERCIAL.md`; (2) resolves each referenced path against the filesystem; (3) scans `reconcile/*.go` and `reconcile/go.mod` for enforcement-code patterns (`license.?check`, `license.?valid`, `payment.?gat`, `phone.?home`) and fails if any match; (4) confirms no license-enforcement import appears in `go.mod`.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`verify-licensing` target passes; `go test ./reconcile/...` still green)
- [x] No linting errors
- [x] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [x] `reconcile/README.md` includes a short licensing section cross-referencing both `LICENSE` and `LICENSE-COMMERCIAL.md`
- [x] Both README license pointers resolve to real files at the module root
- [x] The commercial path is discoverable from the README without hunting the file tree
- [x] A scan of `./reconcile/` confirms no license-check, payment-gating, or telemetry code exists in the module

**Manual Review:**
- [x] Code reviewed and approved
- [x] README licensing section eyeballed to confirm it surfaces (not duplicates) the commercial path
- [x] Code scan output reviewed to confirm a clean negative (no enforcement code)
