# Installing Community Personas

ATCR ships with nine built-in reviewer personas (six generalists plus the `sentinel`, `tracer`, and `idiomatic` bonus personas). Beyond those, the `atcr personas` command installs **community-contributed** personas from a configurable repository, so you can extend the reviewer panel with domain-specific lenses — security, performance, framework-specific, and more — without editing your registry by hand.

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

By default, `install`, `search`, and `upgrade` fetch from the public community repository:

```
https://raw.githubusercontent.com/atcr/personas/main
```

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
# sentinel         built-in   built-in    -
# security/owasp   1.2.0      community   -
# language/go-fmt  0.3.0      community   go
```

Columns: `NAME`, `VERSION` (`built-in` for the built-in personas; the installed manifest version for community personas), `SOURCE` (`built-in` or `community`), and `LANGUAGE` (the persona's declared `language` scope, comma-joined, or `-` when unscoped). If the personas directory is unreadable, `list` prints a warning to stderr and still renders the built-ins (exit 0).

**With corroboration scores.** Add `--scores` to append a `CORROBORATION` column showing each persona's historical corroboration rate from past review runs:

```bash
atcr personas list --scores
# NAME             VERSION    SOURCE      LANGUAGE  CORROBORATION
# security/owasp   1.2.0      community   -         72.4%
# sentinel         built-in   built-in    -         n/a
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

Runs an installed persona against its fixture and reports pass/fail.

> **Current behavior:** the shipped CLI does not yet wire a fixture runner, so `atcr personas test <name>` reports `No fixture defined for persona "<name>"` and exits 0 for every persona today. The pass/fail contract below is what the command produces once a runner is wired (the runner is an injectable seam; fixture execution against the persona's `.patch` is tracked as follow-up work).

```bash
atcr personas test security/owasp
# No fixture defined for persona "security/owasp"
```

The full output contract:

- A persona with no runnable fixture reports `No fixture defined for persona "<name>"` and exits 0 (the current shipped behavior).
- All cases passing reports `PASS: <name> (N/N cases)` (exit 0).
- Any case failing reports `FAIL: <name> (P/N cases)` to stdout and exits non-zero.

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
