# reconcile

[![Go Reference](https://pkg.go.dev/badge/github.com/samestrin/atcr/reconcile.svg)](https://pkg.go.dev/github.com/samestrin/atcr/reconcile)

A deterministic, disagreement-preserving reconciler for multi-reviewer findings.

`reconcile` merges findings from several reviewers or tools into one ordered,
deduplicated result — clustering by location, deduping by text similarity,
scoring confidence, and preserving disagreement inline rather than discarding it.
The same input always yields byte-identical output, which makes the library a
credible reference implementation that other tools can embed.

This is the standalone extraction of ATCR's core reconciler. It is **stdlib-only**
(no third-party dependencies) and ships with one JSON format adapter.

## Install

```sh
go get github.com/samestrin/atcr/reconcile
```

Requires Go 1.25 or newer.

## Quickstart

The snippet below mirrors the runnable [`ExampleReconcile`](example_test.go).
Two reviewers report the same issue at the same location with different
severities; the reconciler merges them, unions the reviewers, raises confidence,
and annotates the severity disagreement.

```go
package main

import (
	"fmt"

	"github.com/samestrin/atcr/reconcile"
)

func main() {
	sources := []reconcile.Source{
		{Name: "reviewer-a", Findings: []reconcile.Finding{{
			Severity: "medium", File: "auth.go", Line: 88,
			Problem: "missing nil check on session", Fix: "add nil guard",
			Category: "bug", EstMinutes: 15, Evidence: "stack trace", Reviewer: "reviewer-a",
		}}},
		{Name: "reviewer-b", Findings: []reconcile.Finding{{
			Severity: "high", File: "auth.go", Line: 88,
			Problem: "missing nil check on session", Fix: "guard session before deref",
			Category: "bug", EstMinutes: 20, Evidence: "local repro", Reviewer: "reviewer-b",
		}}},
	}

	result := reconcile.Reconcile(sources, reconcile.Options{})

	f := result.Findings[0]
	fmt.Printf("severity=%s file=%s line=%d\n", f.Severity, f.File, f.Line)
	fmt.Printf("reviewers=%v confidence=%s\n", f.Reviewers, f.Confidence)
	fmt.Printf("disagreement=%q\n", f.Disagreement)
	// severity=HIGH file=auth.go line=88
	// reviewers=[reviewer-a reviewer-b] confidence=HIGH
	// disagreement="MEDIUM vs HIGH"
}
```

## Public API

The public surface is **lifted as-is** from ATCR — a value-returning entry point
with no error and no wrapper types. Run `go doc github.com/samestrin/atcr/reconcile`
for the full generated documentation.

### Entry point

```go
func Reconcile(sources []Source, opts Options) Result
```

### Types

```go
// Source is one reconcile input: a named origin and the findings it produced.
type Source struct {
	Name     string
	Findings []Finding
}

// Finding is the unified wire-format finding (the nine wire fields plus the
// reconciled reviewer list, confidence, disagreement, and optional verification).
type Finding struct {
	Severity     string
	File         string
	Line         int
	Problem      string
	Fix          string
	Category     string
	EstMinutes   int
	Evidence     string
	Reviewer     string        // per-source 8th column
	Reviewers    []string      // reconciled 8th column
	Confidence   string        // reconciled 9th column
	Disagreement string
	Verification *Verification
}

// Merged is one reconciled output finding (embeds Finding).
type Merged struct {
	Finding
}

// Options parameterizes a run.
type Options struct {
	ReconciledAt time.Time      // stamps the summary; zero falls back to now (UTC)
	Partial      bool           // true when some expected source was missing/unreadable
	Merges       map[string]bool // ambiguous cluster ids a host adjudicated as duplicates
	Root         string         // base dir for embedder-layered path validation (core ignores it)
}

// Result is a completed reconciliation.
type Result struct {
	Findings  []Merged
	Ambiguous []AmbiguousCluster
	Summary   Summary
}

// Summary is the run-stats record (sources scanned, per-source counts,
// clusters collapsed, severity disagreements, totals, reconciled_at, ...).
type Summary struct { /* ... */ }

// Verification is the per-finding adversarial-verification block.
type Verification struct {
	Verdict string // confirmed | refuted | unverifiable
	Skeptic string
	Notes   string
}

// Verdict enum values for Verification.Verdict.
const (
	VerdictConfirmed    = "confirmed"
	VerdictRefuted      = "refuted"
	VerdictUnverifiable = "unverifiable"
)
```

## Behavior

The reconciler is a deterministic pipeline. Determinism is a feature: identical
input produces byte-identical output, so embedders can hash, diff, or cache the
result safely.

- **Clustering — `FILE`, `LINE ± 3`.** Findings are grouped into location
  clusters keyed on `FILE` and a `LINE ± 3` window (single-linkage). Output is
  then sorted by a total order — **severity descending, then file, then line** —
  so emission is stable.
- **Dedupe — optimal bipartite matching.** Within a cluster, findings from
  different sources are matched 1:1 by the Kuhn-Munkres (Hungarian) algorithm
  over a composite edge-weight distance: **0** for findings sharing an
  AST-isomorphism group key, otherwise **`1 − token-set Jaccard`**. Acceptance of
  a match is gated by the same integer cross-multiplication boundary (no float
  comparison): a pair at or **above 0.7 similarity merges**. The 1:1 constraint
  structurally avoids the transitive over-merge a greedy single-linkage pass
  suffers (one finding cannot drag two non-duplicates into one group). A pair in
  the **0.4–0.7 gray zone is ambiguous** — left unmerged and recorded in the
  ambiguity sidecar; below 0.4 the findings are distinct.
- **Merge — max severity, disagreement preserved.** A merged finding takes the
  **maximum severity** across its group; when lower severities are present the
  conflict is annotated inline as `<lo> vs <hi>` (e.g. `MEDIUM vs HIGH`) rather
  than dropped. Reviewers are unioned and sorted; problem/fix take the longest
  text; category is modal; estimated minutes is the max; evidence is
  reviewer-prefixed and concatenated.
- **Confidence — v2 tiers `VERIFIED > HIGH > MEDIUM > LOW`.** Confidence is
  derived from the count of distinct reviewers who agree, and is promoted to
  `VERIFIED` when an adversarial verdict confirms the finding.
- **Ambiguity sidecar.** Three kinds of cluster surface in `Result.Ambiguous` as
  `AmbiguousCluster` values, neither silently merged nor dropped: **gray-zone
  pairs** (0.4–0.7) a host can adjudicate (and force a merge via
  `Options.Merges`); **DBSCAN-isolated noise** — a single uncorroborated
  finding standing alone amid a corroborated cluster (a likely single-model
  hallucination), carried as a one-finding cluster and removed from the merged
  output so the consensus findings stay trustworthy; and **consensus-filtered
  singletons** — on a panel of at least three **distinct reviewers** (counted
  across findings, not source directories, since discovery flattens every pool
  persona into one `pool` source), any uncorroborated singleton (a below-`HIGH`
  confidence finding) is routed to the sidecar rather than promoted, UNLESS it is
  security-related, `HIGH`/`CRITICAL` severity, out-of-scope, or independently
  confirmed. The `Summary.ConsensusFiltered` count records how many were routed
  this way; a two-reviewer panel (the host + 1 pool persona workflow) leaves the
  filter inert so real findings are never dropped wholesale.

## JSON format adapter

The [`adapter/json`](adapter/json) package converts an external JSON finding
stream to `[]reconcile.Source` and a `reconcile.Result` back to a versioned JSON
document. Its schema family, **`reconcile-json/v1`**, is versioned **independently**
of ATCR's internal `atcr-findings/v1` wire format; evolution within v1 is
additive-only and unknown producer fields are ignored on decode. ATCR-internal
path-validation fields are not part of the schema.

```go
sources, err := json.Decode(inputBytes)        // []reconcile.Source
out, err := json.Encode(result, reconcile.Options{}) // reconcile-json/v1 document
```

## Licensing

This module is **dual-licensed**:

- **[Apache 2.0](LICENSE)** — for open-source use.
- **[Commercial license](LICENSE-COMMERCIAL.md)** — a placeholder for proprietary
  or closed-source embedding; contact path inside.

The project ships **no** license-enforcement, license-check, or payment-gating
code — see [`LICENSE-COMMERCIAL.md`](LICENSE-COMMERCIAL.md) for why that is
intentional.
