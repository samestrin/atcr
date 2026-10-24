# Telemetry

`atcr` can emit an **anonymous, opt-out usage ping** when a `review` or
`reconcile` run completes, and — only when you explicitly ask for it — push a
pseudonymous copy of your local scorecard to a cloud dashboard with
`--sync-cloud`. Both paths are designed so that no source code, file paths, or
file contents ever leave your machine.

This document is the complete privacy model for those two paths. It is a
**separate, additive** data path from the local-store `leaderboard export` allowlist
described in [scorecard.md](scorecard.md) — the two use different schemas and
different guarantees; nothing here changes what `leaderboard export` does.

The usage ping is wired to fire silently as a byproduct of a completed run, and
it is **fail-open**: if the network is down, slow, or the endpoint is
unreachable, the ping is dropped and the CLI exits with exactly the code it would
have anyway. It never crashes the command, never changes its exit code, and never
delays the work itself — the send runs on a detached goroutine, so nothing in the
review or reconcile blocks on it.

It can, however, delay process **exit** by a bounded amount. Before exiting,
`atcr` waits up to **2 seconds** for in-flight telemetry work to finish, so a
best-effort send usually lands rather than being killed mid-flight. When nothing
was dispatched — you opted out, or the run emitted nothing — the wait returns
immediately and costs nothing. The full 2 seconds is only ever paid when work is
genuinely in flight and the endpoint is unreachable in a way that hangs rather
than refuses (a firewalled, air-gapped, or blackholed network). Opting out
removes this entirely.

This drain covers **both** surfaces. If you have opted in to the quality signal,
it also covers that payload's local aggregation over `.atcr/debt` — which
previously escaped the drain entirely and was usually stranded at exit, meaning
opted-in signals were frequently never delivered. Now that it is covered, an
opted-in run with a large local debt store may spend measurably longer at exit
than before, still bounded by the same 2 seconds.

