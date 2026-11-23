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

AXI mode is exposed on these agent-facing surfaces:

| Command | How to enable AXI | stdout payload |
|---------|-------------------|----------------|
| `atcr` (no subcommand) | `--axi` flag | a one-row `home` TOON array (executable path, one-line description, current review state) |
| `atcr review` | `--axi` flag | a one-row `review_summary` TOON array (run metadata); a `--all`/`--scope` re-run with nothing changed prints only a `help[]` block ([below](#the-review---axi-run-summary-payload)) |
| `atcr review --resume <id>` | `--axi` flag | identical `review_summary` shape to `review --axi` |
| `atcr report` | `--format axi` | a `findings[N]` TOON array of the reconciled findings |
| `atcr reconcile` | `--axi` flag | a TOON object of the run values (`findings`, `sources`, `reconciled_dir`) |
| `atcr verify` | `--axi` flag | a TOON object of the run values (`findings`, `confirmed`, `refuted`, `unverifiable`, `reconciled_dir`) |
| `atcr status` | `--axi` flag | a TOON object of the review status (the `ReviewStatus` fields of the JSON document) |
| `atcr debate` | `--axi` flag | a TOON object of the run values and next steps |
| `atcr debt clean` | `--axi` flag | a token-dense TOON `{items, summary}` payload |
| `atcr debt filter` | `--axi` flag | a token-dense TOON `{items, summary}` payload |
| `atcr debt group` | `--axi` flag | a token-dense TOON `{groups, ungrouped, summary}` payload |
| `atcr debt validate` | `--axi` flag | a token-dense TOON `{items, coherence}` payload |

Note the two spellings are intentional and different: `review`/`resume` take the
boolean `--axi` flag, while `report` selects AXI through its existing
`--format` enum (`--format axi`). `atcr report --format axi` is the payload an
agent reads to consume the findings themselves — `review --axi` deliberately
emits only a compact run summary, not the findings.

### Standard TOON and the legacy pipe fallback

Every AXI payload is canonical, specification-compliant TOON, encoded by [`go-axi` v0.4.0](https://github.com/samestrin/go-axi) (the version pinned in atcr's `go.mod`): comma-delimited tabular arrays, strict TOON quoting, and only the five valid escapes (`\\ \" \n \r \t`). Any off-the-shelf TOON decoder reads it with no custom delimiter option. Code in `problem`, `fix`, or `evidence` that carries `|`, `||`, quotes, or newlines survives verbatim, for fields of at most 500 runes (longer free-text fields are capped).

Before this, atcr emitted a custom pipe-delimited variant (`findings[N|]{a|b}:`). That encoding is deprecated but still available for consumers that have not migrated yet:

| Surface | Legacy pipe opt-in |
|---------|--------------------|
| `atcr report` | `--format pipe` |
| `atcr review --axi`, `atcr review --resume <id> --axi` | `--legacy-pipe` |
| `atcr --axi` | `--legacy-pipe` |
| every AXI surface above, including `report --format axi`, except `reconcile --axi`, `verify --axi`, `status --axi`, `debate --axi` and the `debt` AXI surfaces | `ATCR_LEGACY_PIPE=1` |

`atcr reconcile --axi`, `atcr verify --axi`, `atcr status --axi`, `atcr debate --axi`, `atcr debt clean --axi`, `atcr debt filter --axi`, `atcr debt group --axi` and `atcr debt validate --axi` have no legacy pipe encoding: they reject `--legacy-pipe` as a usage error (exit 2) and ignore `ATCR_LEGACY_PIPE`. `ATCR_LEGACY_PIPE=1` is a global switch over the AXI surface only; non-AXI formats ignore it. Every legacy route writes one warning to stderr (never stdout), and the exit code does not change. It is a log record, so it honors `--log-format` and `ATCR_LOG_LEVEL`, and it names the switch that turned the legacy path on (`--legacy-pipe`, `ATCR_LEGACY_PIPE` or `--format pipe`). In the default text format it reads:

```
time=... level=WARN msg="warning: pipe-delimited AXI output is deprecated and will be removed in a future release; migrate to standard TOON. (enabled by ATCR_LEGACY_PIPE)"
```

The legacy path keeps its old bytes exactly, including its old pagination contract (header `N` is the true total, no `total:` line). It will be removed in a future release.

```bash
# Kick off a review and capture the run summary (metadata) as a clean payload.
atcr review --axi > run.toon 2> review.log

# A plain review does not reconcile (only --fail-on, --verify, --debate or
# --auto-fix do), so reconcile the latest review before reading its findings.
atcr reconcile > /dev/null 2> reconcile.log

# Read the reconciled findings themselves as a token-dense payload.
atcr report --format axi > findings.toon 2> report.log
```

### `atcr diff-smell --json` — payload-only without AXI

One further agent-facing surface is not an AXI mode but obeys the same stdout
discipline: `atcr diff-smell --json` emits a JSON scan result as the **sole**
content of stdout, with every note and diagnostic on stderr.

It is worth an orchestrator's attention for a reason the AXI surfaces are not:
it is **deterministic and model-free** — no agent, no provider, no API key, no
network — so an agent can run it inside a credential-free sandbox, on every
commit, at zero token cost, and get a byte-identical answer for a byte-identical
diff.

```bash
# Gate a patch on reward-hack fingerprints; payload on stdout, notes on stderr.
atcr diff-smell --staged --json > smell.json 2> smell.log
```

Two inputs that might look like errors are deliberately not: an empty diff and
content that is not a unified diff both report `clean` and exit `0`, with the
explanation on stderr — so `smell.json` above is always a parseable document.
Gating is opt-in via `--fail-on`; see [diff-smell.md](diff-smell.md) for the JSON
shape and its stability contract.

### The bare `atcr --axi` home payload

A bare `atcr` (no subcommand) renders a live "home view" instead of static help
(axi.md Principle 8, "Content First"): an orchestrator can sanity-check state in
one cheap call before deciding what to do next. Under `--axi` that view is a
single-row TOON array — the executable path (home-relativized with `~`), atcr's
one-line description, and the current review's id and status:

```
home[1]{exec_path,description,review_id,review_status}:
  ~/go/bin/atcr,"Agent Team Code Review — a review panel, not a reviewer",2026-07-18_main,completed
help[2]: atcr review,atcr report <id>
```

The trailing `help[]` line lists the commands that usually come next (axi.md
Principle 9, "Contextual Disclosure"). `<id>` is a placeholder for the review id,
never a guessed value. The deprecated `--legacy-pipe` home payload has no
`help[]` line.

When no review has run yet, `review_id` is a quoted empty string (`""`) and
`review_status` is `none` — the home view is never an error and never empty, even
on a first run with no `.atcr/latest` pointer. When `.atcr/latest` exists but the
review it names cannot be read (a dangling or corrupt pointer, or a pruned or
corrupt review), `review_status` is `unavailable` and `review_id` is the named
review's id when known, else `""`.

### The `review --axi` run-summary payload

`atcr review --axi` (and `atcr review --resume <id> --axi`) gate every
human-oriented stdout line and emit a single-row TOON array describing the run
(the one exception, a baseline re-run with nothing to review, is below):

```
review_summary[1]{id,dir,agents_succeeded,agents_total,agents_failed,agents_timed_out,api_calls,findings_total,findings_critical,findings_high,findings_medium,findings_low}:
  2026-07-18_main,.atcr/reviews/2026-07-18_main,3,3,0,0,9,2,0,1,1,0
```

The header declares the fixed column order with the standard comma delimiter; the single data row carries the run id, the review directory, and ten bare-integer counts. A multi-bin baseline run (`--all` or `--scope`) appends four more integer columns after `findings_low`: `bins_reviewed`, `bins_planned`, `files_covered` and `files_in_scope` (see [Baseline review](../README.md#baseline-review---all----scope)); a diff or bulk review never declares them. `findings_total` is the raw pre-reconcile fan-out count, and the four `findings_<severity>` columns break it down by severity. To act on the reconciled
findings, read `atcr report --format axi` against the same review directory —
`findings_total` is a fan-out metric, not the deduplicated reconciled count. One
exception: a `review --resume <id> --axi` run that finds nothing pending (the
review is already fully complete) runs no fan-out, so its payload reports the
just-reconciled total instead of a fan-out count.

A run that exits 0 ends with a `help[]` block naming the usual next commands,
with the review id left as the literal placeholder `<id>`:

```
help[2]: atcr report <id>,atcr verify <id>
```

Without `--axi` the same suggestions go to stderr as `Next: atcr report <id>`
and `Next: atcr verify <id>`, so the human stdout is unchanged. A run that exits
non-zero, `--dry-run`, and the deprecated `--legacy-pipe` payload get no
`Next:` or `help[]` suggestions. The one exception is a signal-interrupted
review, whose stderr notice names `atcr status <id>` and
`atcr review --resume <id>` from the same registry.

A `review --all` or `review --scope <dir>` re-run that finds every in-scope file
unchanged since the last completed review creates no review. It exits 0, writes
its `nothing to review` notice to stderr, and emits no `review_summary` row:
stdout is only a `help[]` block naming the same scan with `--no-file-cache`, never
a report or verify of a review that does not exist:

```
help[1]: atcr review --all --no-file-cache
```

`.atcr/latest` still points at the earlier review. Without `--axi` the notice is
on stdout and the suggestion is a `Next:` line on stderr; under `--legacy-pipe`
stdout is empty.

### The `report --format axi` findings payload

`atcr report --format axi` emits the reconciled findings as a standard TOON tabular array whose header declares the nine-column [`atcr-findings/v1`](findings-format.md) field set plus the finding's `id` (the value `atcr debt resolve <id>` takes). Optional columns follow `id`, declared only when at least one finding carries the signal, in this order: `disagreement`, `verification.verdict`, `verification.skeptic`, `verification.notes`, `verification.challenge_survived`, `evidence_exec.command`, `evidence_exec.exit_code`, `evidence_exec.output_excerpt`, `fix_warning` and `fix_review`. The rows are followed by `total` and `truncated` metadata lines (see [Pagination and truncation](#pagination-and-truncation)) and a `help` line naming the usual next commands (see [Next-step suggestions](#next-step-suggestions)):

```
findings[2]{severity,"file:line",problem,fix,category,est_minutes,evidence,reviewers,confidence,id}:
  CRITICAL,"auth.go:42",token never expires,check expiry,security,15,expiresAt unread,"greta,host",HIGH,cf9a121839d95a37
  LOW,"util.go:7",unused var,"",style,0,"",otto,MEDIUM,dcf65c25aac8f0d4
total: 2
truncated: false
help[2]: atcr debt list,atcr verify <id>
```

Free-text fields are quoted only when TOON requires it (empty string, embedded delimiter or special character, number- or reserved-token-looking value); integer columns are emitted as bare TOON integers. The stdout bytes are guaranteed free
of ANSI/OSC escape sequences and Markdown syntax — it is TOON only.

### Next-step suggestions

After a successful run, some commands name the commands that usually come next,
so an agent without the installed Skill does not spend a turn working out the
command graph. A runtime value the CLI cannot know for certain stays a literal
`<id>` placeholder; substitute the review id or path yourself.

- `atcr report --format axi` ends its payload with a TOON `help` array:
  `help[2]: atcr debt list,atcr verify <id>`. It is written wherever the payload
  goes, `--output` included. `--format md`, `json`, `checklist`, `sarif` and
  `pipe` (and `--format axi` under `ATCR_LEGACY_PIPE`) carry no `help` line.
- `atcr reconcile --axi` replaces its `reconciled N finding(s)` line with one
  TOON object of the same values, followed by the same `help` array:

  ```
  findings: 1
  sources: 1
  reconciled_dir: .atcr/reviews/<id>/reconciled
  help[2]: atcr debt list,atcr verify <id>
  ```

  No `Next:` line is written, on stderr or as a `--log-format json` record.
- `atcr verify --axi` replaces its `verified N finding(s)` line with one TOON
  object of the same values, followed by a `help` array:

  ```
  findings: 4
  confirmed: 2
  refuted: 1
  unverifiable: 1
  reconciled_dir: .atcr/reviews/<id>/reconciled
  help[1]: atcr report <id>
  ```

  No `Next:` line is written, on stderr or as a `--log-format json` record. A
  run whose skeptics' tool harness could not be built stays a plain error
  (exit 1) with no payload and no `help` block, exactly as in text mode.
- Without `--axi`, `atcr verify` prints `Next: atcr report <id>` on **stderr**
  after its result line, and leaves stdout unchanged. It stays a raw stderr
  line under `--log-format json` too, exactly as before `--axi` existed.
- `atcr status --axi` replaces its indented JSON document with one TOON object
  of the same fields (see [status.md](status.md#axi-mode)), followed by the
  same `help` array: `help[1]: atcr report <id>`, plus a
  `review --resume <id>` line while agents are pending. No `Next:` line is
  written, on stderr or as a `--log-format json` record. Without `--axi`,
  stdout stays the JSON document and the `Next:` lines stay on stderr,
  byte-identical.
- Without `--axi`, `atcr reconcile` prints the same suggestions as human
  lines on **stderr**, after its other diagnostics, and leaves stdout unchanged:

  ```
  Next: atcr debt list
  Next: atcr verify <id>
  ```

  It prints them whenever the reconcile itself ran, including a run whose
  `--fail-on` gate exits 1. When `--repo-root` (or the deprecated `--repo`) was
  set, the `verify` line carries it forward, shell-quoted when needed:
  `Next: atcr verify <id> --repo-root='/path/with space'`, and the `debt list`
  line names the store that root's findings were written to, with a relative
  root made absolute:
  `Next: atcr debt list --store='/path/with space/.atcr/debt'`. No other flag
  is carried.

## Exit codes

AXI mode changes only the *shape* of stdout, never the exit code. `atcr review
--axi`, `atcr review --resume <id> --axi`, and `atcr report --format axi` return
the same **0/1/2/3** codes as their non-`--axi` counterparts for the same inputs.
The authoritative table is in [CI Integration → Exit semantics](ci-integration.md#exit-semantics);
it is not duplicated here. In short:

- **0** — run completed, no findings at/above the `--fail-on` threshold.
- **1** — failure, including a gate failure (findings at/above threshold survived reconciliation) and an internal fault such as a render or harness error.
- **2** — usage or configuration error (bad flags, empty range, invalid config).
- **3** — authentication failure (`--sync-cloud` with a missing/invalid key). A
  `--sync-cloud` run that omits the required `--cloud-endpoint` exits **2**
  instead: the destination is validated before the credential, and this build
  compiles in no default destination.

New AXI-introduced error paths classify into that existing contract rather than
adding a code:

- An unsupported `--axi` flag combination — `atcr review --axi --auto-fix`
  (`--auto-fix` drives an interactive write-back flow, not a consumable payload)
  — is a **usage error (2)**. The `--auto-fix` flow's own sandboxed-by-default
  validation and the `--no-sandbox` opt-out are documented in
  [Auto-fix sandboxed validation](auto-fix.md).
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

- **Default cap: 500 lines of array** — the array header plus up to 499 finding rows (header inclusive). The closing `total:`, `truncated:` and `help[...]:` lines are not counted, so a capped payload is at most 503 physical lines.
- **Override:** set `ATCR_AXI_MAX_LINES` to a positive integer to raise or lower
  the cap. A blank, non-numeric, zero, or negative value is ignored — the cap
  fails open to 500 and a single warning is written to **stderr** (never stdout,
  never a nonzero exit).
- **Signal:** every AXI findings payload carries a `total: <int>` line and a `truncated: <bool>` line after the array, cut or uncut. When findings exceed the cap, rows past the cap are dropped before encoding and `truncated: true` is emitted.

The array header's declared count `N` (`findings[N]{...}`) always equals the rows actually present, so a truncated payload still decodes with any stock TOON decoder. The **true, pre-truncation total** is on the `total` line. When `truncated` is `true`, treat the payload as partial: re-invoke with a higher `ATCR_AXI_MAX_LINES` to retrieve the full set, or record the result as incomplete — do not assume you have every finding. (The deprecated legacy pipe path keeps its old contract: header `N` is the true total and there is no `total` line.)

```bash
# Raise the cap for a large review before consuming the full findings set.
ATCR_AXI_MAX_LINES=5000 atcr report --format axi > findings.toon
```

## stdout is payload, stderr is diagnostics

The entire value of AXI mode for an orchestrator is that a redirect produces a
byte-clean file. Under `--axi`:

- **stdout carries only the payload** — the `review_summary`/`findings` TOON and
  its `total`, `truncated` and `help` lines (only the `help` line for a
  nothing-to-review baseline re-run), with zero ANSI/OSC escapes and zero Markdown. `atcr
  review --axi > run.toon` and `atcr report --format axi > findings.toon` yield
  parseable files with nothing else mixed in.
- **stderr carries only diagnostics** — progress, warnings, and all structured
  logs, governed by [`ATCR_LOG_LEVEL` and `--log-format`](logging.md) and passed
  through the same redaction sink. Error diagnostics also go to stderr, not
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
    # Exit 1 also means every agent failed (an invalid key, a provider outage):
    # the gate then has nothing to pass, and the report would read as zero
    # findings. agents_succeeded is the third column of run.toon's one row (the
    # id and dir before it hold no comma for a review under .atcr/reviews).
    succeeded=$(awk -F, 'row { print $3; exit } /^review_summary\[/ { row = 1 }' run.toon)
    if [ "${succeeded:-0}" -eq 0 ]; then
      echo "every agent failed; nothing was reviewed (see review.log)" >&2
      exit 1
    fi

    # Findings survived the gate. Read them as an AXI payload (stdout only).
    # report returns 2 for a usage/config fault (bad anchor, absent findings.json)
    # and 1 for an internal render fault — propagate whichever actually occurred.
    atcr report --format axi > findings.toon 2> report.log
    rc=$?
    if [ "$rc" -ne 0 ]; then
      echo "report failed (exit $rc); see report.log" >&2
      exit "$rc"
    fi

    # 3. Trust the payload only after checking the truncation flag. If truncated,
    #    the header's N counts only the rows present and `total:` holds the true
    #    count — re-run with a higher cap rather than assuming the list is complete.
    if grep -qx 'truncated: true' findings.toon; then
      echo "findings truncated; re-running with a higher cap" >&2
      ATCR_AXI_MAX_LINES=20000 atcr report --format axi > findings.toon 2> report.log
    fi

    # 4. Hand findings.toon to your fixer (see the note below the example).
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
    # Reaching it requires passing --cloud-endpoint too: with no destination the
    # run exits 2 before the credential is ever read.
    echo "auth error: fix credentials (e.g. ATCR_API_KEY) before retrying" >&2
    exit 3
    ;;
esac
```

Step 4 is yours to fill in: pass `findings.toon` to whatever your agent uses to act on findings. Decode the rows with a standard TOON decoder. The payload is spec-compliant TOON, so any off-the-shelf decoder reads it (in Go: `goaxi.DecodeTabular`). Free-text cells can hold commas, pipes, and escaped newlines, so never split rows with a bare shell split. The typed columns `verification.challenge_survived` and `evidence_exec.exit_code` are `null` on a finding with no verification or evidence block, so decode them into nullable fields (pointers in Go); a plain int would read a missing evidence block as exit code 0.

Two invariants the example relies on, both guaranteed by AXI mode: `review.log`
and `report.log` capture *all* diagnostics because stderr is the only diagnostic
sink, so `run.toon`/`findings.toon` are byte-clean payloads; and the exit code is
authoritative, so the sweeper never has to parse stdout to decide whether the run
failed. The one thing exit 1 does not say is why: findings survived the gate, or
every agent failed and nothing was reviewed, which is why step 2 reads
`agents_succeeded` from `run.toon` before acting on the findings.

## The `--json` convention

Outside AXI, every command that prints a report for a person takes the same
opt-in `--json` flag: the default output stays human, and `--json` prints the
same data as JSON on stdout, with notices kept on stderr. The reporting commands
follow it:

| Command | `--json` prints |
|---------|-----------------|
| `atcr report` | the same JSON as `--format json`; `--json` with any other `--format` is a usage error (exit `2`) |
| `atcr quality-report` | the same JSON as `--format json`, with the same usage error |
| `atcr history` | the history table: `columns`, `rows` and the grand `totals` ([history.md](history.md#cli-usage)) |
| `atcr scorecard` | `run_id` and one stored record per reviewer row ([scorecard.md](scorecard.md#atcr-scorecard-id-or-path)) |
| `atcr leaderboard` | the table's rows as a JSON array ([scorecard.md](scorecard.md#atcr-leaderboard)) |
| `atcr debt dashboard` | the dashboard's sections as one document ([technical-debt.md](technical-debt.md#atcr-debt-dashboard)) |
| `atcr personas list` | the table's rows, `--scores` included ([personas-install.md](personas-install.md#atcr-personas-list)) |

`atcr leaderboard export` is not a `--json` view of the table: it prints a
different document, the anonymized public submission, and is always JSON
([scorecard.md](scorecard.md#atcr-leaderboard-export---output-path)).

Three commands are left out on purpose. `atcr range` and `atcr status` print
JSON by default (`atcr status --axi` prints TOON instead), so a flag would change nothing. `atcr trust` authorizes
providers; it is not a report. Four more print reports with no `--json` yet:
`atcr audit-report`, `atcr benchmark fit`, `atcr debt report` (use
`atcr debt list --json` for the machine-readable form) and
`atcr personas search`.
`cli/json_convention_test.go` walks the command tree and fails when a new
report-producing command has no `--json`.

## See also

- [CI Integration](ci-integration.md) — the exit-semantics contract AXI reuses,
  and the one-shot `--fail-on` gate pattern.
- [Findings Format — `atcr-findings/v1`](findings-format.md) — the field set the
  `findings` payload maps onto.
- [Logging](logging.md) — the stderr diagnostics sink, log levels, and redaction.
