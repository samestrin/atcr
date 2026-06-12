# Reconciler & Findings Stream [CRITICAL]

## Overview

The reconciler is the net-new core of atcr v1: a deterministic Go pipeline that merges findings from N reviewer personas into a deduplicated, confidence-scored deliverable. It replaces the prompt-based merge that the source system performed inside `/reconcile-code-review` with mechanical, testable code. The pipeline is **discover → normalize → cluster → dedupe → merge → confidence → emit**, ending in four reconciled artifacts (`findings.txt`, `findings.json`, `report.md`, `summary.json`) plus an `ambiguous.json` sidecar for clusters that fall in the similarity gray zone.

The findings format is the **public contract** between atcr and downstream consumers (including the source system's own `/reconcile-code-review` skill, which reads reconciled `findings.txt` as input). It is pipe-delimited, versioned (`# atcr-findings/v1`), and parseable by any consumer without atcr-specific code.

> Source: [plan.md:Reconciler Go pipeline], [original-requirements.md:Reconciler (net-new Go work — the core of v1)]

## Key Concepts

### Findings Stream Format (the public contract)

Per-source files carry a `# atcr-findings/v1` header and 8 columns:

```
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER
```

Reconciled files add a 9th column for reviewer tracking and a 10th for confidence:

```
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWERS|CONFIDENCE
```

Rules:
- `SEVERITY ∈ {CRITICAL, HIGH, MEDIUM, LOW}`
- Extraction by strict severity-prefix regex (`^(CRITICAL|HIGH|MEDIUM|LOW)\|`) — prose mentions of severities in code comments are ignored
- Literal `|` within fields replaced with `/` to keep column count stable
- Short rows padded to full column count before merge (tolerate model under-specification)

> Source: [plan.md:Findings Format], [original-requirements.md:Findings stream format]

### Reconciliation Pipeline

1. **Discover** sources under `sources/` of the review directory. Any child containing `findings.txt` is a source. `--sources` flag can allowlist if needed. `reconciled/` is never an input source (output only).
2. **Normalize** rows to internal records; tolerate short rows via padding; skip comments (`#`) and blank lines.
3. **Cluster** by `(FILE, LINE ± 3)` — two findings within 3 lines of each other on the same file land in the same cluster.
4. **Dedupe within clusters** by normalized PROBLEM text similarity (deterministic: token-set Jaccard with a conservative threshold).
5. **Merge duplicates** with the rules below.
6. **Confidence** — `HIGH` = 2+ distinct reviewers, `MEDIUM` = single reviewer, `LOW` reserved for untrusted sources.
7. **Ambiguity sidecar** — same-location clusters whose PROBLEM texts fall in the similarity gray zone are written to `ambiguous.json` (conservative unmerged default until the Skill adjudicates).
8. **Emit** all four reconciled artifacts.

> Source: [plan.md:Reconciler Go pipeline], [original-requirements.md:Reconciler (net-new Go work)]

### Merge Rules (when PROBLEMs cluster but aren't identical)

| Field | Merge rule |
|-------|------------|
| `REVIEWERS` | Comma-joined, deduplicated, sorted |
| `SEVERITY` | Max severity wins, with `disagreement: <lo> vs <hi>` annotation preserved if lower severities are also present |
| `PROBLEM` | Longest text wins (most detailed description) |
| `FIX` | Longest text wins |
| `CATEGORY` | Modal value across reviewers (ties broken alphabetically) |
| `EST_MINUTES` | Max value across reviewers (most pessimistic estimate) |
| `EVIDENCE` | Concatenated, newline-separated, reviewer-prefixed |
| `CONFIDENCE` | HIGH (≥2 distinct reviewers) / MEDIUM (single) / LOW (untrusted source) |

> Source: [plan.md:Reconciler Go pipeline — merge (REVIEWERS joined, SEVERITY max, PROBLEM/FIX longest, CATEGORY modal)]

### Ambiguity Sidecar

The reconciler is deliberately conservative. When two findings in the same cluster have PROBLEM text similarity in the gray zone (e.g., 0.4–0.7 Jaccard), the cluster is **not silently merged** and **not silently split**. It is written to `ambiguous.json` so the Skill (or a human) can adjudicate. Without adjudication, ambiguous clusters default to **unmerged** in the final output — i.e., each surface as separate findings, even though they were co-located. This is intentional: false positives in CI gates are worse than false negatives.

> Source: [plan.md:Reconciler — Ambiguity sidecar], [original-requirements.md:Reconciler — Ambiguity sidecar]

### `--fail-on` Exit-Code Gate

`atcr reconcile --fail-on <SEVERITY>` exits nonzero if any finding at or above the threshold survives reconciliation. This makes atcr a CI PR gate with no glue code:

```bash
# In a CI job:
atcr reconcile --fail-on HIGH
# Exit 0: no HIGH/CRITICAL findings survive reconciliation
# Exit 1: at least one HIGH or CRITICAL finding survived
```

> Source: [plan.md:Exit Codes], [original-requirements.md:Exit-code semantics (CI integration)]

## Code Examples

### Findings Row Type

```go
// Reconciler internal record. The 8-col format maps to fields 1-8;
// the 9-col format adds REVIEWERS and CONFIDENCE as fields 9-10.
type Finding struct {
    Severity   string  // CRITICAL | HIGH | MEDIUM | LOW
    File       string  // path relative to repo root
    Line       int     // line number; <0 means "no specific line"
    Problem    string
    Fix        string
    Category   string  // correctness, security, performance, style, test, doc, etc.
    EstMinutes int
    Evidence   string
    Reviewer   string  // per-source only; merged into Reviewers on reconcile
    Reviewers  []string // reconciled only
    Confidence string  // HIGH | MEDIUM | LOW
}
```

> Source: [plan.md:Findings Format], [original-requirements.md:Findings stream format]

### Cluster Key

```go
// Two findings cluster together if they share a file and their
// line numbers are within 3 of each other. Line <0 ("file-level")
// findings cluster with any other finding on the same file.
func clusterKey(f Finding) (string, int) {
    if f.Line < 0 {
        return f.File, 0
    }
    return f.File, f.Line / 3  // group into LINE±3 buckets
}
```

> Source: [plan.md:Reconciler — cluster (FILE, LINE±3)]

### Similarity Threshold (Jaccard on Tokens)

```go
// Conservative threshold: Jaccard ≥ 0.7 → merge, < 0.4 → keep separate,
// in between → write to ambiguous.json for adjudication.
func jaccard(a, b string) float64 {
    ta, tb := tokens(a), tokens(b)
    if len(ta) == 0 || len(tb) == 0 { return 0 }
    inter := 0
    for tok := range ta {
        if _, ok := tb[tok]; ok { inter++ }
    }
    union := len(ta) + len(tb) - inter
    return float64(inter) / float64(union)
}
```

> Source: [plan.md:Risk Mitigation — Conservative threshold + ambiguous.json sidecar]

### Stream Parser (extraction regex)

```go
var severityRe = regexp.MustCompile(`^(CRITICAL|HIGH|MEDIUM|LOW)\|`)

func parseRow(line string) (Finding, bool) {
    line = strings.TrimSpace(line)
    if line == "" || strings.HasPrefix(line, "#") { return Finding{}, false }
    if !severityRe.MatchString(line) { return Finding{}, false }
    cols := strings.Split(line, "|")
    if len(cols) < 8 {
        // Tolerate short rows by padding
        for len(cols) < 8 { cols = append(cols, "") }
    }
    file, lineStr, _ := strings.Cut(cols[1], ":")
    lineNum, _ := strconv.Atoi(lineStr)
    return Finding{
        Severity: cols[0], File: file, Line: lineNum,
        Problem: cols[2], Fix: cols[3], Category: cols[4],
        EstMinutes: atoiOr(cols[5], 0),
        Evidence: cols[6], Reviewer: cols[7],
    }, true
}
```

> Source: [plan.md:Findings Format — Extraction by strict severity-prefix regex]

## Quick Reference

| Pipeline Stage | Algorithm | Output |
|----------------|-----------|--------|
| Discover | `os.ReadDir(sources/*)` filtering for `findings.txt` | List of source paths |
| Normalize | `parseRow` (with padding) | `[]Finding` per source |
| Cluster | `(file, line/3)` bucket key | `map[clusterKey][]Finding` |
| Dedupe | Jaccard token-set similarity ≥ 0.7 | Merged records per cluster |
| Merge | Per-field rules (table above) | One finding per cluster |
| Confidence | Reviewer count → HIGH/MEDIUM/LOW | Annotated finding |
| Ambiguity | Similarity in [0.4, 0.7) | `ambiguous.json` sidecar |
| Emit | Format writers | `findings.txt`, `findings.json`, `report.md`, `summary.json` |

| Output file | Shape | Use |
|-------------|-------|-----|
| `findings.txt` | 10-col pipe-delimited, `# atcr-findings/v1` header | Machine contract for downstream tools |
| `findings.json` | Array of structured records | Scripting, custom tooling |
| `report.md` | Human report with executive summary + severity-grouped findings | Read by humans |
| `summary.json` | Run stats (sources, counts, clusters collapsed, partial flag) | CI dashboards, telemetry |
| `ambiguous.json` | Clusters needing semantic adjudication | Input for the Skill's adjudication path |

## Anti-Patterns to Avoid

- **Using an LLM to merge findings** — the entire reason atcr exists is to replace prompt-based merge with deterministic Go. Do not call out to an LLM during reconcile.
- **Silently merging similar-but-not-identical findings** — false positives corrupt CI gates. Use `ambiguous.json`.
- **Lowering the similarity threshold to "merge more"** — defeats the purpose. Threshold tunes with fixture corpora from real multi-agent runs.
- **Mutating per-source `findings.txt` files** — they are read-only inputs to the reconciler. Output goes to `reconciled/`.
- **Treating `reconciled/` as a source** — it is output only. The source-discovery rule explicitly excludes it.

> Source: [plan.md:Risk Mitigation], [plan.md:Source discovery rule]

## Related Documentation

- [Plan Document](../plan.md) — Reconciler Go pipeline and `--fail-on` exit codes
- [Original Requirements](../original-requirements.md) — Reconciler section under "Reconciler (net-new Go work)"
- [Codebase Discovery](../codebase-discovery.json) — Reconciler subsystem mapped in `test_patterns.test_subsystems`
- [Testing Patterns](testing-patterns.md) — Reconciler test fixture strategy and table-driven tests
- [CLI Architecture](cli-architecture.md) — `atcr reconcile` and `--fail-on` flag handling
