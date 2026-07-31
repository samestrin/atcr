# Adversarial Verification

Adversarial verification is the stage that makes the CI gate trustworthy enough to block merges. After `atcr reconcile` produces unique, deduped findings, `atcr verify` runs **skeptic** agents — a different model from any reviewer that produced the finding — that attempt to *disprove* each finding using the same tool loop the reviewers used (real, path-jailed code access). Their verdicts feed a second confidence axis, and the gate counts only findings that survive.

False positives are the adoption killer for LLM code review: a panel that is mostly noise gets ignored within a week. Reviewer agreement helps but is correlated, not independent — several models can share a blind spot and produce the same plausible-but-wrong finding. A model explicitly prompted to refute, with code access to check the cited evidence, attacks that directly.

## Overview

- **When it runs:** after reconcile, never before. Verification cost is paid **once per unique finding**, not once per duplicate, and reconcile stays purely deterministic (no LLM calls inside it).
- **What it reads:** `reconciled/findings.json` (the deduped findings).
- **What it writes:** `reconciled/verification.json` (the audit record) and re-emitted `reconciled/findings.json` / `summary.json` with v2 confidence; it appends `"verify"` to the manifest stages.
- **Re-runnable and idempotent:** verifying the same reconciled input twice yields the same artifacts. Already-verified findings are skipped unless you pass `--fresh`.
- **Never drops a finding:** a skeptic failure (timeout, provider error, tripped budget, malformed output) yields an `unverifiable` verdict — never a dropped finding and never a failed run by itself.

Run it standalone, chained off a review, or as an MCP tool:

```bash
atcr verify [id-or-path]              # verify an already-reconciled review
atcr review --verify                  # review → reconcile → verify in one run
```

`id-or-path` follows the same resolution as `atcr reconcile`: a bare review id resolves under `.atcr/reviews/<id>/`, a path is used as-is, and omitting it uses `.atcr/latest`.

## Skeptic Selection

