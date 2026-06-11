# Testing Guide

This document provides a comprehensive overview of the testing infrastructure for the AI Brand Voice Aligner project.

## Quick Start

### Standard Test Run (Default)

The standard test run skips infrastructure-dependent tests to ensure fast, reliable CI/CD:

```bash
cd app
npm run test:run
```

This runs all unit and integration tests that don't require external infrastructure.

### Comprehensive Test Run (All Tests)

To run the complete test suite including all infrastructure-dependent tests:

```bash
cd app
ENABLE_S3_TESTS=true \
ENABLE_LOAD_TESTS=true \
ENABLE_PERFORMANCE_TESTS=true \
ENABLE_PUPPETEER_TESTS=true \
npm run test:run
```

Or set these in your shell session:

```bash
export ENABLE_S3_TESTS=true
export ENABLE_LOAD_TESTS=true
export ENABLE_PERFORMANCE_TESTS=true
export ENABLE_PUPPETEER_TESTS=true
npm run test:run
```

## Test Categories

### 1. Unit Tests (Always Run)

Standard unit tests that run without external dependencies:

- Component tests
- Service tests (with mocked dependencies)
- Utility function tests
- Hook tests

### 2. Infrastructure-Dependent Tests (Opt-In)

These tests require external infrastructure and are skipped by default:

| Category | Env Variable | Required Infrastructure | Test Files |
|----------|-------------|-------------------------|------------|
| S3 Storage | `ENABLE_S3_TESTS=true` | MinIO/AWS S3 | `tests/viewport/screenshot-storage.test.ts` |
| Load Testing | `ENABLE_LOAD_TESTS=true` | None (timing-sensitive) | `tests/server/monitoring/loadTesting.test.ts` |
| Performance | `ENABLE_PERFORMANCE_TESTS=true` | Puppeteer, AI APIs | `tests/__tests__/performance/end-to-end-workflow.test.ts` |
| Puppeteer | `ENABLE_PUPPETEER_TESTS=true` | Browser automation | `tests/viewport/viewport-text-extractor.test.ts` |

### 3. Timing-Sensitive Tests

Some tests validate performance requirements and may have slight variance:

- Timer resolution tests (adjusted for ~5ms tolerance)
- Health check response time tests
- Performance overhead measurements

These are included in the standard run but have tolerance ranges to account for system load and timer resolution.

## Infrastructure Setup

### MinIO (S3-Compatible Storage)

For S3-dependent tests, MinIO must be running on orchestrator.lan:

```bash
# Verify MinIO is running
curl http://orchestrator.lan:4566/minio/health/live

# Access MinIO Console (optional)
open http://orchestrator.lan:4567
```

Configuration in `.env.server`:

```env
AWS_ENDPOINT_URL=http://orchestrator.lan:4566
AWS_S3_REGION=us-east-1
AWS_S3_IAM_ACCESS_KEY=minioadmin
AWS_S3_IAM_SECRET_KEY=minioadmin
AWS_S3_FILES_BUCKET=brand
```

### Puppeteer Tests

Puppeteer tests require a browser to be available:

```bash
# Install dependencies
npx playwright install chromium
```

## Test Patterns

### Making Tests Conditional

When adding tests that require external infrastructure, use this pattern:

```typescript
// At the top of the test file
const enableMyTests = process.env.ENABLE_MY_TESTS === 'true';
const describeConditional = enableMyTests ? describe : describe.skip;

describeConditional('My Infrastructure Tests', () => {
  it('should do something with infrastructure', async () => {
    // test code
  });
});
```

### Handling Timing-Sensitive Tests

For tests that measure timing:

```typescript
// Use performance.now() instead of Date.now() for sub-ms precision
const startTime = performance.now();
doSomething();
const duration = performance.now() - startTime;

// Add tolerance for timer resolution
expect(duration).toBeGreaterThanOrEqual(45); // Instead of 50
expect(duration).toBeLessThan(100);
```

## CI/CD Configuration

### GitHub Actions

The standard CI pipeline should use:

```yaml
- name: Run Tests
  run: |
    cd app
    npm run test:run
```

For comprehensive testing (e.g., nightly builds):

```yaml
- name: Run Comprehensive Tests
  run: |
    cd app
    ENABLE_S3_TESTS=true \
    ENABLE_LOAD_TESTS=true \
    ENABLE_PERFORMANCE_TESTS=true \
    ENABLE_PUPPETEER_TESTS=true \
    npm run test:run
```

## Test Statistics

Current test suite (as of Sprint 37.0):

- **Total Tests**: ~6,460
- **Passed (Standard Run)**: ~6,359
- **Skipped (Opt-In)**: ~101
- **Test Files**: 294 (6 skipped)
- **Duration**: ~2 minutes

## Troubleshooting

### Common Issues

1. **S3 Tests Failing**: Ensure MinIO is running on orchestrator.lan
2. **Puppeteer Timeouts**: Check browser installation with `npx playwright install`
3. **Timing Test Flakiness**: These tests have tolerance built in, but may occasionally fail under heavy system load

### Useful Commands

```bash
# Run specific test file
npm run test:run -- tests/path/to/file.test.ts

# Run tests matching pattern
npm run test:run -- --testNamePattern="pattern"

# Run with verbose output
npm run test:run -- --reporter=verbose
```

## Related Documentation

- [Wasp Development Patterns - Testing](../.planning/specifications/wasp-development-patterns/testing-patterns.md)
- [CLAUDE.md - Testing Standards](../app/CLAUDE.md)
