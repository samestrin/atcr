# Installing Community Personas

ATCR ships with nine built-in reviewer personas (six generalists plus the `sasha`, `penny`, and `ingrid` bonus personas). Beyond those, the `atcr personas` command installs **community-contributed** personas from a configurable repository, so you can extend the reviewer panel with domain-specific lenses — security, performance, framework-specific, and more — without editing your registry by hand.

This guide covers every `atcr personas` subcommand. No source-code lookup is required: each command's behavior and output are described here.

## Where personas live

Installed community personas are written to your per-user config directory:

```
~/.config/atcr/personas/
```

(More precisely, `os.UserConfigDir()/atcr/personas` — `~/.config/atcr/personas/` on Linux, `~/Library/Application Support/atcr/personas/` on macOS.)

A persona installed here is picked up by the reviewer panel on your **next review** — no restart or re-init step is needed. Built-in personas are always available regardless of this directory.

> **Trust note:** A persona is a prompt executed as part of the review pipeline. Installing a community persona means running its prompt against your diff. Only install personas from a registry you trust; the install path validates the persona's YAML against the registry schema before writing it, but it cannot vet prompt intent.

## Configuring the registry URL

By default, `install`, `search`, and `upgrade` fetch from the in-repo community-persona path on the product repository (`samestrin/atcr`), raw-content root:

```
https://raw.githubusercontent.com/samestrin/atcr/main/personas/community
```

(Anonymous raw-content fetches from this URL succeed once `samestrin/atcr` is public; until then, point `ATCR_PERSONAS_URL` at a local or mock registry.)

To point at a different (e.g. private or mirrored) registry, set the `ATCR_PERSONAS_URL` environment variable to its raw-content base URL:

```bash
export ATCR_PERSONAS_URL="https://raw.githubusercontent.com/my-org/personas/main"
atcr personas install security/owasp
```

A persona at `<name>` is fetched from `<ATCR_PERSONAS_URL>/<name>.yaml`; the keyword index is fetched from `<ATCR_PERSONAS_URL>/index.json`. An empty or whitespace-only `ATCR_PERSONAS_URL` is treated as unset (the default URL is used).

## The seven subcommands

### `atcr personas install <namespace/name>`

Fetches a single persona from the registry, validates its YAML against the registry schema, and writes it to `~/.config/atcr/personas/`.

```bash
atcr personas install security/owasp
# Installed persona "security/owasp"
```

Persona names may contain letters, digits, `_`, `-`, and `/` (the namespace separator). Names containing `..`, absolute paths, or other characters are rejected before any fetch or write — a persona can never be written outside the personas directory.

**Installing a bundle.** A bundle installs several related personas in one command. Prefix the bundle name with `bundle/`:

```bash
atcr personas install bundle/django
# Installed framework/django-orm
# Installed language/python-types
# Installed security/owasp
# Installed security/secrets
```

Each member is reported on its own line. A member already on disk is reported `already present` and not re-fetched (install is idempotent — re-running is safe). If one member fails to fetch or validate, the failure is reported to stderr and the remaining members are still attempted; the command then exits non-zero. Two bundles ship today: `bundle/django` and `bundle/go-production`. An unknown bundle name exits non-zero with `unknown bundle: "<name>"`.

**Errors:**
- Unknown persona slug → `persona "<slug>" not found in community repo` (non-zero exit).
- Network unavailable → a fetch error naming the failure; if you are pointed at the wrong host, set `ATCR_PERSONAS_URL` to a reachable registry.
- Invalid persona YAML → the registry validation error; nothing is written.

### `atcr personas list`

Lists installed personas — both built-in and community — as a table:

```bash
atcr personas list
# NAME             VERSION    SOURCE      LANGUAGE
# bruce            built-in   built-in    -
# sasha            built-in   built-in    -
# security/owasp   1.2.0      community   -
# language/go-fmt  0.3.0      community   go
```

