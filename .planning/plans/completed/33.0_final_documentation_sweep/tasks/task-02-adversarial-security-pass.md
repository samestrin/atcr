# Task 02: Adversarial/Security Pass — Manual Review for Secrets, Dead Code, Unsafe Patterns, TODO/FIXME

**Source:** Plan 33.0 – Debt Item #2
**Priority:** P1 | **Effort:** M | **Type:** Fix

## Problem Statement
Going public (Epic 33.2) exposes the entire `atcr` codebase and git history to the world. Plan 33.0's Proposed Solution step 1 requires "a thorough adversarial/human pass, with emphasis on security and anything embarrassing to expose publicly (secrets, TODO/FIXME debt, dead code, unsafe patterns)" in addition to the automated multi-agent reviewer run (Task 1). Task 1 is tool-driven and scoped to `cmd/`, `internal/`, `reconcile/`, `skill/`; it will not catch issues living outside those directories — config files, CI workflows, scripts, examples, planning/tooling directories, and git history itself. `plan.md`'s Technical Planning Notes flag that "a shallow pre-scan found none of the latter [TODO/FIXME] in non-test files, but the full pass must confirm this rather than rely on the pre-scan" — the pre-scan is not sufficient evidence for AC2. Without a real, repo-wide manual sweep, hardcoded credentials, leftover debug artifacts, unsafe command/path handling, or stale TODO/FIXME markers could ship into a public release with no final gate to catch them.

