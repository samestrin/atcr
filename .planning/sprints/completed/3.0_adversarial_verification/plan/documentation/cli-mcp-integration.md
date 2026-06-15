# CLI & MCP Integration [CRITICAL]

## Overview

Epic 3.0 adds the `atcr verify` CLI subcommand and the `atcr_verify` MCP tool. The CLI subcommand follows the established Cobra pattern (separate file in `cmd/atcr/`, registered in `main.go`). The MCP tool follows the handler pattern (registration in `server.go:buildServer`, handler in `handlers.go`).

The `atcr review --verify` flag chains `review → reconcile → verify` in one run, mirroring the existing `--reconcile` chaining behavior.

> Source: [codebase-discovery.json:existing_patterns → CLI Command Structure (Cobra)]
> Source: [codebase-discovery.json:existing_patterns → MCP Tool Handlers + Registration]

---

## Key Concepts

### Cobra Command Structure

- Each subcommand is a separate file in `cmd/atcr/` (e.g., `review.go`, `reconcile.go`).
- Commands are registered in `cmd/atcr/main.go` at the `AddCommand` call (line 97).
- Pattern: define command with `cobra.Command`, parse flags, load registry, call internal package, emit output.
- Tests use golden files and mock providers.

> Source: [cobra.md:Core API → Command struct]
> Source: [codebase-discovery.json:existing_patterns → CLI Command Structure]

### `atcr verify` Subcommand

- File: `cmd/atcr/verify.go`
- Flags:
  - `--fresh`: re-verify all findings, even those already VERIFIED
  - `--thorough`: use 3 skeptics with majority rule (default: 1 skeptic)
  - `--min-severity`: minimum severity floor for verification (default: MEDIUM)
- Behavior:
  1. Load registry
  2. Load reconciled findings via `reconcile.ReadReconciledFindings(reviewDir)`
  3. Call `internal/verify.Verify(findings, opts)`
  4. Write `reconciled/verification.json`
  5. Re-emit `reconciled/findings.json` with verification blocks
  6. Update `manifest.json` stages to include `"verify"`
  7. Update `summary.json` with `verdictCounts`

> Source: [original-requirements.md:Pipeline placement]
> Source: [plan.md:Technical Planning Notes]

### `--verify` Chaining Flag

- Added to `cmd/atcr/review.go`
- Chains: `review → reconcile → verify`
- Mirrors the existing `--reconcile` flag behavior

> Source: [original-requirements.md:Pipeline placement]
> Source: [plan.md:Technical Planning Notes]

### MCP Tool Registration

- Tools are registered in `internal/mcp/server.go:buildServer()` (line 57) via `registerTool()`.
- Each tool has a handler in `internal/mcp/handlers.go`.
- Pattern: `handleReview` (line 84), `handleReconcile` (line 159), `handleReport` (line 222).

> Source: [mcp-sdk.md:Tool registration]
> Source: [codebase-discovery.json:existing_patterns → MCP Tool Handlers]

### `atcr_verify` MCP Tool

- Registered in `internal/mcp/server.go:buildServer()` alongside `atcr_review`, `atcr_reconcile`, etc.
- Handler: `handleVerify` in `internal/mcp/handlers.go` (following `handleReconcile` pattern at line 159).
- Input schema: same as CLI flags (`--fresh`, `--thorough`, `--min-severity`).
- Output: structured result with verification summary, verdict counts, gate status.

> Source: [codebase-discovery.json:integration_points → mcp/server.go:57]
> Source: [codebase-discovery.json:integration_points → mcp/handlers.go:159]

### Gate Semantics Update

- `--fail-on <severity>`: counts findings at/above threshold whose verdict is **not `refuted`**.
- `--fail-on high --require-verified`: counts **only VERIFIED** findings (strictest gate).
- Targets:
  - CLI path: `CountAtOrAbove` at `internal/reconcile/gate.go:57`
  - MCP path: `failingFindings` at `internal/mcp/handlers.go:339`
- Both must be updated to:
  1. Exclude findings with `verdict=='refuted'`
  2. Support `--require-verified` (count only `confidence=='VERIFIED'`)

> Source: [original-requirements.md:Gate semantics]
> Source: [codebase-discovery.json:integration_gaps → Gate function naming mismatch]

---

## Code Examples

### Cobra Command Definition (Pattern to Follow)

```go
// From cmd/atcr/reconcile.go (pattern to follow)
var verifyCmd = &cobra.Command{
    Use:   "verify [id-or-path]",
    Short: "Run adversarial verification on reconciled findings",
    Long:  `Run skeptic agents against deduped findings to produce verdicts (confirmed/refuted/unverifiable).`,
    Args:  cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        // Load registry
        reg, err := registry.Load(registryPath)
        if err != nil {
            return err
        }

        // Load reconciled findings
        findings, err := reconcile.ReadReconciledFindings(reviewDir)
        if err != nil {
            return err
        }

        // Call internal/verify
        opts := verify.Options{
            Fresh:       fresh,
            Thorough:    thorough,
            MinSeverity: minSeverity,
        }
        result, err := verify.Verify(findings, reg, opts)
        if err != nil {
            return err
        }

        // Emit verification.json, re-emit findings.json, update manifest
        return result.Emit(reviewDir)
    },
}

func init() {
    verifyCmd.Flags().BoolVar(&fresh, "fresh", false, "Re-verify all findings")
    verifyCmd.Flags().BoolVar(&thorough, "thorough", false, "Use 3 skeptics with majority rule")
    verifyCmd.Flags().StringVar(&minSeverity, "min-severity", "MEDIUM", "Minimum severity floor")
}
```

