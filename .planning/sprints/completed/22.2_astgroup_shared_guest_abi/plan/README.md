# Plan 22.2: Extract Shared Wasm Guest ABI

## Overview
Extracts the duplicated alloc/free/emit/pins Wasm guest ABI boilerplate — currently copy-pasted across `goparser`, `pyparser`, and `braceparser` — into one shared, isolated Go module, and pins the non-moving-GC pointer-packing assumption in a single documented location instead of three.

## Workflow Status
- [x] **Plan Created**
- [x] **Tasks** - `/create-tasks @.planning/plans/active/22.2_astgroup_shared_guest_abi/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/22.2_astgroup_shared_guest_abi/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/22.2_astgroup_shared_guest_abi/`

## Timeline & Milestones
Estimated 1 day (per source epic plan 22.2). Single-session mechanical refactor: create shared module → wire three parsers → verify build/tests.

## Resource Requirements
One engineer. No new infrastructure, dependencies, or CI changes — pure Go refactor using existing stdlib (`unsafe`, `encoding/json`).

## Expected Outcomes
- One shared `guestabi` module replaces three duplicated ~29-line ABI blocks.
- The non-moving-GC pointer-packing assumption documented once instead of three times.
- `goparser`, `pyparser`, `braceparser` `go build`/tests unaffected in behavior.

## Risk Summary
Low risk, mechanical extraction. Main hazards are `go.mod` replace-directive misconfiguration and `wasmexport`'s package-main requirement — both mitigated by per-parser incremental verification (see plan.md Risk Mitigation).

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Tasks](tasks/)
- [Sprint Design](sprint-design.md)
