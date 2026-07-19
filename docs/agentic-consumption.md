# Agentic Consumption

`atcr` is built for humans *and* for other autonomous agents. Its default output
— colored, Markdown-formatted, table-padded — is ergonomic for a person reading a
terminal but hostile to an LLM agent: it burns context-window tokens and is
brittle to parse. The **`--axi` (Agent eXperience Interface)** mode strips that
formatting and emits a token-dense, escape-free [TOON](findings-format.md) payload
on stdout, so a sweeper agent can invoke `atcr` as a subprocess, parse the result
cheaply, and branch on a deterministic exit code.

This page covers how to drive `atcr` from another agent or orchestrator:
invocation, the exit-code contract, pagination/truncation, and the
stdout/stderr isolation guarantee.

## Invocation

AXI mode is exposed on the two agent-facing surfaces:

| Command | How to enable AXI | stdout payload |
|---------|-------------------|----------------|
| `atcr review` | `--axi` flag | a one-row `review_summary` TOON array (run metadata) |
| `atcr review --resume <id>` | `--axi` flag | identical `review_summary` shape to `review --axi` |
| `atcr report` | `--format axi` | a `findings[N]` TOON array of the reconciled findings |

Note the two spellings are intentional and different: `review`/`resume` take the
boolean `--axi` flag, while `report` selects AXI through its existing
`--format` enum (`--format axi`). `atcr report --format axi` is the payload an
agent reads to consume the findings themselves — `review --axi` deliberately
emits only a compact run summary, not the findings.

```bash
# Kick off a review and capture the run summary (metadata) as a clean payload.
atcr review --axi > run.toon 2> review.log

# Read the reconciled findings themselves as a token-dense payload.
atcr report --format axi > findings.toon 2> report.log
```

### The `review --axi` run-summary payload

`atcr review --axi` (and `atcr review --resume <id> --axi`) gate every
human-oriented stdout line and emit a single-row TOON array describing the run:

```
review_summary[1|]{id|dir|agents_succeeded|agents_total|agents_failed|agents_timed_out|api_calls|findings_total}:
  20260718T142233Z-9f3a|.atcr/reviews/20260718T142233Z-9f3a|3|3|0|0|9|2
```

The header names the pipe (`|`) delimiter and the fixed column order; the single
data row carries the run id, the review directory, and six bare-integer counts —
the last, `findings_total`, being the raw pre-reconcile fan-out count. To act on the reconciled
findings, read `atcr report --format axi` against the same review directory —
`findings_total` is a fan-out metric, not the deduplicated reconciled count. One
exception: a `review --resume <id> --axi` run that finds nothing pending (the
review is already fully complete) runs no fan-out, so its payload reports the
just-reconciled total instead of a fan-out count.

### The `report --format axi` findings payload

