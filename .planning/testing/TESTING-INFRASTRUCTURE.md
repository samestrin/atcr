# Automated Frontend Testing Infrastructure

> **Note**: This file contains infrastructure setup and operational documentation.
> For test patterns and guidelines, see [CLAUDE.md](./CLAUDE.md).

## Environment Setup

### Network Architecture

| Machine | Role | Address |
|---------|------|---------|
| **m4-macmini.lan** | Application Server (Wasp) | localhost:3000 (local), 192.168.68.99:3000 (network) |
| **browser.lan** | Remote Browser (Chrome DevTools/Playwright MCP) | 192.168.68.102 |

#### browser.lan Specifications

| Property | Value |
|----------|-------|
| Hardware | Dell Precision 3420, Intel Xeon E3-1270 v5, 32GB RAM, 512GB SSD |
| OS | Ubuntu Server (headless) |
| Network | 2.5GbE NIC |
| MCPs | chrome-devtools-mcp, playwright MCP |
| Output Dir | `/mnt/unraid/Development/playwright-output` (network-accessible) |
| VNC Access | http://browser.lan:6080/vnc.html (password: `browser123`) |

**VNC Access**: Since browser.lan runs headless Ubuntu Server, use noVNC in your browser to view the desktop session when debugging tests.

### Key Insight: Remote Browser Access

The Chrome DevTools MCP connects to a browser running on **browser.lan** (192.168.68.102), NOT the local machine. This means:

- **DO NOT use `localhost:3000`** - this refers to browser.lan's localhost
- **USE the network IP** - `http://192.168.68.99:3000` for the Wasp server on m4-macmini.lan

### MCP Preferences

| MCP | Status | Notes |
|-----|--------|-------|
| **Chrome DevTools MCP** | Preferred | Direct browser control, better snapshots |
| **Playwright MCP** | Alternative | Full Playwright capabilities, saves traces to `/mnt/unraid/Development/playwright-output` |

#### browser.lan MCP Configuration

The browser.lan machine runs mcp-proxy with this configuration:

```json
{
  "mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": [
        "chrome-devtools-mcp@latest",
        "--browserUrl", "http://127.0.0.1:9223",
        "--viewport", "1920x1080",
        "--logFile", "/mnt/unraid/Development/logs/devtools-mcp.log"
      ],
      "transportType": "stdio",
      "env": { "DISPLAY": ":99" }
    },
    "playwright": {
      "command": "npx",
      "args": [
        "@playwright/mcp@latest",
        "--output-dir", "/mnt/unraid/Development/playwright-output",
        "--save-trace",
        "--viewport-size", "1920x1080"
      ],
      "transportType": "stdio",
      "env": { "DISPLAY": ":99" }
    }
  }
}
```

#### MCP Proxy Service Management

The mcp-proxy runs as a systemd service on browser.lan:

| Component | Details |
|-----------|---------|
| Service | `mcp-proxy.service` |
| Config | `/home/samestrin/mcp/mcp-servers.json` |
| Port | 8080 (SSE transport) |
| Restart Timer | `mcp-proxy-restart.timer` (daily at 04:00) |

**Automatic Daily Restart:** A systemd timer restarts mcp-proxy daily at 4am to prevent stale connection state. Long-running mcp-proxy instances (24+ hours) can develop issues where Claude Code connects successfully but tools fail to register.

**Manual Restart Commands:**
```bash
# Restart mcp-proxy (fixes most connection issues)
ssh browser.lan "sudo systemctl restart mcp-proxy"

# Check service status
ssh browser.lan "systemctl status mcp-proxy"

# Check restart timer status
ssh browser.lan "systemctl list-timers mcp-proxy-restart.timer"

# View recent logs
ssh browser.lan "journalctl -u mcp-proxy --since '30 min ago'"

# Check if SSE endpoints are responding
curl -s http://browser.lan:8080/servers/chrome-devtools/sse -H "Accept: text/event-stream" --max-time 2
```

**Symptom: Tools Not Available After /mcp Reconnect**

