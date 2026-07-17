 The user wants me to review a code diff (and some planning documents) as "kai", the architecture and design reviewer. I need to look at the changed regions and emit findings in a specific pipe-delimited format.

Let me analyze the diff carefully.

First, the scope constraint says to focus on changes related to the sprint/epic plan: "Epic Plan 28.0: Telemetry Expansion & Cloud Sync". The diff includes:
1. Planning documents (knowledge base, state, epic refinements, sprint docs)
2. Code changes:
   - cmd/atcr/config.go (new)
   - cmd/atcr/docs_audit_test.go (added tests)
   - cmd/atcr/flags.go (added sync cloud flags)
   - cmd/atcr/flags_test.go (new tests)
   - cmd/atcr/main_test.go (updated subcommand count, exit codes)
   - docs/README.md (added telemetry link)
   - docs/scorecard.md (added telemetry section)
   - internal/registry/project_test.go (added telemetry config test)
   - internal/scorecard/export_test.go (added byte-for-byte regression test)
   - internal/scorecard/telemetry.go (new)
   - internal/telemetry/event.go (new)
   - skill/SKILL.md (updated command list)

Let me look for architecture/design issues:

1. **cmd/atcr/config.go**:
   - `runConfigSet` calls `registry.SetTelemetrySetting(root, enabled)`. This introduces a dependency from `cmd/atcr` to `internal/registry`. The registry package is mentioned in components touched. Is this coupling okay? `internal/registry` already handles config. Seems reasonable.
   - The function `repoRoot()` is used but not defined in the visible diff. It might exist elsewhere.
   - `usageArgs` wrapper is used.
   - The config command only supports "telemetry". This is deliberately scoped, but it's a bit of an extensibility trap if they want to add more config keys later - but that's minor.
   - Wait, the `registry` import is from `github.com/samestrin/atcr/internal/registry`. This seems fine.

2. **cmd/atcr/flags.go**:
   - `addSyncCloudFlags` adds flags and chains PreRunE.
   - The PreRunE chaining pattern: it calls `prev` first, then checks if `sync-cloud` is set. This is okay.
   - However, there's a subtle issue: `boolFlag(cmd, "sync-cloud")` - where is `boolFlag` defined? Not in the diff. Probably existing code.
   - The warning about default endpoint is emitted to stderr. That's fine.
   - `defaultCloudEndpoint` is a global const pointing to HTTPS. Okay.

3. **cmd/atcr/flags_test.go**:
   - Tests look reasonable. No major issues.

4. **cmd/atcr/main_test.go**:
   - Added `authError` cases to `TestExitCode`. This suggests there's an `authError` function/type. The exit code is 3 (`exitAuth`).
   - This is fine.

5. **internal/scorecard/telemetry.go**:
   - This is the most interesting file architecturally.
   - `HashPersonaID` uses `unsafe.Slice(unsafe.StringData(raw), len(raw))` to avoid allocations. This is a performance optimization. Is it safe?
     - The comment says "unsafe.Slice is safe for any pointer when len == 0". That's true.
     - For non-empty strings, `unsafe.StringData` returns a pointer to the string's bytes. The string is immutable, and `sha256.Sum256` only reads. So this should be safe.
     - However, relying on `unsafe` for a simple hash of a persona ID is over-engineering. Persona IDs are small strings; the allocation cost of `[]byte(raw)` is negligible. Using `unsafe` introduces a potential safety/review burden for marginal gain. But is it an architecture/design issue? Maybe LOW or MEDIUM - unnecessary unsafe code.
   - The comment acknowledges it's "deliberately NOT part of the Epic 10.0 PublicRecord allowlist / scrubField export path". Good separation.
   - The type `TelemetryPersonaRecord` is separate from `PublicRecord`. Good.
   - Wait, the knowledge base document (kb-2026-07-16-136124.md) says: "HashPersonaID (internal/scorecard/telemetry.go:18) is a bare, unsalted hex SHA-256 of the persona name — dictionary/rainbow-table reversible against the small, often-public set of persona names (TD-007, HIGH)." and explains why HMAC wasn't used (shared secret in binary would be extractable).
   - This is a known issue captured in tech debt. But the code is still going in as plain SHA-256. Is this an architecture problem?
   - The sprint plan says: "Persona ID hashing added to the scorecard export schema". The knowledge base admits it's reversible via dictionary attack. This seems like a design flaw: the hashing doesn't actually provide pseudonymization against a motivated attacker, which undermines the privacy model. But it's documented and deferred. Since the prompt asks me to "Find problems the author would prefer you didn't", this is worth flagging. However, the knowledge base explicitly documents this as a deferred decision. Is it in scope? The file changed is `internal/scorecard/telemetry.go`.
   - The category would be something like `privacy` or `design`. Severity? The knowledge base says TD-007, HIGH. But it's intentional and documented. Still, from an architecture review perspective, shipping a hash that's trivially reversible is a weak contract. I'll note it as HIGH.

