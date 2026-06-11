# Content Extraction Test Coverage Matrix

**Created:** 2025-12-23
**Related:** TD-52 Gap 4 - Content Extraction Documentation
**Automated Coverage:** TC-20 (1.0_mvp-smoke-tests)

---

## Overview

This document defines the test coverage matrix for the content extraction feature. Content extraction is handled by the `fetchUrlContent` service, which retrieves and processes web page content for voice analysis.

## Automated Test Reference

**Primary Test File:** `app/tests/server/contracts/fetchUrlContent.contract.test.ts`

This contract test file validates the core extraction logic and serves as the authoritative source for what extraction scenarios are currently tested.

## Supported Scenarios

| Scenario | Status | Test Coverage | Notes |
|----------|--------|---------------|-------|
| Static HTML pages | Supported | Contract tests | Standard HTML parsing via Cheerio |
| Dynamic content (SPA) | Supported | Contract tests | Puppeteer fallback for JavaScript-rendered content |
| Standard character encodings (UTF-8) | Supported | Contract tests | Primary encoding support |
| HTTPS pages | Supported | Contract tests | TLS/SSL handled |
| HTTP pages | Supported | Contract tests | Auto-redirect to HTTPS where applicable |
| robots.txt compliance | Supported | Contract tests | Respects crawl directives |

## Known Limitations

| Scenario | Status | Reason |
|----------|--------|--------|
| Paywalled content | Not Supported | Requires authentication/subscription credentials |
| CAPTCHA-protected pages | Not Supported | Cannot programmatically solve CAPTCHAs |
| Login-required pages | Not Supported | Session/cookie management not implemented |
| Non-standard charsets | Best Effort | May produce garbled text for rare encodings |
| PDF/DOC content | Not Supported | Binary document parsing not implemented |
| Infinite scroll pages | Partial | Only initial viewport content extracted |

## Test Plan Coverage

| Test Case | Location | What It Validates |
|-----------|----------|-------------------|
| TC-20 | 1.0_mvp-smoke-tests | Content extraction boundaries (excludes nav/footer) |
| Contract tests | `fetchUrlContent.contract.test.ts` | URL fetching, HTML parsing, error handling |

## Boundary Detection

The extraction logic attempts to identify and exclude:
- Navigation menus
- Headers and footers
- Sidebars
- Advertisement content

Main content is identified using semantic HTML elements (`<main>`, `<article>`) and content density heuristics.

## Edge Case Handling

1. **Empty pages**: Returns error with descriptive message
2. **Timeout (>30s)**: Returns timeout error
3. **404/5xx responses**: Returns HTTP error with status code
4. **Redirect chains**: Follows up to 5 redirects
5. **Large pages (>10MB)**: Truncates content with warning

---

*Last validated: 2025-12-21 (TC-20 passing)*
