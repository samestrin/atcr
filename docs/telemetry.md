# Telemetry

`atcr` can emit an **anonymous, opt-out usage ping** when a `review` or
`reconcile` run completes, and — only when you explicitly ask for it — push an
anonymized copy of your local scorecard to a cloud dashboard with
`--sync-cloud`. Both paths are designed so that no source code, file paths, or
file contents ever leave your machine.

This document is the complete privacy model for those two paths. It is a
**separate, additive** data path from the local-store `--export` allowlist
described in [scorecard.md](scorecard.md) — the two use different schemas and
different guarantees; nothing here changes what `--export` does.

The usage ping is wired to fire silently as a byproduct of a completed run, and
it is **fail-open**: if the network is down, slow, or the endpoint is
unreachable, the ping is dropped and the CLI exits exactly as it would have
anyway. It never blocks, delays, or crashes the command.

> **Currently inactive.** The compiled-in ingestion endpoint is empty in this
> build, so the ping is a wired **no-op** — nothing is transmitted until a real
> backend endpoint is configured. The schema below describes the payload that
> *would* be sent; the opt-out surfaces already suppress it regardless.

---

## Usage ping schema

The ping carries exactly four allowlisted fields and nothing else:

| Field    | Type   | Example                    | Meaning                                  |
|----------|--------|----------------------------|------------------------------------------|
| `event`  | string | `review_run`/`reconcile_run` | Which command completed.               |
| `lang`   | string | `go`                       | Primary language of the reviewed code.   |
| `lines`  | int    | `450`                      | Approximate line count of the change.    |
| `status` | string | `success`                  | Run outcome.                             |

That is the whole payload. A field that is not in this allowlist cannot leak,
because it is never placed into the event in the first place.

The `event` value is `review_run` or `reconcile_run`. A `reconcile_run` event
carries an empty `lang` and `lines` of `0` — a reconcile spans multiple sources
with no single language or change size — so those two fields are populated only
for `review_run`.

**Preserved (allowlist):**

- `event`, `lang`, `lines`, `status`

**Stripped / never collected:**

- Source code and file contents
- File paths and repository names
- Reviewer names, model names, run IDs, cost, and token counts
- Any finding text, prompt, or persona identity (the ping carries none of these)

---

## Opt-out

Telemetry is **on by default** and can be disabled from two independent
surfaces. The two are **OR'd**: telemetry is disabled whenever *either* surface
says so, and neither can re-enable what the other disabled. There is no
precedence or override chain.

| `ATCR_TELEMETRY`   | config `telemetry` | Result   |
|--------------------|--------------------|----------|
| unset / truthy     | unset / `true`     | enabled  |
| `0` / falsy        | unset / `true`     | disabled |
| unset / truthy     | `false`            | disabled |
| `0` / falsy        | `false`            | disabled |

### `ATCR_TELEMETRY` environment variable

Read once, at root-command construction, so every subcommand resolves it
identically.

```sh
ATCR_TELEMETRY=0 atcr review ...
```

> **Note the inverse boolean direction.** `ATCR_TELEMETRY` names the **enabled**
> state directly — the opposite polarity of `ATCR_DISABLE_AST_GROUPING`, which
> names a *disable* switch. So `ATCR_TELEMETRY=0` (not `=1`) turns telemetry
> **off**. A recognized falsy value (`0`, `false`, `f`, `F`, `False`, `FALSE`)
> disables the ping; unset, blank, or any unparseable value fails **open** to the
> default-on posture.

### `atcr config set telemetry`

Persists the setting to `.atcr/config.yaml` in the current project, so the
opt-out survives across runs without needing the environment variable:

```sh
atcr config set telemetry false   # disable the usage ping for this project
atcr config set telemetry true    # re-enable it
```

The value accepts the `strconv.ParseBool` vocabulary (`true`/`false`, `1`/`0`,
`t`/`f`, `True`/`False`, `TRUE`/`FALSE`). Because the two surfaces are OR'd,
persisting `telemetry false` here disables the ping even with no environment
variable set; and setting it back to `true` does **not** override an
`ATCR_TELEMETRY=0` still present in the environment.

---

## Persona Leaderboard data

The usage ping above never carries a persona. Persona identity is used only by
the **Persona Leaderboard** aggregation, and only ever in **hashed** form.

When a persona identity is included (for the leaderboard, and in the
`--sync-cloud` payload below), it is transmitted as a **one-way SHA-256 hash** of
the raw persona/reviewer string — never the raw string itself:

- **Deterministic:** the same persona always hashes to the same value, so the
  backend can correlate a persona's results across runs without ever learning
  its name.
- **One-way:** the digest is not directly reversible.
- **Pseudonymous, not anonymous:** the hash is *unsalted*. Because persona names
  are a small, enumerable, often publicly-known set, someone who pre-hashes a
  list of known persona names could match a digest back to a name. Treat
  `persona_id_hash` as a stable pseudonym, not a secret.

This hashing path is **completely separate** from the `--export` /
`PublicRecord` allowlist in [scorecard.md](scorecard.md): it lives in its own
schema, and the leaderboard/cloud-sync payload is not a superset of the export
record.

