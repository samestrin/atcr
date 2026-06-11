# Sprint Execution Guidelines

This file provides sprint-specific guidance for AI agents executing sprints. For project-wide context, see the root `CLAUDE.md`.

## Remote Browser Infrastructure

### Available Machines

The development environment includes specialized machines for different purposes:

| Machine | Hostname | IP | Purpose |
|---------|----------|-----|---------|
| **App Server** | m4-macmini.lan | 192.168.68.65 | Wasp development server, database, code execution |
| **Browser Machine** | browser.lan | 192.168.68.102 | Remote browser for testing, screenshots, visual analysis |
| **Orchestrator** | orchestrator.lan | 192.168.68.100 | Screenshot service, text-analysis-api |

### Browser Machine (browser.lan)

The browser machine is a dedicated Ubuntu server running Chrome for remote browser automation. **Always prefer this machine over local browser resources** to avoid consuming resources on the development machine.

**Access Methods:**

1. **Chrome DevTools MCP** - For page snapshots, clicks, form filling, network inspection
   ```
   mcp__chrome-devtools__take_snapshot
   mcp__chrome-devtools__navigate_page
   mcp__chrome-devtools__click
   mcp__chrome-devtools__fill
   mcp__chrome-devtools__list_network_requests
   mcp__chrome-devtools__take_screenshot
   ```

2. **Playwright MCP** - For advanced browser automation
   ```
   mcp__playwright__browser_navigate
   mcp__playwright__browser_snapshot
   mcp__playwright__browser_click
   mcp__playwright__browser_type
   mcp__playwright__browser_take_screenshot
   ```

3. **VNC Access** - Visual debugging (human use)
   - URL: `http://browser.lan:6080/vnc.html`
   - Password: `browser123`

4. **SSH Access** - Direct command execution with passwordless login

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

### Screenshot Service (orchestrator.lan)

The orchestrator provides a screenshot capture API:

- **Endpoint**: `http://browser.lan:8000/screenshot`
- **Method**: POST
- **Usage**: For capturing screenshots of external URLs during visual analysis

### Text Analysis API (orchestrator.lan)

- **Endpoint**: `http://orchestrator.lan:8001`
- **Purpose**: Text analysis, voice profiling, content scanning
- **Environment Variable**: `TEXT_ANALYSIS_API_URL`

## When to Use Remote Browser

### ALWAYS Use browser.lan For:
- E2E testing with Playwright or Chrome DevTools
- Taking screenshots for visual analysis testing
- Verifying UI rendering and interactions
- Performance testing that requires a real browser
- Any test that opens browser windows

### Use Local Resources For:
- Unit tests (Vitest with happy-dom)
- Integration tests that mock browser APIs
- API endpoint testing
- Database operations

## Sprint Execution Best Practices

### Before Starting a Sprint

1. **Verify browser.lan is accessible**: 
   ```bash
   ping -c 1 browser.lan
   ```

2. **Check Chrome DevTools MCP is connected**:
   Use `mcp__chrome-devtools__list_pages` to verify connection

3. **Check Playwright MCP is available**:
   Use `mcp__playwright__browser_tabs` with action "list"

### During Sprint Execution

1. **Visual Testing**: Use `mcp__chrome-devtools__take_screenshot` or `mcp__playwright__browser_take_screenshot` for evidence collection

2. **Page Snapshots**: Prefer `mcp__chrome-devtools__take_snapshot` over screenshots for accessibility-tree based testing

3. **Form Testing**: Use Chrome DevTools `fill` and `fill_form` for form interactions

4. **Network Inspection**: Use `mcp__chrome-devtools__list_network_requests` to verify API calls

### Evidence Collection

When executing frontend test plans or validating UI changes:

1. Save screenshots to `.planning/testing/evidence/[sprint-id]/`
2. Include timestamps in filenames: `feature-name-YYYYMMDD-HHMMSS.png`
3. Take before/after screenshots for visual regression verification

## Common Issues

### Browser Not Responding

If Chrome DevTools MCP times out:
1. Check VNC to see if browser is stuck
2. Navigate to a simple page first: `mcp__chrome-devtools__navigate_page` with `about:blank`
3. If needed, the browser container can be restarted on browser.lan

### Screenshot Failures

If screenshots fail:
1. Ensure the page has fully loaded (use `mcp__chrome-devtools__wait_for` with expected text)
2. Check if page requires authentication
3. Verify browser.lan has network access to the target URL

### MCP Connection Issues

If MCP tools return connection errors:
1. Verify the MCP server is running: Check Claude Code settings
2. Restart Claude Code to reconnect MCP servers
3. Check network connectivity between machines