A skeptic is an agent with `role: skeptic` in the registry (see [registry.md](registry.md#skeptic-agents-role-skeptic-active-in-30)). Selection enforces one rule that is **never left to configuration discipline**:

- **Role filtering:** only agents with `role: skeptic` are eligible. An agent with no `role` defaults to `reviewer` and is never selected as a skeptic.
- **Different-model rule:** a skeptic is excluded if its model exactly matches the model of *any* reviewer credited on the finding. A model cannot verify its own work, even indirectly through a shared blind spot. The engine enforces this — it is not a soft hint.
- **`no_eligible_skeptic` fallback:** if no skeptic survives the different-model exclusion for a finding (for example, every skeptic shares a model with a reviewer), the finding is recorded `unverifiable` with the note `no_eligible_skeptic`. It keeps its v1 confidence and is never dropped.

Candidates are ordered deterministically by agent name, so the same roster always selects the same skeptic(s).

## Verdict Envelope

A skeptic returns a strict, parseable envelope:

```json
{"verdict": "confirmed|refuted|unverifiable", "reasoning": "why"}
```

- `confirmed` — the skeptic checked the evidence and the finding holds.
- `refuted` — the skeptic found concrete evidence the finding is wrong (a false positive).
- `unverifiable` — the skeptic could not establish either way (ambiguous evidence, evidence outside the snapshot jail, a tripped budget, a provider error).

Parsing is defensive. The parser unmarshals the JSON; if that fails it scans for a `{...}` object (so a verdict wrapped in markdown fences or surrounded by prose is still recovered); a verdict outside the enum, an empty response, or output that cannot be parsed at all all fall back to `unverifiable` with the raw text preserved in the notes. Malformed skeptic output therefore degrades safely — it never forges a `confirmed` or `refuted`.

## Confidence v2

Verification adds a second confidence axis above the v1 reviewer-agreement tiers. The ordering, highest to lowest:

| Tier | Meaning |
|------|---------|
| `VERIFIED` | A skeptic confirmed the finding. |
| `HIGH` | 2+ independent reviewers agreed; not yet verified (or unverifiable). |
| `MEDIUM` | A single reviewer; not refuted. |
| `LOW` | Refuted — demoted, retained for audit with the skeptic's reasoning. |

Transition rules (a pure mapping from the v1 confidence and the verdict):

- `confirmed` → `VERIFIED` (regardless of the v1 tier).
- `refuted` → `LOW` (demoted — but **never deleted**; a wrong refutation must stay visible to the human).
- `unverifiable`, an empty verdict (no skeptic ran), or an unrecognized token → the v1 confidence passes through unchanged.

Comparison to v1: v1 confidence is a reviewer-agreement signal only (`HIGH`/`MEDIUM`/`LOW`). v2 keeps those tiers and adds `VERIFIED` at the top plus the demote-on-refute rule. Findings from a pre-Epic-3.0 run (no verification block) keep their v1 confidence and render identically.

Refuted findings stay in `findings.json` and in the report under a collapsed **Refuted Findings** section at the bottom; the report adds a `VERIFIED` column to the summary grid and a per-finding Skeptic section for verified findings. See [findings-format.md](findings-format.md#json-form) for the on-disk block.

## Gate Semantics

`atcr verify` itself has no `--fail-on` or `--require-verified` flag — it only
computes and persists verdicts. The CI gate that reads those verdicts lives on
`atcr reconcile` and `atcr review`, and reads verdicts directly: refuted
findings never block a merge. (The `atcr verify diff` subcommand has its own,
unrelated `--fail-on` — it gates on diff-smell verdicts, not on findings. See
[Diff-Smell Scanning](#diff-smell-scanning) below.)

- **`--fail-on <severity>`** (on `atcr reconcile` / `atcr review`) — exit `1` if any finding at or above `<severity>` survives, where *survives* means its verdict is **not** `refuted`. Out-of-scope findings are excluded from the count (precedence over the verdict check). Resolved via the shared gate precedence: flag > project config > registry.
- **`--require-verified`** (on `atcr reconcile` / `atcr review`) — only meaningful with `--fail-on`. Counts only findings whose confidence is `VERIFIED` (i.e. `confirmed`) at or above the threshold — the strictest gate. Using it **without** `--fail-on` is a usage error (`error: --require-verified requires --fail-on`, exit 2). On `atcr reconcile`, if the verify stage never ran, `--require-verified` does **not** refuse — it logs a warning and the gate proceeds (a silently permissive gate is possible). On `atcr review`, `--require-verified` is refused outright unless combined with `--verify` or `--debate` (`error: --require-verified requires --fail-on and --verify or --debate`, exit 2).
- **`--repo <path>`** — reviewed repository root that skeptics inspect and the redactor relativizes absolute paths against. Defaults to the current working directory; use this to verify findings against a repo other than the CWD or when running `verify` from outside the repo root (Epic 22.1).

| Finding | v1 confidence | verdict | `--fail-on high` | `--fail-on high --require-verified` |
|---------|---------------|---------|------------------|-------------------------------------|
| F1 | HIGH | confirmed (VERIFIED) | counts | counts |
| F2 | HIGH | refuted (LOW) | does not count | does not count |
| F3 | HIGH | unverifiable | counts | does not count |
| F4 | MEDIUM | confirmed (VERIFIED) | does not count | does not count |

`atcr verify` exit codes: `0` success, `1` no reconciled findings found (run `atcr reconcile` first), `2` usage or configuration error. `atcr verify` has no gate of its own; gate pass/fail exit codes belong to `atcr reconcile` / `atcr review`.

## Cost Controls

Verification roughly doubles per-finding cost, so it is bounded several ways:

- **`verify.min_severity`** (registry, default `MEDIUM`) — findings below this floor skip verification entirely and keep their v1 confidence. Override per run with `--min-severity <LOW|MEDIUM|HIGH|CRITICAL>`.
- **`verify.votes`** (registry, default `1`) — skeptics consulted per finding. With one vote the single verdict passes through; with multiple, a clear majority wins and a tie becomes `unverifiable` (with all reasonings preserved).
- **`--thorough`** — forces 3 skeptics with majority rule for the run, regardless of `verify.votes`.
- **`--fresh`** — re-verify every finding, even those already carrying a verdict from a previous run. Without it, already-verified findings are skipped (idempotent re-runs are cheap).
- **Per-finding budgets** — each skeptic reuses the reviewer tool-loop budgets: `max_turns`, `tool_budget_bytes`, and `timeout_secs` from the skeptic's agent config. A tripped budget yields `unverifiable`, never a dropped finding.

Note: findings are verified concurrently through a bounded worker pool (`verify.max_parallel`, default `4`); the skeptics within a single finding run sequentially, so a `--thorough` run is `votes` provider calls back to back per finding, with up to `verify.max_parallel` findings in flight at once.

## Artifacts

| Artifact | What verification adds |
|----------|------------------------|
| `reconciled/verification.json` | The full audit record (see schema below). |
| `reconciled/findings.json` | Each verified finding gains a `verification` block `{verdict, skeptic, notes}`; confidence is recomputed to the v2 tier. |
| `reconciled/manifest.json` | `"verify"` appended to `stages` (idempotent — not duplicated on re-run). |
| `reconciled/summary.json` | Gains a `verdictCounts` object `{confirmed, refuted, unverifiable}`. |

`verification.json` schema:

```json
{
  "verifiedAt": "2026-06-14T13:51:53Z",
  "minSeverity": "MEDIUM",
  "fresh": false,
  "thorough": false,
  "findings": [
    {
      "file": "internal/auth/token.go",
      "line": 42,
      "problem": "JWT signature not verified before claims are read",
      "verdict": "confirmed",
      "skeptic": "otto",
      "model": "anthropic/claude-sonnet-4-6",
      "reasoning": "read token.go:42 — jwt.Parse is called without Verify",
      "durationMs": 1840,
      "trippedBudgets": []
    }
  ],
  "verdictCounts": {"confirmed": 1, "refuted": 0, "unverifiable": 0}
}
```

The per-finding `model` (the different-model evidence) lives here, in `verification.json`, not in the `findings.json` block — the report's Skeptic section shows verdict/skeptic/reasoning and does not perform a registry lookup. Skeptic runs do not persist transcripts — only reviewer fan-out and debate do.

## Diff-Smell Scanning

Skeptic verification asks "is this finding real?". Diff-smell asks a different,
purely mechanical question: **did this patch cheat?** It scans a unified diff for
the fingerprints of an over-simplified ("reward-hacked") change — one that makes a
test pass by deleting the test rather than fixing the code.

It is deterministic and model-free: same diff in, same verdict out. No agent, no
provider, no API key, no network.

```bash
atcr verify diff --staged                    # scan the git index
atcr verify diff --range main..HEAD          # scan a commit range
atcr verify diff patch.diff                  # scan a file
git diff | atcr verify diff                  # scan stdin (the default)
```

### Smells

| Smell | Severity | What it catches |
|-------|----------|-----------------|
| `test_only` | hard | the change touched only test files — no implementation change |
| `test_deleted` | hard | a test file was deleted outright |
| `test_skipped` | hard | a skip/ignore marker was added (`t.Skip(`, `@pytest.mark.skip`, `it.skip(`, `xit(`, `@Ignore`, `@Disabled`, `#[ignore]`) |
| `test_renamed_away` | hard | a test file was renamed to a non-test path, disabling it |
| `weakened_assertion` | hard / soft | assertions removed without replacement (hard), or replaced one-for-one (soft — verify they were not weakened) |
| `suppression` | soft | a linter/type-checker suppression was added (`//nolint`, `@ts-ignore`, `# noqa`, …) |
| `empty_catch` | soft | an empty or swallowing exception handler was added |
| `stub_body` | soft | a stub / not-implemented / standalone TODO body was added |

A line that also appears verbatim among the same file's removed lines is treated
as relocated, not introduced, so moving or reindenting code raises nothing.

### Verdicts and exit codes

The verdict is `hard` if any hard smell fired, `soft_only` if only soft smells
fired, else `clean`.

Gating is **opt-in**, matching `atcr review --fail-on`: without the flag the
command reports the verdict and exits `0`.

- **`--fail-on hard`** — exit `1` on a `hard` verdict.
- **`--fail-on soft`** — exit `1` on `hard` or `soft_only`.
- **`--fail-on none`** — explicit no-op, so a script can always pass the flag and vary only its value.

Exit codes follow the standard table: `0` success, `1` the gate tripped, `2` a
usage error (bad `--fail-on` value, unreadable file, git failure, conflicting
input sources, or input that is not a unified diff). A tripped gate still prints
the full report, so one run tells you both that it failed and why.

### Machine-readable output

`--json` puts the scan on stdout and nothing else, so it pipes straight into a
parser. Field names mirror the upstream `llm-tools` `diff-smell` tool:

```json
{
  "files": {"test": ["foo_test.go"], "impl": ["foo.go"]},
  "smells": [
    {"type": "test_deleted", "severity": "hard", "file": "foo_test.go", "evidence": "fix deleted a test file outright"}
  ],
  "summary": {"test_files": 1, "impl_files": 1, "hard": 1, "soft": 0, "by_type": {"test_deleted": 1}, "verdict": "hard"}
}
```

Empty collections render as `[]` / `{}`, never `null`.

### Notes

- **`test_only` is not suppressed here.** A diff touching only test files raises the hard `test_only` smell. The in-process fix gate suppresses that case when the finding's own file is a test, but a standalone caller has no finding to key that exemption on — so a legitimately test-only commit reads as `hard`. Use `--fail-on soft`/`hard` accordingly, or scan a range that includes the implementation change.
- **Input must be a unified diff.** Non-diff content is a usage error (exit `2`), not a silent `clean` — reporting `clean` for input that was never scanned would hand a caller a false pass. Empty input (nothing staged) *is* clean.
- **One source at a time.** A file argument, `-`, `--staged`, and `--range` are mutually exclusive; naming none reads stdin.
- **Same analyzer as the auto-fix gate.** This is the surface over the analyzer that already gates executor-produced fixes, so a patch judged here is judged by identical logic.

## MCP tool

The `atcr_verify` MCP tool mirrors the CLI and routes through the same orchestrator, so the artifacts are identical. It accepts `id_or_path` (review id only — paths are not accepted), `fresh`, `thorough`, `minSeverity`, `failOn`, and `requireVerified` (camelCase, matching the tool's JSON argument keys), and returns `verdict_counts`, `findings_processed`, `duration_ms`, and a `gate_status` object (omitted when `fail_on` is not provided). Missing reconciled findings returns the same clear error as the CLI: `no reconciled findings found in <dir> — run 'atcr reconcile' first`.

## Related documents

- [registry.md](registry.md) — the `role: skeptic` agent configuration and the different-model rule.
- [findings-format.md](findings-format.md) — the on-disk `verification` block and v2 confidence tier.
- [ci-integration.md](ci-integration.md) — wiring the gate into CI.