6. **internal/scorecard/export_test.go**:
   - `TestRunLeaderboardExport_ByteForByteRegression` uses a hardcoded SHA-256 checksum `wantExportChecksum` to verify the export hasn't changed.
   - This is an architecture issue: byte-for-byte regression tests are extremely brittle. Any change to export formatting (whitespace, field ordering, time formatting) will break this test, even if the semantic output is correct. It creates a "duplication of responsibility" or at least a fragile contract. The test asserts on a checksum instead of the structure.
   - Also, it imports `crypto/sha256` and `encoding/hex` just for this checksum.
   - This is an extensibility trap / brittleness issue. MEDIUM or HIGH? The comment says "safety net", but checksum-based regression tests are notoriously brittle. If someone changes the JSON indent or key order, the test fails. This forces future changes to update the checksum rather than understanding the semantics. I'll call it MEDIUM.

7. **internal/telemetry/event.go**:
   - Simple struct with 4 fields. Fine.

8. **internal/registry/project_test.go**:
   - Test checks that default config YAML contains "telemetry:". Fine.

9. **Docs changes**:
   - `docs/scorecard.md` adds a section explaining the separate data paths. This is good architectural documentation.

10. **Boundary/Coupling issues**:
    - `cmd/atcr/config.go` imports `internal/registry` and calls `registry.SetTelemetrySetting`. Is `internal/registry` the right place for telemetry settings? The registry package handles project config. The sprint plan components touched includes `internal/scorecard`, `internal/telemetry`, `cmd/atcr`, `docs/`. It doesn't list `internal/registry` as a touched component, but the code changes in `internal/registry/project_test.go` indicate it was touched. The plan refinements mention adding files to components touched but didn't mention `internal/registry`. However, `registry.SetTelemetrySetting` implies there are registry changes not shown in the diff (or the method was added). The diff only shows the test file for registry.
    - Wait, I only see `internal/registry/project_test.go` in the diff. There must be corresponding changes to `internal/registry` package (like `SetTelemetrySetting` and `DefaultProjectConfigYAML` returning telemetry config). These are not in the diff. The diff only shows the test. But the test verifies `DefaultProjectConfigYAML` documents telemetry. So there are unseen changes in `internal/registry` that this test relies on. Since I can only review changed regions, and `internal/registry/project_test.go` is changed, I can comment on what the test implies.
    - Actually, the test `TestDefaultProjectConfigYAML_DocumentsTelemetry` calls `DefaultProjectConfigYAML` and asserts it contains "telemetry:". This implies the config generation was updated. But since the implementation isn't in the diff, I can't review it directly. However, if the implementation of `SetTelemetrySetting` or config loading isn't shown, I can't flag issues there unless they are visible in the diff.

Let me re-read the scope constraint: "Review only the changed regions. The payload shows you the change in context, but a finding whose FILE:LINE falls outside the changed lines will be discarded before it reaches the report when grounding is active (git-range reviews)".

So I must ensure all my FILE:LINE references fall within the changed lines of the diff.

Let me map out the changed lines for each file:

