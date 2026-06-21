# Code Review Stream - 5.2_diff_caching_incremental_reviews (Epic)

**Started:** June 20, 2026 07:41:43PM
**Mode:** Acceptance Criteria + Adversarial Review
**Review target:** `origin/main` @ `4f2e6b9` (the merged epic 5.2 code)

> ⚠️ **Repo-state note:** Local `main` (`a7c9cd8`) does **not** contain the epic 5.2 implementation. It is **7 ahead / 1 behind** `origin/main`; the 1 commit it is missing is `4f2e6b9` — the entire merge (the `internal/cache/` package + fanout integration). Local `main`'s 7 commits are all `.planning/`-only, including `1eed390 chore: archive epic 5.2 (merged 4f2e6b9)`, which archived the epic on a branch that does not hold the merged code. This review reads the merged code from `4f2e6b9` (read-only); tests/coverage/lint were NOT run because the code is not in the working tree.

---

## Acceptance Criteria Findings

### Criterion: Unchanged file chunks are served from local cache, avoiding LLM API calls.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/engine.go:579-622` (`invokeCachedSingleShot`) — on a cache hit it returns a synthesized `StatusOK` Result with the cached `Content`, zero token usage, no `CallRecords`, and `CacheHit:true`, with no provider round-trip. Wired into the dispatch path at `engine.go:566`.
- **Notes:** Cache unit is per-agent-per-payload (whole rendered prompt), matching the epic's payload-level decision. Tool/degraded agents are deliberately never cached (`engine.go:569-573`).

### Criterion: Cache keys incorporate the chunk diff hash and the reviewer model/persona.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/review.go:613-628` (`diffCacheKey`) keys on the FULL rendered prompt (which subsumes payload, persona, per-agent scope focus, and base/head refs) + model id + temperature; `internal/cache/key.go:44-51` (`cache.Key`) joins the three inputs with a NUL separator before the outer sha256. Wired at `review.go:688` (primary) and `review.go:776` (fallback uses primary prompt + its own model/temperature).
- **Notes:** `min_severity`/`max_findings` correctly excluded (deterministic post-LLM filters). Minor gap logged: provider/BaseURL not in key (TD LOW).

### Criterion: `--no-cache` flag forces a fresh review.
- **Verdict:** VERIFIED ✅
- **Evidence:** Flag declared at `cmd/atcr/review.go:47`, wired to `ReviewRequest.NoCache` at `cmd/atcr/review.go:181` → `PreparedReview.cacheNoRead` (`review.go:321`, `resume.go:319`) → `Engine.cacheNoRead` via `WithCache` (`engine.go:274-280`). Semantics correct: bypasses READ but still WRITES fresh results (`engine.go:585-587`, `engine.go:613-619`), refreshing stale entries per the clarification.
- **Notes:** Covered by `TestEngine_NoCacheBypassesReadButStillWrites` and `TestRunReview_NoCacheRequestStillCallsLive`.

### Criterion: Cache eviction or max-size policy implemented to prevent unbounded growth.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/cache/store.go:108-150` (`evict`) — total-size cap with LRU eviction (oldest mtime first; `Get` refreshes mtime at `store.go:79`). Default 50 MiB (`DefaultCacheMaxBytes`, `registry/project.go:26-30`), overridable via `cache_max_bytes` at registry (`config.go`) and project (`project.go`) tiers, resolved in `precedence.go:126-153`; `0` = unbounded. Negative rejected at all tiers.
- **Notes:** Matches the clarification exactly (50 MiB LRU, `cache_max_bytes *int64`, no entry-count/TTL).

---

## Adversarial Analysis (Discovery Mode — no sprint-design risk profile for epics)

**Files Reviewed:** 11 source files (cache package + fanout integration + registry wiring)
**Issues Found:** 2 (verified) — both LOW
**Risk Profile:** Not available (epic mode)

### Findings
- **LOW / ops** — `.atcr/cache` ignore relies on local-only `.git/info/exclude:18`, not the committed `.gitignore`; fresh clones / CI / collaborators will not ignore the (up to 50 MiB) cache directory. See `.gitignore`.
- **LOW / correctness** — `diffCacheKey` omits provider/BaseURL; same model id across two OpenAI-compatible endpoints can collide on one cache entry. See `internal/fanout/review.go:613-628`.

### Strengths (no action)
- Path-traversal-proof filenames: `fileName`/`isHex64` enforce exactly 64 lowercase hex chars before any `filepath.Join` (`store.go:153-187`).
- Self-healing: corrupt/key-mismatched entries are removed and treated as a miss (`store.go:67-73`); cache read/write faults are non-fatal and degrade to a live call (`engine.go:582-588`, `engine.go:613-619`).
- Failed/timed-out reviews are never cached (`engine.go:611`); only `StatusOK` is stored.
- `cache_hit:true` surfaced in `status.json` (`status.go:325-330`, `artifacts.go:221`) — stale-serve is auditable, not invisible.
- Thorough tests directly mapping ACs + clarifications (`internal/fanout/cache_test.go`, `internal/cache/{key,store}_test.go`, `internal/registry/cache_settings_test.go`).

### Not run (working-tree limitation)
- Tests, coverage, `golangci-lint`, `go vet`, `go fmt` — SKIPPED: the epic 5.2 code is not in the local working tree (it is on `origin/main` @ `4f2e6b9`). Reconcile local `main` with `origin/main` to run them.
