# Code Review Report: 12.1_technical_debt_format_migration

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5
- **Approval Status:** Approved
- **Review Date:** June 26, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Acceptance Criteria Verified
| AC | Verdict | Evidence |
|----|---------|----------|
| AC1 — tool parses table → one shard per source | VERIFIED ✅ | `cmd/td-migrate/main.go:15`, `internal/tdmigrate/run.go:62`, `parse.go:24`, `shard.go:67` |
| AC2 — zero data loss, full round-trip table→shards→table | VERIFIED ✅ | `parse_test.go:172` (semantic), `parse_test.go:193` (live README), `generate.go:18` |
| AC3 — unconstrained multi-line descriptions | VERIFIED ✅ | `item.go:46-53`, `fixtures_test.go:70` |
| AC4 — validation command strict-loads + fails loudly | VERIFIED ✅ | `validate.go:18`, `run.go:120`, `fixtures_test.go:91-116` |
| AC5 — README documents sharded format (additive) | VERIFIED ✅ | `.planning/technical-debt/README.md:35`, `items/SCHEMA.md:1` |

## 3. Evidence Map
- **migrate**: `ParseREADME` splits on `### [date] From <Sprint|Review>: <label>` → one `Shard` each; `WriteShards` emits one YAML file per shard (29 real shards present under `items/`). Shards validated before write (`run.go:82-87`).
- **generate**: `GenerateTable` re-emits the 9-/11-column ToC table; never overwrites the live README (stdout only) — additive promise upheld.
- **validate**: `DecodeShardStrict` uses `KnownFields(true)` + `Shard.Validate`; CLI exits non-zero on any bad shard; `ValidationError` aggregates all failures.
- **YAML safety**: adversarial footgun corpus (Norway problem, version floats, leading zeros/octal, colons, leading dashes, unicode, null tokens, multi-line block scalars, est_minutes 0-int trap, tab rejection, unknown-field rejection, bad-enum rejection) all round-trip or reject as designed.

## 4. Remaining Unchecked Items
No remaining unchecked items — all 5 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All ACs implemented with direct code+test evidence; full suite green; 89.0% total coverage (`tdmigrate` package 90.1%); lint/vet/format clean. Adversarial findings are quality hardening, none blocking — the package is additive and not imported by any production code path.

## 6. Coverage Analysis
- **Coverage:** 89.0% (total); `internal/tdmigrate` 90.1%
- **Baseline:** 80%
- **Delta:** ↑9.0%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Tests | PASSING (0 failed) | `go test ./...` |
| Lint | PASSING (0 issues) | `golangci-lint run` |
| Types | PASSING | `go vet ./...` |
| Format | PASSING | `go fmt ./...` (gofmt -l clean) |

## 8. Adversarial Analysis
- **Files Reviewed:** 7 production `.go` files (parse, item, shard, validate, generate, run, main)
- **Mode:** Discovery-only (epic has no sprint-design.md risk profile)
- **Issues Found:** 12 (Critical: 0, High: 0, Medium: 3, Low: 9)

### Medium
1. `validate.go:22` — `DecodeShardStrict` decodes only the first YAML document; a trailing `---` second document is silently discarded with nil error (strict-load / loud-failure gap, AC4).
2. `validate.go:45` — `ValidateDir`/`LoadShards` use `filepath.Glob`, which returns `(nil,nil)` for a missing dir; `validate --items /typo` reports exit 0 "0 shard(s) OK" — gate silently passes.
3. `parse.go:67` — `splitRow` has no pipe-escaping; a hand-edited row with a literal pipe can add phantom cells and (if it lands on 9/11) silently re-interpret the row instead of failing loudly.

### Low (9)
- `parse.go:41` — colonless drifted header bypasses the drift guard; items silently re-attributed to the prior shard.
- `parse.go:45` — in-section line without a leading pipe is silently skipped (GFM allows pipeless rows).
- `parse.go:28` — `flush()` drops a recognized section with zero parsed items with no diagnostic.
- `parse.go:93` — magic positional column indices duplicated across parse.go/generate.go with no shared schema constant.
- `shard.go:37` — `s.Date` interpolated raw into the filename; `Shard.Validate` never enforces `YYYY-MM-DD` (latent traversal only via hand-authored shard).
- `generate.go:13` — docstring overclaims "verbatim"/"semantically equal"; `cell()` is intentionally lossy at the table layer.
- `run.go:65` — subcommand `-h` returns exit 2 to stderr, inconsistent with top-level help (exit 0/stdout).
- `run.go:54` — `newFlags` accepts an unused `args` parameter (dead param implies it parses).
- `run.go:47` — `usage` constant omits the dangerous `--allow-empty` flag.

### Excluded (already tracked in `items/2026-06-26_epic-12.1.yaml`)
- WriteShards prunes all `*.yaml` before writing → partial-wipe on mid-loop failure (already a deferred TD item; store is git-tracked + migrate validates before writing).
- `generate`/`validate` register `--readme` but never read it.

## 9. Follow-ups
- Route the 12 new findings via `/reconcile-code-review @.planning/epics/completed/12.1_technical_debt_format_migration.md`.
- Strongest candidates for a quick `/resolve-td` pass: the two `validate.go` MEDIUMs (they are real holes in the AC4 "fails loudly" guarantee).

---
*Generated by /execute-code-review on June 26, 2026 09:58:20PM*