- `cmd/atcr/config.go`: Lines 1-78 (entire file new)
- `cmd/atcr/docs_audit_test.go`: Added lines around 596+
- `cmd/atcr/flags.go`: Lines 2-71 (modified, with new imports and new functions)
- `cmd/atcr/flags_test.go`: Lines 1-58 (new file)
- `cmd/atcr/main_test.go`: Lines around 43-140 (modified subcommand count, added auth tests)
- `docs/README.md`: Lines around 5-66 (added telemetry to list)
- `docs/scorecard.md`: Lines around 319+ (added new section)
- `internal/registry/project_test.go`: Lines around 66-80 (added test)
- `internal/scorecard/export_test.go`: Lines 1-21 (imports changed), lines 581+ (new test)
- `internal/scorecard/telemetry.go`: Lines 1-67 (new file)
- `internal/telemetry/event.go`: Lines 1-13 (new file)
- `skill/SKILL.md`: Lines around 55+ (added config command)

Now, let me think about specific findings:

**Finding 1: HashPersonaID uses unsafe for negligible gain**
- SEVERITY: MEDIUM
- FILE:LINE: `internal/scorecard/telemetry.go:31` (the unsafe.Slice line) or maybe line 25 (the function). Let's pick line 31.
- PROBLEM: `HashPersonaID` uses unsafe.StringData/unsafe.Slice to avoid a tiny allocation, trading safety guarantees for negligible performance benefit on small persona strings
- FIX: Replace with standard []byte(raw) copy; remove unsafe import
- CATEGORY: safety
- EST_MINUTES: 10
- EVIDENCE: `sum := sha256.Sum256(unsafe.Slice(unsafe.StringData(raw), len(raw)))`

**Finding 2: Byte-for-byte regression test is brittle**
- SEVERITY: MEDIUM
- FILE:LINE: `internal/scorecard/export_test.go:592` (the test function line) or line 600 (the const). Let's use line 592 (func line).
- PROBLEM: Byte-for-byte SHA-256 checksum regression test forces silent coupling between unrelated formatting changes and test failures, obscuring semantic regressions with cryptographic noise
- FIX: Assert on parsed JSON structure/schema rather than hex checksum; keep a fixture file if exact bytes matter
- CATEGORY: extensibility
- EST_MINUTES: 30
- EVIDENCE: `const wantExportChecksum = "96231aeede4bec24132992b35bcf0a5c069619248ad720f319372517ee39625a"`

**Finding 3: Plain SHA-256 is dictionary-reversible for persona IDs**
- SEVERITY: HIGH
- FILE:LINE: `internal/scorecard/telemetry.go:19` (the function comment/doc) or line 32 (the hash). Let's use line 19 or 25. Actually, the knowledge base admits this. But it's a real design flaw. The code itself documents the vulnerability. Should I flag it? The prompt says "Find problems the author would prefer you didn't." This is documented as a known issue, but it's still a design flaw being introduced. I think it's valid to flag as HIGH because it violates the privacy intent of pseudonymization.
- PROBLEM: Unsalted SHA-256 of a small enumerable persona set is trivially reversible via rainbow table, pseudonymizing in name only
- FIX: Use a keyed HMAC with a pepper or accept that persona IDs are public; do not claim pseudonymization
- CATEGORY: privacy
- EST_MINUTES: 120
- EVIDENCE: `Guarantee and its bound: SHA-256 is a one-way... small, enumerable... UNSALTED digest does not defend against a dictionary/rainbow attack`

Wait, the knowledge base says this is deferred. But the code comment also says it. Is it fair game? Yes. The reviewer should flag it. The fact that they documented the vulnerability doesn't mean it's not a finding. I'll include it.

**Finding 4: addSyncCloudFlags PreRunE warning bypasses validation chain**
Wait, looking at `cmd/atcr/flags.go:62-68`:
```go
if boolFlag(cmd, "sync-cloud") {
    endpoint, _ := cmd.Flags().GetString("cloud-endpoint")
    if strings.TrimSpace(endpoint) == defaultCloudEndpoint {
        _, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: --cloud-endpoint default %q is a placeholder; --sync-cloud will not work until a real endpoint and ATCR_API_KEY are configured\n", defaultCloudEndpoint)
    }
}
```
This runs during PreRunE. It ignores errors from `GetString`. That's fine. But the warning is emitted in PreRunE, which runs before the command. Is there any issue? Not really.

**Finding 5: Config command couples CLI directly to registry config implementation**
- `cmd/atcr/config.go` imports `internal/registry`. But this is normal for a CLI. Not a strong finding.

