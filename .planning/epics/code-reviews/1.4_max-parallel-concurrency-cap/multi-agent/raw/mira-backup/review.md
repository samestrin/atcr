

Looking at this diff, I need to review it against the sprint plan scope which covers the `max_parallel` concurrency cap implementation. Let me analyze the changes.

## Review Findings

**HIGH|internal/fanout/engine.go:124-137|Potential semaphore slot leak on cancellation after acquire|Semaphore slot acquired before ctx cancellation is lost; brief window where other goroutines may be blocked|performance|10|cancel after acquire: slot held indefinitely until ctx deadline|bruce**

The ctx-aware acquire pattern has a subtle issue: if cancellation occurs *after* the goroutine successfully writes to `sem` but *before* `ctx.Done()` is checked in the select, the goroutine exits without releasing the slot. This slot is now permanently leaked until the parent context times out/deadlines, potentially blocking other goroutines.

The current pattern:
```go
case sem <- struct{}{}:  // acquire succeeds
    defer func() { <-sem }()
case <-ctx.Done():        // ctx fires
    results[i] = e.invokeSlot(ctx, s)
    return
}
results[i] = e.invokeSlot(ctx, s)  // ctx already cancelled here
```

If context cancels between the acquire succeeding and the select completing, the slot is held. The goroutine then calls `invokeSlot` with an already-cancelled context (returning timeout result), and exits — but the `defer` never runs because it was set up conditionally inside the case.

**MEDIUM|internal/fanout/engine.go:127|Comment says "provider call" but semantics bound goroutine spawns|Checkpoint 2 says cap bounds "provider calls" but code gates goroutine existence|maintainability|2|"goroutine still spawns; only the provider call is gated"|bruce**

The comment at line 127 says "Each goroutine still spawns; only the provider call is gated" but the acquire happens *before* `invokeSlot` is called, meaning the goroutine's *execution* is gated, not just the provider call. While functionally correct (bypassed goroutines don't call `invokeSlot`), the comment is slightly misleading given the acceptance criteria language.

**LOW|internal/registry/precedence_test.go:231-232|Redundant assertions in test|DefaultMaxParallel checked twice with different messages|maintainability|1|assert.Equal(t, DefaultMaxParallel...) followed by assert.Equal(t, 10...)|bruce**

```go
assert.Equal(t, DefaultMaxParallel, s.MaxParallel, "embedded default used when nothing overrides")
assert.Equal(t, 10, s.MaxParallel, "v1 ships a max_parallel default of 10")
```

These are functionally identical and the second assertion adds no value.

---

**Summary**: 1 HIGH issue (semaphore slot leak on post-acquire cancellation), 1 MEDIUM (comment accuracy), 1 LOW (test redundancy). The HIGH issue is an edge case already tracked in technical debt (TD from epic notes), but worth flagging as it represents a real correctness concern in the implementation.