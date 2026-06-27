# Code Review Stream - 12.1_technical_debt_format_migration (Epic)

**Started:** June 26, 2026 09:58:20PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests if enabled]

---

## Acceptance Criteria Findings

### Criterion: AC1 — A tool parses the current Markdown table and writes one structured shard file per source.
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/td-migrate/main.go:15`, `internal/tdmigrate/run.go:62` (runMigrate), `internal/tdmigrate/parse.go:24` (ParseREADME), `internal/tdmigrate/shard.go:67` (WriteShards)
- **Notes:** `migrate` reads the README, ParseREADME splits on `### [date] From <Sprint|Review>: <label>` into one Shard per section, WriteShards emits one `*.yaml` per shard. 29 real shards present under `items/`.

### Criterion: AC2 — All existing items migrated with zero data loss (full round-trip table → shards → table).
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/tdmigrate/parse_test.go:172` (TestGenerateTable_SemanticRoundTrip), `internal/tdmigrate/parse_test.go:193` (TestLiveREADME_SemanticRoundTrip), `internal/tdmigrate/generate.go:18` (GenerateTable)
- **Notes:** Live README round-trip asserts `reflect.DeepEqual(canonicalize(orig), canonicalize(reparsed))` — semantic equivalence (item set + field values), matching the epic's Clarification that byte-identity is explicitly NOT the bar.

### Criterion: AC3 — New format supports unconstrained multi-line descriptions.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/tdmigrate/item.go:46-53` (Problem/Fix/Notes string fields), `internal/tdmigrate/fixtures_test.go:70` (TestFootgun_MultilineBlockScalar)
- **Notes:** yaml.v3 Marshal emits block scalars; the multi-line fixture proves `"first line\nsecond line\n\nfourth..."` survives marshal → strict-decode unchanged.

### Criterion: AC4 — A validation command strict-loads and schema-checks every shard; malformed fails loudly.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/tdmigrate/validate.go:18` (DecodeShardStrict, KnownFields(true)), `internal/tdmigrate/run.go:120` (runValidate exits 1), `internal/tdmigrate/fixtures_test.go:91-116` (tab/unknown-field/bad-enum rejection)
- **Notes:** ValidationError aggregates all failures in one pass; CLI returns non-zero on any bad shard.

### Criterion: AC5 — README updated to document and point to the sharded format (additive; table authoritative).
- **Verdict:** VERIFIED ✅
- **Evidence:** `.planning/technical-debt/README.md:35` ("## Sharded Storage Format (`items/`) — additive, Epic 12.1"), `.planning/technical-debt/items/SCHEMA.md:1`
- **Notes:** New additive docs section + per-shard schema doc; existing table preserved unchanged as authoritative source.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — epic has no sprint-design.md risk profile)
**Files Reviewed:** 7 production .go files (parse, item, shard, validate, generate, run, main)
**Issues Found:** 12 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 12

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 3
- Low: 9

### Dedup note (excluded from routing — already tracked)
Two adversarial findings were already captured by `/execute-epic` into `items/2026-06-26_epic-12.1.yaml` (and the README table) and were NOT re-routed:
- WriteShards prunes all `*.yaml` before writing → partial-wipe on mid-loop failure (two reviewers flagged it CRITICAL/MEDIUM; existing item rates it LOW, deferred — store is git-tracked + runMigrate validates before writing, so honest severity is MEDIUM at most).
- `generate`/`validate` register `--readme` but never read it (silently accepted/ignored).
