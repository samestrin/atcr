# atcr Reconciled Review

## Summary

- Total findings: 9
- Sources: pool
- Clusters collapsed: 5
- Severity disagreements: 4
- Authority promoted: 4
- Consensus filtered: 2 (uncorroborated singletons routed to the ambiguous sidecar)
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 3 | 0 | 0 |
| MEDIUM | 4 | 0 | 0 |
| LOW | 2 | 0 | 0 |

## Disagreements

Top 10 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. severity_split — `cmd/atcr/config.go:41` (HIGH) · score 4
- Severity disagreement: LOW vs HIGH
- Reviewers: dax, otto (independence 2)
- Problem: (newConfigSetCmd) No tests for runConfigSet error branches (unsupported key, invalid bool, repoRoot, SetTelemetrySetting) and success path

### 2. severity_split — `internal/scorecard/telemetry.go:12` (HIGH) · score 4
- Severity disagreement: MEDIUM vs HIGH
- Reviewers: dax, kai, mira, pace (independence 4)
- Problem: HashPersonaID claims pseudonymization but unsalted SHA-256 over a small enumerable set is dictionary-reversible; the privacy contract is misleading and the Leaderboard cannot claim anonymized aggregation

### 3. severity_split — `internal/scorecard/telemetry.go:31` (MEDIUM) · score 4
- Severity disagreement: LOW vs MEDIUM
- Reviewers: brad, kai, mira, otto (independence 4)
- Problem: (HashPersonaID) HashPersonaID uses unsafe.StringData/unsafe.Slice to avoid a negligible allocation, trading memory safety guarantees for no meaningful performance gain on small strings

### 4. severity_split — `internal/scorecard/telemetry.go:12` (HIGH) · score 3
- Severity disagreement: MEDIUM vs HIGH
- Reviewers: dax, mira, pace (independence 3)
- Problem: Empty string input to HashPersonaID not tested despite unsafe.StringData edge case

### 5. solo_finding — `cmd/atcr/config.go:30` (MEDIUM) · score 2
- Reviewers: mira (independence 1)
- Problem: Long help text warns about ATCR_DISABLE_AST_GROUPING direction but does not document what exit code an I/O failure (missing config file) produces

### 6. gray_zone — `cmd/atcr/flags.go:61` (MEDIUM) · score 2
- Reviewers: greta (independence 1)
- Problem: Warning check uses exact string equality after TrimSpace, so a trailing slash on --cloud-endpoint bypasses the placeholder warning and triggers a redirect-block failure
- Detail: similarity 0.00
- Positions:
  - greta — MEDIUM: Warning check uses exact string equality after TrimSpace, so a trailing slash on --cloud-endpoint bypasses the placeholder warning and triggers a redirect-block failure

### 7. solo_finding — `cmd/atcr/flags_test.go:28` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: (TestAddSyncCloudFlags_RegisteredOnReviewAndReconcile) No test verifies placeholder warning absent when --sync-cloud not set or endpoint overridden

### 8. solo_finding — `internal/scorecard/export_test.go:592` (MEDIUM) · score 2
- Reviewers: kai (independence 1)
- Problem: (TestRunLeaderboardExport_ByteForByteRegression) Byte-for-byte SHA-256 checksum pins the entire serialized output, making the test fail on any formatting, ordering, or whitespace change and obscuring the actual semantic contract

### 9. gray_zone — `cmd/atcr/main_test.go:113` (LOW) · score 1
- Reviewers: bruce (independence 1)
- Problem: TestExitCode does not test authError wrapping a usageError
- Detail: similarity 0.00
- Positions:
  - bruce — LOW: TestExitCode does not test authError wrapping a usageError

### 10. solo_finding — `internal/telemetry/event.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: Event struct has no test for exact JSON field allowlist (privacy regression guard)

## Findings

### HIGH

- `cmd/atcr/config.go:41` — confidence HIGH, reviewers: dax, otto
  - Severity disagreement: LOW vs HIGH
  - Problem: (newConfigSetCmd) No tests for runConfigSet error branches (unsupported key, invalid bool, repoRoot, SetTelemetrySetting) and success path
  - Fix: Change to return &#96;nil&#96; and call &#96;cmd.Help()&#96; or use a dedicated help command
  - Evidence: [otto] &#96;RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }&#96; / [dax] runConfigSet has four error returns and one success output; no config_test.go in diff
- `internal/scorecard/telemetry.go:12` — confidence HIGH, reviewers: dax, mira, pace
  - Severity disagreement: MEDIUM vs HIGH
  - Problem: Empty string input to HashPersonaID not tested despite unsafe.StringData edge case
  - Fix: Validate input or use standard []byte(raw) conversion; the allocation savings are not worth the panic surface in a hash function called on untrusted scorecard data
  - Evidence: [dax] Comment claims safety for empty string but no test confirms / [mira] unsafe.Slice(unsafe.StringData(raw), len(raw)) panics if raw is nil string
