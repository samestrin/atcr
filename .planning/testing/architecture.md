# Testing Architecture

This document describes the distributed testing infrastructure across the homelab network.

## Network Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           DEVELOPMENT WORKFLOW                               │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│  m4-macmini.lan (DEV MACHINE) - 192.168.68.99                               │
│  ──────────────────────────────────────────────────────────────────────────  │
│                                                                              │
│  ┌──────────────────────────────────────┐  ┌──────────────────────────────┐ │
│  │  WASP APPLICATION SERVER             │  │  UNIT/INTEGRATION TESTS      │ │
│  │  ─────────────────────────────────── │  │  ───────────────────────────  │ │
│  │  Frontend: :3000                     │  │  Framework: Vitest 1.6+      │ │
│  │  Backend:  :3001                     │  │  Location: app/tests/        │ │
│  │  Network:  192.168.68.99:3000        │  │  Env: happy-dom              │ │
│  │                                      │  │  Files: ~400+ test files     │ │
│  │  Run: cd app && wasp start           │  │  Tests: ~8000+ tests         │ │
│  └──────────────────────────────────────┘  │                              │ │
│                                            │  Run: cd app && npm test     │ │
│  ┌──────────────────────────────────────┐  │       -- --run               │ │
│  │  PLAYWRIGHT E2E (LOCAL MODE)         │  │                              │ │
│  │  ─────────────────────────────────── │  │  Sharding (local):           │ │
│  │  Location: e2e-tests/                │  │  npm test -- --shard=1/4     │ │
│  │  Can connect to remote browser       │  │                              │ │
│  │  Run: cd e2e-tests && npx playwright │  └──────────────────────────────┘ │
│  └──────────────────────────────────────┘                                    │
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐   │
│  │  AUTOMATED FRONTEND TESTS (CLAUDE CLI via MCP)                        │   │
│  │  ──────────────────────────────────────────────────────────────────── │   │
│  │  Skill: /execute-frontend-tests                                       │   │
│  │  Plans: .planning/testing/automated/*/test-plan.md                    │   │
│  │  Results: .planning/testing/runs/YYYY-MM-DD/                          │   │
│  │  Uses: Chrome DevTools MCP -> browser.lan                             │   │
│  └───────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                    Uses network IP (192.168.68.99:3000)
                    NOT localhost (which would resolve on browser.lan)
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  browser.lan (REMOTE BROWSER) - 192.168.68.102                              │
│  ──────────────────────────────────────────────────────────────────────────  │
│  Hardware: Dell Precision 3420, Xeon E3-1270 v5, 32GB RAM, 512GB SSD        │
│  OS: Ubuntu Server (headless), 2.5GbE NIC                                   │
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐   │
│  │  CHROME BROWSER + DEVTOOLS PROTOCOL                                   │   │
│  │  ──────────────────────────────────────────────────────────────────── │   │
│  │  VNC Access: http://browser.lan:6080/vnc.html (password: browser123)  │   │
│  │                                                                       │   │
│  │  MCPs Hosted (via mcp-proxy):                                         │   │
│  │  ├─ Chrome DevTools MCP (PREFERRED)                                   │   │
│  │  │  - Direct browser control, viewport 1920x1080                      │   │
│  │  │  - Logs: /mnt/unraid/Development/logs/devtools-mcp.log                    │   │
│  │  │                                                                    │   │
│  │  └─ Playwright MCP (ALTERNATIVE)                                      │   │
│  │     - Full Playwright capabilities                                    │   │
│  │     - Output: /mnt/unraid/Development/playwright-output                      │   │
│  │     - Traces: auto-saved for debugging                                │   │
│  │                                                                       │   │
│  │  Used By:                                                             │   │
│  │  - Claude CLI /execute-frontend-tests                                 │   │
│  │  - GitHub Actions E2E job                                             │   │
│  │  - Playwright with PLAYWRIGHT_WS_ENDPOINT                             │   │
│  └───────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│  orchestrator.lan (SERVICES HOST)                                           │
│  ──────────────────────────────────────────────────────────────────────────  │
│                                                                              │
│  ┌───────────────────────────────────┐  ┌─────────────────────────────────┐ │
│  │  CHROMIUM SCREENSHOTS API (:8000) │  │  OTHER SERVICES                 │ │
│  │  ──────────────────────────────── │  │  ────────────────────────────── │ │
│  │  Purpose: Full-page screenshots   │  │  - Redis (:6379)                │ │
│  │  > 8000px (DevTools limit)        │  │  - Postgres (:5432)             │ │
│  │                                   │  │  - MinIO S3 (:4566)             │ │
│  │  curl browser.lan:8000/      │  │  - Turbo Cache (:9080)          │ │
│  │    screenshot?url=...&type=       │  │                                 │ │
│  │    full_page                      │  │                                 │ │
│  └───────────────────────────────────┘  └─────────────────────────────────┘ │
│                                                                              │
│  NOTE: "3 shards" run in GitHub Actions CI, NOT on orchestrator.lan         │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                           CI/CD WORKFLOW                                     │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│  GITHUB ACTIONS (self-hosted runners)                                        │
│  ──────────────────────────────────────────────────────────────────────────  │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐ │
│  │  JOB: structure-check                                                   │ │
│  │  ─────────────────────────────────────────────────────────────────────  │ │
│  │  - No test files in app/src/                                            │ │
│  │  - No __dunder__ directories                                            │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                         │                                                    │
│                         ▼                                                    │
│  ┌──────────────────────┐  ┌──────────────────────┐  ┌───────────────────┐  │
│  │  SHARD 1/3           │  │  SHARD 2/3           │  │  SHARD 3/3        │  │
│  │  ───────────────     │  │  ───────────────     │  │  ───────────────  │  │
│  │  npm run test:ci     │  │  npm run test:ci     │  │  npm run test:ci  │  │
│  │  --shard=1/3         │  │  --shard=2/3         │  │  --shard=3/3      │  │
│  │                      │  │                      │  │                   │  │
│  │  ~133 files          │  │  ~133 files          │  │  ~134 files       │  │
│  │  ~2600 tests         │  │  ~2700 tests         │  │  ~2700 tests      │  │
│  └──────────────────────┘  └──────────────────────┘  └───────────────────┘  │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐ │
│  │  JOB: e2e (parallel with unit tests)                                    │ │
│  │  ─────────────────────────────────────────────────────────────────────  │ │
│  │  PLAYWRIGHT_WS_ENDPOINT=ws://browser.lan:3001                           │ │
│  │  E2E_BASE_URL=http://m4-macmini.lan:3000                               │ │
│  │                                                                         │ │
│  │  Connects to:                                                           │ │
│  │  - Remote browser on browser.lan                                        │ │
│  │  - Wasp app on m4-macmini.lan (or starts locally if localhost)          │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐ │
│  │  JOB: test-summary                                                      │ │
│  │  ─────────────────────────────────────────────────────────────────────  │ │
│  │  Aggregates results, generates GITHUB_STEP_SUMMARY                      │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Summary Table

| Machine | Tests Run | Framework | When |
|---------|-----------|-----------|------|
| **m4-macmini.lan** | Unit/Integration (~8000) | Vitest | Local dev: `npm test` |
| **m4-macmini.lan** | E2E (local browser) | Playwright | `npx playwright test` |
| **browser.lan** | None (browser only) | Chrome DevTools/Playwright MCP | Via mcp-proxy |
| **orchestrator.lan** | Screenshots only | Chromium API | Full-page > 8000px |
| **GitHub Actions** | Unit shards (3x) | Vitest | On push/PR |
| **GitHub Actions** | E2E (remote browser) | Playwright | On push/PR |

## Key Insights

1. **All tests run on m4-macmini.lan** - orchestrator.lan hosts services, not test shards
2. **The 3 shards are GitHub Actions matrix jobs**, not separate machines
3. **browser.lan is browser-only** - it renders pages, doesn't run tests (32GB RAM, Ubuntu Server)
4. **Two E2E modes**: Local Playwright (autostart Wasp) vs Remote (connect to browser.lan)
5. **Claude CLI tests** use Chrome DevTools MCP -> browser.lan -> views app at 192.168.68.99:3000
6. **Shared storage** at /mnt/development accessible from both m4-macmini.lan and browser.lan

## Test Commands Reference

### Local Development (m4-macmini.lan)

```bash
# Unit tests
cd app && npm test -- --run                    # Full suite
cd app && npm test -- --run tests/client/      # Directory
cd app && npm test -- --run --shard=1/4        # Local sharding

# E2E tests (local browser)
cd e2e-tests && npx playwright test
cd e2e-tests && npx playwright test --ui       # Debug mode

# E2E tests (remote browser via browser.lan)
PLAYWRIGHT_WS_ENDPOINT=ws://browser.lan:3001 \
E2E_BASE_URL=http://192.168.68.99:3000 \
npx playwright test

# View browser session via VNC
open http://browser.lan:6080/vnc.html  # password: browser123
```

### Claude CLI Automated Tests

```bash
/execute-frontend-tests @.planning/testing/automated/1.0_mvp-smoke-tests/test-plan.md
```

### Screenshots (orchestrator.lan)

```bash
# Viewport screenshot
curl "http://browser.lan:8000/screenshot?url=http://192.168.68.99:3000" \
  -o screenshot.png

# Full-page screenshot (avoids 8000px DevTools limit)
curl "http://browser.lan:8000/screenshot?url=http://192.168.68.99:3000&type=full_page" \
  -o full-page.png
```

## Related Documentation

- [Testing Guidelines](./CLAUDE.md) - Test execution patterns and conventions
- [E2E Test Guidelines](../../e2e-tests/CLAUDE.md) - Playwright-specific guidance
- [Unit Test Guidelines](../../app/tests/CLAUDE.md) - Vitest patterns and mocking
- [Local Services](../specifications/local-services/README.md) - Docker services on orchestrator.lan
- [CI Workflow](../../.github/workflows/ci.yml) - GitHub Actions configuration
