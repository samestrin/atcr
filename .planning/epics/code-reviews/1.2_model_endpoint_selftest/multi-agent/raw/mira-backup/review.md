

Looking at this diff implementing the `atcr doctor` command (epic 1.2), I'll analyze it against the sprint scope.

**Files in scope for review:**
- `cmd/atcr/doctor.go` - CLI command
- `cmd/atcr/doctor_test.go` - CLI tests
- `internal/doctor/render.go` - Table/JSON rendering
- `internal/doctor/resolve.go` - Roster resolution + dedup
- `internal/doctor/resolve_test.go` - Resolution tests
- `internal/doctor/run.go` - Probing logic
- `internal/doctor/run_test.go` - Run tests
- `internal/llmclient/client.go` - HTTPStatusError export + MaxTokens

Let me identify the issues:

```json
[
  {
    "SEVERITY": "MEDIUM",
    "FILE": "cmd/atcr/doctor.go:84",
    "PROBLEM": "RenderJSON error is silently ignored despite returning error",
    "FIX": "Check and return the error from RenderJSON like RenderJSON does",
    "CATEGORY": "error-handling",
    "EST_MINUTES": 5,
    "EVIDENCE": "if err := doctor.RenderJSON(cmd.OutOrStdout(), rep); err != nil { return err }",
    "REVIEWER": "bruce"
  },
  {
    "SEVERITY": "LOW",
    "FILE": "internal/doctor/run.go:117",
    "PROBLEM": "Goroutine panic in probe() would leak a semaphore slot and deadlock Run",
    "FIX": "Add defer recover() or move semaphore release before probe() call",
    "CATEGORY": "correctness",
    "EST_MINUTES": 10,
    "EVIDENCE": "go func(i int) { ... results[i] = probe(...); <-sem }(i)",
    "REVIEWER": "bruce"
  },
  {
    "SEVERITY": "LOW",
    "FILE": "internal/doctor/render.go:25",
    "PROBLEM": "RenderTable returns no error and discards tabwriter Flush error",
    "FIX": "Return error from RenderTable or log the Flush failure",
    "CATEGORY": "correctness",
    "EST_MINUTES": 5,
    "EVIDENCE": "_ = tw.Flush() — failed table write is silent",
    "REVIEWER": "bruce"
  },
  {
    "SEVERITY": "LOW",
    "FILE": "internal/doctor/run.go:200",
    "PROBLEM": "StatusNetworkError detail surfaces err.Error() containing base_url host/path",
    "FIX": "Document that base_url (never the key) may appear in network_error detail",
    "CATEGORY": "security",
    "EST_MINUTES": 5,
    "EVIDENCE": "detail: bounded(err.Error()) — can leak provider hostname",
    "REVIEWER": "bruce"
  }
]
```

**Summary:**

1. **MEDIUM** - `cmd/atcr/doctor.go:84`: `RenderJSON` returns an error that's completely ignored. While JSON encoding to an in-memory writer rarely fails, this creates an inconsistency with `RenderTable` semantics and would mask a broken writer. The pattern used elsewhere in the file (lines 79-82) correctly checks errors.

2. **LOW** - `internal/doctor/run.go:117`: The goroutine pattern acquires the semaphore slot, then calls `probe()`, then releases the slot in a defer. If `probe()` panics, the defer never runs and the slot leaks, causing `wg.Wait()` to deadlock. Moving the release before `probe()` or adding `defer recover()` would fix this.

3. **LOW** - `internal/doctor/render.go:25`: Already tracked in technical debt. `tabwriter.Flush()` errors are discarded.

4. **LOW** - `internal/doctor/run.go:200`: The network error detail uses `err.Error()` directly. While `bounded()` constrains size, the raw error message can contain the provider's hostname/path. The tech debt note correctly identifies this as a documentation issue rather than a code defect (API keys live in Authorization headers, not error messages).