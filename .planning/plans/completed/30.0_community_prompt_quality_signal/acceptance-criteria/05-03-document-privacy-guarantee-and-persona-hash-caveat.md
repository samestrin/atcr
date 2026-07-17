# Acceptance Criteria: State the Absolute No-Code/No-Finding-Content Guarantee and Restate the Persona-Hash Caveat

**Related User Story:** [05: Document the Quality-Signal Telemetry Contract](../user-stories/05-document-quality-signal-telemetry-contract.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/telemetry.md`) | Extends the existing "Persona Leaderboard data" caveat pattern with an explicit, self-contained restatement |
| Test Framework | Manual cross-check against `HashPersonaID` and the epic's privacy constraints | No executable test suite for prose |
| Key Dependencies | `internal/scorecard/telemetry.go`'s `HashPersonaID` (existing TD-007 caveat), Story 1's `QualitySignal` struct shape, Story 4's content-free report guarantee | Doc content restates an existing caveat plus a payload-specific no-content guarantee, never invents new claims |

## Related Files
- `docs/telemetry.md` - modify: add, within the new quality-signal section, an explicit "no code, no finding content, ever" statement and a restated `HashPersonaID` unsalted-hash/dictionary-attack caveat scoped to this payload's persona identifiers
- `internal/scorecard/telemetry.go` - reference only: `HashPersonaID`'s unsalted-SHA-256 implementation is the existing source of truth this section restates (not a new mechanism)
- `internal/telemetry/quality_signal.go` (Story 1) - reference only: the struct's fixed 4-field shape is the structural evidence backing the "no code, no finding content, ever" claim (there is no field capable of carrying it)
- `docs/telemetry.md`'s existing "Persona Leaderboard data" section (same file, lines ~106-128 as of this writing) - reference only: the caveat's original wording and framing, which the new section must be consistent with, not contradict

### Related Files (from codebase-discovery.json)

- `docs/telemetry.md` - update: within the new quality-signal section, the standalone "no code, no finding content, ever" statement and the restated persona-hash caveat (consistent with "Persona Leaderboard data" at `:106-127`)

## Happy Path Scenarios
**Scenario 1: Explicit "no code, no finding content, ever" statement is present in the new section itself**
- **Given** the story's Constraints require the guarantee to be stated in the new section's own text, not only via cross-reference
- **When** the new quality-signal section is read in isolation (without having read the usage-ping or persona-leaderboard sections first)
- **Then** it contains a standalone sentence asserting the payload never carries source code, file paths, file contents, finding/problem/fix text, or prompt content — matching the structural guarantee enforced by `QualitySignal`'s fixed 4-field shape (Story 1) and the report's content-free render path (Story 4)

**Scenario 2: `HashPersonaID` unsalted-hash caveat is restated in this section's own context**
- **Given** the existing "Persona Leaderboard data" section already documents `HashPersonaID`'s unsalted-SHA-256, dictionary-attack caveat (TD-007) for the leaderboard/`--sync-cloud` payload
- **When** the new quality-signal section documents its `persona_id_hash` field
- **Then** it restates — not merely links to — the same caveat: deterministic, one-way, pseudonymous-not-anonymous, and vulnerable to a pre-computed dictionary attack against the small, often-public set of persona names

**Scenario 3: New section is consistent with, not contradictory to, the existing caveat's wording**
- **Given** the story's Constraints forbid contradicting the existing Epic 28.0 documentation
- **When** a reviewer compares the new caveat restatement against the original in "Persona Leaderboard data"
- **Then** the two describe the same mechanism identically (same hash algorithm, same pseudonymous-not-anonymous framing) — the restatement is additive/duplicative for standalone readability, never a divergent or weakened description

## Edge Cases
**Edge Case 1: Reader reaches the quality-signal section directly (e.g. via a deep link) without reading "Persona Leaderboard data" first**
- **Given** the story's Constraints explicitly anticipate this reading order
- **When** such a reader reads only the new section
- **Then** they still learn the full pseudonymity caveat — the new section's restatement is self-sufficient and does not rely on the reader having seen the earlier section

**Edge Case 2: `TD-007` reference is identifiable, not just a bare code**
- **Given** the caveat is tied to a tracked technical-debt item (TD-007)
- **When** the new section restates the caveat
- **Then** it either names TD-007 in a way consistent with the existing section's citation style, or omits the bare ID if the existing section itself does not surface it as a reader-facing citation — consistency with the existing section's actual citation style takes precedence over introducing a new citation convention

## Error Conditions
**Error Scenario 1: New section states or implies the payload is anonymous rather than pseudonymous**
- **Given** the existing caveat's core point is "pseudonymous, not anonymous"
- **When** the new section's restatement is reviewed
- **Then** any wording that drops this distinction (e.g. calling the payload "fully anonymous") fails this AC — the restatement must preserve the exact privacy posture, not a softened version
- HTTP status / error code: not applicable (documentation-only; failure mode is a review-gate rejection)

**Error Scenario 2: No-code/no-finding-content guarantee is asserted without structural backing**
- **Given** the guarantee must reflect what the shipped `QualitySignal` struct and Story 4's report render path actually enforce
- **When** a reviewer checks the claim against `internal/telemetry/quality_signal.go`'s field set
- **Then** if the struct (as shipped) contains any field capable of carrying code, file paths, or finding text, the doc's guarantee is false and must not be published as-is — this AC requires the claim to be re-verified against shipped Story 1/4 code immediately before finalizing, not asserted from the plan-stage design alone

## Performance Requirements
- **Response Time:** Not applicable — static documentation.
- **Throughput:** Not applicable.
- **Review latency:** A privacy-conscious reader must be able to find and read the complete no-code/no-finding-content guarantee and the persona-hash caveat within the new section alone, without needing to jump to another section, per the story's core "So that" goal (verify the privacy contract by reading documentation alone).

## Security Considerations
- **Sensitive information:** The restated caveat must not include any real persona name, hash digest, or dictionary-attack methodology detail beyond what the existing "Persona Leaderboard data" section already discloses (no new attack surface is introduced by restating an existing, already-public caveat).
- **No invented claims:** The "no code, no finding content, ever" guarantee must be scoped exactly to what `QualitySignal`'s allowlist and the locking regression test (Story 1) plus the report's static import guard (Story 4, AC 04-02) actually enforce — the doc must not claim a stronger guarantee (e.g. "cryptographically impossible to leak") than the structural allowlist + test actually provide.

## Test Implementation Guidance
**Test Type:** MANUAL (doc-accuracy and consistency review)
**Test Data Requirements:** Side-by-side reading of the existing "Persona Leaderboard data" section and the new quality-signal section's caveat restatement; the shipped `internal/scorecard/telemetry.go` `HashPersonaID` implementation for wording accuracy.
**Mock/Stub Requirements:** None — this is a prose-consistency and structural-claim verification, not an executable test.

## Definition of Done
**Auto-Verified:**
- [ ] Markdown renders without syntax errors
- [ ] `go build ./...` and `go test ./...` still pass (no source changed by this story)

**Story-Specific:**
- [ ] New section contains a standalone "no code, no finding content, ever" sentence, readable without cross-referencing another section
- [ ] `HashPersonaID` unsalted-hash/dictionary-attack (TD-007) caveat is restated in full within the new section, consistent with the existing "Persona Leaderboard data" wording
- [ ] Restated caveat preserves the "pseudonymous, not anonymous" distinction exactly — no softened or contradictory wording
- [ ] Guarantee claims are checked against Story 1's shipped struct and Story 4's shipped report import guard before finalizing

**Manual Review:**
- [ ] Code reviewed and approved