**Finding 6: Event struct lacks versioning**
- `internal/telemetry/event.go`: The Event struct has no schema version field. If the backend expects a versioned envelope, this could be a contract issue. But the plan doesn't mention versioning for telemetry events. The existing `PublicRecord` has schema versioning. telemetry event doesn't.
- Is this a problem? The sprint plan says "anonymous background ping". But for a cloud sync payload, versioning is usually important. The Event struct is specifically for telemetry (the ping), not the cloud sync payload.
- Actually, the Event struct is just 4 fields. No version. This might be fine for a minimal v1, but it's an extensibility trap if they need to evolve the schema. However, the AC says "Schema-Constrained Payload (No Source Code or File Paths)". No mention of versioning. I'll skip this as it's speculative.

**Finding 7: `resolveSyncCloud` not shown in diff**
The `flags.go` comment mentions "validated at run time (resolveSyncCloud), not at flag-parse time". But `resolveSyncCloud` is not in the diff. So I can't review it.

**Finding 8: The `config` command is a thin wrapper that will grow**
- The `config` command only supports one key. The comment says "deliberately scoped to that one key". This is fine.

**Finding 9: docs_audit_test.go uses configSetLong helper that does a linear search**
- `configSetLong` in `docs_audit_test.go` iterates commands. That's fine.

**Finding 10: TestAddSyncCloudFlags_PreservesPriorPreRunE uses newReviewCmd**
- The test `TestAddSyncCloudFlags_PreservesPriorPreRunE` calls `newReviewCmd()`. It doesn't test `newReconcileCmd()`. But that's minor.

**Finding 11: unsafe.StringData requires Go 1.20+**
- `unsafe.StringData` was added in Go 1.20. The project likely uses a recent Go version, given the use of `errors.Join` in main_test.go (Go 1.20). So this is fine.

**Finding 12: `TelemetryPersonaRecord` duplicates model field semantics**
- `TelemetryPersonaRecord` carries `Model` unhashed. The comment says model is non-PII. But `PublicRecord` also carries model (or not?). The export path scrubs model? Wait, the scorecard export scrubs model? Let me check the export test. In `export_test.go`, `exportRec` takes name and model. The `PublicRecord` allowlist in the original export might not include model. But `TelemetryPersonaRecord` includes it. This is fine since it's a separate schema.

**Finding 13: Sprint plan vs code mismatch**
The sprint plan says under Payload Formatting: "Update `internal/scorecard/export.go` to conditionally bypass the `scrubField` logic for Persona IDs, replacing them with cryptographic hashes when telemetry is active."
But the actual code introduces `internal/scorecard/telemetry.go` with a completely separate type `TelemetryPersonaRecord` and does NOT modify `export.go`. This is actually BETTER architecture than what the plan specified (which suggested bypassing scrubField). The code correctly avoids bypassing the privacy boundary. So this is a positive, not a negative. The knowledge base also praises this.

But wait, the plan's refinement section says: "Architecture notes: The codebase strictly protects user privacy by scrubbing models and personas in export.go. Emitting a Persona ID (even hashed) contradicts the explicit design of scrubField in export.go. This requires careful separation between 'public submission' schema and the new 'cloud sync' payload."
The code did this separation. Good.

Let me reconsider the unsafe code. Is it really worth flagging? It's a code smell, but from an architecture perspective, it's a localized implementation detail. The prompt asks about boundaries, coupling, contracts, duplication, extensibility. The unsafe usage is more of a correctness/safety issue. I'll include it as LOW or MEDIUM.

Actually, the most architectural issue I see is the byte-for-byte checksum test. That's a maintainability/extensibility trap.

Another issue: The `Event` struct and `TelemetryPersonaRecord` are in separate packages (`internal/telemetry` vs `internal/scorecard`). The `TelemetryPersonaRecord` is in `scorecard` package. Is there a reason `Event` is in telemetry but `TelemetryPersonaRecord` is in scorecard? It might be because `TelemetryPersonaRecord` is built from `scorecard.Record`. But the cloud sync push likely needs to construct a payload. If `--sync-cloud` logic in `cmd/atcr` needs to assemble the payload, it will import `internal/scorecard` for `TelemetryPersonaRecord` and `internal/telemetry` for the ping. That's fine.

