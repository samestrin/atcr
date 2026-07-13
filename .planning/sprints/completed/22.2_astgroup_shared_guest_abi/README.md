# Sprint 22.2: Shared Guest ABI Extraction

**Plan Type:** 🔧 Tech-Debt (TASK-BASED)
**Complexity:** 3/12 (SIMPLE)
**Timeline:** 1 day · 3 phases
**Branch:** `feature/22.2_astgroup_shared_guest_abi`
**Execution Mode:** Continuous · Adversarial Review ENABLED 🎯 (inline-fix: CRITICAL/HIGH)

---

## Overview

Extract the duplicated Wasm guest ABI boilerplate — alloc/free/emit/pins — currently copy-pasted across `goparser`, `pyparser`, and `braceparser`, into one shared internal `guestabi` Go module, now that the project's own documented extraction threshold ("parser count > 2") has been crossed. Three TD rows (from sprints 13.1, 13.3, 13.4) flag the same duplication; the underlying non-moving-GC pointer-packing assumption is currently repeated per-parser instead of documented once in an authoritative location.

## Timeline

| Phase | Focus | Task | Est. |
|-------|-------|------|------|
| 1 | Foundation — Create Shared guestabi Module | Task 01 (AC1 foundation, AC2) | ~0.3 day |
| 2 | Integration — Wire goparser & pyparser to guestabi | Task 02 (AC1) | ~0.4 day |
| 3 | Completion & Validation — Wire braceparser, Rebuild, Full Verify | Task 03 (AC1, AC2, AC3) | ~0.3 day |

Each phase runs: task → fresh-subagent adversarial review → address findings → DoD.

## Expected Outcomes

- **AC1** — `goparser`, `pyparser`, and `braceparser` each import the shared `guestabi` package instead of defining their own alloc/free/emit/pins boilerplate.
- **AC2** — The non-moving-GC pointer-packing assumption is documented once, in `guestabi`, not repeated per-parser.
- **AC3** — `go build ./...` succeeds for all three parser Wasm modules; existing `internal/astgroup` tests pass unchanged.

## Risk Summary (top 3)

1. **`go.mod` replace misconfiguration.** A misconfigured `require`+`replace` pair could silently break one parser's wasm build. Mitigation: build and `go vet` each parser individually immediately after wiring it (Tasks 02/03), not once at the end after all are touched.
2. **Non-moving-GC assumption drift.** A future Go moving GC would silently break the `pins` pointer-packing trick. Mitigation: Task 01's core deliverable pins this assumption as a doc comment on `pins` in the single shared location, making it a documented, discoverable risk.
3. **Vendored `.wasm`/`SHA256SUMS` drift.** Binaries committed out of sync with the manifest would fail `TestEmbeddedParsersMatchManifest` in CI. Mitigation: Task 03's `git status` check confirms both are staged together.

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable phase/task plan (continuous + adversarial) |
| [metadata.md](metadata.md) | Plan + sprint tracking |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (0 referenced) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, risk analysis |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth |
| [plan/tasks/](plan/tasks/) | The 3 task specifications |
| [plan/documentation/](plan/documentation/) | Documentation source index (no specs matched) |

---

🎯 **Next:** `/refine-sprint @.planning/sprints/active/22.2_astgroup_shared_guest_abi/`
