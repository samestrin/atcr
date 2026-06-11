# TDD Task Templates

Reference templates for sprint-plan.md task generation. Select template based on plan type and TDD mode.

**Severity substitution:** The "With Adversarial" templates below contain `$INLINE_FIX_LIST` and `$DEFER_LIST` placeholders in the Action Required and REFACTOR blocks. `/create-sprint` substitutes these from the `--severity` flag at instantiation:
- Default (`--severity` omitted): `$INLINE_FIX_LIST = CRITICAL/HIGH`, `$DEFER_LIST = MEDIUM/LOW`
- `--severity=critical,high,medium`: `$INLINE_FIX_LIST = CRITICAL/HIGH/MEDIUM`, `$DEFER_LIST = LOW`
- `--severity=all`: `$INLINE_FIX_LIST = CRITICAL/HIGH/MEDIUM/LOW`, `$DEFER_LIST = (none — all severities fix inline)`

---

## Feature Plan Templates

### PRAGMATIC (Standard)

```markdown
### N.X [ ] **[Element - TDD](plan/user-stories/$FILE)**
   **Mode:** Pragmatic | **AC:** [AC](plan/acceptance-criteria/$AC_FILE)
   1. RED: Write failing tests, verify fail correctly
   2. GREEN: Implement minimal code (T1 after each change)
   3. COMMIT: `git commit -m "feat($SCOPE): implement (green)"`
   4. Expand: Add edge case -> implement -> commit (T1/T2)
   5. REFACTOR: Improve quality (T1)
   6. COMMIT: `git commit -m "refactor($SCOPE): clean up"`
   **Files:** `test | impl` | **Duration:** (estimate)
```

### PRAGMATIC (With Adversarial)

```markdown
### N.X [ ] **[Element - TDD](plan/user-stories/$FILE)**
   **Mode:** Pragmatic | **AC:** [AC](plan/acceptance-criteria/$AC_FILE)
   1. RED: Write failing tests, verify fail correctly
   2. GREEN: Implement minimal code (T1 after each change)
   3. COMMIT: `git commit -m "feat($SCOPE): implement (green)"`
   4. Expand: Add edge case -> implement -> commit (T1/T2)
   **Files:** `test | impl` | **Duration:** (estimate)

### N.X.A [ ] **[Element - ADVERSARIAL REVIEW (subagent)](plan/user-stories/$FILE)**
   **Changed Files:** [LIST FILES MODIFIED IN N.X]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in N.X — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: N.X`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM N.X]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - $INLINE_FIX_LIST found -> List issues for N.X.R, do NOT proceed until fixed
   - $DEFER_LIST found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### N.X.R [ ] **[Element - REFACTOR](plan/user-stories/$FILE)**
   1. Fix $INLINE_FIX_LIST issues from N.X.A (if any)
   2. Improve code quality (T1)
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor($SCOPE): address review + clean up"`
   **Duration:** (estimate)
```

### MODERATE (Standard)

```markdown
### N.X [ ] **[Element - RED](plan/user-stories/$FILE)**
   1. Analyze AC, identify testable units
   2. Write tests: happy path, edge cases, errors
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** (estimate)

### N.Y [ ] **[Element - GREEN + REFACTOR](plan/user-stories/$FILE)**
   GREEN: Minimal code to pass (T1), verify all pass (T2), COMMIT
   REFACTOR: Improve code and tests (T1), validate (T3), COMMIT
   **Files:** `impl` | **Duration:** (estimate)
```

### MODERATE (With Adversarial)

```markdown
### N.X [ ] **[Element - RED](plan/user-stories/$FILE)**
   1. Analyze AC, identify testable units
   2. Write tests: happy path, edge cases, errors
   3. Verify tests fail correctly
   **Files:** `tests` | **Duration:** (estimate)

### N.Y [ ] **[Element - GREEN](plan/user-stories/$FILE)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT
   **Files:** `impl` | **Duration:** (estimate)

### N.Y.A [ ] **[Element - ADVERSARIAL REVIEW (subagent)](plan/user-stories/$FILE)**
   **Changed Files:** [LIST FILES MODIFIED IN N.Y]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in N.Y — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: N.Y`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM N.Y]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - $INLINE_FIX_LIST found -> List issues for N.Z, do NOT proceed until fixed
   - $DEFER_LIST found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### N.Z [ ] **[Element - REFACTOR](plan/user-stories/$FILE)**
   1. Fix $INLINE_FIX_LIST issues from N.Y.A (if any)
   2. Improve code and tests (T1), validate (T3), COMMIT
   **Duration:** (estimate)
```

### STRICT (Standard)

```markdown
### N.X [ ] **[Element - RED](plan/user-stories/$FILE)**
   Write comprehensive tests, verify fail correctly
   **Files:** `tests` | **Duration:** (estimate)

### N.Y [ ] **[Element - GREEN](plan/user-stories/$FILE)**
   Minimal code, one test at a time (T1), verify all (T2), COMMIT
   **Files:** `impl` | **Duration:** (estimate)

### N.Z [ ] **[Element - REFACTOR](plan/user-stories/$FILE)**
   Improve quality, maintain green (T1), validate (T3), COMMIT
   **Duration:** (estimate)
```

### STRICT (With Adversarial)

```markdown
### N.X [ ] **[Element - RED](plan/user-stories/$FILE)**
   Write comprehensive tests, verify fail correctly
   **Files:** `tests` | **Duration:** (estimate)

### N.Y [ ] **[Element - GREEN](plan/user-stories/$FILE)**
   Minimal code, one test at a time (T1), verify all (T2), COMMIT
   **Files:** `impl` | **Duration:** (estimate)

### N.Y.A [ ] **[Element - ADVERSARIAL REVIEW (subagent)](plan/user-stories/$FILE)**
   **Changed Files:** [LIST FILES MODIFIED IN N.Y]

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in N.Y — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: N.Y`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM N.Y]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - $INLINE_FIX_LIST found -> List issues for N.Z, do NOT proceed until fixed
   - $DEFER_LIST found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### N.Z [ ] **[Element - REFACTOR](plan/user-stories/$FILE)**
   1. Fix $INLINE_FIX_LIST issues from N.Y.A (if any)
   2. Improve quality, maintain green (T1), validate (T3), COMMIT
   **Duration:** (estimate)
```

---

## Non-Feature Plan Template

### TASK-BASED

```markdown
### N.X [ ] **$PLAN_ICON $TASK_NAME**
   **Task:** $DESCRIPTION
   **Priority:** $PRIORITY | **Effort:** $EFFORT
   1. Understand issue, identify affected files
   2. Write tests (if applicable)
   3. Implement solution
   4. Verify, check for side effects
   5. Document changes
   **Success Criteria:** (from task)
   **Files:** (list) | **Duration:** (estimate)
```

---

## Phase-Boundary Gate (Gated Mode)

Use this template when `execution_mode: gated` is set in sprint-plan.md frontmatter. Insert one gate task as the LAST task of each phase, numbered sequentially AFTER the DoD task.

Example numbering: if phase 2's last DoD task is `### 2.5`, the gate is `### 2.6`.

```markdown
### N.LAST [ ] **Phase N - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase N (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase N gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase N (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Downstream phases can consume outputs without rework?
       - REGRESSION: Earlier-phase behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - $INLINE_FIX_LIST found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - $DEFER_LIST found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min
```
