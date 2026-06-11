# Frontend Test Execution Plan

**Created**: 2025-12-09
**Updated**: 2025-12-09
**Purpose**: Comprehensive plan for executing manual frontend tests using DevTools MCP + Puppeteer MCP

## Executive Summary

| Metric | Value |
|--------|-------|
| Total Test Plans | 15 |
| Total Test Cases | 357 |
| P1 (Critical) | 144 |
| P2 (Important) | 155 |
| P3 (Nice-to-have) | 58 |
| Execution Phases | 6 |

---

## Test Plan Format Standard

All test plans use a consistent **Phase.Task numbering format** aligned with sprint-plan.md documents.

### Format Structure

```markdown
## [ ] Phase 1 - [Phase Name]

**Description**: [What this phase tests]
**Blocker**: Yes|No - [reason if blocker]

### 1.1 [ ] **TC-01: [Test Case Title]**

**Priority**: P1|P2|P3
**Blocker**: true|false
**Related**: [../plan/acceptance-criteria/XX-XX-file.md](link)
**Tags**: [comma-separated tags]

#### Prerequisites

| Key | Value |
|-----|-------|
| User | BASIC|PRO|ADMIN|NONE |
| State | [Required state] |
| Viewport | Desktop (1280x800) |
| Start URL | /path |

#### Steps

1. [ ] [navigate] /path
2. [ ] [wait] page-load
3. [ ] [assert] element-exists | selector
4. [ ] [capture] description

#### Expected Results

- [ ] [blocker] Critical result
- [ ] Non-blocker result
```

### Referencing Tests

Tests can be referenced as:
- **Full reference**: "Phase 1 TC-01" or "1.1 TC-01"
- **Short reference**: "TC-01" (unique across all tests)
- **Step reference**: "TC-01 Step 3"

### Phase Summary Table

Each test plan includes a Phase Summary table for tracking:

```markdown
| Phase | Test Cases | Blockers | Status |
|-------|------------|----------|--------|
| Phase 1 - [Name] | 1.1-1.2 (TC-01, TC-02) | X | [ ] |
| Phase 2 - [Name] | 2.1-2.3 (TC-03 to TC-05) | X | [ ] |
```

---

## Execution Order

Tests are organized into 6 phases based on dependencies and feature flow. Execute phases sequentially; tests within a phase can be run in any order.

### Phase 0: MVP Smoke Tests (14 tests)
*Quick validation of core MVP user journey before running feature-specific tests*

| Order | Sprint | Name | Tests | P1 | Key Routes |
|-------|--------|------|-------|----|------------|
| 0 | MVP | MVP Smoke Tests | 14 | 11 | /login, /dashboard, /review |

**Run this phase first to validate basic functionality is operational.**

### Phase 1: Foundation (31 tests)
*Establishes basic navigation, dashboard access, and core filtering patterns*

| Order | Sprint | Name | Tests | P1 | Key Routes |
|-------|--------|------|-------|----|----|
| 1 | 55.0 | Limit Enforcement UI | 11 | 7 | /dashboard, /admin |
| 2 | 21.0 | Email & Filter UI | 20 | 9 | /analysis/multi-page |

### Phase 2: Components (77 tests)
*Tests core UI components that other features depend on*

| Order | Sprint | Name | Tests | P1 | Key Routes |
|-------|--------|------|-------|----|----|
| 3 | 11.3 | Visual Analysis Components | 29 | 16 | /visual-analysis |
| 4 | 46.0 | Missing UI Components | 27 | 13 | /scan, /analytics |
| 5 | 53.0 | Component Integration | 21 | 11 | /content-scan |

### Phase 3: Core Features (103 tests)
*Tests main user workflows (templates, analysis, performance)*

| Order | Sprint | Name | Tests | P1 | Key Routes |
|-------|--------|------|-------|----|----|
| 6 | 36.0 | Voice Profile Templates | 29 | 14 | /templates, /onboarding |
| 7 | 15.0 | Multi-Page Analysis UI | 35 | 14 | /analysis/results |
| 8 | 28.0 | UI Performance | 19 | 7 | /projects/compare |
| 9 | 43.0 | UI Integration Fixes | 20 | 8 | /review/side-by-side, /admin |

### Phase 4: Background Processing (68 tests)
*Background jobs depend on Phase 2-3 components being functional*

| Order | Sprint | Name | Tests | P1 | Key Routes |
|-------|--------|------|-------|----|----|
| 10 | 44.0 | Background Jobs | 20 | 6 | /rewrite, /analysis |
| 11 | 52.0 | Background Jobs UI | 21 | 5 | /job-progress, /admin/dashboard |
| 12 | 35.0 | Progress Notifications | 27 | 14 | /progress-dashboard, /settings |

### Phase 5: Advanced Collaboration (54 tests)
*Collaboration features are the most complex, require all underlying systems working*