If `/mcp` shows "Reconnected to chrome-devtools" but tools like `mcp__chrome-devtools__navigate_page` are unavailable:

1. Restart mcp-proxy: `ssh browser.lan "sudo systemctl restart mcp-proxy"`
2. Wait 2-3 seconds for service startup
3. Run `/mcp` in Claude Code to reconnect
4. Verify tools are available

### SSH Access to browser.lan

The browser.lan machine is accessible via SSH with **passwordless login**:

| Account | SSH Command | Purpose |
|---------|-------------|---------|
| `samestrin` | `ssh browser.lan` or `ssh samestrin@browser.lan` | All browser management (Chrome, file operations) |

**Note**: All services are consolidated under `samestrin@browser.lan`.

### Session Initialization (CRITICAL)

**Claude Code runs on m4-macmini.lan** and has **passwordless SSH access** to browser.lan. When Chrome DevTools MCP is not responding, Claude Code MUST automatically reset the browser - do NOT ask the user to run these commands.

**At the start of EVERY new Claude Code session that involves frontend testing**, reset the Chrome browser:

```bash
# Step 1: Kill existing Chrome
ssh samestrin@browser.lan "pkill -f chrome"

# Step 2: Wait briefly for process cleanup
sleep 2

# Step 3: Start Chrome with remote debugging
# IMPORTANT: --user-data-dir is REQUIRED for remote debugging to work
ssh samestrin@browser.lan "DISPLAY=:99 nohup google-chrome --remote-debugging-port=9222 --user-data-dir=/tmp/chrome-devtools --no-first-run --disable-default-apps http://192.168.68.99:3000/ > /home/samestrin/chrome.log 2>&1 &"

# Step 4: Wait for Chrome to start
sleep 3

# Step 5: Verify browser is running and port is listening
ssh samestrin@browser.lan "pgrep -f chrome && ss -tlnp | grep 9222"
```

After running these commands, **run `/mcp` in Claude Code** to reconnect the Chrome DevTools MCP.

This ensures:
- No stale browser state from previous sessions
- Clean DevTools MCP connection
- Browser window is visible in VNC
- Remote debugging port 9222 is active

### Auto-Recovery Protocol

**When Chrome DevTools MCP tools fail or return errors**, Claude Code should:

1. **Automatically attempt recovery** - do NOT ask the user
2. Run the session initialization commands above via SSH
3. Run `/mcp` to reconnect
4. Retry the failed operation
5. Only escalate to user if recovery fails twice

