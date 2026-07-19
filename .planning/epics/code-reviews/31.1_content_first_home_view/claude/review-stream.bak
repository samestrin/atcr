# Code Review Stream - 31.1_content_first_home_view (Epic)

**Started:** July 19, 2026 07:09:36AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — bare `atcr` shows a live home view (not help)
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/main.go:262-264` (RunE → runHome), `cmd/atcr/home.go:121-153` (renders relHome exec path + cmd.Short + live state)
- **Notes:** RunE now calls `runHome`, replacing `cmd.Help()`. Renders `~`-relativized exec path, one-line description, and current review state. Test `TestRootCmd_BareInvocationShowsHomeView` asserts description present + no "Usage:".

### Criterion: AC2 — `--help`/`-h`/`--version`/subcommands unchanged
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/main.go:278` (`--axi` is a root-LOCAL flag, not persistent), tests `TestRootCmd_HelpAndVersionUnaffected`, `TestRootCmd_AXINotInheritedBySubcommands`, `TestRootCmd_HasExactlyTwentyFourSubcommands`
- **Notes:** cobra's `--help`/`-h`/`--version` short-circuit before RunE (structurally unaffected). `--axi` local flag is not inherited, so `atcr status --axi` still exits 2. Subcommand count unchanged (24).

### Criterion: AC3 — no-reviews-yet state, never error/empty
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/home.go:69-85` (resolveHomeState), `cmd/atcr/anchor.go:18-28` + `internal/fanout/reviewdir.go:588-601` (ReadLatest)
- **Notes:** `anchorDir("")` error is caught; `errors.Is(err, os.ErrNotExist)` (missing file → *PathError) yields first-run; empty/corrupt pointer (distinct non-ErrNotExist error) and stale pointer yield honest "unavailable" — never conflated, never a non-zero exit. Exec-path resolution failure also degrades to fallback name (home.go:123-130). Tests: `TestResolveHomeState_NoReviews/HasReview/StalePointer/CorruptPointer`, `TestRunHome_ExecutableFallback`.

### Criterion: AC4 — bare `atcr --axi` via Epic 31.0 context plumbing
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/main.go:256-259` + `:278` (root-local `--axi` → PersistentPreRunE Lookup → newAXIContext), `cmd/atcr/axi.go:11-24` (shared axiContextKey), `cmd/atcr/home.go:137-151`, `internal/report/home.go` (RenderHomeViewAXI reuses toonQuote/axiDelim)
- **Notes:** Reuses the exact same context key `review.go`/`resume.go` read via `axiFromContext` — one propagation mechanism, not a parallel switch. Per the recorded clarification, a new single-row `HomeViewAXI`/`RenderHomeViewAXI` mirrors the `ReviewSummaryAXI` precedent (findings-less metadata), reusing the shared TOON encoder. Test `TestRootCmd_BareAXIRendersHomeViewPayload`.

### Criterion: AC5 — golden snapshot + suite/vet/lint pass
- **Verdict:** VERIFIED ✅ (golden tests present; suite/vet/lint executed in Phase 4)
- **Evidence:** `cmd/atcr/home_test.go:200-224` (`TestHomeView_GoldenNonAXI`, byte-for-byte), `internal/report/home_test.go:66-79` (`TestRenderHomeViewAXI_Golden`)
- **Notes:** Non-axi golden pins both has-review and no-review states byte-for-byte. Suite/vet/lint results in Phase 4.

---

## Adversarial Analysis (Discovery Mode — no sprint-design risk profile)

**Mode:** Full hostile review (independent general-purpose agent)
**Files Reviewed:** 3 (cmd/atcr/home.go, cmd/atcr/main.go, internal/report/home.go)
**Issues Found:** 5 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic mode)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 5

### Confirmed sound under scrutiny (no defect)
- ErrNotExist vs corrupt-pointer distinction (AC3): correct — `%w` wrapping preserves `errors.Is`; all four states test-pinned.
- AC4 shared-context fidelity: genuinely reuses `axiContextKey{}` that review/resume read — not a parallel renderer.
- AXI stdout purity: degrade logs go to `cmd.ErrOrStderr()`, cannot corrupt the stdout payload.
- AC3 never-error/never-empty: every path returns a value + writes >=2 lines.

### LOW findings (routed to TD stream)
1. `main.go:234` — stale comment claims bare `atcr` still prints help (maintainability, 5m)
2. `home.go:72` — `os.ErrNotExist` superset: dangling-symlink/ENOTDIR pointer misclassified as first-run (correctness, 30m)
3. `home.go:44` — third copy of the filepath.Rel-plus-fallback home idiom (maintainability, 60m)
4. `home.go:93` — human exec-path not control-byte sanitized (asymmetric with AXI) (security, 15m)
5. `home.go:56` — `~\rel` on Windows diverges from documented `~/rel` in AXI payload (correctness, 15m)
