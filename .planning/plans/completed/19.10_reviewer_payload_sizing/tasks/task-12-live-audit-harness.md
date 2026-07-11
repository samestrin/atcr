# Task 12: Live Audit Harness (AC-Live)

**Source:** Plan 19.10 – Debt Item #9 (AC-Live)
**Priority:** P1 | **Effort:** M | **Type:** Add

## Problem Statement
Tasks 01-11 fix the confirmed 19.6 failure (`dax` boundary overflow, `otto`'s unrecognized-truncate error, `greta`/`vera`/`brad` timeouts) through per-model sizing, window-aware chunking, `on_overflow` dispatch, timeout scaling, and diagnosability fields — but every one of those fixes is verified only by unit/integration tests against synthetic fixtures (`go test ./...`). Nothing re-runs the exact, confirmed-failing 19.6 diff range against the real `orchestrator.lan` litellm proxy and the real 11-agent roster to prove the fixes actually resolve the incident they trace back to. The confirmed baseline is on record at `.planning/sprints/completed/19.6_community_registry_hub/code-review/multi-agent/` (a 101-file/6,429-insertion diff, base `f9d5161be5b07214edc3fb435497d169883a3020` → head `b6bcb676d2cbb461ed25f723e7daaae805589450`): `sources/pool/summary.json` shows `dax` failed with `litellm.ContextWindowExceededError`, `otto` failed with a `BadRequestError` naming `--allow-auto-truncate`, `greta`/`vera`/`brad` all show `status: "timeout"` with `"request failed: ... context deadline exceeded"`, and only 1 of 11 agents (`mira`) returned a finding. Without a scripted re-run of this exact range against the exact roster, "the plan fixed 19.6" remains an unverified claim.

## Solution Overview
Add `examples/19.10-live-audit.sh`, modeled on the existing sibling `examples/ci-gate.sh` (shebang + banner comment + `set -euo pipefail` + env-var-driven overrides + `exec`/explicit exit codes), that: (1) early-skips with exit 0 when the live roster isn't reachable, so it never blocks `go test ./...` or a normal CI run; (2) otherwise re-runs `atcr review --base f9d5161be5b07214edc3fb435497d169883a3020 --head b6bcb676d2cbb461ed25f723e7daaae805589450` against the real roster into a fresh output directory; (3) hard-gates the resulting `summary.json` on the three AC-Live assertions (zero `ContextWindowExceededError`, all five previously-failing agents `status=ok`, findings from multiple agents); and (4) diffs the fresh "after" `summary.json` against the committed 19.6 "before" baseline and prints a before/after evidence table. The script is standalone-runnable (`bash examples/19.10-live-audit.sh`) and safe to invoke from the execution/CI loop because failure to reach the environment is a skip, not a failure.

## Technical Implementation
### Steps
1. Create `examples/19.10-live-audit.sh` following `examples/ci-gate.sh`'s exact structural conventions: `#!/usr/bin/env bash`, a top banner comment stating purpose/usage/env vars/exit codes, `set -euo pipefail`, and env-var overrides with `${VAR:-default}` defaults (mirror `FAIL_ON="${ATCR_FAIL_ON:-high}"`). Define:
   - `BASE_SHA="${ATCR_LIVE_AUDIT_BASE:-f9d5161be5b07214edc3fb435497d169883a3020}"` and `HEAD_SHA="${ATCR_LIVE_AUDIT_HEAD:-b6bcb676d2cbb461ed25f723e7daaae805589450}"` — the exact confirmed 19.6 range, overridable only for local re-testing.
   - `BASELINE_DIR="${ATCR_LIVE_AUDIT_BASELINE:-.planning/sprints/completed/19.6_community_registry_hub/code-review/multi-agent}"` — note this corrects the plan's grounding note, which cites `.../sprints/active/19.6_community_registry_hub/...`; that sprint has since archived to `sprints/completed/19.6_community_registry_hub/` (confirmed by directory listing during task generation) — verify this path still resolves at implementation time and update if the sprint moves again.
   - `OUT_DIR="${ATCR_LIVE_AUDIT_OUT:-.atcr/live-audit-$(date +%Y%m%d-%H%M%S)}"` — a fresh, timestamped output directory per run (never overwrites the committed baseline).
   - `MIN_AGENTS_WITH_FINDINGS="${ATCR_LIVE_AUDIT_MIN_AGENTS:-2}"` — the "multiple agents" floor from AC-Live, configurable but defaulting to the minimum that satisfies "not 1".
2. Implement the early skip guard using `atcr doctor` (`cmd/atcr/doctor.go` — the existing "resolve the roster, invoke each configured model endpoint once, report reachability" preflight; exits 0 when every configured agent has a working invocation path, 1 when any agent fails, 2 on config error). Run `atcr doctor --json --timeout 15` and inspect the JSON: if the command errors at the usage/config level (exit 2) or reports zero reachable agents, print `SKIP: live roster unreachable (orchestrator.lan or .atcr/config.yaml providers not configured) — see examples/19.10-live-audit.sh` to stderr and `exit 0`. Do not hard-fail on a partial doctor failure (some agents reachable, some not) — that is itself useful signal the gate below will catch; only skip on total unreachability, since a config that resolves zero endpoints means the environment prerequisite genuinely isn't met, not that the fixes are broken.
3. Run the live review: `atcr review --base "$BASE_SHA" --head "$HEAD_SHA" --output-dir "$OUT_DIR"` (flags confirmed at `cmd/atcr/flags.go:14-17` for `--base`/`--head` and `cmd/atcr/review.go:59` for `--output-dir`) against the roster already configured in the operator's local `.atcr/config.yaml` (the committed local dev config already lists the exact 11-agent panel — `bruce`, `greta`, `kai`, `mira`, `dax`, `pace`, `vera`, `brad`, `archer`, `ronin`, `otto` — that produced the confirmed 19.6 failure, so no roster override is needed). Do not pass `--fail-on` here — this script implements its own AC-Live-specific gate below, not atcr's generic severity gate. Capture the command's own exit code but do not `exec`/early-exit on nonzero — `atcr review` returning nonzero on partial failures is expected and exactly the scenario this script must inspect, not just relay.
4. Locate the fresh `summary.json` (`$OUT_DIR/sources/pool/summary.json`, mirroring the confirmed layout under `.planning/sprints/completed/19.6_community_registry_hub/code-review/multi-agent/sources/pool/summary.json`) and `manifest.json` (`$OUT_DIR/manifest.json`). Fail with a clear error and exit 2 (usage/artifact error, distinct from a gate failure) if either file is missing after the review run — that indicates the run itself crashed before producing artifacts, not a degradation the gate is designed to catch.
5. Implement the three hard-gate assertions against `$OUT_DIR/sources/pool/summary.json` using `jq` (already an assumed dependency elsewhere in this repo's tooling, e.g. `.github/workflows/hermes-auto-merge.yml`):
   - **Zero `ContextWindowExceededError`:** `jq -e '[.agents[] | select(.error != null and (.error | contains("ContextWindowExceededError")))] | length == 0' "$OUT_DIR/sources/pool/summary.json"`. On failure, print the offending agent names/errors and set a `GATE_FAILED=1` flag (accumulate all three checks before exiting, so one run reports every violation, not just the first).
   - **Five previously-failing agents all `status=ok`:** for each of `dax otto greta vera brad`, `jq -e --arg a "$agent" '.agents[] | select(.agent == $a) | .status == "ok"' "$OUT_DIR/sources/pool/summary.json"`; print the agent's actual status/error on failure.
   - **Findings from multiple agents:** `jq -e --argjson min "$MIN_AGENTS_WITH_FINDINGS" '[.agents[] | select(.findings_count > 0)] | length >= $min' "$OUT_DIR/sources/pool/summary.json"`; print the count and per-agent finding tallies on failure. Note per AC-Live this floor is a hard gate on count only — finding *quality/groundedness* is explicitly "reviewed by hand," so the script does not attempt to grade finding content.
6. Capture before/after evidence: read agent `status`/`findings_count` from both `$BASELINE_DIR/sources/pool/summary.json` (before, committed, read-only) and `$OUT_DIR/sources/pool/summary.json` (after, this run), and print a per-agent before→after table plus a summary line (e.g. `before: 5 ok / 3 timeout / 3 failed, 1 agent with findings` vs `after: N ok / ... , M agents with findings`) to stdout. Also write this table to `$OUT_DIR/live-audit-evidence.txt` so the execution loop can archive it as a run artifact.
7. Exit with 0 if all three gates pass, 1 if any gate failed (matching `ci-gate.sh`'s `0 pass / 1 gate failure` convention), 2 for usage/config/artifact errors (missing summary.json, doctor usage error), distinct from the skip path's 0. Print a final one-line `PASS`/`FAIL`/`SKIP` summary so the execution loop can grep it without re-parsing JSON.
8. Make the script executable (`chmod +x examples/19.10-live-audit.sh`), matching `examples/ci-gate.sh`'s mode.
9. Manually dry-run the skip path (with `orchestrator.lan`/the configured proxy intentionally unreachable, e.g. via an invalid `base_url` override or network isolation) and confirm it exits 0 with a `SKIP:` message — this is the only portion of the script verifiable without live roster access; the full live-run path can only be exercised on a host with real `orchestrator.lan` connectivity, per the plan's Constraints.

## Files to Create/Modify
- `examples/19.10-live-audit.sh` – create (based on `examples/ci-gate.sh`)

## Documentation Links
- [Plan](../plan.md)
- [Original Requirements](../original-requirements.md)

## Related Files (from codebase-discovery.json)
- `examples/ci-gate.sh` — sibling script this task's style/conventions are modeled on
- `.planning/sprints/completed/19.6_community_registry_hub/code-review/multi-agent/sources/pool/summary.json` — confirmed "before" baseline (per-agent status/errors); note this has moved from the `sprints/active/...` path cited in the plan's grounding to `sprints/completed/...` since the 19.6 sprint archived
- `.planning/sprints/completed/19.6_community_registry_hub/code-review/multi-agent/manifest.json` — confirmed baseline manifest (`partial: true`, `timeout_secs: 600`, `max_parallel: 6`, base/head SHAs, 11-agent roster)
- `.planning/sprints/completed/19.6_community_registry_hub/code-review/multi-agent/payload/blocks.txt` — confirmed baseline payload (506 KB)
- `cmd/atcr/review.go` — `atcr review` flag registration (`--base`, `--head`, `--output-dir`) this script invokes
- `cmd/atcr/flags.go` — `addRangeFlags` (`--base`/`--head`/`--merge-commit`)
- `cmd/atcr/doctor.go` — `atcr doctor` preflight self-test this script's skip guard reuses
- `.atcr/config.yaml` — local (uncommitted) roster config; the 11-agent panel that reproduced the confirmed 19.6 failure

## Success Criteria
- [ ] `examples/19.10-live-audit.sh` exists, is executable, and follows `examples/ci-gate.sh`'s structural conventions (banner comment, `set -euo pipefail`, env-var overrides, explicit exit codes)
- [ ] Running the script when the live roster is unreachable prints a `SKIP:` message and exits 0 — never blocks a standard `go test ./...` run or a CI job that happens to invoke it
- [ ] Running the script against a reachable `orchestrator.lan` roster re-runs the exact 19.6 range (base `f9d5161be5b07214edc3fb435497d169883a3020`, head `b6bcb676d2cbb461ed25f723e7daaae805589450`) and hard-gates on zero `ContextWindowExceededError` across all agents in the fresh `summary.json`
- [ ] The script hard-gates on `dax`, `otto`, `greta`, `vera`, and `brad` all reporting `status=ok` in the fresh `summary.json`, failing loudly (exit 1, naming the offending agent(s) and their actual status/error) if any do not
- [ ] The script hard-gates on findings appearing from at least `ATCR_LIVE_AUDIT_MIN_AGENTS` (default 2) distinct agents, not just 1 — matching AC-Live's "grounded findings come from multiple agents" floor
- [ ] The script prints (and writes to `$OUT_DIR/live-audit-evidence.txt`) a before/after comparison against the committed 19.6 baseline at `.planning/sprints/completed/19.6_community_registry_hub/code-review/multi-agent/sources/pool/summary.json`, satisfying the plan's "before/after counts captured as evidence" requirement
- [ ] The script is runnable standalone (`bash examples/19.10-live-audit.sh`) with no required arguments, and safely invocable from an execution/CI loop (idempotent, distinct exit codes for skip/pass/fail/usage-error)

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- None — this is a scripted integration harness, not a `go test` (per plan Constraints: "The live audit (AC-Live) is env-coupled ... and therefore is a scripted integration verification, not a `go test`").

**Integration Tests:**
- Standalone dry run of the skip-guard path: with the configured provider(s) unreachable (e.g. an intentionally-broken `base_url` or the network disconnected), run `bash examples/19.10-live-audit.sh` and confirm it prints `SKIP:` and exits 0.
- Standalone live run on a host with real `orchestrator.lan` connectivity and the real 11-agent roster configured: run `bash examples/19.10-live-audit.sh` and confirm (a) it produces `$OUT_DIR/sources/pool/summary.json` and `manifest.json`, (b) the three hard gates evaluate correctly against that summary, (c) the before/after evidence table is printed and written to `$OUT_DIR/live-audit-evidence.txt`, and (d) the final exit code matches PASS/FAIL as expected given the actual agent statuses observed.
- Argument/env-var override check: confirm `ATCR_LIVE_AUDIT_BASE`/`ATCR_LIVE_AUDIT_HEAD`/`ATCR_LIVE_AUDIT_OUT`/`ATCR_LIVE_AUDIT_MIN_AGENTS` overrides are honored (useful for testing the gate logic against a smaller/faster local range before trusting it on the full 19.6 range).

**Test Files:**
- `examples/19.10-live-audit.sh` (self-verifying script; no separate `_test.go`/test harness — this is intentionally outside `go test ./...`)

## Risk Mitigation
- **Risk:** `orchestrator.lan`/the real roster is unreachable from the execution host (CI runners live on `gauntlet.lan`, not necessarily networked to `orchestrator.lan`), causing the script to hang or fail the enclosing job. **Mitigation:** the `atcr doctor`-based early skip guard (Step 2) detects total unreachability up front and exits 0 before attempting the full review run, so an unreachable environment is a no-op, never a build failure.
- **Risk:** The script silently passes because it only checks the happy-path fields and never re-verifies the baseline file actually reflects the confirmed 19.6 incident (e.g. if `.planning/sprints/completed/19.6_community_registry_hub/...` is later deleted/archived further). **Mitigation:** the before/after step (Step 6) reads the baseline file explicitly and should fail loudly (exit 2, usage/artifact error) if it's missing, rather than silently skipping the comparison.
- **Risk:** A partial `atcr doctor` failure (some agents reachable, most not) gets misclassified as either a hard skip (masking a real regression) or a hard fail (blocking on an environment issue unrelated to the sizing fixes). **Mitigation:** Step 2 skips only on *total* unreachability (zero doctor-reachable agents); any partial reachability proceeds to the full live run, where the three AC-Live gates (Step 5) are the authoritative signal.
- **Risk:** Finding-quality floor (`ATCR_LIVE_AUDIT_MIN_AGENTS`) is gamed by a degenerate run where many agents fire trivial/ungrounded findings just to clear the count. **Mitigation:** matches the plan's explicit design choice — AC-Live is "hard-gated on the deterministic assertions, soft on finding quality" (Decisions from Refinement, original-requirements.md); the count floor is intentionally a coarse, automatable signal, with quality reviewed by hand per the plan, not by this script.

## Dependencies
- Task-01 through Task-11 — this harness verifies the combined output of the full sprint (context-window resolver, per-agent budget, window-aware chunking, `on_overflow` dispatch, fallback provenance, timeout scaling, cache-key correctness, diagnosability fields, sprint-plan config) against the real 19.6 diff range and roster; sequence last, only after all preceding tasks land

## Definition of Done
- [ ] `examples/19.10-live-audit.sh` created, executable, and follows `examples/ci-gate.sh`'s conventions
- [ ] Skip-guard path verified locally (unreachable environment → `SKIP:` + exit 0)
- [ ] Full live-run path executed at least once against a reachable `orchestrator.lan` + the real roster, with the three AC-Live gates observed passing (zero `ContextWindowExceededError`; `dax`/`otto`/`greta`/`vera`/`brad` all `status=ok`; findings from ≥2 agents) — this is the plan's ultimate proof-of-fix and cannot be simulated in `go test ./...`
- [ ] Before/after evidence captured and reviewed by hand for finding quality (per AC-Live's "quality reviewed by hand" note)
- [ ] `go test ./...` continues to pass unaffected — this script is never invoked by the Go test suite
- [ ] Script is referenced from the sprint's completion checklist (or equivalent) as the final proof-of-fix step, run manually or via the execution loop once F1-F9 land
