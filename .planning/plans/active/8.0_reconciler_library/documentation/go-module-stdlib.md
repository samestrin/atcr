# Go Module & Standard Library [CRITICAL]

## Overview

The reconcile library extraction turns ATCR's deterministic reconciler into a standalone, stdlib-only Go module at `github.com/samestrin/atcr/reconcile`. The module foundation rests on Go 1.25 and a nested-module layout: a `go.mod` at `./reconcile/` consumed by the root `go.mod` through a `replace` directive during development. This keeps the library co-located with ATCR (its reference implementation) while giving it an independently versionable, inspectable boundary that external tools can embed without importing the ATCR binary.

The standard library is the deliberate and complete dependency surface for non-test files. The Go standard library is "a comprehensive collection of packages that ships with every Go installation" providing "built-in functionality for text processing, I/O, networking, cryptography, data encoding, operating system interactions, and concurrency — eliminating the need for external dependencies for most common programming tasks." testify and other test helpers are confined to `*_test.go` files so the shipped library stays pure-stdlib. The JSON adapter (`reconcile-json/v1`) is built on `encoding/json` rather than any third-party schema library.

The reconcile library's reliance on specific stdlib packages maps directly onto its algorithmic responsibilities: `sort` for the deterministic total-order output that makes the reconciler a credible CI gate, `strings` for severity normalization, and `encoding/json` for the adapter's field mapping. The library itself is synchronous and stateless; `context` and `sync` are not part of its public API or internal implementation. They remain concerns of ATCR's fan-out engine and other consumers that orchestrate parallel agent invocations around calls to `reconcile.Reconcile`. "The library is organized hierarchically into functional domains: standalone top-level packages (e.g., `fmt`, `os`, `context`) and grouped sub-packages under parent directories (e.g., `net/http`, `encoding/json`, `crypto/tls`)."

## Key Concepts

### Go 1.25 toolchain and standalone nested module
The plan's Framework/Technology line fixes the foundation: "Go 1.25; standalone nested module (`go.mod` at `./reconcile/`); stdlib-only library + `encoding/json` adapter; testify tests; golangci-lint; GitHub Actions CI."
> Source: [plan.md:Framework/Technology]

The architecture note on the nested module is explicit: "./reconcile/ with its own go.mod, consumed via root go.mod replace github.com/samestrin/atcr/reconcile => ./reconcile. Separate-repo publication follows extraction, does not precede it."
> Source: [codebase-discovery.json:architecture_notes]

### replace directive during development
The root go.mod wiring recommendation is: "Root go.mod should use replace github.com/samestrin/atcr/reconcile => ./reconcile during development; remove or update the replace only when publishing the library to a separate repository."
> Source: [codebase-discovery.json:architecture_recommendations]

A new file `reconcile/go.mod` is created as the "New standalone module github.com/samestrin/atcr/reconcile (based_on: root go.mod)".
> Source: [codebase-discovery.json:files_to_create]

### stdlib-only constraint for non-test files
The architecture recommendation is firm: "Keep the reconcile/ module strictly stdlib-only in non-test files; testify and other test helpers are allowed only in *_test.go files."
> Source: [codebase-discovery.json:architecture_recommendations]

This aligns with ATCR's broader Epic 1.0 constraint: "Everything in atcr beyond the five direct third-party dependencies (cobra, mcp-go-sdk, yaml.v3, testify, jsonschema-go) is intentionally standard library (Epic 1.0 constraint: keep the dependency tree small)."
> Source: [standard-library.md:Overview]

### Self-contained package ready for extraction
The existing reconcile package is already structured for extraction: "internal/reconcile is already a black-box package: one public entry (Reconcile) + public types (Source/Merged/Result/Options/Summary) + private helpers. Imports only internal/stream."
> Source: [codebase-discovery.json:existing_patterns]

### Deterministic total-order output (sort)
The reconciler's credibility as a deterministic CI gate depends on sort ordering: "sortMerged orders by severity desc, then file, then line so the same input yields byte-identical artifacts — required for a deterministic CI gate." The follow-for guidance: "Preserve the sort exactly; it is what makes the reconciler a credible reference implementation."
> Source: [codebase-discovery.json:existing_patterns]

This relies on the stdlib `sort` package, described as "Sorting primitives for slices and user-defined collections."
> Source: [go.md:Core API/Top-Level Packages]

### Severity normalization (strings)
The library becomes the canonical owner of `SeverityRank` and `NormalizeSeverity`: "Canonical severity rubric + normalizer. Pure (strings only). Moves into the library as the canonical owner; reconcile's local SeverityRank copy (merge.go:30) then sources from the library." The current independent-copy pattern is documented: "merge.go:30 copies stream.SeverityRank at package init so mutations to one map cannot corrupt the other."
> Source: [codebase-discovery.json:existing_patterns]