**When MCP tools are not available after `/mcp` reconnect** (SSE connects but tools don't register):

1. Restart mcp-proxy: `ssh browser.lan "sudo systemctl restart mcp-proxy"`
2. Wait 3 seconds
3. Run `/mcp` to reconnect
4. Retry the operation

### Troubleshooting Chrome DevTools MCP

**Claude Code should automatically handle these issues** - do not ask the user.

**Error: "No such tool available: mcp__chrome-devtools__*"** (tools not registered)

This occurs when mcp-proxy has stale state (typically after 24+ hours uptime). Claude Code should:
```bash
# Restart mcp-proxy
ssh browser.lan "sudo systemctl restart mcp-proxy"
sleep 3
```
Then run `/mcp` to reconnect.

**Error: "The browser is already running..."** or **MCP tools not available**

Claude Code should run these commands automatically:
```bash
# Kill Chrome
ssh samestrin@browser.lan "pkill -f chrome"
sleep 2

# Remove stale locks if needed
ssh samestrin@browser.lan "sudo rm -rf /tmp/chrome-devtools && mkdir -p /tmp/chrome-devtools"

# Restart Chrome
ssh samestrin@browser.lan "DISPLAY=:99 nohup google-chrome --remote-debugging-port=9222 --user-data-dir=/tmp/chrome-devtools --no-first-run http://192.168.68.99:3000/ > /home/samestrin/chrome.log 2>&1 &"
sleep 3

# Verify
ssh samestrin@browser.lan "pgrep -f chrome && ss -tlnp | grep 9222"
```

Then run `/mcp` to reconnect.

**VNC Access** (for user debugging only):
- URL: http://browser.lan:6080/vnc.html
- Password: `browser123`

## Test Execution Prerequisites

### Claude Code Environment

**Claude Code runs on m4-macmini.lan (192.168.68.99)** - the same machine as the Wasp server. This means:
- Wasp server can be started/restarted **locally** (no SSH needed)
- SSH to browser.lan is available for browser management
- Network address for browser.lan to reach server: `http://192.168.68.99:3000`

### Auto-Start Instructions for Claude Code

**IMPORTANT**: Before running any `/execute-frontend-tests` command, Claude Code MUST automatically handle infrastructure - do NOT ask the user.

#### 1. Check and Start Wasp Server

```bash
# Check if server is running
curl -s -o /dev/null -w "%{http_code}" http://192.168.68.99:3000 2>/dev/null
```

If non-200 response, start the server (runs locally on m4-macmini.lan):
```bash
cd /Users/samestrin/Documents/GitHub/brand/app && wasp start
```
Run in background mode and wait for:
- `VITE ready` message
- Server responding with HTTP 200

#### 2. Check and Start Chrome Browser

```bash
# Check if Chrome DevTools port is listening
ssh samestrin@browser.lan "ss -tlnp | grep 9222" 2>/dev/null
```

If port not listening, start Chrome:
```bash
ssh samestrin@browser.lan "pkill -f chrome" 2>/dev/null
sleep 2
ssh samestrin@browser.lan "DISPLAY=:99 nohup google-chrome --remote-debugging-port=9222 --user-data-dir=/tmp/chrome-devtools --no-first-run http://192.168.68.99:3000/ > /home/samestrin/chrome.log 2>&1 &"
sleep 3
```

#### 3. Reconnect MCP if Needed

If Chrome DevTools MCP tools are not available, run `/mcp` to reconnect.

**Do NOT ask the user** for any of these steps - handle automatically.

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
- Fix: Use the Chromium Screenshots API service (see below)

## Screenshot Service (browser.lan:8000)

For screenshots during frontend testing, use the dedicated Chromium Screenshots API instead of MCP screenshot tools.

**Service**: Chromium Screenshots API
**Endpoint**: `http://browser.lan:8000`
**Docs**: https://github.com/samestrin/chromium-screenshots

### Screenshot Tool Decision Matrix

| Scenario | Tool | Reason |
|----------|------|--------|
| **All test evidence screenshots** | Chromium Screenshots API | Saves to file, zero context usage |
| **Authenticated pages (simple state)** | Chromium Screenshots API + localStorage | Inject session, capture authenticated view |
| **Authenticated pages (complex React state)** | Chrome DevTools MCP (no screenshot) | Use `take_snapshot` for validation, skip screenshot |
| Full-page screenshot (may exceed 8000px) | Chromium Screenshots API | Avoids 8000px limit |
| Responsive testing (mobile/tablet) | Chromium Screenshots API | Set width/height params |
| Element-specific screenshot (debugging) | Chrome DevTools MCP | Last resort, use sparingly - causes context bloat |

**IMPORTANT**: For frontend test execution, ALWAYS prefer Chromium Screenshots API to avoid context overflow.

### When Chromium Screenshots API Won't Work

The API captures a fresh page load. It **cannot** replicate:
- Complex React component states (expanded accordions, modal dialogs, form validation errors)
- Multi-step wizard progress
- States requiring user interaction sequences
- Real-time data that differs per request

For these scenarios, use this fallback hierarchy:

1. **Validate via Chrome DevTools `take_snapshot`** - verify the state exists in the accessibility tree
2. **Skip the screenshot** - document the validation in the audit report instead
3. **Do NOT use DevTools `take_screenshot`** - it returns base64 and crashes context

#### Complex State Screenshot Capture (Fallback)

If a screenshot is **required** for a complex state that can't be captured via the API:

1. **Set up the state** via Chrome DevTools MCP:
   ```
   - Navigate to the page
   - Fill forms, click buttons, expand accordions
   - Get the page to the exact state needed
   ```

2. **Verify the state** with `take_snapshot` - confirm the expected elements are present

3. **Offer user a choice**:
   ```
   I've set up the browser to show [description of state].

   Screenshot options:
   A) I can capture via DevTools (fast, but uses ~500KB context)
   B) You capture manually and save to: [path]

   Which do you prefer?
   ```

4. **If user approves DevTools capture (Option A)**:
   - Use `mcp__chrome-devtools__take_screenshot`
   - Be aware this consumes significant context
   - Avoid if already deep in conversation or multiple screenshots needed
   - Document in audit report: "DevTools capture (user-approved)"

5. **If user prefers manual capture (Option B)**:
   ```
   Please capture and save to:
   .planning/testing/runs/YYYY-MM-DD/[test-name]/evidence/[filename].png

   You can use:
   - Browser DevTools (Cmd+Shift+P → "Capture screenshot")
   - VNC to browser.lan (http://browser.lan:6080/vnc.html, password: browser123)
   - The browser window on browser.lan via VNC

   Let me know when it's saved and I'll continue.
   ```

6. **Document in audit report** which method was used and why

**Context budget guidance**: If more than 2-3 DevTools screenshots are needed in a session, prefer manual capture to avoid context exhaustion.

### Authentication Support

The Chromium Screenshots API supports multiple authentication methods:

| Auth Type | Parameter | Use Case |
|-----------|-----------|----------|
| **localStorage** | `localStorage` | Wasp/OpenSaaS apps, SPAs, Firebase |
| **sessionStorage** | `sessionStorage` | Temporary tokens, wizard state |
| **Cookies** | `cookies` | Traditional server sessions, PHP, Rails |

#### Wasp/OpenSaaS Authentication (This Project)

This app uses localStorage for authentication:
```javascript
localStorage.getItem('wasp:sessionId')  // Session token
```

**IMPORTANT**: Wasp stores the session token with escaped quotes around the value. The token format is `"tokenvalue"` (with quotes as part of the value).

**To capture authenticated pages:**

1. **Get the session ID** from a logged-in browser:
   ```javascript
   // Via Chrome DevTools evaluate_script
   () => localStorage.getItem('wasp:sessionId')
   // Returns: "\"abc123xyz...\"" (note the escaped quotes)
   ```

2. **Pass it to the screenshot API with the escaped quotes preserved:**
   ```bash
   curl -X POST "http://browser.lan:8000/screenshot" \
     -H "Content-Type: application/json" \
     -d '{
       "url": "http://192.168.68.99:3000/dashboard",
       "localStorage": {
         "wasp:sessionId": "\"ic3t2fnhclk3pe46zu3d5ml5dnx5ng3u5fbh7h23\""
       },
       "width": 1280,
       "height": 800
     }' \
     -o evidence/dashboard-authenticated.png
   ```

   **Note**: The token value includes escaped quotes (`\"...\"`). Without these quotes, authentication will fail and you'll see the login page.

#### Cookie-Based Authentication

For apps using cookies:
```bash
curl -X POST "http://browser.lan:8000/screenshot" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "http://example.com/dashboard",
    "cookies": [
      {"name": "session_id", "value": "abc123"},
      {"name": "auth_token", "value": "xyz789", "httpOnly": true}
    ]
  }' \
  -o evidence/cookie-auth.png
```

#### Combined Authentication

Some apps use multiple storage types:
```bash
curl -X POST "http://browser.lan:8000/screenshot" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "http://192.168.68.99:3000/dashboard",
    "cookies": [{"name": "tracking", "value": "123"}],
    "localStorage": {"wasp:sessionId": "abc123"},
    "sessionStorage": {"temp-form-data": "draft"}
  }' \
  -o evidence/full-auth.png
```

### Basic Usage Examples

```bash
# Public page - basic viewport screenshot
curl "http://browser.lan:8000/screenshot?url=http://192.168.68.99:3000/visual-analysis" \
  -o evidence/TC-01-viewport.png

# Full-page screenshot (avoids 8000px limit)
curl "http://browser.lan:8000/screenshot?url=http://192.168.68.99:3000/visual-analysis&screenshot_type=full_page" \
  -o evidence/TC-01-full-page.png

# Custom viewport with wait
curl -X POST "http://browser.lan:8000/screenshot" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "http://192.168.68.99:3000/visual-analysis",
    "screenshot_type": "full_page",
    "width": 1280,
    "height": 800,
    "wait_for_timeout": 2000
  }' \
  -o evidence/TC-01-custom.png

# Mobile viewport
curl "http://browser.lan:8000/screenshot?url=http://192.168.68.99:3000&width=375&height=667" \
  -o evidence/TC-R-01-mobile.png
```

### API Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `url` | string | required | URL to capture |
| `screenshot_type` | string | `viewport` | `viewport` or `full_page` |
| `format` | string | `png` | `png` or `jpeg` |
| `width` | int | 1920 | Viewport width (320-3840) |
| `height` | int | 1080 | Viewport height (240-2160) |
| `quality` | int | 90 | JPEG quality (1-100) |
| `wait_for_timeout` | int | 0 | Wait after load (ms, 0-30000) |
| `wait_for_selector` | string | null | CSS selector to wait for |
| `delay` | int | 0 | Delay before capture (ms) |
| `dark_mode` | bool | false | Emulate dark color scheme |
| `block_ads` | bool | false | Block ad/tracking domains |
| `localStorage` | object | null | Key-value pairs to inject |
| `sessionStorage` | object | null | Key-value pairs to inject |
| `cookies` | array | null | Cookie objects to inject |

### How localStorage/sessionStorage Injection Works

The API uses a two-step process:
1. Navigate to the URL's origin (e.g., `http://192.168.68.99:3000`)
2. Inject storage values via JavaScript
3. Navigate to the full target URL
4. Take the screenshot

This adds ~500ms overhead but enables authenticated page capture.

### Database connection from test machine

- The database runs on m4-macmini.lan
- Direct `psql` from test scripts requires proper `DATABASE_URL` with hostname (not localhost)

## Shared Storage (/mnt/development)

### Overview

The `/mnt/development` share on the Unraid server provides high-performance shared storage accessible across all test machines.

| Property | Value |
|----------|-------|
| Storage | 4x 1TB SATA SSD in BTRFS RAID0 |
| Network | 10GbE to server, 2.5GbE to browser.lan |
| Mount Path (browser.lan) | `/mnt/development` |
| Mount Path (m4-macmini.lan) | `/Volumes/Development` (SMB mount) |
| Key Directories | `playwright-output/`, `logs/`, `evidence/` |

### Setting Up SMB Mount on m4-macmini.lan (macOS)

To access the shared storage from the development machine:

1. **Connect via Finder**: `Cmd+K` → `smb://unraid.lan/Development`
2. **Save credentials**: Check "Remember this password in my keychain"
3. **Auto-mount on login**: The mount script at `~/bin/mount-development.sh` handles this via LaunchAgent

```bash
# Verify access
ls /Volumes/Development/playwright-output/

# Manual mount if needed
~/bin/mount-development.sh
```

### Benefits

1. **Direct Screenshot Access**: Screenshots saved by browser.lan are immediately accessible on m4-macmini.lan
2. **Playwright Traces**: Traces saved to `/mnt/unraid/Development/playwright-output` can be viewed from any machine
3. **Shared Logs**: MCP logs at `/mnt/unraid/Development/logs/` for debugging
4. **No Network Transfer During Tests**: Evidence files are "local" to both machines via NFS

### Usage in Tests

Screenshots and evidence can be saved directly to the shared storage:

```bash
# Save screenshot evidence (from m4-macmini.lan via curl)
curl "http://browser.lan:8000/screenshot?url=http://192.168.68.99:3000/page" \
  -o /Volumes/Development/evidence/screenshot.png

# View Playwright traces (saved by browser.lan to /mnt/unraid/Development/)
npx playwright show-trace /Volumes/Development/playwright-output/trace.zip
```

**Path mapping:**
| Machine | Path |
|---------|------|
| browser.lan (Linux) | `/mnt/unraid/Development/` |
| m4-macmini.lan (macOS) | `/Volumes/Development/` |
