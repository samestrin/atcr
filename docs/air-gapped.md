# Running ATCR Air-Gapped

"Air-gapped" here means atcr makes **no outbound network call at all**: no
telemetry, no persona fetches, and every model request goes to a model server
on your own hardware (Ollama, llama.cpp, vLLM) instead of a hosted API. This
page enumerates every outbound call atcr can make and how to disable or avoid
each one, then gives a known-offline setup recipe.

The core pipeline — `review`, `reconcile`, `verify`, `debate`, `diff-smell`,
`debt`, `history`, `status`, `report` — is local-first: it reads your repo,
calls only the provider endpoints in your registry, and writes under `.atcr/`.
Everything else that touches the network is listed below.

## The egress inventory

### 1. LLM provider endpoints (the review itself)

Every `review`/`reconcile` (and any `verify`/`debate` that re-prompts a model)
POSTs to the `base_url` of each configured provider. This is the bulk of
atcr's traffic and the one that carries your code. To keep it on-premises,
point every provider at a local server — see
[providers.md](providers.md) for the canonical `local` provider block:

```yaml
providers:
  local:
    base_url: http://localhost:11434/v1   # ollama, llama.cpp, vllm, ...
    api_key_env: OLLAMA_API_KEY           # must name an env var that is set — even locally
```

Loopback HTTP is fine here; nothing forces HTTPS on a `localhost` address.
`atcr doctor` also probes each configured provider's `base_url` with small
test requests — harmless when the base URL is loopback, but it does count as
traffic to that endpoint.

### 2. Usage-ping telemetry (on by default)

When a `review` or `reconcile` run completes, atcr sends an anonymous,
four-field usage ping to the compiled-in endpoint
`https://atcr.dev/api/v1/telemetry`. It is **on by default** and fail-open.
Disable it from either of the two OR'd surfaces:

```sh
ATCR_TELEMETRY=0 atcr review ...
atcr config set telemetry false   # persists to .atcr/config.yaml
```

Mind the boolean direction: `ATCR_TELEMETRY` names the *enabled* state, so
`0` (not `1`) turns it off — the opposite polarity of
`ATCR_DISABLE_AST_GROUPING`. The full privacy model and the
`--telemetry-endpoint` operator redirect are in
[telemetry.md](telemetry.md).

### 3. Community prompt quality signal (off by default)

An opt-in aggregate signal sent to `https://atcr.dev/api/v1/quality-signal`
when enabled via `ATCR_QUALITY_SIGNAL=1` or
`atcr config set quality_signal true`. It is off unless you turn it on, so an
air-gapped setup needs to do nothing — just do not opt in. Preview what it
would send without transmitting anything:

```sh
atcr review --dry-run ...
```

### 4. Scorecard cloud sync (`--sync-cloud`)

An explicit, per-run push of the pseudonymous scorecard, gated on both the
flag and `ATCR_API_KEY`. Never sent in the background; simply do not pass
`--sync-cloud` (this build ships no default destination anyway). See
[telemetry.md](telemetry.md#cloud-sync---sync-cloud).

### 5. Community persona fetch

`atcr init` and the default `atcr quickstart` fetch the community persona
index and persona files from the public community repo
(`https://raw.githubusercontent.com/samestrin/atcr/main/personas/community`)
unless told otherwise, and `atcr personas search` / `install` / `upgrade`
fetch from the same place. To stay offline:

```sh
atcr init --offline       # scaffold from the embedded built-in personas only
atcr quickstart --offline # same, for the default preset
```

`atcr quickstart --preset local` never fetches — it scaffolds from the copies
built into the binary. `atcr init --refresh-personas` also never fetches: it
only overwrites built-in persona copies under `.atcr/personas` that differ
from the embedded ones, which is how existing projects pick up a tuned
`personas/_base.md`. The fetch source can also be re-pointed at an internal
mirror with `ATCR_PERSONAS_URL` if your policy allows one vetted host.

### 6. GitHub surfaces

These are all explicit, never background, and make no call unless used:

- `atcr github` posts reconciled findings to a pull request via the GitHub
  REST API (`https://api.github.com`).
- `review --auto-fix` in PR mode talks to the GitHub REST API (the `--api-url`
  flag overrides the base).
- `atcr personas submit` drives the `gh` CLI to fork and open a pull request.
- The GitHub Action in [github-action.md](github-action.md) runs on GitHub's
  runners, not your machine.

On an air-gapped network, do not use them; nothing else calls GitHub.

### 7. Maintainer-only fetches

Two surfaces fetch by design and are never part of a review run:

- `atcr models refresh` fetches OpenRouter's `/api/v1/models` to
  regenerate the checked-in catalog snapshot (maintainer-only; requires
  `OPENROUTER_API_KEY`). `atcr models check` reads the checked-in snapshot and
  makes no network call.
- The benchmark dataset importer (`cmd/ingest-alibaba-benchmark`) downloads
  the dataset from GitHub. Running the vendored suites under `benchmarks/`
  with `atcr benchmark run` is fully offline apart from the provider calls.

## A known-offline setup

From a fresh machine with a local Ollama server:

```sh
export OLLAMA_API_KEY=local-no-key-needed   # required even for keyless local endpoints
atcr quickstart --preset local              # no network, no key, telemetry off
ollama pull gemma3:27b                      # gerald; pick the tiers your RAM allows
```

`--preset local` writes the `local` provider (`http://localhost:11434/v1`)
and the `gerald`/`orson`/`liam` agents to `~/.config/atcr/registry.yaml`,
scaffolds `.atcr/` with their personas from the embedded copies, and sets
`telemetry: false` in `.atcr/config.yaml`. If the registry already exists it
is not overwritten without `--force` — the block to merge is printed instead.
See the "Local / Privacy-First" section of
[personas-install.md](personas-install.md) for the per-tier persona table and
the by-hand equivalent.

Then keep the defaults that are already safe: leave the quality signal off,
never pass `--sync-cloud`, and use `--offline` (or the preset) for any future
`init`. With those in place, the only packets atcr produces go to your
loopback model server.

## Verifying the seal

To confirm a configuration makes no external calls, run a review on a machine
(or firewall namespace) whose only route is loopback:

- A provider pointing outside loopback fails fast: the review errors naming
  the unreachable endpoint.
- The telemetry ping is fail-open and drops silently, so on a truly sealed
  network the only observable cost is up to 2 seconds at process exit while
  the sender drains — opt out with `ATCR_TELEMETRY=0` to remove even that.
- `atcr doctor` reports each provider's reachability, so against a loopback
  registry it doubles as a connectivity self-test.

## Related

- [Telemetry](telemetry.md) — the full privacy model for the usage ping, the
  quality signal, and `--sync-cloud`.
- [Installing Community Personas](personas-install.md) — the local personas
  (`gerald`, `orson`, `liam`) and the offline install paths.
- [Providers](providers.md) — provider `base_url` configuration, including
  the `local` block.