---

## Cloud sync (`--sync-cloud`)

Cloud sync is **not** telemetry and is **not** governed by the opt-out above. It
is an explicit, per-run action you request with the `` `--sync-cloud` `` flag,
and it is gated by its own opt-in: you must both pass the flag and provide a
valid API key. It is never sent in the background.

When set, `--sync-cloud` pushes the anonymized scorecard for the just-finished
run to the configured cloud dashboard as a single POST after the run's outcome
is finalized:

```sh
export ATCR_API_KEY=<your-key>
atcr reconcile --sync-cloud ...
```

**Authentication.** The request authenticates with the `ATCR_API_KEY`
environment variable, sent as an `Authorization: Bearer <ATCR_API_KEY>` header.
The key is trimmed and validated before use and is never logged, echoed in an
error message, or placed in the payload.

**Dedicated auth exit code.** A missing/empty `ATCR_API_KEY`, or a `401`/`403`
rejection from the endpoint, exits with a **dedicated authentication exit code
(`3`)** — distinct from the generic usage-error exit code (`2`) and the generic
failure code (`1`) — so scripts and CI can detect an auth failure specifically
rather than seeing a silent no-op. A non-auth failure (timeout, DNS, `5xx`) does
**not** map to the auth code and never corrupts the already-finalized run
outcome.

**Endpoint.** The push targets a compiled-in `https://` dashboard endpoint. The
destination is HTTPS-only so the Bearer key is never transmitted in the clear;
the `` `--cloud-endpoint` `` flag can override it (for example, at a loopback
`http://` address for local testing).

**What is pushed.** The payload is a dedicated allowlist — **not** a superset of
the `--export` record, and it carries no source code, file paths, or raw
identifiers:

- `schema_version`, `run_outcome`
- Run-level metrics already computed for your local scorecard: `cost_usd`,
  `tokens_in`, `tokens_out`, `latency_ms`
- A `personas` list, one entry per reviewer, each carrying `persona_id_hash`
  (the one-way hash above), `model`, and that reviewer's `cost_usd`,
  `tokens_in`, `tokens_out`, `latency_ms`

The CLI ships these **raw metrics** rather than a computed "time/credits saved"
figure — any ROI/savings number the dashboard shows is derived on the backend
against whatever baseline the dashboard defines, not invented by the CLI.

---

## Community prompt quality signal

The **community prompt quality signal** is a separate, **opt-in**, aggregate,
content-free signal that tells the maintainer which reviewer prompts (persona +
model pairs) are over-reporting — derived entirely from the local dismissal
outcomes (`wontfix`/`resolved`) already recorded in your local debt store. Its
purpose is to point prompt tuning at the personas that most need it, without ever
transmitting a line of your code or a word of any finding.

It is its **own path**, distinct from the usage ping and from `--sync-cloud`:

- It is **off by default** and sends nothing until you explicitly opt in — the
  inverse polarity of the usage ping, which is on-by-default/opt-out.
- Its opt-in shares **no state** with the usage-ping opt-out or with
  `--sync-cloud`: enabling or disabling one has no bearing on the other.
- It is **fail-open**: any transport failure (a non-2xx response, DNS failure,
  timeout, or panic) is dropped and the `review`/`reconcile` run exits exactly as
  it would have anyway — the send never changes the exit code or stdout.

> **Currently inactive.** Like the usage ping, the compiled-in ingestion endpoint
> is empty in this build, so an opted-in send is a wired **no-op** — nothing is
> transmitted until a real backend endpoint is configured. The schema below
> describes the payload that *would* be sent; the opt-in is off by default
> regardless.

### Quality-signal payload schema

The payload is a list of per-(persona, model) rows. Each row carries exactly four
allowlisted fields and nothing else:

| Field              | Type   | Example                                                              | Meaning                                                      |
|--------------------|--------|---------------------------------------------------------------------|-------------------------------------------------------------|
| `persona_id_hash`  | string | `a1b2c3…` (SHA-256 hex)                                              | The one-way hash of the reviewer/persona name (see below).  |
| `model`            | string | `claude-sonnet-4-6`                                                  | The model slug that ran under that persona.                 |
| `dismissed_count`  | int    | `3`                                                                  | How many of that pair's findings were dismissed (`wontfix`).|
| `confirmed_count`  | int    | `1`                                                                  | How many were confirmed/kept (`resolved`).                  |

That is the whole payload. This is its **own separately-tested allowlist** — it
does **not** embed or extend the usage-ping `event`, any scorecard struct, or a
local-debt record, so no source code, file path, or finding text can structurally
be placed into it. A dedicated regression test locks the wire schema to exactly
these four keys and fails loudly on any future field addition.

None of the four fields carries `omitempty`, so **zero-value counts always
serialize as `0`** — a maintainer can distinguish "zero dismissals" from "field
absent".

### Opt-in

The quality signal is **off by default** and can be enabled from two independent
surfaces. The two are **OR'd** (opt-**in**): the signal is enabled whenever
*either* surface says so. This is the **inverse** of the usage-ping opt-out, and
it carries no precedence or override chain of its own.