- `internal/scorecard/telemetry.go:12` — confidence HIGH, reviewers: dax, kai, mira, pace
  - Severity disagreement: MEDIUM vs HIGH
  - Problem: HashPersonaID claims pseudonymization but unsalted SHA-256 over a small enumerable set is dictionary-reversible; the privacy contract is misleading and the Leaderboard cannot claim anonymized aggregation
  - Fix: Add HMAC-SHA256 with a provisioned, rotatable application secret (server-side keyed hash acceptable per KB entry); add TD-007 tracking label to the function
  - Evidence: [dax] HashPersonaID is a pure function with no tests; NewTelemetryPersonaRecord untested / [mira] SHA-256 without salt or HMAC over enumerable input set (persona names); digest is trivially reversible with a precomputed table / [kai] small, enumerable, often publicly-known set... UNSALTED digest does not defend against a dictionary/rainbow attack / [pace] HashPersonaID(r.Reviewer) is called for every Record in the scorecard, leading to redundant hash computations when the same reviewer appears multiple times (e.g., reviewing multiple models). For a pool of R reviewers and M models per reviewer, this does R*M hash computations instead of R.

### MEDIUM

- `cmd/atcr/config.go:30` — confidence HIGH, reviewers: mira
  - Problem: Long help text warns about ATCR_DISABLE_AST_GROUPING direction but does not document what exit code an I/O failure (missing config file) produces
  - Fix: Add exit-code expectation to the Long doc: &#34;on I/O failure (missing/unwritable .atcr/config.yaml) returns exit 1&#34;
  - Evidence: docs_audit_test.go already audits the Long text for env-var coverage; adding exit-code documentation closes the observability gap
- `cmd/atcr/flags_test.go:28` — confidence HIGH, reviewers: dax
  - Problem: (TestAddSyncCloudFlags_RegisteredOnReviewAndReconcile) No test verifies placeholder warning absent when --sync-cloud not set or endpoint overridden
  - Fix: Add test cases for warning absence to prevent false positive warnings
  - Evidence: TestAddSyncCloudFlags_DefaultEndpointWarns only checks positive case
- `internal/scorecard/export_test.go:592` — confidence HIGH, reviewers: kai
  - Problem: (TestRunLeaderboardExport_ByteForByteRegression) Byte-for-byte SHA-256 checksum pins the entire serialized output, making the test fail on any formatting, ordering, or whitespace change and obscuring the actual semantic contract
  - Fix: Replace checksum with structured assertions against parsed JSON fields
  - Evidence: wantExportChecksum = &#34;96231aeede4bec24132992b35bcf0a5c069619248ad720f319372517ee39625a&#34;
- `internal/scorecard/telemetry.go:31` — confidence HIGH, reviewers: brad, kai, mira, otto
  - Severity disagreement: LOW vs MEDIUM
  - Problem: (HashPersonaID) HashPersonaID uses unsafe.StringData/unsafe.Slice to avoid a negligible allocation, trading memory safety guarantees for no meaningful performance gain on small strings
  - Fix: Use &#96;[]byte(raw)&#96; for readability; the allocation is negligible for persona IDs
  - Evidence: [kai] unsafe.Slice(unsafe.StringData(raw), len(raw)) / [brad] unsafe.StringData(raw) used when raw == &#34;&#34; violates Go spec; sha256.Sum256 copies anyway so zero-copy is illusory / [mira] Comment notes HMAC hardening is deferred; no TD label in source for reconciler to act on / [otto] &#96;unsafe.Slice(unsafe.StringData(raw), len(raw))&#96;

### LOW

- `cmd/atcr/flags.go:39` — confidence HIGH, reviewers: mira, otto
  - Problem: Hardcoded production endpoint URL in binary is a maintenance hazard if the backend migrates
  - Fix: Make the default configurable via build-time ldflags or a well-documented constant; document the migration path in the inline comment
  - Evidence: [mira] const defaultCloudEndpoint = &#34;https://atcr.dev/dashboard&#34; / [otto] &#96;_, _ = fmt.Fprintf(cmd.ErrOrStderr(), &#34;warning: ...&#34;)&#96;
- `internal/telemetry/event.go:1` — confidence HIGH, reviewers: dax
  - Problem: Event struct has no test for exact JSON field allowlist (privacy regression guard)
  - Fix: Add test marshaling Event and asserting keys are exactly [event,lang,lines,status]
  - Evidence: Comment references TestClient_Send_PayloadHasExactlyFourAllowlistedKeys but no such test in diff