| Order | Sprint | Name | Tests | P1 | Key Routes |
|-------|--------|------|-------|----|----|
| 13 | 18.0 | Visual Collaboration | 13 | 9 | /visual-collaboration |
| 14 | 38.0 | Agency Collaboration Hub | 41 | 15 | /dashboard, /reporting |

---

## Test Users

All test users are seeded via `wasp db seed` command using the `seedFrontendTestUsers` function.

### Required Test Users

| User Role | Email | Tier | Admin | Used In |
|-----------|-------|------|-------|---------|
| BASIC | testuser-basic@test.local | free | No | All sprints |
| PRO | testuser-pro@test.local | pro | No | Most sprints |
| ADMIN | testadmin@test.local | pro | Yes | 55.0, 52.0, 43.0, 35.0, 38.0 |
| AGENCY_OWNER | alex@agency.com | enterprise | Yes | 28.0, 38.0 |
| NEW_USER | newuser@test.local | free | No | 36.0 (onboarding tests) |
| TEAM_A | team-a@test.local | pro | No | 36.0 (team sharing tests) |
| TEAM_B | team-b@test.local | pro | No | 36.0 (team sharing tests) |
| AGENCY_EDITOR | editor@agency.com | enterprise | No | 38.0 |
| AGENCY_VIEWER | viewer@agency.com | enterprise | No | 38.0 |
| TEAM_MEMBER | member@agency.com | enterprise | No | 38.0 |
| CLIENT | client@example.com | free | No | 38.0 |

### User Setup Status
- [x] Test users seeded in database (`wasp db seed seedFrontendTestUsers`)
- [x] RBAC roles assigned correctly (automated in seed function)
- [x] Subscription tiers configured (automated in seed function)
- [ ] Test data created for each user (sprint-specific seed functions needed)

---

## Test Data Requirements

### Per-Sprint Requirements

| Sprint | Required Test Data |
|--------|--------------------|
| 55.0 | Usage data, industry keywords |
| 21.0 | Multi-page analysis with mixed scores |
| 11.3 | Visual analyses with all metrics |
| 46.0 | Completed scans with keywords |
| 53.0 | Scan results with preserved keywords |
| 36.0 | Voice profile templates (various types) |
| 15.0 | Multi-page analysis results |
| 28.0 | Large documents (5000+ words) |
| 43.0 | Documents for side-by-side comparison |
| 44.0 | Pending/completed background jobs |
| 52.0 | Multi-viewport job progress data |
| 35.0 | Active site analyses, notifications |
| 18.0 | Visual analyses with overlays |
| 38.0 | Workspaces, teams, client projects |

---

## Execution Approach

### Recommended: AI-Assisted Checklist Sessions

Frontend tests should be executed as **AI-assisted checklist testing sessions** using Claude Code with Chrome DevTools MCP and Puppeteer MCP.

#### Why This Approach?

**Why NOT convert to sprints/TDD:**
1. These are **validation tests**, not implementation tasks - they verify existing features work
2. No code changes needed - test execution is the deliverable
3. Sprint overhead (planning, user stories, acceptance criteria) adds unnecessary process
4. Tests already have acceptance criteria built into their expected results

**Why NOT fully automated:**
1. Tests require visual verification (screenshots, layouts, animations)
2. Many tests involve subjective quality checks
3. Test environment setup is complex (different user states, mock data)
4. DevTools MCP integration works best interactively
5. Issues discovered need human judgment for severity classification

**Why AI-assisted is optimal:**
1. Claude Code can execute tests via MCP tools systematically
2. Can capture screenshots, check console errors, verify network requests
3. Maintains consistency across sessions
4. Can track progress in markdown files in real-time
5. Faster iteration than pure manual testing
6. Can generate detailed test reports automatically

#### Execution Process

1. **Setup Session**
   ```bash
   # Terminal 1: Start the application
   cd app && wasp start

   # Terminal 2: Seed test users (run once)
   cd app && wasp db seed seedFrontendTestUsers

   # Then: Connect Chrome DevTools MCP
   ```

2. **For Each Test Plan**
   - Open test-plan.md file in Claude Code conversation
   - Execute tests step-by-step using DevTools MCP
   - Mark checkboxes as pass/fail in the file
   - Capture evidence screenshots via MCP
   - Note any failures/issues inline

3. **Evidence Collection**
   - Screenshots saved to `evidence/` folder via MCP
   - Console errors logged from DevTools
   - Network failures captured automatically
   - Results updated in test plan markdown

#### Session Tracking

Create a tracking file per session:
```
.planning/technical-debt/frontend-tests/sessions/
├── 2025-12-09-phase-0-mvp.md
├── 2025-12-10-phase-1.md
└── ...
```

Each session file should include:
- Date/time started
- Phase being executed
- Tests passed/failed count
- Issues discovered (with severity)
- Time taken

### Alternative Approaches Considered

| Approach | Pros | Cons | Verdict |
|----------|------|------|---------|
| **Technical Debt Sprints** | Formal tracking | Overhead for validation work | Not recommended |
| **TDD Sprints** | Good for new features | Tests already exist, no code to write | Not applicable |
| **Pure Manual Testing** | Simple | Slow, inconsistent, tedious | Not scalable |
| **Full Automation** | Repeatable | High setup cost, visual tests hard | Future consideration |
| **AI-Assisted Sessions** | Fast, consistent, smart | Requires MCP setup | **Recommended** |