| `ATCR_QUALITY_SIGNAL` | config `quality_signal` | Result   |
|-----------------------|-------------------------|----------|
| unset / falsy         | unset                   | disabled (the default) |
| unset / falsy         | `true`                  | enabled  (config alone is sufficient consent) |
| unset / falsy         | `false`                 | disabled |
| truthy                | unset                   | enabled  (env alone is sufficient consent) |
| truthy                | `true`                  | enabled  |
| truthy                | `false`                 | enabled  (an explicit env opt-in is never revoked by a stale config `false`) |

An unset config value is **neutral**: it contributes nothing to the OR and can
never out-rank a permitting env var.

**Independence.** This gate shares no state with the usage-ping opt-out or with
`--sync-cloud`. A telemetry opt-out, a valid `ATCR_API_KEY`, or an enabled
`--sync-cloud` plan can neither grant nor revoke quality-signal consent, and vice
versa.

#### `ATCR_QUALITY_SIGNAL` environment variable

```sh
ATCR_QUALITY_SIGNAL=1 atcr review ...
```

> **Note the opt-in direction.** `ATCR_QUALITY_SIGNAL` names the **enabled** state
> directly and defaults **off**, the opposite posture of `ATCR_TELEMETRY` (which
> is enabled-by-default). A recognized truthy value (`1`, `true`, `t`, `T`,
> `True`, `TRUE`) opts in; unset, blank, or any **unparseable** value fails
> **safe** to disabled — a corrupt value can never be read as consent to transmit
> (an unparseable value also warns to stderr so a misspelled opt-in is visible).

#### `atcr config set quality_signal`

Persists the opt-in to `.atcr/config.yaml` in the current project, so it survives
across runs without needing the environment variable:

```sh
atcr config set quality_signal true    # opt in for this project
atcr config set quality_signal false   # opt back out
```

The value accepts the `strconv.ParseBool` vocabulary (`true`/`false`, `1`/`0`,
`t`/`f`, `True`/`False`, `TRUE`/`FALSE`). Setting `quality_signal` leaves the
sibling `telemetry` key untouched, and a **malformed** persisted value fails
**safe** to disabled — never a silent re-enable.

### `--preview` — see exactly what would be sent, before opting in

The `review` and `reconcile` commands accept a `--preview` flag that renders the
exact content-free payload locally and **sends nothing**:

```sh
atcr review --preview ...
atcr reconcile --preview ...
```

`--preview` prints the pretty-printed JSON payload followed by a distinct
human-readable marker on its own line:

```json
[
  {
    "persona_id_hash": "a1b2c3…",
    "model": "claude-sonnet-4-6",
    "dismissed_count": 3,
    "confirmed_count": 1
  }
]
Preview only — nothing was transmitted. The quality signal is sent only after you explicitly opt in.
```

The printed JSON is **byte-identical** to the marshaled body a real send would
transmit — both paths build the payload from a single shared constructor, so the
preview can never drift from what is actually sent. Key properties:

- It **needs no opt-in**: `--preview` renders whether or not you have enabled the
  signal, and it runs **before** any opt-in gate check.
- It **makes no network call** and requires **no credential** — it never reads
  `ATCR_API_KEY` and constructs no HTTP client.
- It **takes precedence over `--sync-cloud`**: `--preview --sync-cloud` together
  prints the payload and pushes nothing.
- On a fresh checkout with no dismissal history, it prints an **empty payload**
  (`[]`) rather than failing.

The marker line is deliberately **separate** from the JSON so the preview can
never be mistaken for confirmation that data was transmitted.

### No code, no finding content, ever

The quality signal transmits **only** the four allowlisted primitive fields above:
a hashed persona identifier, a model slug, and two integer counts. It carries **no
source code, no file path, no repository name, and no finding text, title, or
excerpt** — not by policy but structurally: the payload type cannot embed or
extend any struct that holds those, and the four-key allowlist is locked by a
regression test. Nothing is sent by default, and `--preview` lets you inspect the
exact bytes before you ever opt in.

The `persona_id_hash` follows the **same** hashing model documented under
[Persona Leaderboard data](#persona-leaderboard-data): a **one-way, unsalted
SHA-256** of the raw persona/reviewer name.

- **Deterministic and one-way:** the same persona always hashes to the same value
  and the digest is not directly reversible.
- **Pseudonymous, not anonymous:** because persona names are a small, enumerable,
  often publicly-known set, someone who pre-hashes a list of known persona names
  could match a digest back to a name. Treat `persona_id_hash` as a stable
  pseudonym, not a secret. Hardening the hash to a keyed HMAC is tracked as
  **TD-007**; the quality-signal path shares that same unsalted scheme and would
  be hardened in lockstep.

---

## Related

- [Scorecard](scorecard.md) — the local per-reviewer record and the separate
  `--export` allowlist for the public Model-Eval Leaderboard.
- [Metrics](metrics.md) — the operational metrics atcr records locally.