> Source: [cobra.md:Core API → Command struct]
> Source: [codebase-discovery.json:existing_patterns → CLI Command Structure]

### MCP Tool Registration

```go
// From internal/mcp/server.go:buildServer() (line 57).
// atcr wraps the SDK's mcp.AddTool in a registrar so duplicate names and bad
// schemas fail fast without panicking the server process.
func buildServer(...) *mcpsdk.Server {
    // ... other tools ...

    registerTool(r, &mcpsdk.Tool{
        Name:        ToolVerify,
        Description: "Run adversarial verification on reconciled findings",
    }, e.handleVerify)

    return s
}
```

> Source: [mcp-sdk.md:Tool registration]
> Source: [codebase-discovery.json:integration_points → mcp/server.go:57]

### MCP Handler Pattern

```go
// From internal/mcp/handlers.go:handleReconcile (line 159) — pattern to follow
func handleVerify(ctx context.Context, args VerifyArgs) (*VerifyResult, error) {
    // Parse args
    reviewDir := args.Path
    opts := verify.Options{
        Fresh:       args.Fresh,
        Thorough:    args.Thorough,
        MinSeverity: args.MinSeverity,
    }

    // Load registry and findings
    reg, err := registry.Load(args.RegistryPath)
    if err != nil {
        return nil, err
    }
    findings, err := reconcile.ReadReconciledFindings(reviewDir)
    if err != nil {
        return nil, err
    }

    // Run verification
    result, err := verify.Verify(findings, reg, opts)
    if err != nil {
        return nil, err
    }

    // Emit and return
    if err := result.Emit(reviewDir); err != nil {
        return nil, err
    }

    return &VerifyResult{
        VerdictCounts: result.VerdictCounts,
        GateStatus:    result.GateStatus,
    }, nil
}
```

> Source: [codebase-discovery.json:existing_patterns → MCP Tool Handlers]

### Gate Logic Update

```go
// From internal/reconcile/gate.go:CountAtOrAbove (line 57) — CLI/reconcile path.
// The helper already operates on []Merged and skips out-of-scope findings.
// Epic 3.0 also skips refuted findings and, when --require-verified is set,
// counts only findings whose effective confidence is VERIFIED.
func CountAtOrAbove(findings []Merged, threshold string, requireVerified bool) int {
    count := 0
    for _, f := range findings {
        if f.Category == CategoryOutOfScope {
            continue
        }
        if f.Verification != nil && f.Verification.Verdict == "refuted" {
            continue
        }
        if requireVerified && f.Confidence != "VERIFIED" {
            continue
        }
        if AtOrAbove(f.Severity, threshold) {
            count++
        }
    }
    return count
}

// From internal/mcp/handlers.go:failingFindings (line 339) — MCP path.
// This helper converts []Merged to []JSONFindings and filters by threshold.
// Epic 3.0 mirrors the same refuted-exclusion and --require-verified logic.
func failingFindings(res reconcile.Result, threshold string, requireVerified bool) []reconcile.JSONFinding {
    all := res.JSONFindings()
    out := make([]reconcile.JSONFinding, 0, len(all))
    for _, f := range all {
        if f.Verification != nil && f.Verification.Verdict == "refuted" {
            continue
        }
        if requireVerified && f.Confidence != "VERIFIED" {
            continue
        }
        if reconcile.AtOrAbove(f.Severity, threshold) {
            out = append(out, f)
        }
    }
    return out
}
```

> Source: [codebase-discovery.json:integration_points → reconcile/gate.go:57]
> Source: [original-requirements.md:Gate semantics]

---

## Quick Reference

| Concept | Location | Notes |
|---------|----------|-------|
| AddCommand | `cmd/atcr/main.go:97` | Register `verify` subcommand here |
| verify.go | `cmd/atcr/verify.go` | New file: Cobra command definition |
| review.go | `cmd/atcr/review.go` | Add `--verify` flag |
| buildServer | `internal/mcp/server.go:57` | Register `atcr_verify` tool |
| handleVerify | `internal/mcp/handlers.go` | New handler following `handleReconcile` pattern |
| CountAtOrAbove | `internal/reconcile/gate.go:57` | Update to skip refuted |
| failingFindings | `internal/mcp/handlers.go:339` | Update to skip refuted, support `--require-verified` |

---

## Related Documentation

- [Verification Pipeline Architecture](verification-pipeline.md) — core mechanics, verdict parsing, confidence v2
- [LLM Integration & Tool Loop](llm-tool-loop.md) — skeptic invocation via `invokeToolLoop`
- [Testing & Fixtures](testing-fixtures.md) — CLI and MCP integration tests