Wait, looking at the diff, there's no `cmd/atcr` change for the actual sync-cloud execution logic (like `runReview` or `runReconcile`). The knowledge base mentions `runReview` registers deferred push. But the actual code changes for that aren't in the diff. The diff only shows flag registration, not the runtime logic. This is interesting - it means the actual cloud sync and telemetry call sites are NOT in this diff. The diff is mostly flags, tests, and the hashing library.

So I should focus my findings on what IS in the diff.

Let me look at `cmd/atcr/config.go` again.
- `runConfigSet` resolves `repoRoot()`. Where does that come from? It's not in the diff. But it's used.
- The command structure: `newConfigCmd` follows `newDebtCmd` pattern. That's fine.

Another finding: `cmd/atcr/config.go` hardcodes the key "telemetry" in three places: the error message, the parsing, and the registry call. This is a minor duplication but acceptable for a single-key command.

Wait, I see something in `cmd/atcr/flags.go`:
```go
func addSyncCloudFlags(cmd *cobra.Command) {
    cmd.Flags().Bool("sync-cloud", false, "after the run, push the anonymized scorecard to the cloud dashboard (requires ATCR_API_KEY)")
    cmd.Flags().String("cloud-endpoint", defaultCloudEndpoint, "override the --sync-cloud destination (https://, or loopback http:// for local testing)")
```
There is no validation that `cloud-endpoint` is actually a valid URL. The comment says well-formedness is validated at run time. That's fine.

But wait - the constant `defaultCloudEndpoint` is an HTTPS URL. The comment says "Tests point --cloud-endpoint at an httptest server instead (loopback http is permitted for that; see scorecard.ValidateCloudEndpoint)". This references `scorecard.ValidateCloudEndpoint`, but that function is not in the diff. However, the comment in flags.go creates a cross-package documentation dependency. If `scorecard.ValidateCloudEndpoint` doesn't exist or moves, this comment becomes a lie. But that's minor.