## Solution Overview
Conduct a manual, human-led adversarial security pass across the **entire repository** (broader scope than Task 1's four production directories) — including root-level config and CI files, `.github/workflows/`, `scripts/`-equivalent tooling (`examples/*.sh`), `.planning/`, `docs/`, `personas/`, `skill/`, and non-Go artifacts — plus an awareness pass over git history for previously committed secrets. The sweep targets four categories called out in the original request: (1) hardcoded credentials/API keys/tokens, (2) dead code, (3) unsafe patterns (command injection, path traversal, unsanitized shell/file operations), and (4) leftover TODO/FIXME/HACK/XXX debt. Every finding is logged with file:line, category, and a proposed severity (CRITICAL/HIGH/MEDIUM/LOW) so Task 3 (Findings Triage) can fix CRITICAL/HIGH directly and route MEDIUM/LOW to `.planning/technical-debt/README.md`. This task does not fix anything itself — it produces the evidence trail that proves AC2 rather than assuming it.

## Technical Implementation
### Steps
1. **Secrets/credentials sweep (repo-wide, working tree):** Grep the full repository (not just Go source) for hardcoded secret patterns — AWS keys (`AKIA[0-9A-Z]{16}`), generic API tokens (`sk-`, `ghp_`, `gho_`, `xox[baprs]-`), private key blocks (`-----BEGIN.*PRIVATE KEY-----`), and common credential variable names (`password\s*=`, `api[_-]?key\s*=`, `secret\s*=`, `token\s*=`) across `cmd/`, `internal/`, `reconcile/`, `skill/`, `personas/`, `docs/`, `.github/workflows/*.yml`, `action.yml`, `.goreleaser.yaml`, `examples/*.sh`, `examples/*.yaml`, and `.planning/`. Exclude `.git/`, `dist/`, `coverage.out`, `qdrant_fts.db`, and vendored/build artifacts.
2. **Committed secret-like files check:** Search for accidentally committed sensitive file types tracked by git — `git ls-files | grep -iE '\.(env|pem|key|p12|pfx)$|credentials|\.npmrc$|id_rsa'` — and confirm `.gitignore` (`.gitignore:1`) already excludes the expected patterns (`.env`, key files, etc.); flag anything tracked that shouldn't be.
3. **Git history awareness pass:** Run `git log --all -p | grep -inE '<same secret patterns as step 1>'` (or a bounded `git log -p -- <suspicious file>` if step 2 surfaces a candidate) to check whether a secret was committed and later removed but still lives in history. This is a scan for evidence, not a history rewrite — any hit is escalated to the user before any remediation (e.g. `git filter-repo`) is considered, since history rewrites are high-blast-radius and out of scope for this task.
4. **TODO/FIXME/HACK/XXX sweep (repo-wide, all file types):** Run `grep -rniE '(TODO|FIXME|HACK|XXX)([:( ]|$)' --include='*.go' --include='*.md' --include='*.yml' --include='*.yaml' --include='*.sh' .` scoped to exclude `.git/`, `dist/`, vendor directories, and generated files (`coverage.out`), across the entire repo — not just non-test Go files as the plan's shallow pre-scan did. Record every hit with file:line and surrounding context; classify each as either legitimate forward-looking debt (route to TD) or leftover debug/placeholder text that should be removed before public release.
5. **Dead code sweep:** Run `go vet ./...` and `staticcheck ./...` (or `golangci-lint run` per the repo's existing `.golangci.yml`) across the full module to surface unused exported/unexported symbols, unreachable code, and unused imports; supplement with a manual grep for large commented-out code blocks (`grep -rn '^[[:space:]]*//.*func \|^[[:space:]]*/\*' --include='*.go' .`) that automated linters may not flag as dead but are embarrassing debug scaffolding.
6. **Unsafe pattern sweep:** Grep for command-injection-prone constructs (`exec.Command`, `exec.CommandContext`, `sh -c`, `os/exec` usage with any argument built from string concatenation or `fmt.Sprintf` rather than a fixed argv slice) and path-traversal-prone constructs (`filepath.Join` fed with user/external input without a bounds check, `os.ReadFile`/`os.Open`/`os.WriteFile` on paths derived from untrusted input) across `cmd/`, `internal/`, `reconcile/`, `skill/`. Cross-reference `internal/security/pathguard.go` (existing sanitization pattern per `codebase-discovery.json` > existing_patterns) to confirm untrusted-path call sites route through it rather than bypassing it.
7. **Compile findings log:** For every hit from steps 1-6, record file:line, category (secret/dead-code/unsafe-pattern/todo-fixme), a one-line description, and a proposed severity (CRITICAL/HIGH/MEDIUM/LOW) using the same severity rubric Task 1's multi-agent reviewer uses. Write the compiled log to `.planning/plans/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt`, formatted as the **reconciled** `atcr-findings/v1` shape (same 9-column shape `atcr reconcile` produces for Task 1, per `docs/findings-format.md`) rather than the 8-column per-source shape, so Task 3 can merge both findings sets by direct concatenation without a format-conversion step:
   ```
   # atcr-findings/v1
   SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWERS|CONFIDENCE
   ```
   Set `REVIEWERS` to the fixed literal `adversarial-pass` and `CONFIDENCE` to `HIGH` for every row (a single manually-verified human source, per `docs/findings-format.md`'s reconciled-stream shape), so Task 3 can merge both findings sets into one triage pass.
8. **False-positive filter:** Before finalizing, cross-check each "sentinel"/"idiomatic" hit surfaced by the TODO/dead-code/unsafe-pattern greps against the documented legitimate Go-idiom usages (`internal/verify/severity.go`, `internal/security/pathguard.go`, `docs/payload-modes.md`, `docs/cross-examination.md` — sentinel error values / sentinel delimiter lines, not persona names) so this task's findings don't collide with or duplicate Task 5's persona-name verification work.

## Files to Create/Modify
- No production files are modified by this task — it is a scan/evidence-gathering pass. Any findings requiring code changes are logged for Task 3 (Findings Triage) to action, and CRITICAL/HIGH fixes land there, not here.
- `.planning/plans/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt` – created (this task's findings log, in reconciled `atcr-findings/v1` 9-column format; direct input to Task 3 Step 1)
- `.planning/technical-debt/README.md` – not written directly by this task; Task 3 is the write point for MEDIUM/LOW findings this task surfaces.

## Documentation Links
- [Multi-Agent Review Workflow](../documentation/multi-agent-review-workflow.md)
- [Technical Debt Triage & Resolution](../documentation/technical-debt-triage-resolution.md)

## Related Files (from codebase-discovery.json)
- `personas/retired_slugs_test.go`
- `internal/verify/severity.go`
- `internal/security/pathguard.go`
- `docs/payload-modes.md`
- `docs/cross-examination.md`
- `.planning/technical-debt/README.md`

## Success Criteria
- [ ] A repo-wide grep sweep for hardcoded secrets/credentials (AWS keys, API tokens, private key blocks, credential variable assignments) has been run and returned zero unresolved hits, or every hit is logged with file:line and remediated/escalated.
- [ ] `git ls-files` has been checked for accidentally-tracked sensitive file types (`.env`, `.pem`, `.key`, credentials files) with zero unexpected matches.
- [ ] A git history scan for the same secret patterns has been run and any hit is documented and escalated to the user (no automated history rewrite performed).
- [ ] A repo-wide TODO/FIXME/HACK/XXX sweep (all file types, not just non-test Go files) has been run and every hit is logged with a legitimate-debt-vs-remove classification, confirming or correcting the plan's shallow pre-scan.
- [ ] `go vet ./...` and the repo's configured linter (`golangci-lint run` per `.golangci.yml`) have been run clean or with every finding logged, plus a manual check for large commented-out dead code blocks.
- [ ] A grep-based sweep for command-injection and path-traversal-prone constructs in `cmd/`, `internal/`, `reconcile/`, `skill/` has been run and every hit is logged with a severity, confirming untrusted-path call sites route through `internal/security/pathguard.go` where applicable.
- [ ] All findings from this task are compiled into a single log at `.planning/plans/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt`, in reconciled `atcr-findings/v1` 9-column format (`REVIEWERS=adversarial-pass`, `CONFIDENCE=HIGH`), handed off to Task 3 for triage alongside Task 1's multi-agent reviewer findings.
- [ ] "Sentinel"/"idiomatic" hits are filtered against known legitimate Go-idiom usages before being logged as findings, avoiding duplication with Task 5's persona-name sweep.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
This is a manual review/scan task, not a code-change task — verification is reproducibility of the scan commands and completeness of the findings log, not automated unit/integration tests.

**Verification Commands (reproducible scan, run from repo root):**
- Secrets: `grep -rniE 'AKIA[0-9A-Z]{16}|sk-[a-zA-Z0-9]{20,}|ghp_[a-zA-Z0-9]{30,}|-----BEGIN[A-Z ]*PRIVATE KEY-----|(password|api[_-]?key|secret|token)\s*[:=]\s*["\x27][^"\x27]+["\x27]' --exclude-dir=.git --exclude-dir=dist --exclude=coverage.out --exclude=qdrant_fts.db .`
- Tracked sensitive files: `git ls-files | grep -iE '\.(env|pem|key|p12|pfx)$|credentials|id_rsa'`
- Git history secrets: `git log --all -p | grep -inE '<same secret patterns as above>'`
- TODO/FIXME sweep: `grep -rniE '(TODO|FIXME|HACK|XXX)([:( ]|$)' --exclude-dir=.git --exclude-dir=dist --exclude=coverage.out .`
- Dead code / lint: `go vet ./...` and `golangci-lint run`
- Unsafe patterns: `grep -rn 'exec.Command\|exec.CommandContext' --include='*.go' cmd/ internal/ reconcile/ skill/` and `grep -rn 'filepath.Join\|os.ReadFile\|os.WriteFile\|os.Open' --include='*.go' cmd/ internal/ reconcile/ skill/`

**Reproducibility Check:**
- Every command above must be re-runnable by a second reviewer against the same commit and produce the same finding set (or a documented explanation of any diff, e.g. new commits landing between runs).

**Findings Log:**
- `.planning/plans/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt` (file:line, category, description, proposed severity, in reconciled `atcr-findings/v1` 9-column format) is the deliverable artifact of this task, consumed directly by Task 3 Step 1.

## Risk Mitigation
- **Risk:** Grep-based secret patterns produce false positives (e.g., test fixtures with fake tokens, example config placeholders). **Mitigation:** Manually inspect every hit before logging it as a finding; distinguish real credentials from clearly-fake test/example values (e.g. `sk-test-...`, `<YOUR_API_KEY>`).
- **Risk:** A real secret is found in git history, requiring a destructive history rewrite (`git filter-repo`/`BFG`) to fully remove. **Mitigation:** This task only detects and logs; any history-rewrite remediation is escalated to the user for explicit approval before Task 3 or any later task acts on it, per the hard-wall rule that irreversible actions require confirmation.
- **Risk:** Overlap with Task 1's automated reviewer and Task 5's persona-name sweep produces duplicate findings. **Mitigation:** Step 8 filters known legitimate "sentinel"/"idiomatic" Go-idiom usages before logging, and Task 3's triage step is expected to deduplicate against Task 1's findings by file:line.
- **Risk:** "Entire repository" scope is open-ended and could balloon past estimate. **Mitigation:** Bound the sweep to the concrete directory/file list enumerated in Steps 1-6 (production dirs + root config/CI files + `.planning/` + `docs/` + `examples/`), excluding `.git/`, `dist/`, `coverage.out`, `qdrant_fts.db`, and other generated/binary artifacts.

## Dependencies
- None — this task can run in parallel with Task 1 (Multi-agent code review), since both feed independent findings into Task 3 (Findings Triage).

## Definition of Done
- All six sweep categories (secrets, tracked sensitive files, git history, TODO/FIXME, dead code, unsafe patterns) have been executed with their commands documented and reproducible.
- Every finding is logged with file:line, category, description, and proposed severity.
- Zero unresolved CRITICAL findings remain undocumented; any secret or credential found is escalated immediately rather than held until Task 3.
- The findings log is written to `.planning/plans/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt` in reconciled `atcr-findings/v1` format and is available for Task 3's triage pass.
- No production code, documentation, or config files were modified by this task itself.
