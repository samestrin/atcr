# Code Review Stream - 19.2_shared_registry_remote (Epic)

**Started:** July 05, 2026 05:38:24PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: With `ATCR_REGISTRY_URL` pointing at a reachable `registry.yaml`, providers/personas load from the remote source.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/overlay.go:137-151` (`loadRegistryBytes` reads the env var, fetches when set, reads local when unset), `overlay.go:158-205` (`fetchRemoteRegistry`). Test: `overlay_remote_test.go:43-54` (`TestParseRegistryFile_RemoteURL_Loads` — local path is bogus, so success proves remote load; asserts `openai` provider + `bruce` agent present, `SourceUser` tier).
- **Notes:** Only the user registry is fetched remotely; project overlay + trust store stay local, matching the epic's Clarifications.

### Criterion: API keys still resolve only from env vars; a key present in the remote file is ignored (with a warning).
- **Verdict:** VERIFIED ✅ (implemented per clarified interpretation — see note)
- **Evidence:** Remote bytes flow through the unchanged strict decoder (`decodeStrictYAML`, `KnownFields(true)`); `Provider` has only `api_key_env`/`base_url`. Tests: `overlay_remote_test.go:244-259` (`TestRemoteRegistry_LiteralAPIKeyRejected` — a literal `api_key:` in the remote file is a hard unknown-field error), `:265-282` (`TestRemoteRegistry_KeyReferencedNotStored` — only the env-var NAME travels; load succeeds with the var unset).
- **Notes / tradeoff surfaced:** The epic's literal wording was "ignored (with a warning)". The `/execute-epic` rubber-duck Clarifications (AC2, Option A) deliberately changed this to **fail-closed strict rejection** rather than a pre-decode warning scan — the warning path is unreachable because the strict decoder errors first. This is a *stronger* guarantee than the original wording (hard error vs. silent ignore). The only "warning" in the code is the separate non-https insecure-URL warning. Acceptable and documented, but it is a deviation from the AC's literal text.

### Criterion: An unreachable/invalid URL exits with a clear error and the documented local-fallback behavior.
- **Verdict:** VERIFIED ✅ (fallback semantics clarified — see note)
- **Evidence:** `overlay.go:128-151` (no silent fallback; local read reached only when env var unset), `:158-205` (hard errors on bad scheme, transport failure, non-2xx, oversized body). Tests: `TestFetchRemoteRegistry_Unreachable_HardError` (+ asserts no `registry not found` fallback and no token leak), `_Non200_HardError`, `_MalformedYAML_HardError`, `_RejectsNonHTTPScheme`, `_MalformedURL`, `_BodyLimit`. Docs: `docs/registry.md:94` ("No silent fallback… local read happens only when the variable is unset").
- **Notes:** "local-fallback" means fallback-to-local on **unset**, not on failure. A set-but-broken URL is an unconditional hard error by design (Clarifications AC3, Option A) — a team is told its shared source is broken rather than silently diverging. Documented behavior matches implementation.

### Criterion: `go test ./...` passes; covered by `internal/registry/*_test.go`.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/` tests pass; the feature is covered by the new `internal/registry/overlay_remote_test.go` (18 test functions spanning remote load, unset-reads-local, hard-error paths, DoS body limit, insecure-warning-once + redaction, URL redaction, label derivation, project-overlay merge, and validation-over-remote). Full-suite result confirmed in Phase 4.
- **Notes:** Test file installs a `TestMain` that unsets `ATCR_REGISTRY_URL` so the package stays hermetic — a good defensive touch.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md risk profile)
**Files Reviewed:** 1 (`internal/registry/overlay.go` — the epic's source change; `overlay_remote_test.go` cross-read)
**Issues Found:** 1 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 1

### Assessment
The remote-fetch code is defensively strong and the security-sensitive paths are explicitly tested:
- Token/credential redaction in errors and warnings (`redactRegistryURL`, `*url.Error` unwrap) — covered by `TestFetchRemoteRegistry_Unreachable_HardError` (asserts `leak-me-not` absent) and `TestWarnInsecureRegistryURL_OnceAndRedacted`.
- DoS guard via `io.LimitReader(limit+1)` + size check — `TestFetchRemoteRegistry_BodyLimit`.
- Fail-closed on bad scheme / transport / non-2xx / malformed / empty — five dedicated tests.
- Env-var-only key contract preserved through the strict decoder — `TestRemoteRegistry_LiteralAPIKeyRejected`, `TestRemoteRegistry_KeyReferencedNotStored`.

One low-severity hardening gap found: the http/https scheme check and insecure-http warning run only on the initial URL, but `http.DefaultClient` follows redirects, so an https→http downgrade via a 30x redirect bypasses the guard. Routed to TD (LOW). Not a merge blocker; the source is team-trusted.