> **Active in this build.** The compiled-in ingestion endpoint is
> `https://atcr.dev/api/v1/telemetry`, so a completed `review` or `reconcile`
> run transmits the ping described below unless you opt out. The ping is
> **on by default** — set `ATCR_TELEMETRY=0` or `atcr config set telemetry false`
> to disable it (see [Opt-out](#opt-out)); either surface alone is
> sufficient and final.

### Embedding atcr as a library

The default-on posture described here applies to the **`atcr` binary**. If you
embed the command tree in your own Go program via `cli.NewRootCmd()`, **no
telemetry is sent at all** and no notice is printed — that constructor is wired to
a client with no destinations, so your users are never enrolled in a collection
they did not choose and you did not disclose.

To opt in deliberately, build the client yourself and drain it before exiting:

```go
client := telemetry.NewWithQualitySignal(usageURL, qualityURL)
root := cli.NewRootCmdWithClient(client)
// ... run the tree ...
client.Wait()
```

or call `cli.Main`, which runs the same full lifecycle the binary does.

### First-run notice

The first time you run `atcr` in a repository with the ping enabled, it prints a
short notice on **stderr** naming the destination host (scheme and host only,
never the URL path) and the opt-out, so the default-on behavior is disclosed in
the binary itself rather than only here:

```
atcr sends an anonymous usage ping to https://atcr.dev when a review or reconcile completes.
It carries no source code, file paths, or findings — see docs/telemetry.md.
To opt out: set ATCR_TELEMETRY=0, or run `atcr config set telemetry false`.
(This notice is shown once per repository.)
```

To record that it has been shown, atcr writes `telemetry_notice_shown: true` into
`.atcr/config.yaml`, **creating that file if it does not exist**. This key is
bookkeeping only — nothing reads it to decide whether to transmit, and deleting it
simply makes the notice appear once more. If you have already opted out, the
notice never prints: there is nothing to disclose.

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

## Operator redirect (`--telemetry-endpoint`)

An operator can point the usage ping at a different destination (an internal
collector, for example) without rebuilding the binary, via the persistent root
flag `--telemetry-endpoint`:

```sh
atcr --telemetry-endpoint=https://collector.internal/usage review ...
```

**The flag is the consent.** There is deliberately **no** environment variable
and **no** `.atcr/config.yaml` key for this: an ambient env var or a committed
repo file that silently re-points the destination is an exfiltration channel for
usage data. Passing the flag on the command line is the explicit, auditable
opt-in — an override only ever happens because the person invoking the process
asked for it, in a form visible in CI logs and shell history. An ambient
environment alone can never redirect the ping.

**The https/loopback rule.** The value is validated by
`ValidateTelemetryEndpoint` before any client is built, mirroring the
`--cloud-endpoint` rule (`scorecard.ValidateCloudEndpoint`):

- It must be a well-formed absolute URL.
- The scheme must be `https://` — plaintext `http://` is allowed **only** for a
  loopback host (`localhost`, `127.0.0.0/8`, `::1`) for local testing.

A rejected value is a **usage error (exit `2`)** naming `--telemetry-endpoint`;
the subcommand does not run and nothing is transmitted.

**Scope: the usage ping only.** The override redirects the anonymous usage ping
and nothing else. The quality-signal destination stays on its compiled-in
value — the two payloads have different shapes and one URL cannot serve both
(see [why the destinations must stay distinct](#community-prompt-quality-signal)).
The opt-out surfaces are unaffected: `ATCR_TELEMETRY=0` or
`atcr config set telemetry false` still disable the ping wherever it points.

**Startup audit log.** Whenever the flag is given, atcr writes a startup line to
**stderr** (a `slog` text record) so the effective destination is auditable even
in a headless CI run:

```
level=INFO msg="usage telemetry endpoint overridden" endpoint=https://collector.internal
```

The line is written **whether or not the consent gate permits sending** — it
records where the process *would* send — and it is never written on the
non-override path. `ATCR_LOG_LEVEL` does not suppress it: it is still written at
`warn` and `error`, so a CI run that quiets logging still records the redirect.
It follows `--log-format`, so `--log-format json` writes it as a JSON record. When the first-run notice is shown, it names the **effective**
destination, not the compiled-in `atcr.dev` one.

**Embedded trees do not apply the flag.** A tree built with
`cli.NewRootCmdWithClient(client)` keeps the client its embedder supplied: the
flag is still validated (a bad value is still exit `2`), but the ping keeps going
to the client's own endpoint. The startup line says so instead of claiming a
redirect, and names the endpoint actually used:

```
level=INFO msg="usage telemetry endpoint override ignored: the embedder's client is authoritative" endpoint=https://embedder.example/usage
```

Like the line above, it is written at every `ATCR_LOG_LEVEL` and follows
`--log-format`.

Both the audit line and the notice print the endpoint through
`displayTelemetryEndpoint`, which keeps only the scheme and host (with any port):
**the path, userinfo, query strings, and fragments are dropped**, so a credential
or token embedded in an operator URL — including one in the path, such as
`https://collector.internal/ingest/<key>` — is never echoed to stderr.

---

## Persona Leaderboard data

The usage ping above never carries a persona. Persona identity is used only by
the **Persona Leaderboard** aggregation, and only ever in **hashed** form.

When a persona identity is included (for the leaderboard, and in the
`--sync-cloud` payload below), it is transmitted as a **one-way SHA-256 hash** of
the canonicalized persona/reviewer name (`strings.ToLower(strings.TrimSpace(name))`) — never the raw string itself:

- **Canonicalized before hashing:** the persona name is reduced to
  `strings.ToLower(strings.TrimSpace(name))` first, so `bruce`, `Bruce`,
  `BRUCE`, and `" bruce "` all produce **one** digest.

  For an **ASCII** persona name — which every persona in `personas/community/`
  is — the exact equivalent a backend can use to reproduce these values is:

  ```js
  crypto.createHash("sha256").update(name.trim().toLowerCase()).digest("hex")
  ```

  **The cross-language guarantee is scoped to ASCII, and only ASCII.** Go's
  `strings.ToLower` is Unicode *simple* case mapping and `strings.TrimSpace` uses
  `unicode.IsSpace`; JS `toLowerCase`/`trim` apply different rules, and they
  measurably disagree:

  | Input | Go | JS |
  |---|---|---|
  | `ΟΔΥΣΣΕΥΣ` | `οδυσσευσ` | `οδυσσευς` (final sigma) |
  | `İstanbul` | `istanbul` | `i̇stanbul` (combining dot) |
  | `<U+FEFF>bruce` | BOM retained | BOM stripped |
  | `<U+0085>bruce` | NEL stripped → `bruce` | NEL retained → `<U+0085>bruce` |

  Go's `strings.TrimSpace` (via `unicode.IsSpace`) treats U+0085 as whitespace;
  ECMAScript's `WhiteSpace`/`LineTerminator` productions do not, so JS `trim()`
  leaves it in place.

  This does **not** create split identities: a stored persona identity MUST always
  be the digest received from the Go client, and a JS-computed digest is a match
  key only that must never be written as a storage key. The JS above exists solely
  to build a *lookup table* of known persona names. So a divergence means a
  non-ASCII persona simply fails to match that table and is treated as an
  unrecognized persona — a lookup miss, not a second bucket. Do not rely on the
  JS form for a non-ASCII name; port the Go semantics if you ever need one.

  It matters more than it looks: an ingestion backend may re-key the received
  digest under a server-side secret and discard the original, in which case a
  casing or padding variant becomes a **second permanent identity** for one
  persona that no later backfill can merge. Canonicalizing client-side is what
  keeps one persona to one bucket.

  One consequence worth knowing: persona **lookup** is case-sensitive (persona
  name handling is inconsistent about case across the registry — listing folds
  case, on-disk resolution depends on the filesystem — while telemetry now always
  collapses case), but their telemetry collapses to a single digest. That is
  deliberate and matches the trust-prior key space, which has always been
  lowercased — but it does mean a custom persona named as a case-variant of a
  catalog persona reports into the catalog persona's bucket. Give custom personas
  distinct names, not re-casings.

- **Deterministic:** the same persona always hashes to the same value, so the
  backend can correlate a persona's results across runs without ever learning
  its name.
- **One-way:** the digest is not directly reversible.
- **Pseudonymous, not anonymous:** the hash is *unsalted*. Because persona names
  are a small, enumerable, often publicly-known set, someone who pre-hashes a
  list of known persona names could match a digest back to a name. Treat
  `persona_id_hash` as a stable pseudonym, not a secret.

This hashing path is **completely separate** from the `leaderboard export` /
`PublicRecord` allowlist in [scorecard.md](scorecard.md): it lives in its own
schema, and the leaderboard/cloud-sync payload is not a superset of the export
record. The vocabularies differ too: telemetry, the quality signal, and
`--sync-cloud` are persona-**pseudonymous** (hash only, never the raw name),
whereas `leaderboard export` is submitter-**anonymized** and keeps the public persona
catalog identity in the clear — see the `leaderboard export` section of scorecard.md.

---

## Cloud sync (`--sync-cloud`)

Cloud sync is **not** telemetry and is **not** governed by the opt-out above. It
is an explicit, per-run action you request with the `` `--sync-cloud` `` flag,
and it is gated by its own opt-in: you must both pass the flag and provide a
valid API key. It is never sent in the background.

When set, `--sync-cloud` pushes the pseudonymous scorecard for the
just-finished run to the configured cloud dashboard as a single POST after the run's outcome
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

**Endpoint.** This build ships **no default destination** — the scorecard ingest
contract is owned by atcr-enterprise, and there is no API-key issuance flow for a
community user. `` `--cloud-endpoint` `` is therefore **required** whenever
`` `--sync-cloud` `` is used; a run that omits it refuses with a usage error
(exit `2`) naming the absent destination, and pushes nothing. The destination is
HTTPS-only so the Bearer key is never transmitted in the clear (a loopback
`http://` address is permitted for local testing).

Note the ordering: the destination is validated **before** `ATCR_API_KEY`, so a
run missing both reports the missing destination (exit `2`), not the missing key
(exit `3`).

**What is pushed.** The payload is a dedicated allowlist — **not** a superset of
the `leaderboard export` record, and it carries no source code, file paths, or raw
identifiers:

- `schema_version`, `run_outcome`
- Run-level metrics already computed for your local scorecard: `cost_usd`,
  `tokens_in`, `tokens_out`, `latency_ms`
- A `personas` list, one entry per reviewer, each carrying `persona_id_hash`
  (the one-way hash above), `model`, and that reviewer's `cost_usd`,
  `tokens_in`, `tokens_out`, `latency_ms`. `model` is sent as `""` unless it is
  at most 128 characters of `[A-Za-z0-9._:/~@+-]`; `cost_usd` is still priced
  from the reviewer's real model.

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

> **Active in this build.** The compiled-in ingestion endpoint is
> `https://atcr.dev/api/v1/quality-signal` — a **separate** destination from the
> usage ping, because the two payloads have different shapes and the backend
> validates each against its own allowlist. Unlike the usage ping, this signal
> remains **opt-in and off by default**: nothing is transmitted unless you
> explicitly enable it.

**Why the two destinations must stay distinct.** The atcr.dev backend serves the
usage ping and the quality signal as separate handlers behind the `/api/v1/*`
rewrite, each validating against its own closed key allowlist by strict set
equality — an extra or missing key is a `400`, not a warning. The usage ping is a
single JSON object; the quality signal is a JSON array. Posting either at the
other's handler is therefore rejected, and because the send path is fail-open the
rejection is dropped silently: nothing surfaces the loss. Collapsing the two onto
one URL is the regression `TestRootCmd_RoutesPingAndQualitySignalToDistinctEndpoints`
exists to catch.

This section is the versioned home of that contract. It is deliberately **not**
restated in `internal/telemetry`, which has no dependency on, test against, or
visibility into the service — a copy there could never be detected going stale.

### Quality-signal payload schema

The payload is a list of per-(persona, model) rows. Each row carries exactly
seven allowlisted fields and nothing else:

| Field                  | Type   | Example                                                              | Meaning                                                      |
|------------------------|--------|---------------------------------------------------------------------|-------------------------------------------------------------|
| `persona_id_hash`      | string | `a1b2c3…` (SHA-256 hex)                                              | The one-way hash of the reviewer/persona name (see below).  |
| `model`                | string | `claude-sonnet-4-6`                                                  | The model slug that ran under that persona. Sent as `""` unless it is at most 128 characters of `[A-Za-z0-9._:/~@+-]`. |
| `dismissed_count`      | int    | `3`                                                                  | How many of that pair's findings were dismissed (`wontfix`).|
| `confirmed_count`      | int    | `1`                                                                  | How many were confirmed/kept (`resolved`).                  |
| `wrong_merge_count`    | int    | `1`                                                                  | How many of that pair's findings a developer annotated `wrong-merge` with `atcr debt recluster` — reconcile merged distinct issues into the finding. |
| `wrong_split_count`    | int    | `0`                                                                  | The same for `wrong-split` — reconcile split one issue into two findings. |
| `wrong_category_count` | int    | `2`                                                                  | The same for `wrong-category` — reconcile filed the finding under the wrong category. |

The last three keys are **reconciler-error counts**: a verdict on how reconcile
grouped the finding, recorded as a `reconciler_feedback` annotation in the local
debt store (see
[docs/technical-debt.md](technical-debt.md#atcr-debt-recluster)). They are kept
apart from `dismissed_count`, which is a verdict on the **reviewer**: a
`wontfix` dismissal raises `dismissed_count` only, and a `wrong-merge`
annotation raises `wrong_merge_count` only.

That is the whole payload. This is its **own separately-tested allowlist** — it
does **not** embed or extend the usage-ping `event`, any scorecard struct, or a
local-debt record, so no source code, file path, or finding text can structurally
be placed into it. A dedicated regression test locks the wire schema to exactly
these seven keys and fails loudly on any future field addition. The backend
validates the same set by **strict key-set equality** (see above): an extra or
missing key is a `400`, so any change to this key set is a coordinated change
with the atcr.dev ingest.

None of the seven fields carries `omitempty`, so **zero-value counts always
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

### `--dry-run` — see exactly what would be sent, before opting in

The `review` and `reconcile` commands accept a `--dry-run` flag that renders the
exact content-free payload locally and **sends nothing** (`--preview` remains a
deprecated hidden alias):

```sh
atcr review --dry-run ...
atcr reconcile --dry-run ...
```

`--dry-run` prints the pretty-printed JSON payload followed by a distinct
human-readable marker on its own line:

```json
[
  {
    "persona_id_hash": "a1b2c3…",
    "model": "claude-sonnet-4-6",
    "dismissed_count": 3,
    "confirmed_count": 1,
    "wrong_merge_count": 1,
    "wrong_split_count": 0,
    "wrong_category_count": 2
  }
]
Preview only — nothing was transmitted. The quality signal is sent only after you explicitly opt in.
```

The printed JSON is **byte-identical** to the marshaled body a real send would
transmit — both paths build the payload from a single shared constructor, so the
preview can never drift from what is actually sent. Key properties:

- It **needs no opt-in**: `--dry-run` renders whether or not you have enabled the
  signal, and it runs **before** any opt-in gate check.
- It **makes no network call** and requires **no credential** — it never reads
  `ATCR_API_KEY` and constructs no HTTP client.
- It **takes precedence over `--sync-cloud`**: `--dry-run --sync-cloud` together
  prints the payload and pushes nothing.
- On a fresh checkout with no dismissal history, it prints an **empty payload**
  (`[]`) rather than failing.

The marker line is deliberately **separate** from the JSON so the preview can
never be mistaken for confirmation that data was transmitted.

### No code, no finding content, ever

The quality signal transmits **only** the seven allowlisted primitive fields above:
a hashed persona identifier, a model slug, and five integer counts. It carries **no
source code, no file path, no repository name, and no finding text, title, or
excerpt** — not by policy but structurally: the payload type cannot embed or
extend any struct that holds those, and the seven-key allowlist is locked by a
regression test. Nothing is sent by default, and `--dry-run` lets you inspect the
exact bytes before you ever opt in.

The `persona_id_hash` follows the **same** hashing model documented under
[Persona Leaderboard data](#persona-leaderboard-data): a **one-way, unsalted
SHA-256** of the canonicalized persona/reviewer name (`strings.ToLower(strings.TrimSpace(name))`).

- **Deterministic and one-way:** the same persona always hashes to the same value
  and the digest is not directly reversible.
- **Pseudonymous, not anonymous:** because persona names are a small, enumerable,
  often publicly-known set, someone who pre-hashes a list of known persona names
  could match a digest back to a name. Treat `persona_id_hash` as a stable
  pseudonym, not a secret. The keyed hardening step happens at ingestion: the
  server re-keys the digest with HMAC-SHA256 under a server-side pepper and
  never stores the raw digest. It cannot happen in the CLI — a key compiled
  into an open-source binary ships with the binary. The quality-signal path
  uses the same scheme.

---

## Related

- [Scorecard](scorecard.md) — the local per-reviewer record and the separate
  `leaderboard export` allowlist for the public Model-Eval Leaderboard.
- [Metrics](metrics.md) — the operational metrics atcr records locally.
