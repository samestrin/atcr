# Code Review Report: 1.3_project_registry_overlay

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 8 / 8
- **Approval Status:** Approved
- **Review Date:** June 12, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Acceptance Criteria Verified

| # | Criterion | Verdict | Key Evidence |
|---|-----------|---------|--------------|
| 1 | Fresh clone with project definitions runs `atcr review` end-to-end, no user-registry edits | VERIFIED ✅ | `internal/fanout/review.go:76`, `internal/registry/overlay.go:103-130` |
| 2 | Project agent shadows same-named user agent; removal restores user definition | VERIFIED ✅ | `internal/registry/overlay.go:171-193`, `docs/registry.md:112` |
| 3 | Cross-tier fallback chains validate at load (dangling + cycles, error names file) | VERIFIED ✅ | `internal/registry/overlay.go:198-206`, `internal/registry/graph.go:30-69`, `internal/registry/attribution.go:48-67` |
| 4 | Unknown keys in project registry are load errors (strict parsing parity) | VERIFIED ✅ | `internal/registry/overlay.go:85-92` |
| 5 | Project providers cannot read secrets from repo — only `api_key_env` indirection, schema-enforced | VERIFIED ✅ | `internal/registry/config.go:29-32,151-169` |
| 6 | Trust mitigation implemented + tested (repo provider cannot silently receive a key) | VERIFIED ✅ | `internal/registry/trust.go:29-32,196-215`, `cmd/atcr/trust.go`, `cmd/atcr/main.go:106` |
| 7 | Config provenance visible in an inspection surface (doctor SOURCE column) | VERIFIED ✅ | `internal/doctor/render.go:30,40-44`, `internal/doctor/run.go:80`, `internal/doctor/resolve.go:91` |
| 8 | docs/registry.md documents overlay, merge semantics, trust model; all tests green | VERIFIED ✅ | `docs/registry.md:95-144`; full suite green (below) |

## 3. Evidence Map
- **Merged-registry load path:** `LoadMergedRegistry` (overlay.go:103) loads the user registry, overlays the optional `.atcr/registry.yaml` (absent/empty = nil overlay), validates the merged view, enforces the project-provider trust gate, then applies defaults. Wired into review via `fanout.LoadReviewConfig` → `internal/fanout/review.go:76` and into doctor at `cmd/atcr/doctor.go:58`.
- **Whole-entry shadowing:** `mergeProject` (overlay.go:171) replaces same-named entries wholesale and restamps source to `project`; new names added.
- **Trust gate:** `enforceProjectTrust` (trust.go:196) blocks any project-tier provider whose sha256(base_url + NUL + api_key_env) pin is absent from `~/.config/atcr/trusted_providers.yaml`; `atcr trust` authorizes; store saved atomically at 0600.
- **Provenance:** `AgentResult.Source` populated from `reg.AgentTier`, rendered as a `SOURCE` column and a `source` JSON field.

## 4. Remaining Unchecked Items
No remaining unchecked items — all 8 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** Implementation matches the epic's recorded design decisions (dedicated `.atcr/registry.yaml`, whole-entry merge, `atcr trust` hash-pinned gate + banner, doctor SOURCE column). Trust gate enforces before any key flows; schema permits no secret value; merged-view validation attributes errors to the defining file. No critical or high findings.

## 6. Coverage Analysis
- **Coverage:** 87.2%
- **Baseline:** 80%
- **Delta:** ↑7.2%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... (gofmt -l clean) |

## 8. Adversarial Analysis
- **Files Reviewed:** 13
- **Issues Found:** 14 (Critical: 0, High: 0, Medium: 6, Low: 8)
- **Mode:** Discovery-only (no sprint-design risk profile for epic review)

### Medium
1. `internal/registry/overlay.go:39` — strict overlay decoder rejects shared-settings keys with a cryptic parse error; add a targeted "settings belong in .atcr/config.yaml" message.
2. `internal/registry/config.go:152` — empty provider/agent name returns a plain error, misattributed by `attribute()` to the user `registry.yaml`; return `entryErrf` so the project file is named.
3. `internal/registry/trust.go:80` — `LoadTrustStore` never recomputes the stored hash against base_url+api_key_env, so the audit columns are decorative; recompute on load and reject mismatches.
4. `internal/registry/trust.go:196` — the (project-registry, trust-store) resolution is duplicated/implicitly coupled between the gate and the `atcr trust` command; extract a shared helper to prevent drift.
5. `internal/doctor/run.go:245` — `network_error` Detail surfaces raw `err.Error()` with no key redaction, relying on a comment; scrub at the llmclient boundary.
6. `cmd/atcr/trust.go:68` — `atcr trust --all <name>` silently ignores args, and a mid-list unknown name prints "trusting" lines without persisting; validate the full selection up front.

### Low
1. `internal/registry/overlay.go:30` — `EntrySource.Tier` doc lists non-existent `SourceEmbedded`.
2. `internal/registry/config.go:161` — base_url check relies on unparenthesized `&&`/`||` precedence; accepts opaque/dot-segment URLs.
3. `internal/registry/graph.go:84` — gray-node fallthrough labeled "fail closed" is unreachable and would fail silently.
4. `internal/doctor/render.go:40` — table renderer re-defaults Source to literal `"user"`, dead code duplicating `registry.SourceUser`.
5. `internal/doctor/run.go:250` — `bounded()` truncates by raw byte count, can split a multi-byte rune into invalid UTF-8.
6. `internal/registry/trust.go:110` — `Save()` comment overstates concurrency safety (atomic rename prevents corruption, not lost updates).
7. `internal/registry/trust.go:183` — provider line formatted 3× with inconsistent `->` / `→` glyphs; add one formatter.
8. `cmd/atcr/review.go:64` — trust banner prints before `PrepareReview` can abort and lists all defined (not roster-used) project providers; soften wording / filter.

Two reviewer hypotheses were checked and dropped as non-issues: doctor Serial-lane ordering (moot — `ValidateAgainst` rejects an agent in both lanes) and pre/post-merge trust-hash divergence (safe — `applyDefaults` mutates only agent fields).

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/1.3_project_registry_overlay.md` to merge these 14 findings into the technical-debt README, then `/resolve-td` for the quick wins (the LOW maintainability/doc items are ~5-15 min each).

---
*Generated by /execute-code-review on June 12, 2026 01:06:14PM*