Columns: `NAME`, `VERSION` (`built-in` for the built-in personas; the installed manifest version for community personas), `SOURCE` (`built-in` or `community`), and `LANGUAGE` (the persona's declared `language` scope, comma-joined, or `-` when unscoped). If the personas directory is unreadable, `list` prints a warning to stderr and still renders the built-ins (exit 0).

**With corroboration scores.** Add `--scores` to append a `CORROBORATION` column showing each persona's historical corroboration rate from past review runs:

```bash
atcr personas list --scores
# NAME             VERSION    SOURCE      LANGUAGE  CORROBORATION
# security/owasp   1.2.0      community   -         72.4%
# sasha            built-in   built-in    -         n/a
```

The rate is the fraction of a persona's findings that other reviewers or the verify stage corroborated, formatted as `XX.X%`, or `n/a` when there is no run history for that persona. When no scorecard data exists at all, every row shows `n/a` and a footer names the path that was checked:

```
No scorecard data found at <path>
```

### `atcr personas search <keyword>`

Fetches the registry's `index.json` and lists entries whose name or description matches the keyword:

```bash
atcr personas search performance
# NAME                  VERSION  DESCRIPTION
# performance/sql       1.0.0    SQL/ORM query performance
# performance/memory    1.1.0    Memory leak patterns
```

Use `search` to discover a persona's exact slug before `install`. When nothing matches, it prints `No personas found matching "<keyword>"`.

### `atcr personas remove <namespace/name>`

Removes an installed community persona from `~/.config/atcr/personas/`:

```bash
atcr personas remove security/owasp
# Removed persona "security/owasp"
```

The same name-validation guard applies, so `remove` can only delete files inside the personas directory.

### `atcr personas test <name>`

Runs a persona against its committed fixture — with no LLM and no network — and reports pass/fail. It works for built-in personas and for the embedded community-library personas: the fixture renders the persona template against a known diff and confirms the expected finding category and a clean render. A third-party persona installed from a registry that ships no embedded fixture reports `No fixture defined` instead.

```bash
atcr personas test delia
# PASS: delia (1/1 cases)
```

The output contract:

- All cases passing reports `PASS: <name> (N/N cases)` (exit 0).
- Any case failing reports `FAIL: <name> (P/N cases)` to stdout and exits non-zero.
- A persona with no committed fixture reports `No fixture defined for persona "<name>"` and exits 0.

### `atcr personas submit <name>`

Submits a locally-tuned persona back to the canonical `samestrin/atcr` repository as a pull request. The command first runs the **same fixture gate** as [`atcr personas test`](#atcr-personas-test-name); only if the fixture passes does it fork `samestrin/atcr` under your own GitHub identity, push a branch with the persona files, and open a PR. The submission lands as an unvetted **`submitted`** entry pending maintainer graduation — see [authoring §7](personas-authoring.md#7-from-submitted-to-graduated).

`submit` pushes only the persona **unit** — its `.yaml` binding plus the optional co-located `.md` prompt where local tuning lives — never a fixture. The gate resolves the fixture that already ships with the persona, so `submit` targets a persona that **already has a committed fixture**: in practice a community-library persona you installed and re-tuned locally. A brand-new persona has no shipped fixture yet — commit its fixture to `personas/community/testdata/` in the repo first (see [authoring §3](personas-authoring.md#3-the-fixture)).

**Precondition — `gh` installed and authenticated.** `submit` rides *your own* GitHub CLI session: the [GitHub CLI (`gh`)](https://cli.github.com) must be on your `PATH` and you must have run `gh auth login` first. No bot token and no separate credential are written into atcr's config. (On success `submit` does write one small local file — the `submitted` marker sidecar at `~/.config/atcr/submissions/<name>.yaml`; see [authoring §7](personas-authoring.md#7-from-submitted-to-graduated).) The precondition is checked before any fork or branch work.

```bash
atcr personas submit penny
# https://github.com/samestrin/atcr/pull/123
```

On success the command prints **only** the new PR URL (`https://github.com/<owner>/<repo>/pull/<n>`) to stdout. A re-submission of the same persona reuses your existing fork and PR rather than opening a duplicate.

**Errors** (each blocks submission with a non-zero exit and **no** fork/PR side effects):

- **Invalid persona name** → `invalid persona name "<name>": only letters, digits, '_', '-', and '/' are allowed` — the same validation guard as `install`/`remove`.
- **No fixture** → `cannot submit "<name>": no fixture defined — add a fixture before submitting`. Run [`atcr personas test <name>`](#atcr-personas-test-name) first to check fixture status. (This wording is deliberately distinct from `test`'s softer, non-blocking `No fixture defined` — an unvetted persona with no fixture cannot be submitted.)
- **Fixture fails** → `cannot submit "<name>": fixture failed (<passed>/<total> cases passed)`; no fork or PR is attempted.
- **`gh` missing or unauthenticated** → `gh CLI not found on PATH; install it from https://cli.github.com`, or `gh auth check failed: …`. Install `gh` and run `gh auth login`, then retry.

### `atcr personas upgrade [name]`

Upgrades an installed community persona to the latest version in the registry:

```bash
atcr personas upgrade security/owasp
# Upgraded security/owasp: 1.1.0 → 1.2.0
```

- `atcr personas upgrade --all` upgrades every installed community persona. With nothing installed, it prints `No community personas installed`.
- `atcr personas upgrade --dry-run <name>` (or `--all --dry-run`) reports what *would* change without writing: `Would upgrade <name>: <from> → <to>`.
- A persona already at the newest version reports `<name> is already up to date (<version>)`.
- Specifying both a name and `--all` is a usage error (exit 2); so is specifying neither.
- When upgrading several personas, a failure on one is reported to stderr and skipped; the remaining personas are still attempted and the command exits non-zero if any failed.

Version comparison uses semantic-version ordering; non-semver version strings fall back to string inequality.

**Resolved-lock reporting (Epic 19.7).** For a persona that declares a `binding:` (see [authoring §6](personas-authoring.md#6-model-familychannel-bindings-and-resolved-locks-epic-197)), `upgrade` re-resolves the binding and advances the recorded slug **lock**, printing the before→after slug:

```bash
atcr personas upgrade anthony
# Upgraded anthony: anthropic/claude-opus-4.8 → anthropic/claude-opus-5.0
```

- A resolution that does not change the slug reports `<name>: <slug> (unchanged)`; a dry run reports `Would upgrade <name>: <from> → <to>`.
- `upgrade` is the **only** command that advances a lock. It is also the only path that contacts the model catalog — resolution never happens on the review path.
- **Major-bump verify flag:** when an upgrade crosses a major version (e.g. `4.x → 5.x`), the report appends `  ⚠ prompt tuned for the prior major — verify`. A major jump is additionally gated on the persona's fixture re-passing; if it does not, the lock is held and the command prints `Blocked <name>: <from> → <to> not applied — major version jump; … (lock unchanged)`. A minor advance auto-locks.

## The `atcr models` commands

The `models` command family inspects the resolved-slug locks against a checked-in catalog snapshot. It is read-only and deterministic — no network I/O on the default path.

### `atcr models check [name] [--json]`

Reports three conditions per installed community persona: a newer family member is available (**drift**), the locked slug is expiring (**deprecation**), or the locked slug is absent from the catalog (**missing**).

The shipped roster's locks are all current, so a fresh run prints the clean message. The block below is an **illustrative** example of the three line formats when conditions *are* present (the slugs are hypothetical):

```bash
atcr models check
# anthony: anthropic/claude-opus-4.8 → anthropic/claude-opus-5.0 (newer member)
# glenna: z-ai/glm-5.2 → z-ai/glm-6.0 (newer member)
# quinn: qwen/qwen3-coder-plus no longer in catalog (missing)
```

- A clean run — including every persona in the shipped roster today — prints `No drift, deprecation, or missing-slug conditions found.`
- **Exit codes:** `0` = clean, `1` = one or more conditions found, `2` = usage or command failure. This exit-code contract is the seam Epic 19.8's mechanical maintenance agent wraps.
- `--json` emits a machine-readable array (one object per condition); an empty result is `[]`.
- `check` changes nothing — it only reports. Use `atcr personas upgrade` to act on drift.
- The comparison uses a catalog snapshot compiled into the binary. Point `ATCR_CATALOG_SNAPSHOT` at a file to compare against a different snapshot.

### `atcr models refresh` (maintainer-only)

Regenerates the checked-in catalog snapshot from a live OpenRouter `/api/v1/models` fetch. This is a **maintainer** command — it is the only live-network touchpoint and is never run in CI:

```bash
OPENROUTER_API_KEY=… atcr models refresh
# Wrote 344 models to internal/personas/testdata/catalog_snapshot.json
```

- On the live path it **requires `OPENROUTER_API_KEY`** and refuses to run under a CI environment, failing closed (exit 2) so CI can never fetch live.
- It refuses to overwrite the snapshot with an empty catalog and leaves the existing snapshot untouched on any fetch or write error (atomic replace).
- A refreshed snapshot reaches the default `models check` path by rebuilding the binary (the snapshot is embedded at build time) or, at runtime, via the `ATCR_CATALOG_SNAPSHOT` override.

The catalog schema, exit-code contract, and `--json` shape are specified in the plan documentation: [models-check-command.md](../.planning/sprints/active/19.7_live_model_resolution/plan/documentation/models-check-command.md), [openrouter-catalog-api.md](../.planning/sprints/active/19.7_live_model_resolution/plan/documentation/openrouter-catalog-api.md), and [catalog-snapshot-fixture.md](../.planning/sprints/active/19.7_live_model_resolution/plan/documentation/catalog-snapshot-fixture.md).

## Reproducible by default: locks, not live models

Reviews are **reproducible by default**. A persona's `model` field is a resolved **lock** — a concrete slug — and every review runs that locked slug. The resolver and the model catalog endpoint are **never touched on the review hot path**: a clean diff can never sprout new findings from a model that silently changed underneath it.

The model changes only when you explicitly run `atcr personas upgrade`, which re-resolves any `binding:`, advances the lock, and reports exactly what changed. A persona installed before Epic 19.7 needs no migration — its pinned `model` value already serves as its initial lock. Silent runtime "always latest" resolution is deliberately not offered; opting into a floating channel is done through a persona's `binding:` at authoring time, not at review time. The reproducibility posture and the `fetch()`/`Upgrade()` reuse seams are detailed in [existing-resolver-patterns.md](../.planning/sprints/active/19.7_live_model_resolution/plan/documentation/existing-resolver-patterns.md).

## Discover and install a persona by model

Each community persona carries structured `provider`/`model` metadata (see [personas-authoring.md](personas-authoring.md)), so you can find one by the model you already have — search matches the structured `model` in `index.json`, not free-text. The end-to-end flow:

```bash
# Discover a persona tuned for the model you have
atcr personas search --model deepseek
# or filter by the routing-endpoint provider
atcr personas search --provider openrouter

# Install the discovered persona (writes ~/.config/atcr/personas/, pins the YAML version)
atcr personas install delia

# Confirm it is installed and pinned
atcr personas list

# Run its fixture to verify it matches the model-in-metadata convention
atcr personas test delia
```

- `search --model <substring>` matches a persona's bound model (case-insensitive substring); `search --provider <key>` matches its routing-endpoint provider. In this example `delia` is the DeepSeek-tuned persona (bound model `deepseek/deepseek-v4-pro`, routed through `openrouter`).
- `install` writes the persona unit to `~/.config/atcr/personas/` and pins the version from the YAML's `version` field; `upgrade` advances the pin when the registry advertises a newer one.

## Provider tiers beyond Synthetic

`atcr quickstart` sets up Synthetic (flat-rate) as the one-command default. When you need other models, these are the options, in recommended order:

### DashScope (Alibaba) — secondary flat-rate option

A flat-rate alternative to switch to after trying Synthetic. There is no `atcr quickstart` wiring for it this release — configure it by hand in `~/.config/atcr/registry.yaml`, then set its key in your environment (the key is never written into atcr's own config):

```yaml
providers:
  dashscope:
    api_key_env: DASHSCOPE_API_KEY
    base_url: https://<dashscope-openai-compatible-endpoint>/v1  # from DashScope's own docs
agents:
  qwen-reviewer:
    provider: dashscope
    model: <a-dashscope-hosted-model-id>
    role: reviewer
```

DashScope exposes an OpenAI-compatible endpoint; take the exact `base_url` and model ids from DashScope's documentation.

### Chutes → Featherless — explore, not default

More models, but with caveats: slower inference, tighter context windows, and concurrency limits. Try Chutes first, then Featherless. Treat both as explore, not default — do not place them ahead of Synthetic in the funnel.

### LiteLLM — Advanced

An OpenAI-compatible proxy that aggregates several providers behind a single endpoint. Keep it Advanced — it is not a first-run path. Point atcr's `base_url` at the proxy and treat it as one provider; see [providers.md](providers.md) for the full proxy setup (LiteLLM already covered there).

### Frontier / majors personas — opt-in, bring your own key

Claude/GPT/Gemini-tuned personas — each prompt-phrased per that provider's own official prompting guide — are installed deliberately by anyone who already holds that provider's API key. They stay opt-in and outside the default funnel: discover and install one by the model you have (see the discover-by-model flow above), then set that provider's key in your registry. They are never part of the `atcr quickstart` funnel.

### Local / Privacy-First (zero data egress) — opt-in

For privacy-conscious teams that refuse to send proprietary code to any external API, the library ships three personas tuned for a **local** endpoint (Ollama / llama.cpp / vLLM). Every byte of the review stays on your own hardware — no external network calls once the provider points at `localhost`. Each persona is tuned for a hardware tier:

| Persona | Tier | Bound model | Review lens |
|---------|------|-------------|-------------|
| `gerald` | 32 GB dense (file-by-file) | `local/gemma3-27b` | secrets & data-egress |
| `orson` | 32 GB long-context (256k, full-repo) | `local/qwen3-30b-a3b` | duplication & repo-wide redundancy |
| `liam` | 64 GB+ heavyweight (dual-GPU / M4 Pro) | `local/llama3.3-70b` | invariants & state-consistency |

The **Bound model** column is the discovery id the persona registers under (namespaced so `search --model` finds it); it is not the string your local server answers to. When you wire the persona, set your agent's `model` to the exact tag you pulled — e.g. `gemma3:27b`, not `local/gemma3-27b` (see the config block below). The persona ships the tuned prompt; you supply the model binding.

Discover them by their `local` provider — this is the primary, unambiguous path because it filters out cloud personas that share a vendor token:

```bash
atcr personas search --provider local
```

You can also search by model (`gemma`, `qwen`, or `llama`), but note that `qwen` returns both the local `orson` persona and the cloud `quinn` persona, which share the same vendor token. Use `--provider local` to disambiguate.

**Set it up entirely offline:**

```bash
# 1. Install and start a local model server
brew install ollama
ollama serve &                      # exposes an OpenAI-compatible endpoint on :11434

# 2. Pull the model a persona binds (match the tier to your RAM/VRAM)
ollama pull gemma3:27b              # gerald   (32 GB dense)
# ollama pull qwen3:30b-a3b         # orson    (32 GB long-context)
# ollama pull llama3.3:70b          # liam     (64 GB+ heavyweight)

# 3. REQUIRED even for a keyless local endpoint — see the note below
export OLLAMA_API_KEY=local-no-key-needed
```

**Wire the `local` provider in `~/.config/atcr/registry.yaml`** (the persona's `provider: local` resolves to this block; see [providers.md](providers.md) for the canonical example):

```yaml
providers:
  local:
    base_url: http://localhost:11434/v1   # ollama, llama.cpp, vllm, ...
    api_key_env: OLLAMA_API_KEY           # must name an env var that is set — even locally
agents:
  gerald:
    provider: local
    model: gemma3:27b                     # the tag you pulled with `ollama pull`
    role: reviewer
```

> **`api_key_env` placeholder requirement.** atcr requires every provider to declare `api_key_env`, and it errors at invoke time if that env var is unset or empty — even for a local endpoint that needs no real key. Export any non-empty placeholder value (`export OLLAMA_API_KEY=local-no-key-needed`) so the review can run. The value is never sent anywhere your `base_url` does not point; with `base_url` on `localhost`, nothing leaves the machine.

Then run a review as usual — `gerald`, `orson`, and `liam` behave like any other reviewer persona, only their traffic terminates at your local server instead of a cloud router. These personas are never part of the `atcr quickstart` funnel; they are a deliberate opt-in.

## Quick walkthrough

```bash
# 1. Discover a persona
atcr personas search security

# 2. Install it
atcr personas install security/owasp

# 3. Confirm it landed
atcr personas list

# 4. Run its fixture
atcr personas test security/owasp

# 5. Later, keep it current
atcr personas upgrade security/owasp

# 6. Remove it when you no longer need it
atcr personas remove security/owasp
```

For authoring your own persona (prompt template, `language` scope, fixture, and the contribution checklist), see [personas-authoring.md](personas-authoring.md). For the full registry schema — including how the `language` field drives skeptic routing — see [registry.md](registry.md).
