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

## The six subcommands

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

Runs a persona against its committed fixture — with no LLM and no network — and reports pass/fail. It works for built-in personas and for embedded/installed community-library personas: the fixture renders the persona template against a known diff and confirms the expected finding category and a clean render.

```bash
atcr personas test delia
# PASS: delia (1/1 cases)
```

The output contract:

- All cases passing reports `PASS: <name> (N/N cases)` (exit 0).
- Any case failing reports `FAIL: <name> (P/N cases)` to stdout and exits non-zero.
- A persona with no committed fixture reports `No fixture defined for persona "<name>"` and exits 0.

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
