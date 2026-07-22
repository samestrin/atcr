# Test Planning Matrix

**Generated:** 2026-07-21
**Plan:** 32.3_sandbox_ephemeral_copy_overlay
**Total ACs:** 15

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E/Docs | Complexity |
|-------|-----|------|-------------|----------|------------|
| 01 - Opt-In Writable Configuration Surface | 3 | 2 | 1 | 0 | Low |
| 02 - Conditional Writable /work Mount | 3 | 3 | 0 | 0 | High |
| 03 - Ephemeral-Copy Setup Injection | 3 | 3 | 0 | 0 | High |
| 04 - `--auto-fix` Opts Into the Writable Overlay | 2 | 2 | 0 | 0 | Medium |
| 05 - Regression Proof and Docs Parity | 4 | 3 | 0 | 1 (Docs) | Medium |
| **Total** | **15** | **13** | **1** | **1** | — |

---

## Detailed AC List

### Story 1: Opt-In Writable Configuration Surface

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | RunSpec Gains an Opt-In Writable Field | Unit | Low | P1 |
| 01-02 | DockerConfig Gains a WorkSize Tunable with a Sane Default | Unit | Low | P1 |
| 01-03 | Zero Behavior Change for Existing `--exec` Callers and Test Suite | Integration (full-package regression, backed by unit) | Medium | P1 |

### Story 2: Conditional Writable /work Mount

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Writable:false Argv Stays Byte-Identical | Unit | Medium | P1 |
| 02-02 | Writable:true Mounts /src Read-Only and /work as a tmpfs | Unit | High | P1 |
| 02-03 | Writable Setup Step Populates /work Before the Real Payload Runs | Unit (+ optional fakeDocker-flavored integration case) | High | P1 |

### Story 3: Ephemeral-Copy Setup Injection

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Command-Mode Shell-Wrap Injection | Unit | High | P1 |
| 03-02 | Script-Mode Stdin-Prepend Injection | Unit | Medium | P1 |
| 03-03 | No-Interpolation Injection-Safety Invariant | Unit (adversarial/security-focused) | High | P1 |

### Story 4: `--auto-fix` Opts Into the Writable Overlay

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Auto-Fix Validation Requests the Writable Overlay | Unit | Low | P1 |
| 04-02 | `Writable` Flag Is Pinned by Test, `--exec`/Preflight Stay Read-Only | Unit (+ manual control-group review) | Medium | P1 |

### Story 5: Regression Proof and Documentation Parity

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Writable:true Argv/Stdin Shape Tests for Both RunSpec Modes | Unit | Medium | P2 |
| 05-02 | fakeDocker-Based Proof a Script Can Write Under /work | Unit (functional simulation) | Medium | P1 |
| 05-03 | Writable:false Regression Test Anchor Stays Unmodified | Unit | Low | P1 |
| 05-04 | docs/auto-fix.md and autofix_exec.go Doc-Comment Parity Rewrite | Documentation (manual/review-based) | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 13 ACs require unit tests (`internal/sandbox/*_test.go`, `internal/verify/*_test.go`), all runnable without a real Docker daemon via the existing `writeFakeDocker`/`fakeDockerRecording` shim patterns.
- **Integration Tests:** 1 AC (01-03) requires a full-package regression run confirming the entire existing `internal/sandbox` and `internal/tools` (`--exec`) suites stay green with zero behavior change.
- **Documentation:** 1 AC (05-04) is prose-only — verified by manual/grep-based review, not an automated test.
- **High Complexity:** 4 ACs marked high complexity (02-02, 02-03, 03-01, 03-03) — all cluster around the two riskiest mechanisms in the plan: giving `/work` real writable backing under a read-only rootfs, and shell-wrapping Command-mode exec without reintroducing an injection surface.
