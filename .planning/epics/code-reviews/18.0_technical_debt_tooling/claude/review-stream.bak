# Code Review Stream - 18.0_technical_debt_tooling (Epic)

**Started:** July 03, 2026 06:15:03PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — `atcr debt list` outputs a cleanly formatted table to the terminal
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/debt.go:66-135`
- **Notes:** `newDebtListCmd`/`runDebtList` load records, apply filters + sort, then `renderDebtTable` writes an aligned `text/tabwriter` table (header SEVERITY/STATUS/GROUP/EST/FILE/CATEGORY/PROBLEM). Empty result prints "No matching technical-debt items."

### Criterion: AC2 — `atcr debt add` provides an interactive prompt to scaffold a new debt item
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/debt_add.go:104-113`, `cmd/atcr/debt_add.go:127-195`
- **Notes:** Flag-mode is the primary contract; the interactive wizard (`promptEntry`) engages only when required flags are absent AND stdin is a TTY (`debtStdinIsTTY`). Re-prompts required fields, seeds defaults, writes to README master via `debt.AppendItem` then regenerates shards.

### Criterion: AC3 — A dashboard generator can be hooked into a git pre-commit hook or CI pipeline
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/debt_dashboard.go:35`, `cmd/atcr/debt_dashboard.go:73-86`, `docs/technical-debt.md:87-134`
- **Notes:** `--check` drift mode (`checkDashboard`) exits non-zero when the on-disk DASHBOARD.md differs from a freshly-rendered (deterministic, timestamp-free) output. docs/technical-debt.md ships copy-paste pre-commit and GitHub Actions CI snippets; tracked `.githooks/` intentionally untouched. Registered in `newRootCmd` at cmd/atcr/main.go:197.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Discovery-only (no sprint-design.md — epic)
**Files Reviewed:** 7 production source files (2 parallel hostile-review agents)
**Issues Found:** 17 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 17

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 3
- Low: 13

**Top finding (HIGH):** `internal/debt/add.go:162` — `AppendItem` does an unsynchronized read-modify-write of the authoritative README with no flock, while the rest of the TD tooling serializes README writes (group_td mkdir-flock). Concurrent `atcr debt add` runs can clobber each other on a shared branch.