Severity normalization uses the stdlib `strings` package: "Simple functions to manipulate UTF-8 encoded strings."
> Source: [go.md:Core API/Top-Level Packages]

### JSON adapter (encoding/json)
The JSON adapter uses struct tags for field mapping, the stdlib pattern: "`encoding/json` uses struct tags for field mapping." The adapter schema `reconcile-json/v1` maps external finding streams to `[]Source` and `Result` back out, with "Field names ... the library Finding's JSON tags; path-validation fields (PathValid/PathWarning/PathSuggestion/ClusterMerged) are ATCR-internal and NOT part of the external schema. omitempty on disagreement/verification so absence is byte-stable."
> Source: [codebase-discovery.json:integration_gaps]

### Synchronous library; context/sync stay in ATCR consumers
The extracted library does not accept a `context.Context` and does not spawn goroutines. `Reconcile(sources []Source, opts Options) Result` is a pure, deterministic function: given the same inputs it always returns the same outputs. This is intentional — it keeps the public API minimal, makes the library trivial to embed, and preserves the byte-identical output guarantee that underpins AC#3.

ATCR's fan-out engine uses stdlib concurrency patterns around calls to the library: "One `context.WithTimeout` wraps the whole review (global timeout); each agent invocation derives its own `context.WithTimeout` from it." Parallel lanes coordinate with `sync.WaitGroup`.
> Source: [standard-library.md:context + sync — fan-out engine]

The stdlib packages backing ATCR's orchestration are: `context` "Defines the `Context` type, which carries deadlines, cancellation signals, and request-scoped values across API boundaries" and `sync` "Basic synchronization primitives such as mutual exclusion locks."
> Source: [go.md:Core API/Top-Level Packages]

## Code Examples

These are the verbatim stdlib snippets from the source docs. No reconcile-library Go code is invented here.

### Error Handling (explicit error returns)

```go
result, err := someFunc()
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}
```

> Source: [go.md:Common Patterns/Error Handling]

### Context Propagation (cancellation and deadlines) — ATCR consumers, not the library

The library's `Reconcile` does not take `context.Context`. This snippet shows the stdlib pattern ATCR's fan-out engine uses *around* calls to the library.

```go
func DoWork(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case result := <-compute():
        return nil
    }
}
```

> Source: [go.md:Common Patterns/Context Propagation]

### Struct Tags for JSON (encoding/json field mapping)

```go
type User struct {
    Name  string `json:"name"`
    Email string `json:"email,omitempty"`
}
```

> Source: [go.md:Common Patterns/Struct Tags for JSON]

### Resource Management with defer

```go
f, err := os.Open("file.txt")
if err != nil {
    return err
}
defer f.Close()
```

> Source: [go.md:Common Patterns/Resource Management with defer]

## Quick Reference

| Topic | Detail | Source |
|-------|--------|--------|
| Go version | 1.25.0 | go.md |
| Module path | `github.com/samestrin/atcr/reconcile` | codebase-discovery.json:architecture_notes |
| Module location | `./reconcile/` with its own `go.mod` | codebase-discovery.json:architecture_notes |
| Root wiring | `replace github.com/samestrin/atcr/reconcile => ./reconcile` | codebase-discovery.json:architecture_recommendations |
| Dependency policy | stdlib-only in non-test files; testify only in `*_test.go` | codebase-discovery.json:architecture_recommendations |
| `sort` | Deterministic total-order output (severity desc, file, line) | codebase-discovery.json:existing_patterns |
| `strings` | Severity normalization (NormalizeSeverity/SeverityRank) | codebase-discovery.json:existing_patterns |
| `encoding/json` | JSON adapter (`reconcile-json/v1`) field mapping | go.md:Common Patterns |
| Concurrency model | Library is synchronous; `context`/`sync` stay in ATCR fan-out engine | standard-library.md:context + sync — fan-out engine |
| JSON adapter schema | `reconcile-json/v1`, versioned independently of `atcr-findings/v1` | codebase-discovery.json:integration_gaps |
| Public API | `Reconcile(sources []Source, opts Options) Result` lifted as-is | codebase-discovery.json:existing_patterns |

## Related Documentation

- [Go Standard Library Reference](https://pkg.go.dev/std) — official stdlib docs (go.md)
- [go.md](../../../../specifications/packages/go.md) — Go toolchain and stdlib package catalog
- [standard-library.md](../../../../specifications/packages/standard-library.md) — ATCR stdlib subsystem mapping
- [codebase-discovery.json](../codebase-discovery.json) — reconciler extraction patterns, files_to_create, integration_gaps
- [plan.md](../plan.md) — 8.0 reconciler library plan goal, framework, and success criteria
