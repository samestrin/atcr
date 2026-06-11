# Frontend Test Guidelines - AI Brand Voice Aligner

This file provides testing-specific guidance for the `.planning/testing/` directory. For project-wide context, see the root `CLAUDE.md`.

> **Infrastructure Setup**: For browser.lan, MCP configuration, screenshot service, and shared storage documentation, see [TESTING-INFRASTRUCTURE.md](./TESTING-INFRASTRUCTURE.md).

## Purpose

This directory contains documentation for:
- Flaky test tracking and remediation
- Performance test threshold adjustments
- Mock coverage gaps
- Test infrastructure improvements

## Quick Reference

| Resource | Location |
|----------|----------|
| Infrastructure setup | [TESTING-INFRASTRUCTURE.md](./TESTING-INFRASTRUCTURE.md) |
| Test patterns | [app/tests/CLAUDE.md](../../app/tests/CLAUDE.md) |
| Wasp patterns | [specifications/wasp-development-patterns/testing-patterns.md](../specifications/wasp-development-patterns/testing-patterns.md) |

## Common Test Debt Categories

### 1. Performance Test Flakiness
**Problem:** Wall-clock timing assertions fail intermittently in CI due to variable system load.

**Patterns to Fix:**
```typescript
// BAD: Wall-clock timing assertion
expect(duration).toBeLessThan(100);

// GOOD: Test behavior, not speed
expect(results).toHaveLength(expectedCount);
expect(operation).not.toThrow();

// OR for true benchmarks: use *.bench.ts files with Vitest bench mode
```

### 2. External API Dependencies
**Problem:** Tests hitting real APIs (Gemini, etc.) fail due to rate limiting or network issues.

**Patterns to Fix:**
```typescript
// Mock external APIs for unit tests
vi.mock('../../src/server/services/geminiClient', () => ({
  analyzeWithGemini: vi.fn().mockResolvedValue({ result: 'mocked' }),
}));

// Use skipIf for integration tests that need real API
describe.skipIf(!process.env.GEMINI_API_KEY)('Integration Tests', () => { ... });
```

### 3. Async Timing Issues
**Problem:** Tests fail because async operations complete at different times.

**Patterns to Fix:**
```typescript
// Use waitFor for async assertions
await waitFor(() => {
  expect(element).toBeInTheDocument();
});

// Use fake timers for time-dependent code
beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());
```

### 4. Missing React Import
**Problem:** `React is not defined` errors in test environment.

**Fix:** Always include `import React from 'react'` in `.tsx` files.

## Debt Tracking Template

When adding new technical debt items, use this format:

```markdown
## TD-XXX: [Short Description]

**File(s):** `tests/path/to/file.test.ts`
**Category:** [Flaky | Missing Coverage | Infrastructure | Performance]
**Priority:** [High | Medium | Low]
**Created:** YYYY-MM-DD

### Problem
[Describe the issue]

### Root Cause
[Explain why this happens]

### Proposed Fix
[How to resolve it]

### Status
- [ ] Investigated
- [ ] Fix implemented
- [ ] Verified in CI
```

## Context Management (Critical)

Frontend test execution can exhaust Claude's context window. Follow these rules:

### Screenshot Strategy

**NEVER use Chrome DevTools MCP `take_screenshot` for evidence collection** - it returns base64 and consumes ~500KB+ per screenshot.

**Instead use Chromium Screenshots API** (saves to file, zero context):
```bash
curl "http://browser.lan:8000/screenshot?url=URL" -o evidence/filename.png
```

See [TESTING-INFRASTRUCTURE.md](./TESTING-INFRASTRUCTURE.md#screenshot-service-browserlan8000) for full details.

### Efficiency Rules

| Do | Don't |
|----|-------|
| One snapshot per test group | Snapshot per test case |
| `list_console_messages` once per page | Call repeatedly |
| Capture evidence only on failure | Capture for passing tests |
| Batch related tests | Navigate repeatedly |

## Reference Documentation

- [Testing Patterns](../specifications/wasp-development-patterns/testing-patterns.md)
- [app/tests/CLAUDE.md](../../app/tests/CLAUDE.md)
- [Network Error Testing Strategy](../../app/tests/CLAUDE.md#network-error-testing-strategy)
