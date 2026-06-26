# Code Review Report: 11.0_executing_reviewers

## 1. Executive Summary
- **Overall Result:** Partial
- **Items Checked:** 4 / 6 criteria fully verified (2 partial)
- **Approval Status:** Pending
- **Review Date:** June 26, 2026
- **Review Mode:** Epic (Acceptance/Success Criteria + Adversarial + Tests)
- **Merged as:** PR #96 / merge commit `a2dfa92b` (already on `main`)

The epic's **infrastructure is real and well-built** — a hardened Docker sandbox, opt-in `--exec` flag with a refuse-without-backend gate, the `evidence_exec` schema, the "Reproduced" badge render path, and the 2-run determinism logic all exist and are unit-tested. Quality gates are green (build, vet, tests, lint, format; coverage 89.0%).

**However, the capstone is not wired end-to-end.** The `internal/repro` package — the only code that builds an `evidence_exec` block and stamps a reproduced/confirmed verdict — has **zero non-test importers**. The verify pipeline offers the run_tests/run_script tools to skeptics but never invokes the write-back, so in a real `atcr verify --exec` run `evidence_exec` is never populated, the "Reproduced" badge never renders, and the skeptic verdict stays a model self-report. **SC-3 and SC-4 are therefore only partially met.**

## 2. Criteria Verified

| Criterion | Verdict | Evidence |
|-----------|---------|----------|
| SC-1 (T4) refuse unless `--exec` + preflight | VERIFIED ✅ | `internal/verify/exec.go:14-56`, `cmd/atcr/verify.go:43-58`, `cmd/atcr/review.go:135-160` |
| SC-2 (T1) network-isolated, capped, non-root, RO snapshot | VERIFIED ✅ | `internal/sandbox/docker.go:88-126,188-220`, `internal/sandbox/sandbox.go:48-66` |
| SC-3 (T2+T3) planted-bug reproduced e2e, `evidence_exec` populated | PARTIAL ⚠️ | `internal/repro/repro.go` (no non-test importers); `internal/verify/pipeline.go:604` (no `repro.Stamp`) |
| SC-4 (T3+T5) evidence flows findings.json → report.md → gate | PARTIAL ⚠️ | `internal/reconcile/emit.go:124-140`, `internal/report/render.go:181-182` (badge path correct but never fed) |
| SC-5 (T1–T5) all quality gates green, coverage ≥ 80% | VERIFIED ✅ | build/vet/tests/lint/format pass; total coverage 89.0% |
| AC-SECURITY not auto-merged; manual human merge | VERIFIED ✅ | PR #96, merge commit `a2dfa92b` (epic line 135) |

## 3. Remaining / Partial Items
- **SC-3 & SC-4 (PARTIAL):** The `evidence_exec` write-back (`repro.Reproduce` → `repro.Stamp`) is implemented as an isolated package but never called from `internal/verify/pipeline.go`. Either wire it into the pipeline after `verifyFinding`, or remove the dead package + report/schema plumbing if execution-as-structured-evidence is deferred. Tracked as the HIGH TD item at `internal/repro/repro.go`.

## 4. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Tests | PASSING | `go test ./...` (all packages ok) |
| Coverage | PASSING (89.0%, baseline 80%) | `go test -coverprofile=coverage.out ./...` |
| Lint | PASSING (0 issues) | `golangci-lint run` |
| Types/Vet | PASSING | `go vet ./...` |
| Format | PASSING | `gofmt -l internal/ cmd/` (empty) |
| Build | PASSING | `go build ./...` |

## 5. Adversarial Analysis
- **Files Reviewed:** 16 Go source files (2 independent parallel reviewers, discovery mode)
- **Issues Found:** 16 (Critical: 0, High: 3, Medium: 8, Low: 5)

### High
1. **`internal/repro` dead code — `evidence_exec` never produced in production** (`internal/repro/repro.go:47-99`) — capstone gap; nullifies SC-3/SC-4 end-to-end. *(independently confirmed by both reviewers)*
2. **Timeout kills the docker CLI but orphans the container** (`internal/sandbox/docker.go:140-197`) — `exec.CommandContext` SIGKILLs the client, not the workload; resource caps are not reclaimed on timeout for a signal-ignoring script.
3. **Unbounded host-side output buffer** (`internal/sandbox/docker.go:160-174`) — a model-authored script flooding stdout grows the atcr host heap without limit (truncation only applied after the run completes); host OOM risk outside the sandbox caps.

### Medium (8)
Tool-gating is advisory not structural in the shared dispatcher (`tools/dispatch.go:146`); preflight skips the cap/mount flags (`sandbox/docker.go:202-221`); `SandboxConfig.Validate` ignores Memory/CPUs (`registry/sandbox.go:42-66`); model-supplied `run_script` timeout is unclamped (`tools/exec_tools.go:89-102`); "Reproduced" badge renders for non-confirmed verdicts (`report/render.go:179-183`); `writeReproducedBlock` double-escapes inside a code span (`report/render.go:376-381`); `JSONFindings()` derivation drops `EvidenceExec` (`reconcile/emit.go:162-184`); infra/signal exit codes (137/127/139) stamped as confirmed (`repro/repro.go:34-45`).

### Low (5)
sandbox `truncate` overshoots its byte limit (`sandbox/sandbox.go:105-115`); `EnableExecution` mutates handler map without sync vs concurrent Execute (`tools/exec_tools.go:60-66`); struct-literal `DockerBackend` nil-sem bypasses the concurrency cap (`sandbox/docker.go:139-152`); `verdictRank` duplicates merge.go precedence (`repro/repro.go:87-100`); `review --exec` preflight runs before range/registry validation (`cmd/atcr/review.go:145-152`).

All 16 findings are written to `td-stream.txt` for `/reconcile-code-review`.

## 6. Follow-ups
1. **Decide SC-3/SC-4 disposition** (HIGH): wire the repro write-back into the verify pipeline + add a real end-to-end test, OR remove the dead package and downgrade the epic's "executable proof" claim. This is the one finding that changes whether the epic delivered its headline.
2. Address the two HIGH sandbox-robustness findings (orphaned container on timeout; unbounded host buffer) before `--exec` is advertised as safe for untrusted code.
3. Run `/reconcile-code-review @.planning/epics/completed/11.0_executing_reviewers.md` to merge findings into the TD README.

---
*Generated by /execute-code-review on June 26, 2026*
