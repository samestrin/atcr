# Code Review Stream - 11.0_executing_reviewers (Epic)

**Started:** June 26, 2026 07:09:38AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests if enabled]

---

## Acceptance Criteria Findings

### Criterion: SC-1 (T4) — execution refused unless --exec AND backend passes preflight
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/exec.go:14-56` (ErrExecNoBackend, ResolveExecBackend requires preflight), `cmd/atcr/verify.go:43-58` (resolveExec gate), `cmd/atcr/review.go:135-160` (review --exec requires --verify; resolveExec runs preflight early)
- **Notes:** `--exec` is off by default on both `review` and `verify`. Without a `[sandbox]` block → hard error (exit 2). Preflight is a real container spawn that must pass before any backend is returned. `review --exec` without `--verify` is also rejected.

### Criterion: SC-2 (T1) — sandbox network-isolated, capped, non-root, read-only snapshot
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/sandbox/docker.go:88-126` (dockerRunArgs: --network none, --read-only, --cap-drop ALL, --security-opt no-new-privileges, --user non-root 65534, --memory/--cpus/--pids-limit, -v snap:/work:ro, tmpfs /scratch), `internal/sandbox/docker.go:188-220` (Preflight), `internal/sandbox/sandbox.go:48-66` (SnapshotDir absolute + reject ':' mount-injection)
- **Notes:** Hardening is thorough and asserted in unit tests. Snapshot mounted read-only; only /scratch tmpfs is writable. MaxConcurrent semaphore caps container fan-out (resource-abuse risk).

### Criterion: SC-3 (T2+T3) — planted-bug reproduced e2e: evidence_exec populated, verdict confirmed
- **Verdict:** PARTIAL ⚠️
- **Evidence:** `internal/repro/repro.go:51-99` (Reproduce/Stamp/Verdict — has ZERO non-test importers), `internal/verify/pipeline.go:604` (EnableExecution wires tools but never calls repro.Stamp), `internal/verify/verify_e2e_test.go:73` (e2e runs with exec=false; never asserts evidence_exec)
- **Notes:** Tools (run_tests/run_script) ARE offered to skeptics in --exec runs and the determinism/write-back logic exists — but the repro write-back (Reproduce → Stamp) is never invoked by the production verify pipeline. In a real `--exec` run, `evidence_exec` is never populated and the skeptic verdict remains a model self-report via parseVerdict (the exact hallucination risk the epic set out to eliminate). The "planted-bug e2e" only exercises the repro package in isolation against a fake backend (repro_test.go). End-to-end criterion NOT met.

### Criterion: SC-4 (T3+T5) — evidence flows findings.json → report.md badge → gate (VERIFIED)
- **Verdict:** PARTIAL ⚠️
- **Evidence:** `internal/reconcile/emit.go:124-140` (evidence_exec schema, omitempty), `internal/report/render.go:181-182,376-378` (Reproduced badge keyed on f.EvidenceExec != nil)
- **Notes:** Schema + render path are correct and the badge renders if EvidenceExec is set. But because the write-back is unwired (see SC-3), evidence_exec is never populated in production, so the badge never renders and "reproduced ⇒ confirmed ⇒ VERIFIED ⇒ IsFailing" is unreachable on the real path. Mechanism present; data flow not connected end-to-end.

### Criterion: SC-5 (T1–T5) — all quality gates green (go test/vet/lint/build, coverage ≥ 80%)
- **Verdict:** VERIFIED ✅
- **Evidence:** `go build ./...` exit 0; `go vet ./...` exit 0; `go test ./...` all packages PASS; `golangci-lint run` 0 issues; `gofmt -l` empty; total coverage 89.0% (baseline 80%, +9.0%). New packages: sandbox 87.9%, repro 85.7%, tools 86.5%, verify 94.9%, report 98.0%.
- **Notes:** All gates green at HEAD.

### Criterion: AC-SECURITY — not auto-merged; security checklist; human manual merge
- **Verdict:** VERIFIED ✅
- **Evidence:** Epic merged via PR #96 as merge commit `a2dfa92b` (recorded at epic file line 135; archived by `9b2b9c87`). Manual squash-merge path honored.
- **Notes:** Process criterion — the epic was not auto-merged; it rode a PR and was merged by the human owner.

## Adversarial Analysis (Discovery Mode — epic, no sprint-design risk profile)

**Mode:** Full hostile review (2 independent parallel reviewers)
**Files Reviewed:** 16 Go source files (sandbox, tools, fanout exec-gating, registry, repro, reconcile/emit, report/render, verify pipeline/exec/invoke, cmd review/verify)
**Issues Found:** 16 (verified from TD_STREAM)
**Risk Profile:** Not available (epic mode — discovery-only)

### Issues by Severity (verified)
- Critical: 0
- High: 3
- Medium: 8
- Low: 5

### Headline finding (independently confirmed by both reviewers)
The `internal/repro` package — the entire `evidence_exec` write-back + 2-run determinism mechanism — has **zero non-test importers**. The verify pipeline offers the run_tests/run_script tools to skeptics but never calls `repro.Stamp`, so `evidence_exec` is never populated in a real `--exec` run. The epic's capstone deliverable (SC-3/SC-4: executable proof attached as evidence, flowing to the report's "Reproduced" badge, deterministic confirmation) is **not wired end-to-end**. The supporting MEDIUM rows (badge renders for non-confirmed verdicts; JSONFindings derivation drops EvidenceExec; infra exit codes counted as confirmed) compound it.

### Step 4d — Incomplete / PARTIAL routing
- INCOMPLETE (NOT_FOUND) criteria: 0
- PARTIAL criteria: 2 (SC-3, SC-4). These are NOT added as duplicate TD rows — their root cause is captured by the HIGH `internal/repro/repro.go` write-back item plus the related MEDIUM rows (`report/render.go:181`, `reconcile/emit.go:163`). One source of truth preserved; no false Pass.
