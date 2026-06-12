Looking at this diff through a security lens, focusing on the `max_parallel` concurrency cap implementation within the sprint scope.

Key areas reviewed:
- Semaphore implementation in `engine.go`
- Input validation at all tiers (CLI, project config, registry)
- Settings resolution and precedence
- Resource allocation from untrusted/unvalidated input

```
MEDIUM|internal/registry/precedence.go:77|No upper bound on max_parallel — extremely large value causes OOM crash|Add MaxMaxParallel constant (e.g., 1000) and validate `*cli.MaxParallel <= MaxMaxParallel` at L77, plus matching checks in config.go:141 and project.go:108, mirroring the MaxTimeoutSecs=86400 pattern|security|10|if *cli.MaxParallel < 0 { ... } — only negative is rejected; --max-parallel 2000000000 passes validation and causes make(chan struct{}, 2000000000) to attempt ~32GB allocation, crashing the process. A malicious max_parallel in a shared repo's .atcr/config.yaml is a realistic attack vector|greta
MEDIUM|internal/fanout/engine.go:117|No guard on make(chan struct{}, e.maxParallel) for extremely large values — panics on allocation failure before any result is produced|Validate maxParallel <= reasonable upper bound in NewEngine or Run, or rely on settings-layer cap; at minimum add a defensive clamp so a miswired caller cannot OOM the process|security|5|sem = make(chan struct{}, e.maxParallel) — no bounds check; a value of 2e9 attempts a ~32GB ring-buffer allocation|greta
LOW|internal/fanout/engine.go:131|Cancelled goroutine bypasses semaphore and still calls invokeSlot — if invokeSlot races past the context check, peak concurrency briefly exceeds the cap during cancellation|Confirm in a code comment (per TD item) that invokeSlot with a cancelled context returns StatusTimeout without a provider call; consider adding an early ctx.Done() check before invokeSlot in the cancellation branch for defense in depth|security|5|case <-ctx.Done(): results[i] = e.invokeSlot(ctx, s) — no semaphore token held; if invokeSlot starts a provider call before observing cancellation, the cap is violated|greta
```