`atcr report --format axi` emits the reconciled findings as a TOON tabular array
whose header declares the pipe delimiter and the nine-column
[`atcr-findings/v1`](findings-format.md) field set, followed by a `truncated`
metadata line (see [Pagination and truncation](#pagination-and-truncation)):

```
findings[2|]{severity|"file:line"|problem|fix|category|est_minutes|evidence|reviewers|confidence}:
  CRITICAL|"auth.go:42"|token never expires|check expiry|security|15|expiresAt unread|greta,host|HIGH
  LOW|"util.go:7"|unused var|""|style|0|""|otto|MEDIUM
truncated: false
```

Free-text fields are quoted only when TOON requires it (empty string, embedded
delimiter/special character, number- or reserved-token-looking value); integer
columns are emitted as bare TOON integers. The stdout bytes are guaranteed free
of ANSI/OSC escape sequences and Markdown syntax — it is TOON only.

## Exit codes

AXI mode changes only the *shape* of stdout, never the exit code. `atcr review
--axi`, `atcr review --resume <id> --axi`, and `atcr report --format axi` return
the same **0/1/2/3** codes as their non-`--axi` counterparts for the same inputs.
The authoritative table is in [CI Integration → Exit semantics](ci-integration.md#exit-semantics);
it is not duplicated here. In short:

- **0** — run completed, no findings at/above the `--fail-on` threshold.
- **1** — gate failure: findings at/above threshold survived reconciliation.
- **2** — usage or configuration error (bad flags, empty range, invalid config).
- **3** — authentication failure (`--sync-cloud` with a missing/invalid key).

New AXI-introduced error paths classify into that existing contract rather than
adding a code:

- An unsupported `--axi` flag combination — `atcr review --axi --auto-fix`
  (`--auto-fix` drives an interactive write-back flow, not a consumable payload)
  — is a **usage error (2)**.
- An internal AXI rendering fault is a **generic failure (1)**.
- A malformed `ATCR_AXI_MAX_LINES` is **not an error at all** — it fails open to
  the default cap (see below).

The epic's original proposal to repurpose exit `2` as "internal/syntax error"
was considered and **rejected**: `2` is already reserved for usage/configuration
errors that CI scripts depend on. AXI introduces no new exit code and repurposes
none.

## Pagination and truncation

A large PR can produce a findings list big enough to blow an agent's context
window. `atcr report --format axi` therefore caps its payload deterministically:

- **Default cap: 500 physical lines** — the array header plus up to 499 finding
  rows (the cap counts total physical lines, header inclusive).
- **Override:** set `ATCR_AXI_MAX_LINES` to a positive integer to raise or lower
  the cap. A blank, non-numeric, zero, or negative value is ignored — the cap
  fails open to 500 and a single warning is written to **stderr** (never stdout,
  never a nonzero exit).
- **Signal:** every AXI findings payload ends with a `truncated: <bool>` line.
  When findings exceed the cap, the array is cut on a whole-row boundary, extra
  rows are dropped, and `truncated: true` is emitted.

The array header's declared count `N` (`findings[N|]{...}`) is always the **true,
pre-truncation total**, even when fewer than `N` rows are physically present. A
consumer must therefore read `truncated` and the header `N` as authoritative
rather than counting the rows it received. When `truncated` is `true`, treat the
payload as partial: re-invoke with a higher `ATCR_AXI_MAX_LINES` to retrieve the
full set, or record the result as incomplete — do not assume you have every
finding.

```bash
# Raise the cap for a large review before consuming the full findings set.
ATCR_AXI_MAX_LINES=5000 atcr report --format axi > findings.toon
```

## stdout is payload, stderr is diagnostics

The entire value of AXI mode for an orchestrator is that a redirect produces a
byte-clean file. Under `--axi`:

- **stdout carries only the payload** — the `review_summary`/`findings` TOON and
  its `truncated` line, with zero ANSI/OSC escapes and zero Markdown. `atcr
  review --axi > run.toon` and `atcr report --format axi > findings.toon` yield
  parseable files with nothing else mixed in.
- **stderr carries only diagnostics** — progress, warnings, and all structured
  logs, governed by [`LOG_LEVEL` and `--log-format`](logging.md) and passed
  through the same redaction sink. Structured errors also go to stderr, not
  stdout (axi.md Principle 6 reconciliation), so an agent branches on the exit
  code and never has to parse stdout for an error case.

Capture the two streams separately so diagnostics never contaminate the payload:

```bash
atcr report --format axi > findings.toon 2> report.log
```

## Worked example: an autonomous sweeper

The scenario AXI mode is built for: an autonomous agent (a "sweeper") reviews a
change set it just produced, consumes the findings programmatically, and reacts.
The script below is near-runnable — every flag, env var, field, and exit code in
it is a real part of atcr's contract. Adapt the "fix" step to your agent.

```bash
#!/usr/bin/env bash
# Autonomous sweeper: review the current change set, consume the findings as a
# token-dense AXI payload, and branch on the exit code. No credentials appear in
# this script — atcr reads its provider key from the environment (e.g. OPENROUTER_API_KEY).
set -u

# Bound the findings payload so a large PR cannot blow the agent's context window.
export ATCR_AXI_MAX_LINES=2000

# 1. Run the review as a subprocess. stdout = payload, stderr = diagnostics.
#    --fail-on gates the exit code on surviving findings; --axi keeps stdout clean.
atcr review --axi --fail-on high > run.toon 2> review.log
status=$?

# 2. Branch on the reconciled exit code — the same 0/1/2/3 contract as non-axi.
case $status in
  0)
    echo "clean: no findings at or above the threshold; proceeding"
    ;;
  1)
    # Findings survived the gate. Read them as an AXI payload (stdout only).
    # report returns 2 for a usage/config fault (bad anchor, absent findings.json)
    # and 1 for an internal render fault — propagate whichever actually occurred.
    if ! atcr report --format axi > findings.toon 2> report.log; then
      rc=$?
      echo "report failed (exit $rc); see report.log" >&2
      exit "$rc"
    fi

    # 3. Trust the payload only after checking the truncation flag. If truncated,
    #    the array header's N is the TRUE total but not every row is present —
    #    re-run with a higher cap rather than assuming the list is complete.
    if grep -qx 'truncated: true' findings.toon; then
      echo "findings truncated; re-running with a higher cap" >&2
      ATCR_AXI_MAX_LINES=20000 atcr report --format axi > findings.toon 2> report.log
    fi

    # 4. Iterate the finding rows. Data rows are pipe-delimited and indented; the
    #    header (findings[N|]{...}:) and the trailing truncated: line are skipped.
    #    Free-text cells follow TOON quoting rules — a production consumer should
    #    parse with a TOON library rather than a bare split.
    grep -E '^  [A-Z]+\|' findings.toon | while IFS='|' read -r severity file_line problem fix _rest; do
      printf 'FIX %s at %s: %s\n' "${severity#  }" "$file_line" "$fix"
      # ... hand each finding to the agent's fixer here ...
    done
    ;;
  2)
    echo "usage/config error: fix the invocation, do not retry as-is (see review.log)" >&2
    exit 2
    ;;
  3)
    # Exit 3 is the --sync-cloud auth failure specifically (missing/invalid
    # ATCR_API_KEY or a remote 401/403). It is unreachable for the command above,
    # which does not push to the cloud — a missing provider key surfaces as exit 2.
    # Keep this branch as defensive handling for orchestrators that add --sync-cloud.
    echo "auth error: fix credentials (e.g. ATCR_API_KEY) before retrying" >&2
    exit 3
    ;;
esac
```

Two invariants the example relies on, both guaranteed by AXI mode: `review.log`
and `report.log` capture *all* diagnostics because stderr is the only diagnostic
sink, so `run.toon`/`findings.toon` are byte-clean payloads; and the exit code is
authoritative, so the sweeper never has to parse stdout to decide whether the run
failed.

## See also

- [CI Integration](ci-integration.md) — the exit-semantics contract AXI reuses,
  and the one-shot `--fail-on` gate pattern.
- [Findings Format — `atcr-findings/v1`](findings-format.md) — the field set the
  `findings` payload maps onto.
- [Logging](logging.md) — the stderr diagnostics sink, log levels, and redaction.