Let's look at `internal/scorecard/telemetry.go` more carefully:
```go
type TelemetryPersonaRecord struct {
    PersonaIDHash string `json:"persona_id_hash"`
    Model         string `json:"model"`
}
```
This struct is specifically for telemetry/cloud-sync. But it's in the `scorecard` package. Should it be in `internal/telemetry`? The plan says `internal/telemetry` is the NEW package for background pings. The scorecard package owns the data types. Putting `TelemetryPersonaRecord` in scorecard means telemetry package depends on scorecard, or scorecard depends on telemetry. Since `NewTelemetryPersonaRecord` takes a `scorecard.Record`, it's natural to keep it in scorecard to avoid import cycles (scorecard can't import telemetry if telemetry imports scorecard). But actually, telemetry could import scorecard. If `TelemetryPersonaRecord` were in `internal/telemetry`, it would need to import `internal/scorecard` for the `Record` type. That's fine. Having it in `scorecard` means the telemetry event package is just the ping, and scorecard package grows to include telemetry-specific record types. This might be a boundary issue: the scorecard package now knows about telemetry schema. But it's a small struct. I'll skip this as too minor.

What about the test `TestDocsAudit_ATCRTelemetryEnvVarCoverage`? It asserts that the help text contains specific strings. This is a docs-audit pattern used elsewhere. Fine.

What about `TestDocsAudit_ConfigSetTelemetryFlagCoverage`? Same.

What about `cmd/atcr/main_test.go`? The test `TestRootCmd_HasExactlyTwentyThreeSubcommands` is altered from 22 to 23. This is a brittle pattern (counting exact subcommands), but it's pre-existing. The changed lines show the update. Is it an architecture issue? It's a maintenance burden, but pre-existing in the sense that the pattern existed before. The change just updates the count. I could flag it as out-of-scope, but the changed lines are the count and the list. However, this is more of a test design issue than architecture. I'll skip it.

Let me reconsider the SHA-256 dictionary attack. The code explicitly documents this vulnerability and says it's deferred. Is it the author's intent to ship this? Yes, with the justification that a pepper would need to be shared in the binary. From an architecture perspective, shipping a privacy feature that is known to be ineffective against trivial attacks is a design flaw. The correct fix mentioned is server-side keyed hash. But they're shipping client-side plain SHA-256. This is a valid HIGH finding.

Wait, is there any other coupling issue?

In `cmd/atcr/config.go`:
```go
if err := registry.SetTelemetrySetting(root, enabled); err != nil {
    return err
}
```
This returns a raw error from registry. If `registry.SetTelemetrySetting` returns an error, is it a usageError or a regular error? The comment says "An I/O failure... is an environment error (exit 1), NOT a usage mistake". But if `registry.SetTelemetrySetting` returns a usageError for some reason (e.g., invalid value), it would propagate as exit 2. The config command only calls it with a parsed boolean, so SetTelemetrySetting probably only returns I/O errors. But this couples the error taxonomy of `cmd/atcr` to the implementation details of `registry`. Not a major issue.

Also in `cmd/atcr/config.go`, `repoRoot()` is used. If `repoRoot()` returns an error, it's returned directly. Is `repoRoot()` defined in the same package? Probably. Not visible.

Let's think about extensibility traps:
- The `config set` command only allows "telemetry". If they want to add keys, they need to modify the allowlist in `runConfigSet`. This is a small extensibility trap but explicitly documented as deliberate. I'll skip.
- The `Event` struct in `internal/telemetry/event.go` has no `json:"-"` or omitempty, which is deliberate ("no omitempty tags"). But also no version. If they need to add fields later, the backend won't know the schema version. This is a telemetry event though, so version might be overkill. But the cloud sync payload (which presumably uses `TelemetryPersonaRecord`) also has no version. That's more of a concern for an API payload. However, `TelemetryPersonaRecord` is just a fragment, not the full sync payload.

Actually, the diff doesn't show the cloud sync payload struct or the client. So I can't review that.

What about `internal/scorecard/export_test.go` importing `crypto/sha256` and `encoding/hex` just for the regression test checksum? That's a test-only dependency. Fine.

What about `internal/scorecard/telemetry.go` importing `unsafe`? This is a standard library package, but it introduces unsafe code into a package that otherwise probably doesn't need it. Given the rationale in the code comment, it's a performance optimization. But is it worth it? The HASH is called per record. How many records? Probably few. The unsafe import is a red flag for security reviewers. I'll flag it as LOW or MEDIUM.

Let me look for circular knowledge or boundary violations:
- `internal/scorecard` now imports `unsafe`. No boundary violation.
- `cmd/atcr` imports `internal/registry` and `internal/scorecard` (via tests). That's normal.
- `internal/telemetry` imports nothing from the project (only standard lib, though not shown). The Event struct is self-contained. That's good.

Wait, `docs/scorecard.md` says: "These use a different schema from the `--export` record and are documented in full... in docs/telemetry.md". But `docs/telemetry.md` is not in the diff! The diff only shows changes to `docs/README.md` and `docs/scorecard.md`. The planning docs say docs are part of the components touched, but the actual `telemetry.md` file is not in the diff. This means either it was already committed, or it's not in this commit. But the finding must be on changed lines. I can't flag a missing file.

However, `docs/README.md` adds a link to `telemetry.md`. If `telemetry.md` doesn't exist, that's a broken link. But it's not in the diff, so I don't know if it exists. The `docs_audit_test.go` might check for it, but that's not shown either. I'll skip.

Let me check `cmd/atcr/flags.go` again for contract issues:
- The PreRunE chaining: `addSyncCloudFlags` chains after `prev`. But if `prev` returns an error, it returns early. Then if `sync-cloud` is set, it checks the endpoint. This seems correct.
- However, the `addRangeFlags` also chains PreRunE. If both are called, the order matters. `newReviewCmd` presumably calls `addRangeFlags` then `addSyncCloudFlags`. So `addSyncCloudFlags`'s PreRunE wraps `addRangeFlags`'s PreRunE. The test `TestAddSyncCloudFlags_PreservesPriorPreRunE` verifies this. Good.

Wait, I found something in `cmd/atcr/flags.go:58-60`:
```go
prev := cmd.PreRunE
cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
    if prev != nil {
        if err := prev(cmd, args); err != nil {
            return err
        }
    }
```
This chaining pattern is duplicated from `addRangeFlags`. That's duplication of responsibility. If the chaining logic changes (e.g., switching to PostRunE or different error handling), both places need to be updated. But `addRangeFlags` is existing code, and `addSyncCloudFlags` is new. The new code replicates the pattern. This isn't a severe issue, but it could be abstracted. Not worth a finding probably.

What about `cmd/atcr/config.go:69-72`:
```go
if err := registry.SetTelemetrySetting(root, enabled); err != nil {
    // An I/O failure (missing/unwritable file) is an environment error (exit 1), NOT a usage mistake — config set never silently creates the file.
    return err
}
```
This trusts that `registry.SetTelemetrySetting` never returns a `usageError`. If it does, the exit code would be wrong. The comment assumes the contract. Is this a contract design issue? The boundary between cmd and registry regarding error taxonomy is ambiguous. But this is speculative unless I see `SetTelemetrySetting`.

Let me think about what the panel's architect would care about:
1. **Boundary violations**: The telemetry event is isolated. Good. The PersonaRecord is in scorecard but separate from PublicRecord. Good.
2. **Coupling**: `cmd/atcr/config.go` couples to `registry` for a simple config write. Normal.
3. **Contract design**: `Event` struct is rigid (exactly 4 fields). Good for privacy. `HashPersonaID` contract claims pseudonymization but admits it's reversible. Bad contract.
4. **Duplication of responsibility**: The byte-for-byte checksum test duplicates the export contract in a non-human-readable way.
5. **Extensibility traps**: The hardcoded checksum. The single-key config command is deliberate.

I think my strongest findings are:
1. **HIGH**: The SHA-256 hash is not pseudonymous against enumerated persona sets (privacy contract is misleading).
2. **MEDIUM**: The byte-for-byte checksum regression test is brittle and obscures semantic intent.
3. **MEDIUM** or **LOW**: The `unsafe` optimization in `HashPersonaID` is unnecessary and introduces a safety hazard for negligible gain.

Are there any other issues?

Looking at `cmd/atcr/flags_test.go`:
`TestAddSyncCloudFlags_DefaultEndpointWarns` parses flags and calls PreRunE. It sets `cmd.SetErr(&buf)`. But does it also need to set output? No.

Looking at `cmd/atcr/main_test.go`:
The new test `TestExitAuth_ResolvesToThree` hardcodes that `authError` resolves to 3. It asserts `exitAuth` equals 3. This is fine.

Wait, the `CHANGELOG.md` mentions: "Cloud-sync client now blocks redirect-following so the Bearer API key is never forwarded to a redirect target, and redacts endpoint userinfo credentials from transport-error messages." This is good security practice. The code for this is not in the diff, but the changelog entry is. I can't review the code.

Actually, I should check if any of the planning documents contain architecture issues that affect the code. For example, the knowledge base `kb-2026-07-16-e5c95f.md` describes a bug fix in `runReview` where deferred cloud push was sequenced incorrectly. But the actual code changes for `runReview` are not in this diff. So I can't flag it.

The scope says: "Constrain your findings to files and changes directly related to these work items. Suppress findings for unrelated changes...". The planning document changes (epic refinements, etc.) are related to the work items but are documentation/process. Do they contain architecture issues? The refinements mention scope-guard violations ( exceeding task/component limits), but those are process issues, not code architecture. I should focus on the code.

Let me refine my findings to exactly the strongest ones:

**Finding 1: Privacy contract is misleading**
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE
HIGH|internal/scorecard/telemetry.go:19|HashPersonaID claims pseudonymization but unsalted SHA-256 over a small enumerable set is dictionary-reversible; the privacy contract is misleading and the Leaderboard cannot claim anonymized aggregation|Move to server-side keyed hashing or stop claiming pseudonymization in the contract|privacy|120|small, enumerable, often publicly-known set... UNSALTED digest does not defend against a dictionary/rainbow attack

Wait, line 19 is the comment block. The actual guarantee text is around there. Let me pick a specific line. The comment starts at line 19. The specific sentence is around line 28-30. I'll use line 19 as the function start.

Actually, line 19 is `// HashPersonaID returns...`. The vulnerability explanation is lines 26-30. Let me pick line 26: `// Guarantee and its bound:...` which is around there. Let's look at the line numbers in the diff:

```go
// HashPersonaID returns the lowercase hex SHA-256 digest of raw, pseudonymizing a
// raw Persona ID for the separate telemetry / cloud-sync Persona Leaderboard schema.
...
// Guarantee and its bound: SHA-256 is a one-way (preimage-resistant) hash, so a
// digest is not directly reversible. But Persona IDs are a small, enumerable,
// often publicly-known set (community-registry persona names), so this UNSALTED
// digest does not defend against a dictionary/rainbow attack that pre-hashes known
```

In the diff, this starts around line 19-29. I'll reference line 19.

**Finding 2: Brittle byte-for-byte checksum test**
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE
MEDIUM|internal/scorecard/export_test.go:592|Byte-for-byte SHA-256 checksum pins the entire serialized output, making the test fail on any formatting, ordering, or whitespace change and obscuring the actual semantic contract|Replace checksum with structured assertions against parsed JSON fields|extensibility|30|wantExportChecksum = "96231aeede4bec24132992b35bcf0a5c069619248ad720f319372517ee39625a"

**Finding 3: Unnecessary unsafe code**
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE
LOW|internal/scorecard/telemetry.go:31|HashPersonaID uses unsafe.StringData/unsafe.Slice to avoid a negligible allocation, trading memory safety guarantees for no meaningful performance gain on small strings|Replace with idiomatic []byte(raw) and remove unsafe import|safety|10|unsafe.Slice(unsafe.StringData(raw), len(raw))

Are there any other issues?

Wait, looking at `cmd/atcr/config.go`:
```go
func runConfigSet(cmd *cobra.Command, args []string) error {
    key, raw := args[0], args[1]
    if key != "telemetry" {
        return usageError(fmt.Errorf("unsupported config key %q: only \"telemetry\" is supported", key))
    }
    enabled, err := strconv.ParseBool(raw)
    if err != nil {
        return usageError(fmt.Errorf("invalid value %q for telemetry: must be a boolean (true/false/1/0)", raw))
    }
    root, err := repoRoot()
    if err != nil {
        return err
    }
    if err := registry.SetTelemetrySetting(root, enabled); err != nil {
        return err
    }
```

The `registry.SetTelemetrySetting` is called with `enabled`. But the comment in the Long help says:
"Note the inverse boolean direction versus ATCR_DISABLE_AST_GROUPING: ATCR_TELEMETRY names the ENABLED state directly, so `ATCR_TELEMETRY=0` (not `=1`) disables telemetry. Setting `telemetry true` here re-enables it."

So the config stores `telemetry: true/false` where true means enabled. And the env var `ATCR_TELEMETRY=0` means disabled. The registry function `SetTelemetrySetting(root, enabled)` presumably sets a field. But what does `DefaultProjectConfigYAML` output? The test `TestDefaultProjectConfigYAML_DocumentsTelemetry` checks `out` contains `"telemetry:"`. If the default is `telemetry: true` (enabled by default), that's fine. If it's commented out, the test still passes because it contains the string. No issue.

But wait, there's a potential contract issue: `ATCR_TELEMETRY=0` disables. `atcr config set telemetry false` disables. They are OR'd. But what if the config file has `telemetry: true` and the env is unset? It should be enabled. What if config file has `telemetry: false` and env is `ATCR_TELEMETRY=1`? The env says enabled (since it names the enabled state directly). But the config says false. They are OR'd with disabled-always-wins. So `false` OR `false` (because env is 1/ true? Wait, ATCR_TELEMETRY=1 means enabled, ATCR_TELEMETRY=0 means disabled. So `ATCR_TELEMETRY=0` is disabled. If env is unset, is it considered enabled or disabled? Probably enabled (opt-out). The env var names the enabled state, so unset means default (enabled). `ATCR_TELEMETRY=0` explicitly disables. `ATCR_TELEMETRY=1` explicitly enables (redundantly).
But the config file `telemetry: false` disables. `telemetry: true` enables.
How is the OR logic implemented? "They are OR'd — telemetry is disabled whenever EITHER says so". Wait, OR of disabled signals. If either says disable, it's disabled. So it's a logical AND of enable signals? No, it's "disabled if config says false OR env says 0". If config says true and env says 1, both say enable, so it's enabled. If config says true and env is unset (enabled by default?), it's enabled. The logic seems correct.

However, I can't see the implementation of `registry.SetTelemetry