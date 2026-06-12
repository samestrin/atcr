# Code Review Stream - 1.2_model_endpoint_selftest (Epic)

**Started:** June 12, 2026 12:54:32PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: `atcr doctor` tests all distinct (provider, model, base_url) targets from the effective roster, including fallback agents, each invoked at most once
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/doctor/resolve.go:48-125` (Resolve walks Agents + SerialAgents + fallback chains; dedup by NUL-joined `provider\x00model\x00base_url` key), `internal/doctor/run.go:98-118` (probes each distinct Target once), `internal/doctor/run_test.go:58-71` (TestRun_SharedTargetInvokedOnce asserts count==1)
- **Notes:** Dedup key uses NUL separators to prevent forged collisions; defensive seen-set guards malformed fallback graphs.

### Criterion: Missing API key env reported per agent as `missing_key` without any network call
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/doctor/run.go:185-187` (os.Getenv check returns StatusMissingKey before c.Complete), `internal/doctor/run_test.go:87-99` (TestRun_MissingKeySkipsNetwork asserts fake.count==0 and StatusMissingKey)
- **Notes:** Pre-flight (base_url + key) checks all precede the network call.

### Criterion: Thinking model at default token budget passes (marker in visible content); reasoning-style mock in tests
- **Verdict:** VERIFIED ✅ (with test-fidelity caveat)
- **Evidence:** `cmd/atcr/doctor.go:29` (default --max-tokens 2048), `internal/doctor/run.go:~210` (classify: marker present → StatusOK), `internal/doctor/run_test.go:74-84` (small-budget empty-content → warning)
- **Notes:** Default budget 2048 is correct and the warning path is tested, but `fakeCompleter` returns the marker unconditionally and never reads `inv.MaxTokens` — no mock conditions its output on the budget, so the "reasoning-style mock" is only superficially simulated. Captured as LOW.

### Criterion: Empty visible content on HTTP 200 is a warning with hint to raise --max-tokens, not a hard failure
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/doctor/run.go:~210-216` (err==nil + marker absent → StatusOKWarning + hint), `internal/doctor/run.go:37` (healthy() includes StatusOKWarning → exit-0 path), `internal/doctor/run_test.go:74-84`
- **Notes:** Warning counts as a working path; exit code stays 0.

### Criterion: Failure classes distinguished (auth/not-found/rate-limit/network/timeout) with bounded provider error-body snippet
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/doctor/run.go:~219-245` (classify via errors.As(*HTTPStatusError): 401/403→auth_failed, 404→not_found, 429→rate_limited, 5xx→provider_error; DeadlineExceeded→timeout; else→network_error; detail=se.Snippet), `internal/llmclient/client.go:203-225` (HTTPStatusError{Status,Snippet}), `client.go:259` (snippet key-redacted), `internal/doctor/run_test.go:162-199`
- **Notes:** network_error detail bounded to 512 bytes; snippet redaction of API key preserved.

### Criterion: `--json` output stable and documented; human table to stdout, logs to stderr
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/doctor/render.go:11-21` (RenderJSON stable wrapper, never null), `cmd/atcr/doctor.go:92-108` (table→OutOrStdout, summary + banner→ErrOrStderr), `docs/registry.md:240` (schema documented), `internal/doctor/run_test.go:201-237`
- **Notes:** JSON top-level is always a `{agents:[...]}` object.

### Criterion: Exit codes 0/1/2 covered by httptest fake providers
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/doctor/run.go:131-152` (exitVerdict 0/1), `cmd/atcr/main.go:43-55` (usageError→exit 2 via codedError), `cmd/atcr/doctor_test.go:19-77` (echoProvider httptest.Server), `doctor_test.go:99-153` (MissingKey/Auth→exit1, NoConfig/UnknownAgent→usage error)
- **Notes:** Exit 2 mapping is centralized in main.go; doctor wraps all config/usage failures in usageError.

### Criterion: README and docs/registry.md document doctor as recommended post-`atcr init` verification step
- **Verdict:** VERIFIED ✅
- **Evidence:** `README.md:59` ("recommended post-`atcr init` verification step"), `README.md:74,82` (Commands table + flags/exit codes), `docs/registry.md:181-190` ("Verifying the configuration" section with examples)
- **Notes:** Docs cover flags, exit codes, dedup behavior, and SOURCE provenance column.

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (no sprint-design.md risk profile — epic)
**Files Reviewed:** 5 (resolve.go, run.go, render.go, doctor.go, client.go)
**Issues Found:** 4 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 4

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 4
