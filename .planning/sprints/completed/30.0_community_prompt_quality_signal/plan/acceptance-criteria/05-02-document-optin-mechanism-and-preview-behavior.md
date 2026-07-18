# Acceptance Criteria: Document the Independent Opt-In Mechanism and `--preview` Behavior

**Related User Story:** [05: Document the Quality-Signal Telemetry Contract](../user-stories/05-document-quality-signal-telemetry-contract.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/telemetry.md`) | Extends the existing "Opt-out" section's OR'd-truth-table pattern for a second, independent gate |
| Test Framework | Manual cross-check against shipped gate/CLI behavior | No executable test suite for prose |
| Key Dependencies | Story 2's gate (`cmd/atcr/qualitysignal.go`, `internal/registry/quality_signal_setting.go`, `cmd/atcr/config.go`) and Story 3's `--preview` flag (`cmd/atcr/flags.go`) | Doc content is derived from these, never invented |

## Related Files
- `docs/telemetry.md` - modify: add a subsection documenting the quality-signal opt-in truth table (env var + `atcr config set quality_signal`) and a subsection documenting `--preview`'s exact behavior
- `cmd/atcr/qualitysignal.go` (Story 2) - reference only: `qualitySignalEnabled`/`qualitySignalGate`'s opt-in (OR-enables) truth table is the source of truth for the doc's truth-table rows
- `internal/registry/quality_signal_setting.go` (Story 2) - reference only: `quality_signal` config key semantics (accepted `strconv.ParseBool` vocabulary, fail-safe-to-disabled on malformed values)
- `cmd/atcr/flags.go` / the command that hosts `--preview` (Story 3) - reference only: confirms the exact flag name, host command(s), and printed output shape (pretty-printed JSON plus an explicit "not sent" marker) the doc must describe

### Related Files (from codebase-discovery.json)

- `docs/telemetry.md` - update: opt-in subsection (env var + `atcr config set quality_signal` truth table, extending the "Opt-out" section's format at `:58`) and `--preview` behavior subsection

## Happy Path Scenarios
**Scenario 1: Doc states the opt-in mechanism and its exact env var / config key names**
- **Given** Story 2 ships the `ATCR_QUALITY_SIGNAL` env var and the `atcr config set quality_signal <bool>` config key
- **When** the new subsection is read
- **Then** it names both surfaces exactly as shipped, with a runnable example for each (mirroring the existing `ATCR_TELEMETRY` / `atcr config set telemetry` examples' style)

**Scenario 2: Doc documents the two-surface opt-in truth table, matching the shipped `qualitySignalEnabled` matrix**
- **Given** Story 2's gate resolves enabled when EITHER the env var explicitly opts in OR the persisted `quality_signal` config is `true`, and disabled only when neither surface has explicitly opted in (six meaningful cells, per Story 02's Assumptions)
- **When** the new subsection is read
- **Then** it contains a truth table (mirroring the existing `ATCR_TELEMETRY` / `telemetry` config truth table's format) showing the same enabled/disabled outcomes for every combination, with a sentence stating there is no precedence/override between the two — and a note that, unlike the opt-OUT usage ping, this gate is opt-IN (either surface opting in is sufficient consent)

**Scenario 3: Doc states the quality-signal gate is independent of the existing `telemetry`/`--sync-cloud` gates**
- **Given** Story 2's Constraints require the quality-signal gate to share no state with `telemetryGate()`/`resolveSyncCloud()`
- **When** the new subsection is read
- **Then** it explicitly states that enabling/disabling `telemetry` or `--sync-cloud` has no effect on the quality-signal gate, and vice versa — using the same "OR'd, no precedence" framing already used for the usage-ping opt-out, applied here as a *cross-feature* independence claim

**Scenario 4: Doc documents `--preview`'s exact behavior**
- **Given** Story 3 ships a `--preview` flag that prints pretty-printed JSON of the payload plus a human-readable "not sent" marker, and never performs network I/O or requires `ATCR_API_KEY`
- **When** the new subsection is read
- **Then** it states: the exact flag name (`--preview`) and host commands (`atcr review` and `atcr reconcile`); that it prints the allowlisted JSON payload; that it includes an explicit "nothing was transmitted" statement; and that it works without any opt-in or credential, so an undecided user can inspect the payload before ever enabling transmission

## Edge Cases
**Edge Case 1: `--preview` combined with `--sync-cloud`**
- **Given** Story 3's AC states `--preview` takes precedence when both flags are passed on the same invocation
- **When** the doc describes `--preview`
- **Then** it notes this precedence explicitly, so a reader does not assume the two flags could both trigger a transmission in the same run

**Edge Case 2: Malformed persisted `quality_signal` config value**
- **Given** Story 2's AC states a malformed persisted value fails safe to disabled rather than silently re-enabling
- **When** the doc describes the config key
- **Then** it states this fail-safe behavior explicitly (mirroring the existing doc's handling of `ATCR_TELEMETRY`'s unparseable-value-fails-open note), so a reader understands a corrupted config can never be interpreted as consent

**Edge Case 3: Config-key naming collision risk with `telemetry`**
- **Given** the existing "Opt-out" section documents the `telemetry` key and this story adds a second key, `quality_signal`, in the same file
- **When** the new subsection is written
- **Then** it does not reuse or overload the existing "Opt-out" heading/table for the new key — it is a clearly separate subsection so a reader cannot mistake the two independent keys for one shared toggle

## Error Conditions
**Error Scenario 1: Doc omits the "no precedence/override" independence statement**
- **Given** a reviewer checks the new subsection against Story 2's independence requirement
- **When** the doc does not explicitly state that `quality_signal` and `telemetry`/`--sync-cloud` are independent
- **Then** this AC is not met — the risk noted in the user story (contradicting or blurring the existing Epic 28.0 documentation) has materialized and must be corrected before merge
- HTTP status / error code: not applicable (documentation-only; failure mode is a review-gate rejection)

## Performance Requirements
- **Response Time:** Not applicable — static documentation.
- **Throughput:** Not applicable.
- **Review latency:** A reviewer must be able to verify the truth table and `--preview` description against the shipped gate function and CLI help output in a single reading pass, without needing to trace through multiple source files simultaneously — achieved by keeping the two subsections co-located and cross-referencing exact file/flag names.

## Security Considerations
- **Sensitive information:** No `ATCR_API_KEY` value, real config file path outside the documented `.atcr/config.yaml` convention, or internal implementation detail beyond what a CLI user needs is disclosed.
- **No invented claims:** The doc must not describe `--preview` as requiring or checking any credential (it must not), and must not claim the opt-in gate shares any state with `telemetry`/`--sync-cloud` (it must not) — both claims are verified against Story 2/3's shipped code and tests, not asserted from the plan-stage design alone.

## Test Implementation Guidance
**Test Type:** MANUAL (doc-accuracy review) + a manual dry run of `atcr config set quality_signal true/false` and `--preview` against the shipped CLI to confirm the doc's described behavior matches actual output
**Test Data Requirements:** A local `.atcr/config.yaml` fixture to exercise the config-key example commands; the shipped `--preview` flag's actual stdout output for comparison against the doc's description.
**Mock/Stub Requirements:** None — manual CLI invocation and prose comparison, no automated test harness for this AC.

## Definition of Done
**Auto-Verified:**
- [ ] Markdown renders without syntax errors (table, headers, links)
- [ ] `go build ./...` and `go test ./...` still pass (no source changed by this story)

**Story-Specific:**
- [ ] New subsection names the exact env var and `atcr config set quality_signal` key as shipped by Story 2
- [ ] Truth table matches `qualitySignalEnabled`'s six-cell matrix exactly
- [ ] Doc explicitly states independence from `telemetry`/`--sync-cloud` (no shared state, no precedence)
- [ ] `--preview`'s exact behavior (payload printed, "not sent" marker, no credential required, precedence over `--sync-cloud`) is documented per Story 3's shipped behavior

**Manual Review:**
- [ ] Code reviewed and approved
