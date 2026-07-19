# atcr Reconciled Review

## Summary

- Total findings: 4
- Sources: pool
- Clusters collapsed: 1
- Severity disagreements: 0
- Consensus filtered: 6 (uncorroborated singletons routed to the ambiguous sidecar)
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 3 | 0 |
| MEDIUM | 0 | 0 | 0 |
| LOW | 1 | 0 | 0 |

## Disagreements

Top 9 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `cmd/atcr/home.go:148` (HIGH) · score 3
- Reviewers: dax (independence 1)
- Problem: (runHome) No integration test exercises the &#96;atcr --axi&#96; bare invocation path; the AXI home view is only unit-tested at the renderer level, not end-to-end (AC4)

### 2. solo_finding — `cmd/atcr/main_test.go:96` (HIGH) · score 3
- Reviewers: dax (independence 1)
- Problem: (TestRootCmd_BareInvocationShowsHomeView) AC5 requires a golden/snapshot test pinning the non-&#96;--axi&#96; home-view output, but no such test exists; the existing test only checks substrings, not the exact output

### 3. solo_finding — `cmd/atcr/main_test.go:96` (HIGH) · score 3
- Reviewers: dax (independence 1)
- Problem: (TestRootCmd_BareInvocationShowsHomeView) TestRootCmd_BareInvocationShowsHomeView is not isolated from the filesystem; its output depends on whether &#96;.atcr/latest&#96; exists in the working directory, making it unreliable

### 4. gray_zone — `cmd/atcr/home.go:44` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: relHome has no direct tests for edge cases (path under home, path outside home, home dir error); the integration test relies on the real home directory, making relativization nondeterministic
- Detail: similarity 0.00
- Positions:
  - dax — MEDIUM: relHome has no direct tests for edge cases (path under home, path outside home, home dir error); the integration test relies on the real home directory, making relativization nondeterministic

### 5. gray_zone — `cmd/atcr/home.go:46` (MEDIUM) · score 2
- Reviewers: brad (independence 1)
- Problem: relHome constructs paths using filepath.Separator, which produces backslashes on Windows, potentially breaking downstream TOON parsers or scripts that expect forward slashes in the exec_path field
- Detail: similarity 0.00
- Positions:
  - brad — MEDIUM: relHome constructs paths using filepath.Separator, which produces backslashes on Windows, potentially breaking downstream TOON parsers or scripts that expect forward slashes in the exec_path field

### 6. gray_zone — `cmd/atcr/home.go:81` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: resolveHomeState error paths (anchorDir non-ErrNotExist, ReadReviewStatus failure) have zero test coverage; the &#34;unavailable&#34; degrade is never exercised
- Detail: similarity 0.00
- Positions:
  - dax — MEDIUM: resolveHomeState error paths (anchorDir non-ErrNotExist, ReadReviewStatus failure) have zero test coverage; the &#34;unavailable&#34; degrade is never exercised

### 7. gray_zone — `cmd/atcr/home.go:115` (MEDIUM) · score 2
- Reviewers: otto (independence 1)
- Problem: runHome uses a hardcoded fallback &#34;atcr&#34; for the executable path
- Detail: similarity 0.00
- Positions:
  - otto — MEDIUM: runHome uses a hardcoded fallback &#34;atcr&#34; for the executable path

### 8. gray_zone — `internal/report/home_test.go:63` (MEDIUM) · score 2
- Reviewers: mira (independence 1)
- Problem: Golden test hardcodes Unix forward-slash paths but relHome uses platform filepath.Separator — golden passes while real code path produces backslash paths on Windows
- Detail: similarity 0.00
- Positions:
  - mira — MEDIUM: Golden test hardcodes Unix forward-slash paths but relHome uses platform filepath.Separator — golden passes while real code path produces backslash paths on Windows

### 9. gray_zone — `cmd/atcr/home.go:53` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: relHome uses a bespoke string concatenation for the home separator
- Detail: similarity 0.00
- Positions:
  - otto — LOW: relHome uses a bespoke string concatenation for the home separator

## Findings

### HIGH

- `cmd/atcr/home.go:148` — confidence MEDIUM, reviewers: dax
  - Problem: (runHome) No integration test exercises the &#96;atcr --axi&#96; bare invocation path; the AXI home view is only unit-tested at the renderer level, not end-to-end (AC4)
  - Fix: Add a test in &#96;main_test.go&#96; that runs &#96;execute(t, &#34;--axi&#34;)&#96; and asserts the TOON header &#96;home[1
  - Evidence: 20/&#96;runHome&#96; calls &#96;report.RenderHomeViewAXI&#96; under &#96;if axiFromContext(ctx)&#96; but no test in &#96;main_test.go&#96; executes &#96;atcr --axi&#96;
- `cmd/atcr/main_test.go:96` — confidence MEDIUM, reviewers: dax
  - Problem: (TestRootCmd_BareInvocationShowsHomeView) AC5 requires a golden/snapshot test pinning the non-&#96;--axi&#96; home-view output, but no such test exists; the existing test only checks substrings, not the exact output
  - Fix: Add a golden test with mocked &#96;homeExecutable&#96;/&#96;homeUserDir&#96; asserting the full byte-for-byte output for the first-run state
  - Evidence: &#96;TestRootCmd_BareInvocationShowsHomeView&#96; uses &#96;Contains&#96;/&#96;NotContains&#96; instead of exact output match; AC5 mandates a golden test
- `cmd/atcr/main_test.go:96` — confidence MEDIUM, reviewers: dax
  - Problem: (TestRootCmd_BareInvocationShowsHomeView) TestRootCmd_BareInvocationShowsHomeView is not isolated from the filesystem; its output depends on whether &#96;.atcr/latest&#96; exists in the working directory, making it unreliable
  - Fix: Set up a temp directory with no &#96;.atcr/latest&#96; or mock &#96;anchorDir&#96; to ensure deterministic first-run state
  - Evidence: &#96;execute(t)&#96; runs in the current directory without isolating &#96;.atcr/latest&#96;; the test only checks for description and absence of &#34;Usage:&#34;

### LOW

- `cmd/atcr/home.go:126` — confidence HIGH, reviewers: brad, bruce
  - Problem: (runHome) runHome falls back to the literal string &#34;atcr&#34; when os.Executable() fails, which may not match the actual binary name or path, causing misleading exec_path output in edge cases (e.g., renamed binaries or embedded environments)
  - Fix: See note in findings — the message &#34;Latest review pointer is unreadable&#34; is technically correct but conflates &#34;pointer file unreadable&#34; with &#34;review directory unreadable&#34;; the user may not understand whether they need to fix the pointer or simply run a new review
  - Evidence: [brad] execPath = &#34;atcr&#34; hardcodes the name instead of deriving it from the invocation or command metadata / [bruce] fmt.Fprintln(w, &#34;Latest review pointer is unreadable — run &#96;atcr review&#96; to start a fresh one.&#34;)
