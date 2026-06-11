# Findings Stream Format v1 (the public contract) [CRITICAL]

## Overview

The `atcr-findings/v1` format is the **public contract** between atcr and any downstream consumer. The format is versioned from day one with an additive-only evolution policy: any new field is appended, never inserted, and any new severity or status is added to the closed enumeration. Consumers can rely on a `# atcr-findings/v1` header to confirm they are reading v1 data; producers must write the header in every file (per-source and reconciled).

The format is intentionally minimal: pipe-delimited rows, one finding per line, a strict severity prefix so extraction is regex-driven (not parser-fragile), and column padding so a model that emits a short row doesn't poison the merge. The reconciler and the source system's `/reconcile-code-review` skill both consume this format, so changes are coordinated.

> Source: [plan.md:Findings Format], [original-requirements.md:Findings stream format (the public contract)]

## Key Concepts

### Per-Source Format (8 columns)

Each reviewer agent writes its findings to `sources/<agent>/findings.txt`:

```
# atcr-findings/v1
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER
CRITICAL|src/auth/session.go:142|Session fixation: token is not rotated on privilege change|Add `session.Rotate()` after role escalation|security|45|JWT contains pre-escalation `sub` claim; `Rotate()` would mint a new jti|greta
HIGH|cmd/server/main.go:88|Goroutine leak in `bgWorker` on shutdown|Use `sync.WaitGroup` and ctx cancellation|concurrency|30|wg.Add but no wg.Wait in deferred shutdown path|kai
```

The 8 columns are:

| # | Field | Description |
|---|-------|-------------|
| 1 | `SEVERITY` | One of: `CRITICAL`, `HIGH`, `MEDIUM`, `LOW` |
| 2 | `FILE:LINE` | `path/to/file.ext:NNN`; line may be `-1` or `0` for file-level findings |
| 3 | `PROBLEM` | One-sentence description of the issue |
| 4 | `FIX` | Suggested remediation |
| 5 | `CATEGORY` | Free-form but conventionally: `correctness`, `security`, `performance`, `style`, `concurrency`, `test`, `doc`, `api`, `arch` |
| 6 | `EST_MINUTES` | Integer estimate of time to fix |
| 7 | `EVIDENCE` | Supporting excerpt, repro, or rationale (free-form) |
| 8 | `REVIEWER` | The agent name (`bruce`, `greta`, `kai`, `mira`, `dax`, `otto`, or `host`) |

> Source: [plan.md:Findings Format], [original-requirements.md:Findings stream format]

### Reconciled Format (9 columns)

The reconciler emits `reconciled/findings.txt` with **9 columns**: the original 8, with the 8th slot renamed from `REVIEWER` (singular) to `REVIEWERS` (plural, comma-joined, deduplicated), plus `CONFIDENCE` (`HIGH`, `MEDIUM`, or `LOW`) as the 9th column:

```
# atcr-findings/v1
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWERS|CONFIDENCE
CRITICAL|src/auth/session.go:142|Session fixation: token is not rotated on privilege change|Add `session.Rotate()` after role escalation|security|45|"JWT contains pre-escalation `sub` claim; `Rotate()` would mint a new jti" — greta
```

> Source: [plan.md:Reconciler Go pipeline — emit], [original-requirements.md:Reconciled (9 columns)]

### Strict Severity-Prefix Extraction

The parser uses a regex anchored at the start of each line: `^(CRITICAL|HIGH|MEDIUM|LOW)\|`. Lines that do not match — comments, blank lines, model prose that mentions "HIGH" mid-sentence — are **silently skipped**. This is the format's most important contract: prose mentions never become findings.

```go
var severityRe = regexp.MustCompile(`^(CRITICAL|HIGH|MEDIUM|LOW)\|`)
```

> Source: [plan.md:Findings Format — Extraction by strict severity-prefix regex]

### Escaping and Padding

Two rules keep the format machine-parseable even when a model misbehaves:

1. **Literal `|` within fields replaced with `/`.** Example: a PROBLEM that says "use `a || b`" becomes "use `a // b`" in the wire format. The parser does not unescape — the loss is intentional, because column count stability matters more than preserving OR.
2. **Short rows padded to 8 (per-source) or 9 (reconciled) columns.** A model that emits only 6 columns (e.g., empty `EVIDENCE`) gets the missing fields filled with empty strings on the right. This keeps the column index stable across rows.

> Source: [plan.md:Findings Format — literal `|` within fields replaced with `/`; short rows padded]

### Version Header and Evolution Policy

