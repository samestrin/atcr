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

## TD-003 — --format flag accepts any string at flag layer (LOW)
**Origin:** Phase 1, task 1.3 adversarial review, 2026-06-10
**File:** cmd/atcr/report.go:15
**Issue:** `--format` help promises md/json/checklist but no enum validation exists yet, so invalid values would only fail inside the future handler.
**Mitigation this sprint:** Task 3.37 (report renderers) implements invalid-format errors as part of its AC; this marker tracks that the flag-layer validation must land there.
**Fix in:** Phase 3, task 3.37 — typed enum value or PreRunE validation mapping to exit 2.
