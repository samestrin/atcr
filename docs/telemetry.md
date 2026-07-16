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

The usage ping fires silently as a byproduct of a completed run. It is
**fail-open**: if the network is down, slow, or the endpoint is unreachable, the
ping is dropped and the CLI exits exactly as it would have anyway. It never
blocks, delays, or crashes the command.

---

## Usage ping schema

The ping carries exactly four allowlisted fields and nothing else:

| Field    | Type   | Example        | Meaning                                  |
|----------|--------|----------------|------------------------------------------|
| `event`  | string | `review_run`   | Which command completed.                 |
| `lang`   | string | `go`           | Primary language of the reviewed code.   |
| `lines`  | int    | `450`          | Approximate line count of the change.    |
| `status` | string | `success`      | Run outcome.                             |

That is the whole payload. A field that is not in this allowlist cannot leak,
because it is never placed into the event in the first place.

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

## Related

- [Scorecard](scorecard.md) — the local per-reviewer record and the separate
  `--export` allowlist for the public Model-Eval Leaderboard.
- [Metrics](metrics.md) — the operational metrics atcr records locally.