Every file starts with `# atcr-findings/v1`. The reconciler rejects files without a recognized header (unknown versions are a hard error to avoid silent data corruption). The version is bumped only for breaking changes:

| Bump | When |
|------|------|
| None (additive) | New optional columns appended to the right; new CATEGORY values; new EST_MINUTES encodings |
| Minor (v1.x) | New SEVERITY value (e.g., adding `INFO`) — coordinate with all consumers |
| Major (v2) | Any breaking change: column reorder, mandatory fields, escaping rule change |

> Source: [plan.md:Risk Mitigation — Version header (atcr-findings/v1) from day one; additive-only evolution policy]

## Code Examples

### Reading a Per-Source File

```go
func ReadSource(path string) ([]Finding, error) {
    data, err := os.ReadFile(path)
    if err != nil { return nil, err }

    header, rest, _ := strings.Cut(string(data), "\n")
    if !strings.HasPrefix(strings.TrimSpace(header), "# atcr-findings/v1") {
        return nil, fmt.Errorf("missing or wrong version header in %s", path)
    }

    var out []Finding
    for _, line := range strings.Split(rest, "\n") {
        if line == "" || strings.HasPrefix(line, "#") { continue }
        if !severityRe.MatchString(line) { continue }  // skip prose
        cols := strings.Split(line, "|")
        for len(cols) < 8 { cols = append(cols, "") } // pad
        // ... map cols to Finding struct
    }
    return out, nil
}
```

> Source: [plan.md:Findings Format]

### Writing a Reconciled File

```go
func WriteReconciled(path string, findings []Finding) error {
    f, err := os.Create(path)
    if err != nil { return err }
    defer f.Close()

    fmt.Fprintln(f, "# atcr-findings/v1")
    for _, fi := range findings {
        cols := []string{
            fi.Severity,
            fmt.Sprintf("%s:%d", fi.File, fi.Line),
            fi.Problem, fi.Fix, fi.Category,
            strconv.Itoa(fi.EstMinutes),
            fi.Evidence,
            strings.Join(fi.Reviewers, ","),  // 9th: REVIEWERS
            fi.Confidence,                     // 10th: CONFIDENCE
        }
        for i, c := range cols {
            cols[i] = strings.ReplaceAll(c, "|", "/")  // escape
        }
        fmt.Fprintln(f, strings.Join(cols, "|"))
    }
    return nil
}
```

> Source: [plan.md:Findings Format — escaping and padding]

## Quick Reference

| Element | Spec |
|---------|------|
| Version header | `# atcr-findings/v1` (required, first non-blank line) |
| Per-source columns | 8 (SEVERITY, FILE:LINE, PROBLEM, FIX, CATEGORY, EST_MINUTES, EVIDENCE, REVIEWER) |
| Reconciled columns | 9 (per-source 8 with REVIEWER→REVIEWERS rename + CONFIDENCE) |
| Severities | `CRITICAL`, `HIGH`, `MEDIUM`, `LOW` (closed set) |
| Extraction regex | `^(CRITICAL\|HIGH\|MEDIUM\|LOW)\|` |
| Pipe escape | `\|` → `/` (lossy but stable) |
| Short-row handling | Pad to column count with empty strings |
| Comments | Lines starting with `#` (other than the version header) are ignored |
| Confidence | `HIGH` (≥2 distinct reviewers), `MEDIUM` (single), `LOW` (untrusted source) |

## Anti-Patterns to Avoid

- **Quoting fields with `"..."`** — the format is pipe-delimited, not RFC 4180 CSV. Quotes are literal characters.
- **Wrapping long EVIDENCE in a continuation** — the format is one line per finding. Break long evidence into a `findings.json` representation instead, where structured fields are available.
- **Writing a new severity like `BLOCKER`** — extend the closed set only through a coordinated minor version bump.
- **Skipping the version header "because the consumer is in the same repo"** — every file has the header. Bypass the header and you bypass versioned parsing.
- **Mutating per-source `findings.txt` after a run completes** — those files are an audit trail. They are inputs to the reconciler, never editable outputs.

> Source: [plan.md:Risk Mitigation], [original-requirements.md:Findings stream format]

## Related Documentation

- [Plan Document](../plan.md) — Findings Format in Technical Planning Notes
- [Original Requirements](../original-requirements.md) — Findings stream format section under "Findings stream format"
- [Reconciler & Findings Stream](reconciler.md) — How the format is consumed by the reconciler pipeline
- [Codebase Discovery](../codebase-discovery.json) — `internal/stream/parser.go` mapped in `files_to_create`
