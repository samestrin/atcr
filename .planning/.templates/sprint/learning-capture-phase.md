# Learning Capture Phase Template

Template for the pre-final phase in feature plan sprint-plan.md files.

---

## Pre-Final Phase: Learning Capture

$REMINDER_STRING

### Learning Capture [ ] **Analyze Implementation for Learnings**

**Purpose:** Identify implicit decisions, patterns, and conventions discovered during implementation to capture as project knowledge.

#### Step 1: Gather Implementation Context

`git diff main...HEAD --stat`

Review the diff to understand what was changed in this sprint.

#### Step 2: Analyze for Patterns

Look for these learning categories:
- **Architectural decisions** - Why code is structured this way
- **Pattern choices** - Consistent patterns across files
- **Integration approaches** - How components connect
- **Debugging insights** - Non-obvious problem solutions
- **Convention discoveries** - Project-specific naming or organization

#### Step 3: Extract Candidates (Inline Analysis)

Analyze the git diff output from Step 1 and identify learnings that are:
1. **Architectural decisions** - Why code is structured a certain way
2. **Pattern choices** - Consistent patterns used across files
3. **Integration approaches** - How components connect
4. **Debugging insights** - Solutions to non-obvious problems
5. **Convention discoveries** - Project-specific naming or organization

**Quality criteria:**
- Must be SPECIFIC to this codebase (not generic programming advice)
- Must have CONCRETE examples from the diff
- Must answer "why" not just "what"
- Skip cosmetic changes, single-occurrence patterns, and generic advice

For each learning found, generate a pipe-delimited entry:
`QUESTION|ANSWER|FILES|TAGS|CONFIDENCE`
- QUESTION: What decision was made? (max 150 chars)
- ANSWER: The pattern/decision and WHY (max 400 chars)
- FILES: Comma-separated file paths where pattern appears
- TAGS: Comma-separated domain tags
- CONFIDENCE: 0.0-1.0 score

Maximum 10 learnings. Only include entries with CONFIDENCE >= 0.7.
If no high-confidence patterns found, set result to `NO_HIGH_CONFIDENCE_PATTERNS|analysis complete`.

#### Step 4: Present Candidates for Confirmation

Display each learning candidate:
```
### [N] {QUESTION}

**Answer:** {ANSWER}
**Files:** {FILES}
**Tags:** {TAGS}
**Confidence:** {CONFIDENCE}

[ ] Capture this learning
```

#### Step 5: Store Confirmed Learnings

For each confirmed learning:
1. Generate knowledge ID: `kb-YYYYMMDD-<hash>`
2. Store to `.planning/.knowledge/` with sprint context
3. Update sprint-knowledge.yaml with created ID
4. Display: "Captured: $ENTRY_ID"

If KNOWLEDGE_ENABLED = false:
  Display: "Skipping learning capture - knowledge system not enabled"
  Display: "Run /knowledge --rebuild to enable"
