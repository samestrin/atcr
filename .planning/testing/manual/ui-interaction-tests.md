# Manual UI Interaction Tests

**Created:** 2025-12-23
**Related:** TD-52 Gap 6 - Clipboard Validation Documentation

---

## Overview

This document contains manual test cases for UI interactions that cannot be fully automated due to browser security restrictions or platform-specific behaviors.

## Why Manual Testing Is Required

Certain browser APIs are restricted in headless/automated contexts:
- **Clipboard API** (`navigator.clipboard.writeText()`): Requires user gesture and secure context permissions
- **File download triggers**: May be blocked by browser policies
- **Native OS dialogs**: Cannot be controlled programmatically

---

## Manual Test Cases

### TC-MAN-01: Copy Rewrite to Clipboard

**Priority:** High
**Frequency:** Per release, after any changes to copy functionality
**Related Component:** `app/src/client/components/ReviewInterface.tsx`

#### Partial Automated Coverage

The following unit tests validate the copy logic is invoked correctly:
- `app/tests/client/hooks/useTextSelection.test.tsx`

These tests verify:
- Copy function is called with correct text
- Error handling for copy failures
- UI state updates (button text, toast notification)

**Limitation:** Unit tests mock the Clipboard API. The actual browser integration requires manual verification.

#### Prerequisites

| Key | Value |
|-----|-------|
| User | Any authenticated user |
| State | Content scan completed with rewrite suggestions |
| Browser | Chrome, Firefox, or Safari (not incognito) |

#### Steps

1. Navigate to `/content-scan` with an existing scan result
2. Accept at least one rewrite suggestion
3. Click the "Copy" button on the rewritten content
4. Observe that a "Copied!" toast notification appears
5. Open any text editor (Notepad, TextEdit, VS Code)
6. Paste the content (Ctrl+V / Cmd+V)
7. Compare the pasted text to the displayed rewrite

#### Expected Results

| Step | Expected Outcome |
|------|------------------|
| 3 | Copy button responds to click (visual feedback) |
| 4 | Toast notification appears within 500ms |
| 6 | Paste operation succeeds |
| 7 | Pasted text matches the rewritten content exactly |

#### Failure Criteria

- Paste is empty or contains wrong content
- No toast notification appears
- Copy button does not respond
- Browser console shows Clipboard API errors

#### Browser-Specific Notes

| Browser | Notes |
|---------|-------|
| Chrome | Requires HTTPS or localhost; works in standard mode |
| Firefox | May prompt for clipboard permission on first use |
| Safari | Clipboard access may be restricted in private browsing |
| Edge | Same behavior as Chrome (Chromium-based) |

---

## Additional Manual Checks

### Keyboard Shortcut Copy (Optional)

**ID:** TC-MAN-02

If the application supports keyboard shortcuts for copying:
1. Select rewritten text
2. Press Ctrl+C (Windows) or Cmd+C (Mac)
3. Verify content copies correctly

### Drag and Drop (Optional)

**ID:** TC-MAN-03

If drag-and-drop is supported:
1. Drag rewritten text to external application
2. Verify content transfers correctly

---

## Test Execution Log Template

| Date | Tester | TC | Browser | OS | Result | Notes |
|------|--------|----|---------|----|--------|-------|
| YYYY-MM-DD | Name | TC-MAN-01 | Chrome 120 | macOS 14 | PASS/FAIL | Any observations |

---

*Last validated: 2025-12-23*