### Prioritization Strategy

1. **Phase 0 (MVP Smoke)**: Run first, ~30 minutes
   - Validates core functionality works
   - Blocks other phases if critical failures found

2. **P1 Tests Only Pass**: Run all P1 tests across phases
   - 144 critical tests covering core functionality
   - Identifies major blockers quickly

3. **Full Phase Execution**: Complete each phase sequentially
   - Phase 0 → Phase 5 in order
   - Respects feature dependencies

---

## Quick Start Commands

### Start Testing
```bash
# Terminal 1: Start app
cd app && wasp start

# Terminal 2: Seed test users (once)
cd app && wasp db seed seedFrontendTestUsers

# Browser: Navigate to http://localhost:3000
# Connect DevTools MCP to Chrome
```

### Execute Tests via Slash Commands

```bash
# Create a new test plan from sprint artifacts
/create-frontend-tests .planning/sprints/completed/55.0_limit_enforcement_ui/

# Execute all tests in a plan
/execute-frontend-tests path/to/test-plan.md

# Execute specific test range
/execute-frontend-tests path/to/test-plan.md TC-01 through TC-05

# Execute with options
/execute-frontend-tests path/to/test-plan.md --capture=all --detail=verbose
```

### Run P1 Tests Only (Quick Validation)
Execute only P1 tests from each sprint for a quick validation pass (144 tests total).

### Run Single Phase
- Phase 0: ~30 minutes (14 tests) - MVP Smoke Tests
- Phase 1: ~1 hour (31 tests)
- Phase 2: ~2.5 hours (77 tests)
- Phase 3: ~3.5 hours (103 tests)
- Phase 4: ~2.5 hours (68 tests)
- Phase 5: ~2 hours (54 tests)

---

## Test Plan Locations

| Sprint | Path |
|--------|------|
| MVP | `.planning/technical-debt/frontend-tests/mvp-smoke-test-plan.md` |
| 55.0 | `.planning/sprints/completed/55.0_limit_enforcement_ui/frontend-tests/test-plan.md` |
| 21.0 | `.planning/sprints/completed/21.0_email__filter_ui_completion/frontend-tests/test-plan.md` |
| 11.3 | `.planning/sprints/completed/11.3_component_implementation/frontend-tests/test-plan.md` |
| 46.0 | `.planning/sprints/completed/46.0_missing_ui_components/frontend-tests/test-plan.md` |
| 53.0 | `.planning/sprints/completed/53.0_missing_ui_components/frontend-tests/test-plan.md` |
| 36.0 | `.planning/sprints/completed/36.0_voice_profile_templates/frontend-tests/test-plan.md` |
| 15.0 | `.planning/sprints/completed/15.0_multi-page_analysis_ui_enhancements/frontend-tests/test-plan.md` |
| 28.0 | `.planning/sprints/completed/28.0_ui_performance_optimization/frontend-tests/test-plan.md` |
| 43.0 | `.planning/sprints/completed/43.0_ui_integration/frontend-tests/test-plan.md` |
| 44.0 | `.planning/sprints/completed/44.0_background_jobs/frontend-tests/test-plan.md` |
| 52.0 | `.planning/sprints/completed/52.0_background_jobs_ui/frontend-tests/test-plan.md` |
| 35.0 | `.planning/sprints/completed/35.0_site_analysis_progress_notifications/frontend-tests/test-plan.md` |
| 18.0 | `.planning/sprints/completed/18.0_visual_collaboration/frontend-tests/test-plan.md` |
| 38.0 | `.planning/sprints/completed/38.0_agency_collaboration_hub/frontend-tests/test-plan.md` |

---

## MVP Coverage Gap Analysis

### Current Coverage
The 15 test plans now cover MVP and features from Sprint 11.0 onwards. Earlier MVP sprints (1.0-10.0) focused on:
- Foundation & basic rewrite flow (Sprint 1.0)
- Voice profile management (Sprint 2.0-3.0)
- Content scanning (Sprint 4.0-5.0)
- Export functionality (Sprint 3.5)
- RBAC system (Sprint 10.1)
- Logging infrastructure (Sprint 7.0)

### MVP Smoke Test Plan
A dedicated MVP smoke test covering core user journeys has been created:
1. User registration/login
2. Voice profile creation from URL
3. Content scan initiation
4. Rewrite review and accept/reject
5. Copy rewritten content
6. SEO keyword preservation

**Status**: [x] MVP smoke test plan created (`mvp-smoke-test-plan.md`)

---

## Notes

- All tests assume server running at `http://localhost:3000`
- Default viewport is Desktop (1280x800) unless otherwise specified
- Mobile viewport: 375x667
- Tablet viewport: 768x1024
- Evidence folder: `./evidence/` relative to test plan location
