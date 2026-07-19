# Acceptance Criteria: Pinning Test Guarantees No ANSI/OSC Escape Sequences Reach `--axi` Stdout

**Related User Story:** [04: AXI Stderr Isolation and Escape-Sequence Guarantee](../user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go unit test (pinning/regression guard) | Style precedent: `TestDriftLine_StripsControlChars`, `TestRenderPersonaSearch_StripsControlChars` |
| Test Framework | `go test` / `testify` (`assert`, `require`) + `regexp` for escape-byte matching | No new dependencies |
| Key Dependencies | `cmd/atcr/models.go`'s `sanitizeDisplay` idiom (line 288-296, `strings.Map` + `unicode.IsControl`) as the pattern precedent, not a shared call site | Reuses the existing sanitization idiom rather than a parallel mechanism |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/models_test.go` (or a new `cmd/atcr/axi_escape_test.go`) - create/modify: add a pinning test that asserts captured `--axi` stdout contains no bytes matching an ANSI/OSC escape pattern (`\x1b\[` for CSI, `\x1b\]` for OSC), following the existing `TestDriftLine_StripsControlChars` precedent.
- `cmd/atcr/quickstart.go` - reference (not modified): `osc8()` (line 456-461) is the known counterexample proving `atcr` can emit raw OSC-8 escapes (`"\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\"`) on an interactive path (`quickstart`, invoked from line 194); the pinning test's regex must be shaped to catch this exact byte pattern as a regression guard, even though `osc8()` itself is never called from `atcr review`/`atcr resume`.
- `cmd/atcr/review.go` - reference: the AXI-payload write path this test exercises (post-Story-1 gating from AC 04-01).
- `cmd/atcr/resume.go` - reference: the AXI-payload write path for the resume flow this test exercises.

## Happy Path Scenarios
**Scenario 1: `--axi` stdout contains zero escape bytes for a clean review**
- **Given** `atcr review --axi` is run against a valid commit range producing findings
- **When** the resulting stdout is captured into a buffer
- **Then** a regex scan for `\x1b\[` (CSI) and `\x1b\]` (OSC) over the captured bytes returns zero matches

**Scenario 2: `--axi` stdout contains zero escape bytes for a resumed review**
- **Given** `atcr resume --axi` is run to completion
- **When** the resulting stdout is captured into a buffer
- **Then** the same escape-byte regex scan returns zero matches

**Scenario 3: Regression guard reproduces and rejects the known `osc8()` pattern**
- **Given** a test fixture that injects `osc8("https://example.com")`'s exact output (`"\x1b]8;;https://example.com\x1b\\https://example.com\x1b]8;;\x1b\\"`) into a buffer standing in for `--axi` stdout
- **When** the pinning test's escape-detection helper is run against that fixture
- **Then** the helper flags the fixture as containing an escape sequence, proving the test would actually catch this exact counterexample class if it ever leaked onto `--axi` stdout

## Edge Cases
**Edge Case 1: A finding field containing a crafted escape sequence**
- **Given** a review finding whose `message`/`title`/`file` field was crafted (e.g. by a compromised persona/catalog entry) to contain a raw `\x1b[31m` CSI color-code sequence
- **When** that finding is rendered through the `--axi` TOON/JSON payload (Story 1)
- **Then** the escape bytes do not appear unescaped in stdout — either because the encoder escapes/rejects control characters by construction (mirroring the `--json` path's standard-library encoding behavior, per `models.go:290-291`'s comment) or because an explicit sanitization step strips them before write

**Edge Case 2: Multiple escape variants in one payload**
- **Given** a payload containing both CSI (`\x1b[`) and OSC (`\x1b]`) sequences from different injected fields
- **When** the pinning test's regex scan runs
- **Then** both variants are independently detected — the test's regex must not be narrowly scoped to only one of the two forms

## Error Conditions
**Error Scenario 1: Pinning test itself fails to compile/parse a malformed escape fixture**
- **Given** a test fixture string is malformed (e.g., an unterminated escape sequence)
- **When** the test suite runs
- **Then** the test fails loudly with a clear assertion message identifying which fixture/byte offset triggered the detection, not a silent pass
- Error message: `"unexpected escape sequence in --axi stdout at byte offset %d: %q"` (or equivalent, test-local convention)
- HTTP status / error code: N/A (Go test failure, not a CLI exit code)

## Performance Requirements
- **Response Time:** The regex scan runs once per test case over a bounded in-memory buffer (typical review output, low KB range) — negligible test-suite runtime impact (sub-millisecond per assertion).
- **Throughput:** N/A — test-only code path, never executed in production `atcr` binaries.

## Security Considerations
- **Authentication/Authorization:** N/A — this AC is a defensive regression test, not an auth boundary.
- **Input Validation:** The pinning test's primary value is validating that untrusted/crafted input (finding fields sourced from repo content, persona catalogs, or agent output) cannot smuggle terminal escape sequences into a machine-consumed artifact (`findings.json`), which could otherwise be used to manipulate a downstream terminal or naively-parsing consumer that echoes the payload.

## Test Implementation Guidance
**Test Type:** UNIT (primary) — a self-contained pinning/regression test, in the style of `TestDriftLine_StripsControlChars` / `TestRenderPersonaSearch_StripsControlChars`, requiring no live provider or network access.
**Test Data Requirements:** (1) captured `--axi` stdout from a fixture review/resume run with normal findings; (2) a synthetic fixture reproducing `osc8()`'s exact byte sequence as a known-bad control case to prove the detector works; (3) a fixture with a crafted CSI/OSC sequence embedded in a finding field.
**Mock/Stub Requirements:** No network/provider mocks required for the core escape-detection helper test; the review/resume-level scenarios may reuse the fixture/test-double setup already established for AC 04-01.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A pinning test asserts zero `\x1b\[`/`\x1b\]` byte matches in captured `--axi` stdout for both the fresh-review and resume paths
- [ ] The pinning test includes a positive-control case reproducing `osc8()`'s exact escape pattern (`quickstart.go:456-461`) and confirms the detector flags it
- [ ] A crafted-input edge case (escape sequence embedded in a finding field) confirms the guarantee holds against untrusted data, not just static human-text strings
- [ ] Test is named/documented in the style of `TestDriftLine_StripsControlChars` for discoverability alongside existing sanitization precedent

**Manual Review:**
- [ ] Code reviewed and approved
