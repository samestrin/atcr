# Tech Debt Captured — Sprint 1.0_atcr_core

## TD-001 — CI workflow guard inconsistency and redundant vet step (LOW)
**Origin:** Phase 1, task 1.3 adversarial review, 2026-06-10
**File:** .github/workflows/ci.yml:34
**Issue:** go.mod existence guards are dead code now that go.mod is committed, and two guard styles coexist (`[ -f go.mod ]` vs `hashFiles`). The standalone `go vet` step duplicates golangci-lint's built-in govet.
**Why accepted:** Cosmetic CI cleanup; behavior is correct, just redundant.
**Fix in:** Phase 5 docs/CI pass — drop the guards and the standalone vet step.

## TD-002 — coverage.out generated in CI but never consumed (LOW)
**Origin:** Phase 1, task 1.3 adversarial review, 2026-06-10
**File:** .github/workflows/ci.yml:49
**Issue:** CI generates a coverage profile but never uploads, thresholds, or reports it, implying coverage enforcement that does not exist.
**Why accepted:** Coverage gate (≥70%) is enforced locally in DoD validation; CI threshold wiring is a nice-to-have.
**Fix in:** Phase 5 — add a coverage threshold check or artifact upload to ci.yml.

## TD-004 — payload_mode / fail_on never enum-validated or case-normalized at any tier (MEDIUM)
**Origin:** Phase 1, task 1.30 gate review, 2026-06-10
**File:** internal/registry/precedence.go:53
**Issue:** `payload_mode: bogus` and `fail_on: bogus` resolve into Settings silently; docs use lowercase `--fail-on high` while the embedded default is `HIGH`. Downstream phases could each invent divergent validation.
**Mitigation this sprint:** Tasks 2.25 (payload-mode enum validation, lowercase-only) and 3.33 (fail-on threshold validated against enum before any I/O) are already planned to land exactly this validation centrally.
**Fix in:** Phase 2 task 2.25 and Phase 3 task 3.33.

## TD-005 — personas package exports raw strings without a template-data contract (MEDIUM)
**Origin:** Phase 1, task 1.30 gate review, 2026-06-10
**File:** personas/personas.go:5
**Issue:** Persona templates use 7 variables ({{.AgentName}}, {{.ScopeRule}}, {{.FileCount}}, {{.BaseRef}}, {{.HeadRef}}, {{.PayloadMode}}, {{.Payload}}) but no exported struct anchors them; renderer and templates can drift.
**Mitigation this sprint:** Task 2.33 (payload template vars) defines the typed template-data struct with missingkey=error; task 2.45 adds tests that all six embedded personas render against it.
**Fix in:** Phase 2 tasks 2.33 / 2.45.

## TD-006 — atcr init writes explicit defaults that mask the registry settings tier (LOW, kept by design)
**Origin:** Phase 1, task 1.30 gate review, 2026-06-10
**File:** internal/registry/project.go (DefaultProjectConfigYAML)
**Issue:** The generated config bakes payload_mode/timeout_secs/fail_on explicitly, so registry-tier user defaults never apply to initialized projects unless the user removes those lines.
**Why accepted:** AC 02-01 mandates the generated config contain all five top-level keys with these exact defaults. Users who want registry-tier inheritance can delete the lines; docs/registry.md will note this in the Phase 5 rewrite.
**Fix in:** Phase 5 docs — document the inheritance behavior in docs/registry.md.

## TD-003 — --format flag accepts any string at flag layer (LOW)
**Origin:** Phase 1, task 1.3 adversarial review, 2026-06-10
**File:** cmd/atcr/report.go:15
**Issue:** `--format` help promises md/json/checklist but no enum validation exists yet, so invalid values would only fail inside the future handler.
**Mitigation this sprint:** Task 3.37 (report renderers) implements invalid-format errors as part of its AC; this marker tracks that the flag-layer validation must land there.
**Fix in:** Phase 3, task 3.37 — typed enum value or PreRunE validation mapping to exit 2.

## TD-007 — merge-commit base uses first parent only, undocumented for non-2-parent merges (LOW)
**Origin:** Phase 2, task 2.3 adversarial review, 2026-06-10
**File:** internal/gitrange/resolver.go:99
**Issue:** `--merge-commit SHA` resolves base as `SHA^` (first parent). For an octopus merge, or when the user wants the merged-in branch's actual fork point, this can produce a surprisingly large range, and the behavior is not documented in the flag help.
**Why accepted:** `base = SHA^` is the AC-mandated decision-tree behavior (carried over verbatim); the common 2-parent merge case is correct. Refining for octopus merges is a v2 concern.
**Fix in:** Phase 5 docs — note the first-parent assumption in docs and `--merge-commit` flag help.

## TD-008 — resolveRef conflates a hard git failure with an invalid ref (LOW)
**Origin:** Phase 2, task 2.3 adversarial review, 2026-06-10
**File:** internal/gitrange/resolver.go:177
**Issue:** `resolveRef` treats any non-empty error OR empty stdout as `ErrInvalidRef`. A genuine git failure (corrupt object, I/O error) on `rev-parse --verify` would be mislabeled "does not resolve to a commit" rather than surfaced as an infrastructure error.
**Why accepted:** With `--verify --quiet` the dominant failure mode is a non-existent ref, which the AC requires be reported as an invalid-ref error; the mislabel only occurs on rare repo corruption.
**Fix in:** Phase 3+ — distinguish `err != nil` (wrap raw git error) from `out == "" && err == nil` (true invalid ref).
