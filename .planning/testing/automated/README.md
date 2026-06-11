# Automated Frontend Testing Infrastructure

## Environment Setup

### Network Architecture

| Machine | Role | Address |
|---------|------|---------|
| **m4-macmini.lan** | Application Server (Wasp) | localhost:3000 (local), 192.168.68.99:3000 (network) |
| **browser.lan** | Remote Browser (Chrome DevTools/Playwright MCP) | 192.168.68.102 |

> **VNC Access**: View browser sessions at http://browser.lan:6080/vnc.html (password: `browser123`)

### Key Insight: Remote Browser Access

The Chrome DevTools MCP connects to a browser running on **browser.lan** (192.168.68.102), NOT the local machine. This means:

- **DO NOT use `localhost:3000`** - this refers to browser.lan's localhost
- **USE the network IP** - `http://192.168.68.99:3000` for the Wasp server on m4-macmini.lan

### SSH Access to browser.lan

The browser.lan machine is accessible via SSH with **passwordless login**:

| Account | SSH Command | Purpose |
|---------|-------------|---------|
| `samestrin` | `ssh browser.lan` | All operations (Chrome, file access, process management) |

All browser services run under the `samestrin` user.

### Session Initialization (CRITICAL)

**At the start of EVERY new Claude Code session**, reset the Chrome browser to ensure a clean state:

```bash
# Step 1: Kill existing Chrome
ssh browser.lan "pkill -f chrome"

# Step 2: Wait and start Chrome with remote debugging
# IMPORTANT: --user-data-dir is REQUIRED for remote debugging to work
ssh browser.lan "DISPLAY=:99 nohup google-chrome --remote-debugging-port=9222 --user-data-dir=/tmp/chrome-devtools --no-first-run --disable-default-apps http://192.168.68.99:3000/ > /tmp/chrome.log 2>&1 &"

# Step 3: Run /mcp in Claude Code to reconnect Chrome DevTools MCP
```

This ensures:
- No stale browser state from previous sessions
- Clean DevTools MCP connection
- Browser window is visible in VNC
- Remote debugging port 9222 is active

**Verify browser is running and port is listening:**
```bash
ssh browser.lan "pgrep -f chrome && ss -tlnp | grep 9222"
```

### MCP Preferences

| MCP | Status | Notes |
|-----|--------|-------|
| **Chrome DevTools MCP** | Preferred | Direct browser control, better snapshots |
| **Playwright MCP** | Alternative | Full Playwright capabilities, traces to /mnt/development |

### Troubleshooting Chrome DevTools MCP

**Error: "The browser is already running..."**

1. Kill Chrome/Chromium on **browser.lan**:
   ```bash
   ssh browser.lan "pkill -f chrome"
   ```
2. Or run `/mcp` in Claude Code to reconnect

3. Remove stale locks (remote):
   ```bash
   ssh browser.lan "rm -f ~/.cache/chrome-devtools-mcp/chrome-profile/SingletonLock"
   ```

4. View browser via VNC:
   - Open http://browser.lan:6080/vnc.html (password: `browser123`)

**Browser not visible in VNC?**
```bash
# Restart Chrome with display set
ssh browser.lan "pkill -f chrome"
ssh browser.lan "DISPLAY=:99 nohup google-chrome --remote-debugging-port=9222 --user-data-dir=/tmp/chrome-devtools --no-first-run http://192.168.68.99:3000/ > /tmp/chrome.log 2>&1 &"
```

## Test Execution Prerequisites

### 1. Start Application Server (on m4-macmini.lan)

```bash
cd app && wasp start
```

Wait for:
- `Server listening on port 3001`
- `VITE ready`

Server will show network URLs like:
- `http://192.168.68.99:3000/` (use this for remote browser)

### 2. Verify Browser Connection

```bash
# In Claude Code, test DevTools MCP:
mcp__chrome-devtools__list_pages
```

### 3. Handle Cookie Consent

On first page load, accept the cookie consent banner before running tests.

## Test Users

### Email Verification Challenge

The app requires email verification for new registrations. Options for testing:

1. **Register via UI** then verify in database:
   ```sql
   UPDATE "Auth" SET "isEmailVerified" = true 
   WHERE id IN (SELECT "authId" FROM "User" WHERE email = 'test@example.com');
   ```

2. **Use pre-seeded users** (if configured):
   ```bash
   cd app && wasp db seed seedFrontendTestUsers
   ```
   
   **Known Issue (Sprint 88.0):** Seeded users lack Auth/AuthIdentity records, causing login failures.

### Test User Credentials

| Email | Password | Notes |
|-------|----------|-------|
| testuser-basic@test.local | TestPassword123! | Basic tier user |
| testrun-YYYYMMDD@test.local | TestPassword123! | Auto-created during test runs |

## Running Tests

### Execute Full Test Plan

```bash
/execute-frontend-tests @.planning/testing/automated/1.0_mvp-smoke-tests/test-plan.md
```

### Execute Specific Tests

```bash
/execute-frontend-tests @.planning/testing/automated/1.0_mvp-smoke-tests/test-plan.md TC-01 through TC-05
```

## Test Output Location

```
.planning/testing/runs/YYYY-MM-DD/[test-plan-name]/
├── audit-report.md
├── TC-01-results.md
├── TC-02-results.md
└── evidence/
    ├── TC-01-final.png
    └── TC-02-failure-step-3.png
```

## Common Issues

### "Running" stuck on page load

- Symptom: Browser shows only "Running" text
- Cause: Using `localhost` instead of network IP
- Fix: Use `http://192.168.68.99:3000` (or current server IP)

### Screenshot save failures

- Symptom: `ENOENT: no such file or directory` when saving screenshots
- Cause: Path mismatch between local and remote filesystems
- Fix: Take screenshots without file path (returns inline), save manually if needed

### Full-page screenshot size limit exceeded

- Symptom: `API Error: 400 ... image dimensions exceed max allowed size: 8000 pixels`
- Cause: Chrome DevTools MCP inline screenshots fail when any dimension exceeds 8000px
- Fix: Use the Chromium Screenshots API at `browser.lan:8000`

## Screenshot Service (browser.lan:8000)

For full-page screenshots that may exceed 8000px, use the dedicated screenshot service.

**Service**: Chromium Screenshots API
**Endpoint**: `http://browser.lan:8000`
**Docs**: https://github.com/samestrin/chromium-screenshots

### When to Use

| Scenario | Tool |
|----------|------|
| Viewport screenshot (< 8000px) | Chrome DevTools MCP |
| Element-specific screenshot | Chrome DevTools MCP |
| Full-page screenshot (may exceed 8000px) | **Chromium Screenshots API** |
| Need to save directly to local filesystem | **Chromium Screenshots API** |

### Quick Reference

```bash
# Full-page screenshot (saves locally, avoids 8000px limit)
curl "http://browser.lan:8000/screenshot?url=http://192.168.68.99:3000/page&type=full_page" \
  -o evidence/screenshot.png

# With wait for dynamic content
curl "http://browser.lan:8000/screenshot?url=http://192.168.68.99:3000/page&type=full_page&wait_for_timeout=2000" \
  -o evidence/screenshot.png
```

### Database connection from test machine

- The database runs on m4-macmini.lan
- Direct `psql` from test scripts requires proper `DATABASE_URL` with hostname (not localhost)
