# Configuration Reference

atcr reads configuration from a user file, optional project files, and embedded defaults, resolved by a strict precedence chain. Every file is strictly parsed: **unknown keys are load errors**, so configs stay typo-safe, and every validation failure surfaces in a second at load time — not after a 10-minute timeout. All faults in a config are reported together (each naming the file that defined the offending entry), so you fix them in a single pass rather than one error per run.

| File | Scope | Holds |
|------|-------|-------|
| `~/.config/atcr/registry.yaml` | User | Providers, agents, and user-level defaults for the shared review settings. Personas live as `.md` files beside it. Can instead be fetched from a shared URL — see [Shared team registry](#shared-team-registry-remote-registryyaml). |
| `.atcr/config.yaml` | Project | The agent roster (which agents run, in which lane) and project defaults. Written by `atcr init`. |
| `.atcr/registry.yaml` | Project (optional) | Project-defined providers and agents, merged over the user registry (project entries win by name). Lets a repo ship a self-contained review setup. See [Project registry overlay](#project-registry-overlay). |
| `~/.config/atcr/trusted_providers.yaml` | User | Allow list pinning which project-defined providers may receive a key. Managed by `atcr trust`. |

## Three concepts, deliberately decoupled

- **Provider** — an OpenAI-compatible endpoint + key environment variable. See [providers.md](providers.md).
- **Persona** — a named prompt: lens, personality, severity rubric. atcr ships six (bruce/generalist+correctness, greta/algorithmic, kai/architecture, mira/production, dax/tests+error-paths, otto/style+idiom); `atcr init` writes editable copies into `.atcr/personas/`.
- **Agent** — a provider+model binding that references a persona. Fallback agents reference the *same persona* — a fallback is by construction the same lens on a different model, never duplicated prompt text.

## `registry.yaml` (user level)

```yaml
providers:
  openrouter:
    api_key_env: OPENROUTER_API_KEY
    base_url: https://openrouter.ai/api/v1

agents:
  bruce:
    persona: bruce
    provider: openrouter
    model: anthropic/claude-3.7-sonnet
    temperature: 0.3
    timeout_secs: 600
    fallback: bruce-backup
  bruce-backup:
    persona: bruce
    provider: openrouter
    model: anthropic/claude-3.5-haiku

# Optional user-level defaults (the tier between project config and embedded defaults):
payload_mode: blocks
timeout_secs: 600
payload_byte_budget: 524288
max_parallel: 10
cache_max_bytes: 52428800
fail_on: HIGH
# Retry/backoff (Epic 4.6) — registry (global) tier only; not carried by project config or CLI.
max_retries: 5
initial_backoff_ms: 500
```

### Provider fields

| Field | Required | Notes |
|-------|----------|-------|
| `api_key_env` | yes | Name of the environment variable holding the key. Must be a valid POSIX env var name (`^[A-Za-z_][A-Za-z0-9_]*$`). The key is **never stored in config** — it is resolved from the environment at invoke time. |
| `base_url` | no | An `http`/`https` URL. Must not embed credentials (userinfo). |

### Agent fields

| Field | Default | Notes |
|-------|---------|-------|
| `provider` | (required) | Must name a provider defined above. |
| `model` | (required) | Model id at that provider. |
| `persona` | agent name | Selects the prompt; resolution chain below. |
| `temperature` | 0.7 | Must be within `[0, 2]`. |
| `timeout_secs` | inherited | Covers the whole invocation. When unset, the agent inherits the resolved shared timeout (precedence chain); set it to override per agent. Must be within `1..86400`. |
| `rate_limited` | false | `true` places the agent in the serial lane. |
| `fallback` | — | Another agent name, tried when this one fails. Chains are validated at load (dangling refs and cycles fail fast). |
| `payload` | inherited | Per-agent payload mode override (`diff`, `blocks`, `files`). When unset, inherits the resolved shared payload mode. See [payload-modes.md](payload-modes.md). |
| `max_context_lines` | `1500` | Per-agent cap on a single chunk's diff line count, used **only** when `review_strategy: chunked` (ignored in `bulk`). The fan-out bin-packs the persona's diff into chunks no larger than this and sends one call per chunk, so a lower value means smaller, more focused context (and more requests); a higher value packs more per call. When unset, inherits the `1500`-line default. Must be within `1..1000000`. A single file larger than this cap is sent as its own oversized chunk with a warning (a file is never split). |
| `max_retries` | inherited | Per-agent retry budget for rate-limit/transient failures (429, 5xx, transport). When unset, inherits the resolved shared budget (default `5`); set it to override per agent. Must be within `0..10` (`0` = a single attempt, no retry). The `Retry-After` header is always honored regardless of this value. |
| `initial_backoff_ms` | inherited | Per-agent base delay (ms) between retries when no server `Retry-After` is present; the schedule grows exponentially from here (×1.5, capped at 30s). When unset, inherits the resolved shared delay (default `500`). Must be within `1..30000`. |

## Shared team registry (remote `registry.yaml`)

A team can share **one** registry instead of every workstation maintaining its own. Set `ATCR_REGISTRY_URL` to the URL of a `registry.yaml` and atcr fetches the **user-level** registry over HTTP at load time instead of reading `~/.config/atcr/registry.yaml`:

```bash
export ATCR_REGISTRY_URL="https://raw.githubusercontent.com/acme/atcr-config/main/registry.yaml"
atcr doctor        # loads providers/agents from the remote registry
```

**The config-repo pattern.** Commit a `registry.yaml` to a shared git repo (e.g. an internal `atcr-config` repository) and point every workstation and CI runner at its raw URL. A registry change — a new model, a re-tuned agent, an added skeptic — lands in one place and everyone picks it up on the next run. The remote file uses the **exact same shape** documented above (`providers:` / `agents:` / user-level defaults); it flows through the identical strict parser and validation, so a typo or a dangling `fallback` in the shared file fails the load the same way a local one would.

**Only the user registry is remote.** The optional project overlay (`.atcr/registry.yaml`), the trust store (`~/.config/atcr/trusted_providers.yaml`), and personas stay local. The remote file is the *base*; a repo's local `.atcr/registry.yaml` still merges over it with the usual project-wins semantics ([overlay](#project-registry-overlay)).

**Local vs. remote — resolved by whether the variable is set:**

| `ATCR_REGISTRY_URL` | Source of the user registry |
|---------------------|-----------------------------|
| unset (or whitespace) | Local `~/.config/atcr/registry.yaml` (unchanged behavior). |
| set to an `http`/`https` URL | Fetched over HTTP; the local file is **not** read. |

**No silent fallback.** When the variable is set but the URL is unreachable, returns a non-2xx status, or serves invalid YAML, the load **fails with a clear error** — it does **not** quietly fall back to a local file. This is deliberate: a team sharing one registry should be told their shared source is broken, not silently diverge onto a stale local copy. The local read happens **only** when the variable is unset.

**Keys are never read from the remote file.** The env-var contract is unchanged: a provider names an `api_key_env` (the *variable name*), and the key value is resolved from the **local** environment at invoke time — it never travels in any registry, remote or local. Because the schema has no `api_key` field and every registry is strictly parsed, a literal secret pasted into the shared file (`api_key: sk-…`) is an **unknown-field load error**, not a silently-honored credential. Keep secrets in each workstation's environment; share only the wiring.

**Security posture.** Both `http` and `https` URLs are accepted, but a non-`https` URL draws a one-time warning on stderr — a shared registry is fully trusted (it defines the endpoints your local keys are sent to), so prefer `https` so it cannot be tampered with in transit. Any credentials embedded in the URL (`https://user:pass@…`) are redacted from warnings. The fetch is bounded by a short timeout and a response-size cap. Authentication beyond what a plain HTTP GET supports (bearer tokens, signed URLs, secrets managers) is out of scope; host the shared registry somewhere a simple GET can reach.

## `.atcr/config.yaml` (project level)

```yaml
# Roster entries must match agent names in ~/.config/atcr/registry.yaml.
agents:
  - bruce
  - greta
  - kai
serial_agents: []
payload_mode: blocks
timeout_secs: 600
payload_byte_budget: 524288
max_parallel: 10
cache_max_bytes: 52428800
fail_on: HIGH
```

| Field | Default | Notes |
|-------|---------|-------|
| `agents` | (required†) | The parallel-lane roster. Every entry must exist in the registry. †May be empty when `serial_agents` is non-empty — only a roster empty in both lanes is rejected. |
| `serial_agents` | `[]` | The serial-lane roster (sequential execution, for rate-limited providers). |
| `payload_mode` | `blocks` | One of `diff`, `blocks`, `files`. |
| `review_strategy` | `bulk` | Fan-out strategy: `bulk` (default — the whole diff goes to each persona in one prompt, keeping API cost strictly bounded) or `chunked` (each persona's diff is bin-packed into multiple context-limited calls, trading extra requests for smaller per-call context to curb large-diff hallucination). When `chunked`, each bin's size is capped by the agent's `max_context_lines`. Resolves at the registry and project tiers only (no CLI flag). **Chunking splits on `diff --git a/` file boundaries**, so it applies to the `diff` and `blocks` payload modes (both carry those headers); under `payload_mode: files` — which emits full-file bodies with no diff headers — a `chunked` run finds no boundaries and degrades to a single bulk chunk. |
| `on_overflow` | `chunk` | Degradation policy when a payload exceeds the model's effective byte budget: `chunk` (default — split the diff into window-sized chunks, preserving all files), `truncate` (drop lowest-priority files and record the truncation), `fallback` (recognized but not yet dispatched; requires fallback-provenance recording), or `fail` (hard-fail the slot). Resolves at the registry and project tiers only (no CLI flag). |
| `max_sprint_plan_bytes` | `65536` | Byte ceiling for the `--sprint-plan` file's SCOPE CONSTRAINT block. The plan body is truncated to this size before injection; `0` and negative values are rejected. Resolves at the registry and project tiers only (no CLI flag). |
| `timeout_secs` | `600` | Global fan-out timeout. Must be positive and `≤ 86400`; an explicit `0` is rejected (not silently defaulted). |
| `payload_byte_budget` | `524288` | Per-payload byte budget (512 KiB ≈ 128k tokens). Files are dropped largest-first when a payload exceeds it, recorded per agent in `status.json`. `0` = unlimited; negative is rejected. CLI override: `atcr review --byte-budget N`. **Context sizing:** models with context limits below 128k will time out or fail on the default; set to `163840` (160 KiB ≈ 40k tokens) for rosters that include smaller-context models (e.g. 49k-limit). |
| `max_parallel` | `10` | Cap on concurrent parallel-lane agent calls. Bounds the fan-out so a large roster cannot burst every provider call at once. When `serial_agents` is non-empty, the serial lane runs concurrently with the parallel lane in its own goroutine — peak provider concurrency is therefore `max_parallel + 1`, not `max_parallel`. `0` = unbounded; negative is rejected. CLI override: `atcr review --max-parallel N`. |
| `cache_max_bytes` | `52428800` | Total-size cap (bytes, 50 MiB default) for the diff cache under `.atcr/cache`. A re-run over an unchanged diff replays each reviewer's prior output and skips the LLM call; cache keys combine the payload digest, model id, and persona digest. Least-recently-used entries are evicted once the cap is exceeded. `0` = unbounded; negative is rejected. Bypass a single run's cache reads with `atcr review --no-cache` (fresh results are still written back). Only the review fan-out is cached — tool-enabled agents (live code reads) and the verification stage are not. |
| `fail_on` | `HIGH` (template only) | CI gate threshold (see [ci-integration.md](ci-integration.md)). The `HIGH` value is seeded into the config `atcr init` generates; the gate itself is opt-in — an unconfigured project does not gate. |

An agent may not appear twice, and may not appear in both `agents` and `serial_agents`.

## Project registry overlay

A repository can ship its own providers and agents in **`.atcr/registry.yaml`**, so a clone is self-contained — no contributor has to mirror agent definitions into `~/.config/atcr/registry.yaml` by hand. The overlay reuses the exact `providers:` / `agents:` shapes documented above (including the reserved fields) and is strictly parsed like every other config file. It carries **definitions only** — shared settings such as `payload_mode` belong in `.atcr/config.yaml`, so a settings key here is an unknown-field load error.

```yaml
# .atcr/registry.yaml — project-defined providers and agents (optional).
providers:
  team-llm:
    base_url: https://llm.team.example/v1
    api_key_env: TEAM_LLM_KEY
agents:
  team-reviewer:
    provider: team-llm        # a project provider…
    model: team-model
    fallback: bruce           # …may fall back to a user-defined agent, and vice versa
```

**Merge semantics — whole-entry shadowing, project wins.** The effective registry is the user registry with project entries merged in by name: a project provider or agent with the same name as a user one **replaces it entirely** (no field-level deep merge — drop the project entry to restore the user definition), and a new name is simply added. Validation — roster references, fallback dangling/cycle checks, persona refs, and range checks — all run over the **merged** view, so a chain may span tiers; any error names the file that defined the offending entry (`registry.yaml` vs `.atcr/registry.yaml`).

### Trust model for project-defined providers

A project provider names a `base_url` and an `api_key_env`, so a cloned repo could otherwise direct one of your keys to an arbitrary endpoint. atcr therefore **refuses to send a key to a project-defined provider until you explicitly trust it**:

```bash
atcr trust                 # list project providers and their trust status
atcr trust team-llm        # authorize one (prints its base_url + key env first)
atcr trust --all           # authorize every project provider
```

`atcr trust` pins the provider's `(base_url, api_key_env)` pair by sha256 in `~/.config/atcr/trusted_providers.yaml`. Change either field and trust must be re-granted — this is what stops a repo from silently re-pointing a trusted key at a new host. **Only `api_key_env` (the variable name) is ever stored; the key value never enters any file.** A project **agent** that references an existing **user-defined** provider needs no trust prompt — only project-defined *providers* are gated. Until a project provider is trusted, `atcr review` and `atcr doctor` fail fast (exit 2) naming the provider and the `atcr trust` remedy. On a run that does use trusted project providers, a one-time banner names each provider's `base_url` and key env on stderr (confirmation, not the gate).

## Precedence

The shared review settings (`payload_mode`, `review_strategy`, `on_overflow`, `max_sprint_plan_bytes`, `timeout_secs`, `payload_byte_budget`, `max_parallel`, `cache_max_bytes`, `fail_on`) resolve **per field, independently**, in this order:

```
CLI flag  >  .atcr/config.yaml  >  registry.yaml  >  embedded default
```

A tier participates only where it explicitly sets a value; whitespace-only values count as unset, and a set-but-empty CLI flag is treated as unset rather than clobbering lower tiers. CLI values are validated at resolution time (they bypass the file-load checks), so an invalid `--payload`, out-of-range `--timeout`, or negative `--max-parallel` fails before any review work begins. Embedded defaults: `payload_mode=blocks`, `timeout_secs=600`, `payload_byte_budget=524288`, `max_parallel=10`, `cache_max_bytes=52428800`. `cache_max_bytes` resolves across the registry and project tiers only (no CLI flag); `--no-cache` is a per-run read bypass, not a settings override. There is **no embedded default for `fail_on`**: the gate is opt-in, and `fail_on` resolution stops at the registry tier (`--fail-on` flag > project config > registry). The `fail_on: HIGH` line in a freshly generated config comes from the `atcr init` template, not from gate resolution.

The retry tunables (`max_retries`, `initial_backoff_ms`, Epic 4.6) resolve over a **shorter chain**: agent-level field > `registry.yaml` (global) > embedded default (`max_retries=5`, `initial_backoff_ms=500`). They are deliberately **not** carried by `.atcr/config.yaml` or a CLI flag — rate-limit resilience is a property of the user's provider/account, set once at the registry tier or refined per agent. An agent's effective budget is threaded onto each call so per-agent overrides take effect without rebuilding the shared client.

The same **project-over-user** rule now applies uniformly across all three kinds of configuration — settings, personas, and definitions:

```
CLI flag  >  .atcr/*  (project)  >  ~/.config/atcr/*  (user)  >  embedded default
```

Settings resolve per field (above); personas resolve per file (`.atcr/personas/` shadows `~/.config/atcr/personas/`, chain below); and provider/agent **definitions** merge whole-entry (`.atcr/registry.yaml` shadows `~/.config/atcr/registry.yaml`, [overlay](#project-registry-overlay) above). There is no embedded tier for definitions — providers and agents must be defined in at least one registry file.

**Generated configs shadow registry defaults.** `atcr init` bakes explicit `payload_mode`, `timeout_secs`, `payload_byte_budget`, `max_parallel`, `cache_max_bytes`, and `fail_on` values into `.atcr/config.yaml` so every knob is visible and editable. Because the project tier outranks the registry tier, those baked lines shadow any user-global defaults set in `registry.yaml` — an initialized project never inherits registry-tier values for them. To inherit a registry-tier value, delete the corresponding line from the project config (for `payload_mode` and `fail_on`, a whitespace-only value also counts as unset).

## Persona resolution chain

For each agent, the prompt is resolved by walking six levels in order, first hit wins:

1. **Programmatic task-message override** — an internal resolution seam (the `taskMessage` argument to persona resolution) that, when set, wins outright even when empty (an explicit "no system prompt"). It is **not** exposed as a CLI flag, so ordinary CLI and MCP runs pass nothing here and resolution effectively begins at level 2.
2. **`<persona>.md` in the project personas dir** (`.atcr/personas/`).
3. **`<persona>.md` in the registry personas dir** (`~/.config/atcr/personas/`).
4. **`_base.md`** in the project dir, then the registry dir.
5. **embedded `<agentName>.md`**, then
6. **embedded `_base.md`**.

When `persona` names a value other than the agent name (an explicit ref) and no file exists at level 2 or 3, resolution **fails** with "persona not found" — an explicit ref never silently falls through to a base or embedded default. Empty or whitespace-only persona files are treated as missing (with a stderr warning). Symlinked persona files are refused for safety (persona text is fed verbatim into prompts). Persona and agent names are sanitized against path traversal: no path separators, no `..`, no leading dot, and `_base` is reserved for the shared base template.

## Fallback-chain validation

Fallback references are validated when the registry loads:

- A `fallback` pointing at an agent that does not exist → **dangling fallback** error.
- A cycle (`a → b → a`, or any longer loop) → **fallback cycle** error, reported with the offending path.

Both fail the load immediately, so a misconfigured chain can never surface mid-run.

## Tool-using reviewer fields (active in 2.0)

Epic 2.0 turns the pool reviewers into bounded, tool-using agents. The fields below were **reserved in 1.1/1.x** (parsed and type-validated but inert) and are now **active in 2.0**: the engine acts on them to drive the multi-turn tool loop. A registry written for 1.x that set these fields keeps working unchanged — the values now take effect instead of being ignored.

| Field | Type | Default | Validated at load | Behavior |
|-------|------|---------|-------------------|----------|
| `tools` | bool (per agent) | `false` | type only | `true` enables the multi-turn tool loop (`read_file`, `grep`, `list_files`) for that agent. Default `false` runs the 1.0 single-shot path. |
| `max_turns` | int (per agent) | `10` when `tools: true` | must be within `1..1000` | Caps the number of Chat-with-tools turns per agent. The default `10` is applied at load **only when `tools: true`**; a non-tool agent leaves it unset. The hard upper bound (`1000`) backstops a runaway loop. |
| `tool_budget_bytes` | int (per agent) | `0` (unlimited) | must be `>= 0` | Caps cumulative tool-result bytes delivered to the model across the run. `0 = unlimited`; a negative value is rejected at load. |
| `supports_function_calling` | bool (per model/agent) | `false` | type only | Declares that the agent's model speaks the OpenAI function-calling wire format. **Required for a `tools: true` agent to run the loop** — a `tools: true` agent whose model is not declared capable degrades to single-shot and records `tools_degraded: true`. There is no runtime probing; capability is registry-declared. |

**Safety:** set `tools: true` **only** for a model you have also marked `supports_function_calling: true`. A `tools: true` agent on a non-capable model degrades cleanly to single-shot rather than looping, but the intent will not run — keep the two flags consistent. Tool agents are read-only and path-jailed (no write tools, no shell, no network); see [payload-modes.md](payload-modes.md) for the payload-as-starting-point semantics and the per-agent cost in the [README](../README.md).

An out-of-range value (e.g. `max_turns: 0`, a negative `tool_budget_bytes`, an unknown `role`) is a load error, so a misconfiguration is caught in a second at load rather than mid-run.

## Skeptic agents (`role: skeptic`, active in 3.0)

The `role` field selects what an agent does in the pipeline. It was **reserved (parsed but inert) in 1.1/1.x** and is **active in 3.0** for the adversarial-verification stage:

| Value | Active in | Purpose |
|-------|-----------|---------|
| `reviewer` | 1.x | Produces findings in the review fan-out. The default when `role` is unset. |
| `skeptic` | 3.0 | Attempts to **refute** reconciled findings in `atcr verify` (see [verification.md](verification.md)). Also casts the **challenger** seat in `atcr debate`. |
| `judge` | 6.0 | Rules on disputed findings in `atcr debate` — the **judge** seat that settles a cross-examination (see [cross-examination.md](cross-examination.md)). |

An agent with no `role` defaults to `reviewer`, so every 1.x registry keeps working unchanged. The enum is validated at load (`reviewer`, `skeptic`, `judge`); any other value is a load error.

A skeptic is an ordinary agent — provider, model, persona — flagged `role: skeptic`. Because a skeptic reads the actual code to refute a finding, give it `tools: true` and `supports_function_calling: true` (same tool-loop fields as a tool-using reviewer, above):

```yaml
agents:
  otto:                       # a reviewer (role defaults to reviewer)
    persona: otto
    provider: openrouter
    model: openai/gpt-4o

  skeptic-sonnet:             # a skeptic on a different model than the reviewers
    persona: otto             # any persona; the skeptic prompt is engine-supplied
    provider: openrouter
    model: claude-sonnet-4-6
    role: skeptic
    tools: true               # reads the cited code to confirm/refute
    supports_function_calling: true
```

**Different-model rule.** The verify engine never selects a skeptic whose `model` exactly matches the model of any reviewer credited on the finding — a model cannot verify its own work, even indirectly through a shared blind spot. This is enforced by the engine, not left to configuration discipline. If no eligible skeptic remains for a finding, that finding is recorded `unverifiable` (reason `no_eligible_skeptic`) and keeps its v1 confidence — it is never dropped. Roster enough skeptics on distinct models that every reviewer model is covered.

The verification stage also reads an optional registry-level `verify:` block (`min_severity`, `votes`) — see [verification.md](verification.md#cost-controls) for the full mechanics.

```yaml
verify:
  min_severity: MEDIUM        # findings below this floor skip verification
  votes: 1                    # skeptics consulted per finding
```

| Key | Default | Notes |
|-----|---------|-------|
| `min_severity` | `MEDIUM` | Findings below this floor skip verification entirely and keep their v1 confidence. Override per run with `--min-severity`. |
| `votes` | `1` | Skeptics consulted per finding. With one vote the single verdict passes through; with multiple, a clear majority wins and a tie becomes `unverifiable`. |

## Language scope and skeptic routing (`language`, active in 9.0)

An agent may declare an optional `language` scope — the file extensions it specializes in — so the verification stage can prefer a language-matched skeptic over a generalist when refuting a finding. It is the routing lever behind the community persona ecosystem (Epic 9.0): a Go-specific persona is preferred on Go findings, a TypeScript one on `.ts` findings, and so on.

| Field | Type | Default | Validated at load | Effect |
|-------|------|---------|-------------------|--------|
| `language` | string[] (per agent) | — (no constraint) | each entry must be non-empty in canonical form and free of control characters | Declares the file extensions this agent specializes in. When a finding's file extension matches one of these, the agent is **preferred** in skeptic selection over an agent with no matching scope. Optional and backward-compatible — an omitted field imposes no constraint. |

**Canonical format — no leading dot, lowercased.** Entries are canonicalized at load: surrounding whitespace is trimmed, a single leading dot is stripped, and the value is lowercased. So `go`, `.go`, and ` .GO ` all store as `go` and compare identically against a finding's normalized file extension. Multiple values are allowed:

```yaml
agents:
  skeptic-go:
    provider: openrouter
    model: anthropic/claude-3.7-sonnet
    role: skeptic
    tools: true
    supports_function_calling: true
    language: ["go", "ts"]   # canonical: dotless, lowercased
```

Prefer writing the canonical form directly (`["go"]`). A leading-dot or mixed-case value such as `[".Go"]` is **not** an error — it is canonicalized to `["go"]` at load. What *is* rejected (a load error) is an entry that is empty, whitespace-only, or just a dot (`"."`), since each canonicalizes to a blank token that would match every extensionless finding; control characters are rejected too. There is **no** allow-list of known languages — third-party persona authors may declare any extension.

**Nil semantics.** Omit `language` (or leave it empty) and the agent carries no language constraint: it is eligible for **every** review regardless of the repository's detected language, with no routing preference. This is the default and keeps every pre-9.0 registry loading and routing unchanged.

### How routing works (`SelectEligibleSkeptics`)

When the verify stage picks skeptics for a finding, it first collects the eligible skeptics (excluding any whose model matches a crediting reviewer's — a model never verifies its own work), sorts them alphabetically for determinism, then applies a **two-partition reorder**:

1. **Matched partition** — skeptics whose `language` scope contains the finding's (normalized) file extension. These lead, so the per-finding skeptic cap favors them. Within this partition, ordering is by corroboration score (highest first, from prior-run scorecard data) then alphabetical; with no score data the partition is simply alphabetical.
2. **Unmatched partition** — every other eligible skeptic, in alphabetical order, appended after the matched ones.

**Silent, automatic fallback.** If no skeptic's `language` matches the finding — or the finding's file has no extension — the matched partition is empty and all eligible skeptics fall through to the unmatched partition. Selection proceeds exactly as it did pre-9.0: the review is never blocked for lack of a language-scoped skeptic, and no warning is emitted. A registry with only generalist (unscoped) skeptics routes precisely as before.

The runnable examples [`examples/registry-without-executor.yaml`](../examples/registry-without-executor.yaml) and [`examples/registry-with-executor.yaml`](../examples/registry-with-executor.yaml) each declare a `language` scope on an agent. See [personas-install.md](personas-install.md) and [personas-authoring.md](personas-authoring.md) for installing and authoring language-scoped community personas.

## Judge agents (`role: judge`, active in 6.0)

The cross-examination stage (`atcr debate`) casts three seats per disputed finding: a **proposer** (a crediting reviewer's agent), a **challenger** (a `role: skeptic` agent), and a **judge** (a `role: judge` agent). All three must be **distinct models** — enforced by the engine. Give the judge the strongest model available, with `tools: true` and `supports_function_calling: true` so it can read the cited code:

```yaml
agents:
  judge-opus:                 # the judge — a third distinct model, the strongest available
    persona: otto             # any persona; the judge prompt is engine-supplied
    provider: anthropic
    model: claude-opus-4-6
    role: judge
    tools: true
    supports_function_calling: true
```

When fewer than three distinct models are available across the roles, the stage records the item **unresolved** by default rather than loosening independence; opt in to a same-model persona fallback with `debate.allow_single_model: true` (or `--single-model`).

The stage reads an optional registry-level `debate:` block:

```yaml
debate:
  triggers:                   # which disputes to debate (default: all three)
    - severity_split
    - gray_zone
    - verification_disagreement
  max_items: 5                # cost cap; 0 = unlimited; overflow is recorded, never silent
  allow_single_model: false   # opt in to the same-model persona fallback
```

| Key | Default | Notes |
|-----|---------|-------|
| `triggers` | all three | Any subset of `severity_split`, `gray_zone`, `verification_disagreement`. An unknown trigger is a load error. |
| `max_items` | `5` | Highest-priority (by severity) disputes are debated; the rest are recorded as overflow. `0` = unlimited. A negative value is a load error. |
| `allow_single_model` | `false` | When `true`, fewer than three distinct models falls back to distinct personas on one model (disclosed as `single_model` in the artifacts and report). |

See [cross-examination.md](cross-examination.md) for the full mechanics, the judge envelope, and gate semantics.

## Executor (fix generation, active in 7.0)

The review panel catches bugs with multiple models; a single **executor** model generates fixes for the findings worth fixing. This keeps fix generation cheap (one model, not N) and consistent (one model, one style), while letting you spend a more capable model on fix quality than on review breadth.

Fix generation is **opt-in and additive**: add an optional top-level `executor:` block and the fix phase runs during `atcr verify`, `atcr review --verify`, and the `atcr_verify` MCP tool. Omit the block and ATCR behaves exactly as before — no fix phase, no executor tokens, no errors. The executor is **not** part of the review panel: it lives outside `agents:` and carries its own role (`executor`).

```yaml
executor:
  name: opus                   # attribution label written into the finding's evidence
  provider: anthropic          # must reference a provider in providers:
  model: claude-opus-4-8
  persona: fixer               # default: fixer
  role: executor               # must be "executor" (defaulted)
  min_severity_for_fix: MEDIUM # fix floor: LOW | MEDIUM | HIGH | CRITICAL (default MEDIUM)
  fix_timeout: 120             # optional per-fix timeout (seconds)
  temperature: 0.0             # API temperature [0,2]; default 0.0 (deterministic fixes)
  system_prompt: |             # optional: replaces the default fixer framing verbatim (supersedes persona)
    You are a senior Go engineer. Emit only gofmt-clean, idiomatic Go.
  rules:                       # optional coding guidelines appended to the fix prompt
    - Use tabs for indentation
    - Avoid panic() in library code; return errors instead
  agent_mode: false            # opt-in tool-loop fix generation (Epic 7.4); default false
  max_tool_calls: 10           # agent-mode tool-call budget per finding (1..1000; default 10)
```

| Key | Default | Notes |
|-----|---------|-------|
| `name` | `executor` | Attribution label; the finding's evidence gains `fix by <name>`. |
| `provider` | — | Required. Must reference a key in `providers:`. |
| `model` | — | Required. The fix-generation model id. |
| `persona` | `fixer` | Persona token used in the executor prompt. Superseded by `system_prompt` when that is set. |
| `role` | `executor` | Must be `executor` if set; any other value is a load error. |
| `min_severity_for_fix` | `MEDIUM` | A finding is fixed only when its severity is at or above this floor. Normalized to upper-case; a non-canonical value is a load error. |
| `fix_timeout` | inherit | Optional per-fix timeout in seconds; a non-positive or out-of-range value is a load error. |
| `temperature` | `0.0` | API temperature for fix calls. Must be within `[0, 2]`; an out-of-range value is a load error. Sent on every call — when omitted it defaults to `0.0` so fixes are deterministic (rather than inheriting the provider's own, often higher, default). |
| `system_prompt` | — | Optional. Replaces the default `"You are <persona>, a code-fix executor…"` framing verbatim; `persona` is then ignored for the call. The finding metadata, rules, and code snippet are still appended after it. Capped at 4096 characters. Unlike `persona`, `name`, and `rules`, control characters (including CR/LF) are **intentionally permitted**: a system prompt is a full, multi-line instruction block whose newlines are legitimate structure, not a mid-sentence token. The trust boundary is the registry itself — it is assumed self-authored, so a value that could forge prompt lines is your own input. Do not paste an unverified third-party registry's `system_prompt` without review. |
| `rules` | — | Optional list of coding guidelines appended to the fix prompt as a constraints block, so generated fixes match your project's conventions. Each rule must be non-empty, free of control characters, and at most 512 characters; a violation is a load error. |
| `agent_mode` | `false` | Opt-in (Epic 7.4). When `true`, the executor explores the codebase with read-only tools (`read_file`, `grep`, `list_files`) before proposing a fix, instead of working from a single pre-loaded snippet. Default `false` keeps the unchanged snippet path. |
| `max_tool_calls` | `10` | Agent-mode tool-call budget per finding (maps to the tool loop's `max_turns`). Must be within `1..1000` when set; a non-positive or over-cap value is a load error. Ignored when `agent_mode` is `false`. |

A fix is generated only for a finding that is **HIGH-or-better confidence** (so a `VERIFIED` finding — one a skeptic confirmed — is included) **AND** at or above `min_severity_for_fix`. The executor reads a snippet of the cited code from the review snapshot for context, then writes a minimal fix into the finding's `fix` column and appends `fix by <name>` to its `evidence` (no new column is added). Generation is idempotent per executor: a re-run does not re-fix an already-attributed finding. A failed or empty completion leaves the reviewer's own fix suggestion in place and never fails the run.

The `temperature`, `system_prompt`, and `rules` fields (Epic 7.0.1) let you control fix determinism and style without editing ATCR source — e.g. pinning `temperature: 0.0` for reproducible fixes, or adding `rules` so generated fixes pass your linters on the first CI run. Cross-provider temperature normalization (Anthropic vs OpenAI ranges) is handled at the gateway layer, not in ATCR: the value is validated to `[0, 2]` and passed through unchanged. **Caveat:** ATCR validates only against the generic `[0, 2]` range, not your provider's native ceiling. If you call a provider directly (no normalizing gateway) with a value above its native maximum — e.g. `1.8` against Anthropic's `1.0` ceiling — the provider rejects the request at call time, and the fix simply fails (the rejection surfaces in the run's fix-failed warning, not as a load-time error). Pin `temperature` within your provider's native range, or route through a gateway that normalizes it.

**Agent mode (`agent_mode`, Epic 7.4)** changes how the executor gathers context, not what it produces. In the default snippet path the executor sees only a fixed window (±30 lines) around the cited line. With `agent_mode: true` it instead borrows the same read-only tool harness the skeptics use and runs a read/search loop — reading the cited file, following imports or callers, grepping for a type definition — before proposing the fix. This reuses the dispatcher already open for verification: no second snapshot, no extra git checkout. The fix still lands only in the `fix` column (it is never executed), and failure isolation is unchanged — a tool-loop timeout, tripped budget, provider error, or unparseable response records a `fix` warning on the finding and never fails the run or drops a finding.

Use it when findings are **cross-file** (a fix that depends on a type, interface, or caller defined elsewhere), where a single snippet is too narrow. The trade-off is cost: each tool call is an extra model round-trip, so agent mode adds latency and tokens that the snippet path does not. `max_tool_calls` (default 10) caps that spend per finding — the executor must propose its fix from what it has read once the budget is reached. Raise it for deep cross-file work, lower it to bound cost; the hard ceiling is 1000 (the same cap the skeptic tool loop uses). If the tool harness is unavailable for a run (no skeptic ran and no snapshot was built), `agent_mode: true` degrades gracefully to the snippet path with a logged warning rather than failing.

The runnable examples [`examples/registry-with-executor.yaml`](../examples/registry-with-executor.yaml) and [`examples/registry-without-executor.yaml`](../examples/registry-without-executor.yaml) show both shapes side by side.

> Posting these fixes as inline PR comments is owned by a separate GitHub Action (Epic 7.3); this stage stops at populating the `fix` column in the findings artifacts.

## Review-constraint fields (active in 2.2)

Three optional per-agent guardrails bound what a reviewer contributes to the fan-out. They exist because a weak or mis-prompted model can drift off its role and flood the merged stream with low-value noise — e.g. hundreds of blank-line `LOW` style findings that bury the handful of `HIGH` findings stronger models caught. All three are optional and backward-compatible: an unset field imposes no constraint, so an existing registry keeps loading unchanged.

| Field | Type | Default | Validated at load | Effect |
|-------|------|---------|-------------------|--------|
| `scope` | string[] (per agent) | — (all categories) | every entry must be non-empty | **Soft** prompt-injection focus hint. The listed categories are appended to the agent's persona prompt as a "Review Focus" instruction steering it toward those areas. It is *not* a hard filter — out-of-category findings are never dropped, so a genuine cross-cutting issue is preserved. Use it to nudge a specialist (e.g. a performance reviewer) without silencing it. |
| `min_severity` | string (per agent) | `LOW` (no floor) | one of `CRITICAL`, `HIGH`, `MEDIUM`, `LOW` (case-insensitive; normalized to upper-case) | **Hard** floor enforced in fan-out post-processing: findings below this severity are dropped from the agent's `findings.txt` before reconciliation. A dropped count is logged to stderr. |
| `max_findings` | int (per agent) | — (unlimited) | must be `> 0` | **Hard** cap enforced in fan-out post-processing: the agent's findings are truncated to this many, keeping the **most severe** first (a severity-sorted cap, so a flood of `LOW` items can never push out a `HIGH` one). A truncated count is logged to stderr. |

`min_severity` and `max_findings` are enforced deterministically in the fan-out per-source path (right after the engine stamps the `REVIEWER` column from the agent key), never in the reconciler — the reconciler stays source-agnostic. `scope` is applied earlier, as soft prompt injection at agent build time.

**Put these fields on a *rostered* agent** (one listed in `agents` or `serial_agents`). A fallback inherits all three from the primary it stands in for — the constraint follows the slot, like the persona prompt — so `scope`/`min_severity`/`max_findings` set on an entry that is *only* reachable as a fallback are ignored: the primary's constraints govern whoever ultimately answers that slot.

```yaml
agents:
  bruce:
    persona: bruce
    provider: openrouter
    model: anthropic/claude-3.7-sonnet
  # A weaker model rostered directly (not as a fallback), constrained so it
  # contributes a focused, bounded review instead of flooding the stream:
  # performance-focused, no LOW noise, and a hard volume cap. Because it is a
  # primary roster entry, its own constraints below actually take effect.
  nemo:
    persona: bruce
    provider: openrouter
    model: nvidia/nemotron-nano
    scope: ["performance", "efficiency"]   # soft focus hint injected into the prompt
    min_severity: MEDIUM                    # drop LOW findings before reconciliation
    max_findings: 20                        # keep at most the 20 most severe
```

An out-of-range value (an unknown `min_severity`, a non-positive `max_findings`, or a blank `scope` entry) is a load error, so a misconfiguration is caught at load rather than mid-run.

## Verifying the configuration (`atcr doctor`)

`atcr doctor` is the recommended check to run right after `atcr init` and after any registry edit. It resolves the **effective roster** — every agent in `agents` and `serial_agents`, plus every agent reachable through `fallback` chains — deduplicates it to the distinct `(provider, model, base_url)` targets, and invokes each target **once** with a trivial nonce prompt. Success is verified by the marker appearing in the response content, not merely by HTTP 200.

```bash
atcr doctor                      # human-readable table
atcr doctor --json               # machine-readable, stable schema
atcr doctor --agents bruce,kai   # test only these listed agents (their fallback chains are still probed)
atcr doctor --max-tokens 4096    # raise the completion budget for thinking models
atcr doctor --timeout 30         # per-call timeout in seconds
```

Flags: `--max-tokens` (default `2048`), `--timeout` (default `60`s), `--json`, `--agents <a,b,...>` (restrict to a subset of the directly-listed agents — each selected agent's fallback chain is still probed so its health verdict stays accurate). The token budget defaults high on purpose: reasoning/thinking models spend completion tokens on internal reasoning, so a small budget can exhaust before the marker is emitted.

### Status classes

| Status | Meaning | Typical fix |
|--------|---------|-------------|
| `ok` | Marker returned in visible content | — |
| `ok_warning` | HTTP 200 but the marker was absent/empty (often a thinking model that spent the whole budget reasoning) | Raise `--max-tokens` |
| `auth_failed` | 401/403 | Check the API key in the provider's `api_key_env` |
| `not_found` | 404 | Check the `model` name and the provider `base_url` |
| `rate_limited` | 429 | Retry later, or test a smaller `--agents` subset |
| `provider_error` | 5xx or other non-classified HTTP error | Inspect the bounded error-body snippet in the row |
| `network_error` | Transport-level failure (DNS, connection, TLS) | Check connectivity and `base_url` host |
| `timeout` | Per-call deadline exceeded | Raise `--timeout` |
| `missing_key` | The provider's `api_key_env` variable is unset — reported **without any network call** | `export` the named variable |
| `invalid_config` | `base_url` empty or malformed — reported **without any network call** | Set a valid `http(s)` `base_url` |

A failure class always references the **environment variable name**, never its value: no secret is ever logged.

### Exit codes

- **0** — every directly-listed agent has at least one working invocation path (its primary target *or* any fallback in its chain). A `fallback` that works covers a failing primary.
- **1** — at least one listed agent has no working path.
- **2** — usage/configuration error (missing or invalid registry / project config, bad flag value, unknown `--agents` name).

### JSON schema (`--json`)

A stable top-level object with an `agents` array; one entry per effective-roster agent (fallback agents included):

```json
{
  "agents": [
    {
      "agent": "bruce",
      "serial": false,
      "provider": "openrouter",
      "model": "anthropic/claude-3.5-sonnet",
      "status": "ok",
      "latency_ms": 412,
      "hint": "",
      "detail": "",
      "source": "user"
    }
  ]
}
```

`hint` (actionable next step) and `detail` (a bounded, secret-redacted provider error snippet) are present only when relevant and omitted when empty. `source` is the agent's definition tier — `user` or `project` — surfaced so overlay shadowing is visible rather than silent; the human-readable table shows it as a `SOURCE` column. The doctor invokes each distinct target at most once, so several agents that share a `(provider, model, base_url)` report the same latency and status.
