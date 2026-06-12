# Concept: Reconciler as an Embeddable Library

**Status:** Conceptual  
**Created:** 2026-06-11  
**Priority:** High  

## Problem

ATCR's reconciler is genuinely unique:
- Deterministic clustering (FILE, LINE ± 3)
- Text-similarity dedupe (Jaccard with configurable threshold)
- Confidence scoring (2+ reviewers = HIGH, single = MEDIUM)
- Ambiguity sidecar (gray-zone clusters written to `ambiguous.json`)
- Disagreement annotation (severity conflicts preserved inline)

**Nobody else ships this.** Other multi-reviewer tools (CodeRabbit, Qodo, Copilot) either:
- Don't merge at all (N walls of prose)
- Do prompt-based dedupe (non-deterministic, opaque)
- Do simple concatenation (no dedupe, no confidence)

The reconciler is the hardest part of multi-reviewer merge, and it's already built.

## Solution

Extract `internal/reconcile` into a **standalone Go module** that other tools can embed:

```
github.com/atcr/reconcile
```

### API Surface

```go
package reconcile

// Input: N source streams (8-column pipe-delimited)
type Source struct {
    Name      string   // e.g., "bruce", "greta", "host"
    Findings  []Finding
}

type Finding struct {
    Severity   string
    File       string
    Line       int
    Problem    string
    Fix        string
    Category   string
    EstMinutes int
    Evidence   string
}

// Output: reconciled findings (9-column)
type ReconciledFinding struct {
    Finding
    Reviewers  []string  // comma-joined in output
    Confidence string    // HIGH, MEDIUM, LOW
}

// Main entry point
func Reconcile(sources []Source, opts Options) (*Result, error)

type Result struct {
    Findings   []ReconciledFinding
    Ambiguous  []AmbiguousCluster  // gray-zone clusters
    Summary    Summary             // counts, stats
}

type Options struct {
    LineTolerance    int     // ±3 lines for clustering
    SimilarityThreshold float64  // Jaccard threshold for dedupe
    // ... extensible
}
```

### Usage

Other review tools embed the reconciler:

```go
import "github.com/atcr/reconcile"

// Run your reviewers however you want
reviewer1Findings := runReviewer1(diff)
reviewer2Findings := runReviewer2(diff)

// Reconcile with ATCR's deterministic merge
result, err := reconcile.Reconcile([]reconcile.Source{
    {Name: "reviewer1", Findings: reviewer1Findings},
    {Name: "reviewer2", Findings: reviewer2Findings},
}, reconcile.Options{
    LineTolerance: 3,
    SimilarityThreshold: 0.6,
})

// result.Findings is deduped, confidence-scored, disagreement-annotated
```

### White-Label Option

For competitors who don't want to embed Go:
- **HTTP/gRPC service** — `atcr reconcile` as a microservice
- Input: POST findings streams (8-column)
- Output: reconciled findings (9-column)
- Self-hosted or cloud-hosted

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| Standalone module | Extract `internal/reconcile` into `github.com/atcr/reconcile` | 1 week |
| Clean API | Documented Go API with examples | 3 days |
| Format adapters | Convert from SARIF, JSON, other formats to ATCR's input | 1 week |
| HTTP/gRPC service | `atcr reconcile` as a microservice | 1-2 weeks |
| SDK for other languages | Python, TypeScript wrappers around the HTTP service | 2-3 weeks |

## Revenue Model

**Dual licensing:**
- OSS (Apache 2.0): free for open-source projects, non-commercial use
- Commercial license: paid for proprietary use, white-label

**White-label service:**
- Cloud-hosted reconcile API: $X per 1000 reconciliations
- Self-hosted: enterprise license + support contract

**Consulting:**
- Custom reconciler strategies (e.g., language-specific dedupe)
- Integration assistance for competitors

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| Extract module | 1 week | Move `internal/reconcile` → `github.com/atcr/reconcile`, update imports |
| Clean API | 3 days | Document public API, write examples, add godoc |
| Format adapters | 1 week | SARIF → ATCR, JSON → ATCR, etc. |
| HTTP/gRPC service | 1-2 weeks | Wrap `Reconcile()` in an HTTP/gRPC handler |
| SDKs (Python, TS) | 2-3 weeks | Thin wrappers around the HTTP service |
| **Total** | **5-7 weeks** | Can be phased: module first, service later |

## Moat / Differentiation

- **Nobody else has this** — the reconciler is ATCR's core innovation. Extracting it doesn't weaken ATCR; it makes ATCR the reference implementation.
- **Standards play** — if the reconciler becomes widely adopted, ATCR's findings format becomes the de facto standard (like SARIF for static analysis).
- **Network effects** — more tools using the reconciler → more feedback → better reconciler → more adoption.
- **White-label revenue** — competitors pay to use it, which funds ATCR's development.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Competitors don't adopt | High | Medium | Focus on OSS adoption first; white-label is a secondary channel |
| Extracting the module breaks ATCR | Low | High | Keep ATCR's internal imports pointing to the extracted module; test thoroughly |
| API surface is too narrow | Medium | Medium | Start with `Reconcile(sources, opts)` → `Result`; extend based on feedback |
| White-label service becomes a maintenance burden | Medium | Medium | Keep it simple — just a thin wrapper around the library. No SLA for the cloud service initially |

## Open Questions

- **License?** Apache 2.0 (permissive) vs. GPL (copyleft)? Apache is more friendly to commercial adoption.
- **Module path?** `github.com/atcr/reconcile` (standalone) or `github.com/atcr/atcr/pkg/reconcile` (sub-package)?
- **Versioning?** Semver from day one? How to handle breaking changes?
- **Service protocol?** HTTP/REST (simple, universal) vs. gRPC (fast, typed) vs. both?

## References

- SQLite: OSS, embedded in countless products, dual-licensed for commercial use
- RocksDB: OSS, embedded in Facebook's infrastructure, widely adopted
- Prometheus client libraries: OSS, embedded in thousands of apps, became the standard
