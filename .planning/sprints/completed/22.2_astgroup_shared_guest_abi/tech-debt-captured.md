# Tech Debt Captured — Sprint 22.2 (Shared Guest ABI Extraction)

Deferred findings surfaced during `/execute-sprint`. Read by `/execute-code-review` and pre-seeded into the adversarial TD stream (SOURCE=execute-sprint).

## TD-001 — guestabi.Lookup drops n-bounds check; callers must re-validate (MEDIUM)
**Origin:** Phase 1, task 1.1.A adversarial review, 2026-07-13
**File:** internal/astgroup/parsers/src/guestabi/guestabi.go:47
**Issue:** guestabi.Lookup returns the raw pinned buffer without the `int(n) > len(buf)` guard that the original parse() co-located with the `pins[ptr]` read. Every parser's parse() still performs the guard itself, but Lookup's doc comment does not warn future callers to re-validate n before slicing buf[:n], risking a guest trap on a malformed host-supplied n.
**Why accepted:** By design — Lookup mirrors the comma-ok map index exactly per the task-01 spec; all three current call sites retain the `int(n) > len(buf)` check, so there is no live defect. This is a documentation/hardening nicety, below the CRITICAL/HIGH inline-fix bar.
**Fix in:** future guestabi hardening — add a doc note on Lookup that callers MUST bounds-check n against len(buf), or fold the length check into a `Lookup(p, n int32) ([]byte, bool)` variant.

## TD-002 — Emit pointer packing sign-extends for high-bit guest pointers (LOW)
**Origin:** Phase 1, task 1.1.A adversarial review, 2026-07-13
**File:** internal/astgroup/parsers/src/guestabi/guestabi.go:65
**Issue:** `(int64(p) << 32)` sign-extends when p (int32) has its high bit set (guest address >= 2GB), corrupting the high 32 bits the host decodes as the result pointer. Masking via `int64(uint32(p))` would be correct.
**Why accepted:** Inherited byte-for-byte from the original per-parser emit; NOT a regression. Guest linear memory for these parsers stays far below 2GB, so the high bit is never set in practice. Deliberately preserved to keep the extraction a pure mechanical refactor (AC3: existing tests pass unchanged); changing it here would be an out-of-scope opportunistic behavior change.
**Fix in:** a dedicated correctness sprint — mask with `int64(uint32(p))` in guestabi.Emit (single canonical location now), alongside a host-side test exercising a high-address pointer.

## TD-003 — Emit result buffers rely on host to free resPtr; pins can grow unbounded (LOW)
**Origin:** Phase 1, task 1.1.A adversarial review, 2026-07-13
**File:** internal/astgroup/parsers/src/guestabi/guestabi.go:63
**Issue:** Emit pins a fresh result buffer on every call and never frees it; it relies on the host calling free(resPtr) after reading the result. If the host neglects a result pointer, the pins map grows unbounded for the lifetime of the guest instance.
**Why accepted:** Inherited from the original emit; the memory-protocol contract already obligates the host to free every returned resPtr. Now the single canonical copy, so the contract is documented in one place. No code change needed for fidelity.
**Fix in:** documentation / host-contract test — assert the astgroup wazero host frees every returned resPtr, or add a guest-side reuse strategy in a future guestabi hardening pass.

## TD-004 — pyparser/main.go lacks //go:build wasip1 tag, breaking host-GOOS tooling after guestabi import (MEDIUM)
**Origin:** Phase 2, task 2.1.A adversarial review, 2026-07-13
**File:** internal/astgroup/parsers/src/pyparser/main.go:1
**Issue:** Unlike goparser and braceparser, pyparser/main.go carries no `//go:build wasip1` tag. Now that it imports the wasip1-tagged guestabi package, a non-wasip1 GOOS can no longer compile/vet the pyparser module (main.go is always included, pulls in guestabi, and guestabi has zero buildable files under host GOOS). This is asymmetric with the other two parsers, which self-guard the same import via their build tag.
**Why accepted:** build.sh and CI always build under `GOOS=wasip1`, and the parent module's `go build ./...` excludes the isolated nested module entirely, so no supported build path regresses and no sprint success criterion is violated. Impact is limited to editor/gopls/`go vet` run directly in the pyparser dir under host GOOS. The task-02 spec deliberately documented pyparser's tag-less state and worked around it by mandating `GOOS=wasip1` for vet, so this is below the CRITICAL/HIGH inline-fix bar.
**Fix in:** guestabi consistency follow-up — add `//go:build wasip1` as pyparser/main.go's first line so all three parser entrypoints are uniformly wasip1-tagged.

## TD-005 — Negative n bypasses the parse() bounds guard in goparser and pyparser (LOW)
**Origin:** Phase 2, task 2.1.A adversarial review, 2026-07-13
**File:** internal/astgroup/parsers/src/goparser/main.go:52
**Issue:** `if !ok || int(n) > len(buf)` is false for a negative n, so a host-supplied n < 0 falls through to `buf[:n]` (goparser) / `string(buf[:n])` (pyparser) and panics with slice-out-of-range. braceparser already guards this with `n < 0 || int(n) > len(buf)`; goparser and pyparser do not.
**Why accepted:** Pre-existing — the task-02 refactor only swapped the `pins[ptr]` read for `guestabi.Lookup(ptr)` and did not touch the guard. The host controls n and never supplies a negative value in practice, so risk is low; changing the guard is out of scope for a pure mechanical extraction.
**Fix in:** a hardening pass — align goparser/pyparser parse() guards with braceparser's `int(n) < 0 || int(n) > len(buf)`.

## TD-006 — emit delegation style diverges: braceparser inlines guestabi.Emit, goparser/pyparser keep a local wrapper (LOW)
**Origin:** Phase 3, task 3.1.A adversarial review, 2026-07-13
**File:** internal/astgroup/parsers/src/braceparser/main.go:38
**Issue:** braceparser calls `guestabi.Emit(...)` directly at all three call sites, while goparser and pyparser retain a local `func emit(n node) int64 { return guestabi.Emit(n) }` and call `emit(...)`. Functionally identical, but the three parsers are structurally inconsistent.
**Why accepted:** Each parser followed its own task spec exactly — task-02 prescribed the local `emit` delegate for goparser/pyparser; task-03 prescribed replacing braceparser's call sites with `guestabi.Emit(...)` directly. No behavioral difference and below the CRITICAL/HIGH inline-fix bar.
**Fix in:** a consistency follow-up — converge on one style (preferably drop the goparser/pyparser `emit` wrappers so all three call `guestabi.Emit` directly).
