# Code Review Stream - 1.3_project_registry_overlay (Epic)

**Started:** June 12, 2026 01:06:14PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: Fresh clone with project-level definitions runs `atcr review` end-to-end, no user-registry edits
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/review.go:76`, `internal/registry/overlay.go:103-130`, `internal/registry/overlay.go:76-93`
- **Notes:** `LoadReviewConfig` → `LoadMergedRegistry(regPath, root)` loads the user registry, overlays the optional `.atcr/registry.yaml` (absent/empty = no-op, nil overlay), validates the merged view, enforces the provider trust gate, applies defaults. A repo shipping providers+agents is self-contained; project agents on user providers need no trust, project providers need a one-time `atcr trust`.

### Criterion: Project agent same name as user agent fully shadows it; removal restores user definition
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/overlay.go:171-193`, `docs/registry.md:112`
- **Notes:** `mergeProject` performs whole-entry replacement keyed by name (`r.Providers[name]=p`, `r.Agents[name]=a`), no field-level merge, new names added, source restamped to `project`. Dropping the project entry restores the user definition. Covered by `overlay_test.go`.

### Criterion: Fallback chains spanning tiers validate at load (dangling refs + cycles fail fast, error names file)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/overlay.go:117-119,198-206`, `internal/registry/graph.go:30-69`, `internal/registry/attribution.go:48-67`
- **Notes:** `validateMerged` runs `validate()` + `ValidateFallbacks()` over the merged registry. `ValidateFallbacks` flags dangling refs (graph.go:43) and DFS cycle detection, preferring a project-tier node for attribution when a cycle spans tiers (graph.go:56-63). `attribute()` prefixes the defining file label (`registry.yaml` vs `.atcr/registry.yaml`).

### Criterion: Unknown keys in project registry file are load errors (strict parsing parity)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/overlay.go:85-92`
- **Notes:** `LoadProjectRegistry` decodes via `decodeStrictYAML` (KnownFields) — the same strict decoder used for the user registry — so unknown keys are load errors. `ProjectRegistry` reuses `Provider`/`AgentConfig` shapes incl. Epic 1.1 reserved fields. Empty file treated as no overlay.

### Criterion: Project providers cannot read secrets from repo — only api_key_env indirection, enforced by schema
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/config.go:29-32,151-169`
- **Notes:** `Provider` struct carries only `APIKeyEnv` + `BaseURL` — no field can hold a key value, so YAML has no slot for a secret. `validate()` requires `api_key_env`, enforces POSIX env-var name regex, and rejects a `base_url` embedding userinfo credentials. `ProjectRegistry` reuses this struct, so the schema invariant holds at the project tier.

### Criterion: Chosen trust mitigation implemented and tested (repo-defined provider cannot silently receive a key)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/trust.go:29-32,196-215`, `internal/registry/overlay.go:121-126`, `cmd/atcr/trust.go:22-95`, `cmd/atcr/main.go:106`
- **Notes:** `enforceProjectTrust` (invoked inside `LoadMergedRegistry`) blocks any project-tier provider whose sha256(base_url + NUL + api_key_env) pin is absent from `~/.config/atcr/trusted_providers.yaml`, returning a `usageError`-mappable `ErrUntrustedProvider`. `atcr trust` (registered in main.go) lists/authorizes; store saved atomically with 0600 perms. Loud `ProjectProviderBanner` after the gate passes. Tested by `trust_test.go`, `attribution_test.go`, `trust_test.go` (cmd).

### Criterion: Config provenance (project/user/embedded) visible in at least one inspection surface
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/doctor/render.go:30,40-44`, `internal/doctor/run.go:80,135`, `internal/doctor/resolve.go:91`, `cmd/atcr/doctor.go:58`
- **Notes:** `atcr doctor` table gained a `SOURCE` column; `AgentResult.Source` (omitempty) is populated from `reg.AgentTier(name)` (`user`/`project`). doctor.go loads via `LoadMergedRegistry` so provenance reflects the overlay. JSON output carries `source` too. Covered by `source_test.go`, `doctor_test.go`.

### Criterion: docs/registry.md documents overlay, merge semantics, trust model; all tests green
- **Verdict:** VERIFIED ✅ (docs) / tests pending Phase 4
- **Evidence:** `docs/registry.md:95-124,126-144,9-10,240`
- **Notes:** `## Project registry overlay` section, whole-entry shadowing merge semantics, trust model with `atcr trust` usage and re-pin-on-change rationale, updated precedence section, and SOURCE provenance note all present. "All tests green" verified in Phase 4 below.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Discovery-only (no sprint-design.md risk profile — epic review)
**Files Reviewed:** 13
**Issues Found:** 14 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 14

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 6
- Low: 8

### Notes
No critical or high-severity findings. Implementation is solid: the trust gate enforces before any key flows, validation runs over the merged view with tier-aware attribution, the schema permits no key value, and tests/lint/vet/format all pass. Findings are hardening (UTF-8 truncation, trust-store hash audit, network-error key scrubbing), error-message attribution polish, doc/comment accuracy, and DRY cleanups. Two reviewer hypotheses were checked against the code and dropped as non-issues: the doctor Serial-lane ordering concern (moot — ValidateAgainst rejects an agent listed in both lanes) and the pre/post-merge trust-hash divergence (safe — applyDefaults only mutates agent fields, never provider base_url/api_key_env).
